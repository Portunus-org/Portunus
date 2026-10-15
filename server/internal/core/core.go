// Package core is the application layer between the HTTP API and the
// lower-level packages: it validates input, keeps key material encrypted at
// rest, decides which clients may connect, and keeps the running sing-box
// config in sync with the database.
package core

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/Portunus-org/Portunus/server/internal/backup"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/secrets"
	"github.com/Portunus-org/Portunus/server/internal/singbox"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

// ValidationError is returned for bad user input; the API maps it to 400.
type ValidationError struct {
	Field string
	Code  string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Code }

func invalid(field, code string) error { return &ValidationError{Field: field, Code: code} }

// ReservedPort is a TCP port the panel itself uses; inbounds may not take it.
type ReservedPort struct {
	Port int
	What string
}

type Core struct {
	store    *store.Store
	box      *secrets.Box
	mgr      *singbox.Manager
	stats    *singbox.StatsClient
	statsAPI string
	reserved []ReservedPort
	acme     ACMEPaths
	ca       *proxy.CA // cached; see CA()

	// subListen is PORTUNUS_SUB_LISTEN; when set it fixes the subscription
	// listener and the port setting is ignored.
	subListen string
	log       *slog.Logger
	now       func() time.Time

	nodeRuntime
	online  online
	updates updates
	sso     ssoRuntime
	// beforeApply runs before a configuration is handed to sing-box.
	beforeApply  func(context.Context)
	offline      bool
	panelCertDir string
	panelACMEDir string
	hostPorts    func() (tcp, udp map[int]bool)
	// listHTTP replaces the list downloader in tests.
	listHTTP *http.Client

	// applyMu serialises config generation and stats flushing.
	applyMu sync.Mutex
	// lastApplyErr is the most recent config apply failure, shown in the UI.
	lastApplyErr string
}

// ACMEPaths locates sing-box's ACME storage. DataDirectory is relative to
// sing-box's working directory (what goes into the config); StorageDir is
// the same directory as seen by the panel.
type ACMEPaths struct {
	DataDirectory string
	StorageDir    string
	// Directory overrides the ACME server (tests, other CAs); empty means
	// Let's Encrypt.
	Directory string
}

// Options configure a Core.
type Options struct {
	StatsAPI string
	// UpdateSource is the image repository releases are published to; the
	// panel reads its tags to learn of a newer version. Empty: never asked.
	UpdateSource string
	// UpdateDir is the directory shared with the updater next to the panel.
	UpdateDir string
	// BeforeUpdate is where a copy of the database is left before an update
	// is installed; empty: none is made.
	BeforeUpdate string
	// PanelCertDir is where the panel puts the certificate it serves its
	// own HTTPS with (PanelCertFiles); empty: inbounds never share it.
	PanelCertDir string
	// PanelACMEDir is the panel's cache of the certificates it obtained
	// from Let's Encrypt, for the settings to list and remove them.
	PanelACMEDir string
	// ControlAPI is the loopback address of sing-box's control API, used
	// to measure exits; empty disables that.
	ControlAPI string
	// ListHTTP replaces the downloader of route lists (tests).
	ListHTTP *http.Client
	// HostPorts reports the TCP and UDP ports something already listens on
	// on this host, so an inbound is not put on a port another program
	// holds. Nil skips the check (tests).
	HostPorts func() (tcp, udp map[int]bool)
	// Offline: only edit the database, never touch sing-box or the nodes.
	// For commands that run next to a live panel, which applies the change
	// at its next round.
	Offline bool
	// GeoURL is the geolocation service queried as GeoURL + IP, answering
	// in the ipwho.is format; empty turns lookups off.
	GeoURL string
	// NodeImage is the container image new nodes are started from.
	NodeImage string
	// Backup says where the panel's own data is, for the copies standby
	// nodes keep (Snapshot is filled in). Zero: no copies are made.
	Backup    backup.Source
	Reserved  []ReservedPort
	ACME      ACMEPaths
	SubListen string
}

func New(st *store.Store, box *secrets.Box, mgr *singbox.Manager, o Options, log *slog.Logger) *Core {
	c := &Core{
		acme:     o.ACME,
		store:    st,
		box:      box,
		mgr:      mgr,
		stats:    singbox.NewStatsClient(o.StatsAPI),
		statsAPI: o.StatsAPI,
		reserved: append(o.Reserved,
			ReservedPort{Port: PanelInnerPort, What: "panel (inner HTTPS)"},
			ReservedPort{Port: DecoyPort, What: "cover site"}),
		subListen:    o.SubListen,
		log:          log,
		now:          time.Now,
		listHTTP:     o.ListHTTP,
		updates:      updates{source: o.UpdateSource, dir: o.UpdateDir, before: o.BeforeUpdate, client: &http.Client{Timeout: 20 * time.Second}},
		offline:      o.Offline,
		panelCertDir: o.PanelCertDir,
		panelACMEDir: o.PanelACMEDir,
		hostPorts:    o.HostPorts,
		nodeRuntime: nodeRuntime{
			nodeStatus:  map[int64]NodeStatus{},
			exitHealth:  map[int64]exitHealth{},
			backupSrc:   o.Backup,
			standbySent: map[int64]time.Time{},
			nodeClients: map[int64]*agent.Client{},
			nodeKick:    make(chan struct{}, 1),
			nodeImage:   o.NodeImage,
			controlAPI:  o.ControlAPI,
			geoURL:      o.GeoURL,
			geoTried:    map[int64]time.Time{},
		},
	}
	// sing-box counters reset on reload; collect them first.
	mgr.BeforeReload = func(ctx context.Context) { c.flushStatsLocked(ctx) }
	return c
}

