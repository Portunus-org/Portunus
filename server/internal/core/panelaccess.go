package core

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"github.com/PaulichP/Portunus/server/internal/proxy"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The panel answers on 127.0.0.1 only, for an SSH tunnel. Once the server
// has a domain, it can also be opened at https://<domain>: a listener of the
// panel's own, with a certificate from Let's Encrypt (obtained and renewed
// by the panel) or one the operator supplies.
//
// What listens there must not tell a stranger what it is. Until a browser
// has opened the secret entry address (https://<domain>/<gate>), every
// request is answered like a path that does not exist; see api: gated.
const (
	settingPanelPort     = "panel_port" // 0: off
	settingPanelCertMode = "panel_cert_mode"
	settingAccessCert    = "panel_access_cert"
	settingAccessKey     = "panel_access_key" // encrypted
	settingPanelGate     = "panel_gate"
)

// Where the panel's certificate comes from.
const (
	PanelCertACME = "acme"
	PanelCertOwn  = "own"
)

// Loopback ports of the panel's own (variables, so that tests can move them
// out of the way of a panel running on the same machine).
var (
	// PanelInnerPort is where the panel's HTTPS also listens, on loopback:
	// a VLESS + REALITY connection on the panel's public port passes
	// everybody who is not its client on to here (see PanelAccess.Shared).
	PanelInnerPort = 2058
	// DecoyPort serves the cover site in plain HTTP, on loopback, for
	// connections a Trojan inbound turns away.
	DecoyPort = 2059
)

// DefaultPanelPort is the HTTPS port: Let's Encrypt can check a server's
// right to a domain on it without port 80 (TLS-ALPN-01), and an address
// without a port number is what one expects of a site.
const DefaultPanelPort = 443

// PanelAccess is how the panel is reached by its domain.
type PanelAccess struct {
	Enabled bool
	Domain  string
	Port    int
	Mode    string
	// Certificate (own mode) is the PEM chain; its key stays inside.
	Certificate string
	key         string
	// Gate is the secret path that lets a browser in.
	Gate string
	// Decoy is the site shown to everybody else, when the operator chose
	// one; empty means the site the panel makes itself (package cover).
	Decoy string
	// Shared: a VLESS connection listens on Port, and the panel is reached
	// through it.
	Shared bool
}

// URL is the entry address to give to the operator ("" when off).
func (p PanelAccess) URL() string {
	if !p.Enabled {
		return ""
	}
	return p.Origin() + "/" + p.Gate
}

// Origin is the panel's address without a path.
func (p PanelAccess) Origin() string {
	if p.Port == 443 {
		return "https://" + p.Domain
	}
	return "https://" + net.JoinHostPort(p.Domain, strconv.Itoa(p.Port))
}

