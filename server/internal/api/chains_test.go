package api

import (
	"net/http"
	"strconv"
	"testing"
)

func TestChains(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

	// Two agent nodes: a nearby entry and a farther exit.
	resp, hk := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "HK", "address": "203.0.113.10", "isEntry": true}, h)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create entry: %d %v", resp.StatusCode, hk)
	}
	resp, tw := e.do(t, "POST", "/api/nodes", map[string]any{"kind": "agent", "name": "TW", "address": "203.0.113.20", "isExit": true}, h)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create exit: %d %v", resp.StatusCode, tw)
	}
	hkID := int64(hk["id"].(float64))
	twID := int64(tw["id"].(float64))

	// Validation: no edges is not a chain; an unknown node and the main
	// server as a target are rejected.
	if resp, body := e.do(t, "POST", "/api/chains", map[string]any{"name": "Empty", "edges": []map[string]int64{}}, h); resp.StatusCode != http.StatusBadRequest || body["field"] != "edges" || body["code"] != "required" {
		t.Fatalf("empty chain: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "POST", "/api/chains", map[string]any{"name": "Ghost", "edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": 99}}}, h); resp.StatusCode != http.StatusBadRequest || body["field"] != "edges" || body["code"] != "unknown" {
		t.Fatalf("ghost chain: %d %v", resp.StatusCode, body)
	}
	if resp, body := e.do(t, "POST", "/api/chains", map[string]any{"name": "BackToMain", "edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": 0}}}, h); resp.StatusCode != http.StatusBadRequest || body["field"] != "edges" || body["code"] != "unknown" {
		t.Fatalf("main as target: %d %v", resp.StatusCode, body)
	}

	// Create the graph.
	resp, ch := e.do(t, "POST", "/api/chains", map[string]any{"name": "Asia", "edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": twID}}}, h)
	if resp.StatusCode != http.StatusCreated || len(ch["edges"].([]any)) != 2 {
		t.Fatalf("create chain: %d %v", resp.StatusCode, ch)
	}
	chID := int64(ch["id"].(float64))

	// The list shows the graph and its client count.
	list := e.list(t, "/api/chains")
	if len(list) != 1 || list[0]["name"] != "Asia" || list[0]["clients"] != float64(0) {
		t.Fatalf("list: %v", list)
	}

	// A second entry feeds the same exit; it is stored as a backup source.
	resp, safe := e.do(t, "POST", "/api/chains", map[string]any{"name": "Safe", "edges": []map[string]int64{{"from": 0, "to": twID}, {"from": hkID, "to": twID}}, "backups": []int64{hkID}}, h)
	if resp.StatusCode != http.StatusCreated || len(safe["backups"].([]any)) != 1 {
		t.Fatalf("create chain with backup: %d %v", resp.StatusCode, safe)
	}

	// A client can be bound to the chain; mixing it with an exit is refused.
	resp, cl := e.do(t, "POST", "/api/clients", map[string]any{"name": "c1", "enabled": true, "chainId": chID}, h)
	if resp.StatusCode != http.StatusCreated || cl["chainId"] == nil {
		t.Fatalf("client on chain: %d %v", resp.StatusCode, cl)
	}
	if resp, body := e.do(t, "POST", "/api/clients", map[string]any{"name": "c2", "enabled": true, "exitId": twID, "chainId": chID}, h); resp.StatusCode != http.StatusBadRequest || body["field"] != "chainId" || body["code"] != "route_conflict" {
		t.Fatalf("mixed route: %d %v", resp.StatusCode, body)
	}

	// Renaming keeps the graph and its material.
	resp, up := e.do(t, "PUT", "/api/chains/"+strconv.FormatInt(chID, 10), map[string]any{"name": "Asia Loop", "edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": twID}}}, h)
	if resp.StatusCode != http.StatusOK || up["name"] != "Asia Loop" {
		t.Fatalf("update chain: %d %v", resp.StatusCode, up)
	}

	// Deleting the node drops the chain and releases the client.
	resp, _ = e.do(t, "DELETE", "/api/nodes/"+strconv.FormatInt(twID, 10), nil, h)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete node: %d", resp.StatusCode)
	}
	if list := e.list(t, "/api/chains"); len(list) != 0 {
		t.Fatalf("chains after delete: %v", list)
	}
	for _, c := range e.list(t, "/api/clients") {
		if c["name"] == "c1" && c["chainId"] != nil {
			t.Fatalf("client kept the chain: %v", c)
		}
	}
}
