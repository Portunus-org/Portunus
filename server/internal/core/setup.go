package core

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
	"github.com/Portunus-org/Portunus/server/internal/setup"
)

const (
	settingSetupDone = "setup_completed"
	settingDomain    = "domain"
	settingSubPort   = "subscription_port"
)

// ------------------------------------------------------ domain & subscriptions

// Domain is the server's public domain, if it has one. It is used for
// certificates and HTTPS subscriptions; links use PublicHost.
func (c *Core) Domain(ctx context.Context) (string, error) {
	return c.store.Setting(ctx, settingDomain)
}

func (c *Core) SetDomain(ctx context.Context, domain string) error {
	domain = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(domain)), ".")
	if domain != "" && (!validHost(domain) || net.ParseIP(domain) != nil || !strings.Contains(domain, ".")) {
		return invalid("domain", "invalid_hostname")
	}
	return c.store.SetSetting(ctx, settingDomain, domain)
}

// SubscriptionPort is the TCP port of the built-in subscription listener
// (0: off). PORTUNUS_SUB_LISTEN, when set, takes precedence.
func (c *Core) SubscriptionPort(ctx context.Context) int {
	if c.subListen != "" {
		_, p, _ := net.SplitHostPort(c.subListen)
		n, _ := strconv.Atoi(p)
		return n
	}
	v, _ := c.store.Setting(ctx, settingSubPort)
	n, _ := strconv.Atoi(v)
	return n
}

// SubscriptionListenAddr is where the subscription listener should bind, or
// "" when subscriptions are off.
func (c *Core) SubscriptionListenAddr(ctx context.Context) string {
	if c.subListen != "" {
		return c.subListen
	}
	if p := c.SubscriptionPort(ctx); p > 0 {
		return ":" + strconv.Itoa(p)
	}
	return ""
}

func (c *Core) SetSubscriptionPort(ctx context.Context, port int) error {
	if c.subListen != "" {
		return invalid("subscriptionPort", "set_by_environment")
	}
	if port != 0 && (port < 1024 || port > 65535) {
		return invalid("subscriptionPort", "out_of_range")
	}
	for _, r := range c.reserved {
		if port != 0 && r.Port == port {
			return invalid("subscriptionPort", "reserved")
		}
	}
	if port != 0 && port != c.SubscriptionPort(ctx) {
		if c.tcpPortTaken(ctx, port) || port == c.acmeListenPort(ctx) || port == c.PanelPort(ctx) {
			return invalid("subscriptionPort", "in_use")
		}
		if c.hostPorts != nil {
			if tcp, _ := c.hostPorts(); tcp[port] {
				return invalid("subscriptionPort", "busy_on_host")
			}
		}
	}
	return c.store.SetSetting(ctx, settingSubPort, strconv.Itoa(port))
}

// EffectiveSubscriptionBase is what a subscription URL starts with, the
// client's token following after a slash (and ca.crt, for the private CA):
//
//   - the operator's own address (behind their reverse proxy), with /sub;
//   - when the panel is opened by its domain, the panel's address with the
//     subscription path (SubscriptionPath): one port, one certificate;
//   - otherwise the built-in listener's port with /sub, over HTTPS once the
//     domain has a certificate, by the IP address before.
func (c *Core) EffectiveSubscriptionBase(ctx context.Context) string {
	if manual, _ := c.SubscriptionBaseURL(ctx); manual != "" {
		return manual + "/sub"
	}
	if pa, err := c.PanelAccess(ctx); err == nil && pa.Enabled {
		return pa.Origin() + "/" + c.SubscriptionPath(ctx)
	}
	port := c.SubscriptionPort(ctx)
	if port == 0 {
		return ""
	}
	if domain, _ := c.Domain(ctx); domain != "" && (c.Certificate(domain) != nil || c.sharedCertificate(domain) != nil) {
		return "https://" + net.JoinHostPort(domain, strconv.Itoa(port)) + "/sub"
	}
	host, _ := c.PublicHost(ctx)
	if host == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/sub"
}

const settingSubPath = "subscription_path"

// DefaultSubscriptionPath is where subscriptions are under the panel's
// domain unless the operator chose another path.
const DefaultSubscriptionPath = "sub"

