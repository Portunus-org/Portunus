// Package setup inspects the host so the first-run wizard can choose sensible
// defaults on its own: the public address, which ports are free, whether a
// reverse proxy owns port 80, where a domain points, and which site suits
// REALITY best.
//
// Portunus runs with host networking, so /proc/net and the interfaces it sees
// are the host's.
package setup

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PublicAddress is how the host is reachable from the internet.
type PublicAddress struct {
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
	// Source is "interface" (a public address is configured on the host) or
	// "lookup" (the host is behind NAT; an external service reported it).
	Source string `json:"source"`
}

func (a PublicAddress) Primary() string {
	if a.IPv4 != "" {
		return a.IPv4
	}
	return a.IPv6
}

// lookupServices report the caller's address as plain text. Only used when
// no interface carries a public address (NAT), and only from the wizard.
var lookupServices = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://checkip.amazonaws.com",
}

// DetectPublicAddress prefers addresses configured on the host's interfaces
// and falls back to asking external services.
func DetectPublicAddress(ctx context.Context) (PublicAddress, error) {
	var out PublicAddress
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || !isPublic(ipnet.IP) {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			if out.IPv4 == "" {
				out.IPv4 = ip4.String()
			}
		} else if out.IPv6 == "" {
			out.IPv6 = ipnet.IP.String()
		}
	}
	if out.IPv4 != "" {
		out.Source = "interface"
		return out, nil
	}

	ip, err := lookupIPv4(ctx)
	if err != nil {
		if out.IPv6 != "" {
			out.Source = "interface"
			return out, nil
		}
		return out, fmt.Errorf("no public address on any interface and external lookup failed: %w", err)
	}
	out.IPv4, out.Source = ip, "lookup"
	return out, nil
}

func isPublic(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !isCGNAT(ip)
}

// isCGNAT reports 100.64.0.0/10 (carrier-grade NAT), which is not reachable
// from the internet either.
func isCGNAT(ip net.IP) bool {
	ip4 := ip.To4()
	return ip4 != nil && ip4[0] == 100 && ip4[1]&0xc0 == 64
}

// lookupIPv4 asks the services concurrently and takes the answer at least
// two agree on (or the only one that answered).
func lookupIPv4(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{
		// Force IPv4 so the services report the IPv4 address.
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp4", addr)
		},
	}}
	type res struct{ ip string }
	ch := make(chan res, len(lookupServices))
	for _, u := range lookupServices {
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			resp, err := client.Do(req)
			if err != nil {
				ch <- res{}
				return
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			ip := net.ParseIP(strings.TrimSpace(string(b)))
			if ip == nil || ip.To4() == nil || !isPublic(ip) {
				ch <- res{}
				return
			}
			ch <- res{ip.String()}
		}()
	}
	votes := map[string]int{}
	for range lookupServices {
		r := <-ch
		if r.ip != "" {
			votes[r.ip]++
		}
	}
	best, n := "", 0
	for ip, v := range votes {
		if v > n {
			best, n = ip, v
		}
	}
	if best == "" {
		return "", errors.New("no lookup service answered")
	}
	return best, nil
}

// ListeningPorts returns the TCP ports in LISTEN state and the bound UDP
// ports on the host, from /proc/net.
func ListeningPorts() (tcp, udp map[int]bool) {
	tcp, udp = map[int]bool{}, map[int]bool{}
	readProcNet("/proc/net/tcp", "0A", tcp)
	readProcNet("/proc/net/tcp6", "0A", tcp)
	readProcNet("/proc/net/udp", "07", udp)
	readProcNet("/proc/net/udp6", "07", udp)
	return tcp, udp
}

func readProcNet(path, state string, into map[int]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[3] != state {
			continue
		}
		_, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		b, err := hex.DecodeString(portHex)
		if err != nil || len(b) != 2 {
			continue
		}
		into[int(b[0])<<8|int(b[1])] = true
	}
}

// HTTPServerOn80 identifies what answers plain HTTP on the host's port 80:
// "" when nothing does, otherwise the Server header (e.g. "nginx",
// "openresty" for Nginx Proxy Manager, "Caddy") or "unknown".
func HTTPServerOn80(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "http://127.0.0.1:80/", nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return "unknown"
		}
		// Connection refused: nothing listens.
		if _, tcp := ListeningPorts(); tcp[80] {
			return "unknown"
		}
		return ""
	}
	resp.Body.Close()
	if s := resp.Header.Get("Server"); s != "" {
		name, _, _ := strings.Cut(s, "/")
		return name
	}
	return "unknown"
}

// DomainCheck is where a domain points and whether that is this host.
type DomainCheck struct {
	Domain     string   `json:"domain"`
	Addresses  []string `json:"addresses"`
	PointsHere bool     `json:"pointsHere"`
}

