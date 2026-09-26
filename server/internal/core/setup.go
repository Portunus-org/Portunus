package core

import (
	"context"
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/setup"
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
	return c.store.SetSetting(ctx, settingSubPort, strconv.Itoa(port))
}

// EffectiveSubscriptionBase is the origin put into subscription URLs: the
// operator's explicit URL (e.g. behind their reverse proxy) if set,
// otherwise the built-in listener, over HTTPS once the domain has a
// certificate.
func (c *Core) EffectiveSubscriptionBase(ctx context.Context) string {
	if manual, _ := c.SubscriptionBaseURL(ctx); manual != "" {
		return manual
	}
	port := c.SubscriptionPort(ctx)
	if port == 0 {
		return ""
	}
	if domain, _ := c.Domain(ctx); domain != "" && c.Certificate(domain) != nil {
		return "https://" + net.JoinHostPort(domain, strconv.Itoa(port))
	}
	host, _ := c.PublicHost(ctx)
	if host == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// SubscriptionCertificate returns the ACME certificate for the domain from
// sing-box's storage, for serving subscriptions over HTTPS; nil until issued.
func (c *Core) SubscriptionCertificate(ctx context.Context) *tls.Certificate {
	domain, _ := c.Domain(ctx)
	if domain == "" || c.acme.StorageDir == "" {
		return nil
	}
	matches, _ := filepath.Glob(filepath.Join(c.acme.StorageDir, "certificates", "*", domain, domain+".crt"))
	var best *tls.Certificate
	for _, crt := range matches {
		cert, err := tls.LoadX509KeyPair(crt, strings.TrimSuffix(crt, ".crt")+".key")
		if err != nil || cert.Leaf == nil {
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