// ---------------------------------------------------------------- inbounds

func (c *Core) decodeInbound(row *store.InboundRow) (proxy.Inbound, error) {
	in := proxy.Inbound{
		ID:       row.ID,
		Tag:      row.Tag,
		Name:     row.Name,
		Protocol: proxy.Protocol(row.Protocol),
		Port:     row.Port,
		Enabled:  row.Enabled,
	}
	if err := json.Unmarshal([]byte(row.Settings), &in.Settings); err != nil {
		return in, fmt.Errorf("inbound %d settings: %w", row.ID, err)
	}
	if row.Secret != "" {
		pt, err := c.box.Open(row.Secret, secretAAD(row.ID))
		if err != nil {
			return in, fmt.Errorf("inbound %d secret: %w", row.ID, err)
		}
		if err := json.Unmarshal([]byte(pt), &in.Secret); err != nil {
			return in, fmt.Errorf("inbound %d secret: %w", row.ID, err)
		}
	}
	return in, nil
}

func secretAAD(id int64) string { return "inbound:" + strconv.FormatInt(id, 10) }

func (c *Core) sealSecret(id int64, sec proxy.Secret) (string, error) {
	b, err := json.Marshal(sec)
	if err != nil {
		return "", err
	}
	return c.box.Seal(string(b), secretAAD(id)), nil
}

func (c *Core) Inbounds(ctx context.Context) ([]proxy.Inbound, error) {
	rows, err := c.store.ListInbounds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]proxy.Inbound, 0, len(rows))
	for _, r := range rows {
		in, err := c.decodeInbound(r)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, nil
}

func (c *Core) Inbound(ctx context.Context, id int64) (proxy.Inbound, error) {
	row, err := c.store.InboundByID(ctx, id)
	if err != nil {
		return proxy.Inbound{}, err
	}
	return c.decodeInbound(row)
}

// InboundInput is what the operator sets on create and update. Protocol is
// fixed after creation.
type InboundInput struct {
	Protocol          proxy.Protocol
	Name              string
	Port              int
	Enabled           bool
	RealityServer     string
	RealityServerPort int
	ServerName        string
	TLSMode           proxy.TLSMode
	CertificatePath   string
	KeyPath           string
	// StrictTLS: links verify a private certificate normally (devices have
	// the CA installed) instead of pinning it.
	StrictTLS bool
	// Hysteria2
	MasqueradeURL string
	Obfs          bool
}

var hostnameRE = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)

func validHost(h string) bool {
	return len(h) <= 253 && (hostnameRE.MatchString(h) || net.ParseIP(h) != nil)
}