// subPathShape: one lowercase segment of letters, digits and hyphens.
var subPathShape = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// subPathReserved are the first parts of paths the panel and its cover
// site answer themselves.
var subPathReserved = map[string]bool{
	"api": true, "login": true, "welcome": true, "account": true, "clients": true, "docs": true,
	"inbounds": true, "nodes": true, "routing": true, "settings": true, "users": true, "healthz": true,
	"log": true, "about": true, "static": true, "icon": true,
}

// SubscriptionPath is the first part of the path subscriptions are served
// under on the panel's domain: https://<domain>/<path>/<token>.
func (c *Core) SubscriptionPath(ctx context.Context) string {
	if v, _ := c.store.Setting(ctx, settingSubPath); v != "" {
		return v
	}
	return DefaultSubscriptionPath
}

// SetSubscriptionPath changes it ("" goes back to the default). Subscribed
// apps keep the address they were given, so links handed out before stop
// working: the page says so.
func (c *Core) SetSubscriptionPath(ctx context.Context, p string) error {
	p = strings.ToLower(strings.Trim(strings.TrimSpace(p), "/"))
	if p == "" {
		p = DefaultSubscriptionPath
	}
	if !subPathShape.MatchString(p) {
		return invalid("subscriptionPath", "bad_path")
	}
	if subPathReserved[p] {
		return invalid("subscriptionPath", "path_reserved")
	}
	if gate, _ := c.store.Setting(ctx, settingPanelGate); strings.EqualFold(gate, p) {
		return invalid("subscriptionPath", "taken")
	}
	return c.store.SetSetting(ctx, settingSubPath, p)
}

