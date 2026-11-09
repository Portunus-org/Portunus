package core

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// newChainCore builds a core with two entry-capable agent nodes, an exit
// agent and an imported link, ready for chain tests.
func newChainCore(t *testing.T) (*Core, map[string]int64) {
	t.Helper()
	c, _ := newBindingCore(t)
	ctx := context.Background()
	agent := func(name, address string, entry, exit bool) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Name: name, Kind: NodeAgent, Address: address, IsExit: exit, IsEntry: entry, Enabled: true})
		if err != nil {
			t.Fatalf("node %s: %v", name, err)
		}
		return n.ID
	}
	link := func(name string) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@" + name + ".example.com:443#" + name})
		if err != nil {
			t.Fatalf("link %s: %v", name, err)
		}
		return n.ID
	}
	ids := map[string]int64{
		"a": agent("A", "203.0.113.10", true, false), // entry point
		"b": agent("B", "203.0.113.11", false, true), // exit
		"c": agent("C", "203.0.113.12", true, false), // second source
		"l": link("L"),                               // a sink only
	}
	return c, ids
}

func chainEdges(t *testing.T, c *Core, id int64) []store.ChainEdgeRow {
	t.Helper()
	edges, err := c.store.ChainEdges(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return edges
}

func edgeBy(t *testing.T, edges []store.ChainEdgeRow, from, to int64) store.ChainEdgeRow {
	t.Helper()
	for _, e := range edges {
		if e.FromNodeID == from && e.ToNodeID == to {
			return e
		}
	}
	t.Fatalf("no edge %d -> %d in %+v", from, to, edges)
	return store.ChainEdgeRow{}
}

func TestChainValidation(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	code := func(err error) string {
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("not a validation error: %v", err)
		}
		return v.Field + ":" + v.Code
	}
	a, b, cn, l := ids["a"], ids["b"], ids["c"], ids["l"]

	if _, err := c.CreateChain(ctx, ChainInput{Name: "x"}); code(err) != "edges:required" {
		t.Fatalf("no edges: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "", Edges: []EdgeInput{{From: 0, To: a}}}); code(err) != "name:required" {
		t.Fatalf("no name: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: a, To: a}}}); code(err) != "edges:self_loop" {
		t.Fatalf("self loop: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: 0, To: a}, {From: 0, To: a}}}); code(err) != "edges:duplicate" {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: 0, To: 999999}}}); code(err) != "edges:unknown" {
		t.Fatalf("unknown node: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: 0, To: 0}}}); code(err) != "edges:self_loop" {
		t.Fatalf("self loop 0->0: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "to-main", Edges: []EdgeInput{{From: a, To: 0}}}); err != nil {
		t.Fatalf("edge into main must be valid: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: a, To: b}, {From: b, To: a}}}); code(err) != "edges:cycle" {
		t.Fatalf("cycle: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: l, To: b}}}); code(err) != "edges:link_not_source" {
		t.Fatalf("link as source: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: b, To: l}}}); code(err) != "edges:source_not_entry" {
		t.Fatalf("non-entry source: %v", err)
	}

	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: cn}}}); code(err) != "name:taken" {
		t.Fatalf("clash: %v", err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{Name: "x", Edges: []EdgeInput{{From: 0, To: a}}, Backups: []int64{b}}); code(err) != "backups:not_source" {
		t.Fatalf("sink as backup: %v", err)
	}

	// Every edge gets hop material on its receiving node.
	edges := chainEdges(t, c, ch.ID)
	if len(edges) != 2 {
		t.Fatalf("edges: %+v", edges)
	}
	for _, e := range edges {
		if e.HopPort < chainPortFirst || e.UUID == "" || e.Settings == "" || e.Secret == "" {
			t.Fatalf("material: %+v", e)
		}
		// The hop imitates a real site, not the node's own address: clients
		// verify the decoy certificate against that name.
		var set proxy.Settings
		if err := json.Unmarshal([]byte(e.Settings), &set); err != nil {
			t.Fatalf("settings: %v", err)
		}
		if set.RealityServer != proxy.DefaultRealityServer {
			t.Fatalf("reality server %q, want the default site", set.RealityServer)
		}
	}
}

// TestChainMainFirst checks that the main server may open a chain as an
// implicit origin.
func TestChainMainFirst(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]

	ch, err := c.CreateChain(ctx, ChainInput{Name: "ViaMain", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	edges := chainEdges(t, c, ch.ID)
	e0 := edgeBy(t, edges, 0, a)

	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	user := proxy.Client{ID: cl.ID}.UserName()

	r, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	bound := false
	for _, bd := range r.Bindings {
		if len(bd.Users) == 1 && bd.Users[0] == user && bd.Outbound == proxy.ClinkTag(e0.ID) {
			bound = true
		}
	}
	if !bound {
		t.Fatalf("main does not bind the client into the chain: %+v", r.Bindings)
	}
}

func TestChainColor(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]
	code := func(err error) string {
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Fatalf("not a validation error: %v", err)
		}
		return v.Field + ":" + v.Code
	}
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Color: "#8b5cf6", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Color != "#8b5cf6" {
		t.Fatalf("color not stored: %q", ch.Color)
	}
	got, err := c.Chain(ctx, ch.ID)
	if err != nil || got.Color != "#8b5cf6" {
		t.Fatalf("color not read back: %+v %v", got, err)
	}
	in := ChainInput{Name: "Asia", Color: "#10b981", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}}
	got, err = c.UpdateChain(ctx, ch.ID, in)
	if err != nil || got.Color != "#10b981" {
		t.Fatalf("color not updated: %+v %v", got, err)
	}
	in.Color = ""
	got, err = c.UpdateChain(ctx, ch.ID, in)
	if err != nil || got.Color != "" {
		t.Fatalf("color not reset to default: %+v %v", got, err)
	}
	in.Color = "#123456"
	if _, err = c.UpdateChain(ctx, ch.ID, in); code(err) != "color:unknown" {
		t.Fatalf("bad color accepted: %v", err)
	}
}

func TestChainUpdateKeepsMaterial(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b, cn := ids["a"], ids["b"], ids["c"]
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	before := chainEdges(t, c, ch.ID)

	// Adding a second source keeps the old edges and adds new ones.
	if _, err := c.UpdateChain(ctx, ch.ID, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}, {From: cn, To: b}}}); err != nil {
		t.Fatal(err)
	}
	after := chainEdges(t, c, ch.ID)
	if len(after) != 3 {
		t.Fatalf("edges after extend: %+v", after)
	}
	e0 := edgeBy(t, before, 0, a)
	e1 := edgeBy(t, before, a, b)
	for _, e := range after {
		switch {
		case e.FromNodeID == 0 && e.ToNodeID == a:
			if e.ID != e0.ID || e.UUID != e0.UUID {
				t.Fatalf("edge 0->a not kept: %+v vs %+v", e, e0)
			}
		case e.FromNodeID == a && e.ToNodeID == b:
			if e.ID != e1.ID || e.UUID != e1.UUID {
				t.Fatalf("edge a->b not kept: %+v vs %+v", e, e1)
			}
		case e.FromNodeID == cn && e.ToNodeID == b:
			if e.HopPort < chainPortFirst || e.UUID == "" {
				t.Fatalf("new edge material: %+v", e)
			}
		default:
			t.Fatalf("unexpected edge: %+v", e)
		}
	}

	// Removing an edge drops it.
	if _, err := c.UpdateChain(ctx, ch.ID, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}}); err != nil {
		t.Fatal(err)
	}
	if edges := chainEdges(t, c, ch.ID); len(edges) != 2 {
		t.Fatalf("edges after shrink: %+v", edges)
	}
}