func (c *Core) validateInbound(ctx context.Context, id int64, in *InboundInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.RealityServer = strings.TrimSpace(strings.ToLower(in.RealityServer))
	in.ServerName = strings.TrimSpace(strings.ToLower(in.ServerName))

	if !in.Protocol.Valid() {
		return invalid("protocol", "unsupported")
	}
	if len(in.Name) > 64 {
		return invalid("name", "too_long")
	}
	if oddText(in.Name, false) {
		return invalid("name", "bad_characters")
	}
	if in.Port < 1 || in.Port > 65535 {
		return invalid("port", "out_of_range")
	}
	if in.Protocol.UsesTCP() {
		for _, r := range c.reserved {
			if r.Port == in.Port {
				return invalid("port", "reserved")
			}
		}
		// The panel itself, opened by its domain. VLESS can share the port
		// with it (below); nothing else can.
		if in.Port == c.PanelPort(ctx) && in.Protocol != proxy.VLESS {
			return invalid("port", "panel_port")
		}
	}
	switch in.Protocol {
	case proxy.VLESS:
		// On the panel's port the connection presents the panel's domain:
		// whoever is not its client is passed on to the panel's listener
		// and sees a real site with a real certificate.
		if pa, err := c.PanelAccess(ctx); err == nil && pa.Enabled && in.Port == pa.Port {
			in.RealityServer, in.RealityServerPort = pa.Domain, pa.Port
		}
		if in.RealityServer == "" {
			in.RealityServer = proxy.DefaultRealityServer
		}
		if in.RealityServerPort == 0 {
			in.RealityServerPort = 443
		}
		if !validHost(in.RealityServer) || net.ParseIP(in.RealityServer) != nil {
			return invalid("realityServer", "invalid_hostname")
		}
		if in.RealityServerPort < 1 || in.RealityServerPort > 65535 {
			return invalid("realityServerPort", "out_of_range")
		}
	case proxy.Hysteria2, proxy.TUIC, proxy.Trojan:
		if in.ServerName == "" {
			in.ServerName = proxy.DefaultServerName
		}
		if !validHost(in.ServerName) {
			return invalid("serverName", "invalid_hostname")
		}
		switch in.TLSMode {
		case proxy.TLSSelfSigned:
		case proxy.TLSACME:
			// Let's Encrypt needs a public domain that resolves to this server.
			if net.ParseIP(in.ServerName) != nil || !strings.Contains(in.ServerName, ".") || in.ServerName == proxy.DefaultServerName {
				return invalid("serverName", "domain_required")
			}
		case proxy.TLSFile:
			in.CertificatePath = strings.TrimSpace(in.CertificatePath)
			in.KeyPath = strings.TrimSpace(in.KeyPath)
			if !filepath.IsAbs(in.CertificatePath) {
				return invalid("certificatePath", "absolute_path_required")
			}
			if !filepath.IsAbs(in.KeyPath) {
				return invalid("keyPath", "absolute_path_required")
			}
			// sing-box runs as the same user, so what we can read, it can.
			info, err := readCertificate(in.CertificatePath)
			if err != nil {
				return invalid("certificatePath", "unreadable")
			}
			if f, err := os.Open(in.KeyPath); err != nil {
				return invalid("keyPath", "unreadable")
			} else {
				f.Close()
			}
			if !certCovers(info.names, in.ServerName) {
				return invalid("serverName", "not_in_certificate")
			}
		default:
			return invalid("tlsMode", "unsupported")
		}
	default:
		in.TLSMode = proxy.TLSSelfSigned
	}
	in.MasqueradeURL = strings.TrimSpace(in.MasqueradeURL)
	if in.Protocol != proxy.Hysteria2 {
		in.MasqueradeURL, in.Obfs = "", false
	} else if in.MasqueradeURL != "" {
		u, err := url.Parse(in.MasqueradeURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("masqueradeUrl", "invalid_url")
		}
	}

	// A TCP and a UDP protocol may share a port number; two TCP (or two UDP)
	// listeners may not.
	existing, err := c.store.ListInbounds(ctx)
	if err != nil {
		return err
	}
	// The HTTP-01 solver needs its TCP port whenever an inbound uses ACME.
	if p := c.acmeChallengePort(ctx); p != 0 && in.Protocol.UsesTCP() && in.Port == p {
		for _, e := range existing {
			if e.ID != id && strings.Contains(e.Settings, `"tlsMode":"acme"`) {
				return invalid("port", "reserved")
			}
		}
	}
	for _, e := range existing {
		if e.ID == id || e.Port != in.Port {
			continue
		}
		p := proxy.Protocol(e.Protocol)
		if (p.UsesTCP() && in.Protocol.UsesTCP()) || (p.UsesUDP() && in.Protocol.UsesUDP()) {
			return invalid("port", "in_use")
		}
	}
	// The panel's own listeners that are set in the panel rather than fixed
	// at start: subscriptions, and the port certificates are verified on.
	if in.Protocol.UsesTCP() {
		if c.subListen == "" && in.Port == c.SubscriptionPort(ctx) {
			return invalid("port", "reserved")
		}
		if in.Port == c.acmeListenPort(ctx) {
			return invalid("port", "reserved")
		}
	}
	// A port another program on this server already listens on: sing-box
	// would fail to start and take every connection down with it. The
	// inbound's own current port is, of course, ours.
	if c.hostPorts != nil {
		own := false
		for _, e := range existing {
			if e.ID == id && e.Port == in.Port {
				own = true
			}
		}
		// So is the panel's port, to a VLESS inbound that will share it.
		if in.Protocol == proxy.VLESS && in.Port == c.PanelPort(ctx) {
			own = true
		}
		if !own {
			tcp, udp := c.hostPorts()
			if (in.Protocol.UsesTCP() && tcp[in.Port]) || (in.Protocol.UsesUDP() && udp[in.Port]) {
				return invalid("port", "busy_on_host")
			}
		}
	}
	// A backup entry point serves the same ports next to its own services.
	if in.Protocol.UsesTCP() {
		nodes, err := c.store.ListNodes(ctx)
		if err != nil {
			return err
		}
		for _, row := range nodes {
			if row.Kind != NodeAgent || !row.IsEntry {
				continue
			}
			n, err := c.decodeNode(row)
			if err != nil {
				return err
			}
			if in.Port == n.Settings.AgentPort || (n.IsExit && in.Port == n.Settings.HopPort) {
				return invalid("port", "node_port_conflict")
			}
		}
	}
	return nil
}

func (c *Core) CreateInbound(ctx context.Context, in InboundInput) (proxy.Inbound, error) {
	if err := c.validateInbound(ctx, 0, &in); err != nil {
		return proxy.Inbound{}, err
	}
	ca, err := c.CA(ctx)
	if err != nil {
		return proxy.Inbound{}, err
	}
	set, sec, err := proxy.Provision(in.Protocol, proxy.ProvisionOptions{
		StrictTLS:         in.StrictTLS,
		CA:                ca,
		RealityServer:     in.RealityServer,
		RealityServerPort: in.RealityServerPort,
		ServerName:        in.ServerName,
		TLSMode:           in.TLSMode,
		CertificatePath:   in.CertificatePath,
		KeyPath:           in.KeyPath,
		MasqueradeURL:     in.MasqueradeURL,
		Obfs:              in.Obfs,
	})
	if err != nil {
		return proxy.Inbound{}, err
	}
	settings, _ := json.Marshal(set)
	row := &store.InboundRow{
		Tag:      proxy.Tag(in.Protocol, in.Port),
		Name:     in.Name,
		Protocol: string(in.Protocol),
		Port:     in.Port,
		Enabled:  in.Enabled,
		Settings: string(settings),
	}
	err = c.store.CreateInbound(ctx, row, func(id int64) (string, error) { return c.sealSecret(id, sec) })
	if errors.Is(err, store.ErrConflict) {
		return proxy.Inbound{}, invalid("port", "in_use")
	}
	if err != nil {
		return proxy.Inbound{}, err
	}
	c.Reconcile(ctx)
	return c.decodeInbound(row)
}