// SubscriptionCertificate returns the certificate for the domain to serve
// subscriptions over HTTPS with; nil until there is one. It is looked for
// in both places a certificate for the domain can be: sing-box's storage
// (an inbound that asked Let's Encrypt itself) and the panel's own, which
// the inbounds on the panel's domain serve instead of asking. The one that
// lasts longest is taken.
func (c *Core) SubscriptionCertificate(ctx context.Context) *tls.Certificate {
	domain, _ := c.Domain(ctx)
	if domain == "" {
		return nil
	}
	pairs := [][2]string{}
	if c.acme.StorageDir != "" && !strings.ContainsAny(domain, `/\`) {
		matches, _ := filepath.Glob(filepath.Join(c.acme.StorageDir, "certificates", "*", domain, domain+".crt"))
		for _, crt := range matches {
			pairs = append(pairs, [2]string{crt, strings.TrimSuffix(crt, ".crt") + ".key"})
		}
	}
	if c.sharedCertificate(domain) != nil {
		crt, key := c.PanelCertFiles()
		pairs = append(pairs, [2]string{crt, key})
	}
	var best *tls.Certificate
	for _, pair := range pairs {
		cert, err := tls.LoadX509KeyPair(pair[0], pair[1])
		if err != nil || cert.Leaf == nil || !c.now().Before(cert.Leaf.NotAfter) {
			continue
		}
		if best == nil || cert.Leaf.NotAfter.After(best.Leaf.NotAfter) {
			best = &cert
		}
	}
	return best
}

// --------------------------------------------------------------- wizard

// SetupCompleted reports whether the first-run wizard has been finished or
// skipped. Installations that already had inbounds count as set up.
func (c *Core) SetupCompleted(ctx context.Context) bool {
	if v, _ := c.store.Setting(ctx, settingSetupDone); v == "1" {
		return true
	}
	ins, _ := c.store.ListInbounds(ctx)
	return len(ins) > 0
}

func (c *Core) MarkSetupCompleted(ctx context.Context) error {
	return c.store.SetSetting(ctx, settingSetupDone, "1")
}

// The first setup does not end with the server's address: a server is of
// use once it has a connection and a client. The guide is the rest of the
// way, offered after the address is saved and until it is walked or
// declined. It is remembered here and not in the browser, because the
// operator usually changes browsers' idea of where the panel is in the
// middle of it: from the tunnel to the panel's own domain.
const settingSetupGuide = "setup_guide"

// GuidePending reports whether the guide should be offered now: it was
// started and neither finished nor declined, and something is still
// missing. With both a connection and a client in place it is over.
func (c *Core) GuidePending(ctx context.Context) bool {
	if v, _ := c.store.Setting(ctx, settingSetupGuide); v != "pending" {
		return false
	}
	ins, _ := c.store.ListInbounds(ctx)
	clients, _ := c.store.ListClients(ctx)
	if len(ins) > 0 && len(clients) > 0 {
		c.store.SetSetting(ctx, settingSetupGuide, "done")
		return false
	}
	return true
}

// EndGuide closes the guide, walked to the end or declined.
func (c *Core) EndGuide(ctx context.Context) error {
	return c.store.SetSetting(ctx, settingSetupGuide, "done")
}

// DetectAddress returns the host's public address (cached for a while:
// it may involve external lookups).
func (c *Core) DetectAddress(ctx context.Context) (setup.PublicAddress, error) {
	detectCache.Lock()
	defer detectCache.Unlock()
	if time.Since(detectCache.addressAt) < 10*time.Minute && detectCache.address.Primary() != "" {
		return detectCache.address, nil
	}
	a, err := setup.DetectPublicAddress(ctx)
	if err != nil {
		return a, err
	}
	detectCache.address, detectCache.addressAt = a, time.Now()
	return a, nil
}

func (c *Core) realityProbes(ctx context.Context) []setup.RealityProbe {
	detectCache.Lock()
	defer detectCache.Unlock()
	if time.Since(detectCache.realityAt) > 10*time.Minute || len(detectCache.reality) == 0 {
		detectCache.reality = setup.ProbeRealityTargets(ctx, setup.RealityCandidates)
		detectCache.realityAt = time.Now()
	}
	return detectCache.reality
}

var detectCache struct {
	sync.Mutex
	address   setup.PublicAddress
	addressAt time.Time
	reality   []setup.RealityProbe
	realityAt time.Time
}

// Basics is what the first-run welcome asks for.
type Basics struct {
	PublicHost string `json:"publicHost"`
	Domain     string `json:"domain"`
}

// SaveBasics stores the public address and domain and finishes the welcome.
// With a domain, subscriptions are turned on (HTTPS once the domain has a
// certificate).
func (c *Core) SaveBasics(ctx context.Context, b Basics) error {
	return c.AllOrNothing(ctx, func() error { return c.saveBasics(ctx, b) })
}

func (c *Core) saveBasics(ctx context.Context, b Basics) error {
	if err := c.SetPublicHost(ctx, b.PublicHost); err != nil {
		return err
	}
	if err := c.SetDomain(ctx, b.Domain); err != nil {
		return err
	}
	if strings.TrimSpace(b.Domain) != "" && c.subListen == "" && c.SubscriptionPort(ctx) == 0 {
		tcp, _ := setup.ListeningPorts()
		if p := setup.FirstFree(tcp, 2096, 2097, 8096); p != 0 {
			if err := c.SetSubscriptionPort(ctx, p); err != nil {
				return err
			}
		}
	}
	// The first time the address is saved the rest of the setup is offered.
	if !c.SetupCompleted(ctx) {
		if err := c.store.SetSetting(ctx, settingSetupGuide, "pending"); err != nil {
			return err
		}
	}
	return c.MarkSetupCompleted(ctx)
}

// CheckDomain reports where domain points and whether that is this server.
func (c *Core) CheckDomain(ctx context.Context, domain string) (setup.DomainCheck, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(domain)), ".")
	if !validHost(domain) || net.ParseIP(domain) != nil || !strings.Contains(domain, ".") {
		return setup.DomainCheck{}, invalid("domain", "invalid_hostname")
	}
	addr, _ := c.DetectAddress(ctx)
	return setup.CheckDomain(ctx, domain, addr), nil
}

// CertificatePlan is how a TLS inbound would get its certificate.
type CertificatePlan struct {
	// Recommended is "acme" when the domain points here, else "self-signed".
	Recommended string `json:"recommended"`
	// Challenge is the verification method in effect: the saved setting, or
	// what the host allows ("http" when port 80 is free, else "proxy").
	Challenge          string `json:"challenge"`
	ChallengeSaved     bool   `json:"challengeSaved"`
	HTTPServer80       string `json:"httpServer80"`
	ProxyPort          int    `json:"proxyPort"`
	CloudflareTokenSet bool   `json:"cloudflareTokenSet"`
}

// InboundSuggestion is what the inbound wizard proposes for a protocol.
type InboundSuggestion struct {
	Port int `json:"port"`
	// PreferredPortBusy: the protocol's usual port is taken on this host.
	PreferredPortBusy bool                 `json:"preferredPortBusy"`
	Reality           []setup.RealityProbe `json:"reality,omitempty"`
	Masquerade        string               `json:"masquerade,omitempty"`
	Domain            string               `json:"domain"`
	DomainCheck       *setup.DomainCheck   `json:"domainCheck,omitempty"`
	Certificate       *CertificatePlan     `json:"certificate,omitempty"`
	PublicHost        string               `json:"publicHost"`
	// SharedWith is the panel's domain when the suggested port is the one
	// the panel is opened on: the connection then presents that domain. It
	// is the first and recommended entry of Reality; the other sites remain
	// to choose from, and a connection that presents one of them takes
	// OtherPort instead, since the panel's port goes with the panel's name.
	SharedWith string `json:"sharedWith,omitempty"`
	OtherPort  int    `json:"otherPort,omitempty"`
}

// SuggestInbound inspects the host for creating an inbound of protocol p.
// domain overrides the saved domain (the wizard lets the user type one).
func (c *Core) SuggestInbound(ctx context.Context, p proxy.Protocol, domain string) (*InboundSuggestion, error) {
	if !p.Valid() {
		return nil, invalid("protocol", "unsupported")
	}
	out := &InboundSuggestion{}
	out.PublicHost, _ = c.PublicHost(ctx)

	tcp, udp := setup.ListeningPorts()
	if rows, err := c.store.ListInbounds(ctx); err == nil {
		// Our own listeners count as taken for the same transport.
		for _, r := range rows {
			q := proxy.Protocol(r.Protocol)
			if q.UsesTCP() {
				tcp[r.Port] = true
			}
			if q.UsesUDP() {
				udp[r.Port] = true
			}
		}
	}
	for _, r := range c.reserved {
		tcp[r.Port] = true
	}
	busy := func(port int) bool {
		return (p.UsesTCP() && tcp[port]) || (p.UsesUDP() && udp[port])
	}
	preferred := []int{443, 8443, 2083, 2087, 2053, 9443}
	if p == proxy.Shadowsocks {
		preferred = []int{8388, 8389, 9388}
	}
	for _, port := range preferred {
		if !busy(port) {
			out.Port = port
			break
		}
	}
	out.PreferredPortBusy = out.Port != preferred[0]
	// VLESS can share the panel's port, which is the best one to have.
	pa, _ := c.PanelAccess(ctx)
	if p == proxy.VLESS && pa.Enabled && !pa.Shared {
		out.OtherPort = out.Port
		out.Port, out.PreferredPortBusy, out.SharedWith = pa.Port, false, pa.Domain
	}

	best := proxy.DefaultRealityServer
	if p == proxy.VLESS || p == proxy.Hysteria2 {
		out.Reality = c.realityProbes(ctx)
		for _, r := range out.Reality {
			if r.OK {
				best = r.Host
				break
			}
		}
	}
	if out.SharedWith != "" {
		// The panel's own domain first: nothing looks more like a visit to
		// this server than its own site with its own certificate.
		out.Reality = append([]setup.RealityProbe{{Host: out.SharedWith, OK: true}}, out.Reality...)
	}
	if p == proxy.Hysteria2 {
		// The probes only served to pick a masquerade site.
		out.Masquerade = "https://" + best
		out.Reality = nil
	}

	if p.UsesTLSCert() {
		if domain = strings.TrimSpace(domain); domain == "" {
			domain, _ = c.Domain(ctx)
		}
		out.Domain = domain
		plan := &CertificatePlan{Recommended: "self-signed"}
		if domain != "" {
			if dc, err := c.CheckDomain(ctx, domain); err == nil {
				out.DomainCheck = &dc
				if dc.PointsHere {
					plan.Recommended = "acme"
				}
			}
		}
		acme, err := c.ACME(ctx)
		if err != nil {
			return nil, err
		}
		saved, _ := c.store.Setting(ctx, settingACMEChallenge)
		plan.ChallengeSaved = saved != ""
		plan.CloudflareTokenSet = acme.CloudflareTokenSet
		plan.HTTPServer80 = setup.HTTPServerOn80(ctx)
		plan.ProxyPort = acme.HTTPPort
		switch {
		case plan.ChallengeSaved:
			plan.Challenge = acme.Challenge
		case plan.HTTPServer80 == "" && !tcp[80]:
			plan.Challenge = ChallengeHTTP
		default:
			plan.Challenge = ChallengeProxy
		}
		out.Certificate = plan
	}
	return out, nil
}

// VerifyProxyForwarding checks the reverse proxy forwards ACME challenges.
func (c *Core) VerifyProxyForwarding(ctx context.Context, domain string, port int) error {
	return setup.VerifyChallengeForwarding(ctx, domain, port)
}
