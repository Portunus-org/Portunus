package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseList(t *testing.T) {
	l, skipped := ParseList(7, `
# comment
Example.com
*.wild.example   # trailing comment
.ua
full:exact.example
keyword:video
203.0.113.7
2001:db8::/32
10.1.2.3/8
not a domain
http://url.example/path
`)
	if skipped != 2 {
		t.Fatalf("skipped %d", skipped)
	}
	if got := strings.Join(l.Suffixes, ","); got != "example.com,wild.example,.ua" {
		t.Fatalf("suffixes: %s", got)
	}
	if got := strings.Join(l.CIDRs, ","); got != "203.0.113.7/32,2001:db8::/32,10.0.0.0/8" {
		t.Fatalf("cidrs: %s", got)
	}
	if len(l.Domains) != 1 || len(l.Keywords) != 1 || l.Empty() {
		t.Fatalf("list: %+v", l)
	}
}

func testRouting(t *testing.T) Routing {
	t.Helper()
	hop, err := NewHop(443, "")
	if err != nil {
		t.Fatal(err)
	}
	blocked, _ := ParseList(1, "blocked.example\n.onion")
	local, _ := ParseList(2, "bank.example\n198.51.100.0/24")
	ads, _ := ParseList(3, "keyword:adserver")
	return Routing{
		Exits: []Exit{{ID: 5, Outbound: hop.Outbound("203.0.113.20")}},
		Rules: []RouteRule{
			{List: local, Action: RouteDirect},
			{List: ads, Action: RouteBlock},
			{List: blocked, Action: RouteNode, ExitID: 5},
		},
		Default: RouteDirect,
	}
}

func routeRules(t *testing.T, cfg []byte) (rules []map[string]any, final string) {
	t.Helper()
	var parsed struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Route.Rules, parsed.Route.Final
}

func TestRoutingConfig(t *testing.T) {
	r := testRouting(t)
	r.ControlAPI, r.ControlSecret = "127.0.0.1:2055", "s3cret"
	cfg, err := ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, r)
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	rules, final := routeRules(t, cfg)
	if final != "direct" {
		t.Fatalf("final: %s", final)
	}
	// The guard against private addresses must sit before anything is
	// allowed out directly.
	guard := -1
	for i, r := range rules {
		if r["ip_is_private"] == true && r["action"] == "reject" {
			guard = i
		}
		if r["outbound"] == "direct" && guard == -1 {
			t.Fatalf("direct route before the private-address guard: %v", rules)
		}
	}
	if guard == -1 || rules[guard-1]["action"] != "resolve" {
		t.Fatalf("no resolve + guard: %v", rules)
	}
	if !strings.Contains(string(cfg), `"clash_api"`) {
		t.Fatal("control API missing")
	}

	// Everything else through the exit: nothing but the direct list is
	// resolved here.
	r.Default, r.DefaultExit = RouteNode, 5
	r.Rules = r.Rules[1:] // drop the rule with addresses
	r.Rules = append([]RouteRule{{List: mustList(2, "bank.example"), Action: RouteDirect}}, r.Rules...)
	cfg, err = ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, r)
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	rules, _ = routeRules(t, cfg)
	catchAll := -1
	for i, r := range rules {
		if r["invert"] == true && r["outbound"] == ExitTag(5) {
			catchAll = i
		}
		if r["action"] == "resolve" && catchAll == -1 {
			t.Fatalf("resolve before the catch-all to the exit: %v", rules)
		}
	}
	if catchAll == -1 {
		t.Fatalf("no catch-all: %v", rules)
	}
}

func mustList(id int64, text string) RouteList {
	l, _ := ParseList(id, text)
	return l
}

