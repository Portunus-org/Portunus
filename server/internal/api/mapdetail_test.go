package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The detailed map comes from the first source that answers with tiles: one
// that is out of reach, or lists tiles it then fails to give, is passed
// over. Tiles named relative to the list are found too.
func TestDetailSourceFallback(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/planet") {
			w.Write([]byte(`{"tiles":["` + "http://" + r.Host + `/t/{z}/{x}/{y}.pbf"]}`))
			return
		}
		http.Error(w, "throttled", http.StatusBadGateway) // the list comes, the tiles do not
	}))
	t.Cleanup(dead.Close)
	asked := 0
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tiles/osm/tiles.json" {
			w.Write([]byte(`{"tiles":["/tiles/osm/{z}/{x}/{y}"]}`))
			return
		}
		asked++
		w.Write([]byte("tile " + r.URL.Path))
	}))
	t.Cleanup(good.Close)

	c := newTileCache(t.TempDir())
	sources := []string{"http://127.0.0.1:1/unreachable", dead.URL + "/planet", good.URL + "/tiles/osm/tiles.json"}
	source, template, err := c.detailSource(context.Background(), sources)
	if err != nil || source != sources[2] || template != good.URL+"/tiles/osm/{z}/{x}/{y}" {
		t.Fatalf("picked %q %q: %v", source, template, err)
	}
	// The choice is kept: nothing is tried again for the next tile.
	if again, _, _ := c.detailSource(context.Background(), sources); again != source || asked != 1 {
		t.Fatalf("chosen again: %q, tiles asked of the good source: %d", again, asked)
	}
	// When nothing works that is said, and not tried again at once.
	none := newTileCache(t.TempDir())
	if _, _, err := none.detailSource(context.Background(), sources[:2]); err == nil || !strings.Contains(err.Error(), dead.URL) {
		t.Fatalf("no source works: %v", err)
	}
	if _, _, err := none.detailSource(context.Background(), sources); err == nil {
		t.Fatal("tried again within the minute")
	}
}
