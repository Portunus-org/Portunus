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