// KeyPair is the operator's certificate, ready to serve (own mode).
func (p PanelAccess) KeyPair() (*tls.Certificate, error) {
	cert, err := tls.X509KeyPair([]byte(p.Certificate), []byte(p.key))
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

func (c *Core) PanelAccess(ctx context.Context) (PanelAccess, error) {
	var (
		p   PanelAccess
		err error
	)
	if p.Domain, err = c.Domain(ctx); err != nil {
		return p, err
	}
	port, _ := c.store.Setting(ctx, settingPanelPort)
	p.Port, _ = strconv.Atoi(port)
	p.Enabled = p.Port > 0 && p.Domain != ""
	if p.Mode, _ = c.store.Setting(ctx, settingPanelCertMode); p.Mode == "" {
		p.Mode = PanelCertACME
	}
	p.Certificate, _ = c.store.Setting(ctx, settingAccessCert)
	if sealed, _ := c.store.Setting(ctx, settingAccessKey); sealed != "" {
		p.key, _ = c.box.Open(sealed, "setting:"+settingAccessKey)
	}
	p.Gate, _ = c.store.Setting(ctx, settingPanelGate)
	p.Decoy = c.Decoy(ctx)
	p.Shared = p.Enabled && c.vlessOn(ctx, p.Port) != nil
	return p, nil
}

// PanelPort is the TCP port the panel's HTTPS listener holds (0: none).
func (c *Core) PanelPort(ctx context.Context) int {
	p, err := c.PanelAccess(ctx)
	if err != nil || !p.Enabled {
		return 0
	}
	return p.Port
}

// PanelAccessInput is what the operator sets. Certificate and Key are given
// together, in own mode, and only when they change.
type PanelAccessInput struct {
	// Decoy, when given, replaces the cover site.
	Decoy       *string
	Enabled     bool
	Port        int
	Mode        string
	Certificate *string
	Key         *string
}

func (c *Core) SetPanelAccess(ctx context.Context, in PanelAccessInput) error {
	cur, err := c.PanelAccess(ctx)
	if err != nil {
		return err
	}
	defer c.Reconcile(ctx)
	if in.Decoy != nil {
		decoy := strings.TrimRight(strings.TrimSpace(*in.Decoy), "/")
		if decoy != "" {
			u, err := url.Parse(decoy)
			if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return invalid("decoy", "invalid_url")
			}
		}
		if err := c.store.SetSetting(ctx, settingDecoy, decoy); err != nil {
			return err
		}
	}
	if !in.Enabled {
		// A VLESS connection that presented the panel's domain needs
		// another site to present: the panel will not be there to answer.
		if v := c.vlessOn(ctx, cur.Port); cur.Enabled && v != nil && v.Settings.RealityServer == cur.Domain {
			set := v.Settings
			set.RealityServer, set.RealityServerPort = proxy.DefaultRealityServer, 443
			if _, err := c.saveInbound(ctx, v.ID, v.Protocol, v.Name, v.Port, v.Enabled, set, v.Secret); err != nil {
				return err
			}
		}
		return c.store.SetSetting(ctx, settingPanelPort, "0")
	}
	if cur.Domain == "" {
		return invalid("domain", "required")
	}
	if in.Port == 0 {
		in.Port = DefaultPanelPort
	}
	if in.Port < 1 || in.Port > 65535 {
		return invalid("port", "out_of_range")
	}
	switch in.Mode {
	case "", PanelCertACME:
		in.Mode = PanelCertACME
		// Let's Encrypt connects to port 443 and nowhere else for the check
		// the panel can answer by itself.
		if in.Port != 443 {
			return invalid("port", "acme_needs_443")
		}
	case PanelCertOwn:
	default:
		return invalid("mode", "unsupported")
	}
	if !cur.Enabled || cur.Port != in.Port {
		for _, r := range c.reserved {
			if r.Port == in.Port {
				return invalid("port", "reserved")
			}
		}
		// A VLESS connection may hold the port: the two then share it. Any
		// other listener may not.
		shared := c.vlessOn(ctx, in.Port)
		if (shared == nil && c.tcpPortTaken(ctx, in.Port)) || in.Port == c.SubscriptionPort(ctx) || in.Port == c.acmeListenPort(ctx) {
			return invalid("port", "in_use")
		}
		if c.hostPorts != nil && shared == nil {
			if tcp, _ := c.hostPorts(); tcp[in.Port] {
				return invalid("port", "busy_on_host")
			}
		}
	}
	if in.Mode == PanelCertOwn {
		certPEM, keyPEM := cur.Certificate, cur.key
		if in.Certificate != nil && in.Key != nil && (strings.TrimSpace(*in.Certificate) != "" || strings.TrimSpace(*in.Key) != "") {
			certPEM, keyPEM = strings.TrimSpace(*in.Certificate)+"\n", strings.TrimSpace(*in.Key)+"\n"
		}
		if strings.TrimSpace(certPEM) == "" || strings.TrimSpace(keyPEM) == "" {
			return invalid("certificate", "required")
		}
		pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return invalid("certificate", "invalid_certificate")
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return invalid("certificate", "invalid_certificate")
		}
		if leaf.VerifyHostname(cur.Domain) != nil {
			return invalid("certificate", "certificate_other_domain")
		}
		if !c.now().Before(leaf.NotAfter) {
			return invalid("certificate", "certificate_expired")
		}
		if err := c.store.SetSetting(ctx, settingAccessCert, certPEM); err != nil {
			return err
		}
		if err := c.store.SetSetting(ctx, settingAccessKey, c.box.Seal(keyPEM, "setting:"+settingAccessKey)); err != nil {
			return err
		}
	}
	if cur.Gate == "" {
		if err := c.store.SetSetting(ctx, settingPanelGate, newGate()); err != nil {
			return err
		}
	}
	if err := c.store.SetSetting(ctx, settingPanelCertMode, in.Mode); err != nil {
		return err
	}
	if err := c.store.SetSetting(ctx, settingPanelPort, strconv.Itoa(in.Port)); err != nil {
		return err
	}
	return c.shareWithPanel(ctx)
}

const settingDecoy = "decoy_url"

// Decoy is the site of the operator's choice that strangers are shown
// instead of a refusal; empty when the panel shows its own.
func (c *Core) Decoy(ctx context.Context) string {
	v, _ := c.store.Setting(ctx, settingDecoy)
	return v
}

// CoverSeed is what the panel's own cover site is generated from: it stays
// the same for this installation and cannot be guessed from outside.
func (c *Core) CoverSeed() []byte { return c.box.Derive("cover site") }

// vlessOn returns the enabled VLESS inbound on the TCP port, if any.
func (c *Core) vlessOn(ctx context.Context, port int) *proxy.Inbound {
	ins, err := c.Inbounds(ctx)
	if err != nil {
		return nil
	}
	for i := range ins {
		if ins[i].Protocol == proxy.VLESS && ins[i].Port == port && ins[i].Enabled {
			return &ins[i]
		}
	}
	return nil
}

