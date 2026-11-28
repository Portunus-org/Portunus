package core

import (
	"context"
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

	e0 := edgeBy(t, chainEdges(t, c, europe.ID), 0, spb)
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

	// Healthy: client goes to the primary entry under one credential; SPB splits
	// across HEL and SIN via its urltest group.
	if r := routing(0); !boundTo(r, base, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("healthy main: %+v", r.Bindings)
	}
	if r := routing(spb); len(r.Links) == 0 || r.Links[0].Next != autoTag(europe.ID, spb) {
		t.Fatalf("healthy spb hop next = %+v, want %s", r.Links, autoTag(europe.ID, spb))
	}

	// The primary way in fails: the main server has no live edge, while MOS
	// reaches both exits via its urltest group.
	setLost(spb, true)
	if r := routing(0); boundTo(r, base, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("entry down, main still on the dead edge: %+v", r.Bindings)
	}
	r := routing(mos)
	if !boundTo(r, base, autoTag(europe.ID, mos)) {
		t.Fatalf("entry down, MOS: %+v, want %s", r.Bindings, autoTag(europe.ID, mos))
	}
	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != europe.ID || rows[0].ExitID != nil {
		t.Fatalf("moved off a chain with a working backup entry: %+v", rows[0])
	}

	// One exit fails too: MOS switches the client directly to the surviving exit SIN.
	setLost(hel, true)
	r = routing(mos)
	if !boundTo(r, base, proxy.ClinkTag(eMs.ID)) {
		t.Fatalf("both down, MOS: %+v, want %s", r.Bindings, proxy.ClinkTag(eMs.ID))
	}

	// Everything returns: the primary route is taken again.
	setLost(spb, false)
	setLost(hel, false)
	if r := routing(0); !boundTo(r, base, proxy.ClinkTag(e0.ID)) {
		t.Fatalf("recovered main: %+v", r.Bindings)
	}
	for _, id := range []int64{0, spb, hel, mos, sin, hk} {
		noDupTags(id)
	}
}

