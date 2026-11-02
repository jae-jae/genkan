package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jae-jae/genkan/internal/auth"
	"github.com/jae-jae/genkan/internal/health"
	"github.com/jae-jae/genkan/internal/media"
	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/skills"
)

func (s *Server) routes() {
	m := s.mux
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok")
	})

	m.HandleFunc("GET /api/bootstrap", s.guard(permPublic, s.handleBootstrap))
	m.HandleFunc("POST /api/auth/setup", s.guard(permPublic, s.handleSetup))
	m.HandleFunc("POST /api/auth/login", s.guard(permPublic, s.handleLogin))
	m.HandleFunc("POST /api/auth/logout", s.guard(permPublic, s.handleLogout))
	m.HandleFunc("POST /api/auth/password", s.guard(permAdmin, s.handlePassword))

	m.HandleFunc("PATCH /api/settings", s.guard(permAdmin, s.handleSettings))

	m.HandleFunc("POST /api/workspaces", s.guard(permAdmin, s.handleWorkspaceCreate))
	m.HandleFunc("POST /api/workspaces/reorder", s.guard(permAdmin, s.handleWorkspaceReorder))
	m.HandleFunc("PATCH /api/workspaces/{id}", s.guard(permAdmin, s.handleWorkspaceUpdate))
	m.HandleFunc("DELETE /api/workspaces/{id}", s.guard(permAdmin, s.handleWorkspaceDelete))

	m.HandleFunc("POST /api/categories", s.guard(permAdmin, s.handleCategoryCreate))
	m.HandleFunc("POST /api/categories/reorder", s.guard(permAdmin, s.handleCategoryReorder))
	m.HandleFunc("PATCH /api/categories/{id}", s.guard(permAdmin, s.handleCategoryUpdate))
	m.HandleFunc("DELETE /api/categories/{id}", s.guard(permAdmin, s.handleCategoryDelete))

	m.HandleFunc("POST /api/links", s.guard(permAdmin, s.handleLinkCreate))
	m.HandleFunc("POST /api/links/reorder", s.guard(permAdmin, s.handleLinkReorder))
	m.HandleFunc("PATCH /api/links/{id}", s.guard(permAdmin, s.handleLinkUpdate))
	m.HandleFunc("DELETE /api/links/{id}", s.guard(permAdmin, s.handleLinkDelete))
	m.HandleFunc("POST /api/links/{id}/click", s.guard(permRead, s.handleClick))
	m.HandleFunc("POST /api/links/{id}/refresh-icon", s.guard(permAdmin, s.handleRefreshIcon))

	m.HandleFunc("GET /api/health", s.guard(permRead, s.handleHealth))
	m.HandleFunc("POST /api/uploads/wallpaper", s.guard(permAdmin, s.uploadHandler(s.wallpaperDir, 15<<20, "wallpapers", "jpg", "png", "webp", "avif")))
	m.HandleFunc("POST /api/uploads/icon", s.guard(permAdmin, s.uploadHandler(s.iconsDir, 1<<20, "icons", "png", "svg", "ico", "webp", "jpg")))
	m.HandleFunc("GET /api/wallpapers", s.guard(permAdmin, s.handleWallpaperList))
	m.HandleFunc("DELETE /api/wallpapers/{file}", s.guard(permAdmin, s.handleWallpaperDelete))
	m.HandleFunc("GET /api/export", s.guard(permAdmin, s.handleExport))
	m.HandleFunc("POST /api/import", s.guard(permAdmin, s.handleImport))

	m.HandleFunc("GET /api/token", s.guard(permAdmin, s.handleTokenGet))
	m.HandleFunc("POST /api/token/regenerate", s.guard(permAdmin, s.handleTokenRegenerate))
	m.HandleFunc("GET /api/skill", s.guard(permAdmin, s.handleSkill))

	m.HandleFunc("GET /api/events", s.guard(permRead, s.handleEvents))
	m.HandleFunc("GET /uploads/{kind}/{name}", s.guard(permRead, s.handleUpload))
	m.HandleFunc("/mcp", s.handleMCP)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	m.Handle("/", s.static)
}

// Bootstrap & auth -------------------------------------------------------------------

type bootstrapResp struct {
	Auth struct {
		SetupRequired bool   `json:"setup_required"`
		Role          string `json:"role"`
	} `json:"auth"`
	Settings   any                      `json:"settings"`
	Workspaces []store.Workspace        `json:"workspaces"`
	Categories []store.Category         `json:"categories"`
	Links      []store.Link             `json:"links"`
	Health     map[string]health.Result `json:"health"`
	Version    int64                    `json:"version"`
	// ServerVersion is the binary's build version (-ldflags "-X main.version=..."); only sent to logged-in roles.
	ServerVersion string `json:"server_version,omitempty"`
}

