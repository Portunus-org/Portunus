// Package proxy is the protocol layer: it provisions key material for
// inbounds, renders the sing-box server configuration, and produces what
// clients import (share links and sing-box client profiles).
//
// Everything here is pure: no I/O, no database. That keeps the generated
// configs easy to test against `sing-box check`.
package proxy

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"time"
)

type Protocol string

const (
	VLESS       Protocol = "vless"
	Hysteria2   Protocol = "hysteria2"
	Trojan      Protocol = "trojan"
	Shadowsocks Protocol = "shadowsocks"
	TUIC        Protocol = "tuic"
)

var Protocols = []Protocol{VLESS, Hysteria2, Trojan, Shadowsocks, TUIC}

func (p Protocol) Valid() bool {
	for _, q := range Protocols {
		if p == q {
			return true
		}
	}
	return false
}

// UsesTCP and UsesUDP tell which listeners an inbound occupies, so that a TCP
// and a UDP protocol can share a port number (e.g. VLESS and Hysteria2 on 443).
func (p Protocol) UsesTCP() bool { return p == VLESS || p == Trojan || p == Shadowsocks }
func (p Protocol) UsesUDP() bool { return p == Hysteria2 || p == TUIC || p == Shadowsocks }

// Settings are the public parameters of an inbound, stored as JSON.
type Settings struct {
	// VLESS + REALITY: the site whose TLS handshake is borrowed.
	RealityServer     string `json:"realityServer,omitempty"`
	RealityServerPort int    `json:"realityServerPort,omitempty"`
	RealityPublicKey  string `json:"realityPublicKey,omitempty"`
	RealityShortID    string `json:"realityShortId,omitempty"`

	// Hysteria2, TUIC, Trojan: TLS for ServerName.
	//   - "acme": issued by Let's Encrypt through sing-box's ACME provider;
	//   - "file": an existing certificate on disk (e.g. from certbot or Nginx
	//     Proxy Manager), reloaded by sing-box when it changes;
	//   - "" (private): Certificate below, issued by the installation's own
	//     CA (CACertificate). sing-box profiles trust the CA, so renewals are
	//     seamless; devices can install the CA. Share links either pin the
	//     certificate (insecure=1 + pinSHA256) or, with StrictTLS, verify
	//     normally against a CA installed on the device.
	// In the first two cases clients verify the certificate normally.
	ServerName      string  `json:"serverName,omitempty"`
	TLSMode         TLSMode `json:"tlsMode,omitempty"`
	CertificatePath string  `json:"certificatePath,omitempty"`
	KeyPath         string  `json:"keyPath,omitempty"`
	Certificate     string  `json:"certificate,omitempty"`
	CertSHA256      string  `json:"certSha256,omitempty"`
	CACertificate   string  `json:"caCertificate,omitempty"`
	StrictTLS       bool    `json:"strictTls,omitempty"`

	// Hysteria2: what a plain HTTP/3 request (a probe) sees instead of a 404,
	// and optional Salamander obfuscation, which makes packets unrecognisable
	// as QUIC. The obfuscation password is shared by all clients of the inbound.
	MasqueradeURL string `json:"masqueradeUrl,omitempty"`
	ObfsPassword  string `json:"obfsPassword,omitempty"`

	// Shadowsocks
	Method string `json:"method,omitempty"`
}

// Secret is the private key material of an inbound; stored encrypted.
type Secret struct {
	RealityPrivateKey string `json:"realityPrivateKey,omitempty"`
	TLSKey            string `json:"tlsKey,omitempty"`
	ServerPSK         string `json:"serverPsk,omitempty"`
}

type Inbound struct {
	// RealityDial, when set, is where a REALITY inbound takes the handshake
	// it shows to strangers from, instead of Settings.RealityServer: the
	// panel's own HTTPS listener on this machine, when the two share a port.
	RealityDial string
	// Fallback, when set, is the local address (host:port) a Trojan inbound
	// hands a connection to that is not Trojan: a web server, so a browser
	// or a probe gets a site and not a closed connection.
	Fallback string
	ID       int64
	Tag      string
	Name     string
	Protocol Protocol
	Port     int
	Enabled  bool
	Settings Settings
	Secret   Secret
}

// Label is the human-readable name of an inbound: what client apps show for
// its link. Unnamed inbounds are called after their protocol and port.
func (in Inbound) Label() string {
	if in.Name != "" {
		return in.Name
	}
	return fmt.Sprintf("%s %d", in.Protocol.Display(), in.Port)
}

// Display is the protocol's name as shown to people.
func (p Protocol) Display() string {
	switch p {
	case VLESS:
		return "VLESS Reality"
	case Hysteria2:
		return "Hysteria2"
	case TUIC:
		return "TUIC"
	case Trojan:
		return "Trojan"
	case Shadowsocks:
		return "Shadowsocks"
	}
	return string(p)
}

type TLSMode string

const (
	TLSSelfSigned TLSMode = ""
	TLSACME       TLSMode = "acme"
	TLSFile       TLSMode = "file"
)

