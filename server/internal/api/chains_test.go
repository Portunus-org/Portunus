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
	if resp, body := e.do(t, "POST", "/api/chains", map[string]any{"name": "Cycle", "edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": 0}}}, h); resp.StatusCode != http.StatusBadRequest || body["field"] != "edges" || body["code"] != "cycle" {
		t.Fatalf("cycle: %d %v", resp.StatusCode, body)
	}
	resp, toMain := e.do(t, "POST", "/api/chains", map[string]any{"name": "ToMain", "edges": []map[string]int64{{"from": hkID, "to": 0}}}, h)
	if resp.StatusCode != http.StatusCreated || len(toMain["edges"].([]any)) != 1 {
		t.Fatalf("main as target: %d %v", resp.StatusCode, toMain)
	}
	toMainID := int64(toMain["id"].(float64))
	if resp, _ := e.do(t, "DELETE", "/api/chains/"+strconv.FormatInt(toMainID, 10), nil, h); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete toMain: %d", resp.StatusCode)
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

func TestChainDefaultRouteAPI(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}

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

	// Create one chain defaulting to direct, another defaulting to auto (via chain).
	resp, c1 := e.do(t, "POST", "/api/chains", map[string]any{
		"name":    "DirectChain",
		"default": map[string]any{"action": "direct"},
		"edges":   []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": twID}},
	}, h)
	if resp.StatusCode != http.StatusCreated || c1["default"].(map[string]any)["action"] != "direct" {
		t.Fatalf("create direct chain: %d %v", resp.StatusCode, c1)
	}
	c1ID := int64(c1["id"].(float64))

	resp, c2 := e.do(t, "POST", "/api/chains", map[string]any{
		"name":  "AutoChain",
		"edges": []map[string]int64{{"from": 0, "to": twID}},
	}, h)
	if resp.StatusCode != http.StatusCreated || c2["default"] != nil {
		t.Fatalf("create default-inherit chain: %d %v", resp.StatusCode, c2)
	}
	c2ID := int64(c2["id"].(float64))

	// Set c2's default via PUT /api/routing/default with chainId.
	if resp, body := e.do(t, "PUT", "/api/routing/default", map[string]any{"chainId": c2ID, "action": "auto"}, h); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set chain default auto: %d %v", resp.StatusCode, body)
	}
	// Block is invalid as default.
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"chainId": c2ID, "action": "block"}, h); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for block default, got %d", resp.StatusCode)
	}
	// Unknown chain returns 404; negative chainId returns 400.
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"chainId": 99999, "action": "direct"}, h); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown chain, got %d", resp.StatusCode)
	}
	if resp, _ := e.do(t, "PUT", "/api/routing/default", map[string]any{"chainId": -1, "action": "direct"}, h); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative chainId, got %d", resp.StatusCode)
	}

	// PUT /api/chains/{id} without default keeps c1's default intact.
	resp, up1 := e.do(t, "PUT", "/api/chains/"+strconv.FormatInt(c1ID, 10), map[string]any{
		"name":  "DirectChain",
		"color": "#0ea5e9",
		"edges": []map[string]int64{{"from": 0, "to": hkID}, {"from": hkID, "to": twID}},
	}, h)
	if resp.StatusCode != http.StatusOK || up1["default"].(map[string]any)["action"] != "direct" {
		t.Fatalf("update chain without default: %d %v", resp.StatusCode, up1)
	}

	// Clear c1's default via PUT /api/routing/default with action: "".
	if resp, body := e.do(t, "PUT", "/api/routing/default", map[string]any{"chainId": c1ID, "action": ""}, h); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear chain default: %d %v", resp.StatusCode, body)
	}
	for _, ch := range e.list(t, "/api/chains") {
		switch int64(ch["id"].(float64)) {
		case c1ID:
			if ch["default"] != nil {
				t.Fatalf("c1 default not cleared: %v", ch)
			}
		case c2ID:
			if ch["default"] == nil || ch["default"].(map[string]any)["action"] != "auto" {
				t.Fatalf("c2 default missing: %v", ch)
			}
		}
	}
}