func TestChainDeletionUnbindsClients(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: ids["a"]}, {From: ids["a"], To: ids["b"]}}})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.store.ListClients(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].ChainID == nil || *rows[0].ChainID != ch.ID {
		t.Fatalf("client not bound: %+v", rows[0])
	}
	if err := c.DeleteChain(ctx, ch.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	if rows[0].ChainID != nil {
		t.Fatalf("client still bound: %+v", rows[0])
	}
	if edges := chainEdges(t, c, ch.ID); len(edges) != 0 {
		t.Fatalf("edges left behind: %v", edges)
	}
	_ = cl
}

func TestChainPortsDoNotClash(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b, cn := ids["a"], ids["b"], ids["c"]
	first, err := c.CreateChain(ctx, ChainInput{Name: "one", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateChain(ctx, ChainInput{Name: "two", Edges: []EdgeInput{{From: 0, To: cn}, {From: cn, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	p1 := edgeBy(t, chainEdges(t, c, first.ID), 0, a).HopPort
	p2 := edgeBy(t, chainEdges(t, c, second.ID), 0, cn).HopPort
	if p1 == p2 {
		t.Fatalf("two chains took port %d", p1)
	}
	// The entry node's own ports stay clear of the chain hops.
	n, err := c.Node(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []int{p1, p2} {
		if p == n.Settings.AgentPort || p == n.Settings.HopPort {
			t.Fatalf("chain hop on the node's own port %d", p)
		}
	}
}

func TestChainClientBinding(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]
	exit, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@exit.example.com:443#exit"})
	if err != nil {
		t.Fatal(err)
	}
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	chID := ch.ID
	if _, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &chID}); err != nil {
		t.Fatal(err)
	}
	// A chain and an exit are two different routes.
	if _, err := c.CreateClient(ctx, ClientInput{Name: "c2", Enabled: true, Chain: &chID, Exit: &a}); err == nil {
		t.Fatal("both a chain and an exit accepted")
	}
	var bad int64 = 999999
	if _, err := c.CreateClient(ctx, ClientInput{Name: "c3", Enabled: true, Chain: &bad}); err == nil {
		t.Fatal("unknown chain accepted")
	}

	rows, _ := c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != chID || rows[0].ExitID != nil {
		t.Fatalf("bound: %+v", rows[0])
	}

	// An intact chain keeps its clients.
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != chID {
		t.Fatalf("moved off an intact chain: %+v", rows[0])
	}

	// A broken chain sends its clients to an exit.
	if _, err := c.UpdateNode(ctx, a, NodeInput{Name: "A", Kind: NodeAgent, Address: "203.0.113.10", IsEntry: true, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != chID || rows[0].ExitID == nil {
		// The client keeps its chain (to come back when it heals) and gets a
		// stopgap exit meanwhile.
		t.Fatalf("broken chain: %+v", rows[0])
	}

	// Choosing an exit releases the chain, and rotating does too.
	ch2, err := c.CreateChain(ctx, ChainInput{Name: "Asia 2", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	ch2ID := ch2.ID
	cl2, err := c.CreateClient(ctx, ClientInput{Name: "c4", Enabled: true, Chain: &ch2ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.UpdateClient(ctx, cl2.ID, ClientInput{Name: "c4", Exit: &exit.ID}); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	for _, r := range rows {
		if r.ID == cl2.ID && (r.ChainID != nil || *r.ExitID != exit.ID) {
			t.Fatalf("exit choice: %+v", r)
		}
	}
	if _, err := c.RotateClient(ctx, cl2.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	for _, r := range rows {
		// The chain is released; the auto binding may pick an exit anew.
		if r.ID == cl2.ID && r.ChainID != nil {
			t.Fatalf("rotate kept the chain: %+v", r)
		}
	}
}

// TestChainRouting checks what each server of a chain gets rendered.
func TestChainRouting(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	user := proxy.Client{ID: cl.ID}.UserName()

	e0 := edgeBy(t, chainEdges(t, c, ch.ID), 0, a)
	e1 := edgeBy(t, chainEdges(t, c, ch.ID), a, b)

	routing := func(self int64) proxy.Routing {
		t.Helper()
		r, err := c.routing(ctx, self)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tagOf := func(r proxy.Routing, tag string) bool {
		for _, o := range r.ChainOutbounds {
			m, _ := o.(map[string]any)
			if m != nil && m["tag"] == tag {
				return true
			}
		}
		return false
	}
	boundTo := func(r proxy.Routing, out string) bool {
		for _, b := range r.Bindings {
			if len(b.Users) == 1 && b.Users[0] == user && b.Outbound == out && !b.Local {
				return true
			}
		}
		return false
	}
	linkNext := func(r proxy.Routing, id int64) (string, bool) {
		for _, l := range r.Links {
			if l.ID == id {
				return l.Next, true
			}
		}
		return "", false
	}

	// The main server opens the chain through its first edge.
	r := routing(0)
	if len(r.Links) != 0 || !tagOf(r, proxy.ClinkTag(e0.ID)) || !boundTo(r, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("main: %+v", r.Bindings)
	}

	// The first node ends the first edge and forwards through the second.
	r = routing(a)
	if next, ok := linkNext(r, e0.ID); !ok || next != proxy.ClinkTag(e1.ID) {
		t.Fatalf("a: edge %d next=%q", e0.ID, next)
	}
	if !tagOf(r, proxy.ClinkTag(e1.ID)) || !boundTo(r, proxy.ClinkTag(e1.ID)) {
		t.Fatalf("a bindings: %+v", r.Bindings)
	}

	// The sink lets the traffic leave; it serves no clients of its own.
	r = routing(b)
	if next, ok := linkNext(r, e1.ID); !ok || next != "direct" {
		t.Fatalf("b: edge %d next=%q", e1.ID, next)
	}
	if len(r.ChainOutbounds) != 0 {
		t.Fatalf("b outbounds: %+v", r.ChainOutbounds)
	}
	if len(r.Bindings) != 0 {
		t.Fatalf("b serves no clients: %+v", r.Bindings)
	}

	// When the sink is itself an entry point, clients that connect to it
	// directly leave from there.
	ch2, err := c.CreateChain(ctx, ChainInput{Name: "Back", Edges: []EdgeInput{{From: 0, To: b}, {From: b, To: a}}})
	if err != nil {
		t.Fatal(err)
	}
	cl2, err := c.CreateClient(ctx, ClientInput{Name: "c2", Enabled: true, Chain: &ch2.ID})
	if err != nil {
		t.Fatal(err)
	}
	user2 := proxy.Client{ID: cl2.ID}.UserName()
	r = routing(a)
	if !boundLocal(r, user2) {
		t.Fatalf("a local binding missing: %+v", r.Bindings)
	}
}

func boundLocal(r proxy.Routing, user string) bool {
	for _, b := range r.Bindings {
		if b.Local && len(b.Users) == 1 && b.Users[0] == user {
			return true
		}
	}
	return false
}

// TestChainFanOut checks a node with two outgoing edges: its incoming hop
// hands the traffic to the urltest group, and each credential is bound to
// the branch that reaches its sink.
func TestChainFanOut(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b, l := ids["a"], ids["b"], ids["l"]
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Split", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}, {From: a, To: l}}})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	base := proxy.Client{ID: cl.ID}.UserName()
	other := base + "-e" + strconv.FormatInt(l, 10)

	e0 := edgeBy(t, chainEdges(t, c, ch.ID), 0, a)
	eB := edgeBy(t, chainEdges(t, c, ch.ID), a, b)
	eL := edgeBy(t, chainEdges(t, c, ch.ID), a, l)
	auto := autoTag(ch.ID, a)

	routing := func(self int64) proxy.Routing {
		t.Helper()
		r, err := c.routing(ctx, self)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	tagOf := func(r proxy.Routing, tag string) bool {
		for _, o := range r.ChainOutbounds {
			m, _ := o.(map[string]any)
			if m == nil {
				continue
			}
			if m["tag"] == tag {
				return true
			}
			if t, _ := m["type"].(string); t == "urltest" && m["tag"] == auto {
				group, _ := m["outbounds"].([]any)
				for _, g := range group {
					if g == tag {
						return true
					}
				}
			}
		}
		return false
	}
	boundTo := func(r proxy.Routing, user, out string) bool {
		for _, bd := range r.Bindings {
			if len(bd.Users) == 1 && bd.Users[0] == user && bd.Outbound == out && !bd.Local {
				return true
			}
		}
		return false
	}

	// The main server has one live way to both sinks.
	r := routing(0)
	if !boundTo(r, base, proxy.ClinkTag(e0.ID)) || !boundTo(r, other, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("main: %+v", r.Bindings)
	}

	// The split node fans out through the urltest group.
	r = routing(a)
	var next string
	for _, lk := range r.Links {
		if lk.ID == e0.ID {
			next = lk.Next
		}
	}
	if next != auto {
		t.Fatalf("a: incoming edge next=%q, want %q", next, auto)
	}
	if !tagOf(r, proxy.ClinkTag(eB.ID)) || !tagOf(r, proxy.ClinkTag(eL.ID)) {
		t.Fatalf("a outbounds: %+v", r.ChainOutbounds)
	}
	if !boundTo(r, base, proxy.ClinkTag(eB.ID)) || !boundTo(r, other, proxy.ClinkTag(eL.ID)) {
		t.Fatalf("a bindings: %+v", r.Bindings)
	}
}

// TestChainBackups checks a source marked as a backup: it is provisioned,
// it serves the chain when the primary way in fails, and the client stays
// on the chain while it does.
func TestChainBackups(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b, cn := ids["a"], ids["b"], ids["c"]

	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}, {From: cn, To: b}}, Backups: []int64{cn}})
	if err != nil {
		t.Fatal(err)
	}
	e0 := edgeBy(t, chainEdges(t, c, ch.ID), 0, a)
	eC := edgeBy(t, chainEdges(t, c, ch.ID), cn, b)

	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	user := proxy.Client{ID: cl.ID}.UserName()
	routing := func(self int64) proxy.Routing {
		t.Helper()
		r, err := c.routing(ctx, self)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	boundTo := func(r proxy.Routing, out string) bool {
		for _, bd := range r.Bindings {
			if len(bd.Users) == 1 && bd.Users[0] == user && bd.Outbound == out && !bd.Local {
				return true
			}
		}
		return false
	}

	// Healthy: the main server takes the primary edge.
	r := routing(0)
	if !boundTo(r, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("healthy main: %+v", r.Bindings)
	}
	// The backup source serves its own way into the chain.
	r = routing(cn)
	if len(r.Links) != 0 || !boundTo(r, proxy.ClinkTag(eC.ID)) {
		t.Fatalf("backup source: %+v", r.Bindings)
	}

	// The primary way in fails for good: the main server has no live edge,
	// but the chain still works through the backup — the client stays on it.
	c.nodeMu.Lock()
	c.chainHealth[a] = exitHealth{since: c.now().Add(-time.Hour), fails: exitFailures, told: true}
	c.nodeMu.Unlock()
	r = routing(0)
	if boundTo(r, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("failed main still bound to the dead edge: %+v", r.Bindings)
	}
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != ch.ID || rows[0].ExitID != nil {
		t.Fatalf("moved off a chain that has a working backup: %+v", rows[0])
	}

	// The primary way in returns: it is taken again.
	c.nodeMu.Lock()
	delete(c.chainHealth, a)
	c.nodeMu.Unlock()
	r = routing(0)
	if !boundTo(r, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("recovered main: %+v", r.Bindings)
	}

	// When the whole chain is down, the client takes an exit.
	c.nodeMu.Lock()
	c.chainHealth[a] = exitHealth{since: c.now().Add(-time.Hour), fails: exitFailures, told: true}
	c.chainHealth[cn] = exitHealth{since: c.now().Add(-time.Hour), fails: exitFailures, told: true}
	c.nodeMu.Unlock()
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ = c.store.ListClients(ctx)
	if rows[0].ExitID == nil {
		t.Fatalf("no fallback for a dead chain: %+v", rows[0])
	}
}

// The rendered configuration carries the hop inbounds and their rules.
func TestChainConfigRendering(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]
	ch, err := c.CreateChain(ctx, ChainInput{Name: "Asia", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	e0 := edgeBy(t, chainEdges(t, c, ch.ID), 0, a)
	e1 := edgeBy(t, chainEdges(t, c, ch.ID), a, b)

	// Bindings take effect through the auto route, as with exits.
	if err := c.SetDefaultRoute(ctx, proxy.RouteAuto, nil); err != nil {
		t.Fatal(err)
	}
	n, err := c.Node(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := c.nodeConfig(ctx, &n, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(cfg)
	for _, want := range []string{
		proxy.LinkTag(e0.ID),  // the hop inbound tag
		proxy.ClinkTag(e1.ID), // the outbound to the next node
		proxy.Client{ID: cl.ID}.UserName(),
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("config lacks %s", want)
		}
	}
}

// TestChainToMainServer verifies that an entry node can send traffic to the
// main server as its exit (e.g. domestic VPS -> foreign main server).
func TestChainToMainServer(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a := ids["a"] // entry agent

	if err := c.SetPublicHost(ctx, "main.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateInbound(ctx, InboundInput{Protocol: "vless", Port: 443, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nodeA, _ := c.Node(ctx, a)
	nodeA.Settings.JoinedAt = &now
	if err := c.saveNode(ctx, nodeA); err != nil {
		t.Fatal(err)
	}

	ch, err := c.CreateChain(ctx, ChainInput{Name: "ToMain", Edges: []EdgeInput{{From: a, To: 0}}})
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}
	edges := chainEdges(t, c, ch.ID)
	if len(edges) != 1 || edges[0].FromNodeID != a || edges[0].ToNodeID != 0 {
		t.Fatalf("unexpected edges: %+v", edges)
	}
	e := edges[0]

	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	user := proxy.Client{ID: cl.ID}.UserName()

	// 1. Client links: should point to the entry node 'a'.
	links, err := c.ClientLinks(ctx, &store.ClientRow{ID: cl.ID, UUID: cl.UUID, ChainID: &ch.ID})
	if err != nil {
		t.Fatalf("client links: %v", err)
	}
	if len(links) == 0 {
		t.Fatal("no client links returned")
	}
	for _, l := range links {
		if l.Host != "203.0.113.10" {
			t.Fatalf("expected host 203.0.113.10, got %s", l.Host)
		}
	}

	// 2. Routing on entry node 'a': should have an outbound to the main server,
	// and the client should be bound to that outbound.
	ra, err := c.routing(ctx, a)
	if err != nil {
		t.Fatalf("routing a: %v", err)
	}
	tag := proxy.ClinkTag(e.ID)
	bound := false
	for _, b := range ra.Bindings {
		if len(b.Users) == 1 && b.Users[0] == user && b.Outbound == tag {
			bound = true
		}
	}
	if !bound {
		t.Fatalf("entry node 'a' does not bind client to main hop: %+v", ra.Bindings)
	}

	// 3. Routing on main server (0): should have an incoming Link with Next == "direct".
	r0, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatalf("routing 0: %v", err)
	}
	foundLink := false
	for _, l := range r0.Links {
		if l.ID == e.ID {
			foundLink = true
			if l.Next != "direct" {
				t.Fatalf("main link next = %q, want 'direct'", l.Next)
			}
		}
	}
	if !foundLink {
		t.Fatalf("main server missing incoming link for edge %d: %+v", e.ID, r0.Links)
	}
}

// TestChainBackupToMainServer verifies that when a chain has only backup entries
// leading into the main server (Selectel --backup--> Main), the main server is
// treated as the direct primary entry and also the exit.
func TestChainBackupToMainServer(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a := ids["a"] // entry agent

	if err := c.SetPublicHost(ctx, "main.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateInbound(ctx, InboundInput{Protocol: "vless", Port: 443, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nodeA, _ := c.Node(ctx, a)
	nodeA.Settings.JoinedAt = &now
	if err := c.saveNode(ctx, nodeA); err != nil {
		t.Fatal(err)
	}

	ch, err := c.CreateChain(ctx, ChainInput{
		Name:    "BackupToMain",
		Edges:   []EdgeInput{{From: a, To: 0}},
		Backups: []int64{a},
	})
	if err != nil {
		t.Fatalf("create chain: %v", err)
	}

	cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}
	user := proxy.Client{ID: cl.ID}.UserName()

	// 1. Client links: should include both the main server (primary) and node 'a' (backup).
	links, err := c.ClientLinks(ctx, &store.ClientRow{ID: cl.ID, UUID: cl.UUID, ChainID: &ch.ID})
	if err != nil {
		t.Fatalf("client links: %v", err)
	}
	if len(links) < 2 {
		t.Fatalf("expected at least 2 links (main + backup), got %d: %+v", len(links), links)
	}
	hasMain := false
	hasBackup := false
	for _, l := range links {
		if l.Host == "main.example.com" {
			hasMain = true
		}
		if l.Host == "203.0.113.10" {
			hasBackup = true
		}
	}
	if !hasMain || !hasBackup {
		t.Fatalf("expected both main and backup hosts, got %+v", links)
	}

	// 2. Routing on main server (0): client is bound locally (traffic leaves directly).
	r0, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatalf("routing 0: %v", err)
	}
	if !boundLocal(r0, user) {
		t.Fatalf("main server missing local binding for client: %+v", r0.Bindings)
	}

	// 3. Routing on node 'a': client is bound to the hop to main server.
	ra, err := c.routing(ctx, a)
	if err != nil {
		t.Fatalf("routing a: %v", err)
	}
	e := chainEdges(t, c, ch.ID)[0]
	tag := proxy.ClinkTag(e.ID)
	bound := false
	for _, b := range ra.Bindings {
		if len(b.Users) == 1 && b.Users[0] == user && b.Outbound == tag {
			bound = true
		}
	}
	if !bound {
		t.Fatalf("entry node 'a' does not bind client to main hop: %+v", ra.Bindings)
	}
}
