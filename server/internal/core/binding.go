package core

import (
	"context"
	"slices"
	"time"

	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// A client is bound to one exit: traffic that rules send "through the
// client's exit" always leaves there, whichever server the client entered
// through, so sites keep seeing one address for it. By default it is the
// fastest exit at the moment the client is bound (see pick); the operator
// can choose another. The binding is stored and only changes when
//
//   - the exit has not answered for exitGrace (and another one does),
//   - the exit is removed or switched off,
//   - the client's keys are reset, or the operator picks another exit.
//
// It does not move back by itself when the old exit returns: that would be
// one more change of address for nothing.
const (
	// exitGrace is how long an exit must stay unreachable before its
	// clients are moved; exitFailures is how many checks in a row must have
	// failed by then. Short outages and a lost check change nothing.
	exitGrace    = 2 * time.Minute
	exitFailures = 3
	// exitSpeedMargin is the difference in latency, in milliseconds, below
	// which two exits are taken to be equally fast.
	exitSpeedMargin = 50
)

// exitHealth counts the failed checks of an exit since it last answered.
type exitHealth struct {
	since time.Time
	fails int
	// told: the loss has been reported (by noteExit) already.
	told bool
}

// noteExit records the outcome of a check and reports whether the exit has
// just been given up on.
func (c *Core) noteExit(id int64, ok bool) (lost bool) {
	c.nodeMu.Lock()
	defer c.nodeMu.Unlock()
	if ok {
		delete(c.exitHealth, id)
		return false
	}
	h := c.exitHealth[id]
	if h.fails == 0 {
		h.since = c.now()
	}
	h.fails++
	lost = c.lostLocked(h) && !h.told
	h.told = h.told || lost
	c.exitHealth[id] = h
	return lost
}

func (c *Core) lostLocked(h exitHealth) bool {
	return h.fails >= exitFailures && c.now().Sub(h.since) >= exitGrace
}

// exitState says whether an exit is failing its checks, and whether it has
// done so for long enough to be given up on.
func (c *Core) exitState(id int64) (failing, lost bool) {
	c.nodeMu.Lock()
	defer c.nodeMu.Unlock()
	h := c.exitHealth[id]
	return h.fails > 0, c.lostLocked(h)
}

// bindClients gives every client an exit and moves the clients of exits
// that are gone. Callers hold applyMu.
func (c *Core) bindClients(ctx context.Context) error {
	nodes, err := c.store.ListNodes(ctx)
	if err != nil {
		return err
	}
	var exits []int64
	for _, n := range nodes {
		if n.IsExit && n.Enabled {
			exits = append(exits, n.ID)
		}
	}
	if len(exits) == 0 {
		// Nothing to bind to. Bindings to an exit that is switched off are
		// kept: its clients return to it when it is switched on again.
		return nil
	}
	clients, err := c.store.ListClients(ctx)
	if err != nil {
		return err
	}
	load := map[int64]int{}
	for _, cl := range clients {
		if cl.ExitID != nil {
			load[*cl.ExitID]++
		}
	}
	// pick is the fastest exit that answers. Exits about as fast as the
	// fastest (within exitSpeedMargin) count as equal, and among equals the
	// one with the fewest clients wins, so that similar exits share the
	// load. An exit that is failing its checks, but not given up on yet, is
	// taken only when nothing better exists; with all, even one given up on.
	pick := func(all bool) (best int64, ok bool) {
		var healthy, rest []int64
		for _, id := range exits {
			switch failing, lost := c.exitState(id); {
			case !failing:
				healthy = append(healthy, id)
			case !lost || all:
				rest = append(rest, id)
			}
		}
		if len(healthy) == 0 {
			healthy = rest
		}
		fastest := -1
		for _, id := range healthy {
			if ms := c.NodeStatus(id).LatencyMs; ms != nil && (fastest < 0 || *ms < fastest) {
				fastest = *ms
			}
		}
		for _, id := range healthy {
			// Not measured yet counts as slow, unless nothing is measured.
			if ms := c.NodeStatus(id).LatencyMs; fastest >= 0 && (ms == nil || *ms > fastest+exitSpeedMargin) {
				continue
			}
			if !ok || load[id] < load[best] {
				best, ok = id, true
			}
		}
		return best, ok
	}
	for _, cl := range clients {
		valid := cl.ExitID != nil && slices.Contains(exits, *cl.ExitID)
		if valid {
			if _, lost := c.exitState(*cl.ExitID); !lost {
				continue
			}
		}
		to, ok := pick(false)
		if !ok {
			if valid {
				// Every exit is down: moving would not help.
				continue
			}
			to, _ = pick(true)
		}
		if cl.ExitID != nil {
			load[*cl.ExitID]--
			c.log.Info("client moved to another exit", "client", cl.Name, "from", *cl.ExitID, "to", to)
		}
		load[to]++
		if err := c.store.SetClientExit(ctx, cl.ID, &to); err != nil {
			return err
		}
	}
	return nil
}

// bindings are the exits of the clients allowed to connect, as the server
// self (0: the main one) has to route them.
func (c *Core) bindings(ctx context.Context, self int64, exits []int64) ([]proxy.Binding, error) {
	clients, err := c.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	var out []proxy.Binding
	for _, id := range exits {
		b := proxy.Binding{ExitID: id, Local: id == self}
		for _, cl := range clients {
			if cl.ExitID != nil && *cl.ExitID == id && c.ClientStatus(cl) == ClientActive {
				b.Users = append(b.Users, toProxyClient(cl).UserName())
			}
		}
		if len(b.Users) > 0 {
			out = append(out, b)
		}
	}
	return out, nil
}

// ExitClients counts the clients bound to each exit.
func (c *Core) ExitClients(ctx context.Context) (map[int64]int, error) {
	clients, err := c.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]int{}
	for _, cl := range clients {
		if cl.ExitID != nil {
			out[*cl.ExitID]++
		}
	}
	return out, nil
}

// setClientExit applies the operator's choice: 0 lets the panel choose
// anew, anything else must be an exit that is switched on.
func (c *Core) setClientExit(ctx context.Context, cl *store.ClientRow, exit int64) error {
	if exit == 0 {
		return c.store.SetClientExit(ctx, cl.ID, nil)
	}
	n, err := c.store.NodeByID(ctx, exit)
	if err != nil || !n.IsExit || !n.Enabled {
		return invalid("exitId", "not_an_exit")
	}
	return c.store.SetClientExit(ctx, cl.ID, &exit)
}
