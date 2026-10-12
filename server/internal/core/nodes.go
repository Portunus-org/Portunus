package core

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PaulichP/Portunus/server/internal/backup"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PaulichP/Portunus/server/internal/agent"
	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// Node kinds.
const (
	// NodeAgent is a server of ours running `portunus agent`.
	NodeAgent = "agent"
	// NodeLink is a third-party proxy imported from a share link.
	NodeLink = "link"
)

// EntryCert is the certificate a backup entry point serves for one inbound.
type EntryCert struct {
	Certificate   string `json:"certificate"`
	CertSHA256    string `json:"certSha256"`
	CACertificate string `json:"caCertificate"`
}

// NodeSettings are the public parameters of a node.
type NodeSettings struct {
	// Agent nodes.
	AgentPort int            `json:"agentPort,omitempty"`
	HopPort   int            `json:"hopPort,omitempty"`
	Hop       proxy.Settings `json:"hop,omitzero"`
	// JoinedAt is when the node first answered the panel; until then it is
	// not offered to clients as an entry point.
	JoinedAt *time.Time `json:"joinedAt,omitempty"`
	// EntryCerts are keyed by inbound ID.
	EntryCerts map[string]EntryCert `json:"entryCerts,omitempty"`

	// Standby: the node keeps a sealed copy of the panel (see standby.go).
	Standby bool `json:"standby,omitempty"`

	// Location places the node on the map: looked up from its address, or
	// set by hand.
	Location *Location `json:"location,omitempty"`

	// Imported links.
	Protocol string `json:"protocol,omitempty"`
	Port     int    `json:"port,omitempty"`
}

// NodeSecret is the private material of a node; stored encrypted.
type NodeSecret struct {
	HopSecret proxy.Secret `json:"hopSecret,omitzero"`
	HopUUID   string       `json:"hopUuid,omitempty"`
	// AgentCert and AgentKey are the node's control-channel certificate,
	// kept so the join command can be shown again.
	AgentCert string            `json:"agentCert,omitempty"`
	AgentKey  string            `json:"agentKey,omitempty"`
	EntryKeys map[string]string `json:"entryKeys,omitempty"`
	// Outbound is the sing-box outbound of an imported link.
	Outbound map[string]any `json:"outbound,omitempty"`
}

// Node is another server of the installation or an imported proxy.
type Node struct {
	ID        int64
	Name      string
	Kind      string
	Address   string
	IsExit    bool
	IsEntry   bool
	Enabled   bool
	Settings  NodeSettings
	Secret    NodeSecret
	CreatedAt time.Time
}

func (n Node) hop() proxy.Hop {
	return proxy.Hop{Port: n.Settings.HopPort, Settings: n.Settings.Hop, Secret: n.Secret.HopSecret, UUID: n.Secret.HopUUID}
}

// exit is the outbound other servers use to send traffic out through n.
func (n Node) exit() proxy.Exit {
	if n.Kind == NodeLink {
		return proxy.Exit{ID: n.ID, Outbound: n.Secret.Outbound}
	}
	return proxy.Exit{ID: n.ID, Outbound: n.hop().Outbound(n.Address)}
}

func nodeAAD(id int64) string { return "node:" + strconv.FormatInt(id, 10) }

func (c *Core) decodeNode(row *store.NodeRow) (Node, error) {
	n := Node{
		ID: row.ID, Name: row.Name, Kind: row.Kind, Address: row.Address,
		IsExit: row.IsExit, IsEntry: row.IsEntry, Enabled: row.Enabled, CreatedAt: row.CreatedAt,
	}
	if err := json.Unmarshal([]byte(row.Settings), &n.Settings); err != nil {
		return n, fmt.Errorf("node %d settings: %w", row.ID, err)
	}
	plain, err := c.box.Open(row.Secret, nodeAAD(row.ID))
	if err != nil {
		return n, fmt.Errorf("node %d secret: %w", row.ID, err)
	}
	if err := json.Unmarshal([]byte(plain), &n.Secret); err != nil {
		return n, fmt.Errorf("node %d secret: %w", row.ID, err)
	}
	return n, nil
}

