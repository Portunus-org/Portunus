package api

import (
	"strconv"
	"testing"
)

// Ports are shared by everything that listens on the server: no two
// listeners of the panel may meet, and none may land on a port another
// program holds. TCP and UDP are separate spaces.
func TestPortLogic(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	inbound := func(protocol string, port int) (int, map[string]any) {
		body := map[string]any{"protocol": protocol, "port": port}
		if protocol != "vless" && protocol != "shadowsocks" {
			body["serverName"] = "vpn.example.com"
		}
		resp, out := e.do(t, "POST", "/api/inbounds", body, h)
		return resp.StatusCode, out
	}
	if code, out := inbound("hysteria2", 30443); code != 201 {
		t.Fatalf("hysteria2: %d %v", code, out)
	}
	// The same number on the other transport is a different port.
	code, vless := inbound("vless", 30443)
	if code != 201 {
		t.Fatalf("vless next to hysteria2: %d %v", code, vless)
	}
	for _, p := range []string{"tuic", "shadowsocks", "trojan"} {
		if code, out := inbound(p, 30443); code != 400 || out["code"] != "in_use" {
			t.Fatalf("%s on a taken port: %d %v", p, code, out)
		}
	}
	// A port of another program on the host.
	e.busyTCP[30022] = true
	if code, out := inbound("trojan", 30022); code != 400 || out["code"] != "busy_on_host" {
		t.Fatalf("port of another program: %d %v", code, out)
	}
	if code, out := inbound("hysteria2", 30022); code != 201 {
		t.Fatalf("UDP is free there: %d %v", code, out)
	}
	// An inbound keeps its own port when edited, even though the host now
	// (rightly) reports it as listening.
	e.busyTCP[30443] = true
	id := strconv.Itoa(int(vless["id"].(float64)))
	edit := func(port int) (int, map[string]any) {
		resp, out := e.do(t, "PUT", "/api/inbounds/"+id, map[string]any{
			"name": "Renamed", "port": port, "enabled": true, "realityServer": "www.example.com", "realityServerPort": 443,
		}, h)
		return resp.StatusCode, out
	}
	if code, out := edit(30443); code != 200 {
		t.Fatalf("edit keeping the port: %d %v", code, out)
	}
	if code, out := edit(30022); code != 400 || out["code"] != "busy_on_host" {
		t.Fatalf("edit onto a busy port: %d %v", code, out)
	}

	// Subscriptions and certificate checks listen on TCP too.
	settings := func(extra map[string]any) (int, map[string]any) {
		body := map[string]any{"publicHost": "proxy.example.com", "subscriptionBaseUrl": ""}
		for k, v := range extra {
			body[k] = v
		}
		resp, out := e.do(t, "PUT", "/api/settings", body, h)
		return resp.StatusCode, out
	}
	if code, out := settings(map[string]any{"subscriptionPort": 30443}); code != 400 || out["field"] != "subscriptionPort" {
		t.Fatalf("subscriptions on an inbound's TCP port: %d %v", code, out)
	}
	if code, out := settings(map[string]any{"subscriptionPort": 30022}); code != 400 || out["code"] != "busy_on_host" {
		t.Fatalf("subscriptions on a port of another program: %d %v", code, out)
	}
	if code, out := settings(map[string]any{"subscriptionPort": 32096}); code != 200 {
		t.Fatalf("subscriptions on a free port: %d %v", code, out)
	}
	if code, out := inbound("trojan", 32096); code != 400 || out["code"] != "reserved" {
		t.Fatalf("inbound on the subscription port: %d %v", code, out)
	}
	if code, out := settings(map[string]any{"acmeChallenge": "proxy", "acmeHttpPort": 30443}); code != 400 || out["field"] != "acmeHttpPort" {
		t.Fatalf("certificate checks on an inbound's port: %d %v", code, out)
	}
}

// "Through the fastest exit" needs an exit to exist.
func TestLastExitStays(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	add := func(name string) string {
		resp, n := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "link", "link": "trojan://secret@" + name + ".example.com:443#" + name}, h)
		if resp.StatusCode != 201 {
			t.Fatalf("node %s: %d %v", name, resp.StatusCode, n)
		}
		return strconv.Itoa(int(n["id"].(float64)))
	}
	a, b := add("a"), add("b")
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"action": "auto"}, h); resp.StatusCode != 204 {
		t.Fatalf("default auto: %d", resp.StatusCode)
	}
	// One of two may go.
	if resp, out := e.do(t, "DELETE", "/api/nodes/"+a, nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete one of two exits: %d %v", resp.StatusCode, out)
	}
	// The last one may not, nor be switched off.
	if resp, out := e.do(t, "DELETE", "/api/nodes/"+b, nil, h); resp.StatusCode != 400 || out["code"] != "node_in_use" {
		t.Fatalf("delete the last exit under auto: %d %v", resp.StatusCode, out)
	}
	if resp, out := e.do(t, "PUT", "/api/nodes/"+b, map[string]any{"name": "b", "enabled": false}, h); resp.StatusCode != 400 || out["code"] != "node_in_use" {
		t.Fatalf("disable the last exit under auto: %d %v", resp.StatusCode, out)
	}
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"action": "direct"}, h); resp.StatusCode != 204 {
		t.Fatalf("default direct: %d", resp.StatusCode)
	}
	if resp, out := e.do(t, "DELETE", "/api/nodes/"+b, nil, h); resp.StatusCode != 204 {
		t.Fatalf("delete the last exit once nothing needs it: %d %v", resp.StatusCode, out)
	}
}