func CheckDomain(ctx context.Context, domain string, here PublicAddress) DomainCheck {
	out := DomainCheck{Domain: domain, Addresses: []string{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := LookupFresh(ctx, domain)
	if err != nil {
		return out
	}
	for _, ip := range ips {
		s := ip.IP.String()
		out.Addresses = append(out.Addresses, s)
		if s == here.IPv4 || (here.IPv6 != "" && ip.IP.Equal(net.ParseIP(here.IPv6))) {
			out.PointsHere = true
		}
	}
	sort.Strings(out.Addresses)
	return out
}

// LookupFresh resolves a name by asking the name servers of its own zone,
// not the resolver this host is set up with. That one is usually a cache at
// the hosting provider, and after a record is changed it goes on giving the
// old address for as long as it pleases, a day in one observed case, while
// the rest of the world already sees the new one. The zone's servers have
// no cache, and they are also where Let's Encrypt looks.
//
// When they cannot be asked (no outgoing DNS, a name that is an alias into
// another zone), the host's resolver is the fallback.
func LookupFresh(ctx context.Context, name string) ([]net.IPAddr, error) {
	for _, server := range zoneServers(ctx, name) {
		actx, cancel := context.WithTimeout(ctx, 2*time.Second)
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(server, "53"))
			},
		}
		ips, err := r.LookupIPAddr(actx, name)
		cancel()
		if err == nil && len(ips) > 0 {
			return ips, nil
		}
	}
	return net.DefaultResolver.LookupIPAddr(ctx, name)
}

// zoneServers returns the addresses of up to three name servers of the
// zone name belongs to: the nearest parent that has any.
func zoneServers(ctx context.Context, name string) []string {
	zone := strings.TrimSuffix(name, ".")
	for strings.Contains(zone, ".") {
		nss, err := net.DefaultResolver.LookupNS(ctx, zone)
		if err == nil && len(nss) > 0 {
			var out []string
			for _, ns := range nss {
				if addrs, err := net.DefaultResolver.LookupHost(ctx, ns.Host); err == nil && len(addrs) > 0 {
					out = append(out, addrs[0])
				}
				if len(out) == 3 {
					break
				}
			}
			return out
		}
		_, zone, _ = strings.Cut(zone, ".")
	}
	return nil
}

// VerifyChallengeForwarding checks that http://domain/.well-known/acme-challenge/
// reaches localPort on this host, i.e. that a reverse proxy on port 80
// forwards ACME HTTP-01 challenges as configured. It briefly listens on
// localPort itself (the same port sing-box's ACME solver will use).
func VerifyChallengeForwarding(ctx context.Context, domain string, localPort int) error {
	token := strconv.FormatInt(time.Now().UnixNano(), 36)
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(localPort))
	if err != nil {
		return fmt.Errorf("port %d is not free: %w", localPort, err)
	}
	srv := &http.Server{
		ReadHeaderTimeout: 3 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/acme-challenge/"+token {
				w.Header().Set("Content-Type", "text/plain")
				io.WriteString(w, token) // nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter -- our own random token, text/plain
				return
			}
			http.NotFound(w, r)
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+domain+"/.well-known/acme-challenge/"+token, nil)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return fmt.Errorf("could not reach http://%s: %w", domain, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != token {
		return fmt.Errorf("the request did not reach port %d (HTTP %d)", localPort, resp.StatusCode)
	}
	return nil
}

// RealityCandidates are well-known sites with TLS 1.3, X25519 and HTTP/2,
// commonly used as REALITY targets. They are probed from this server and the
// fastest suitable one wins.
var RealityCandidates = []string{
	"www.microsoft.com",
	"www.apple.com",
	"dl.google.com",
	"www.cloudflare.com",
	"addons.mozilla.org",
	"www.samsung.com",
	"www.nvidia.com",
	"www.amazon.com",
	"www.intel.com",
	"www.oracle.com",
}

// RealityProbe is the result of probing one candidate.
type RealityProbe struct {
	Host   string `json:"host"`
	OK     bool   `json:"ok"`
	RTTms  int    `json:"rttMs,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ProbeRealityTargets handshakes with every candidate in parallel and
// returns them best first: suitable ones by round-trip time, then the rest.
func ProbeRealityTargets(ctx context.Context, hosts []string) []RealityProbe {
	out := make([]RealityProbe, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Go(func() { out[i] = probeReality(ctx, h) })
	}
	wg.Wait()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OK != out[j].OK {
			return out[i].OK
		}
		return out[i].RTTms < out[j].RTTms
	})
	return out
}

// probeReality requires what REALITY needs from its target: TLS 1.3, the
// X25519 key exchange, HTTP/2, and a certificate valid for the name. The
// best of three handshakes counts, to smooth out jitter.
func probeReality(ctx context.Context, host string) RealityProbe {
	p := RealityProbe{Host: host}
	best := time.Duration(0)
	for range 3 {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		start := time.Now()
		d := tls.Dialer{Config: &tls.Config{
			ServerName:       host,
			MinVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{tls.X25519},
			NextProtos:       []string{"h2", "http/1.1"},
		}}
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
		cancel()
		if err != nil {
			p.Reason = shortErr(err)
			return p
		}
		st := conn.(*tls.Conn).ConnectionState()
		conn.Close()
		switch {
		case st.Version != tls.VersionTLS13:
			p.Reason = "no TLS 1.3"
			return p
		case st.NegotiatedProtocol != "h2":
			p.Reason = "no HTTP/2"
			return p
		}
		if rtt := time.Since(start); best == 0 || rtt < best {
			best = rtt
		}
	}
	p.OK, p.RTTms = true, int(best.Milliseconds())
	return p
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && i+2 < len(s) {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// FirstFree returns the first of candidates that is free in used, or 0.
func FirstFree(used map[int]bool, candidates ...int) int {
	if i := slices.IndexFunc(candidates, func(p int) bool { return !used[p] }); i >= 0 {
		return candidates[i]
	}
	return 0
}