// UsesTLSCert reports whether the protocol carries its own TLS certificate
// (as opposed to REALITY or none).
func (p Protocol) UsesTLSCert() bool { return p == Hysteria2 || p == TUIC || p == Trojan }

// ACME reports whether this inbound's certificate is issued by ACME.
func (in Inbound) ACME() bool { return in.Protocol.UsesTLSCert() && in.Settings.TLSMode == TLSACME }

// TrustedCert reports whether clients can verify the certificate against
// public roots (ACME or an operator-provided file) rather than pinning it.
func (in Inbound) TrustedCert() bool {
	return in.Protocol.UsesTLSCert() && (in.Settings.TLSMode == TLSACME || in.Settings.TLSMode == TLSFile)
}

type Client struct {
	ID       int64
	Name     string
	UUID     string
	Password string
}

// UserName is the name a client carries inside sing-box. It is derived from
// the immutable ID so traffic counters survive renames.
func (c Client) UserName() string { return "c" + strconv.FormatInt(c.ID, 10) }

// Tag is the canonical sing-box tag for an inbound.
func Tag(p Protocol, port int) string { return fmt.Sprintf("%s-%d", p, port) }

const (
	DefaultRealityServer = "www.microsoft.com"
	DefaultServerName    = "localhost"
	ssMethod             = "2022-blake3-aes-128-gcm"
	vlessFlow            = "xtls-rprx-vision"
)

// ProvisionOptions are the user choices made when an inbound is created.
type ProvisionOptions struct {
	RealityServer     string
	RealityServerPort int
	ServerName        string
	TLSMode           TLSMode
	CertificatePath   string
	KeyPath           string
	StrictTLS         bool
	// CA issues private certificates; nil falls back to a standalone
	// self-signed certificate (tests only).
	CA *CA
	// Hysteria2
	MasqueradeURL string
	Obfs          bool
}

// Provision generates fresh key material for a new inbound, or for key
// rotation of an existing one (pass its current options to keep them).
func Provision(p Protocol, o ProvisionOptions) (Settings, Secret, error) {
	var (
		set Settings
		sec Secret
	)
	switch p {
	case VLESS:
		priv, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return set, sec, err
		}
		set.RealityServer = o.RealityServer
		if set.RealityServer == "" {
			set.RealityServer = DefaultRealityServer
		}
		set.RealityServerPort = o.RealityServerPort
		if set.RealityServerPort == 0 {
			set.RealityServerPort = 443
		}
		set.RealityPublicKey = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
		set.RealityShortID = randomHex(8)
		sec.RealityPrivateKey = base64.RawURLEncoding.EncodeToString(priv.Bytes())

	case Hysteria2, TUIC, Trojan:
		name := o.ServerName
		if name == "" {
			name = DefaultServerName
		}
		set.ServerName = name
		set.TLSMode = o.TLSMode
		set.StrictTLS = o.StrictTLS
		if o.TLSMode == TLSFile {
			set.CertificatePath, set.KeyPath = o.CertificatePath, o.KeyPath
		}
		// Every TLS inbound keeps a private certificate as a fallback, so
		// switching away from ACME or files never leaves it without one.
		if err := IssuePrivate(&set, &sec, o.CA); err != nil {
			return set, sec, err
		}
		if p == Hysteria2 {
			set.MasqueradeURL = o.MasqueradeURL
			if o.Obfs {
				set.ObfsPassword = NewObfsPassword()
			}
		}

	case Shadowsocks:
		set.Method = ssMethod
		sec.ServerPSK = NewPassword()

	default:
		return set, sec, fmt.Errorf("unsupported protocol %q", p)
	}
	return set, sec, nil
}

// IssuePrivate (re)issues the private certificate of a TLS inbound for its
// ServerName: from ca when given, otherwise standalone self-signed.
func IssuePrivate(set *Settings, sec *Secret, ca *CA) error {
	if ca == nil {
		certPEM, keyPEM, sum, err := selfSigned(set.ServerName)
		if err != nil {
			return err
		}
		set.Certificate, set.CertSHA256, set.CACertificate, sec.TLSKey = certPEM, sum, "", keyPEM
		return nil
	}
	certPEM, keyPEM, sum, _, err := ca.Issue(set.ServerName)
	if err != nil {
		return err
	}
	set.Certificate, set.CertSHA256, set.CACertificate, sec.TLSKey = certPEM, sum, ca.CertPEM, keyPEM
	return nil
}

// NewPassword returns base64 of 16 random bytes. That is a valid
// 2022-blake3-aes-128-gcm key and a strong password for every other protocol,
// so one client credential works everywhere.
func NewPassword() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// NewObfsPassword returns a random Salamander password (URL-safe).
func NewObfsPassword() string {
	b := make([]byte, 18)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewUUID returns a random (version 4) UUID.
func NewUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomHex(n int) string {
	b := make([]byte, n/2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// selfSigned creates an ECDSA P-256 certificate for name, valid 10 years.
// It returns PEM certificate, PEM key and the hex SHA-256 of the DER
// certificate (what Hysteria2's pinSHA256 expects).
func selfSigned(name string) (string, string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(name); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{name}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", "", err
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		hex.EncodeToString(sum[:]), nil
}
