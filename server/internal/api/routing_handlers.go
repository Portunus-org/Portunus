package api

import (
	"context"
	"github.com/Portunus-org/Portunus/server/internal/agent"
	"net/http"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/core"
	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

func (s *Server) routesRouting(mux *http.ServeMux) {
	view := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleViewer, h)) }
	op := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleOperator, h)) }
	admin := func(h http.HandlerFunc) http.Handler { return s.requireSession(requireRole(store.RoleAdmin, h)) }

	// Nodes hold keys and are joined with a secret command: admins only.
	mux.Handle("GET /api/nodes", view(s.handleListNodes))
	mux.Handle("POST /api/nodes", admin(s.handleCreateNode))
	mux.Handle("PUT /api/nodes/{id}", admin(s.handleUpdateNode))
	mux.Handle("DELETE /api/nodes/{id}", admin(s.handleDeleteNode))
	mux.Handle("GET /api/nodes/{id}/join", admin(s.handleNodeJoin))
	mux.Handle("POST /api/nodes/check", op(s.handleCheckNodes))
	mux.Handle("PUT /api/nodes/{id}/location", admin(s.handleSetNodeLocation))
	// The main server, for the map of nodes.
	mux.Handle("GET /api/recovery", admin(s.handleRecovery))
	mux.Handle("GET /api/topology", view(s.handleTopology))
	mux.Handle("PUT /api/topology/location", admin(s.handleSetServerLocation))

	mux.Handle("GET /api/routing", view(s.handleGetRouting))
	mux.Handle("PUT /api/routing/default", op(s.handleSetDefaultRoute))
	mux.Handle("POST /api/routing/lists", op(s.handleCreateRouteList))
	mux.Handle("PUT /api/routing/lists/{id}", op(s.handleUpdateRouteList))
	mux.Handle("DELETE /api/routing/lists/{id}", op(s.handleDeleteRouteList))
	mux.Handle("POST /api/routing/lists/{id}/refresh", op(s.handleRefreshRouteList))
	mux.Handle("POST /api/routing/rules", op(s.handleCreateRouteRule))
	mux.Handle("PUT /api/routing/rules/order", op(s.handleOrderRouteRules))
	mux.Handle("PUT /api/routing/rules/{id}", op(s.handleUpdateRouteRule))
	mux.Handle("DELETE /api/routing/rules/{id}", op(s.handleDeleteRouteRule))
}

// ------------------------------------------------------------------ nodes