func (s *Server) healthSnapshot() map[string]health.Result {
	if s.cfg.Health == nil {
		return map[string]health.Result{}
	}
	return s.cfg.Health.Snapshot()
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request, role Role) {
	snap := s.st.Snapshot()
	var b bootstrapResp
	b.Auth.SetupRequired = s.st.SetupRequired()
	b.Auth.Role = role.String()
	if role == RoleNone {
		b.Settings = map[string]string{"title": snap.Settings.Title}
		b.Workspaces, b.Categories, b.Links = []store.Workspace{}, []store.Category{}, []store.Link{}
		b.Health = map[string]health.Result{}
	} else {
		b.Settings = snap.Settings
		b.Workspaces, b.Categories, b.Links = snap.Workspaces, snap.Categories, snap.Links
		b.Health = s.healthSnapshot()
		b.Version = snap.Version
		b.ServerVersion = s.cfg.Version
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, &b)
}

type passwordReq struct {
	Password    string `json:"password"`
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request, _ Role) {
	var req passwordReq
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if !s.st.SetupRequired() {
		writeErr(w, http.StatusForbidden, "setup already completed")
		return
	}
	if msg := auth.ValidatePassword(req.Password); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(w, err)
		return
	}
	ok, err := s.st.SetInitialPassword(hash)
	if err != nil {
		fail(w, err)
		return
	}
	if !ok {
		writeErr(w, http.StatusForbidden, "setup already completed")
		return
	}
	_, secret := s.st.AuthState()
	s.setSession(w, r, secret)
	writeOK(w)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request, _ Role) {
	ip := clientIP(r)
	if ok, wait := s.limiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	var req passwordReq
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	hash, secret := s.st.AuthState()
	if hash == "" {
		writeErr(w, http.StatusConflict, "setup required")
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		s.limiter.Fail(ip)
		writeErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	s.limiter.Reset(ip)
	s.setSession(w, r, secret)
	writeOK(w)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ Role) {
	http.SetCookie(w, &http.Cookie{
		Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isSecure(r),
	})
	writeOK(w)
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request, _ Role) {
	ip := clientIP(r)
	if ok, wait := s.limiter.Allow(ip); !ok {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	var req passwordReq
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	hash, _ := s.st.AuthState()
	if !auth.CheckPassword(hash, req.OldPassword) {
		s.limiter.Fail(ip)
		writeErr(w, http.StatusUnauthorized, "old password is incorrect")
		return
	}
	if msg := auth.ValidatePassword(req.NewPassword); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	nh, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		fail(w, err)
		return
	}
	secret, err := s.st.ChangePassword(nh)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := r.Cookie(auth.CookieName); err == nil && r.Header.Get("Authorization") == "" {
		s.setSession(w, r, secret) // re-issue for the current browser session
	}
	writeOK(w)
}

// Settings / workspaces / categories ---------------------------------------------------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request, _ Role) {
	var p store.SettingsPatch
	if !readJSON(w, r, &p, maxJSONBody) {
		return
	}
	ns, err := s.st.UpdateSettings(p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, ns)
}

func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request, _ Role) {
	var in store.WorkspaceInput
	if !readJSON(w, r, &in, maxJSONBody) {
		return
	}
	ws, err := s.st.CreateWorkspace(in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, ws)
}

func (s *Server) handleWorkspaceUpdate(w http.ResponseWriter, r *http.Request, _ Role) {
	var p store.WorkspacePatch
	if !readJSON(w, r, &p, maxJSONBody) {
		return
	}
	ws, err := s.st.UpdateWorkspace(r.PathValue("id"), p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, ws)
}