func (c *Core) UpdateInbound(ctx context.Context, id int64, in InboundInput) (proxy.Inbound, error) {
	cur, err := c.Inbound(ctx, id)
	if err != nil {
		return proxy.Inbound{}, err
	}
	in.Protocol = cur.Protocol
	if err := c.validateInbound(ctx, id, &in); err != nil {
		return proxy.Inbound{}, err
	}

	set, sec := cur.Settings, cur.Secret
	switch cur.Protocol {
	case proxy.VLESS:
		set.RealityServer, set.RealityServerPort = in.RealityServer, in.RealityServerPort
	case proxy.Hysteria2, proxy.TUIC, proxy.Trojan:
		// The certificate is issued for the server name; a new name needs a
		// new certificate (and clients must re-import).
		if in.ServerName != set.ServerName {
			ca, err := c.CA(ctx)
			if err != nil {
				return proxy.Inbound{}, err
			}
			set.ServerName = in.ServerName
			if err := proxy.IssuePrivate(&set, &sec, ca); err != nil {
				return proxy.Inbound{}, err
			}
		}
		set.TLSMode = in.TLSMode
		set.StrictTLS = in.StrictTLS
		set.CertificatePath, set.KeyPath = "", ""
		if in.TLSMode == proxy.TLSFile {
			set.CertificatePath, set.KeyPath = in.CertificatePath, in.KeyPath
		}
		if cur.Protocol == proxy.Hysteria2 {
			set.MasqueradeURL = in.MasqueradeURL
			switch {
			case in.Obfs && set.ObfsPassword == "":
				set.ObfsPassword = proxy.NewObfsPassword()
			case !in.Obfs:
				set.ObfsPassword = ""
			}
		}
	}
	return c.saveInbound(ctx, id, in.Protocol, in.Name, in.Port, in.Enabled, set, sec)
}

// RotateInboundKeys replaces the inbound's key material (REALITY key pair,
// TLS certificate, Shadowsocks server key). Every client must re-import.
func (c *Core) RotateInboundKeys(ctx context.Context, id int64) (proxy.Inbound, error) {
	cur, err := c.Inbound(ctx, id)
	if err != nil {
		return proxy.Inbound{}, err
	}
	ca, err := c.CA(ctx)
	if err != nil {
		return proxy.Inbound{}, err
	}
	set, sec, err := proxy.Provision(cur.Protocol, proxy.ProvisionOptions{
		StrictTLS:         cur.Settings.StrictTLS,
		CA:                ca,
		RealityServer:     cur.Settings.RealityServer,
		RealityServerPort: cur.Settings.RealityServerPort,
		ServerName:        cur.Settings.ServerName,
		TLSMode:           cur.Settings.TLSMode,
		CertificatePath:   cur.Settings.CertificatePath,
		KeyPath:           cur.Settings.KeyPath,
		MasqueradeURL:     cur.Settings.MasqueradeURL,
		Obfs:              cur.Settings.ObfsPassword != "",
	})
	if err != nil {
		return proxy.Inbound{}, err
	}
	return c.saveInbound(ctx, id, cur.Protocol, cur.Name, cur.Port, cur.Enabled, set, sec)
}

func (c *Core) saveInbound(ctx context.Context, id int64, p proxy.Protocol, name string, port int, enabled bool, set proxy.Settings, sec proxy.Secret) (proxy.Inbound, error) {
	settings, _ := json.Marshal(set)
	sealed, err := c.sealSecret(id, sec)
	if err != nil {
		return proxy.Inbound{}, err
	}
	row := &store.InboundRow{
		ID:       id,
		Tag:      proxy.Tag(p, port),
		Name:     name,
		Port:     port,
		Enabled:  enabled,
		Settings: string(settings),
		Secret:   sealed,
	}
	if err := c.store.UpdateInbound(ctx, row); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return proxy.Inbound{}, invalid("port", "in_use")
		}
		return proxy.Inbound{}, err
	}
	c.Reconcile(ctx)
	return c.Inbound(ctx, id)
}

