package core

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// A chain is a named directed graph over the nodes: clients enter at its
// sources and leave from its sinks, hopping along the edges in between. An
// edge may start at the main server (from 0) or at any node; a source can be
// marked as a backup — it stands in when the primary ones fail. Every hop
// has its own VLESS + REALITY material on the receiving node, so a node may
// carry several chains at once.

const (
	// maxChainEdges keeps a graph from growing past what is operable.
	maxChainEdges = 64
	// Chain hops take their ports from here; it stays clear of the usual
	// inbound range and of the panel's own ports.
	chainPortFirst = 18000
	chainPortLast  = 65535
)

// EdgeInput is one directed edge of a chain: from the main server (0) or a
// node, to a node.
type EdgeInput struct {
	From int64
	To   int64
}

// ChainInput is what the operator submits for a chain: its name, the edges
// of its graph, and the sources marked as backups.
type ChainInput struct {
	Name    string
	Color   string // display color; empty means the default
	Edges   []EdgeInput
	Backups []int64
}

// ChainEdge is one directed edge of a chain with its hop material.
type ChainEdge struct {
	ID         int64
	FromNodeID int64 // 0: the main server
	ToNodeID   int64
	Port       int
	Set        proxy.Settings
	Sec        proxy.Secret
	UUID       string
}

// ready reports whether the edge has hop material. Edges converted from the
// old backup entry/exit arrive without it until their chain is next saved.
func (e ChainEdge) ready() bool { return e.Port != 0 && e.UUID != "" }

// Chain is one stored chain with its graph decoded.
// ChainColors are the display colors a chain may take on the map and in
// the chains list; the empty string is the panel default (foreground).
var ChainColors = []string{"#0ea5e9", "#8b5cf6", "#10b981", "#f59e0b", "#f43f5e", "#84cc16"}

type Chain struct {
	ID        int64
	Name      string
	Color     string // display color on the map; empty means the default
	Edges     []ChainEdge
	Backups   []int64 // source nodes marked as backups
	CreatedAt time.Time
	UpdatedAt time.Time
}

func edgeAAD(id int64) string { return "chain-edge:" + strconv.FormatInt(id, 10) }

// outEdges are the edges leaving from (0: the main server).
func (ch Chain) outEdges(from int64) []ChainEdge {
	var out []ChainEdge
	for _, e := range ch.Edges {
		if e.FromNodeID == from {
			out = append(out, e)
		}
	}
	return out
}

// inEdges are the edges entering to (a real node).
func (ch Chain) inEdges(to int64) []ChainEdge {
	var out []ChainEdge
	for _, e := range ch.Edges {
		if e.ToNodeID == to {
			out = append(out, e)
		}
	}
	return out
}

// isSource reports whether id is an independent way into the chain: the
// main server when it has outgoing edges, or a node with outgoing edges and
// no incoming ones at all. A node fed only by the main server is a hop,
// not a source — clients reach it through the main server.
func (ch Chain) isSource(id int64) bool {
	if len(ch.outEdges(id)) == 0 {
		return false
	}
	if id == 0 {
		return true
	}
	return len(ch.inEdges(id)) == 0
}

// isSink reports whether traffic can leave the chain from id.
func (ch Chain) isSink(id int64) bool {
	return id != 0 && len(ch.outEdges(id)) == 0 && len(ch.inEdges(id)) > 0
}

// sources are the real source nodes, in id order.
func (ch Chain) sources() []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, e := range ch.Edges {
		if e.FromNodeID != 0 && !seen[e.FromNodeID] {
			seen[e.FromNodeID] = true
			out = append(out, e.FromNodeID)
		}
	}
	slices.Sort(out)
	var res []int64
	for _, id := range out {
		if ch.isSource(id) {
			res = append(res, id)
		}
	}
	return res
}

