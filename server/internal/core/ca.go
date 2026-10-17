package core

import (
	"context"
	"sync"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

const (
	settingCACert = "ca_certificate"
	settingCAKey  = "ca_key" // encrypted
)

var caMu sync.Mutex

// CA returns the installation's private certificate authority, creating it
// on first use. Its key is stored encrypted with the master key.
func (c *Core) CA(ctx context.Context) (*proxy.CA, error) {
	caMu.Lock()
	defer caMu.Unlock()
	if c.ca != nil {
		return c.ca, nil
	}
	certPEM, err := c.store.Setting(ctx, settingCACert)
	if err != nil {
		return nil, err
	}
	sealed, err := c.store.Setting(ctx, settingCAKey)
	if err != nil {
		return nil, err
	}
	if certPEM != "" && sealed != "" {
		keyPEM, err := c.box.Open(sealed, "setting:"+settingCAKey)
		if err != nil {
			return nil, err
		}
		ca, err := proxy.ParseCA(certPEM, keyPEM)
		if err != nil {
			return nil, err
		}
		c.ca = ca
		return ca, nil
	}
	certPEM, keyPEM, err := proxy.NewCA()
	if err != nil {
		return nil, err
	}
	if err := c.store.SetSetting(ctx, settingCAKey, c.box.Seal(keyPEM, "setting:"+settingCAKey)); err != nil {
		return nil, err
	}
	if err := c.store.SetSetting(ctx, settingCACert, certPEM); err != nil {
		return nil, err
	}
	ca, err := proxy.ParseCA(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	c.log.Info("created the private certificate authority", "subject", ca.Cert.Subject.CommonName)
	c.ca = ca
	return ca, nil
}

// renewPrivateCertificates reissues private server certificates that expire
// within proxy.LeafRenewBefore, or that were not issued by the current CA
// (older installations). Clients trust the CA, so this needs no re-import.
func (c *Core) renewPrivateCertificates(ctx context.Context) {
	ins, err := c.Inbounds(ctx)
	if err != nil {
		return
	}
	for _, in := range ins {
		if !in.Protocol.UsesTLSCert() {
			continue
		}
		ca, err := c.CA(ctx)
		if err != nil {
			c.log.Error("load certificate authority", "err", err)
			return
		}
		exp, err := proxy.CertExpiry(in.Settings.Certificate)
		if err == nil && time.Until(exp) > proxy.LeafRenewBefore && in.Settings.CACertificate == ca.CertPEM {
			continue
		}
		set, sec := in.Settings, in.Secret
		if err := proxy.IssuePrivate(&set, &sec, ca); err != nil {
			c.log.Error("renew private certificate", "inbound", in.Tag, "err", err)
			continue
		}
		if _, err := c.saveInbound(ctx, in.ID, in.Protocol, in.Name, in.Port, in.Enabled, set, sec); err != nil {
			c.log.Error("save renewed certificate", "inbound", in.Tag, "err", err)
			continue
		}
		c.log.Info("renewed private certificate", "inbound", in.Tag)
	}
}
