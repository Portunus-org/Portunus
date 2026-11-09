package core

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

// TestChainBackupFailover mirrors the production stand: SPB(entry) ->
// HEL(exit) with MOS as a backup source and SIN as a second exit, plus a
// second chain sharing the same backup source. It checks that no server
// ever renders two outbounds with one tag, and that failure of any single
// node leaves a working route for the client.
func TestChainBackupFailover(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	mk := func(name, address string, entry, exit bool) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Name: name, Kind: NodeAgent, Address: address, IsExit: exit, IsEntry: entry, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	spb := mk("SPB", "203.0.113.21", true, false)
	hel := mk("HEL", "203.0.113.22", false, true)
	mos := mk("MOS", "203.0.113.23", true, false)
	sin := mk("SIN", "203.0.113.24", false, true)
	hk := mk("HK", "203.0.113.25", true, false)

	europe, err := c.CreateChain(ctx, ChainInput{
		Name:    "Europe",
		Edges:   []EdgeInput{{From: 0, To: spb}, {From: spb, To: hel}, {From: spb, To: sin}, {From: mos, To: hel}, {From: mos, To: sin}},
		Backups: []int64{mos},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateChain(ctx, ChainInput{
		Name:    "Asia",
		Edges:   []EdgeInput{{From: 0, To: hk}, {From: hk, To: sin}, {From: hk, To: hel}, {From: mos, To: sin}},
		Backups: []int64{mos},
	}); err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "demo", Enabled: true, Chain: &europe.ID})
	if err != nil {
		t.Fatal(err)
	}
	base := proxy.Client{ID: cl.ID}.UserName()
	other := base + "-e" + strconv.FormatInt(sin, 10)

	e0 := edgeBy(t, chainEdges(t, c, europe.ID), 0, spb)
	eSh := edgeBy(t, chainEdges(t, c, europe.ID), spb, hel)
	eMh := edgeBy(t, chainEdges(t, c, europe.ID), mos, hel)
	eMs := edgeBy(t, chainEdges(t, c, europe.ID), mos, sin)

	routing := func(self int64) proxy.Routing {
		t.Helper()
		r, err := c.routing(ctx, self)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	boundTo := func(r proxy.Routing, user, out string) bool {
		for _, bd := range r.Bindings {
			if len(bd.Users) == 1 && bd.Users[0] == user && bd.Outbound == out && !bd.Local {
				return true
			}
		}
		return false
	}
	noDupTags := func(self int64) {
		t.Helper()
		r := routing(self)
		seen := map[string]bool{}
		for _, o := range r.ChainOutbounds {
			m, _ := o.(map[string]any)
			if m == nil {
				continue
			}
			tag, _ := m["tag"].(string)
			if seen[tag] {
				t.Fatalf("node %d: duplicate tag %s", self, tag)
			}
			seen[tag] = true
		}
	}
	setLost := func(id int64, lost bool) {
		c.nodeMu.Lock()
		defer c.nodeMu.Unlock()
		if lost {
			c.chainHealth[id] = exitHealth{since: c.now().Add(-time.Hour), fails: exitFailures, told: true}
		} else {
			delete(c.chainHealth, id)
		}
	}

	for _, id := range []int64{0, spb, hel, mos, sin, hk} {
		noDupTags(id)
	}

	// Healthy: the client goes to the primary entry; both credentials ride
	// the same first edge.
	if r := routing(0); !boundTo(r, base, proxy.ClinkTag(e0.ID)) || !boundTo(r, other, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("healthy main: %+v", r.Bindings)
	}

	// The primary way in fails: the main server has no live edge, but the
	// backup source serves both exits — the client stays on the chain.
	setLost(spb, true)
	if r := routing(0); boundTo(r, base, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("entry down, main still on the dead edge: %+v", r.Bindings)
	}
	r := routing(mos)
	if !boundTo(r, base, proxy.ClinkTag(eMh.ID)) || !boundTo(r, other, proxy.ClinkTag(eMs.ID)) {
		t.Fatalf("entry down, MOS: %+v", r.Bindings)
	}
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != europe.ID || rows[0].ExitID != nil {
		t.Fatalf("moved off a chain with a working backup entry: %+v", rows[0])
	}

	// The exit fails too: the credential aimed at it rides the urltest
	// group, which switches it over to the surviving branch.
	setLost(hel, true)
	r = routing(mos)
	auto := autoTag(europe.ID, mos)
	if !boundTo(r, base, auto) || !boundTo(r, other, proxy.ClinkTag(eMs.ID)) {
		t.Fatalf("both down, MOS: %+v", r.Bindings)
	}

	// Everything returns: the primary route is taken again.
	setLost(spb, false)
	setLost(hel, false)
	if r := routing(0); !boundTo(r, base, proxy.ClinkTag(e0.ID)) || !boundTo(r, other, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("recovered main: %+v", r.Bindings)
	}
	for _, id := range []int64{0, spb, hel, mos, sin, hk} {
		noDupTags(id)
	}
	_ = eSh
}