func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	if err := s.st.DeleteWorkspace(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleWorkspaceReorder(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if err := s.st.ReorderWorkspaces(req.IDs); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleCategoryCreate(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	c, err := s.st.CreateCategory(req.WorkspaceID, req.Name)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, c)
}

func (s *Server) handleCategoryUpdate(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		Name *string `json:"name"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	c, err := s.st.UpdateCategory(r.PathValue("id"), req.Name)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, c)
}

func (s *Server) handleCategoryDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	dl := r.URL.Query().Get("delete_links")
	del := dl == "1" || strings.EqualFold(dl, "true")
	if err := s.st.DeleteCategory(r.PathValue("id"), del); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleCategoryReorder(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		WorkspaceID string   `json:"workspace_id"`
		IDs         []string `json:"ids"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if err := s.st.ReorderCategories(req.WorkspaceID, req.IDs); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

// Links ----------------------------------------------------------------------------

func (s *Server) handleLinkCreate(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		store.LinkInput
		FetchIcon *bool `json:"fetch_icon"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	l, err := s.st.CreateLink(req.LinkInput)
	if err != nil {
		fail(w, err)
		return
	}
	if req.FetchIcon == nil || *req.FetchIcon {
		s.fetchIcons([]store.Link{l})
	}
	writeJSON(w, r, http.StatusOK, l)
}

func (s *Server) handleLinkUpdate(w http.ResponseWriter, r *http.Request, _ Role) {
	var p store.LinkPatch
	if !readJSON(w, r, &p, maxJSONBody) {
		return
	}
	l, err := s.st.UpdateLink(r.PathValue("id"), p)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, l)
}

func (s *Server) handleLinkDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	n, _, err := s.st.DeleteLinks([]string{r.PathValue("id")})
	if err != nil {
		fail(w, err)
		return
	}
	if n == 0 {
		writeErr(w, http.StatusNotFound, "link not found: "+r.PathValue("id"))
		return
	}
	writeOK(w)
}

func (s *Server) handleLinkReorder(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		WorkspaceID string   `json:"workspace_id"`
		CategoryID  string   `json:"category_id"`
		IDs         []string `json:"ids"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if err := s.st.ReorderLinks(req.WorkspaceID, req.CategoryID, req.IDs); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleClick(w http.ResponseWriter, r *http.Request, role Role) {
	if role == RoleAdmin {
		if err := s.st.Click(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshIcon(w http.ResponseWriter, r *http.Request, _ Role) {
	id := r.PathValue("id")
	l, ok := s.st.GetLink(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "link not found: "+id)
		return
	}
	if s.cfg.Favicon == nil {
		writeErr(w, http.StatusServiceUnavailable, "icon fetching is disabled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	name, err := s.cfg.Favicon.Fetch(ctx, l.URL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "could not fetch icon: "+err.Error())
		return
	}
	s.st.SetIcon(id, "favicon:"+name, false)
	l, ok = s.st.GetLink(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "link not found: "+id)
		return
	}
	writeJSON(w, r, http.StatusOK, l)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request, _ Role) {
	writeJSON(w, r, http.StatusOK, s.healthSnapshot())
}

// Export / import -------------------------------------------------------------------

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request, _ Role) {
	b, err := json.MarshalIndent(s.st.Export(), "", "  ")
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="genkan-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSONBytes(w, r, http.StatusOK, b)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request, _ Role) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(2 * time.Minute))
	_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	var req struct {
		Mode string       `json:"mode"`
		Data store.Export `json:"data"`
	}
	req.Data.Settings = store.DefaultSettings()
	if !readJSON(w, r, &req, maxImportBody) {
		return
	}
	nw, nl, err := s.st.Import(req.Mode, req.Data)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"ok": true, "workspaces": nw, "links": nl})
}

// API token ------------------------------------------------------------

func writeSecretJSON(w http.ResponseWriter, r *http.Request, v any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, v)
}

func (s *Server) handleTokenGet(w http.ResponseWriter, r *http.Request, _ Role) {
	writeSecretJSON(w, r, map[string]string{"token": s.st.APIToken()})
}

func (s *Server) handleTokenRegenerate(w http.ResponseWriter, r *http.Request, _ Role) {
	tok, err := s.st.RegenerateToken()
	if err != nil {
		fail(w, err)
		return
	}
	writeSecretJSON(w, r, map[string]string{"token": tok})
}

// instanceURL derives the public base URL from the request (reverse-proxy aware).
func instanceURL(r *http.Request) string {
	first := func(v string) string {
		v, _, _ = strings.Cut(v, ",")
		return strings.TrimSpace(v)
	}
	scheme := "http"
	if isSecure(r) {
		scheme = "https"
	}
	if p := strings.ToLower(first(r.Header.Get("X-Forwarded-Proto"))); p == "http" || p == "https" {
		scheme = p
	}
	host := first(r.Header.Get("X-Forwarded-Host"))
	if !validHost(host) {
		host = r.Host
	}
	if !validHost(host) {
		host = "localhost"
	}
	return scheme + "://" + host
}

func validHost(h string) bool {
	if h == "" || len(h) > 255 {
		return false
	}
	for _, c := range h {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '-', c == ':', c == '[', c == ']', c == '_':
		default:
			return false
		}
	}
	return true
}

