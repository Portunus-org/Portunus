package core

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/PaulichP/Portunus/server/internal/proxy"
	"github.com/PaulichP/Portunus/server/internal/store"
)

// The server's domain can be moved to another one. Everything that goes by
// the domain (the panel opened by it, HTTPS subscriptions, the links, the
// connections that present it with a Let's Encrypt certificate) moves along
// in one step, so nothing is left on a name that no longer leads here.
//
// When the panel is opened by its domain with a certificate from Let's
// Encrypt, the new domain's certificate is obtained first, while everything
// still works on the old one; the move happens once it is there (see api:
// the panel's listener asks for it, then calls MoveDomain). Otherwise there
// is nothing to obtain it with ahead, and the move is made at once: the
// certificates follow, asked for by what serves the new name.
//
// The certificates of earlier domains are kept until removed.

const settingDomainNext = "domain_next"

// DomainCertificate is a certificate kept for a domain.
type DomainCertificate struct {
	Domain   string    `json:"domain"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"notAfter"`
}

// DomainState is the server's domain as the settings show it.
type DomainState struct {
	Domain string `json:"domain"`
	// Certificate is the one the domain is served with now; nil while there
	// is none (yet).
	Certificate *DomainCertificate `json:"certificate"`
	// Own: the panel is opened with the operator's own certificate, which
	// the panel cannot obtain anew for another name.
	Own bool `json:"own"`
	// Next is the domain being moved to, while its certificate is obtained.
	Next string `json:"next,omitempty"`
	// Others are the certificates kept for other domains, to remove.
	Others []DomainCertificate `json:"others"`
}

func (c *Core) DomainState(ctx context.Context) (DomainState, error) {
	domain, err := c.Domain(ctx)
	if err != nil {
		return DomainState{}, err
	}
	st := DomainState{Domain: domain, Others: []DomainCertificate{}}
	st.Next, _ = c.store.Setting(ctx, settingDomainNext)
	pa, _ := c.PanelAccess(ctx)
	st.Own = pa.Enabled && pa.Mode == PanelCertOwn
	for _, cert := range c.keptCertificates() {
		switch {
		case strings.EqualFold(cert.Domain, domain):
			if st.Certificate == nil || cert.NotAfter.After(st.Certificate.NotAfter) {
				st.Certificate = &cert
			}
		case strings.EqualFold(cert.Domain, st.Next):
		default:
			// One entry for a domain, the one that lasts longest.
			i := slices.IndexFunc(st.Others, func(o DomainCertificate) bool { return strings.EqualFold(o.Domain, cert.Domain) })
			if i < 0 {
				st.Others = append(st.Others, cert)
			} else if cert.NotAfter.After(st.Others[i].NotAfter) {
				st.Others[i] = cert
			}
		}
	}
	// The operator's own certificate is not in either store.
	if st.Certificate == nil && domain != "" {
		if shared := c.sharedCertificate(domain); shared != nil {
			st.Certificate = &DomainCertificate{Domain: domain, Issuer: shared.Issuer, NotAfter: shared.NotAfter}
		}
	}
	slices.SortFunc(st.Others, func(a, b DomainCertificate) int { return strings.Compare(a.Domain, b.Domain) })
	return st, nil
}

// MoveDomainTo starts moving the server to domain. It has to point to this
// server already: a name that leads elsewhere could get no certificate, and
// the panel would be lost behind it. It reports whether the move is done
// (false: waiting for the new domain's certificate).
func (c *Core) MoveDomainTo(ctx context.Context, domain string) (bool, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(domain)), ".")
	current, err := c.Domain(ctx)
	if err != nil {
		return false, err
	}
	if domain == "" {
		return false, invalid("domain", "required")
	}
	if strings.EqualFold(domain, current) {
		return false, invalid("domain", "same_domain")
	}
	check, err := c.CheckDomain(ctx, domain)
	if err != nil {
		return false, err
	}
	if !check.PointsHere {
		return false, invalid("domain", "points_elsewhere")
	}
	pa, err := c.PanelAccess(ctx)
	if err != nil {
		return false, err
	}
	if pa.Enabled && pa.Mode == PanelCertOwn {
		return false, invalid("domain", "own_certificate")
	}
	if pa.Enabled && current != "" {
		// The panel's listener sees this and asks for the certificate.
		return false, c.store.SetSetting(ctx, settingDomainNext, domain)
	}
	return true, c.MoveDomain(ctx, domain)
}

// CancelDomainMove forgets a move that waits for its certificate.
func (c *Core) CancelDomainMove(ctx context.Context) error {
	return c.store.SetSetting(ctx, settingDomainNext, "")
}

