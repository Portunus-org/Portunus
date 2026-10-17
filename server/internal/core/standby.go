package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/backup"
	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

// Standby: the panel lives on one server. So that losing that server is
// not losing the installation, nodes marked "keeps a copy of the panel"
// receive a backup of it after every change and at least every
// standbyEvery, sealed with the recovery key.
// A node cannot read its copy; the operator, who has the key, can turn
// that node into the main server with `portunus restore` (see PromoteNode).
// Nothing takes over by itself: two panels over one database would fight
// for the nodes, and telling "the main server is gone" from "I cannot
// reach it" needs a third party this installation does not have.
const standbyEvery = 5 * time.Minute

const settingRecoveryKey = "recovery_key" // encrypted

// RecoveryKey is the key that opens the copies of the panel kept on nodes,
// made up on first use. The operator has to keep it somewhere else: it is
// in the database, and the database is what will be gone.
func (c *Core) RecoveryKey(ctx context.Context) (string, error) {
	sealed, err := c.store.Setting(ctx, settingRecoveryKey)
	if err != nil {
		return "", err
	}
	if sealed != "" {
		if key, err := c.box.Open(sealed, "setting:"+settingRecoveryKey); err == nil {
			return key, nil
		}
	}
	key := backup.NewRecoveryKey()
	return key, c.store.SetSetting(ctx, settingRecoveryKey, c.box.Seal(key, "setting:"+settingRecoveryKey))
}

// noteConfig is told the configuration every time it is rendered. When it
// has changed, something was edited (clients, connections, routes, nodes),
// and the standby nodes get a new copy at the next round instead of waiting
// for standbyEvery: what the operator has just done should not be what is
// missing after a loss.
func (c *Core) noteConfig(cfg []byte) {
	sum := sha256.Sum256(cfg)
	c.standbyMu.Lock()
	defer c.standbyMu.Unlock()
	if sum != c.standbyCfg {
		c.standbyCfg, c.standbyBlob = sum, nil
		clear(c.standbySent)
	}
}

// sealedCopy is the current backup of the panel, made at most once per
// standbyEvery however many nodes take it.
func (c *Core) sealedCopy(ctx context.Context) ([]byte, error) {
	c.standbyMu.Lock()
	defer c.standbyMu.Unlock()
	if c.standbyBlob != nil && c.now().Sub(c.standbyAt) < standbyEvery {
		return c.standbyBlob, nil
	}
	if c.backupSrc.DataDir == "" {
		return nil, errors.New("backups are not configured")
	}
	key, err := c.RecoveryKey(ctx)
	if err != nil {
		return nil, err
	}
	src := c.backupSrc
	src.Snapshot = c.store.Snapshot
	var archive bytes.Buffer
	if err := backup.Write(ctx, &archive, src); err != nil {
		return nil, err
	}
	blob, err := backup.Seal(archive.Bytes(), key)
	if err != nil {
		return nil, err
	}
	c.standbyBlob, c.standbyAt = blob, c.now()
	return blob, nil
}

// syncPanelCopy keeps node n's copy of the panel fresh, or removes it from
// a node that is no longer to have one. It returns when the node's copy was
// made (nil: it has none).
func (c *Core) syncPanelCopy(ctx context.Context, n *Node, cl *agent.Client, has *agent.PanelCopy) *time.Time {
	if !n.Settings.Standby || !n.Enabled {
		if has != nil {
			if _, err := cl.SetPanelCopy(ctx, n.ID, nil); err != nil {
				c.log.Error("remove the panel's copy", "node", n.Name, "err", err)
				return &has.At
			}
			c.log.Info("panel copy removed", "node", n.Name)
		}
		return nil
	}
	c.standbyMu.Lock()
	sent, ok := c.standbySent[n.ID]
	c.standbyMu.Unlock()
	if has != nil && has.Node == n.ID && ok && c.now().Sub(sent) < standbyEvery {
		return &sent
	}
	blob, err := c.sealedCopy(ctx)
	if err == nil {
		_, err = cl.SetPanelCopy(ctx, n.ID, blob)
	}
	if err != nil {
		c.log.Error("send the panel's copy", "node", n.Name, "err", err)
		if has != nil {
			return &has.At
		}
		return nil
	}
	now := c.now()
	c.standbyMu.Lock()
	c.standbySent[n.ID] = now
	c.standbyMu.Unlock()
	return &now
}

