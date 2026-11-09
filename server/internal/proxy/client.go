package proxy

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
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

	// userinfo is the credential, already percent-encoded with
	// escapeUserinfo: url.URL would leave sub-delimiters such as + and =
	// unescaped, which breaks clients that decode userinfo as form data.
	var userinfo string

	switch in.Protocol {
	case VLESS:
		u.Scheme = "vless"
		userinfo = escapeUserinfo(c.UUID)
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
		userinfo = escapeUserinfo(c.Password)
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
		// The colon is a structural separator, kept literal as
		// url.UserPassword rendered it.
		userinfo = escapeUserinfo(c.UUID) + ":" + escapeUserinfo(c.Password)
		q.Set("congestion_control", "bbr")
		q.Set("alpn", "h3")
		q.Set("sni", in.Settings.ServerName)
		if in.pinned() {
			q.Set("allow_insecure", "1")
		}

	case Trojan:
		u.Scheme = "trojan"
		userinfo = escapeUserinfo(c.Password)
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
		userinfo = escapeUserinfo(in.Settings.Method + ":" + in.Secret.ServerPSK + ":" + c.Password)
	}
	u.RawQuery = q.Encode()
	// Render host, query and fragment with the standard library, then splice
	// in the credential right after the scheme.
	return strings.Replace(u.String(), "://", "://"+userinfo+"@", 1)
}

// escapeUserinfo percent-encodes every byte of s except the unreserved set
// (A-Za-z0-9 and -._~), so +, /, = and spaces come out as %2B, %2F, %3D and
// %20. Share-link clients decode userinfo in different ways; some treat it
// as form data where + means a space, which would turn a correct password
// into a wrong one. Only a fully percent-encoded credential survives all of
// them.
func escapeUserinfo(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
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

// URLTestURL is the probe every auto group checks: a tiny request that
// costs nothing and answers from most networks.
const URLTestURL = "https://www.gstatic.com/generate_204"

// ProfileServer is one server a client can connect to in a sing-box profile:
// where to enter, with which credential, and what it is called.
type ProfileServer struct {
	Host    string
	Inbound Inbound
	// UUID is the credential of this connection; password-only protocols use
	// the client's password instead.
	UUID  string
	Label string // shown to the user ("HEL", "SPB → HEL")
}

// ClientProfile renders a complete sing-box client configuration: a TUN
// inbound, one outbound per server, a urltest group that picks the fastest
// live connection (client-side failover), and a `proxy` selector for manual
// choice. With one server it is exactly the classic profile.
func ClientProfile(c Client, servers []ProfileServer) ([]byte, error) {
	var (
		tags      []string
		outbounds []any
		groups    []any
		used      = map[string]bool{"proxy": true, "auto": true, "direct": true, "remote": true, "local": true}
	)
	for _, s := range servers {
		in := s.Inbound
		if !in.Enabled {
			continue
		}
		ob := ClientOutbound(s.Host, in, Client{ID: c.ID, Name: c.Name, UUID: s.UUID, Password: c.Password})
		// Tags must be unique in a sing-box config, but inbound names are free
		// text: two may match, or collide with the group tags below.
		tag := ob["tag"].(string)
		for n := 2; used[tag]; n++ {
			tag = fmt.Sprintf("%s %d", ob["tag"], n)
		}
		used[tag] = true
		ob["tag"] = tag
		tags = append(tags, tag)
		outbounds = append(outbounds, ob)
	}
	if len(tags) == 0 {
		return nil, fmt.Errorf("no servers to render")
	}
	selector := tags
	defaultTag := tags[0]
	if len(tags) > 1 {
		groups = append(groups, obj{
			"type":      "urltest",
			"tag":       "auto",
			"outbounds": tags,
			"url":       URLTestURL,
			// How soon a dead connection is noticed. A check is one tiny
			// request per server.
			"interval": "30s",
			// Stay on the connection in use for as long as it answers, however
			// fast the others are: what leaves directly carries its address,
			// and that should not change without need.
			"tolerance": 5000,
			// Connections stuck on a dead server are dropped, so apps
			// reconnect through the new one at once.
			"interrupt_exist_connections": true,
		})
		selector = append([]string{"auto"}, tags...)
		defaultTag = "auto"
	}
	groups = append([]any{obj{
		"type":      "selector",
		"tag":       "proxy",
		"outbounds": selector,
		"default":   defaultTag,
	}}, groups...)
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
