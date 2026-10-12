package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/PaulichP/Portunus/server/internal/store"
)

// What is counted for clients also lands in the hour it was counted in,
// summed over all of them, and that is what the overview's chart is given.
func TestTrafficHistory(t *testing.T) {
	e := newTestEnv(t)
	h := map[string]string{csrfHeader: e.loginFresh(t)}
	ctx := context.Background()
	var ids []int64
	for _, name := range []string{"one", "two"} {
		resp, out := e.do(t, "POST", "/api/clients", map[string]any{"name": name}, h)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create client: %d %v", resp.StatusCode, out)
		}
		ids = append(ids, int64(out["id"].(float64)))
	}
	for range 2 {
		if err := e.store.AddTraffic(ctx, []store.TrafficDelta{{ClientID: ids[0], Upload: 10, Download: 100}, {ClientID: ids[1], Upload: 1, Download: 5}}); err != nil {
			t.Fatal(err)
		}
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/traffic", nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hours []struct {
		At               string
		Upload, Download int64
	}
	if err := json.NewDecoder(resp.Body).Decode(&hours); err != nil || resp.StatusCode != 200 {
		t.Fatalf("traffic: %d %v", resp.StatusCode, err)
	}
	if len(hours) != 1 || hours[0].Upload != 22 || hours[0].Download != 210 {
		t.Fatalf("history: %+v", hours)
	}
	c, err := e.store.ClientByID(ctx, ids[0])
	if err != nil || c.Download != 200 || c.LastSeen == nil {
		t.Fatalf("client after traffic: %+v %v", c, err)
	}
}
