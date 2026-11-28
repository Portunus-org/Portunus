package core

import (
	"context"
	"errors"
	"testing"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

// newScopedRuleCore builds a 0→a→b chain (a is an entry point, b an exit),
// optionally with an active client on it, plus a manual list and a rule
// scoped to the chain. It returns the core, the chain id, the list id, the
// hop node, the sink node and (when there is a client) its sing-box user
// name.
func newScopedRuleCore(t *testing.T, withClient bool) (*Core, int64, int64, int64, int64, string) {
	t.Helper()
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b := ids["a"], ids["b"]

	ch, err := c.CreateChain(ctx, ChainInput{Name: "ViaMain", Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}}})
	if err != nil {
		t.Fatal(err)
	}
	var user string
	if withClient {
		cl, err := c.CreateClient(ctx, ClientInput{Name: "c1", Enabled: true, Chain: &ch.ID})
		if err != nil {
			t.Fatal(err)
		}
		user = proxy.Client{ID: cl.ID}.UserName()
	}
	list, err := c.CreateRouteList(ctx, RouteListInput{Name: "ads", Kind: ListManual, Content: "ad.example\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateRouteRule(ctx, RouteRuleInput{ListID: list.ID, Action: proxy.RouteDirect, ChainID: ch.ID, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	return c, ch.ID, list.ID, a, b, user
}

func scopedRule(t *testing.T, r proxy.Routing, listID int64) (proxy.RouteRule, bool) {
	t.Helper()
	for _, rule := range r.Rules {
		if rule.List.ID == listID {
			return rule, true
		}
	}
	return proxy.RouteRule{}, false
}

// A chain-scoped rule is served at the chain's entry points only, and then
// only for that chain's clients: nobody else is touched by it.
func TestRouteRuleChainScope(t *testing.T) {
	c, _, listID, a, b, user := newScopedRuleCore(t, true)
	ctx := context.Background()

	r0, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := scopedRule(t, r0, listID)
	if !ok {
		t.Fatalf("scoped rule missing at the entry: %+v", r0.Rules)
	}
	if len(rule.Users) != 1 || rule.Users[0] != user {
		t.Fatalf("users at the entry: %v, want [%s]", rule.Users, user)
	}

	// a is an entry point the chain passes through: a client may come in
	// there and travel on, so the rule is served at a as well.
	ra, err := c.routing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	ruleA, ok := scopedRule(t, ra, listID)
	if !ok {
		t.Fatalf("scoped rule missing at the mid-chain entry: %+v", ra.Rules)
	}
	if len(ruleA.Users) != 1 || ruleA.Users[0] != user {
		t.Fatalf("users at the mid-chain entry: %v, want [%s]", ruleA.Users, user)
	}

	// b is a sink and not an entry point: the rule does not reach it.
	rb, err := c.routing(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := scopedRule(t, rb, listID); ok {
		t.Fatalf("scoped rule reached the sink: %+v", rb.Rules)
	}
}

// With no client riding the chain there is nobody for the rule to serve.
func TestRouteRuleChainScopeNoClients(t *testing.T) {
	c, _, listID, _, _, _ := newScopedRuleCore(t, false)
	ctx := context.Background()

	r0, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rule, ok := scopedRule(t, r0, listID); ok {
		t.Fatalf("scoped rule without clients: %+v (users %v)", r0.Rules, rule.Users)
	}
}

// Scoping a rule to a chain that does not exist is refused.
func TestRouteRuleBadChain(t *testing.T) {
	c, _ := newChainCore(t)
	ctx := context.Background()
	list, err := c.CreateRouteList(ctx, RouteListInput{Name: "ads", Kind: ListManual, Content: "ad.example\n"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.CreateRouteRule(ctx, RouteRuleInput{ListID: list.ID, Action: proxy.RouteDirect, ChainID: 999, Enabled: true})
	var v *ValidationError
	if !errors.As(err, &v) || v.Field+":"+v.Code != "chain:required" {
		t.Fatalf("bad chain: %v", err)
	}
}

// Per-chain default routes override the global default per chain, protect
// referenced exit nodes from deletion, and reach entry points only.
func TestChainDefaultRouteCore(t *testing.T) {
	c, ids := newChainCore(t)
	ctx := context.Background()
	a, b, cn := ids["a"], ids["b"], ids["c"]

	ch1, err := c.CreateChain(ctx, ChainInput{
		Name:    "Split",
		Edges:   []EdgeInput{{From: 0, To: a}, {From: a, To: b}},
		Default: &ChainDefaultInput{Action: proxy.RouteDirect},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ch1.DefaultAction != proxy.RouteDirect || ch1.DefaultNodeID != nil {
		t.Fatalf("ch1 default = %q %v", ch1.DefaultAction, ch1.DefaultNodeID)
	}

	ch2, err := c.CreateChain(ctx, ChainInput{
		Name:    "FullVPN",
		Edges:   []EdgeInput{{From: 0, To: cn}, {From: cn, To: b}},
		Default: &ChainDefaultInput{Action: proxy.RouteAuto},
	})
	if err != nil {
		t.Fatal(err)
	}
	cl1, err := c.CreateClient(ctx, ClientInput{Name: "u1", Enabled: true, Chain: &ch1.ID})
	if err != nil {
		t.Fatal(err)
	}
	cl2, err := c.CreateClient(ctx, ClientInput{Name: "u2", Enabled: true, Chain: &ch2.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Updating ch1 without Default preserves its existing override.
	updated, err := c.UpdateChain(ctx, ch1.ID, ChainInput{
		Name:  "Split2",
		Edges: []EdgeInput{{From: 0, To: a}, {From: a, To: b}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DefaultAction != proxy.RouteDirect {
		t.Fatalf("DefaultAction lost on update without Default: %q", updated.DefaultAction)
	}

	// Routing on main server (entry) includes UserDefaults for both chains.
	r0, err := c.routing(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r0.UserDefaults) != 2 {
		t.Fatalf("UserDefaults on main: %+v", r0.UserDefaults)
	}
	u1 := proxy.Client{ID: cl1.ID}.UserName()
	u2 := proxy.Client{ID: cl2.ID}.UserName()
	gotByUser := map[string]string{}
	for _, ud := range r0.UserDefaults {
		if len(ud.Users) != 1 {
			t.Fatalf("unexpected users: %+v", ud)
		}
		gotByUser[ud.Users[0]] = ud.Action
	}
	if gotByUser[u1] != proxy.RouteDirect || gotByUser[u2] != proxy.RouteAuto {
		t.Fatalf("UserDefaults actions: %v", gotByUser)
	}

	// Routing on non-entry sink b has no UserDefaults.
	rb, err := c.routing(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.UserDefaults) != 0 {
		t.Fatalf("UserDefaults reached non-entry sink: %+v", rb.UserDefaults)
	}

	// Pointing ch1 default to exit node b protects b from DeleteNode/disabling exit.
	if err := c.SetChainDefaultRoute(ctx, ch1.ID, proxy.RouteNode, &b); err != nil {
		t.Fatal(err)
	}
	// Make a standalone chain without b to test exitInUse specifically for default_node_id.
	ch3, err := c.CreateChain(ctx, ChainInput{
		Name:    "RefExit",
		Edges:   []EdgeInput{{From: 0, To: a}, {From: a, To: cn}},
		Default: &ChainDefaultInput{Action: proxy.RouteNode, NodeID: &b},
	})
	if err != nil {
		t.Fatal(err)
	}
	if used, err := c.exitInUse(ctx, b); err != nil || !used {
		t.Fatalf("exitInUse for b = %v, %v", used, err)
	}
	// Clear ch3 default and verify clearing works.
	if err := c.SetChainDefaultRoute(ctx, ch3.ID, "", nil); err != nil {
		t.Fatal(err)
	}
	got3, err := c.Chain(ctx, ch3.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got3.DefaultAction != "" || got3.DefaultNodeID != nil {
		t.Fatalf("cleared default still set: %+v", got3)
	}
}