func (c *Core) DeleteInbound(ctx context.Context, id int64) error {
	if err := c.store.DeleteInbound(ctx, id); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// certCovers reports whether a certificate for names is valid for host,
// honouring single-label wildcards.
func certCovers(names []string, host string) bool {
	for _, n := range names {
		if strings.EqualFold(n, host) {
			return true
		}
		if rest, ok := strings.CutPrefix(n, "*."); ok {
			if _, parent, found := strings.Cut(host, "."); found && strings.EqualFold(parent, rest) {
				return true
			}
		}
	}
	return false
}

// ----------------------------------------------------------------- clients

type ClientStatus string

const (
	ClientActive   ClientStatus = "active"
	ClientDisabled ClientStatus = "disabled"
	ClientExpired  ClientStatus = "expired"
	ClientLimited  ClientStatus = "limited" // traffic limit reached
)

func (c *Core) ClientStatus(cl *store.ClientRow) ClientStatus {
	switch {
	case !cl.Enabled:
		return ClientDisabled
	case cl.ExpiresAt != nil && !c.now().Before(*cl.ExpiresAt):
		return ClientExpired
	case cl.TrafficLimit > 0 && cl.Upload+cl.Download >= cl.TrafficLimit:
		return ClientLimited
	}
	return ClientActive
}

func toProxyClient(cl *store.ClientRow) proxy.Client {
	return proxy.Client{ID: cl.ID, Name: cl.Name, UUID: cl.UUID, Password: cl.Password}
}

func (c *Core) Clients(ctx context.Context) ([]*store.ClientRow, error) {
	return c.store.ListClients(ctx)
}

func (c *Core) Client(ctx context.Context, id int64) (*store.ClientRow, error) {
	return c.store.ClientByID(ctx, id)
}

type ClientInput struct {
	Name         string
	Enabled      bool
	TrafficLimit int64
	ExpiresAt    *time.Time
	Note         string
	// Exit changes the exit the client is bound to: nil leaves it, 0 has
	// one chosen anew.
	Exit *int64
}

func validateClient(in *ClientInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Note = strings.TrimSpace(in.Note)
	if in.Name == "" {
		return invalid("name", "required")
	}
	if len(in.Name) > 64 {
		return invalid("name", "too_long")
	}
	if oddText(in.Name, false) {
		return invalid("name", "bad_characters")
	}
	if oddText(in.Note, true) {
		return invalid("note", "bad_characters")
	}
	if in.TrafficLimit < 0 {
		return invalid("trafficLimit", "negative")
	}
	if len(in.Note) > 500 {
		return invalid("note", "too_long")
	}
	return nil
}

// subToken is the secret part of a subscription URL: 256 random bits.
func subToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (c *Core) CreateClient(ctx context.Context, in ClientInput) (*store.ClientRow, error) {
	if err := validateClient(&in); err != nil {
		return nil, err
	}
	cl := &store.ClientRow{
		Name:         in.Name,
		Enabled:      in.Enabled,
		UUID:         proxy.NewUUID(),
		Password:     proxy.NewPassword(),
		SubToken:     subToken(),
		TrafficLimit: in.TrafficLimit,
		ExpiresAt:    in.ExpiresAt,
		Note:         in.Note,
	}
	// Checked before anything is created: a request that is refused leaves
	// nothing behind.
	if in.Exit != nil && *in.Exit != 0 {
		if n, err := c.store.NodeByID(ctx, *in.Exit); err != nil || !n.IsExit || !n.Enabled {
			return nil, invalid("exitId", "not_an_exit")
		}
	}
	if err := c.store.CreateClient(ctx, cl); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	if in.Exit != nil && *in.Exit != 0 {
		if err := c.setClientExit(ctx, cl, *in.Exit); err != nil {
			return nil, err
		}
	}
	c.Reconcile(ctx)
	return c.store.ClientByID(ctx, cl.ID)
}

func (c *Core) UpdateClient(ctx context.Context, id int64, in ClientInput) (*store.ClientRow, error) {
	if err := validateClient(&in); err != nil {
		return nil, err
	}
	cl, err := c.store.ClientByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cl.Name, cl.Enabled, cl.TrafficLimit, cl.ExpiresAt, cl.Note = in.Name, in.Enabled, in.TrafficLimit, in.ExpiresAt, in.Note
	if in.Exit != nil && (cl.ExitID == nil || *cl.ExitID != *in.Exit) {
		if err := c.setClientExit(ctx, cl, *in.Exit); err != nil {
			return nil, err
		}
	}
	if err := c.store.UpdateClient(ctx, cl); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, invalid("name", "taken")
		}
		return nil, err
	}
	c.Reconcile(ctx)
	return c.store.ClientByID(ctx, id)
}

// RotateClient issues new credentials and a new subscription URL, revoking
// everything the client had imported before.
func (c *Core) RotateClient(ctx context.Context, id int64) (*store.ClientRow, error) {
	cl, err := c.store.ClientByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cl.UUID, cl.Password, cl.SubToken = proxy.NewUUID(), proxy.NewPassword(), subToken()
	if err := c.store.UpdateClient(ctx, cl); err != nil {
		return nil, err
	}
	// New keys are a new start: the exit is chosen anew as well.
	if err := c.store.SetClientExit(ctx, id, nil); err != nil {
		return nil, err
	}
	c.Reconcile(ctx)
	return c.store.ClientByID(ctx, id)
}

func (c *Core) ResetClientTraffic(ctx context.Context, id int64) (*store.ClientRow, error) {
	if err := c.store.ResetTraffic(ctx, id); err != nil {
		return nil, err
	}
	c.Reconcile(ctx)
	return c.store.ClientByID(ctx, id)
}

