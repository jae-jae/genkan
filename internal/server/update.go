package server

import (
	"errors"
	"net/http"

	"github.com/jae-jae/genkan/internal/update"
)

// Self-update API (docs/UPDATES.md §6). Admin only; the cookie CSRF rule of
// guard applies to the POSTs.

func (s *Server) updateRoutes() {
	m := s.mux
	m.HandleFunc("GET /api/update", s.guard(permAdmin, s.handleUpdateStatus))
	m.HandleFunc("POST /api/update/check", s.guard(permAdmin, s.handleUpdateCheck))
	m.HandleFunc("POST /api/update/apply", s.guard(permAdmin, s.handleUpdateApply))
	m.HandleFunc("POST /api/update/dismiss", s.guard(permAdmin, s.handleUpdateDismiss))
}

func (s *Server) writeUpdateStatus(w http.ResponseWriter, r *http.Request, code int) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, code, s.updater.Status())
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request, _ Role) {
	s.writeUpdateStatus(w, r, http.StatusOK)
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request, _ Role) {
	if err := s.updater.Check(r.Context(), true); err != nil {
		if errors.Is(err, update.ErrCheckDisabled) {
			st := s.updater.Status()
			msg := "update checks are disabled"
			switch {
			case st.Dev:
				msg = "development builds do not check for updates"
			case st.EnvDisabled:
				msg = "update checks are turned off by GENKAN_UPDATE_CHECK=off"
			}
			writeErr(w, http.StatusBadRequest, msg)
			return
		}
		fail(w, err)
		return
	}
	s.writeUpdateStatus(w, r, http.StatusOK)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		Version string `json:"version"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if _, ok := update.ParseVersion(req.Version); !ok {
		writeErr(w, http.StatusBadRequest, "version must look like v1.2.3")
		return
	}
	err := s.updater.Apply(req.Version)
	switch {
	case err == nil:
		s.writeUpdateStatus(w, r, http.StatusAccepted)
	case errors.Is(err, update.ErrBusy):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, update.ErrUnknownVersion):
		writeErr(w, http.StatusBadRequest, err.Error())
	default: // unsupported (with the reason), or a bad public key
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) handleUpdateDismiss(w http.ResponseWriter, r *http.Request, _ Role) {
	var req struct {
		Version    string `json:"version"`
		RolledBack bool   `json:"rolled_back"`
	}
	if !readJSON(w, r, &req, maxJSONBody) {
		return
	}
	if req.Version == "" && !req.RolledBack {
		writeErr(w, http.StatusBadRequest, `give {"version":"vX.Y.Z"} or {"rolled_back":true}`)
		return
	}
	if req.Version != "" {
		if _, err := s.st.SetUpdateDismissed(req.Version); err != nil {
			fail(w, err)
			return
		}
	}
	if req.RolledBack {
		s.updater.ClearRolledBack()
	}
	s.writeUpdateStatus(w, r, http.StatusOK)
}
