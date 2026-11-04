package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jae-jae/genkan/internal/plugin"
)

// Plugins exposes the plugin manager (tests and shutdown hooks).
func (s *Server) Plugins() *plugin.Manager { return s.plugins }

func (s *Server) pluginRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/plugins", s.guard(permAdmin, s.handlePluginList))
	m.HandleFunc("POST /api/plugins", s.guard(permAdmin, s.handlePluginInstall))
	m.HandleFunc("GET /api/plugins/catalog", s.guard(permAdmin, s.handlePluginCatalog))
	m.HandleFunc("POST /api/plugins/preview", s.guard(permAdmin, s.handlePluginPreview))
	m.HandleFunc("GET /api/plugins/market", s.guard(permAdmin, s.handlePluginMarket))
	m.HandleFunc("GET /api/plugins/market/repo", s.guard(permAdmin, s.handlePluginMarketRepo))
	m.HandleFunc("POST /api/plugins/market/cache/clear", s.guard(permAdmin, s.handlePluginMarketClear))
	m.HandleFunc("GET /api/plugins/search", s.guard(permRead, s.handlePluginSearch))
	m.HandleFunc("GET /api/plugins/wallpaper/{plugin}/{source}", s.guard(permRead, s.handlePluginWallpaper))
	m.HandleFunc("GET /api/plugins/{id}/source", s.guard(permAdmin, s.handlePluginSource))
	m.HandleFunc("PUT /api/plugins/{id}/source", s.guard(permAdmin, s.handlePluginSourcePut))
	m.HandleFunc("GET /api/plugins/{id}/logs", s.guard(permAdmin, s.handlePluginLogs))
	m.HandleFunc("GET /api/plugins/{id}/storage", s.guard(permAdmin, s.handlePluginStorage))
	m.HandleFunc("DELETE /api/plugins/{id}/storage", s.guard(permAdmin, s.handlePluginStorageClear))
	m.HandleFunc("POST /api/plugins/{id}/test", s.guard(permAdmin, s.handlePluginTest))
	m.HandleFunc("PATCH /api/plugins/{id}", s.guard(permAdmin, s.handlePluginPatch))
	m.HandleFunc("DELETE /api/plugins/{id}", s.guard(permAdmin, s.handlePluginDelete))

	m.HandleFunc("POST /api/widgets", s.guard(permAdmin, s.handleWidgetCreate))
	m.HandleFunc("POST /api/widgets/reorder", s.guard(permAdmin, s.handleWidgetReorder))
	m.HandleFunc("PATCH /api/widgets/{id}", s.guard(permAdmin, s.handleWidgetUpdate))
	m.HandleFunc("DELETE /api/widgets/{id}", s.guard(permAdmin, s.handleWidgetDelete))
	m.HandleFunc("GET /api/widgets/{id}/data", s.guard(permRead, s.handleWidgetData))
	m.HandleFunc("POST /api/widgets/{id}/action", s.guard(permAdmin, s.handleWidgetAction))
}

// pluginLang picks the caller's language: ?lang=, else Accept-Language (base
// language such as "zh", "en", "ja").
func pluginLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l != "" {
		return plugin.NormLang(l)
	}
	return plugin.NormLang(r.Header.Get("Accept-Language"))
}

// failPlugin maps plugin errors (hook failures) and store errors to HTTP statuses.
func failPlugin(w http.ResponseWriter, err error) {
	var he *plugin.HookError
	switch {
	case errors.As(err, &he):
		status := http.StatusBadGateway
		if he.Timeout {
			status = http.StatusGatewayTimeout
		}
		writeErr(w, status, he.Msg)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeErr(w, http.StatusServiceUnavailable, "request canceled")
	default:
		fail(w, err)
	}
}

func (s *Server) handlePluginList(w http.ResponseWriter, r *http.Request, _ Role) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, s.plugins.Infos(true))
}

func (s *Server) handlePluginCatalog(w http.ResponseWriter, r *http.Request, _ Role) {
	writeJSON(w, r, http.StatusOK, s.plugins.Catalog())
}

func (s *Server) handlePluginInstall(w http.ResponseWriter, r *http.Request, _ Role) {
	var req plugin.InstallRequest
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	inf, err := s.plugins.Install(r.Context(), req)
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, inf)
}

func (s *Server) handlePluginSource(w http.ResponseWriter, r *http.Request, _ Role) {
	src, lang, err := s.plugins.Source(r.PathValue("id"))
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, map[string]string{"source": src, "lang": lang})
}