// handleSkill serves the Skill package: the generic SKILL.md and reference.md
// exactly as embedded, plus a generated genkan/.env (mode 0600) holding this
// instance's URL and the current token.
func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request, _ Role) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	add := func(name string, mode os.FileMode, content []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now}
		h.SetMode(mode)
		f, err := zw.CreateHeader(h)
		if err == nil {
			_, err = f.Write(content)
		}
		return err
	}
	var err error
	for _, sf := range skills.Files() {
		if err = add(sf.Name, 0o644, sf.Content); err != nil {
			break
		}
	}
	if err == nil {
		env := "GENKAN_URL=" + instanceURL(r) + "\nGENKAN_TOKEN=" + s.st.APIToken() + "\n"
		err = add("genkan/.env", 0o600, []byte(env))
	}
	if err == nil {
		err = zw.Close()
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", `attachment; filename="genkan-skill.zip"`)
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", itoa(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

// MCP ---------------------------------------------------------------------------------

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeErr(w, http.StatusMethodNotAllowed, "use POST")
		return
	}
	if _, cred := s.authenticate(r); cred != credToken {
		w.Header().Set("WWW-Authenticate", `Bearer realm="genkan"`)
		writeErr(w, http.StatusUnauthorized, "a valid API token is required (Authorization: Bearer gk_...)")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMCPBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	resp, status := s.mcp.Handle(body)
	if resp == nil {
		w.WriteHeader(status)
		return
	}
	writeJSONBytes(w, r, status, resp)
}

// Uploads --------------------------------------------------------------------------------

func (s *Server) uploadHandler(dir func() string, limit int64, kind string, allowed ...string) authedHandler {
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	return func(w http.ResponseWriter, r *http.Request, _ Role) {
		rc := http.NewResponseController(w)
		_ = rc.SetReadDeadline(time.Now().Add(2 * time.Minute))
		_ = rc.SetWriteDeadline(time.Now().Add(2 * time.Minute))
		r.Body = http.MaxBytesReader(w, r.Body, limit+(256<<10))
		mr, err := r.MultipartReader()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "expected multipart/form-data with a \"file\" field")
			return
		}
		var data []byte
		found := false
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				uploadReadErr(w, err)
				return
			}
			if part.FormName() != "file" {
				continue
			}
			data, err = readPart(part, limit)
			if err != nil {
				uploadReadErr(w, err)
				return
			}
			found = true
			break
		}
		if !found || len(data) == 0 {
			writeErr(w, http.StatusBadRequest, "missing \"file\" field")
			return
		}
		ext, _, ok := media.Sniff(data)
		if !ok || !allow[ext] {
			writeErr(w, http.StatusBadRequest, "unsupported image type (allowed: "+strings.Join(allowed, ", ")+")")
			return
		}
		name, err := media.Save(dir(), data, ext)
		if err != nil {
			log.Printf("upload: %v", err)
			writeErr(w, http.StatusInternalServerError, "could not save file")
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]string{"value": "upload:" + name, "url": "/uploads/" + kind + "/" + name})
	}
}

var errTooLarge = errors.New("too large")

func readPart(p *multipart.Part, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(p, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

func uploadReadErr(w http.ResponseWriter, err error) {
	var mbe *http.MaxBytesError
	if errors.Is(err, errTooLarge) || errors.As(err, &mbe) {
		writeErr(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}
	writeErr(w, http.StatusBadRequest, "invalid multipart body")
}

type wallpaperItem struct {
	Value     string `json:"value"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleWallpaperList(w http.ResponseWriter, r *http.Request, _ Role) {
	files, err := media.List(s.wallpaperDir())
	if err != nil {
		log.Printf("wallpapers: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not read wallpapers")
		return
	}
	out := make([]wallpaperItem, len(files))
	for i, f := range files {
		out[i] = wallpaperItem{
			Value: "upload:" + f.Name, URL: "/uploads/wallpapers/" + f.Name,
			Size: f.Size, CreatedAt: f.ModTime.UTC().Format(time.RFC3339),
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleWallpaperDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	name := r.PathValue("file")
	if !media.SafeName(name) {
		writeErr(w, http.StatusBadRequest, "invalid wallpaper file name")
		return
	}
	path := filepath.Join(s.wallpaperDir(), name)
	if st, err := os.Lstat(path); err != nil || !st.Mode().IsRegular() {
		writeErr(w, http.StatusNotFound, "wallpaper not found: "+name)
		return
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErr(w, http.StatusNotFound, "wallpaper not found: "+name)
			return
		}
		log.Printf("wallpapers: remove %s: %v", name, err)
		writeErr(w, http.StatusInternalServerError, "could not delete file")
		return
	}
	if _, err := s.st.ResetWallpaperUsers(name); err != nil {
		fail(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, _ Role) {
	kind, name := r.PathValue("kind"), r.PathValue("name")
	if (kind != "wallpapers" && kind != "icons") || !media.SafeName(name) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(s.cfg.DataDir, "uploads", kind, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	h := w.Header()
	h.Set("Content-Type", media.ContentType(ext))
	if s.st.Settings().GuestAccess {
		h.Set("Cache-Control", "public, max-age=86400")
	} else {
		h.Set("Cache-Control", "private, max-age=86400")
	}
	// Uploaded SVGs must never run script when opened directly.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	http.ServeContent(w, r, name, st.ModTime(), f)
}