// shareWithPanel makes a VLESS connection on the panel's port present the
// panel's own domain: to a stranger the two are then one HTTPS site with a
// real certificate. It is the name clients are given as well, so links
// change when this does.
func (c *Core) shareWithPanel(ctx context.Context) error {
	p, err := c.PanelAccess(ctx)
	if err != nil || !p.Enabled {
		return err
	}
	in := c.vlessOn(ctx, p.Port)
	if in == nil || (in.Settings.RealityServer == p.Domain && in.Settings.RealityServerPort == p.Port) {
		return nil
	}
	set := in.Settings
	set.RealityServer, set.RealityServerPort = p.Domain, p.Port
	_, err = c.saveInbound(ctx, in.ID, in.Protocol, in.Name, in.Port, in.Enabled, set, in.Secret)
	return err
}

// RotatePanelGate replaces the secret entry address: the old one, and the
// browsers let in through it, stop being recognised.
func (c *Core) RotatePanelGate(ctx context.Context) error {
	return c.store.SetSetting(ctx, settingPanelGate, newGate())
}

// gateWordList is what a secret path is chosen from: the EFF long word
// list (eff.org/dice, CC BY 3.0 US), its words of four to eight letters.
// Its authors chose them to be easy to read and free of rude ones.
//
//go:embed gatewords.txt
var gateWordList string

// gateReserved are words a secret path must not be: the panel's own paths,
// the paths the cover site may use, and the first things anyone would try.
var gateReserved = map[string]bool{
	"login": true, "welcome": true, "account": true, "clients": true, "docs": true, "inbounds": true,
	"nodes": true, "routing": true, "settings": true, "healthz": true, "icon": true, "static": true,
	"about": true, "notes": true, "posts": true, "journal": true, "entries": true, "style": true,
	"main": true, "site": true, "screen": true, "base": true, "blog": true, "feed": true, "index": true,
	"admin": true, "panel": true, "secret": true, "private": true, "manage": true, "console": true,
	"dashboard": true, "portal": true, "control": true, "system": true, "server": true,
}

var gateWords = func() []string {
	var out []string
	for _, w := range strings.Fields(gateWordList) {
		if !gateReserved[w] {
			out = append(out, w)
		}
	}
	return out
}()

// newGate makes up a secret path: one random word, something a person can
// read out and remember. A word alone is a weak secret, some thirteen bits,
// so the listener counts wrong guesses and shuts the gate to whoever makes
// too many (api: gateGuard). The path only keeps the panel out of sight;
// signing in still takes the password.
func newGate() string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(gateWords))))
	if err != nil {
		panic(err) // no randomness: nothing safe can be made up
	}
	return gateWords[n.Int64()]
}

// PanelGateToken is what a browser that came through the gate carries from
// then on. It is derived from the gate, so a new gate voids it, and from
// the master key, so it cannot be made up from the gate alone elsewhere.
func (c *Core) PanelGateToken(gate string) string {
	mac := hmac.New(sha256.New, c.box.Derive("panel gate"))
	mac.Write([]byte(gate))
	return hex.EncodeToString(mac.Sum(nil))
}

// ACMEEmail is the address given to Let's Encrypt, if any.
func (c *Core) ACMEEmail(ctx context.Context) string {
	s, _ := c.ACME(ctx)
	return s.Email
}

// PanelCertFiles are where the panel keeps a copy of the certificate it
// serves its own HTTPS with, for sing-box to read: the chain and the key.
// Both are empty when there is no such place (see Options.PanelCertDir).
func (c *Core) PanelCertFiles() (cert, key string) {
	if c.panelCertDir == "" {
		return "", ""
	}
	return filepath.Join(c.panelCertDir, "fullchain.pem"), filepath.Join(c.panelCertDir, "key.pem")
}

// sharedCertificate describes the panel's certificate when it can serve
// domain right now: it is there, it names the domain and it has not
// expired. Otherwise nil.
func (c *Core) sharedCertificate(domain string) *CertificateInfo {
	cert, key := c.PanelCertFiles()
	if cert == "" || domain == "" {
		return nil
	}
	if _, err := os.Stat(key); err != nil {
		return nil
	}
	info, err := readCertificate(cert)
	if err != nil || !c.now().Before(info.NotAfter) {
		return nil
	}
	if !certCovers(info.names, domain) {
		return nil
	}
	return info
}

// shareCertificate lets an inbound that would ask Let's Encrypt for a
// certificate use the panel's instead, when the panel is opened by the
// same domain and has one. Nothing about it is stored: if the panel stops
// being opened by the domain, the inbound gets a certificate of its own
// again at the next round.
func (c *Core) shareCertificate(in *proxy.Inbound, pa PanelAccess) {
	if !in.ACME() || !pa.Enabled || !strings.EqualFold(in.Settings.ServerName, pa.Domain) {
		return
	}
	if c.sharedCertificate(pa.Domain) != nil {
		in.SharedCert, in.SharedKey = c.PanelCertFiles()
	}
}
