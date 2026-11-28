package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/backup"
	"github.com/Portunus-org/Portunus/server/internal/db"
	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/secrets"
	"github.com/Portunus-org/Portunus/server/internal/singbox"
	"github.com/Portunus-org/Portunus/server/internal/store"
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
	chains, err := c.store.ListChains(ctx)
	if err != nil {
		return Node{}, err
	}
	for _, ch := range chains {
		if ch.DefaultAction == proxy.RouteNode && ch.DefaultNodeID != nil && *ch.DefaultNodeID == id {
			if err := c.store.SetChainDefault(ctx, ch.ID, proxy.RouteDirect, nil); err != nil {
				return Node{}, err
			}
		}
	}
	return n, c.store.DeleteNode(ctx, id)
}

// WriteBackup writes a complete tar.gz backup of the panel's database, master
// key and certificate directories to w.
func (c *Core) WriteBackup(ctx context.Context, w io.Writer) error {
	if c.backupSrc.DataDir == "" {
		return errors.New("backups are not configured")
	}
	if !c.offline {
		c.applyMu.Lock()
		if c.mgr.Status().PID != 0 {
			c.flushStatsLocked(ctx)
		}
		c.applyMu.Unlock()
	}
	src := c.backupSrc
	src.Snapshot = c.store.Snapshot
	return backup.Write(ctx, w, src)
}

// RestoreBackup validates a backup archive (plain .tar.gz or sealed with a
// recovery key) and replaces the panel's database, master key and certificate
// storage in place.
func (c *Core) RestoreBackup(ctx context.Context, archive []byte, recoveryKey string) error {
	if c.backupSrc.DataDir == "" {
		return errors.New("backups are not configured")
	}
	if len(archive) == 0 {
		return invalid("file", "bad_backup")
	}
	if backup.Sealed(archive) {
		key := strings.TrimSpace(recoveryKey)
		if key == "" {
			return invalid("key", "key_required")
		}
		opened, err := backup.Open(archive, key)
		if err != nil {
			return invalid("key", "wrong_key")
		}
		archive = opened
	}

	tmpDir, err := os.MkdirTemp(c.backupSrc.DataDir, "backup-restore-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := backup.Unpack(bytes.NewReader(archive), tmpDir); err != nil {
		return invalid("file", "bad_backup")
	}
	dbName := filepath.Base(c.backupSrc.DBPath)
	keyName := filepath.Base(c.backupSrc.MasterKeyPath)
	tmpKeyPath := filepath.Join(tmpDir, keyName)
	tmpDBPath := filepath.Join(tmpDir, dbName)
	if _, err := os.Stat(tmpKeyPath); err != nil {
		return invalid("file", "bad_backup")
	}
	if _, err := os.Stat(tmpDBPath); err != nil {
		return invalid("file", "bad_backup")
	}
	tmpBox, _, err := secrets.LoadOrCreate(tmpKeyPath)
	if err != nil {
		return invalid("file", "bad_backup")
	}

	discardLog := slog.New(slog.DiscardHandler)
	tmpSQL, err := db.Open(ctx, tmpDBPath, discardLog)
	if errors.Is(err, db.ErrNewer) {
		return invalid("file", "newer_version")
	}
	if err != nil {
		return invalid("file", "bad_backup")
	}

	mustChange, err := validateRestoredDB(ctx, tmpSQL, tmpBox, tmpDir, discardLog)
	if err != nil {
		tmpSQL.Close()
		return err
	}
	_, _ = tmpSQL.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	if err := tmpSQL.Close(); err != nil {
		return invalid("file", "bad_backup")
	}

	if !c.offline {
		_ = c.mgr.Apply(ctx, nil)
	}

	err = c.store.ReplaceFrom(ctx, tmpDBPath, func() error {
		if err := tmpBox.Save(c.backupSrc.MasterKeyPath); err != nil {
			return err
		}
		c.box.Replace(tmpBox)
		return c.replaceBackupFiles(tmpDir, dbName, keyName, mustChange)
	})
	if err != nil {
		return err
	}
	_ = c.MarkSetupCompleted(ctx)

	caMu.Lock()
	c.ca = nil
	caMu.Unlock()

	c.sso.mu.Lock()
	c.sso.provider = nil
	c.sso.key = ""
	c.sso.mu.Unlock()

	c.online.mu.Lock()
	clear(c.online.seen)
	clear(c.online.read)
	c.online.mu.Unlock()

	c.nodeMu.Lock()
	for id, cl := range c.nodeClients {
		cl.Close()
		delete(c.nodeClients, id)
	}
	clear(c.nodeStatus)
	clear(c.exitHealth)
	clear(c.chainHealth)
	c.nodeMu.Unlock()

	c.standbyMu.Lock()
	c.standbyBlob = nil
	c.standbyAt = time.Time{}
	clear(c.standbySent)
	c.standbyCfg = [sha256.Size]byte{}
	c.standbyMu.Unlock()

	c.geoMu.Lock()
	clear(c.geoTried)
	c.geoMu.Unlock()

	if !c.offline {
		if key, err := os.ReadFile(filepath.Join(c.backupSrc.DataDir, "sing-box", "quic-reset.key")); err == nil && len(key) == 32 {
			c.mgr.Env = []string{"QUIC_GO_STATELESS_RESET_KEY=" + base64.StdEncoding.EncodeToString(key)}
		}
		c.renewPrivateCertificates(ctx)
		c.Reconcile(ctx)
		c.KickNodes()
	}
	return nil
}

