package core

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/PaulichP/Portunus/server/internal/db"
	"github.com/PaulichP/Portunus/server/internal/secrets"
	"github.com/PaulichP/Portunus/server/internal/singbox"
	"github.com/PaulichP/Portunus/server/internal/store"
)

func newBindingCore(t *testing.T) (*Core, *time.Time) {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	sqlDB, err := db.Open(ctx, filepath.Join(dir, "portunus.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	box, _, err := secrets.LoadOrCreate(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	// sing-box is absent: applying fails and is only reported, the bindings
	// are made all the same.
	mgr := singbox.New("/nonexistent/sing-box", filepath.Join(dir, "sing-box"), log)
	c := New(store.New(sqlDB), box, mgr, Options{StatsAPI: "127.0.0.1:1"}, log)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	return c, &now
}

func TestClientExitBinding(t *testing.T) {
	c, now := newBindingCore(t)
	ctx := context.Background()
	exit := func(name string) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@" + name + ".example.com:443#" + name})
		if err != nil {
			t.Fatalf("exit %s: %v", name, err)
		}
		return n.ID
	}
	bound := func() map[string]int64 {
		t.Helper()
		rows, err := c.Clients(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, r := range rows {
			if r.ExitID != nil {
				out[r.Name] = *r.ExitID
			}
		}
		return out
	}
	client := func(name string) int64 {
		cl, err := c.CreateClient(ctx, ClientInput{Name: name, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		return cl.ID
	}

	// Without exits nobody is bound.
	first := client("c1")
	if got := bound(); len(got) != 0 {
		t.Fatalf("bound without exits: %v", got)
	}
	a, b := exit("a"), exit("b")
	client("c2")
	client("c3")
	client("c4")
	// Spread evenly, and nothing moves when more clients arrive.
	before := bound()
	count := map[int64]int{}
	for _, id := range before {
		count[id]++
	}
	if len(before) != 4 || count[a] != 2 || count[b] != 2 {
		t.Fatalf("not spread evenly: %v", before)
	}
	client("c5")
	client("c6")
	after := bound()
	for name, id := range before {
		if after[name] != id {
			t.Fatalf("%s moved from %d to %d", name, id, after[name])
		}
	}

	// A lost check or a short outage moves nobody.
	if c.noteExit(a, false) {
		t.Fatal("given up on after one failed check")
	}
	*now = now.Add(time.Minute)
	c.noteExit(a, false)
	c.Reconcile(ctx)
	if got := bound(); got["c1"] != after["c1"] || got["c2"] != after["c2"] {
		t.Fatalf("moved during a short outage: %v", got)
	}
	// It answers again: the count starts over.
	c.noteExit(a, true)
	c.noteExit(a, false)
	*now = now.Add(exitGrace - time.Second)
	if c.noteExit(a, false) || c.noteExit(a, false) {
		t.Fatal("given up on before the grace time")
	}
	// New clients avoid an exit that is failing.
	client("c7")
	if got := bound(); got["c7"] != b {
		t.Fatalf("new client bound to a failing exit: %v", got)
	}
	// Down for long enough: its clients move, the others stay.
	*now = now.Add(2 * time.Second)
	if !c.noteExit(a, false) {
		t.Fatal("not given up on after the grace time")
	}
	if c.noteExit(a, false) {
		t.Fatal("given up on twice")
	}
	c.Reconcile(ctx)
	moved := bound()
	for name, id := range moved {
		if id != b {
			t.Fatalf("%s still on the lost exit: %v", name, moved)
		}
	}
	// It comes back: nobody returns by themselves, new clients go there.
	c.noteExit(a, true)
	c.Reconcile(ctx)
	if got := bound(); got["c1"] != b || got["c2"] != b {
		t.Fatalf("moved back: %v", got)
	}
	client("c8")
	if got := bound(); got["c8"] != a {
		t.Fatalf("new client not on the freshest exit: %v", got)
	}

	// The operator's choice, and "choose anew".
	cl, err := c.UpdateClient(ctx, first, ClientInput{Name: "c1", Enabled: true, Exit: &a})
	if err != nil || cl.ExitID == nil || *cl.ExitID != a {
		t.Fatalf("pick an exit: %v %v", cl, err)
	}
	// An ordinary edit keeps it.
	if cl, err = c.UpdateClient(ctx, first, ClientInput{Name: "renamed", Enabled: true}); err != nil || *cl.ExitID != a {
		t.Fatalf("edit keeps the exit: %v %v", cl, err)
	}
	unknown := int64(999)
	if _, err := c.UpdateClient(ctx, first, ClientInput{Name: "renamed", Enabled: true, Exit: &unknown}); err == nil {
		t.Fatal("bound to an exit that does not exist")
	}
	// New keys: the exit is chosen anew, and there still is one.
	if cl, err = c.RotateClient(ctx, first); err != nil || cl.ExitID == nil {
		t.Fatalf("exit after new keys: %v %v", cl, err)
	}
	// With every exit down nobody is moved around.
	for range exitFailures {
		c.noteExit(a, false)
		c.noteExit(b, false)
	}
	*now = now.Add(exitGrace + time.Second)
	c.noteExit(a, false)
	c.noteExit(b, false)
	was := bound()
	c.Reconcile(ctx)
	for name, id := range bound() {
		if was[name] != id {
			t.Fatalf("%s moved while every exit is down", name)
		}
	}
	c.noteExit(a, true)
	c.noteExit(b, true)

	// An exit that is removed hands its clients over.
	if err := c.DeleteNode(ctx, a); err != nil {
		t.Fatal(err)
	}
	for name, id := range bound() {
		if id != b {
			t.Fatalf("%s left on a removed exit", name)
		}
	}
	if got := bound(); len(got) != 8 {
		t.Fatalf("clients lost their exit: %v", got)
	}
}

// By default a client gets the fastest exit; exits that are about equally
// fast share the clients.
func TestFastestExitByDefault(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	exit := func(name string, ms int) int64 {
		n, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@" + name + ".example.com:443#" + name})
		if err != nil {
			t.Fatal(err)
		}
		c.setNodeStatus(n.ID, func(st *NodeStatus) { st.LatencyMs = &ms })
		return n.ID
	}
	on := func(name string) int64 {
		t.Helper()
		cl, err := c.CreateClient(ctx, ClientInput{Name: name, Enabled: true})
		if err != nil || cl.ExitID == nil {
			t.Fatalf("%s: %v %v", name, cl, err)
		}
		return *cl.ExitID
	}
	slow, fast := exit("slow", 240), exit("fast", 60)
	for _, name := range []string{"c1", "c2", "c3"} {
		if got := on(name); got != fast {
			t.Fatalf("%s bound to %d, the fastest is %d", name, got, fast)
		}
	}
	// A third exit about as fast as the fastest takes the next clients,
	// until they are even.
	near := exit("near", 95)
	for _, name := range []string{"c4", "c5", "c6"} {
		if got := on(name); got != near {
			t.Fatalf("%s bound to %d, expected the emptier of the fast exits", name, got)
		}
	}
	// One not measured yet is not preferred to measured ones…
	n, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@new.example.com:443#new"})
	if err != nil {
		t.Fatal(err)
	}
	if got := on("c7"); got == n.ID || got == slow {
		t.Fatalf("c7 bound to %d", got)
	}
	// …and nobody already bound moved through all this.
	rows, _ := c.Clients(ctx)
	for _, r := range rows[:3] {
		if *r.ExitID != fast {
			t.Fatalf("%s moved to %d", r.Name, *r.ExitID)
		}
	}
	// The operator's choice wins over speed.
	if cl, err := c.CreateClient(ctx, ClientInput{Name: "own", Enabled: true, Exit: &slow}); err != nil || *cl.ExitID != slow {
		t.Fatalf("chosen exit: %v %v", cl, err)
	}
}

// A standby node that becomes the main server takes over what clients know
// it by, and routes through it become direct.
func TestPromoteNode(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if err := c.SetPublicHost(ctx, "old.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetSubscriptionBaseURL(ctx, "https://old.example.com:2096"); err != nil {
		t.Fatal(err)
	}
	in, err := c.CreateInbound(ctx, InboundInput{Protocol: "hysteria2", Port: 30443, Enabled: true, ServerName: "old.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.CreateNode(ctx, NodeInput{Kind: NodeAgent, Name: "Backup", Address: "203.0.113.7", IsExit: true, IsEntry: true, Standby: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.CreateNode(ctx, NodeInput{Kind: NodeLink, Enabled: true, Link: "trojan://secret@x.example.com:443#x"})
	if err != nil {
		t.Fatal(err)
	}
	// The certificate it serves as a backup entry: what clients have.
	ins, _ := c.Inbounds(ctx)
	entry, err := c.entryInbounds(ctx, &n, ins)
	if err != nil || len(entry) != 1 {
		t.Fatalf("entry inbounds: %v %v", entry, err)
	}
	known := entry[0].Settings.CertSHA256

	list, err := c.CreateRouteList(ctx, RouteListInput{Kind: "manual", Name: "L", Content: "a.example"})
	if err != nil {
		t.Fatal(err)
	}
	id := n.ID
	if _, err := c.CreateRouteRule(ctx, RouteRuleInput{ListID: list.ID, Action: "node", NodeID: &id, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetDefaultRoute(ctx, "node", &id); err != nil {
		t.Fatal(err)
	}
	cl, err := c.CreateClient(ctx, ClientInput{Name: "c", Enabled: true, Exit: &id})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.PromoteNode(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if host, _ := c.PublicHost(ctx); host != "203.0.113.7" {
		t.Fatalf("public host %q", host)
	}
	if base, _ := c.SubscriptionBaseURL(ctx); base != "https://203.0.113.7:2096" {
		t.Fatalf("subscription address %q", base)
	}
	got, err := c.Inbound(ctx, in.ID)
	if err != nil || got.Settings.ServerName != "203.0.113.7" || got.Settings.CertSHA256 != known {
		t.Fatalf("inbound did not take over the entry's certificate: %v %v", got.Settings.ServerName, err)
	}
	rules, _ := c.RouteRules(ctx)
	if len(rules) != 1 || rules[0].Action != "direct" || rules[0].NodeID != nil {
		t.Fatalf("rule through the promoted server: %+v", rules)
	}
	if act, _ := c.DefaultRoute(ctx); act != "direct" {
		t.Fatalf("default route %q", act)
	}
	nodes, _ := c.Nodes(ctx)
	if len(nodes) != 1 || nodes[0].ID != other.ID {
		t.Fatalf("nodes after promotion: %v", nodes)
	}
	// Its client finds another exit at the first start.
	c.Reconcile(ctx)
	if row, _ := c.Client(ctx, cl.ID); row.ExitID == nil || *row.ExitID != other.ID {
		t.Fatalf("client not bound anew: %v", row.ExitID)
	}
}