func (c *Core) DeleteClient(ctx context.Context, id int64) error {
	if err := c.store.DeleteClient(ctx, id); err != nil {
		return err
	}
	c.Reconcile(ctx)
	return nil
}

// ---------------------------------------------------------------- settings

const settingPublicHost = "public_host"

// PublicHost is the address clients connect to (domain or IP).
func (c *Core) PublicHost(ctx context.Context) (string, error) {
	return c.store.Setting(ctx, settingPublicHost)
}

func (c *Core) SetPublicHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host != "" && !validHost(host) {
		return invalid("publicHost", "invalid_hostname")
	}
	return c.store.SetSetting(ctx, settingPublicHost, host)
}

const settingSubBaseURL = "subscription_base_url"

// SubscriptionBaseURL is the public origin subscription links are built on,
// e.g. https://sub.example.com (empty: subscriptions are not published).
func (c *Core) SubscriptionBaseURL(ctx context.Context) (string, error) {
	return c.store.Setting(ctx, settingSubBaseURL)
}

func (c *Core) SetSubscriptionBaseURL(ctx context.Context, raw string) error {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw != "" {
		u, err := url.Parse(raw)
		// No login in it either: this address goes into every client's
		// subscription link, and a password written there would go to all
		// of them.
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || oddText(raw, false) {
			return invalid("subscriptionBaseUrl", "invalid_url")
		}
	}
	return c.store.SetSetting(ctx, settingSubBaseURL, raw)
}

func (c *Core) ClientBySubToken(ctx context.Context, token string) (*store.ClientRow, error) {
	if len(token) < 32 {
		return nil, store.ErrNotFound
	}
	return c.store.ClientBySubToken(ctx, token)
}

const (
	settingACMEEmail       = "acme_email"
	settingACMEChallenge   = "acme_challenge"
	settingACMEHTTPPort    = "acme_http_port"
	settingCloudflareToken = "acme_cloudflare_token" // encrypted
)

// ACME challenge types.
const (
	// ChallengeHTTP answers HTTP-01 on TCP 80 directly.
	ChallengeHTTP = "http"
	// ChallengeProxy answers HTTP-01 on a local port; a reverse proxy that
	// owns port 80 forwards /.well-known/acme-challenge/ to it.
	ChallengeProxy = "proxy"
	// ChallengeCloudflare uses DNS-01 through the Cloudflare API: no ports.
	ChallengeCloudflare = "cloudflare"

	DefaultACMEHTTPPort = 8088
)

// ACMESettings configure certificate issuance for inbounds in ACME mode.
type ACMESettings struct {
	Email     string `json:"acmeEmail"`
	Challenge string `json:"acmeChallenge"`
	HTTPPort  int    `json:"acmeHttpPort"`
	// CloudflareToken: nil keeps the stored token. Never returned.
	CloudflareToken *string `json:"-"`
	// CloudflareTokenSet reports whether a token is stored.
	CloudflareTokenSet bool `json:"cloudflareTokenSet"`
}

func (c *Core) cloudflareToken(ctx context.Context) (string, error) {
	sealed, err := c.store.Setting(ctx, settingCloudflareToken)
	if err != nil || sealed == "" {
		return "", err
	}
	return c.box.Open(sealed, "setting:"+settingCloudflareToken)
}

func (c *Core) ACME(ctx context.Context) (ACMESettings, error) {
	var (
		out ACMESettings
		err error
	)
	if out.Email, err = c.store.Setting(ctx, settingACMEEmail); err != nil {
		return out, err
	}
	if out.Challenge, err = c.store.Setting(ctx, settingACMEChallenge); err != nil {
		return out, err
	}
	if out.Challenge == "" {
		out.Challenge = ChallengeHTTP
	}
	port, err := c.store.Setting(ctx, settingACMEHTTPPort)
	if err != nil {
		return out, err
	}
	if out.HTTPPort, _ = strconv.Atoi(port); out.HTTPPort == 0 {
		out.HTTPPort = DefaultACMEHTTPPort
	}
	sealed, err := c.store.Setting(ctx, settingCloudflareToken)
	out.CloudflareTokenSet = sealed != ""
	return out, err
}

