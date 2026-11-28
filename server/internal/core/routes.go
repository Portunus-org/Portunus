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

// deriveChainUUID is the deterministic credential of a client's extra chain:
// derived from the client's own uuid and the chain, so it survives
// re-renders and client edits without changing working links.
func deriveChainUUID(base string, chainID int64) string {
	sum := sha256.Sum256([]byte(base + ":chain:" + strconv.FormatInt(chainID, 10)))
	b := sum[:16]
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// chainCredential is the credential that carries a client's traffic on
// chainID: the base uuid for the client's first chain, a derived one for any
// additional chain.
func chainCredential(cl *store.ClientRow, chainIDs []int64, chainID int64) string {
	if len(chainIDs) == 0 || chainIDs[0] == chainID {
		return cl.UUID
	}
	return deriveChainUUID(cl.UUID, chainID)
}

// chainUserName is the sing-box auth_user name of clientID on chainID.
func chainUserName(clientID int64, chainIDs []int64, chainID int64) string {
	base := "c" + strconv.FormatInt(clientID, 10)
	if len(chainIDs) > 0 && chainIDs[0] != chainID {
		return base + "-c" + strconv.FormatInt(chainID, 10)
	}
	return base
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

func (c *Core) mainServerName(ctx context.Context) string {
	if loc := c.ServerLocation(ctx); loc != nil && loc.City != "" {
		return loc.City
	}
	return "Main"
}

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

	chainIDs := cl.Chains()
	if len(chainIDs) == 0 {
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

	allChains, err := c.Chains(ctx)
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
		if id == 0 {
			return false
		}
		n, ok := nodesByID[id]
		return !ok || c.nodeUnusable(n)
	}
	isEntry := func(id int64) bool {
		n, ok := nodesByID[id]
		return ok && n.IsEntry
	}

	// Render one link per dialable entry point for each assigned chain:
	// failover between multiple exits inside a chain happens on the server,
	// so the client sees one clean connection per chain (per entry).
	broken := make([]int64, 0)
	for _, cid := range chainIDs {
		ch, ok := chainByID(allChains, cid)
		if !ok || !c.chainWorks(ch, unusable, isEntry) {
			broken = append(broken, cid)
			continue
		}

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
		if ch.isSource(0) || ch.mainIsDirectEntry() {
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
			broken = append(broken, cid)
			continue
		}

		cred := chainCredential(cl, chainIDs, cid)
		onlyUUID := cred != cl.UUID
		for _, id := range entryIDs {
			idx, ok := pointIdx[id]
			if !ok {
				continue
			}
			p := points[idx]
			entryName := c.mainServerName(ctx)
			if id != 0 {
				entryName = nodesByID[id].Name
			}
			label := ch.Name
			if entryName != "" && entryName != ch.Name {
				label = fmt.Sprintf("%s → %s", entryName, ch.Name)
			}
			add(p, cred, label, onlyUUID)
		}
	}

	if len(broken) > 0 {
		for i := range rows {
			if err := addPoint(rows[i].ID); err != nil {
				return nil, err
			}
		}
		for _, cid := range broken {
			cred := chainCredential(cl, chainIDs, cid)
			onlyUUID := cred != cl.UUID
			label := ""
			if ch, ok := chainByID(allChains, cid); ok && len(chainIDs) > 1 {
				label = ch.Name
			}
			for _, p := range points {
				add(p, cred, label, onlyUUID)
			}
		}
	}
	return out, nil
}