type nodeDTO struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	IsExit  bool   `json:"isExit"`
	IsEntry bool   `json:"isEntry"`
	Enabled bool   `json:"enabled"`
	// Agent nodes.
	AgentPort int `json:"agentPort,omitempty"`
	HopPort   int `json:"hopPort,omitempty"`
	// Joined: the server has answered the panel at least once.
	Joined bool `json:"joined"`
	// Standby: the node keeps a sealed copy of the panel.
	Standby bool `json:"standby"`
	// Imported links.
	Protocol  string          `json:"protocol,omitempty"`
	Port      int             `json:"port,omitempty"`
	Location  *core.Location  `json:"location,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	Status    core.NodeStatus `json:"status"`
	// Clients bound to this exit (only in the list of nodes).
	Clients int `json:"clients"`
}

func (s *Server) nodeDTO(n core.Node) nodeDTO {
	d := nodeDTO{
		ID: n.ID, Name: n.Name, Kind: n.Kind, Address: n.Address, IsExit: n.IsExit, IsEntry: n.IsEntry,
		Enabled: n.Enabled, AgentPort: n.Settings.AgentPort, HopPort: n.Settings.HopPort,
		Joined: n.Settings.JoinedAt != nil || n.Kind == core.NodeLink, Protocol: n.Settings.Protocol, Port: n.Settings.Port,
		Location: n.Settings.Location, CreatedAt: n.CreatedAt, Status: s.core.NodeStatus(n.ID),
		Standby: n.Settings.Standby,
	}
	if d.Status.State == "" {
		d.Status.State = core.NodePending
	}
	return d
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.core.Nodes(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	bound, err := s.core.ExitClients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		d := s.nodeDTO(n)
		d.Clients = bound[n.ID]
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

type nodeRequest struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Address   string `json:"address"`
	IsExit    bool   `json:"isExit"`
	IsEntry   bool   `json:"isEntry"`
	Enabled   *bool  `json:"enabled"`
	HopPort   int    `json:"hopPort"`
	AgentPort int    `json:"agentPort"`
	Link      string `json:"link"`
	Standby   bool   `json:"standby"`
}

func (req nodeRequest) input() core.NodeInput {
	return core.NodeInput{
		Name: req.Name, Kind: req.Kind, Address: req.Address, IsExit: req.IsExit, IsEntry: req.IsEntry,
		Enabled: req.Enabled == nil || *req.Enabled, HopPort: req.HopPort, AgentPort: req.AgentPort, Link: req.Link, Standby: req.Standby,
	}
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	n, err := s.core.CreateNode(r.Context(), req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "node.create", n.Name, map[string]any{"kind": n.Kind, "address": n.Address})
	writeJSON(w, http.StatusCreated, s.nodeDTO(n))
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	n, err := s.core.UpdateNode(r.Context(), id, req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "node.update", n.Name, nil)
	writeJSON(w, http.StatusOK, s.nodeDTO(n))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	n, err := s.core.Node(r.Context(), id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	if err := s.core.DeleteNode(r.Context(), id); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "node.delete", n.Name, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleNodeJoin returns the command that starts the agent on the server.
// It contains the node's private key, hence admin only and never cached.
func (s *Server) handleNodeJoin(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	join, err := s.core.NodeJoin(r.Context(), id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "node.join_command", "", map[string]any{"id": id})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, join)
}

// handleCheckNodes contacts every node now instead of waiting for the next
// round, and returns the fresh list.
func (s *Server) handleCheckNodes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	s.core.SyncNodes(ctx)
	s.core.ProbeExits(ctx)
	s.handleListNodes(w, r)
}

// locationRequest places a server on the map by hand; a null location
// returns to automatic lookup.
type locationRequest struct {
	Location *struct {
		Lat float64 `json:"lat"`
		Lon float64 `json:"lon"`
	} `json:"location"`
}

func (req locationRequest) location() *core.Location {
	if req.Location == nil {
		return nil
	}
	return &core.Location{Lat: req.Location.Lat, Lon: req.Location.Lon}
}

func (s *Server) handleSetNodeLocation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req locationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.SetNodeLocation(r.Context(), id, req.location()); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.core.KickNodes()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	host, _ := s.core.PublicHost(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"main": map[string]any{"host": host, "location": s.core.ServerLocation(r.Context())},
		// Whether detailed map tiles are available (PORTUNUS_MAP_TILES),
		// and what the map component has to know about them.
		"tiles": s.cfg.MapTiles != "",
		"map": map[string]any{
			// The provider has a light and a dark version of its own.
			"themed": strings.Contains(s.cfg.MapTiles, "{style}"),
			"labels": s.cfg.MapLabels != "",
			// The drawn map can be zoomed in to towns (PORTUNUS_MAP_DETAIL).
			"detail": s.cfg.MapDetail != "",
			"credit": s.cfg.MapCredit,
		},
	})
}

func (s *Server) handleSetServerLocation(w http.ResponseWriter, r *http.Request) {
	var req locationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.SetServerLocation(r.Context(), req.location()); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.core.KickNodes()
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- routing

type routeTargetDTO struct {
	Action string `json:"action"`
	NodeID *int64 `json:"nodeId,omitempty"`
}

type routeListDTO struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
	// Format is "text" or "ruleset" (a sing-box rule-set).
	Format string `json:"format"`
	// Content is returned for typed lists only; downloaded ones can be large.
	Content string `json:"content,omitempty"`
	// What the list holds, counted the way sing-box will see it.
	Domains    int        `json:"domains"`
	Addresses  int        `json:"addresses"`
	Skipped    int        `json:"skipped"`
	FetchedAt  *time.Time `json:"fetchedAt,omitempty"`
	FetchError string     `json:"fetchError,omitempty"`
}

func routeListToDTO(l *store.RouteListRow) routeListDTO {
	sites, addresses, skipped := core.RouteListCounts(l)
	d := routeListDTO{
		ID: l.ID, Name: l.Name, Kind: l.Kind, URL: l.URL, Format: l.Format,
		Domains: sites, Addresses: addresses, Skipped: skipped, FetchedAt: l.FetchedAt, FetchError: l.FetchError,
	}
	if l.Kind == core.ListManual {
		d.Content = l.Content
	}
	return d
}

type routeRuleDTO struct {
	ID      int64  `json:"id"`
	ListID  int64  `json:"listId"`
	Action  string `json:"action"`
	NodeID  *int64 `json:"nodeId,omitempty"`
	Enabled bool   `json:"enabled"`
}

func (s *Server) handleGetRouting(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lists, err := s.core.RouteLists(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	rules, err := s.core.RouteRules(ctx)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := struct {
		Default routeTargetDTO `json:"default"`
		Rules   []routeRuleDTO `json:"rules"`
		Lists   []routeListDTO `json:"lists"`
	}{Rules: []routeRuleDTO{}, Lists: []routeListDTO{}}
	action, node := s.core.DefaultRoute(ctx)
	out.Default.Action = action
	if action == proxy.RouteNode {
		out.Default.NodeID = &node
	}
	for _, l := range lists {
		out.Lists = append(out.Lists, routeListToDTO(l))
	}
	for _, rule := range rules {
		out.Rules = append(out.Rules, routeRuleDTO{ID: rule.ID, ListID: rule.ListID, Action: rule.Action, NodeID: rule.NodeID, Enabled: rule.Enabled})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetDefaultRoute(w http.ResponseWriter, r *http.Request) {
	var req routeTargetDTO
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.SetDefaultRoute(r.Context(), req.Action, req.NodeID); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.default", req.Action, nil)
	w.WriteHeader(http.StatusNoContent)
}

type routeListRequest struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (s *Server) handleCreateRouteList(w http.ResponseWriter, r *http.Request) {
	var req routeListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	l, err := s.core.CreateRouteList(r.Context(), core.RouteListInput(req))
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.list.create", l.Name, nil)
	writeJSON(w, http.StatusCreated, routeListToDTO(l))
}

func (s *Server) handleUpdateRouteList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req routeListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	l, err := s.core.UpdateRouteList(r.Context(), id, core.RouteListInput(req))
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.list.update", l.Name, nil)
	writeJSON(w, http.StatusOK, routeListToDTO(l))
}

func (s *Server) handleDeleteRouteList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := s.core.DeleteRouteList(r.Context(), id); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.list.delete", "", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRefreshRouteList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	l, err := s.core.RefreshRouteList(r.Context(), id)
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, routeListToDTO(l))
}

type routeRuleRequest struct {
	ListID  int64  `json:"listId"`
	Action  string `json:"action"`
	NodeID  *int64 `json:"nodeId"`
	Enabled *bool  `json:"enabled"`
}

func (req routeRuleRequest) input() core.RouteRuleInput {
	return core.RouteRuleInput{ListID: req.ListID, Action: req.Action, NodeID: req.NodeID, Enabled: req.Enabled == nil || *req.Enabled}
}

func (s *Server) handleCreateRouteRule(w http.ResponseWriter, r *http.Request) {
	var req routeRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	rule, err := s.core.CreateRouteRule(r.Context(), req.input())
	if err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.rule.create", rule.Action, nil)
	writeJSON(w, http.StatusCreated, routeRuleDTO{ID: rule.ID, ListID: rule.ListID, Action: rule.Action, NodeID: rule.NodeID, Enabled: rule.Enabled})
}

func (s *Server) handleUpdateRouteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var req routeRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.UpdateRouteRule(r.Context(), id, req.input()); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.rule.update", req.Action, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteRouteRule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err := s.core.DeleteRouteRule(r.Context(), id); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.rule.delete", "", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOrderRouteRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.core.OrderRouteRules(r.Context(), req.IDs); err != nil {
		s.writeCoreError(w, r, err)
		return
	}
	s.auditAction(r, "routing.rule.order", "", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleRecovery returns what the operator must keep to bring the panel up
// on a standby node: the recovery key, and the commands that do it.
func (s *Server) handleRecovery(w http.ResponseWriter, r *http.Request) {
	key, err := s.core.RecoveryKey(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.auditAction(r, "recovery.key_shown", "", nil)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"key": key,
		"commands": []string{
			"curl -fsSLO https://raw.githubusercontent.com/Portunus-org/Portunus/main/docker-compose.yml",
			"sudo docker compose run --rm -v portunus-node:/node:ro portunus restore /node/" + agent.PanelCopyFile,
			"sudo docker rm -f portunus-node && sudo docker compose up -d",
		},
	})
}