func (c *Core) SetACME(ctx context.Context, s ACMESettings) error {
	s.Email = strings.TrimSpace(s.Email)
	if s.Email != "" {
		if _, err := mail.ParseAddress(s.Email); err != nil || strings.ContainsAny(s.Email, "<> ") {
			return invalid("acmeEmail", "invalid_email")
		}
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = DefaultACMEHTTPPort
	}
	// Whether or not the chosen check uses the port: a number that is not
	// a port is never stored.
	if s.HTTPPort < 1 || s.HTTPPort > 65535 {
		return invalid("acmeHttpPort", "out_of_range")
	}
	switch s.Challenge {
	case ChallengeHTTP:
	case ChallengeProxy:
		if s.HTTPPort < 1024 || s.HTTPPort > 65535 {
			return invalid("acmeHttpPort", "out_of_range")
		}
		for _, r := range c.reserved {
			if r.Port == s.HTTPPort {
				return invalid("acmeHttpPort", "reserved")
			}
		}
		if c.tcpPortTaken(ctx, s.HTTPPort) || (c.subListen == "" && s.HTTPPort == c.SubscriptionPort(ctx)) {
			return invalid("acmeHttpPort", "in_use")
		}
	case ChallengeCloudflare:
		if s.CloudflareToken == nil {
			if cur, _ := c.cloudflareToken(ctx); cur == "" {
				return invalid("cloudflareToken", "required")
			}
		}
	default:
		return invalid("acmeChallenge", "unsupported")
	}
	if s.CloudflareToken != nil {
		tok := strings.TrimSpace(*s.CloudflareToken)
		sealed := ""
		if tok != "" {
			sealed = c.box.Seal(tok, "setting:"+settingCloudflareToken)
		} else if s.Challenge == ChallengeCloudflare {
			return invalid("cloudflareToken", "required")
		}
		if err := c.store.SetSetting(ctx, settingCloudflareToken, sealed); err != nil {
			return err
		}
	}
	for k, v := range map[string]string{
		settingACMEEmail:     s.Email,
		settingACMEChallenge: s.Challenge,
		settingACMEHTTPPort:  strconv.Itoa(s.HTTPPort),
	} {
		if err := c.store.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	c.Reconcile(ctx)
	return nil
}

// acmeChallengePort is the TCP port the HTTP-01 solver occupies (0: none).
// tcpPortTaken reports whether an inbound listens on the TCP port.
func (c *Core) tcpPortTaken(ctx context.Context, port int) bool {
	rows, err := c.store.ListInbounds(ctx)
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Port == port && proxy.Protocol(r.Protocol).UsesTCP() {
			return true
		}
	}
	return false
}

// acmeListenPort is the TCP port sing-box answers certificate checks on,
// or 0 while no inbound gets its certificate from Let's Encrypt.
func (c *Core) acmeListenPort(ctx context.Context) int {
	ins, err := c.Inbounds(ctx)
	if err != nil {
		return 0
	}
	for _, in := range ins {
		if in.ACME() {
			return c.acmeChallengePort(ctx)
		}
	}
	return 0
}

func (c *Core) acmeChallengePort(ctx context.Context) int {
	s, err := c.ACME(ctx)
	if err != nil {
		return 0
	}
	switch s.Challenge {
	case ChallengeHTTP:
		return 80
	case ChallengeProxy:
		return s.HTTPPort
	}
	return 0
}

func (c *Core) acmeOptions(ctx context.Context) (proxy.ACMEOptions, error) {
	s, err := c.ACME(ctx)
	if err != nil {
		return proxy.ACMEOptions{}, err
	}
	o := proxy.ACMEOptions{DataDirectory: c.acme.DataDirectory, Email: s.Email, Directory: c.acme.Directory}
	switch s.Challenge {
	case ChallengeProxy:
		o.HTTPPort = s.HTTPPort
	case ChallengeCloudflare:
		if o.CloudflareToken, err = c.cloudflareToken(ctx); err != nil {
			return o, err
		}
	}
	return o, nil
}