func (s *Server) handlePluginSourcePut(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		Source string `json:"source"`
		Lang   string `json:"lang"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	inf, err := s.plugins.UpdateSource(r.PathValue("id"), req.Source, req.Lang)
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, inf)
}

func (s *Server) handlePluginPreview(w http.ResponseWriter, r *http.Request, _ Role) {
	var req plugin.InstallRequest
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	res, err := s.plugins.Preview(r.Context(), req)
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, res)
}

func (s *Server) handlePluginMarket(w http.ResponseWriter, r *http.Request, _ Role) {
	q := r.URL.Query()
	page := 1
	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "page must be a number")
			return
		}
		page = n
	}
	res, err := s.plugins.Market(q.Get("sort"), q.Get("q"), page)
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, res)
}

func (s *Server) handlePluginMarketRepo(w http.ResponseWriter, r *http.Request, _ Role) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(80 * time.Second)) // a scan may take a minute
	res, err := s.plugins.MarketRepo(r.URL.Query().Get("repo"))
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, res)
}

func (s *Server) handlePluginMarketClear(w http.ResponseWriter, r *http.Request, _ Role) {
	s.plugins.ClearMarketCache()
	writeJSON(w, r, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handlePluginPatch(w http.ResponseWriter, r *http.Request, _ Role) {
	var req plugin.PatchRequest
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	req.Lang = pluginLang(r)
	inf, err := s.plugins.Patch(r.PathValue("id"), req)
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, inf)
}

func (s *Server) handlePluginDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	if err := s.plugins.Uninstall(r.PathValue("id")); err != nil {
		failPlugin(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handlePluginLogs(w http.ResponseWriter, r *http.Request, _ Role) {
	logs, err := s.plugins.Logs(r.PathValue("id"))
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handlePluginStorage(w http.ResponseWriter, r *http.Request, _ Role) {
	info, err := s.plugins.Storage(r.PathValue("id"))
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, info)
}

// handlePluginStorageClear clears all storage, or one key (?key=, plus &instance= for instance-scoped keys).
func (s *Server) handlePluginStorageClear(w http.ResponseWriter, r *http.Request, _ Role) {
	q := r.URL.Query()
	if err := s.plugins.ClearStorage(r.PathValue("id"), q.Get("key"), q.Get("instance")); err != nil {
		failPlugin(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handlePluginTest(w http.ResponseWriter, r *http.Request, _ Role) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(100 * time.Second)) // schedules may run for 60 s
	var req plugin.TestRequest
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if req.Lang == "" {
		req.Lang = r.URL.Query().Get("lang")
	}
	res, err := s.plugins.Test(r.Context(), r.PathValue("id"), req)
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, res)
}

func (s *Server) handlePluginSearch(w http.ResponseWriter, r *http.Request, _ Role) {
	groups := s.plugins.Search(r.Context(), r.URL.Query().Get("q"), pluginLang(r))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handlePluginWallpaper(w http.ResponseWriter, r *http.Request, _ Role) {
	info, err := s.plugins.Wallpaper(r.Context(), r.PathValue("plugin"), r.PathValue("source"), pluginLang(r))
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, info)
}

// Widgets ---------------------------------------------------------------------------------

func (s *Server) handleWidgetCreate(w http.ResponseWriter, r *http.Request, _ Role) {
	var req plugin.WidgetAdd
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	req.Lang = pluginLang(r)
	out, err := s.plugins.AddWidget(req)
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleWidgetUpdate(w http.ResponseWriter, r *http.Request, _ Role) {
	var p plugin.WidgetPatch
	if !readJSON(w, r, &p, maxJSONBody) {
		return
	}
	p.Lang = pluginLang(r)
	out, err := s.plugins.UpdateWidget(r.PathValue("id"), p)
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, out)
}

func (s *Server) handleWidgetDelete(w http.ResponseWriter, r *http.Request, _ Role) {
	if err := s.plugins.DeleteWidget(r.PathValue("id")); err != nil {
		failPlugin(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleWidgetReorder(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if err := s.plugins.ReorderWidgets(req.IDs); err != nil {
		failPlugin(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleWidgetData(w http.ResponseWriter, r *http.Request, role Role) {
	force := role == RoleAdmin && (r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true"))
	d, err := s.plugins.Data(r.Context(), r.PathValue("id"), pluginLang(r), force)
	if err != nil {
		failPlugin(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, d)
}

func (s *Server) handleWidgetAction(w http.ResponseWriter, r *http.Request, _ Role) {
	var req plugin.ActionRequest
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	res, err := s.plugins.Action(r.Context(), r.PathValue("id"), req, pluginLang(r))
	if err != nil {
		failPlugin(w, err)
		return
	}
	writeJSON(w, r, http.StatusOK, res)
}