// A rule or default pointing at an exit that is gone must refuse traffic,
// not let it out directly.
func TestRoutingFailsClosed(t *testing.T) {
	r := Routing{
		Rules:   []RouteRule{{List: mustList(1, "blocked.example"), Action: RouteNode, ExitID: 9}},
		Default: RouteAuto,
	}
	cfg, err := ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, r)
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	rules, _ := routeRules(t, cfg)
	for _, r := range rules {
		if r["action"] == "route" {
			t.Fatalf("traffic routed although no exit exists: %v", r)
		}
	}
	if last := rules[len(rules)-1]; last["action"] != "reject" && rules[1]["action"] != "reject" {
		t.Fatalf("no reject: %v", rules)
	}
}

func TestNodeConfig(t *testing.T) {
	hop, err := NewHop(8443, "www.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// Exit only.
	cfg, err := NodeConfig(&hop, nil)
	if err != nil || cfg == nil {
		t.Fatalf("exit config: %v", err)
	}
	singBoxCheck(t, cfg)
	if strings.Contains(string(cfg), "v2ray_api") {
		t.Fatal("an exit keeps no client statistics")
	}
	// Exit and backup entry.
	cfg, err = NodeConfig(&hop, &EntryConfig{Inbounds: testInbounds(t), Clients: testClients, StatsAPI: "127.0.0.1:2054", Routing: testRouting(t)})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	// What other servers send over the hop leaves at once, whatever the
	// rules for this server's own clients say (here: refuse everybody, as
	// nobody is bound to an exit).
	cfg, err = NodeConfig(&hop, &EntryConfig{Inbounds: testInbounds(t), Clients: testClients, StatsAPI: "127.0.0.1:2054", Routing: Routing{Default: RouteAuto}})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	rules, _ := routeRules(t, cfg)
	for _, r := range rules {
		in, _ := r["inbound"].([]any)
		if len(in) == 1 && in[0] == "hop" && r["action"] == "route" && r["outbound"] == "direct" {
			break
		}
		if r["action"] == "reject" && len(in) == 0 {
			t.Fatalf("hop traffic meets the clients' rules: %v", rules)
		}
	}
	// Nothing to run.
	if cfg, _ := NodeConfig(nil, nil); cfg != nil {
		t.Fatal("expected no config")
	}
	// The panel's outbound to the hop.
	main, err := ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, Routing{Exits: []Exit{{ID: 1, Outbound: hop.Outbound("2001:db8::5")}}, Default: RouteAuto})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, main)
}

// Our own share links must import back as working outbounds.
func TestParseShareLink(t *testing.T) {
	var exits []Exit
	for i, in := range testInbounds(t) {
		link := ShareLink("203.0.113.10", in, testClients[0])
		imp, err := ParseShareLink(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if imp.Address != "203.0.113.10" || imp.Port != in.Port || imp.Protocol != string(in.Protocol) {
			t.Fatalf("%s: %+v", link, imp)
		}
		exits = append(exits, Exit{ID: int64(i + 1), Outbound: imp.Outbound})
	}
	cfg, err := ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, Routing{Exits: exits, Default: RouteAuto})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)

	for _, link := range []string{
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@198.51.100.4:8388#Old%20style",
		"vless://11111111-2222-3333-4444-555555555555@cdn.example.com:443?type=ws&security=tls&path=%2Fws&host=cdn.example.com#ws",
		"trojan://secret@t.example.com:443?sni=t.example.com#t",
		"hy2://pw@h.example.com:443?insecure=1&obfs=salamander&obfs-password=x#h",
	} {
		imp, err := ParseShareLink(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		cfg, err := ServerConfig(testInbounds(t), testClients, "", ACMEOptions{}, Routing{Exits: []Exit{{ID: 1, Outbound: imp.Outbound}}, Default: RouteAuto})
		if err != nil {
			t.Fatal(err)
		}
		singBoxCheck(t, cfg)
	}
	for _, bad := range []string{"", "https://example.com", "vless://@host:443", "vmess://abc", "ss://bad@host:1", "vless://u@host:443?type=xhttp"} {
		if _, err := ParseShareLink(bad); err == nil {
			t.Fatalf("%q: expected an error", bad)
		}
	}
}

