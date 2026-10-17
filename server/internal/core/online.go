package core

import (
	"context"
	"sync"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/store"
)

// A client is online while traffic of its own is being counted. There is
// nothing else to go by: the protocols have no sessions the panel could
// see. Counters are read every statsEvery on this server and about twice a
// minute on the nodes that take clients, so a client shows up within
// seconds and stays online for onlineWindow after its last byte.
const (
	statsEvery   = 10 * time.Second
	onlineWindow = 45 * time.Second
)

// Activity is what a client is doing right now.
type Activity struct {
	Online bool
	// Bytes per second, averaged over the last reading of the counters.
	DownRate, UpRate int64
}

type lastTraffic struct {
	at       time.Time
	down, up int64 // bytes per second
}

// online remembers the last counted traffic of each client. Nothing of it
// is stored: after a restart everybody is offline until they send
// something. (When a client was last seen is stored, with its traffic.)
type online struct {
	mu   sync.Mutex
	seen map[int64]lastTraffic
	// read is when each server's counters were read last (0: this one), to
	// turn what was counted since into a speed.
	read map[int64]time.Time
}

// note records traffic counted on the server source (0: this one) since its
// counters were read last.
func (o *online) note(source int64, deltas []store.TrafficDelta, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen, o.read = map[int64]lastTraffic{}, map[int64]time.Time{}
	}
	// A first reading, or one after a long break, says nothing about speed.
	secs := int64(0)
	if prev, ok := o.read[source]; ok {
		if d := now.Sub(prev); d >= time.Second && d <= 2*time.Minute {
			secs = int64(d / time.Second)
		}
	}
	o.read[source] = now
	for _, d := range deltas {
		t := lastTraffic{at: now}
		if secs > 0 {
			t.down, t.up = d.Download/secs, d.Upload/secs
		}
		o.seen[d.ClientID] = t
	}
	for id, t := range o.seen {
		if now.Sub(t.at) > onlineWindow {
			delete(o.seen, id)
		}
	}
}

// ClientActivity reports whether traffic of the client was counted just
// now, and how fast it was going.
func (c *Core) ClientActivity(id int64) Activity {
	c.online.mu.Lock()
	defer c.online.mu.Unlock()
	t, ok := c.online.seen[id]
	if !ok || c.now().Sub(t.at) > onlineWindow {
		return Activity{}
	}
	return Activity{Online: true, DownRate: t.down, UpRate: t.up}
}

// TrafficSince returns the traffic of all clients together, by the hour,
// from since on.
func (c *Core) TrafficSince(ctx context.Context, since time.Time) ([]store.TrafficHour, error) {
	return c.store.TrafficSince(ctx, since)
}