// MoveDomain moves everything that goes by the domain to domain, now.
func (c *Core) MoveDomain(ctx context.Context, domain string) error {
	old, err := c.Domain(ctx)
	if err != nil {
		return err
	}
	err = c.AllOrNothing(ctx, func() error {
		if err := c.SetDomain(ctx, domain); err != nil {
			return err
		}
		// The address in the links, when it was the domain, or when this is
		// the first domain: links by a name survive a new IP address.
		if host, _ := c.PublicHost(ctx); host == "" || old == "" || strings.EqualFold(host, old) {
			if err := c.SetPublicHost(ctx, domain); err != nil {
				return err
			}
		}
		return c.store.SetSetting(ctx, settingDomainNext, "")
	})
	if err != nil {
		return err
	}
	if old != "" {
		if err := c.moveInbounds(ctx, old, domain); err != nil {
			return err
		}
	}
	c.log.Info("the server moved to another domain", "from", old, "to", domain)
	c.Reconcile(ctx)
	return nil
}

// moveInbounds has the connections that presented the old domain with a
// certificate from Let's Encrypt present the new one.
func (c *Core) moveInbounds(ctx context.Context, old, domain string) error {
	rows, err := c.store.ListInbounds(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		var set proxy.Settings
		if err := json.Unmarshal([]byte(row.Settings), &set); err != nil {
			return err
		}
		in := proxy.Inbound{Protocol: proxy.Protocol(row.Protocol), Settings: set}
		if !in.ACME() || !strings.EqualFold(set.ServerName, old) {
			continue
		}
		set.ServerName = domain
		raw, err := json.Marshal(set)
		if err != nil {
			return err
		}
		row.Settings = string(raw)
		if err := c.store.UpdateInbound(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

// RemoveCertificate deletes the certificates kept for domain, which must be
// neither the server's domain nor the one it is moving to.
func (c *Core) RemoveCertificate(ctx context.Context, domain string) error {
	domain = strings.ToLower(strings.TrimSpace(domain))
	current, _ := c.Domain(ctx)
	next, _ := c.store.Setting(ctx, settingDomainNext)
	if domain == "" || strings.EqualFold(domain, current) || strings.EqualFold(domain, next) {
		return invalid("domain", "certificate_in_use")
	}
	// A name, never a path: nothing outside the stores is reached.
	if !validHost(domain) || net.ParseIP(domain) != nil || strings.ContainsAny(domain, `/\`) {
		return invalid("domain", "invalid_hostname")
	}
	found := false
	for _, path := range c.certificateFiles(domain) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		found = true
	}
	if !found {
		return store.ErrNotFound
	}
	c.log.Info("certificate removed", "domain", domain)
	return nil
}

// certificateFiles are the files and directories that hold certificates for
// domain: sing-box's (certificates/<issuer>/<domain>/) and the panel's
// (<domain> and <domain>+rsa in its cache).
func (c *Core) certificateFiles(domain string) []string {
	var out []string
	if c.acme.StorageDir != "" {
		dirs, _ := filepath.Glob(filepath.Join(c.acme.StorageDir, "certificates", "*", domain))
		out = append(out, dirs...)
	}
	if c.panelACMEDir != "" {
		out = append(out, filepath.Join(c.panelACMEDir, domain), filepath.Join(c.panelACMEDir, domain+"+rsa"))
	}
	return out
}

// keptCertificates lists every certificate in both stores.
func (c *Core) keptCertificates() []DomainCertificate {
	var out []DomainCertificate
	if c.acme.StorageDir != "" {
		files, _ := filepath.Glob(filepath.Join(c.acme.StorageDir, "certificates", "*", "*", "*.crt"))
		for _, path := range files {
			domain := filepath.Base(filepath.Dir(path))
			if filepath.Base(path) != domain+".crt" {
				continue
			}
			if info, err := readCertificate(path); err == nil {
				out = append(out, DomainCertificate{Domain: domain, Issuer: info.Issuer, NotAfter: info.NotAfter})
			}
		}
	}
	if c.panelACMEDir != "" {
		entries, _ := os.ReadDir(c.panelACMEDir)
		for _, e := range entries {
			name := e.Name()
			// The cache also holds the account key and tokens, which are no domains.
			if e.IsDir() || strings.Contains(name, "+token") || !strings.Contains(name, ".") || strings.HasSuffix(name, "+key") {
				continue
			}
			if info := cachedCertificate(filepath.Join(c.panelACMEDir, name)); info != nil {
				out = append(out, DomainCertificate{Domain: strings.TrimSuffix(name, "+rsa"), Issuer: info.Issuer, NotAfter: info.NotAfter})
			}
		}
	}
	return out
}

// cachedCertificate reads a certificate the panel keeps: its key and then
// its chain, in one PEM file.
func cachedCertificate(path string) *CertificateInfo {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	for {
		var block *pem.Block
		block, raw = pem.Decode(raw)
		if block == nil {
			return nil
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil
		}
		issuer := cert.Issuer.CommonName
		if len(cert.Issuer.Organization) > 0 {
			issuer = cert.Issuer.Organization[0] + " " + issuer
		}
		return &CertificateInfo{Issuer: strings.TrimSpace(issuer), NotAfter: cert.NotAfter.UTC(), names: cert.DNSNames}
	}
}