// "Through the client's exit": every client leaves through the exit it is
// bound to, a client without one is refused, and on a server that is an exit
// itself its own clients leave directly, after the private-address guard.
func TestBoundExits(t *testing.T) {
	hop, err := NewHop(443, "")
	if err != nil {
		t.Fatal(err)
	}
	exits := []Exit{{ID: 5, Outbound: hop.Outbound("203.0.113.20")}, {ID: 6, Outbound: hop.Outbound("203.0.113.21")}}
	names, _ := ParseList(1, "video.example")
	addrs, _ := ParseList(2, "198.51.100.0/24")
	home, _ := ParseList(3, "bank.example")
	for _, tc := range []struct {
		name     string
		rules    []RouteRule
		def      string
		bindings []Binding
	}{
		{"default", nil, RouteAuto, []Binding{{ExitID: 5, Users: []string{"c1"}}, {ExitID: 6, Users: []string{"c2"}}}},
		{"rule by name", []RouteRule{{List: home, Action: RouteDirect}, {List: names, Action: RouteAuto}}, RouteDirect, []Binding{{ExitID: 5, Users: []string{"c1"}}, {ExitID: 6, Users: []string{"c2"}}}},
		{"rule by address", []RouteRule{{List: addrs, Action: RouteAuto}}, RouteAuto, []Binding{{ExitID: 5, Users: []string{"c1"}}, {ExitID: 6, Users: []string{"c2"}}}},
		{"this server is an exit", []RouteRule{{List: home, Action: RouteDirect}, {List: names, Action: RouteAuto}, {List: home, Action: RouteNode, ExitID: 5}}, RouteAuto, []Binding{{ExitID: 5, Users: []string{"c1"}}, {ExitID: 7, Users: []string{"c2"}, Local: true}}},
		{"nobody bound", []RouteRule{{List: names, Action: RouteAuto}}, RouteAuto, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ServerConfig(testInbounds(t), testClients, "127.0.0.1:2054", ACMEOptions{}, Routing{Exits: exits, Rules: tc.rules, Default: tc.def, Bindings: tc.bindings})
			if err != nil {
				t.Fatal(err)
			}
			singBoxCheck(t, cfg)
			if strings.Contains(string(cfg), "urltest") {
				t.Fatal("exits are chosen per client, not by speed")
			}
			rules, final := routeRules(t, cfg)
			if final != "direct" {
				t.Fatalf("final %q", final)
			}
			resolve := -1
			routed := map[string]string{} // user → exit, before resolving
			for i, r := range rules {
				if r["action"] == "resolve" {
					resolve = i
				}
				users, _ := r["auth_user"].([]any)
				if r["action"] != "route" {
					continue
				}
				out, _ := r["outbound"].(string)
				if len(users) == 0 {
					// Inside a logical rule the users sit one level down.
					if sub, ok := r["rules"].([]any); ok {
						users, _ = sub[len(sub)-1].(map[string]any)["auth_user"].([]any)
					}
				}
				if len(users) == 0 && out != "direct" && !(tc.name == "this server is an exit" && out == "node-5") {
					t.Fatalf("routed to %s for everybody: %v", out, r)
				}
				for _, u := range users {
					if out == "direct" && resolve == -1 {
						t.Fatalf("%v leaves directly before the guard: %v", u, r)
					}
					if resolve == -1 {
						routed[u.(string)] = out
					}
				}
			}
			for _, b := range tc.bindings {
				want := ExitTag(b.ExitID)
				for _, u := range b.Users {
					if b.Local {
						if got, ok := routed[u]; ok {
							t.Fatalf("%s sent to %s although this server is its exit", u, got)
						}
					} else if routed[u] != want {
						t.Fatalf("%s: got %q, want %s", u, routed[u], want)
					}
				}
			}
			// Whoever is left over is refused, never let out.
			if last := rules[len(rules)-1]; tc.def == RouteAuto && last["action"] != "reject" {
				ok := false
				for _, r := range rules[:max(resolve, 0)] {
					ok = ok || r["action"] == "reject"
				}
				if !ok {
					t.Fatalf("no reject for clients without an exit: %v", rules)
				}
			}
		})
	}
}
