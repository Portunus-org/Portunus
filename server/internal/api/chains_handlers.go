package api

import (
	"net/http"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/core"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

func (s *Server) routesChains(mux *http.ServeMux) {
	view := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleViewer, h)) }
	op := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleOperator, h)) }

	mux.Handle("GET /api/chains", view(s.handleListChains))
	mux.Handle("POST /api/chains", op(s.handleCreateChain))
	mux.Handle("PUT /api/chains/{id}", op(s.handleUpdateChain))
	mux.Handle("DELETE /api/chains/{id}", op(s.handleDeleteChain))
}

// ------------------------------------------------------------------ chains

// chainEdgeDTO is one directed edge of a chain; From 0 means "every panel
// server" (the main one).
type chainEdgeDTO struct {
	ID   int64 `json:"id"`
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type chainDTO struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Color     string         `json:"color"`
	Edges     []chainEdgeDTO `json:"edges"`
	Backups   []int64        `json:"backups"`
	Clients   int            `json:"clients"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type chainRequest struct {
	Name    string        `json:"name"`
	Color   string        `json:"color"`
	Edges   []edgeRequest `json:"edges"`
	Backups []int64       `json:"backups"`
}

type edgeRequest struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

func (req chainRequest) input() core.ChainInput {
	edges := make([]core.EdgeInput, 0, len(req.Edges))
	for _, e := range req.Edges {
		edges = append(edges, core.EdgeInput{From: e.From, To: e.To})
	}
	return core.ChainInput{Name: req.Name, Color: req.Color, Edges: edges, Backups: req.Backups}
}

func (s *Server) toChainDTO(ch *core.Chain, clients int) chainDTO {
	edges := make([]chainEdgeDTO, 0, len(ch.Edges))
	for _, e := range ch.Edges {
		edges = append(edges, chainEdgeDTO{ID: e.ID, From: e.FromNodeID, To: e.ToNodeID})
	}
	backups := ch.Backups
	if backups == nil {
		backups = []int64{}
	}
	return chainDTO{ID: ch.ID, Name: ch.Name, Color: ch.Color, Edges: edges, Backups: backups,
		Clients: clients, CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt}
}

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request) {
	chains, err := s.core.Chains(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	counts, err := s.core.ChainClients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]chainDTO, 0, len(chains))
	for i := range chains {
		out = append(out, s.toChainDTO(&chains[i], counts[chains[i].ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateChain(w http.ResponseWriter, r *http.Request) {
	var req chainRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ch, err := s.core.CreateChain(r.Context(), req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "chain.create", ch.Name, map[string]any{"edges": len(ch.Edges), "backups": ch.Backups})
	writeJSON(w, http.StatusCreated, s.toChainDTO(ch, 0))
}

func (s *Server) handleUpdateChain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req chainRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ch, err := s.core.UpdateChain(r.Context(), id, req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "chain.update", ch.Name, map[string]any{"edges": len(ch.Edges), "backups": ch.Backups})
	writeJSON(w, http.StatusOK, s.toChainDTO(ch, 0))
}

func (s *Server) handleDeleteChain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	ch, err := s.core.Chain(r.Context(), id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	if err := s.core.DeleteChain(r.Context(), id); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "chain.delete", ch.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}
