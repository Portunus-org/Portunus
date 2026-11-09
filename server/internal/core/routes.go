package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// clientIDFromName reads the client out of a sing-box user name: "c7" is
// client 7, and "c7-e3" is client 7's credential for its extra exit on node
// 3.
func clientIDFromName(name string) (int64, bool) {
	s := strings.TrimPrefix(name, "c")
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}

// deriveExitUUID is the deterministic credential of a client's extra exit:
// derived from the client's own uuid and the exit node, so it survives
// re-renders and chain edits without changing a working link.
func deriveExitUUID(base string, sink int64) string {
	sum := sha256.Sum256([]byte(base + ":exit:" + strconv.FormatInt(sink, 10)))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// exitCredential is the credential that carries a client's traffic out of
// sink: the base uuid for the first sink of its chain, a derived one for
// the rest.
func exitCredential(cl *store.ClientRow, ch Chain, sink int64) string {
	sinks := ch.sinks()
	if len(sinks) > 0 && sinks[0] == sink {
		return cl.UUID
	}
	return deriveExitUUID(cl.UUID, sink)
}

// ClientLink is one connection of a client: where to enter (host), through
// which inbound, with which credential, and what it is called for the user
// ("" while the inbound's own name stands for it).
type ClientLink struct {
	Host    string
	Inbound proxy.Inbound
	UUID    string
	Label   string
}

// uuidProtocols are the inbounds whose per-connection credential is a uuid;
// the rest carry the client's password, which does not vary per exit.
var uuidProtocols = map[proxy.Protocol]bool{proxy.VLESS: true, proxy.TUIC: true}

// ClientLinks renders the client's connections. With a chain there is one
// link per dialable entry (the main server when it feeds the chain, then the
// primary sources in id order, then the backup-marked ones) aimed at each
// sink reachable from it — carrying that sink's credential and named after
// it ("entry → exit" when several entries exist). A client without a chain
// reaches every entry point with its own credential, as before.
func (c *Core) ClientLinks(ctx context.Context, cl *store.ClientRow) ([]ClientLink, error) {
	host, err := c.PublicHost(ctx)
	if err != nil {
		return nil, err
	}
	inbounds, err := c.Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	nodeByID := make(map[int64]*store.NodeRow, len(rows))
	for _, row := range rows {
		nodeByID[row.ID] = row
	}

	// Where a connection can enter: the main server first, then every joined
	// entry node with the inbounds as they are served there.
	type point struct {
		host     string
		inbounds []proxy.Inbound
	}
	points := []point{{host, inbounds}}
	addPoint := func(id int64) error {
		if id == 0 {
			return nil
		}
		row, ok := nodeByID[id]
		if !ok || row.Kind != NodeAgent || !row.IsEntry || !row.Enabled {
			return nil
		}
		n, err := c.decodeNode(row)
		if err != nil {
			return err
		}
		if n.Settings.JoinedAt == nil {
			return nil
		}
		ins, err := c.entryInbounds(ctx, &n, inbounds)
		if err != nil {
			return err
		}
		points = append(points, point{n.Address, ins})
		return nil
	}

	var out []ClientLink
	seen := map[string]bool{}
	add := func(p point, uuid, label string, onlyUUID bool) {
		for _, in := range p.inbounds {
			if !in.Enabled {
				continue
			}
			// The password-only protocols carry the same credential for every
			// exit: one link per entry is enough for them.
			if onlyUUID && !uuidProtocols[in.Protocol] {
				continue
			}
			key := p.host + "|" + strconv.Itoa(in.Port) + "|" + string(in.Protocol) + "|" + uuid
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ClientLink{Host: p.host, Inbound: in, UUID: uuid, Label: label})
		}
	}

	if cl.ChainID == nil {
		for i := range rows {
			if err := addPoint(rows[i].ID); err != nil {
				return nil, err
			}
		}
		for _, p := range points {
			add(p, cl.UUID, "", false)
		}
		return out, nil
	}

	ch, err := c.Chain(ctx, *cl.ChainID)
	if err != nil {
		return nil, err
	}
	allNodes, err := c.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	nodesByID := make(map[int64]Node, len(allNodes))
	for _, n := range allNodes {
		nodesByID[n.ID] = n
	}
	unusable := func(id int64) bool {
		n, ok := nodesByID[id]
		return !ok || c.nodeUnusable(n)
	}

	fallback := func() error {
		// The chain is down (or nothing dialable feeds it): the client
		// reaches every entry point with its own credential, and the panel
		// gives it an exit for now. Its chain links return when the chain
		// works again.
		for i := range rows {
			if err := addPoint(rows[i].ID); err != nil {
				return err
			}
		}
		for _, p := range points {
			add(p, cl.UUID, "", false)
		}
		return nil
	}
	isEntry := func(id int64) bool {
		n, ok := nodesByID[id]
		return ok && n.IsEntry
	}
	if !c.chainWorks(*ch, unusable, isEntry) {
		return out, fallback()
	}

	// Dialable entries: the main server when it feeds the chain, then every
	// entry point that takes part in the graph in id order, backup-marked
	// ones last.
	backups := map[int64]bool{}
	for _, id := range ch.Backups {
		backups[id] = true
	}
	inGraph := map[int64]bool{}
	for _, e := range ch.Edges {
		if e.FromNodeID != 0 {
			inGraph[e.FromNodeID] = true
		}
		if e.ToNodeID != 0 {
			inGraph[e.ToNodeID] = true
		}
	}
	var primary, backup []int64
	for id := range inGraph {
		n, ok := nodesByID[id]
		if !ok || n.Kind != NodeAgent || !n.IsEntry {
			continue
		}
		if backups[id] {
			backup = append(backup, id)
		} else {
			primary = append(primary, id)
		}
	}
	slices.Sort(primary)
	slices.Sort(backup)
	var entryIDs []int64
	if ch.isSource(0) {
		entryIDs = append(entryIDs, 0)
	}
	entryIDs = append(entryIDs, primary...)
	entryIDs = append(entryIDs, backup...)

	pointIdx := map[int64]int{}
	for _, id := range entryIDs {
		idx := len(points)
		if err := addPoint(id); err != nil {
			return nil, err
		}
		switch {
		case id == 0:
			pointIdx[0] = 0 // the main server is always first
		case len(points) > idx:
			pointIdx[id] = idx
		}
	}
	if len(pointIdx) == 0 {
		return out, fallback()
	}

	multi := len(pointIdx) > 1
	for _, id := range entryIDs {
		idx, ok := pointIdx[id]
		if !ok {
			continue
		}
		p := points[idx]
		entryName := ""
		if id != 0 {
			entryName = nodesByID[id].Name
		}
		// Structural reachability: links stay put while nodes flap; the
		// panel re-binds the traffic automatically.
		reach := ch.sinksFrom(id, func(int64) bool { return true })
		firstSink := true
		for _, s := range ch.sinks() {
			if !reach[s] {
				continue
			}
			label := nodesByID[s].Name
			if multi {
				label = fmt.Sprintf("%s → %s", entryName, label)
			}
			add(p, exitCredential(cl, *ch, s), label, !firstSink)
			firstSink = false
		}
	}
	return out, nil
}