// TestMainTwoExitFailover verifies Alexander's topology: Main(0) connected
// directly to two exit nodes (Old VPN and Stockholm) inside one chain. The
// client receives ONE link for the chain, and sing-box on Main routes it
// through the chain's urltest group (server-side automatic failover). When
// Old VPN fails checks, traffic switches to Stockholm, then restores urltest.
func TestMainTwoExitFailover(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if err := c.SetPublicHost(ctx, "main.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateInbound(ctx, InboundInput{Protocol: "vless", Port: 14443, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mkExit := func(name, address string) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Name: name, Kind: NodeAgent, Address: address, IsExit: true, IsEntry: false, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	oldVPN := mkExit("Old VPN", "203.0.113.11")
	stockholm := mkExit("Stockholm", "203.0.113.12")

	ch, err := c.CreateChain(ctx, ChainInput{
		Name:    "Europe",
		Default: &ChainDefaultInput{Action: proxy.RouteAuto},
		Edges:   []EdgeInput{{From: 0, To: oldVPN}, {From: 0, To: stockholm}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "alex", Enabled: true, Chain: &ch.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Exactly ONE client link for the whole two-exit chain.
	links, err := c.ClientLinks(ctx, cl)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].UUID != cl.UUID || links[0].Label != "Main → Europe" {
		t.Fatalf("expected 1 unified link 'Main → Europe', got %+v", links)
	}

	edges := chainEdges(t, c, ch.ID)
	eSthlm := edgeBy(t, edges, 0, stockholm)
	base := proxy.Client{ID: cl.ID}.UserName()
	auto := autoTag(ch.ID, 0)

	boundTo := func(r proxy.Routing, user, out string) bool {
		for _, bd := range r.Bindings {
			if len(bd.Users) == 1 && bd.Users[0] == user && bd.Outbound == out {
				return true
			}
		}
		return false
	}

	// Healthy: single credential routes via server-side urltest group across both exits.
	r, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundTo(r, base, auto) {
		t.Fatalf("healthy bindings = %+v, want %s->%s", r.Bindings, base, auto)
	}

	// Old VPN drops past exitGrace: client link stays unchanged, server routes strictly via Stockholm.
	c.nodeMu.Lock()
	c.chainHealth[oldVPN] = exitHealth{since: c.now().Add(-time.Hour), fails: exitFailures, told: true}
	c.nodeMu.Unlock()

	if err := c.bindClients(ctx); err != nil {
		t.Fatal(err)
	}
	rows, _ := c.store.ListClients(ctx)
	if rows[0].ChainID == nil || *rows[0].ChainID != ch.ID || rows[0].ExitID != nil {
		t.Fatalf("client moved off working 2-exit chain: %+v", rows[0])
	}

	r, err = c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundTo(r, base, proxy.ClinkTag(eSthlm.ID)) {
		t.Fatalf("failover bindings = %+v, want %s->%s", r.Bindings, base, proxy.ClinkTag(eSthlm.ID))
	}
	inb, err := c.Inbounds(ctx)
	if err != nil || len(inb) == 0 {
		t.Fatal(err)
	}
	cfg, err := proxy.ServerConfig(inb, []proxy.Client{toProxyClient(cl)}, "", proxy.ACMEOptions{}, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg) == 0 {
		t.Fatal("empty config")
	}

	// Old VPN recovers: server returns to urltest balancing across both exits.
	c.nodeMu.Lock()
	delete(c.chainHealth, oldVPN)
	c.nodeMu.Unlock()
	r, err = c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !boundTo(r, base, auto) {
		t.Fatalf("recovered bindings = %+v, want %s->%s", r.Bindings, base, auto)
	}
}

// TestClientMultiChain verifies that one client can be assigned multiple chains
// (receiving one link per chain) with independent credentials and routing.
func TestClientMultiChain(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if err := c.SetPublicHost(ctx, "main.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateInbound(ctx, InboundInput{Protocol: "vless", Port: 14443, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	mkExit := func(name, address string) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Name: name, Kind: NodeAgent, Address: address, IsExit: true, IsEntry: false, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	ex1 := mkExit("Old VPN", "203.0.113.11")
	ex2 := mkExit("Stockholm", "203.0.113.12")
	exUS := mkExit("New York", "203.0.113.13")

	eu, err := c.CreateChain(ctx, ChainInput{
		Name:  "Europe",
		Edges: []EdgeInput{{From: 0, To: ex1}, {From: 0, To: ex2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	us, err := c.CreateChain(ctx, ChainInput{
		Name:  "US Route",
		Edges: []EdgeInput{{From: 0, To: exUS}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "alex", Enabled: true, Chains: &[]int64{eu.ID, us.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cl.Chains()) != 2 || cl.Chains()[0] != eu.ID || cl.Chains()[1] != us.ID {
		t.Fatalf("client chains = %v, want [%d %d]", cl.Chains(), eu.ID, us.ID)
	}

	links, err := c.ClientLinks(ctx, cl)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 2 {
		t.Fatalf("expected 2 links (1 per chain), got %d: %+v", len(links), links)
	}
	if links[0].Label != "Main → Europe" || links[0].UUID != cl.UUID {
		t.Fatalf("first link = %+v", links[0])
	}
	if links[1].Label != "Main → US Route" || links[1].UUID == cl.UUID {
		t.Fatalf("second link = %+v", links[1])
	}

	r, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	eUS := edgeBy(t, chainEdges(t, c, us.ID), 0, exUS)
	euUser := chainUserName(cl.ID, cl.Chains(), eu.ID)
	usUser := chainUserName(cl.ID, cl.Chains(), us.ID)
	bound := map[string]string{}
	for _, bd := range r.Bindings {
		for _, u := range bd.Users {
			bound[u] = bd.Outbound
		}
	}
	if bound[euUser] != autoTag(eu.ID, 0) {
		t.Fatalf("EU user %s -> %q, want %s", euUser, bound[euUser], autoTag(eu.ID, 0))
	}
	if bound[usUser] != proxy.ClinkTag(eUS.ID) {
		t.Fatalf("US user %s -> %q, want %s", usUser, bound[usUser], proxy.ClinkTag(eUS.ID))
	}

	// Updating client to clear chains moves it cleanly to auto exit.
	empty := []int64{}
	updated, err := c.UpdateClient(ctx, cl.ID, ClientInput{Name: cl.Name, Enabled: true, Chains: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Chains()) != 0 || updated.ChainID != nil {
		t.Fatalf("expected cleared chains, got %+v", updated)
	}
}
