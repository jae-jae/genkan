// Package server wires Genkan's HTTP surface: REST API, SSE, MCP, uploads and static assets.
package server

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jae-jae/genkan/internal/auth"
	"github.com/jae-jae/genkan/internal/favicon"
	"github.com/jae-jae/genkan/internal/health"
	"github.com/jae-jae/genkan/internal/mcp"
	"github.com/jae-jae/genkan/internal/plugin"
	"github.com/jae-jae/genkan/internal/store"
	"github.com/jae-jae/genkan/plugins"
)

// Config holds the server's dependencies.
type Config struct {
	Store   *store.Store
	Health  *health.Checker // optional
	Favicon *favicon.Fetcher
	DataDir string
	Web     fs.FS
	Version string
	// GitHubToken (GENKAN_GITHUB_TOKEN) is optional; it raises the GitHub API
	// rate limit of the third-party plugin market.
	GitHubToken string
	// GitHubAPI / GitHubRaw override the GitHub base URLs (tests).
	GitHubAPI, GitHubRaw string
}

type Role int

const (
	RoleNone Role = iota
	RoleGuest
	RoleAdmin
)

func (r Role) String() string {
	switch r {
	case RoleAdmin:
		return "admin"
	case RoleGuest:
		return "guest"
	}
	return "none"
}

type credential int

const (
	credNone credential = iota
	credCookie
	credToken
	credGuest
)

type perm int

const (
	permPublic perm = iota
	permRead
	permAdmin
)

const (
	maxJSONBody   = 1 << 20
	maxImportBody = 32 << 20
	maxMCPBody    = 4 << 20
	csrfHeader    = "X-Requested-With"
	csrfValue     = "genkan"
)

// Server is the HTTP application.
type Server struct {
	cfg     Config
	st      *store.Store
	broker  *Broker
	limiter *auth.Limiter
	mcp     *mcp.Server
	plugins *plugin.Manager
	static  *static
	mux     *http.ServeMux

	bgCtx    context.Context
	bgCancel context.CancelFunc
	wg       sync.WaitGroup
	iconQ    chan store.Link
}

// New builds the Server and starts its background icon workers.
func New(cfg Config) *Server {
	s := &Server{
		cfg:     cfg,
		st:      cfg.Store,
		broker:  NewBroker(64),
		limiter: auth.NewLimiter(5, time.Minute),
		static:  newStatic(cfg.Web),
		mux:     http.NewServeMux(),
		iconQ:   make(chan store.Link, 1024),
	}
	s.bgCtx, s.bgCancel = context.WithCancel(context.Background())
	s.plugins = plugin.New(plugin.Config{
		Store: cfg.Store, DataDir: cfg.DataDir, Version: cfg.Version,
		Catalog: plugins.Catalog(), OnLinksCreated: s.fetchIcons, GitHubToken: cfg.GitHubToken, GitHubAPI: cfg.GitHubAPI, GitHubRaw: cfg.GitHubRaw,
	})
	s.mcp = mcp.New(cfg.Store, cfg.Version)
	s.mcp.Plugins = s.plugins
	s.mcp.OnLinksCreated = s.fetchIcons
	cfg.Store.OnChange(func(v int64) {
		s.broker.PublishChange(v)
		if cfg.Health != nil {
			cfg.Health.Kick()
		}
	})
	if cfg.Favicon != nil {
		for i := 0; i < 4; i++ {
			s.wg.Add(1)
			go s.iconWorker()
		}
	}
	s.routes()
	return s
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler { return s.recoverer(s.mux) }

// PublishHealth pushes a health event to SSE clients.
func (s *Server) PublishHealth(m map[string]health.Result) { s.broker.PublishHealth(m) }

// Broker exposes the SSE broker (for shutdown hooks).
func (s *Server) Broker() *Broker { return s.broker }

// Close stops SSE streams and background workers and waits for them.
func (s *Server) Close() {
	s.broker.Close()
	s.plugins.Close()
	s.bgCancel()
	s.wg.Wait()
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Printf("panic: %v (%s %s)", v, r.Method, r.URL.Path)
				writeErr(w, http.StatusInternalServerError, "internal error")
			}
		}()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// Auth ---------------------------------------------------------------------------