// PromoteNode rewrites a restored database so that the server which was
// node id becomes the main server:
//
//   - connections take over the certificates that server had as a backup
//     entry, so every link and profile clients already hold for it keeps
//     working unchanged;
//   - its address becomes the address clients are given;
//   - routes that sent traffic through it become direct: for clients that
//     enter here, "through that server" is where the traffic already is
//     (from another entry it now leaves from that entry);
//   - the node itself is removed: a server is not a node of itself.
//
// Clients that were bound to it as an exit are bound anew on the first
// start. For use on a database nothing else has open.
func (c *Core) PromoteNode(ctx context.Context, id int64) (Node, error) {
	n, err := c.Node(ctx, id)
	if err != nil {
		return Node{}, err
	}
	if n.Kind != NodeAgent {
		return Node{}, errors.New("only a server of this installation can become the main one")
	}
	inbounds, err := c.Inbounds(ctx)
	if err != nil {
		return Node{}, err
	}
	for _, in := range inbounds {
		if !in.Protocol.UsesTLSCert() {
			continue
		}
		set, sec := in.Settings, in.Secret
		// A public certificate was for the old server's domain; what
		// clients know this server by is its private certificate.
		set.TLSMode, set.CertificatePath, set.KeyPath = proxy.TLSSelfSigned, "", ""
		set.ServerName = n.Address
		key := strconv.FormatInt(in.ID, 10)
		if cert, ok := n.Settings.EntryCerts[key]; ok && n.IsEntry && n.Secret.EntryKeys[key] != "" {
			set.Certificate, set.CertSHA256, set.CACertificate = cert.Certificate, cert.CertSHA256, cert.CACertificate
			sec.TLSKey = n.Secret.EntryKeys[key]
		} else {
			ca, err := c.CA(ctx)
			if err != nil {
				return Node{}, err
			}
			if err := proxy.IssuePrivate(&set, &sec, ca); err != nil {
				return Node{}, err
			}
		}
		if _, err := c.saveInbound(ctx, in.ID, in.Protocol, in.Name, in.Port, in.Enabled, set, sec); err != nil {
			return Node{}, err
		}
	}
	// Subscriptions published at the old server's address move along.
	old, _ := c.PublicHost(ctx)
	if base, _ := c.SubscriptionBaseURL(ctx); base != "" && old != "" {
		if u, err := url.Parse(base); err == nil && strings.EqualFold(u.Hostname(), old) {
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(n.Address, port)
			} else {
				u.Host = n.Address
			}
			if err := c.store.SetSetting(ctx, settingSubBaseURL, u.String()); err != nil {
				return Node{}, err
			}
		}
	}
	if err := c.store.SetSetting(ctx, settingPublicHost, n.Address); err != nil {
		return Node{}, err
	}
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return Node{}, err
	}
	for _, r := range rules {
		if r.Action == proxy.RouteNode && r.NodeID != nil && *r.NodeID == id {
			r.Action, r.NodeID = proxy.RouteDirect, nil
			if err := c.store.UpdateRouteRule(ctx, r); err != nil {
				return Node{}, err
			}
		}
	}
	if act, node := c.DefaultRoute(ctx); act == proxy.RouteNode && node == id {
		if err := c.store.SetSetting(ctx, settingRouteDefault, proxy.RouteDirect); err != nil {
			return Node{}, err
		}
	}
	return n, c.store.DeleteNode(ctx, id)
}
