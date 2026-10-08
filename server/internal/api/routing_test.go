package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PaulichP/Portunus/server/internal/agent"
	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/singbox"
)

func (e *testEnv) list(t *testing.T, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if code := e.getJSON(t, path, &out); code != 200 {
		t.Fatalf("GET %s: %d", path, code)
	}
	return out
}

func TestNodesAndRouting(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	// An imported proxy.
	resp, link := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "link", "link": "trojan://secret@exit.example.com:443?sni=exit.example.com#Rented"}, h)
	if resp.StatusCode != 201 || link["name"] != "Rented" || link["isExit"] != true || link["address"] != "exit.example.com" {
		t.Fatalf("import link: %d %v", resp.StatusCode, link)
	}
	if resp, body := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "link", "link": "https://example.com"}, h); resp.StatusCode != 400 || body["code"] != "unsupported_link" {
		t.Fatalf("bad link: %d %v", resp.StatusCode, body)
	}
	// A server of ours: validation, then the join command.
	if resp, body := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "203.0.113.20"}, h); resp.StatusCode != 400 || body["field"] != "roles" {
		t.Fatalf("no roles: %d %v", resp.StatusCode, body)
	}
	resp, node := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "203.0.113.20", "isExit": true, "isEntry": true}, h)
	if resp.StatusCode != 201 || node["hopPort"] != float64(8443) || node["agentPort"] != float64(agent.DefaultPort) || node["joined"] != false {
		t.Fatalf("create node: %d %v", resp.StatusCode, node)
	}
	if resp, body := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Again", "address": "203.0.113.20", "isExit": true}, h); resp.StatusCode != 400 || body["field"] != "address" {
		t.Fatalf("duplicate address: %d %v", resp.StatusCode, body)
	}
	nodeID := int64(node["id"].(float64))
	resp, join := e.do(t, "GET", "/api/nodes/"+strconv.FormatInt(nodeID, 10)+"/join", nil, nil)
	run, _ := join["run"].(string)
	if resp.StatusCode != 200 || !strings.Contains(run, "PORTUNUS_JOIN=") || !strings.HasSuffix(run, " portunus:local") ||
		!strings.HasPrefix(run, "sudo ") || strings.Contains(run, "--pull") ||
		!strings.Contains(join["copyImage"].(string), "ssh 203.0.113.20") {
		t.Fatalf("join: %d %v", resp.StatusCode, join)
	}
	token := strings.Fields(run[strings.Index(run, "PORTUNUS_JOIN=")+len("PORTUNUS_JOIN="):])[0]
	if b, err := agent.DecodeBundle(token); err != nil || b.Port != agent.DefaultPort {
		t.Fatalf("bundle: %+v %v", b, err)
	} else if _, err := b.TLSConfig(); err != nil {
		t.Fatalf("bundle TLS: %v", err)
	}

	// Lists: typed and downloaded.
	if resp, body := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Empty", "content": "# nothing"}, h); resp.StatusCode != 400 || body["code"] != "empty_list" {
		t.Fatalf("empty list: %d %v", resp.StatusCode, body)
	}
	resp, own := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Banks", "content": "bank.example\n198.51.100.0/24\nnot valid!"}, h)
	if resp.StatusCode != 201 || own["domains"] != float64(1) || own["addresses"] != float64(1) || own["skipped"] != float64(1) {
		t.Fatalf("manual list: %d %v", resp.StatusCode, own)
	}
	served := "blocked.example\nother.example\n"
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			w.Write([]byte(served))
		case "/html":
			w.Write([]byte("<html><body><p>Not a list at all</p></body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer src.Close()
	for path, code := range map[string]string{"/missing": "fetch_failed", "/html": "not_a_list"} {
		if resp, body := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "url", "name": "Bad", "url": src.URL + path}, h); resp.StatusCode != 400 || body["code"] != code {
			t.Fatalf("%s: %d %v", path, resp.StatusCode, body)
		}
	}
	resp, remote := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "url", "name": "Blocked", "url": src.URL + "/list"}, h)
	if resp.StatusCode != 201 || remote["domains"] != float64(2) || remote["fetchedAt"] == nil || remote["content"] != nil {
		t.Fatalf("url list: %d %v", resp.StatusCode, remote)
	}
	served += "third.example\n"
	if resp, body := e.do(t, "POST", "/api/routing/lists/"+strconv.Itoa(int(remote["id"].(float64)))+"/refresh", nil, h); resp.StatusCode != 200 || body["domains"] != float64(3) {
		t.Fatalf("refresh: %d %v", resp.StatusCode, body)
	}

	// sing-box rule-sets, source and binary, are converted by sing-box.
	if bin := os.Getenv("PORTUNUS_TEST_SINGBOX"); bin != "" {
		source := `{"version":1,"rules":[{"domain_suffix":["video.example","tube.example"],"ip_cidr":["198.51.100.0/24"]}]}`
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "s.json"), []byte(source), 0o600)
		if out, err := exec.Command(bin, "rule-set", "compile", filepath.Join(dir, "s.json"), "-o", filepath.Join(dir, "s.srs")).CombinedOutput(); err != nil {
			t.Fatalf("compile: %v %s", err, out)
		}
		binary, _ := os.ReadFile(filepath.Join(dir, "s.srs"))
		sets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/set.json":
				w.Write([]byte(source))
			case "/set.srs":
				w.Write(binary)
			case "/broken.json":
				w.Write([]byte(`{"version":1,"rules":[{"no_such_field":["x"]}]}`))
			}
		}))
		defer sets.Close()
		for _, name := range []string{"set.json", "set.srs"} {
			resp, l := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "url", "name": name, "url": sets.URL + "/" + name}, h)
			if resp.StatusCode != 201 || l["format"] != "ruleset" || l["domains"] != float64(2) || l["addresses"] != float64(1) {
				t.Fatalf("%s: %d %v", name, resp.StatusCode, l)
			}
			// A rule deleted takes its own list with it.
			resp, rule := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": l["id"], "action": "block"}, h)
			if resp.StatusCode != 201 {
				t.Fatalf("rule on %s: %d", name, resp.StatusCode)
			}
			if resp, _ := e.do(t, "DELETE", "/api/routing/rules/"+strconv.Itoa(int(rule["id"].(float64))), nil, h); resp.StatusCode != 204 {
				t.Fatalf("delete rule: %d", resp.StatusCode)
			}
		}
		if resp, body := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "url", "name": "Broken", "url": sets.URL + "/broken.json"}, h); resp.StatusCode != 400 || body["code"] != "not_a_list" {
			t.Fatalf("broken rule-set: %d %v", resp.StatusCode, body)
		}
		var after struct {
			Lists []map[string]any `json:"lists"`
		}
		e.getJSON(t, "/api/routing", &after)
		if len(after.Lists) != 2 {
			t.Fatalf("lists of deleted rules were kept: %v", after.Lists)
		}
	}

	// A name is never "taken": a leftover list gives way, a used one makes
	// the newcomer numbered.
	resp, dup := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Twice", "content": "a.example"}, h)
	if resp.StatusCode != 201 {
		t.Fatalf("first: %d %v", resp.StatusCode, dup)
	}
	resp, again := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "twice", "content": "b.example"}, h)
	if resp.StatusCode != 201 || again["name"] != "twice" || again["content"] != "b.example" {
		t.Fatalf("leftover list did not give way: %d %v", resp.StatusCode, again)
	}
	_, used := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": again["id"], "action": "block"}, h)
	resp, third := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Twice", "content": "c.example"}, h)
	if resp.StatusCode != 201 || third["name"] != "Twice 2" {
		t.Fatalf("second list over a used name: %d %v", resp.StatusCode, third)
	}
	e.do(t, "DELETE", "/api/routing/rules/"+strconv.Itoa(int(used["id"].(float64))), nil, h)
	e.do(t, "DELETE", "/api/routing/lists/"+strconv.Itoa(int(third["id"].(float64))), nil, h)

	// Rules.
	if resp, body := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": remote["id"], "action": "node"}, h); resp.StatusCode != 400 || body["field"] != "node" {
		t.Fatalf("rule without node: %d %v", resp.StatusCode, body)
	}
	resp, r1 := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": remote["id"], "action": "node", "nodeId": nodeID}, h)
	if resp.StatusCode != 201 {
		t.Fatalf("rule 1: %d %v", resp.StatusCode, r1)
	}
	resp, r2 := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": own["id"], "action": "direct"}, h)
	if resp.StatusCode != 201 {
		t.Fatalf("rule 2: %d %v", resp.StatusCode, r2)
	}
	if resp, _ := e.do(t, "PUT", "/api/routing/rules/order", map[string]any{"ids": []any{r2["id"], r1["id"]}}, h); resp.StatusCode != 204 {
		t.Fatalf("order: %d", resp.StatusCode)
	}
	if resp, body := e.do(t, "PUT", "/api/routing/rules/order", map[string]any{"ids": []any{r2["id"]}}, h); resp.StatusCode != 400 {
		t.Fatalf("incomplete order: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "PUT", "/api/routing/default", map[string]any{"action": "block"}, h); resp.StatusCode != 400 {
		t.Fatalf("default block: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"action": "auto"}, h); resp.StatusCode != 204 {
		t.Fatalf("default auto: %d", resp.StatusCode)
	}
	var routing struct {
		Default map[string]any   `json:"default"`
		Rules   []map[string]any `json:"rules"`
		Lists   []map[string]any `json:"lists"`
	}
	e.getJSON(t, "/api/routing", &routing)
	if routing.Default["action"] != "auto" || len(routing.Rules) != 2 || routing.Rules[0]["id"] != r2["id"] || len(routing.Lists) != 2 {
		t.Fatalf("routing: %+v", routing)
	}

	// What is in use cannot be removed from under the rules.
	if resp, body := e.do(t, "DELETE", "/api/nodes/"+strconv.FormatInt(nodeID, 10), nil, h); resp.StatusCode != 400 || body["code"] != "node_in_use" {
		t.Fatalf("delete node in use: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "PUT", "/api/nodes/"+strconv.FormatInt(nodeID, 10), map[string]any{"name": "Exit", "isExit": false, "isEntry": true}, h); resp.StatusCode != 400 || body["code"] != "node_in_use" {
		t.Fatalf("drop exit role in use: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "DELETE", "/api/routing/lists/"+strconv.Itoa(int(own["id"].(float64))), nil, h); resp.StatusCode != 400 || body["code"] != "list_in_use" {
		t.Fatalf("delete list in use: %d %v", resp.StatusCode, body)
	}

	// With a real sing-box the whole configuration must load.
	if os.Getenv("PORTUNUS_TEST_SINGBOX") != "" {
		if resp, in := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "vless", "port": freeTCPPort(t)}, h); resp.StatusCode != 201 {
			t.Fatalf("inbound: %d %v", resp.StatusCode, in)
		}
		var sb struct {
			ApplyError string `json:"applyError"`
		}
		e.getJSON(t, "/api/singbox", &sb)
		if sb.ApplyError != "" {
			t.Fatalf("config with routing rejected: %s", sb.ApplyError)
		}
	}

	if resp, _ := e.do(t, "DELETE", "/api/routing/rules/"+strconv.Itoa(int(r1["id"].(float64))), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete rule: %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "DELETE", "/api/nodes/"+strconv.FormatInt(nodeID, 10), nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete node: %d", resp.StatusCode)
	}
	if n := e.list(t, "/api/nodes"); len(n) != 1 {
		t.Fatalf("nodes left: %v", n)
	}
}

// Servers get a place on the map from the lookup service, unless one was
// set by hand.
func TestNodeLocation(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	resp, node := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "203.0.113.20", "isExit": true}, h)
	if resp.StatusCode != 201 || node["location"] != nil {
		t.Fatalf("create: %d %v", resp.StatusCode, node)
	}
	id := strconv.Itoa(int(node["id"].(float64)))
	e.core.LocateServers(context.Background())
	n := e.list(t, "/api/nodes")[0]
	loc, _ := n["location"].(map[string]any)
	if loc == nil || loc["city"] != "Testville" || loc["lat"] != 52.37 {
		t.Fatalf("looked up location: %v", n["location"])
	}
	if resp, body := e.do(t, "PUT", "/api/nodes/"+id+"/location", map[string]any{"location": map[string]any{"lat": 95, "lon": 0}}, h); resp.StatusCode != 400 {
		t.Fatalf("bad latitude: %d %v", resp.StatusCode, body)
	}
	if resp, _ := e.do(t, "PUT", "/api/nodes/"+id+"/location", map[string]any{"location": map[string]any{"lat": 48.85, "lon": 2.35}}, h); resp.StatusCode != 204 {
		t.Fatalf("set location: %d", resp.StatusCode)
	}
	e.core.LocateServers(context.Background())
	loc = e.list(t, "/api/nodes")[0]["location"].(map[string]any)
	if loc["lat"] != 48.85 || loc["manual"] != true || loc["city"] != nil {
		t.Fatalf("manual location overwritten: %v", loc)
	}
	if resp, _ := e.do(t, "PUT", "/api/topology/location", map[string]any{"location": map[string]any{"lat": 55.75, "lon": 37.62}}, h); resp.StatusCode != 204 {
		t.Fatalf("set server location: %d", resp.StatusCode)
	}
	var topo struct {
		Main struct {
			Location map[string]any `json:"location"`
		} `json:"main"`
	}
	e.getJSON(t, "/api/topology", &topo)
	if topo.Main.Location["lat"] != 55.75 {
		t.Fatalf("topology: %+v", topo)
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestAgentSync runs a real agent next to the panel: the panel must
// configure it over the mutually authenticated channel, a restarted agent
// must come back with its saved configuration, and nobody but the panel may
// talk to it.
func TestAgentSync(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	ctx := context.Background()

	agentPort, hopPort := freeTCPPort(t), freeTCPPort(t)
	resp, node := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "127.0.0.1", "isExit": true, "agentPort": agentPort, "hopPort": hopPort}, h)
	if resp.StatusCode != 201 {
		t.Fatalf("create node: %d %v", resp.StatusCode, node)
	}
	id := strconv.Itoa(int(node["id"].(float64)))

	// Not started yet.
	e.core.SyncNodes(ctx)
	if n := e.list(t, "/api/nodes")[0]; n["status"].(map[string]any)["state"] != core.NodePending {
		t.Fatalf("before join: %v", n["status"])
	}

	_, join := e.do(t, "GET", "/api/nodes/"+id+"/join", nil, nil)
	run := join["run"].(string)
	bundle, err := agent.DecodeBundle(strings.Fields(run[strings.Index(run, "PORTUNUS_JOIN=")+len("PORTUNUS_JOIN="):])[0])
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	start := func() (stop func()) {
		log := testLogger(t)
		mgr := singbox.New(bin, filepath.Join(dir, "sing-box"), log)
		if err := mgr.Init(ctx); err != nil {
			t.Fatal(err)
		}
		srv := agent.NewServer(mgr, dir, log)
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { mgr.Run(runCtx); close(done) }()
		srv.Resume(runCtx)
		tlsCfg, err := bundle.TLSConfig()
		if err != nil {
			t.Fatal(err)
		}
		hs := &http.Server{Handler: srv.Handler(), TLSConfig: tlsCfg}
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(bundle.Port))
		if err != nil {
			t.Fatal(err)
		}
		go hs.ServeTLS(ln, "", "")
		return func() { hs.Close(); cancel(); <-done }
	}
	stop := start()

	e.core.SyncNodes(ctx)
	n := e.list(t, "/api/nodes")[0]
	st := n["status"].(map[string]any)
	if st["state"] != core.NodeOnline || n["joined"] != true {
		t.Fatalf("after join: %v", n)
	}
	waitFor(t, "the node's hop to listen", func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(hopPort), time.Second)
		if err == nil {
			c.Close()
		}
		return err == nil
	})

	// Without the panel's certificate the agent does not talk.
	plain := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: bundleClientTLS(t, bundle)}}
	if res, err := plain.Get("https://127.0.0.1:" + strconv.Itoa(agentPort) + "/v1/status"); err == nil {
		res.Body.Close()
		t.Fatalf("agent answered a client without a certificate: %d", res.StatusCode)
	}

	// A restart: the agent resumes by itself, before the panel says anything.
	stop()
	stop = start()
	defer stop()
	waitFor(t, "the hop to come back from the saved configuration", func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(hopPort), time.Second)
		if err == nil {
			c.Close()
		}
		return err == nil
	})

	// Disabling the node stops its sing-box.
	if resp, body := e.do(t, "PUT", "/api/nodes/"+id, map[string]any{"name": "Exit", "isExit": true, "enabled": false}, h); resp.StatusCode != 200 {
		t.Fatalf("disable: %d %v", resp.StatusCode, body)
	}
	e.core.SyncNodes(ctx)
	waitFor(t, "the hop to stop", func() bool {
		c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(hopPort), 300*time.Millisecond)
		if err == nil {
			c.Close()
		}
		return err != nil
	})
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

// bundleClientTLS trusts the installation's CA but presents no client
// certificate: what anyone who only knows the node's address has.
func bundleClientTLS(t *testing.T, b agent.Bundle) *tls.Config {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(b.CA)) {
		t.Fatal("bad CA in bundle")
	}
	return &tls.Config{RootCAs: pool}
}
