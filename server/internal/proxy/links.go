package proxy

import (
	"encoding/base64"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrUnsupportedLink is returned for share links Portunus cannot turn into
// an outbound.
var ErrUnsupportedLink = errors.New("unsupported link")

// ImportedOutbound is a third-party proxy read from a share link.
type ImportedOutbound struct {
	// Name is the label after # in the link, if any.
	Name    string
	Address string
	Port    int
	// Protocol is the sing-box outbound type.
	Protocol string
	// Outbound is the complete sing-box outbound except for its tag.
	Outbound map[string]any
}

// ParseShareLink turns a vless://, trojan://, hysteria2://, tuic:// or
// ss:// link into a sing-box outbound.
func ParseShareLink(link string) (*ImportedOutbound, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.Host == "" {
		return nil, ErrUnsupportedLink
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || host == "" {
		return nil, ErrUnsupportedLink
	}
	q := u.Query()
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := q.Get(k); v != "" {
				return v
			}
		}
		return ""
	}
	insecure := func() bool {
		v := first("insecure", "allowInsecure", "allow_insecure")
		return v == "1" || v == "true"
	}
	tls := func() obj {
		t := obj{"enabled": true}
		if sni := first("sni", "peer"); sni != "" {
			t["server_name"] = sni
		} else if net.ParseIP(host) == nil {
			t["server_name"] = host
		}
		if insecure() {
			t["insecure"] = true
		}
		if alpn := q.Get("alpn"); alpn != "" {
			t["alpn"] = strings.Split(alpn, ",")
		}
		return t
	}
	// transport reads v2ray-style transports; ok is false for ones sing-box
	// does not have.
	transport := func() (obj, bool) {
		switch first("type", "net") {
		case "", "tcp", "raw":
			return nil, true
		case "ws":
			t := obj{"type": "ws", "path": q.Get("path")}
			if h := q.Get("host"); h != "" {
				t["headers"] = obj{"Host": h}
			}
			return t, true
		case "grpc":
			return obj{"type": "grpc", "service_name": first("serviceName", "service_name")}, true
		case "httpupgrade":
			t := obj{"type": "httpupgrade", "path": q.Get("path")}
			if h := q.Get("host"); h != "" {
				t["host"] = h
			}
			return t, true
		}
		return nil, false
	}

	out := &ImportedOutbound{Name: u.Fragment, Address: host, Port: port}
	o := obj{"server": host, "server_port": port}

	switch strings.ToLower(u.Scheme) {
	case "vless":
		if u.User.Username() == "" {
			return nil, ErrUnsupportedLink
		}
		o["type"], o["uuid"] = "vless", u.User.Username()
		if flow := q.Get("flow"); flow != "" {
			o["flow"] = flow
		}
		o["packet_encoding"] = "xudp"
		switch q.Get("security") {
		case "reality":
			t := tls()
			t["utls"] = obj{"enabled": true, "fingerprint": orDefault(q.Get("fp"), "chrome")}
			if q.Get("pbk") == "" {
				return nil, ErrUnsupportedLink
			}
			t["reality"] = obj{"enabled": true, "public_key": q.Get("pbk"), "short_id": q.Get("sid")}
			o["tls"] = t
		case "tls":
			t := tls()
			if fp := q.Get("fp"); fp != "" {
				t["utls"] = obj{"enabled": true, "fingerprint": fp}
			}
			o["tls"] = t
		case "", "none":
		default:
			return nil, ErrUnsupportedLink
		}
		tr, ok := transport()
		if !ok {
			return nil, ErrUnsupportedLink
		}
		if tr != nil {
			o["transport"] = tr
		}

	case "trojan":
		pw, _ := url.PathUnescape(u.User.Username())
		if pw == "" {
			return nil, ErrUnsupportedLink
		}
		o["type"], o["password"] = "trojan", pw
		if q.Get("security") != "none" {
			o["tls"] = tls()
		}
		tr, ok := transport()
		if !ok {
			return nil, ErrUnsupportedLink
		}
		if tr != nil {
			o["transport"] = tr
		}

	case "hysteria2", "hy2":
		pw := u.User.String()
		if p, err := url.PathUnescape(pw); err == nil {
			pw = p
		}
		if pw == "" {
			return nil, ErrUnsupportedLink
		}
		o["type"], o["password"] = "hysteria2", pw
		t := tls()
		// A pinned certificate cannot be expressed without the certificate
		// itself; links that pin always also say insecure=1.
		o["tls"] = t
		if q.Get("obfs") == "salamander" {
			o["obfs"] = obj{"type": "salamander", "password": q.Get("obfs-password")}
		}

	case "tuic":
		pw, _ := u.User.Password()
		if u.User.Username() == "" {
			return nil, ErrUnsupportedLink
		}
		o["type"], o["uuid"], o["password"] = "tuic", u.User.Username(), pw
		if cc := first("congestion_control", "congestion-control"); cc != "" {
			o["congestion_control"] = cc
		}
		t := tls()
		if _, ok := t["alpn"]; !ok {
			t["alpn"] = []string{"h3"}
		}
		o["tls"] = t

	case "ss":
		method, password, ok := ssUserInfo(u)
		if !ok {
			return nil, ErrUnsupportedLink
		}
		if q.Get("plugin") != "" {
			return nil, ErrUnsupportedLink
		}
		o["type"], o["method"], o["password"] = "shadowsocks", method, password

	default:
		return nil, ErrUnsupportedLink
	}
	out.Protocol = o["type"].(string)
	out.Outbound = o
	return out, nil
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// ssUserInfo reads SIP002 user info: either plain "method:password"
// (percent-encoded, as 2022 ciphers require) or base64 of it.
func ssUserInfo(u *url.URL) (method, password string, ok bool) {
	if pw, has := u.User.Password(); has {
		return u.User.Username(), pw, u.User.Username() != "" && pw != ""
	}
	raw := u.User.Username()
	// Percent-encoded "method:password" arrives as one user name.
	if m, p, found := strings.Cut(raw, ":"); found && m != "" && p != "" {
		return m, p, true
	}
	for _, enc := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := enc.DecodeString(raw); err == nil {
			if m, p, found := strings.Cut(string(b), ":"); found && m != "" && p != "" {
				return m, p, true
			}
		}
	}
	return "", "", false
}
