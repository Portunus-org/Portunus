package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/store"
)

// auditShown is how many of the latest entries the audit log page gets;
// the table sorts and searches them in the browser.
const auditShown = 5000

func (s *Server) routesAudit(mux *http.ServeMux) {
	mux.Handle("GET /api/audit", s.requireSession(requireRole(store.RoleAdmin, s.handleListAudit)))
}

type auditDTO struct {
	ID      int64           `json:"id"`
	At      time.Time       `json:"at"`
	Actor   string          `json:"actor"`
	Action  string          `json:"action"`
	Target  string          `json:"target"`
	IP      string          `json:"ip"`
	Details json.RawMessage `json:"details"`
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListAudit(r.Context(), auditShown)
	if err != nil {
		s.log.Error("list audit log", "err", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	out := make([]auditDTO, 0, len(rows))
	for _, a := range rows {
		details := json.RawMessage(a.Details)
		if !json.Valid(details) {
			details = json.RawMessage("{}")
		}
		out = append(out, auditDTO{ID: a.ID, At: a.At, Actor: a.Actor, Action: a.Action, Target: a.Target, IP: a.IP, Details: details})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "limit": auditShown})
}
