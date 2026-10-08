package proxy

import (
	"encoding/json"
	"fmt"
	"slices"
)

// obj is a JSON object; the sing-box config is built from these rather than
// from sing-box's own option types, so Portunus does not link sing-box.
type obj = map[string]any

// ACMEOptions configure certificate issuance for inbounds in ACME mode.
type ACMEOptions struct {
	// DataDirectory is where sing-box keeps accounts and certificates,
	// relative to its working directory.
	DataDirectory string
	Email         string
	// Directory is the ACME directory URL; empty means Let's Encrypt.
	Directory string
	// CloudflareToken switches from HTTP-01 to DNS-01 via Cloudflare.
	CloudflareToken string
	// HTTPPort, when set, answers HTTP-01 on this port instead of 80, for a
	// reverse proxy on port 80 that forwards /.well-known/acme-challenge/.
	HTTPPort int
}

// ACMETag is the certificate provider tag referenced by ACME inbounds.
const ACMETag = "acme"

// ServerConfig renders the sing-box configuration for this node. Only
// enabled inbounds are included; clients must already be filtered to the
// ones allowed to connect. It returns nil when there is nothing to run.
//
// statsAPI is the loopback address for sing-box's v2ray_api stats service.
func ServerConfig(inbounds []Inbound, clients []Client, statsAPI string, acme ACMEOptions, routing Routing) ([]byte, error) {
	return serverConfig(inbounds, clients, statsAPI, acme, routing, nil)
}

func serverConfig(inbounds []Inbound, clients []Client, statsAPI string, acme ACMEOptions, routing Routing, hop *Hop) ([]byte, error) {
	var (
		ins     []any
		domains []string
	)
	for _, in := range inbounds {
		if !in.Enabled {
			continue
		}
		if in.ACME() && !slices.Contains(domains, in.Settings.ServerName) {
			domains = append(domains, in.Settings.ServerName)
		}
		v, err := serverInbound(in, clients)
		if err != nil {
			return nil, fmt.Errorf("inbound %s: %w", in.Tag, err)
		}
		ins = append(ins, v)
	}
	if hop != nil {
		routing.hop = true
		// Traffic arriving over the hop is already accounted for where it
		// entered; its single user is not a client.
		v, err := serverInbound(hop.inbound(), []Client{hop.client()})
		if err != nil {
			return nil, fmt.Errorf("hop inbound: %w", err)
		}
		ins = append(ins, v)
	}
	if len(ins) == 0 {
		return nil, nil
	}

	users := make([]string, 0, len(clients))
	for _, c := range clients {
		users = append(users, c.UserName())
	}

	cfg := obj{
		"log": obj{"level": "warn", "timestamp": true},
		"dns": obj{
			"servers": []any{obj{"type": "local", "tag": "local"}},
		},
		"inbounds": ins,
		"experimental": obj{
			"v2ray_api": obj{
				"listen": statsAPI,
				"stats": obj{
					"enabled": true,
					"users":   users,
				},
			},
		},
	}
	if statsAPI == "" {
		delete(cfg, "experimental")
	}
	routing.apply(cfg)
	if len(domains) > 0 {
		p := obj{
			"type":           "acme",
			"tag":            ACMETag,
			"domain":         domains,
			"data_directory": acme.DataDirectory,
			"key_type":       "p256",
			// TLS-ALPN would need TCP 443, which REALITY usually holds.
			"disable_tls_alpn_challenge": true,
		}
		if acme.Email != "" {
			p["email"] = acme.Email
		}
		if acme.Directory != "" {
			p["provider"] = acme.Directory
		}
		if acme.HTTPPort != 0 {
			p["alternative_http_port"] = acme.HTTPPort
		}
		if acme.CloudflareToken != "" {
			p["disable_http_challenge"] = true
			p["dns01_challenge"] = obj{"provider": "cloudflare", "api_token": acme.CloudflareToken}
		}
		cfg["certificate_providers"] = []any{p}
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func serverInbound(in Inbound, clients []Client) (obj, error) {
	o := obj{
		"type":        string(in.Protocol),
		"tag":         in.Tag,
		"listen":      "::",
		"listen_port": in.Port,
	}
	users := make([]any, 0, len(clients))

	switch in.Protocol {
	case VLESS:
		for _, c := range clients {
			users = append(users, obj{"name": c.UserName(), "uuid": c.UUID, "flow": vlessFlow})
		}
		o["tls"] = obj{
			"enabled":     true,
			"server_name": in.Settings.RealityServer,
			"reality": obj{
				"enabled": true,
				"handshake": obj{
					"server":      in.Settings.RealityServer,
					"server_port": in.Settings.RealityServerPort,
				},
				"private_key": in.Secret.RealityPrivateKey,
				"short_id":    []string{in.Settings.RealityShortID},
			},
		}

	case Hysteria2:
		for _, c := range clients {
			users = append(users, obj{"name": c.UserName(), "password": c.Password})
		}
		o["tls"] = serverTLS(in, "h3")
		if in.Settings.MasqueradeURL != "" {
			o["masquerade"] = obj{"type": "proxy", "url": in.Settings.MasqueradeURL, "rewrite_host": true}
		}
		if in.Settings.ObfsPassword != "" {
			o["obfs"] = obj{"type": "salamander", "password": in.Settings.ObfsPassword}
		}

	case TUIC:
		for _, c := range clients {
			users = append(users, obj{"name": c.UserName(), "uuid": c.UUID, "password": c.Password})
		}
		o["congestion_control"] = "bbr"
		o["tls"] = serverTLS(in, "h3")

	case Trojan:
		for _, c := range clients {
			users = append(users, obj{"name": c.UserName(), "password": c.Password})
		}
		o["tls"] = serverTLS(in, "h2", "http/1.1")

	case Shadowsocks:
		for _, c := range clients {
			users = append(users, obj{"name": c.UserName(), "password": c.Password})
		}
		o["method"] = in.Settings.Method
		o["password"] = in.Secret.ServerPSK

	default:
		return nil, fmt.Errorf("unsupported protocol %q", in.Protocol)
	}
	o["users"] = users
	return o, nil
}

func serverTLS(in Inbound, alpn ...string) obj {
	if in.Settings.TLSMode == TLSFile {
		return obj{
			"enabled":          true,
			"server_name":      in.Settings.ServerName,
			"alpn":             alpn,
			"certificate_path": in.Settings.CertificatePath,
			"key_path":         in.Settings.KeyPath,
		}
	}
	if in.ACME() {
		return obj{
			"enabled":              true,
			"server_name":          in.Settings.ServerName,
			"alpn":                 alpn,
			"certificate_provider": ACMETag,
		}
	}
	return obj{
		"enabled":     true,
		"server_name": in.Settings.ServerName,
		"alpn":        alpn,
		"certificate": []string{in.Settings.Certificate},
		"key":         []string{in.Secret.TLSKey},
	}
}