func validateRestoredDB(ctx context.Context, sqlDB *sql.DB, box *secrets.Box, tmpDir string, log *slog.Logger) (bool, error) {
	st := store.New(sqlDB)
	ok, err := st.IntegrityCheck(ctx)
	if err != nil || !ok {
		return false, invalid("file", "bad_backup")
	}
	users, err := st.Users(ctx)
	if err != nil {
		return false, invalid("file", "bad_backup")
	}
	hasAdmin := false
	mustChange := false
	for _, u := range users {
		if u.Role == store.RoleAdmin && !u.Disabled {
			hasAdmin = true
		}
		if u.MustChangePassword {
			mustChange = true
		}
	}
	if !hasAdmin {
		return false, invalid("file", "bad_backup")
	}
	tmpCore := New(st, box, singbox.New("", tmpDir, log), Options{Offline: true}, log)
	if _, err := tmpCore.Inbounds(ctx); err != nil {
		return false, invalid("file", "bad_backup")
	}
	if _, err := tmpCore.Nodes(ctx); err != nil {
		return false, invalid("file", "bad_backup")
	}
	if _, err := tmpCore.Chains(ctx); err != nil {
		return false, invalid("file", "bad_backup")
	}
	for _, key := range []string{settingCAKey, settingPanelKey, settingSSOSecret, settingRecoveryKey, settingCloudflareToken, settingAccessKey} {
		sealed, err := st.Setting(ctx, key)
		if err != nil {
			return false, invalid("file", "bad_backup")
		}
		if sealed != "" {
			if _, err := box.Open(sealed, "setting:"+key); err != nil {
				return false, invalid("file", "bad_backup")
			}
		}
	}
	return mustChange, nil
}

func (c *Core) replaceBackupFiles(tmpDir, dbName, keyName string, mustChange bool) error {
	for _, sub := range []string{
		filepath.Join(c.backupSrc.DataDir, "sing-box", "acme"),
		filepath.Join(c.backupSrc.DataDir, "sing-box", "panel-cert"),
		filepath.Join(c.backupSrc.DataDir, "panel-acme"),
	} {
		_ = os.RemoveAll(sub)
	}
	for _, dir := range []string{c.acme.StorageDir, c.panelCertDir, c.panelACMEDir} {
		if dir != "" {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
		}
	}
	if !mustChange {
		_ = os.Remove(filepath.Join(c.backupSrc.DataDir, "initial-admin-password"))
	}
	return filepath.WalkDir(tmpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(tmpDir, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == keyName || strings.HasPrefix(rel, dbName) || rel == "initial-admin-password" {
			return nil
		}
		dst := filepath.Join(c.backupSrc.DataDir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o700)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o600)
	})
}
