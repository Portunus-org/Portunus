package core

import (
	"context"
	"testing"
)

// The subscription path is one plain segment, not one the panel or its cover
// site answers itself, and never the secret word of the gate.
func TestSubscriptionPath(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if p := c.SubscriptionPath(ctx); p != DefaultSubscriptionPath {
		t.Fatalf("default: %q", p)
	}
	if err := c.store.SetSetting(ctx, settingPanelGate, "lantern"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"My Sub", "a/b", "sub path", "-sub", "sub-", "api", "login", "log", "lantern", "Lantern", "ж", "x" + string(make([]byte, 40))} {
		if err := c.SetSubscriptionPath(ctx, bad); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	for _, good := range []string{"feed", "/My-Feed-2/", "s"} {
		if err := c.SetSubscriptionPath(ctx, good); err != nil {
			t.Fatalf("%q refused: %v", good, err)
		}
	}
	if p := c.SubscriptionPath(ctx); p != "s" {
		t.Fatalf("stored: %q", p)
	}
	if err := c.SetSubscriptionPath(ctx, ""); err != nil || c.SubscriptionPath(ctx) != DefaultSubscriptionPath {
		t.Fatalf("empty goes back to the default: %v %q", err, c.SubscriptionPath(ctx))
	}
	// A new gate is never the subscription path.
	if err := c.SetSubscriptionPath(ctx, "feed"); err != nil {
		t.Fatal(err)
	}
	for range 2000 {
		if c.gateFor(ctx) == "feed" {
			t.Fatal("a gate the same as the subscription path")
		}
	}
}

// Where subscription links point: the operator's own address, else the
// panel's domain with the path, else the port by the IP address.
func TestSubscriptionAddress(t *testing.T) {
	c, _ := newBindingCore(t)
	ctx := context.Background()
	if b := c.EffectiveSubscriptionBase(ctx); b != "" {
		t.Fatalf("nothing set: %q", b)
	}
	if err := c.SetPublicHost(ctx, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	if err := c.store.SetSetting(ctx, settingSubPort, "2096"); err != nil {
		t.Fatal(err)
	}
	if b := c.EffectiveSubscriptionBase(ctx); b != "http://203.0.113.5:2096/sub" {
		t.Fatalf("by the IP address: %q", b)
	}
	// The panel opened by its domain: the subscriptions go with it.
	if err := c.SetDomain(ctx, "vpn.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := c.store.SetSetting(ctx, settingPanelPort, "443"); err != nil {
		t.Fatal(err)
	}
	if b := c.EffectiveSubscriptionBase(ctx); b != "https://vpn.example.com/sub" {
		t.Fatalf("by the domain: %q", b)
	}
	if err := c.SetSubscriptionPath(ctx, "my-feed"); err != nil {
		t.Fatal(err)
	}
	if b := c.EffectiveSubscriptionBase(ctx); b != "https://vpn.example.com/my-feed" {
		t.Fatalf("own path: %q", b)
	}
	if err := c.store.SetSetting(ctx, settingPanelPort, "8443"); err != nil {
		t.Fatal(err)
	}
	if b := c.EffectiveSubscriptionBase(ctx); b != "https://vpn.example.com:8443/my-feed" {
		t.Fatalf("panel on another port: %q", b)
	}
	// The operator's reverse proxy wins.
	if err := c.SetSubscriptionBaseURL(ctx, "https://sub.example.org/"); err != nil {
		t.Fatal(err)
	}
	if b := c.EffectiveSubscriptionBase(ctx); b != "https://sub.example.org/sub" {
		t.Fatalf("own address: %q", b)
	}
}