func (s *Server) authenticate(r *http.Request) (Role, credential) {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			if s.st.CheckToken(strings.TrimSpace(h[7:])) {
				return RoleAdmin, credToken
			}
		}
		return RoleNone, credNone
	}
	if c, err := r.Cookie(auth.CookieName); err == nil {
		_, secret := s.st.AuthState()
		if auth.VerifySession(secret, c.Value, time.Now()) {
			return RoleAdmin, credCookie
		}
	}
	if s.st.Settings().GuestAccess {
		return RoleGuest, credGuest
	}
	return RoleNone, credNone
}

func isClick(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodPost && strings.HasPrefix(p, "/api/links/") && strings.HasSuffix(p, "/click")
}

type authedHandler func(w http.ResponseWriter, r *http.Request, role Role)

// guard wraps a handler with authentication, CSRF and permission checks.
func (s *Server) guard(p perm, fn authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		role, cred := s.authenticate(r)
		if cred == credCookie && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
			!isClick(r) && r.Header.Get(csrfHeader) != csrfValue {
			writeErr(w, http.StatusForbidden, "missing X-Requested-With: genkan header")
			return
		}
		switch p {
		case permAdmin:
			if role != RoleAdmin {
				if role == RoleGuest {
					writeErr(w, http.StatusForbidden, "admin access required")
				} else {
					writeErr(w, http.StatusUnauthorized, "authentication required")
				}
				return
			}
		case permRead:
			if role == RoleNone {
				writeErr(w, http.StatusUnauthorized, "authentication required")
				return
			}
		}
		fn(w, r, role)
	}
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, secret string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    auth.SignSession(secret, time.Now().Add(auth.SessionTTL)),
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure(r),
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// JSON helpers -----------------------------------------------------------------

var gzPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

func writeJSONBytes(w http.ResponseWriter, r *http.Request, status int, b []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Add("Vary", "Accept-Encoding")
	if r != nil && len(b) > 1024 && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		w.WriteHeader(status)
		gz := gzPool.Get().(*gzip.Writer)
		gz.Reset(w)
		_, _ = gz.Write(b)
		_ = gz.Close()
		gzPool.Put(gz)
		return
	}
	h.Set("Content-Length", itoa(len(b)))
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeErr(w, 500, "internal error")
		return
	}
	writeJSONBytes(w, r, status, b)
}

func writeOK(w http.ResponseWriter) { writeJSON(w, nil, http.StatusOK, map[string]bool{"ok": true}) }

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, nil, status, map[string]string{"error": msg})
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// readJSON decodes the request body into v. Reports false (after writing the error) on failure.
func readJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else if errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "request body is empty")
		} else {
			writeErr(w, http.StatusBadRequest, "invalid JSON body")
		}
		return false
	}
	return true
}

// fail maps store errors to HTTP statuses.
func fail(w http.ResponseWriter, err error) {
	var se *store.Error
	if errors.As(err, &se) {
		switch se.Kind {
		case store.KindInvalid:
			writeErr(w, http.StatusBadRequest, se.Msg)
		case store.KindNotFound:
			writeErr(w, http.StatusNotFound, se.Msg)
		case store.KindConflict:
			writeErr(w, http.StatusConflict, se.Msg)
		default:
			writeErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	log.Printf("error: %v", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

// Background icon fetching ------------------------------------------------------------

func (s *Server) fetchIcons(links []store.Link) {
	for _, l := range links {
		if l.Icon != "" {
			continue
		}
		select {
		case s.iconQ <- l:
		default: // queue full: skip, user can refresh manually
		}
	}
}

func (s *Server) iconWorker() {
	defer s.wg.Done()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case l := <-s.iconQ:
			ctx, cancel := context.WithTimeout(s.bgCtx, 15*time.Second)
			name, err := s.cfg.Favicon.Fetch(ctx, l.URL)
			cancel()
			if err != nil {
				continue
			}
			s.st.SetIcon(l.ID, "favicon:"+name, true) // notifies via OnChange
		}
	}
}

func (s *Server) iconsDir() string     { return filepath.Join(s.cfg.DataDir, "uploads", "icons") }
func (s *Server) wallpaperDir() string { return filepath.Join(s.cfg.DataDir, "uploads", "wallpapers") }
