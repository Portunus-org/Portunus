package proxy

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func testInbounds(t *testing.T) []Inbound {
	t.Helper()
	var out []Inbound
	for i, p := range Protocols {
		set, sec, err := Provision(p, ProvisionOptions{ServerName: "example.com", MasqueradeURL: "https://www.bing.com", Obfs: true})
		if err != nil {
			t.Fatalf("provision %s: %v", p, err)
		}
		port := 20000 + i
		out = append(out, Inbound{ID: int64(i + 1), Tag: Tag(p, port), Protocol: p, Port: port, Enabled: true, Settings: set, Secret: sec})
	}
	return out
}

var testClients = []Client{
	{ID: 1, Name: "alice", UUID: NewUUID(), Password: NewPassword()},
	{ID: 2, Name: "bob", UUID: NewUUID(), Password: NewPassword()},
}

// singBoxCheck validates cfg with a real sing-box binary when
// PORTUNUS_TEST_SINGBOX points at one; otherwise it only checks JSON.
func singBoxCheck(t *testing.T, cfg []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(cfg, &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Log("PORTUNUS_TEST_SINGBOX not set; skipping sing-box check")
		return
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "check", "-c", path).CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check: %v\n%s\n%s", err, out, cfg)
	}
}

func TestServerConfig(t *testing.T) {
	ins := testInbounds(t)
	cfg, err := ServerConfig(ins, testClients, "127.0.0.1:2054", ACMEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	if !strings.Contains(string(cfg), `"c2"`) {
		t.Fatal("stats users missing")
	}

	// No clients yet: inbounds must still produce a valid config.
	cfg, err = ServerConfig(ins, nil, "127.0.0.1:2054", ACMEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)

	for i := range ins {
		ins[i].Enabled = false
	}
	if cfg, _ := ServerConfig(ins, testClients, "127.0.0.1:2054", ACMEOptions{}); cfg != nil {
		t.Fatal("expected no config when every inbound is disabled")
	}
}

func TestClientProfile(t *testing.T) {
	cfg, err := ClientProfile("203.0.113.10", testInbounds(t), testClients[0])
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
}

// Inbound names are free text: equal names, or names equal to the profile's
// own group tags, must still give a config with unique tags.
func TestClientProfileUniqueTags(t *testing.T) {
	ins := testInbounds(t)
	ins[0].Name, ins[1].Name, ins[2].Name = "Main", "Main", "proxy"
	cfg, err := ClientProfile("203.0.113.10", ins, testClients[0])
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, o := range parsed.Outbounds {
		if seen[o.Tag] {
			t.Fatalf("duplicate outbound tag %q", o.Tag)
		}
		seen[o.Tag] = true
	}
	if !seen["Main"] || !seen["Main 2"] || !seen["proxy 2"] {
		t.Fatalf("tags: %v", seen)
	}
	singBoxCheck(t, cfg)

	// Unnamed inbounds are labelled by protocol and port, not by the internal tag.
	if got := (Inbound{Protocol: Hysteria2, Port: 443, Tag: "hysteria2-443"}).Label(); got != "Hysteria2 443" {
		t.Fatalf("label: %q", got)
	}
}

func TestShareLinks(t *testing.T) {
	for _, in := range testInbounds(t) {
		link := ShareLink("203.0.113.10", in, testClients[0])
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if u.Host != "203.0.113.10:"+strconv.Itoa(in.Port) || u.User == nil {
			t.Fatalf("%s: bad host or userinfo", link)
		}
		switch in.Protocol {
		case VLESS:
			if u.Query().Get("pbk") != in.Settings.RealityPublicKey || u.User.Username() != testClients[0].UUID {
				t.Fatalf("vless link: %s", link)
			}
		case Hysteria2:
			if u.Query().Get("obfs") != "salamander" || u.Query().Get("obfs-password") != in.Settings.ObfsPassword {
				t.Fatalf("hysteria2 obfs params: %s", link)
			}
		case Shadowsocks:
			if got := u.User.Username(); got != "2022-blake3-aes-128-gcm:"+in.Secret.ServerPSK+":"+testClients[0].Password {
				t.Fatalf("ss userinfo: %s", got)
			}
		}
	}
	// IPv6 hosts must be bracketed.
	if l := ShareLink("2001:db8::1", testInbounds(t)[0], testClients[0]); !strings.Contains(l, "@[2001:db8::1]:") {
		t.Fatalf("ipv6 link: %s", l)
	}
}

func TestACMEConfig(t *testing.T) {
	ins := testInbounds(t)
	for i := range ins {
		if ins[i].Protocol.UsesTLSCert() {
			ins[i].Settings.TLSMode = TLSACME
			ins[i].Settings.ServerName = "proxy.example.com"
		}
	}
	for _, acme := range []ACMEOptions{
		{DataDirectory: "acme", Email: "ops@example.com"},
		{DataDirectory: "acme", HTTPPort: 8088},
		{DataDirectory: "acme", CloudflareToken: "token", Directory: "https://acme-staging-v02.api.letsencrypt.org/directory"},
	} {
		cfg, err := ServerConfig(ins, testClients, "127.0.0.1:2054", acme)
		if err != nil {
			t.Fatal(err)
		}
		singBoxCheck(t, cfg)
		var v struct {
			Providers []map[string]any `json:"certificate_providers"`
		}
		json.Unmarshal(cfg, &v)
		// One provider, one domain, shared by all three TLS inbounds.
		if len(v.Providers) != 1 || len(v.Providers[0]["domain"].([]any)) != 1 {
			t.Fatalf("providers: %v", v.Providers)
		}
	}

	// Clients verify an ACME certificate instead of skipping verification.
	for _, in := range ins {
		if !in.Protocol.UsesTLSCert() {
			continue
		}
		link := ShareLink("proxy.example.com", in, testClients[0])
		if strings.Contains(link, "insecure") || strings.Contains(link, "pinSHA256") || strings.Contains(link, "Insecure") {
			t.Fatalf("ACME link still skips verification: %s", link)
		}
		if _, pinned := ClientOutbound("proxy.example.com", in, testClients[0])["tls"].(obj)["certificate"]; pinned {
			t.Fatalf("%s: ACME outbound still pins the self-signed certificate", in.Protocol)
		}
	}
}

func TestFileCertificateConfig(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM, _, err := selfSigned("proxy.example.com")
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	os.WriteFile(certPath, []byte(certPEM), 0o600)
	os.WriteFile(keyPath, []byte(keyPEM), 0o600)

	ins := testInbounds(t)
	for i := range ins {
		if ins[i].Protocol.UsesTLSCert() {
			ins[i].Settings.TLSMode = TLSFile
			ins[i].Settings.ServerName = "proxy.example.com"
			ins[i].Settings.CertificatePath, ins[i].Settings.KeyPath = certPath, keyPath
		}
	}
	cfg, err := ServerConfig(ins, testClients, "127.0.0.1:2054", ACMEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
	if strings.Contains(string(cfg), "certificate_providers") {
		t.Fatal("file certificates must not start ACME")
	}
	for _, in := range ins {
		if in.Protocol.UsesTLSCert() && strings.Contains(ShareLink("proxy.example.com", in, testClients[0]), "nsecure") {
			t.Fatalf("%s: file certificate link skips verification", in.Protocol)
		}
	}
}

func TestPrivateCA(t *testing.T) {
	certPEM, keyPEM, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}
	ca, err := ParseCA(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	set, sec, err := Provision(Hysteria2, ProvisionOptions{ServerName: "vpn.example.com", CA: ca})
	if err != nil {
		t.Fatal(err)
	}
	if set.CACertificate != ca.CertPEM || sec.TLSKey == "" {
		t.Fatal("certificate not issued by the CA")
	}
	// The leaf verifies against the CA for its name, and stays under Apple's
	// 825-day limit.
	leaf, _ := pem.Decode([]byte(set.Certificate))
	cert, err := x509.ParseCertificate(leaf.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := cert.Verify(x509.VerifyOptions{DNSName: "vpn.example.com", Roots: pool}); err != nil {
		t.Fatalf("leaf does not verify against the CA: %v", err)
	}
	if days := cert.NotAfter.Sub(cert.NotBefore).Hours() / 24; days > 825 {
		t.Fatalf("leaf valid for %.0f days", days)
	}

	in := Inbound{ID: 1, Tag: "hysteria2-443", Protocol: Hysteria2, Port: 443, Enabled: true, Settings: set, Secret: sec}
	// Profiles trust the CA, not the leaf.
	if got := ClientOutbound("vpn.example.com", in, testClients[0])["tls"].(obj)["certificate"].([]string)[0]; got != ca.CertPEM {
		t.Fatal("profile does not trust the CA")
	}
	// Links pin by default, verify normally in strict mode.
	if l := ShareLink("vpn.example.com", in, testClients[0]); !strings.Contains(l, "pinSHA256="+set.CertSHA256) {
		t.Fatalf("default link does not pin: %s", l)
	}
	in.Settings.StrictTLS = true
	if l := ShareLink("vpn.example.com", in, testClients[0]); strings.Contains(l, "insecure") {
		t.Fatalf("strict link skips verification: %s", l)
	}
	cfg, err := ServerConfig([]Inbound{in}, testClients, "127.0.0.1:2054", ACMEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	singBoxCheck(t, cfg)
}