// sinks are all the sink nodes, in id order.
func (ch Chain) sinks() []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, e := range ch.Edges {
		if !seen[e.ToNodeID] {
			seen[e.ToNodeID] = true
			out = append(out, e.ToNodeID)
		}
	}
	slices.Sort(out)
	var res []int64
	for _, id := range out {
		if ch.isSink(id) {
			res = append(res, id)
		}
	}
	return res
}

// reach is the set of nodes entered from id by following edges, where ok
// admits every node entered (the start itself is assumed usable).
func (ch Chain) reach(id int64, ok func(int64) bool) map[int64]bool {
	seen := map[int64]bool{id: true}
	stack := []int64{id}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range ch.outEdges(cur) {
			if !seen[e.ToNodeID] && ok(e.ToNodeID) {
				seen[e.ToNodeID] = true
				stack = append(stack, e.ToNodeID)
			}
		}
	}
	return seen
}

// sinksFrom is the set of sinks reachable from id through usable nodes.
func (ch Chain) sinksFrom(id int64, ok func(int64) bool) map[int64]bool {
	out := map[int64]bool{}
	for n := range ch.reach(id, ok) {
		if ch.isSink(n) {
			out[n] = true
		}
	}
	return out
}

// hasCycle reports whether the edges over real nodes contain a cycle.
func hasCycle(edges []EdgeInput) bool {
	adj := map[int64][]int64{}
	for _, e := range edges {
		if e.From != 0 && e.To != 0 {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}
	const (
		white, gray, black = 0, 1, 2
	)
	color := map[int64]int{}
	var visit func(int64) bool
	visit = func(id int64) bool {
		color[id] = gray
		for _, next := range adj[id] {
			if color[next] == gray {
				return true
			}
			if color[next] == white && visit(next) {
				return true
			}
		}
		color[id] = black
		return false
	}
	for id := range adj {
		if color[id] == white && visit(id) {
			return true
		}
	}
	return false
}

// validateChain checks the graph and returns the nodes it runs through.
func (c *Core) validateChain(ctx context.Context, in ChainInput) (map[int64]Node, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, invalid("name", "required")
	}
	if tooLong(in.Name, 64) {
		return nil, invalid("name", "too_long")
	}
	if oddText(in.Name, false) {
		return nil, invalid("name", "bad_characters")
	}
	if in.Color != "" && !slices.Contains(ChainColors, in.Color) {
		return nil, invalid("color", "unknown")
	}
	if len(in.Edges) == 0 {
		return nil, invalid("edges", "required")
	}
	if len(in.Edges) > maxChainEdges {
		return nil, invalid("edges", "too_long")
	}

	nodes := map[int64]Node{}
	pairs := map[string]bool{}
	for _, e := range in.Edges {
		if e.To == 0 {
			return nil, invalid("edges", "unknown") // the head must be a node
		}
		if e.From != 0 && e.From == e.To {
			return nil, invalid("edges", "self_loop")
		}
		key := strconv.FormatInt(e.From, 10) + "|" + strconv.FormatInt(e.To, 10)
		if pairs[key] {
			return nil, invalid("edges", "duplicate")
		}
		pairs[key] = true
		for _, id := range []int64{e.From, e.To} {
			if id == 0 {
				continue
			}
			row, err := c.store.NodeByID(ctx, id)
			if err != nil {
				return nil, invalid("edges", "unknown")
			}
			n, err := c.decodeNode(row)
			if err != nil {
				return nil, err
			}
			nodes[id] = n
		}
	}
	if hasCycle(in.Edges) {
		return nil, invalid("edges", "cycle")
	}

	g := Chain{Edges: make([]ChainEdge, len(in.Edges))}
	for i, e := range in.Edges {
		g.Edges[i] = ChainEdge{FromNodeID: e.From, ToNodeID: e.To}
	}
	// An imported link has no agent to push a configuration into, so it may
	// only be a sink.
	for id, n := range nodes {
		if n.Kind == NodeLink && len(g.outEdges(id)) > 0 {
			return nil, invalid("edges", "link_not_source")
		}
	}
	// A source serves client inbounds: it must be an entry point.
	for _, id := range g.sources() {
		if !nodes[id].IsEntry {
			return nil, invalid("edges", "source_not_entry")
		}
	}
	// Backups are entry points marked as reserves: they must be agents that
	// receive clients and take part in the graph. The main server cannot be
	// one.
	inGraph := map[int64]bool{}
	for _, e := range g.Edges {
		if e.FromNodeID != 0 {
			inGraph[e.FromNodeID] = true
		}
		if e.ToNodeID != 0 {
			inGraph[e.ToNodeID] = true
		}
	}
	backups := map[int64]bool{}
	for _, id := range in.Backups {
		if backups[id] {
			continue
		}
		n, ok := nodes[id]
		if !ok || n.Kind != NodeAgent || !n.IsEntry || !inGraph[id] {
			return nil, invalid("backups", "not_source")
		}
		backups[id] = true
	}
	return nodes, nil
}