func (c *Core) sealNode(id int64, sec NodeSecret) (string, error) {
	raw, err := json.Marshal(sec)
	if err != nil {
		return "", err
	}
	return c.box.Seal(string(raw), nodeAAD(id)), nil
}

func (c *Core) Nodes(ctx context.Context) ([]Node, error) {
	rows, err := c.store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Node, 0, len(rows))
	for _, r := range rows {
		n, err := c.decodeNode(r)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func (c *Core) Node(ctx context.Context, id int64) (Node, error) {
	row, err := c.store.NodeByID(ctx, id)
	if err != nil {
		return Node{}, err
	}
	return c.decodeNode(row)
}

// NodeInput is what the operator chooses for a node.
type NodeInput struct {
	Name    string
	Kind    string
	Address string
	IsExit  bool
	IsEntry bool
	Enabled bool
	// Agent nodes; zero means the default.
	HopPort   int
	AgentPort int
	// Link is the share link of an imported proxy (creation only).
	Link string
	// Standby: keep a copy of the panel on the node (agent nodes).
	Standby bool
}

const (
	defaultHopPort = 443
	// Used instead of 443 on a node that is also an entry point, where
	// TCP 443 usually belongs to an inbound.
	entryHopPort = 8443
)

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func (c *Core) validateNode(ctx context.Context, id int64, in *NodeInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(strings.ToLower(in.Address))
	if in.Name == "" {
		return invalid("name", "required")
	}
	if len(in.Name) > 64 {
		return invalid("name", "too_long")
	}
	if in.Kind == NodeLink {
		in.IsExit, in.IsEntry = true, false
		return nil
	}
	if !validHost(in.Address) {
		return invalid("address", "invalid_hostname")
	}
	// Keeping a copy of the panel is a role of its own: a server may do
	// nothing else.
	if !in.IsExit && !in.IsEntry && !in.Standby {
		return invalid("roles", "roles_required")
	}
	if in.AgentPort == 0 {
		in.AgentPort = agent.DefaultPort
	}
	if in.HopPort == 0 {
		in.HopPort = defaultHopPort
		if in.IsEntry {
			in.HopPort = entryHopPort
		}
	}
	if !validPort(in.AgentPort) {
		return invalid("agentPort", "out_of_range")
	}
	if !validPort(in.HopPort) {
		return invalid("hopPort", "out_of_range")
	}
	if in.IsExit && in.HopPort == in.AgentPort {
		return invalid("hopPort", "port_conflict")
	}
	// On an entry point the inbounds listen too.
	if in.IsEntry {
		rows, err := c.store.ListInbounds(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !proxy.Protocol(r.Protocol).UsesTCP() {
				continue
			}
			if in.IsExit && r.Port == in.HopPort {
				return invalid("hopPort", "port_conflict")
			}
			if r.Port == in.AgentPort {
				return invalid("agentPort", "port_conflict")
			}
		}
	}
	others, err := c.store.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, o := range others {
		if o.ID != id && o.Kind == NodeAgent && o.Address == in.Address {
			return invalid("address", "address_taken")
		}
	}
	return nil
}

// CreateNode adds a server of ours (to be joined with the command from
// NodeJoin) or imports a proxy from a share link.
func (c *Core) CreateNode(ctx context.Context, in NodeInput) (Node, error) {
	if in.Kind != NodeAgent && in.Kind != NodeLink {
		return Node{}, invalid("kind", "unsupported")
	}
	var (
		set NodeSettings
		sec NodeSecret
	)
	if in.Kind == NodeLink {
		imp, err := proxy.ParseShareLink(in.Link)
		if err != nil {
			return Node{}, invalid("link", "unsupported_link")
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = imp.Name
		}
		if strings.TrimSpace(in.Name) == "" {
			in.Name = net.JoinHostPort(imp.Address, strconv.Itoa(imp.Port))
		}
		in.Address = imp.Address
		set.Protocol, set.Port = imp.Protocol, imp.Port
		sec.Outbound = imp.Outbound
	}
	if err := c.validateNode(ctx, 0, &in); err != nil {
		return Node{}, err
	}
	if in.Kind == NodeAgent {
		ca, err := c.CA(ctx)
		if err != nil {
			return Node{}, err
		}
		hop, err := proxy.NewHop(in.HopPort, c.hopCamouflage(ctx))
		if err != nil {
			return Node{}, err
		}
		set.AgentPort, set.HopPort, set.Hop = in.AgentPort, in.HopPort, hop.Settings
		set.Standby = in.Standby
		sec.HopSecret, sec.HopUUID = hop.Secret, hop.UUID
		if sec.AgentCert, sec.AgentKey, err = ca.IssueNode(in.Address); err != nil {
			return Node{}, err
		}
	}
	settings, _ := json.Marshal(set)
	row := &store.NodeRow{
		Name: in.Name, Kind: in.Kind, Address: in.Address,
		IsExit: in.IsExit, IsEntry: in.IsEntry, Enabled: in.Enabled, Settings: string(settings),
	}
	err := c.store.CreateNode(ctx, row, func(id int64) (string, error) { return c.sealNode(id, sec) })
	if errors.Is(err, store.ErrConflict) {
		return Node{}, invalid("name", "taken")
	}
	if err != nil {
		return Node{}, err
	}
	c.Reconcile(ctx)
	return c.decodeNode(row)
}

// hopCamouflage is the site hops between servers imitate: the one the
// operator already chose for VLESS, if any.
func (c *Core) hopCamouflage(ctx context.Context) string {
	ins, _ := c.Inbounds(ctx)
	for _, in := range ins {
		if in.Protocol == proxy.VLESS && in.Settings.RealityServer != "" {
			return in.Settings.RealityServer
		}
	}
	return ""
}

func (c *Core) saveNode(ctx context.Context, n Node) error {
	settings, _ := json.Marshal(n.Settings)
	sealed, err := c.sealNode(n.ID, n.Secret)
	if err != nil {
		return err
	}
	err = c.store.UpdateNode(ctx, &store.NodeRow{
		ID: n.ID, Name: n.Name, Address: n.Address, IsExit: n.IsExit, IsEntry: n.IsEntry,
		Enabled: n.Enabled, Settings: string(settings), Secret: sealed,
	})
	if errors.Is(err, store.ErrConflict) {
		return invalid("name", "taken")
	}
	return err
}

// UpdateNode changes the name, roles and ports of a node. Its address and
// keys stay: a server that moved is removed and joined again.
func (c *Core) UpdateNode(ctx context.Context, id int64, in NodeInput) (Node, error) {
	n, err := c.Node(ctx, id)
	if err != nil {
		return Node{}, err
	}
	in.Kind, in.Address = n.Kind, n.Address
	if in.Kind == NodeAgent {
		if in.HopPort == 0 {
			in.HopPort = n.Settings.HopPort
		}
		if in.AgentPort == 0 {
			in.AgentPort = n.Settings.AgentPort
		}
	}
	if err := c.validateNode(ctx, id, &in); err != nil {
		return Node{}, err
	}
	if (n.IsExit && n.Enabled) && !(in.IsExit && in.Enabled) {
		if used, err := c.exitInUse(ctx, id); err != nil {
			return Node{}, err
		} else if used {
			return Node{}, invalid("node", "node_in_use")
		}
	}
	n.Name, n.IsExit, n.IsEntry, n.Enabled = in.Name, in.IsExit, in.IsEntry, in.Enabled
	if n.Kind == NodeAgent {
		n.Settings.HopPort, n.Settings.AgentPort = in.HopPort, in.AgentPort
		n.Settings.Standby = in.Standby
	}
	if err := c.saveNode(ctx, n); err != nil {
		return Node{}, err
	}
	c.dropNodeClient(id)
	c.Reconcile(ctx)
	return n, nil
}

// exitInUse reports whether a rule or the default route sends traffic
// through the node.
func (c *Core) exitInUse(ctx context.Context, id int64) (bool, error) {
	act, node := c.DefaultRoute(ctx)
	if act == proxy.RouteNode && node == id {
		return true, nil
	}
	rules, err := c.store.ListRouteRules(ctx)
	if err != nil {
		return false, err
	}
	auto := act == proxy.RouteAuto
	for _, r := range rules {
		if r.Action == proxy.RouteNode && r.NodeID != nil && *r.NodeID == id {
			return true, nil
		}
		auto = auto || r.Action == proxy.RouteAuto
	}
	if !auto {
		return false, nil
	}
	// "The fastest exit" needs at least one: the last exit cannot go while
	// something is routed that way, or that traffic would be refused.
	nodes, err := c.store.ListNodes(ctx)
	if err != nil {
		return false, err
	}
	for _, n := range nodes {
		if n.ID != id && n.IsExit && n.Enabled {
			return false, nil
		}
	}
	return true, nil
}

// DeleteNode removes a node. One that rules still send traffic through is
// kept: removing it silently would change where that traffic goes.
func (c *Core) DeleteNode(ctx context.Context, id int64) error {
	if used, err := c.exitInUse(ctx, id); err != nil {
		return err
	} else if used {
		return invalid("node", "node_in_use")
	}
	if err := c.store.DeleteNode(ctx, id); err != nil {
		return err
	}
	c.dropNodeClient(id)
	c.nodeMu.Lock()
	delete(c.nodeStatus, id)
	delete(c.exitHealth, id)
	c.nodeMu.Unlock()
	c.Reconcile(ctx)
	return nil
}

// NodeJoin is what the operator runs on a new server.
type NodeJoin struct {
	Image string `json:"image"`
	// CopyImage, when set, brings the image to the server first: it is a
	// local build, not something the server can pull.
	CopyImage string `json:"copyImage,omitempty"`
	Run       string `json:"run"`
	// InstallDocker is for a server that has no Docker yet.
	InstallDocker string `json:"installDocker"`
	// Ports to open on the server's firewall.
	AgentPort int `json:"agentPort"`
	HopPort   int `json:"hopPort,omitempty"`
}

func (c *Core) NodeJoin(ctx context.Context, id int64) (NodeJoin, error) {
	n, err := c.Node(ctx, id)
	if err != nil {
		return NodeJoin{}, err
	}
	if n.Kind != NodeAgent {
		return NodeJoin{}, store.ErrNotFound
	}
	ca, err := c.CA(ctx)
	if err != nil {
		return NodeJoin{}, err
	}
	token := agent.Bundle{Port: n.Settings.AgentPort, CA: ca.CertPEM, Cert: n.Secret.AgentCert, Key: n.Secret.AgentKey}.Encode()
	// An image without a registry host exists only on this machine.
	first, _, _ := strings.Cut(c.nodeImage, "/")
	local := !strings.ContainsAny(first, ".:") || !strings.Contains(c.nodeImage, "/")
	// From a registry the image is fetched anew each time, so running the
	// command again is also how a node is updated.
	pull := " --pull always"
	if local {
		pull = ""
	}
	out := NodeJoin{
		Image:     c.nodeImage,
		AgentPort: n.Settings.AgentPort,
		// One line for a fresh server, run as an ordinary user. It replaces
		// a node container left from an earlier attempt, so it can simply be
		// run again. Host networking: the node listens on the ports of the
		// connections, which are not known in advance.
		Run: "sudo docker rm -f portunus-node 2>/dev/null; sudo docker run -d" + pull +
			" --name portunus-node --restart unless-stopped --network host" +
			" --read-only --cap-drop ALL --cap-add NET_BIND_SERVICE" +
			" -v portunus-node:/var/lib/portunus -e PORTUNUS_JOIN=" + token + " " + c.nodeImage,
		InstallDocker: "curl -fsSL https://get.docker.com | sudo sh",
	}
	if n.IsExit {
		out.HopPort = n.Settings.HopPort
	}
	if local {
		out.CopyImage = "docker save " + c.nodeImage + " | ssh " + n.Address + " sudo docker load"
	}
	return out, nil
}

// ------------------------------------------------------------ entry points

// entryInbounds returns the inbounds as node serves them: the same ports and
// keys, but with certificates for the node's own address. Missing or
// expiring certificates are issued and saved.
func (c *Core) entryInbounds(ctx context.Context, n *Node, inbounds []proxy.Inbound) ([]proxy.Inbound, error) {
	var (
		out     []proxy.Inbound
		changed bool
		ca      *proxy.CA
	)
	for _, in := range inbounds {
		if !in.Enabled {
			continue
		}
		in.Name = in.Label() + " (" + n.Name + ")"
		if in.Protocol.UsesTLSCert() {
			key := strconv.FormatInt(in.ID, 10)
			cert, ok := n.Settings.EntryCerts[key]
			if ca == nil {
				var err error
				if ca, err = c.CA(ctx); err != nil {
					return nil, err
				}
			}
			exp, err := proxy.CertExpiry(cert.Certificate)
			if !ok || err != nil || time.Until(exp) < proxy.LeafRenewBefore || cert.CACertificate != ca.CertPEM || n.Secret.EntryKeys[key] == "" {
				certPEM, keyPEM, sum, _, err := ca.Issue(n.Address)
				if err != nil {
					return nil, err
				}
				cert = EntryCert{Certificate: certPEM, CertSHA256: sum, CACertificate: ca.CertPEM}
				if n.Settings.EntryCerts == nil {
					n.Settings.EntryCerts, n.Secret.EntryKeys = map[string]EntryCert{}, map[string]string{}
				}
				if n.Secret.EntryKeys == nil {
					n.Secret.EntryKeys = map[string]string{}
				}
				n.Settings.EntryCerts[key], n.Secret.EntryKeys[key] = cert, keyPEM
				changed = true
			}
			// Always the private certificate here: a public one is bound to
			// the main server's domain.
			in.Settings.TLSMode = ""
			in.Settings.CertificatePath, in.Settings.KeyPath = "", ""
			in.Settings.ServerName = n.Address
			in.Settings.Certificate, in.Settings.CertSHA256, in.Settings.CACertificate = cert.Certificate, cert.CertSHA256, cert.CACertificate
			in.Secret.TLSKey = n.Secret.EntryKeys[key]
		}
		out = append(out, in)
	}
	if changed {
		if err := c.saveNode(ctx, *n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// EntryPoints are the servers clients can connect to: the main one, then
// every backup entry point that has joined.
func (c *Core) EntryPoints(ctx context.Context) ([]proxy.EntryPoint, error) {
	host, err := c.PublicHost(ctx)
	if err != nil {
		return nil, err
	}
	inbounds, err := c.Inbounds(ctx)
	if err != nil {
		return nil, err
	}
	points := []proxy.EntryPoint{{Host: host, Inbounds: inbounds}}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != NodeAgent || !n.IsEntry || !n.Enabled || n.Settings.JoinedAt == nil {
			continue
		}
		ins, err := c.entryInbounds(ctx, n, inbounds)
		if err != nil {
			return nil, err
		}
		points = append(points, proxy.EntryPoint{Host: n.Address, Inbounds: ins})
	}
	return points, nil
}

// ------------------------------------------------------------------- sync

// Node states.
const (
	NodeOnline  = "online"
	NodeOffline = "offline"
	// NodePending: created, but the server has not answered yet.
	NodePending = "pending"
	// NodeRejected: reachable, but it refused the configuration.
	NodeRejected = "rejected"
)

// NodeStatus is what the panel last learnt about a node.
type NodeStatus struct {
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt,omitzero"`
	Version   string    `json:"version,omitempty"`
	// SingBox is the state of the node's sing-box ("running", "stopped"…).
	SingBox string `json:"singBox,omitempty"`
	// LatencyMs is the time of a small request through the exit, measured
	// from this server; nil when unknown or failing.
	LatencyMs *int `json:"latencyMs,omitempty"`
	// CopyAt is when the node last received a copy of the panel.
	CopyAt *time.Time `json:"copyAt,omitempty"`
}

type nodeRuntime struct {
	nodeMu      sync.Mutex
	nodeStatus  map[int64]NodeStatus
	nodeClients map[int64]*agent.Client
	nodeKick    chan struct{}
	nodeImage   string
	controlAPI  string
	panelCertMu sync.Mutex
	// exitHealth holds the exits that are failing their checks.
	exitHealth map[int64]exitHealth

	// The sealed copy of the panel for standby nodes and when each got it.
	backupSrc   backup.Source
	standbyMu   sync.Mutex
	standbyBlob []byte
	standbyAt   time.Time
	standbySent map[int64]time.Time
	standbyCfg  [sha256.Size]byte

	// geoURL is the geolocation service ("" turns lookups off); geoTried
	// remembers when each server was last looked up (0 is the main one).
	geoURL   string
	geoMu    sync.Mutex
	geoTried map[int64]time.Time
}

func (c *Core) NodeStatus(id int64) NodeStatus {
	c.nodeMu.Lock()
	defer c.nodeMu.Unlock()
	return c.nodeStatus[id]
}

func (c *Core) setNodeStatus(id int64, fn func(*NodeStatus)) {
	c.nodeMu.Lock()
	defer c.nodeMu.Unlock()
	st := c.nodeStatus[id]
	fn(&st)
	c.nodeStatus[id] = st
}

func (c *Core) dropNodeClient(id int64) {
	c.nodeMu.Lock()
	defer c.nodeMu.Unlock()
	if cl := c.nodeClients[id]; cl != nil {
		cl.Close()
		delete(c.nodeClients, id)
	}
}

const (
	settingPanelCert = "panel_client_cert"
	settingPanelKey  = "panel_client_key" // encrypted
)

// panelCertificate is the client certificate the panel shows its nodes.
func (c *Core) panelCertificate(ctx context.Context, ca *proxy.CA) (certPEM, keyPEM string, err error) {
	// Nodes are synchronised concurrently: without this, several of them
	// would each make a certificate at the first round and leave the
	// certificate of one with the key of another in the database.
	c.panelCertMu.Lock()
	defer c.panelCertMu.Unlock()
	certPEM, _ = c.store.Setting(ctx, settingPanelCert)
	sealed, _ := c.store.Setting(ctx, settingPanelKey)
	if certPEM != "" && sealed != "" {
		if keyPEM, err = c.box.Open(sealed, "setting:"+settingPanelKey); err == nil {
			// A pair that does not fit (see above) is replaced: nodes
			// accept any panel certificate of this installation's CA.
			if _, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err == nil {
				return certPEM, keyPEM, nil
			}
		}
	}
	if certPEM, keyPEM, err = ca.IssueClient(); err != nil {
		return "", "", err
	}
	if err := c.store.SetSetting(ctx, settingPanelCert, certPEM); err != nil {
		return "", "", err
	}
	return certPEM, keyPEM, c.store.SetSetting(ctx, settingPanelKey, c.box.Seal(keyPEM, "setting:"+settingPanelKey))
}

func (c *Core) nodeClient(ctx context.Context, n Node) (*agent.Client, error) {
	c.nodeMu.Lock()
	cl := c.nodeClients[n.ID]
	c.nodeMu.Unlock()
	if cl != nil {
		return cl, nil
	}
	ca, err := c.CA(ctx)
	if err != nil {
		return nil, err
	}
	certPEM, keyPEM, err := c.panelCertificate(ctx, ca)
	if err != nil {
		return nil, err
	}
	cl, err = agent.NewClient(n.Address, n.Settings.AgentPort, ca, certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	c.nodeMu.Lock()
	c.nodeClients[n.ID] = cl
	c.nodeMu.Unlock()
	return cl, nil
}

// nodeConfig renders what node n should run.
func (c *Core) nodeConfig(ctx context.Context, n *Node, inbounds []proxy.Inbound, clients []proxy.Client) ([]byte, error) {
	if !n.Enabled {
		return nil, nil
	}
	var hop *proxy.Hop
	if n.IsExit {
		h := n.hop()
		hop = &h
	}
	var entry *proxy.EntryConfig
	if n.IsEntry {
		ins, err := c.entryInbounds(ctx, n, inbounds)
		if err != nil {
			return nil, err
		}
		routing, err := c.routing(ctx, n.ID)
		if err != nil {
			return nil, err
		}
		entry = &proxy.EntryConfig{Inbounds: ins, Clients: clients, Routing: routing, StatsAPI: agent.StatsAPI}
	}
	return proxy.NodeConfig(hop, entry)
}

// SyncNodes brings every node of ours in line with the database: sends
// the configuration where it differs and collects traffic from backup
// entry points. It never fails as a whole; each node's outcome is in its
// status.
func (c *Core) SyncNodes(ctx context.Context) {
	nodes, err := c.Nodes(ctx)
	if err != nil {
		c.log.Error("load nodes", "err", err)
		return
	}
	inbounds, err := c.Inbounds(ctx)
	if err != nil {
		c.log.Error("load inbounds", "err", err)
		return
	}
	clients, err := c.activeClients(ctx)
	if err != nil {
		c.log.Error("load clients", "err", err)
		return
	}
	var wg sync.WaitGroup
	for i := range nodes {
		n := &nodes[i]
		if n.Kind != NodeAgent {
			continue
		}
		wg.Go(func() { c.syncNode(ctx, n, inbounds, clients) })
	}
	wg.Wait()
}

func (c *Core) syncNode(ctx context.Context, n *Node, inbounds []proxy.Inbound, clients []proxy.Client) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	fail := func(state string, err error) {
		c.setNodeStatus(n.ID, func(st *NodeStatus) {
			if state == NodeOffline && n.Settings.JoinedAt == nil {
				state = NodePending
			}
			st.State, st.Error, st.CheckedAt = state, err.Error(), c.now()
			if state != NodeRejected {
				st.SingBox = ""
			}
		})
	}
	cfg, err := c.nodeConfig(ctx, n, inbounds, clients)
	if err != nil {
		fail(NodeRejected, err)
		return
	}
	cl, err := c.nodeClient(ctx, *n)
	if err != nil {
		fail(NodeOffline, err)
		return
	}
	st, err := cl.Status(ctx)
	if err != nil {
		fail(NodeOffline, err)
		return
	}
	if n.Settings.JoinedAt == nil {
		now := c.now()
		n.Settings.JoinedAt = &now
		if err := c.saveNode(ctx, *n); err != nil {
			c.log.Error("save node", "node", n.Name, "err", err)
		}
		c.log.Info("node joined", "node", n.Name, "address", n.Address)
	}
	if st.ConfigHash != agent.HashConfig(cfg) {
		if st, err = cl.SetConfig(ctx, cfg); err != nil {
			var remote *agent.RemoteError
			if errors.As(err, &remote) {
				fail(NodeRejected, err)
			} else {
				fail(NodeOffline, err)
			}
			return
		}
		c.log.Info("node configured", "node", n.Name)
	}
	if n.IsEntry && n.Enabled {
		if traffic, err := cl.Traffic(ctx); err == nil {
			var deltas []store.TrafficDelta
			for _, t := range traffic {
				id, err := strconv.ParseInt(strings.TrimPrefix(t.User, "c"), 10, 64)
				if err != nil || id == 0 || (t.Uplink == 0 && t.Downlink == 0) {
					continue
				}
				deltas = append(deltas, store.TrafficDelta{ClientID: id, Upload: t.Uplink, Download: t.Downlink})
			}
			c.online.note(n.ID, deltas, c.now())
			if err := c.store.AddTraffic(ctx, deltas); err != nil {
				c.log.Error("store node traffic", "node", n.Name, "err", err)
			}
		}
	}
	copyAt := c.syncPanelCopy(ctx, n, cl, st.PanelCopy)
	c.setNodeStatus(n.ID, func(s *NodeStatus) {
		s.CopyAt = copyAt
		s.State, s.Error, s.CheckedAt, s.Version = NodeOnline, "", c.now(), st.Version
		s.SingBox = string(st.SingBox.State)
		if st.SingBox.LastError != "" && st.SingBox.State != "running" {
			s.Error = st.SingBox.LastError
		}
	})
}

// ProbeExits measures every exit through the local sing-box.
func (c *Core) ProbeExits(ctx context.Context) {
	if c.controlAPI == "" || c.mgr.Status().PID == 0 {
		return
	}
	nodes, err := c.Nodes(ctx)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 8 * time.Second}
	var (
		wg   sync.WaitGroup
		lost atomic.Bool
	)
	for _, n := range nodes {
		if !n.IsExit || !n.Enabled {
			c.setNodeStatus(n.ID, func(st *NodeStatus) { st.LatencyMs = nil })
			continue
		}
		wg.Go(func() {
			ms, err := c.probeExit(ctx, client, proxy.ExitTag(n.ID))
			if ctx.Err() != nil {
				return
			}
			if c.noteExit(n.ID, err == nil) {
				c.log.Warn("exit does not answer, moving its clients", "node", n.Name)
				lost.Store(true)
			}
			c.setNodeStatus(n.ID, func(st *NodeStatus) {
				if err != nil {
					st.LatencyMs = nil
				} else {
					st.LatencyMs = &ms
				}
				// An imported proxy has no agent: this probe is all we know.
				if n.Kind == NodeLink {
					st.CheckedAt = c.now()
					if err != nil {
						st.State, st.Error = NodeOffline, err.Error()
					} else {
						st.State, st.Error = NodeOnline, ""
					}
				}
			})
		})
	}
	wg.Wait()
	if lost.Load() {
		c.Reconcile(ctx)
	}
}

func (c *Core) probeExit(ctx context.Context, client *http.Client, tag string) (int, error) {
	u := "http://" + c.controlAPI + "/proxies/" + url.PathEscape(tag) + "/delay?timeout=5000&url=" + url.QueryEscape("https://www.gstatic.com/generate_204")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.controlSecret())
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	var body struct {
		Delay   int    `json:"delay"`
		Message string `json:"message"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusOK || body.Delay == 0 {
		if body.Message == "" {
			body.Message = "no answer through this exit"
		}
		return 0, errors.New(body.Message)
	}
	return body.Delay, nil
}

func (c *Core) controlSecret() string {
	return fmt.Sprintf("%x", c.box.Derive("sing-box control api"))
}

// KickNodes asks the node loop to run now instead of at its next tick.
func (c *Core) KickNodes() {
	select {
	case c.nodeKick <- struct{}{}:
	default:
	}
}

// RunNodes keeps nodes in sync until ctx ends: at once when something
// changed, and every interval to notice nodes that went away or came back.
func (c *Core) RunNodes(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		c.SyncNodes(ctx)
		c.ProbeExits(ctx)
		c.LocateServers(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.nodeKick:
			// Give sing-box a moment to load the new configuration before
			// measuring through it.
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}
}