// CertificateInfo describes the certificate ACME has issued for a domain.
type CertificateInfo struct {
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"notAfter"`
	names    []string
}

// CertificateFor describes the publicly trusted certificate an inbound
// serves: issued by ACME (nil while pending) or read from its file.
func (c *Core) CertificateFor(in proxy.Inbound) *CertificateInfo {
	switch {
	case in.ACME() && c.sharedCertificate(in.Settings.ServerName) != nil:
		return c.sharedCertificate(in.Settings.ServerName)
	case in.ACME():
		return c.Certificate(in.Settings.ServerName)
	case in.Protocol.UsesTLSCert() && in.Settings.TLSMode == proxy.TLSFile:
		info, _ := readCertificate(in.Settings.CertificatePath)
		return info
	}
	return nil
}

func readCertificate(path string) (*CertificateInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("no PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	issuer := cert.Issuer.CommonName
	if len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0] + " " + issuer
	}
	return &CertificateInfo{Issuer: strings.TrimSpace(issuer), NotAfter: cert.NotAfter.UTC(), names: cert.DNSNames}, nil
}

// Certificate returns the newest certificate sing-box has stored for domain,
// or nil while none has been issued yet. certmagic keeps them at
// certificates/<issuer>/<domain>/<domain>.crt.
func (c *Core) Certificate(domain string) *CertificateInfo {
	if c.acme.StorageDir == "" || domain == "" || strings.ContainsAny(domain, `/\`) {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(c.acme.StorageDir, "certificates", "*", domain, domain+".crt"))
	var best *CertificateInfo
	for _, path := range matches {
		if info, err := readCertificate(path); err == nil && (best == nil || info.NotAfter.After(best.NotAfter)) {
			best = info
		}
	}
	return best
}

// ------------------------------------------------------------ data plane

// ApplyError is the last error from generating or loading the config.
func (c *Core) ApplyError() string {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	if c.lastApplyErr != "" {
		return c.lastApplyErr
	}
	// A configuration that was accepted and then rolled back.
	return c.mgr.RejectedError()
}

// Reconcile renders the config from the database and hands it to the
// manager, which does nothing if it is unchanged. Errors are logged and
// kept for the UI rather than failing the request that triggered it: the
// database change stands, and the running config stays valid.
func (c *Core) Reconcile(ctx context.Context) {
	if c.offline {
		return
	}
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	// The nodes follow the same database; they are brought in line in the
	// background so that an unreachable one never delays a request.
	defer c.KickNodes()
	if err := c.reconcileLocked(ctx); err != nil {
		c.lastApplyErr = err.Error()
		c.log.Error("apply sing-box config", "err", err)
		return
	}
	c.lastApplyErr = ""
}

func (c *Core) reconcileLocked(ctx context.Context) error {
	if err := c.bindClients(ctx); err != nil {
		return err
	}
	inbounds, err := c.Inbounds(ctx)
	if err != nil {
		return err
	}
	if pa, err := c.PanelAccess(ctx); err == nil {
		for i := range inbounds {
			c.shareCertificate(&inbounds[i], pa)
			switch in := &inbounds[i]; {
			case in.Protocol == proxy.VLESS && pa.Enabled && in.Port == pa.Port:
				in.RealityDial = net.JoinHostPort("127.0.0.1", strconv.Itoa(PanelInnerPort))
			case in.Protocol == proxy.Trojan:
				in.Fallback = net.JoinHostPort("127.0.0.1", strconv.Itoa(DecoyPort))
			}
		}
	}
	// Whoever listens beside sing-box gets out of its way first (the panel
	// leaves a port it is about to share).
	if c.beforeApply != nil {
		c.beforeApply(ctx)
	}
	active, err := c.activeClients(ctx)
	if err != nil {
		return err
	}
	acme, err := c.acmeOptions(ctx)
	if err != nil {
		return err
	}
	routing, err := c.routing(ctx, 0)
	if err != nil {
		return err
	}
	cfg, err := proxy.ServerConfig(inbounds, active, c.statsAPI, acme, routing)
	if err != nil {
		return err
	}
	c.noteConfig(cfg)
	return c.mgr.Apply(ctx, cfg)
}

// activeClients are the clients allowed to connect right now.
func (c *Core) activeClients(ctx context.Context) ([]proxy.Client, error) {
	rows, err := c.store.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	var active []proxy.Client
	for _, r := range rows {
		if c.ClientStatus(r) == ClientActive {
			active = append(active, toProxyClient(r))
		}
	}
	return active, nil
}

// flushStatsLocked moves sing-box's per-user counters into the database.
// Callers hold applyMu (it runs from Reconcile via the manager hook).
func (c *Core) flushStatsLocked(ctx context.Context) {
	users, err := c.stats.QueryUsers(ctx)
	if err != nil {
		c.log.Debug("query traffic stats", "err", err)
		return
	}
	var deltas []store.TrafficDelta
	for _, u := range users {
		id, err := strconv.ParseInt(strings.TrimPrefix(u.User, "c"), 10, 64)
		if err != nil || (u.Uplink == 0 && u.Downlink == 0) {
			continue
		}
		deltas = append(deltas, store.TrafficDelta{ClientID: id, Upload: u.Uplink, Download: u.Downlink})
	}
	c.online.note(0, deltas, c.now())
	if err := c.store.AddTraffic(ctx, deltas); err != nil {
		c.log.Error("store traffic stats", "err", err)
	}
}

// RestartSingBox restarts the data plane, saving traffic counters first.
func (c *Core) RestartSingBox(ctx context.Context) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	return c.mgr.Restart(ctx)
}

// Run collects traffic every interval and re-applies the config, which
// disconnects clients that just expired or exhausted their limit. In
// between it only reads the counters, so that the panel knows who is
// online (see online).
func (c *Core) Run(ctx context.Context, interval time.Duration) {
	c.renewPrivateCertificates(ctx)
	c.dropUnusedLists(ctx)
	lastRenew := time.Now()
	c.Reconcile(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	stats := time.NewTicker(statsEvery)
	defer stats.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stats.C:
			c.applyMu.Lock()
			if c.mgr.Status().PID != 0 {
				c.flushStatsLocked(ctx)
			}
			c.applyMu.Unlock()
		case <-t.C:
			if time.Since(lastRenew) > time.Hour {
				lastRenew = time.Now()
				c.renewPrivateCertificates(ctx)
			}
			c.refreshRouteLists(ctx)
			c.applyMu.Lock()
			if c.mgr.Status().PID != 0 {
				c.flushStatsLocked(ctx)
			}
			c.applyMu.Unlock()
			c.Reconcile(ctx)
		}
	}
}

// OnBeforeApply registers what has to happen before sing-box gets a new
// configuration.
func (c *Core) OnBeforeApply(fn func(context.Context)) { c.beforeApply = fn }

// AllOrNothing runs a change of several settings so that it is saved whole
// or not at all: when save fails part of the way, the settings go back to
// what they were before it began. A form with one wrong field is refused as
// a form, without its right fields being quietly kept.
func (c *Core) AllOrNothing(ctx context.Context, save func() error) error {
	was, err := c.store.Settings(ctx)
	if err != nil {
		return err
	}
	if err := save(); err != nil {
		if rerr := c.store.RestoreSettings(ctx, was); rerr != nil {
			c.log.Error("restore settings after a refused change", "err", rerr)
		}
		return err
	}
	return nil
}