// freeChainPort picks the next free port for a chain hop on node n.
func (c *Core) freeChainPort(ctx context.Context, n Node) (int, error) {
	used := map[int]bool{}
	// Several nodes may live on one host, so the ports of every node stay
	// out of the way.
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return 0, err
	}
	anyEntry := false
	for _, m := range nodes {
		if m.Kind == NodeAgent {
			used[m.Settings.AgentPort] = true
			used[m.Settings.HopPort] = true
		}
		if m.IsEntry {
			anyEntry = true
		}
	}
	// Entry points all serve the same client inbounds on their hosts.
	if n.IsEntry || anyEntry {
		inbounds, err := c.Inbounds(ctx)
		if err != nil {
			return 0, err
		}
		for _, in := range inbounds {
			if in.Enabled {
				used[in.Port] = true
			}
		}
	}
	// Chain hops keep globally distinct ports so that colocated nodes never
	// collide.
	edges, err := c.store.AllChainEdges(ctx)
	if err != nil {
		return 0, err
	}
	for _, e := range edges {
		used[e.HopPort] = true
	}
	for port := chainPortFirst; port <= chainPortLast; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port for a chain hop on node %d", n.ID)
}

// decodeEdge reads one stored edge of a chain. An empty secret means the
// edge was converted from an old backup and has no material yet.
func (c *Core) decodeEdge(l *store.ChainEdgeRow) (ChainEdge, error) {
	if l.Secret == "" {
		return ChainEdge{ID: l.ID, FromNodeID: l.FromNodeID, ToNodeID: l.ToNodeID, Port: l.HopPort, UUID: l.UUID}, nil
	}
	var set proxy.Settings
	if err := json.Unmarshal([]byte(l.Settings), &set); err != nil {
		return ChainEdge{}, fmt.Errorf("chain edge %d settings: %w", l.ID, err)
	}
	plain, err := c.box.Open(l.Secret, edgeAAD(l.ID))
	if err != nil {
		return ChainEdge{}, fmt.Errorf("chain edge %d secret: %w", l.ID, err)
	}
	var sec proxy.Secret
	if err := json.Unmarshal([]byte(plain), &sec); err != nil {
		return ChainEdge{}, fmt.Errorf("chain edge %d secret: %w", l.ID, err)
	}
	return ChainEdge{ID: l.ID, FromNodeID: l.FromNodeID, ToNodeID: l.ToNodeID, Port: l.HopPort, Set: set, Sec: sec, UUID: l.UUID}, nil
}

