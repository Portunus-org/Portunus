package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
)

// pinned reports whether share links must skip normal verification and pin
// the certificate instead: a private certificate, unless the operator chose
// strict links for devices that have the CA installed.
func (in Inbound) pinned() bool { return !in.TrustedCert() && !in.Settings.StrictTLS }

// ShareLink returns the URI a client app imports for one inbound
// (vless://, hysteria2://, trojan://, tuic://, ss://). host is the public
// address of this node.
func ShareLink(host string, in Inbound, c Client) string {
	addr := net.JoinHostPort(host, strconv.Itoa(in.Port))
	q := url.Values{}
	u := url.URL{Host: addr, Fragment: in.Label()}

	switch in.Protocol {
	case VLESS:
		u.Scheme = "vless"
		u.User = url.User(c.UUID)
		q.Set("type", "tcp")
		q.Set("encryption", "none")
		q.Set("flow", vlessFlow)
		q.Set("security", "reality")
		q.Set("sni", in.Settings.RealityServer)
		q.Set("fp", "chrome")
		q.Set("pbk", in.Settings.RealityPublicKey)
		q.Set("sid", in.Settings.RealityShortID)

	case Hysteria2:
		u.Scheme = "hysteria2"
		u.User = url.User(c.Password)
		q.Set("sni", in.Settings.ServerName)
		if in.pinned() {
			q.Set("insecure", "1")
			q.Set("pinSHA256", in.Settings.CertSHA256)
		}
		if in.Settings.ObfsPassword != "" {
			q.Set("obfs", "salamander")
			q.Set("obfs-password", in.Settings.ObfsPassword)
		}

	case TUIC:
		u.Scheme = "tuic"
		u.User = url.UserPassword(c.UUID, c.Password)
		q.Set("congestion_control", "bbr")
		q.Set("alpn", "h3")
		q.Set("sni", in.Settings.ServerName)
		if in.pinned() {
			q.Set("allow_insecure", "1")
		}

	case Trojan:
		u.Scheme = "trojan"
		u.User = url.User(c.Password)
		q.Set("type", "tcp")
		q.Set("security", "tls")
		q.Set("sni", in.Settings.ServerName)
		if in.pinned() {
			q.Set("allowInsecure", "1")
		}

	case Shadowsocks:
		// SIP002 with a plain (percent-encoded) userinfo, as required for
		// 2022 ciphers: method:serverPSK:userPSK.
		u.Scheme = "ss"
		u.User = url.User(in.Settings.Method + ":" + in.Secret.ServerPSK + ":" + c.Password)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ClientOutbound is the sing-box outbound a client uses for one inbound.
// Unlike share links, TLS certificates are pinned rather than skipped.
func ClientOutbound(host string, in Inbound, c Client) obj {
	o := obj{
		"type":        string(in.Protocol),
		"tag":         in.Label(),
		"server":      host,
		"server_port": in.Port,
	}
	pinned := func(alpn ...string) obj {
		t := obj{
			"enabled":     true,
			"server_name": in.Settings.ServerName,
		}
		// A publicly trusted certificate is verified normally; a private one
		// against its CA (so renewals need no re-import), or pinned when it
		// predates the CA.
		if !in.TrustedCert() {
			trust := in.Settings.CACertificate
			if trust == "" {
				trust = in.Settings.Certificate
			}
			t["certificate"] = []string{trust}
		}
		if len(alpn) > 0 {
			t["alpn"] = alpn
		}
		return t
	}

	switch in.Protocol {
	case VLESS:
		o["uuid"] = c.UUID
		o["flow"] = vlessFlow
		o["tls"] = obj{
			"enabled":     true,
			"server_name": in.Settings.RealityServer,
			"utls":        obj{"enabled": true, "fingerprint": "chrome"},
			"reality": obj{
				"enabled":    true,
				"public_key": in.Settings.RealityPublicKey,
				"short_id":   in.Settings.RealityShortID,
			},
		}
	case Hysteria2:
		o["password"] = c.Password
		o["tls"] = pinned("h3")
		if in.Settings.ObfsPassword != "" {
			o["obfs"] = obj{"type": "salamander", "password": in.Settings.ObfsPassword}
		}
	case TUIC:
		o["uuid"] = c.UUID
		o["password"] = c.Password
		o["congestion_control"] = "bbr"
		o["tls"] = pinned("h3")
	case Trojan:
		o["password"] = c.Password
		o["tls"] = pinned("h2", "http/1.1")
	case Shadowsocks:
		o["method"] = in.Settings.Method
		o["password"] = in.Secret.ServerPSK + ":" + c.Password
	}
	return o
}

const urlTestURL = "https://www.gstatic.com/generate_204"

// ClientProfile renders a complete sing-box client configuration: a TUN
// inbound, one outbound per server inbound, an `auto` urltest group that
// picks the fastest live one (client-side failover), and a `proxy` selector
// for manual choice.
func ClientProfile(host string, inbounds []Inbound, c Client) ([]byte, error) {
	var (
		tags      []string
		outbounds []any
		used      = map[string]bool{"proxy": true, "auto": true, "direct": true, "remote": true, "local": true}
	)
	for _, in := range inbounds {
		if !in.Enabled {
			continue
		}
		ob := ClientOutbound(host, in, c)
		// Tags must be unique in a sing-box config, but inbound names are
		// free text: two may match, or collide with the group tags below.
		tag := ob["tag"].(string)
		for n := 2; used[tag]; n++ {
			tag = fmt.Sprintf("%s %d", ob["tag"], n)
		}
		used[tag] = true
		ob["tag"] = tag
		tags = append(tags, tag)
		outbounds = append(outbounds, ob)
	}

	groups := []any{
		obj{
			"type":      "selector",
			"tag":       "proxy",
			"outbounds": append([]string{"auto"}, tags...),
			"default":   "auto",
		},
		obj{
			"type":      "urltest",
			"tag":       "auto",
			"outbounds": tags,
			"url":       urlTestURL,
			"interval":  "3m",
		},
	}
	outbounds = append(groups, outbounds...)
	outbounds = append(outbounds, obj{"type": "direct", "tag": "direct"})

	cfg := obj{
		"log": obj{"level": "warn"},
		"dns": obj{
			"servers": []any{
				obj{"type": "https", "tag": "remote", "server": "1.1.1.1", "detour": "proxy"},
				obj{"type": "local", "tag": "local"},
			},
			"final": "remote",
		},
		"inbounds": []any{
			obj{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
				"auto_route":   true,
				"strict_route": true,
			},
		},
		"outbounds": outbounds,
		"route": obj{
			"rules": []any{
				obj{"action": "sniff"},
				obj{"protocol": "dns", "action": "hijack-dns"},
				obj{"ip_is_private": true, "outbound": "direct"},
			},
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "local",
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}
