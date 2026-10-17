package core

import (
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/store"
)

func TestClientActivity(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	c := &Core{now: func() time.Time { return now }}
	if c.ClientActivity(1).Online {
		t.Fatal("online before any traffic")
	}
	// The first reading shows who is online but not how fast.
	c.online.note(0, []store.TrafficDelta{{ClientID: 1, Download: 1000}}, now)
	if a := c.ClientActivity(1); !a.Online || a.DownRate != 0 || c.ClientActivity(2).Online {
		t.Fatalf("after the first reading: %+v", a)
	}
	now = now.Add(10 * time.Second)
	c.online.note(0, []store.TrafficDelta{{ClientID: 1, Download: 50_000, Upload: 2_000}}, now)
	if a := c.ClientActivity(1); a.DownRate != 5000 || a.UpRate != 200 {
		t.Fatalf("speed: %+v", a)
	}
	// Another server's readings have their own clock.
	c.online.note(7, []store.TrafficDelta{{ClientID: 2, Download: 9000}}, now)
	if a := c.ClientActivity(2); !a.Online || a.DownRate != 0 {
		t.Fatalf("first reading of a node: %+v", a)
	}
	now = now.Add(onlineWindow)
	if !c.ClientActivity(1).Online {
		t.Fatal("offline within the window")
	}
	now = now.Add(time.Second)
	if c.ClientActivity(1).Online {
		t.Fatal("still online after the window")
	}
	// Old entries do not pile up.
	c.online.note(0, nil, now)
	if len(c.online.seen) != 0 {
		t.Fatalf("stale entries kept: %v", c.online.seen)
	}
}