// decodeChain reads a stored chain with its edges.
func (c *Core) decodeChain(row store.ChainRow, edges []store.ChainEdgeRow, backups []int64) (*Chain, error) {
	out := make([]ChainEdge, 0, len(edges))
	for i := range edges {
		e, err := c.decodeEdge(&edges[i])
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return &Chain{ID: row.ID, Name: row.Name, Color: row.Color, Edges: out, Backups: backups, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

// Chains lists every chain.
func (c *Core) Chains(ctx context.Context) ([]Chain, error) {
	rows, err := c.store.ListChains(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Chain, 0, len(rows))
	for _, row := range rows {
		ch, err := c.Chain(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, *ch)
	}
	return out, nil
}

// Chain returns one chain with its graph decoded.
func (c *Core) Chain(ctx context.Context, id int64) (*Chain, error) {
	row, err := c.store.ChainByID(ctx, id)
	if err != nil {
		return nil, err
	}
	edges, err := c.store.ChainEdges(ctx, id)
	if err != nil {
		return nil, err
	}
	backups, err := c.store.ChainEntryFlags(ctx, id)
	if err != nil {
		return nil, err
	}
	return c.decodeChain(row, edges, backups)
}

// reconcileChainEdges brings the stored edges of a chain to the input graph:
// unchanged edges keep their material, new ones get fresh keys and ports,
// and what is left over (or was converted without material) is dropped and
// rebuilt.
func (c *Core) reconcileChainEdges(ctx context.Context, chainID int64, in ChainInput, nodes map[int64]Node) error {
	stored, err := c.store.ChainEdges(ctx, chainID)
	if err != nil {
		return err
	}
	key := func(from, to int64) string {
		return strconv.FormatInt(from, 10) + "|" + strconv.FormatInt(to, 10)
	}
	inKeys := map[string]bool{}
	for _, e := range in.Edges {
		inKeys[key(e.From, e.To)] = true
	}
	byKey := map[string]*store.ChainEdgeRow{}
	var toCreate []EdgeInput
	for i := range stored {
		k := key(stored[i].FromNodeID, stored[i].ToNodeID)
		if !inKeys[k] || stored[i].HopPort == 0 || stored[i].UUID == "" {
			// Dropped from the graph, or converted from an old backup without
			// material: rebuild it below.
			if err := c.store.DeleteChainEdge(ctx, stored[i].ID); err != nil {
				return err
			}
			if inKeys[k] {
				toCreate = append(toCreate, EdgeInput{From: stored[i].FromNodeID, To: stored[i].ToNodeID})
			}
			continue
		}
		byKey[k] = &stored[i]
	}
	for _, e := range in.Edges {
		if byKey[key(e.From, e.To)] == nil {
			toCreate = append(toCreate, e)
		}
	}
	slices.SortFunc(toCreate, func(a, b EdgeInput) int {
		if a.From != b.From {
			return cmp.Compare(a.From, b.From)
		}
		return cmp.Compare(a.To, b.To)
	})
	for _, e := range toCreate {
		port, err := c.freeChainPort(ctx, nodes[e.To])
		if err != nil {
			return err
		}
		// The hop imitates the same site as the legacy exit hops do, so
		// clients verify against a real certificate.
		link, err := proxy.NewChainLink(port, c.hopCamouflage(ctx))
		if err != nil {
			return err
		}
		setJSON, err := json.Marshal(link.Settings)
		if err != nil {
			return err
		}
		row := store.ChainEdgeRow{
			ChainID: chainID, FromNodeID: e.From, ToNodeID: e.To,
			HopPort: port, UUID: link.UUID, Settings: string(setJSON),
		}
		if _, err := c.store.CreateChainEdge(ctx, row, func(id int64) (string, error) {
			return c.box.Seal(mustJSON(link.Secret), edgeAAD(id)), nil
		}); err != nil {
			return err
		}
	}
	return c.store.SetChainEntryFlags(ctx, chainID, in.Backups)
}

// CreateChain stores a new chain and provisions the material of its edges.
func (c *Core) CreateChain(ctx context.Context, in ChainInput) (*Chain, error) {
	nodes, err := c.validateChain(ctx, in)
	if err != nil {
		return nil, err
	}
	id, err := c.store.CreateChain(ctx, in.Name, in.Color)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	if err := c.reconcileChainEdges(ctx, id, in, nodes); err != nil {
		c.store.DeleteChain(ctx, id) // leave no orphan behind
		return nil, err
	}
	c.Reconcile(ctx)
	return c.Chain(ctx, id)
}

// UpdateChain re-points a chain at a new name and graph.
func (c *Core) UpdateChain(ctx context.Context, id int64, in ChainInput) (*Chain, error) {
	if _, err := c.store.ChainByID(ctx, id); err != nil {
		return nil, err
	}
	nodes, err := c.validateChain(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := c.store.UpdateChain(ctx, id, in.Name, in.Color); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	if err := c.reconcileChainEdges(ctx, id, in, nodes); err != nil {
		return nil, err
	}
	c.Reconcile(ctx)
	return c.Chain(ctx, id)
}

// DeleteChain removes a chain; its clients are unbound by the database.
func (c *Core) DeleteChain(ctx context.Context, id int64) error {
	if _, err := c.store.ChainByID(ctx, id); err != nil {
		return err
	}
	if err := c.store.DeleteChain(ctx, id); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// ChainClients counts the active clients bound to each chain.
func (c *Core) ChainClients(ctx context.Context) (map[int64]int, error) {
	clients, err := c.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	for _, cl := range clients {
		if cl.ChainID != nil && c.ClientStatus(cl) == ClientActive {
			counts[*cl.ChainID]++
		}
	}
	return counts, nil
}

// chainInUse reports whether a node participates in any chain.
func (c *Core) chainInUse(ctx context.Context, id int64) (bool, error) {
	return c.store.NodeInChain(ctx, id)
}

// autoTag is the urltest group of one server's outgoing edges of a chain.
// mustJSON renders a value as JSON. A chain edge's secret is a plain
// string, so this never fails.
func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func autoTag(chainID, nodeID int64) string {
	return fmt.Sprintf("chain-auto-%d-%d", chainID, nodeID)
}

// chainClientNames returns the sing-box user names of every active client
// riding ch: the base name for its first sink and one derived name per
// further sink (the same scheme chainRouting binds).
func (c *Core) chainClientNames(ctx context.Context, ch Chain) ([]string, error) {
	clients, err := c.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, cl := range clients {
		if c.ClientStatus(cl) != ClientActive || cl.ChainID == nil || *cl.ChainID != ch.ID {
			continue
		}
		sinks := ch.sinks()
		base := "c" + strconv.FormatInt(cl.ID, 10)
		for i, s := range sinks {
			name := base
			if i > 0 {
				name += "-e" + strconv.FormatInt(s, 10)
			}
			names = append(names, name)
		}
	}
	return names, nil
}

// chainEntryNames returns, for every chain that self can enter (the main
// server, or an entry-point node the chain passes through), the credential
// names of its active clients. Rules scoped to a chain are served at those
// entries, for those names only: a client meets the rule where it comes in,
// and then travels on.
func (c *Core) chainEntryNames(ctx context.Context, self int64) (map[int64][]string, error) {
	chains, err := c.Chains(ctx)
	if err != nil {
		return nil, err
	}
	entry := self == 0
	if !entry {
		n, err := c.Node(ctx, self)
		if err != nil {
			return nil, err
		}
		entry = n.IsEntry
	}
	out := map[int64][]string{}
	for _, ch := range chains {
		if !entry || len(ch.outEdges(self)) == 0 {
			continue
		}
		names, err := c.chainClientNames(ctx, ch)
		if err != nil {
			return nil, err
		}
		out[ch.ID] = names
	}
	return out, nil
}

// chainRouting computes the chain part of server self's configuration
// (0: the main one): the hops that end on it, the outbounds its traffic
// leaves through, and the bindings of the clients bound to chains. Only
// servers that serve client inbounds (the main one and entry points) get
// the bindings; the rest only carry the hops.
func (c *Core) chainRouting(ctx context.Context, self int64) (links []proxy.ChainLink, outbounds []map[string]any, bindings []proxy.Binding, err error) {
	chains, err := c.Chains(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(chains) == 0 {
		return nil, nil, nil, nil
	}
	all, err := c.Nodes(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	nodes := make(map[int64]Node, len(all))
	for _, n := range all {
		nodes[n.ID] = n
	}
	isEntry := self == 0 || (self != 0 && nodes[self].IsEntry)

	// Every active client bound to a chain wants out of each of the chain's
	// sinks, under its own credential: the first sink carries the base uuid,
	// the rest derived ones.
	type userTarget struct {
		name string
		sink int64
	}
	usersByChain := map[int64][]userTarget{}
	if isEntry {
		clients, err := c.store.ListClients(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, cl := range clients {
			if c.ClientStatus(cl) != ClientActive || cl.ChainID == nil {
				continue
			}
			ch, ok := chainByID(chains, *cl.ChainID)
			if !ok {
				continue
			}
			sinks := ch.sinks()
			base := "c" + strconv.FormatInt(cl.ID, 10)
			for i, s := range sinks {
				name := base
				if i > 0 {
					name += "-e" + strconv.FormatInt(s, 10)
				}
				usersByChain[ch.ID] = append(usersByChain[ch.ID], userTarget{name: name, sink: s})
			}
		}
	}

	unusable := func(id int64) bool {
		n, ok := nodes[id]
		return !ok || c.nodeUnusable(n)
	}
	for _, ch := range chains {
		outE := ch.outEdges(self)
		inE := ch.inEdges(self)
		if len(outE) == 0 && self != 0 && len(inE) == 0 {
			continue // this server is not part of the chain
		}

		live := liveOutgoing(ch, outE, unusable)
		autoNeeded := false

		// Tags of every ready outgoing branch (dead ones included): a tag is
		// only ever referenced by a urltest group or a binding when it also
		// exists as an outbound, so sing-box can resolve the config.
		tags := make([]any, 0, len(outE))
		for _, e := range outE {
			if e.ready() {
				tags = append(tags, proxy.ClinkTag(e.ID))
			}
		}

		// The hops that end on this server, and where each one carries the
		// traffic next.
		for _, e := range inE {
			if !e.ready() {
				continue // converted without material: no hop inbound to render
			}
			next := ""
			switch {
			case ch.isSink(self):
				// The traffic leaves from this server.
				next = "direct"
			case len(live) == 1:
				next = proxy.ClinkTag(live[0].ID)
			case len(tags) > 0:
				// Several branches leave here (or none is live right now):
				// let the urltest group pick among them.
				autoNeeded = true
				next = autoTag(ch.ID, self)
			}
			links = append(links, proxy.ChainLink{ID: e.ID, Port: e.Port, Settings: e.Set, Secret: e.Sec, UUID: e.UUID, Next: next})
		}

		users := usersByChain[ch.ID]
		if isEntry && len(users) > 0 {
			if ch.isSink(self) {
				// Traffic that entered here leaves from here — but only for
				// the credentials aimed at this sink; the others have no way
				// out through this server.
				for _, u := range users {
					if u.sink == self {
						bindings = append(bindings, proxy.Binding{Users: []string{u.name}, Local: true})
					}
				}
			} else if len(tags) > 0 && (len(live) > 0 || len(outE) > 1) {
				for _, u := range users {
					var cand []ChainEdge
					for _, e := range live {
						if ch.sinksFrom(e.ToNodeID, func(id int64) bool { return !unusable(id) })[u.sink] {
							cand = append(cand, e)
						}
					}
					switch {
					case len(cand) == 1:
						bindings = append(bindings, proxy.Binding{Users: []string{u.name}, Outbound: proxy.ClinkTag(cand[0].ID)})
					default:
						// Several branches reach the sink — or none is live
						// right now: let the urltest group pick (or fail over to
						// one that comes back).
						autoNeeded = true
						bindings = append(bindings, proxy.Binding{Users: []string{u.name}, Outbound: autoTag(ch.ID, self)})
					}
				}
			}
		}
		if len(outE) == 0 {
			continue
		}
		// Every ready branch gets an outbound, even a dead one: the urltest
		// group must be able to resolve every tag it lists, and a branch that
		// comes back should be picked up without another reconciliation.
		for _, e := range outE {
			if !e.ready() {
				continue
			}
			outbounds = append(outbounds, tagged(edgeOutbound(e, nodes[e.ToNodeID].Address), proxy.ClinkTag(e.ID)))
		}
		if autoNeeded {
			outbounds = append(outbounds, map[string]any{
				"type":      "urltest",
				"tag":       autoTag(ch.ID, self),
				"outbounds": tags,
				"url":       proxy.URLTestURL,
				// How soon a dead branch is noticed: this is how long a client
				// rides the failed one before switching. A check is one tiny
				// request per branch.
				"interval": "30s",
				// Stay on the branch in use for as long as it answers, however
				// fast the others are: what leaves a sink directly carries its
				// address, and that should not change without need.
				"tolerance": 50,
			})
		}
	}
	return links, outbounds, bindings, nil
}

// liveOutgoing is the subset of a server's outgoing edges whose head is
// usable and can still reach some sink.
func liveOutgoing(ch Chain, out []ChainEdge, unusable func(int64) bool) []ChainEdge {
	var live []ChainEdge
	for _, e := range out {
		if !e.ready() || unusable(e.ToNodeID) {
			continue
		}
		if len(ch.sinksFrom(e.ToNodeID, func(id int64) bool { return !unusable(id) })) > 0 {
			live = append(live, e)
		}
	}
	return live
}

// edgeOutbound renders the sing-box outbound that leaves through this edge.
func edgeOutbound(e ChainEdge, address string) map[string]any {
	return proxy.ChainLink{Port: e.Port, Settings: e.Set, Secret: e.Sec, UUID: e.UUID}.Outbound(address)
}

// tagged copies an outbound and gives it a tag.
func tagged(ob map[string]any, tag string) map[string]any {
	out := make(map[string]any, len(ob)+1)
	for k, v := range ob {
		out[k] = v
	}
	out["tag"] = tag
	return out
}

// chainWorks reports whether the chain still has a usable route: the main
// server, or some entry node clients can dial directly (backups included),
// reaches some sink through usable nodes.
func (c *Core) chainWorks(ch Chain, unusable func(int64) bool, isEntry func(int64) bool) bool {
	ok := func(id int64) bool { return !unusable(id) }
	if len(ch.outEdges(0)) > 0 && len(ch.sinksFrom(0, ok)) > 0 {
		return true
	}
	seen := map[int64]bool{}
	for _, e := range ch.Edges {
		id := e.FromNodeID
		if id == 0 || seen[id] || !isEntry(id) || unusable(id) {
			continue
		}
		seen[id] = true
		if len(ch.sinksFrom(id, ok)) > 0 {
			return true
		}
	}
	return false
}

// nodeUnusable reports whether a node cannot carry traffic right now:
// switched off, or failing its checks and given up on. An imported link has
// no agent to check.
func (c *Core) nodeUnusable(n Node) bool {
	if !n.Enabled {
		return true
	}
	switch {
	case n.Kind == NodeAgent && c.chainLost(n.ID):
		return true
	case n.IsExit:
		if _, lost := c.exitState(n.ID); lost {
			return true
		}
	}
	return false
}

func chainByID(chains []Chain, id int64) (Chain, bool) {
	for _, ch := range chains {
		if ch.ID == id {
			return ch, true
		}
	}
	return Chain{}, false
}
