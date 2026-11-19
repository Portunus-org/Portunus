package singbox

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

func errorLines(logs []string) []string {
	var errs []string
	for _, line := range logs {
		if strings.Contains(line, "ERROR[") || strings.Contains(line, " ERROR ") || strings.HasPrefix(line, "ERROR ") {
			errs = append(errs, line)
		}
	}
	return errs
}

func startLocalTLS13Decoy(t *testing.T) int {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h2", "http/1.1"},
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func startClientSingBox(t *testing.T, bin string, outbound map[string]any) (socksPort int, logBuf *syncedBuffer) {
	t.Helper()
	socksPort = freePort(t)
	cfg, err := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "error"},
		"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": socksPort}},
		"outbounds": []any{outbound},
		"route":     map[string]any{"final": outbound["tag"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf syncedBuffer
	cmd := exec.Command(bin, "run", "-c", path)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return socksPort, &buf
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("client sing-box never opened SOCKS port %d; logs:\n%s", socksPort, buf.String())
	return 0, nil
}

type syncedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncedBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncedBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// socks5Connect opens a SOCKS5 TCP tunnel through socksAddr to targetAddr (host:port)
// using no authentication so the caller can cancel mid-handshake or mid-download.
func socks5Connect(socksAddr, targetHost string, targetPort int, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", socksAddr, timeout)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	// Greeting: VER=5, NMETHODS=1, NOAUTH=0.
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var sel [2]byte
	if _, err := io.ReadFull(conn, sel[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	// Request: VER=5, CONNECT=1, RSV=0, DOMAINNAME=3.
	req := make([]byte, 0, 7+len(targetHost))
	req = append(req, 0x05, 0x01, 0x00, 0x03, byte(len(targetHost)))
	req = append(req, targetHost...)
	req = append(req, byte(targetPort>>8), byte(targetPort))
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		_ = conn.Close()
		return nil, err
	}
	var rest int
	switch hdr[3] {
	case 0x01:
		rest = 4 + 2
	case 0x03:
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			_ = conn.Close()
			return nil, err
		}
		rest = int(l[0]) + 2
	case 0x04:
		rest = 16 + 2
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected atyp %d", hdr[3])
	}
	if _, err := io.ReadFull(conn, make([]byte, rest)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func TestPatchedHysteria2RemoteStreamCancel(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Target 1: fast stream download that stays open until server sees write error.
	streamLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = streamLn.Close() })
	go func() {
		payload := bytes.Repeat([]byte("A"), 32*1024)
		for {
			c, err := streamLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				for i := 0; i < 64; i++ {
					if _, err := conn.Write(payload); err != nil {
						return
					}
					time.Sleep(2 * time.Millisecond)
				}
			}(c)
		}
	}()

	// Target 2: waits before replying so client cancels during/right around Hysteria2 handshake response.
	slowLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slowLn.Close() })
	go func() {
		for {
			c, err := slowLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				time.Sleep(150 * time.Millisecond)
				_, _ = conn.Write([]byte("OK\n"))
			}(c)
		}
	}()

	set, sec, err := proxy.Provision(proxy.Hysteria2, proxy.ProvisionOptions{ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	hyPort := freeUDPPort(t)
	in := proxy.Inbound{ID: 1, Tag: proxy.Tag(proxy.Hysteria2, hyPort), Protocol: proxy.Hysteria2, Port: hyPort, Enabled: true, Settings: set, Secret: sec}
	client := proxy.Client{ID: 1, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}
	api := "127.0.0.1:" + strconv.Itoa(freePort(t))

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	m.Env = []string{"SING_QUIC_STATELESS_RESET_KEY=" + hex.EncodeToString(make([]byte, 32))}
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	cfg, err := proxy.ServerConfig([]proxy.Inbound{in}, []proxy.Client{client}, api, proxy.ACMEOptions{}, proxy.Routing{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, allowLoopback(t, cfg)); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)

	socksPort, _ := startClientSingBox(t, bin, proxy.ClientOutbound("127.0.0.1", in, client))
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)

	streamPort := streamLn.Addr().(*net.TCPAddr).Port
	slowPort := slowLn.Addr().(*net.TCPAddr).Port

	// Warm up QUIC connection so subsequent cancels hit active Hysteria2 streams.
	for attempt := 0; attempt < 20; attempt++ {
		c, err := socks5Connect(socksAddr, "127.0.0.1", streamPort, 2*time.Second)
		if err == nil {
			buf := make([]byte, 16)
			_, _ = io.ReadFull(c, buf)
			_ = c.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			c, err := socks5Connect(socksAddr, "127.0.0.1", streamPort, 3*time.Second)
			if err != nil {
				return
			}
			// Trigger mid-download cancel once the server starts streaming payload.
			buf := make([]byte, 128)
			_, _ = c.Read(buf)
			_ = c.Close()
		}(i)
		go func(idx int) {
			defer wg.Done()
			c, err := socks5Connect(socksAddr, "127.0.0.1", slowPort, 3*time.Second)
			if err != nil {
				return
			}
			// Trigger close before server finishes slow response / around stream setup.
			_, _ = c.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
			time.Sleep(time.Duration(idx%10) * time.Millisecond)
			_ = c.Close()
		}(i)
	}
	wg.Wait()
	time.Sleep(300 * time.Millisecond)

	if errs := errorLines(m.Logs()); len(errs) > 0 {
		t.Fatalf("unexpected ERROR logs on Hysteria2 remote stream cancel:\n%s", strings.Join(errs, "\n"))
	}
}

func TestPatchedDialCanceledVsRefused(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Listener that accepts TCP and never responds so VLESS+REALITY dial blocks in handshake.
	stallLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stallLn.Close() })
	accepted := make(chan struct{}, 16)
	go func() {
		for {
			c, err := stallLn.Accept()
			if err != nil {
				return
			}
			select {
			case accepted <- struct{}{}:
			default:
			}
			go func(conn net.Conn) {
				defer conn.Close()
				time.Sleep(5 * time.Second)
			}(c)
		}
	}()
	stallPort := stallLn.Addr().(*net.TCPAddr).Port

	// Closed port that immediately fails with connection refused.
	refusePort := freePort(t)

	decoyPort := startLocalTLS13Decoy(t)
	link, err := proxy.NewChainLink(stallPort, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	_ = decoyPort

	inSet, inSec, err := proxy.Provision(proxy.Hysteria2, proxy.ProvisionOptions{ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	inPort := freeUDPPort(t)
	in := proxy.Inbound{ID: 1, Tag: proxy.Tag(proxy.Hysteria2, inPort), Protocol: proxy.Hysteria2, Port: inPort, Enabled: true, Settings: inSet, Secret: inSec}
	client := proxy.Client{ID: 1, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}

	// Build outbound specs pointing to stalling VLESS server (clink-1) and refused port (clink-2).
	clinkStall := link.Outbound("127.0.0.1")
	clinkStall["tag"] = "clink-1"
	clinkRefuse := map[string]any{
		"type":        "vless",
		"tag":         "clink-2",
		"server":      "127.0.0.1",
		"server_port": refusePort,
		"uuid":        link.UUID,
	}

	stallTargetPort := freePort(t)
	refuseTargetPort := freePort(t)

	cfg, err := json.Marshal(map[string]any{
		"log":       map[string]any{"level": "info"},
		"inbounds":  []any{mustInboundSpec(t, in, []proxy.Client{client})},
		"outbounds": []any{clinkStall, clinkRefuse, map[string]any{"type": "direct", "tag": "direct"}},
		"route": map[string]any{
			"rules": []any{
				map[string]any{"port": []int{stallTargetPort}, "outbound": "clink-1"},
				map[string]any{"port": []int{refuseTargetPort}, "outbound": "clink-2"},
			},
			"final": "direct",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	m.Env = []string{"SING_QUIC_STATELESS_RESET_KEY=" + hex.EncodeToString(make([]byte, 32))}
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	if err := m.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)

	socksPort, _ := startClientSingBox(t, bin, proxy.ClientOutbound("127.0.0.1", in, client))
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)

	// Open connections to stalling clink-1 until clink-1 actually connects,
	// cancel the client stream and reload sing-box mid-handshake to force context.Canceled.
	for i := 0; i < 10; i++ {
		c, err := socks5Connect(socksAddr, "127.0.0.1", stallTargetPort, 2*time.Second)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_, _ = c.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
		select {
		case <-accepted:
			_ = c.Close()
			i = 10
		case <-time.After(100 * time.Millisecond):
			_ = c.Close()
		}
	}
	// Reload in place while clink-1 is stalled in handshake so sing-box cancels the in-flight dial context.
	var rawCfg map[string]any
	_ = json.Unmarshal(cfg, &rawCfg)
	rawCfg["outbounds"] = []any{clinkStall, clinkRefuse, map[string]any{"type": "direct", "tag": "direct", "connect_timeout": "6s"}}
	reloadBytes, _ := json.Marshal(rawCfg)
	if err := m.Apply(ctx, reloadBytes); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	for _, line := range errorLines(m.Logs()) {
		if strings.Contains(line, "operation was canceled") || strings.Contains(line, "canceled") {
			t.Fatalf("canceled dial should not log ERROR, got: %s", line)
		}
	}

	// Verify a real dial error (connection refused on clink-2) STILL logs at ERROR
	// once the QUIC client finishes reconnecting after the in-place reload.
	deadline := time.Now().Add(6 * time.Second)
	sawRefused := false
	for time.Now().Before(deadline) && !sawRefused {
		if c, err := socks5Connect(socksAddr, "127.0.0.1", refuseTargetPort, 2*time.Second); err == nil {
			_, _ = c.Write([]byte("GET / HTTP/1.0\r\n\r\n"))
			_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
			_, _ = c.Read(make([]byte, 16))
			_ = c.Close()
		}
		for _, line := range errorLines(m.Logs()) {
			if strings.Contains(line, "connection refused") {
				sawRefused = true
				break
			}
		}
		if !sawRefused {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if !sawRefused {
		t.Fatalf("expected connection refused ERROR log to be preserved; logs:\n%s", strings.Join(m.Logs(), "\n"))
	}
}

func TestPatchedVLESSVisionTLS13LargeDownloadAndClose(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Inner TLS 1.3 HTTPS target server so VLESS Vision activates direct splice across >8171 byte TLS 1.3 records.
	payload := bytes.Repeat([]byte("VisionTLS13Payload-0123456789abcdef\n"), 8000) // ~288 KiB
	target := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	target.Config.ErrorLog = log.New(io.Discard, "", 0)
	target.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	target.StartTLS()
	t.Cleanup(target.Close)

	decoyPort := startLocalTLS13Decoy(t)
	link, err := proxy.NewChainLink(freePort(t), "localhost")
	if err != nil {
		t.Fatal(err)
	}
	link.ID = 1

	vlessIn := proxy.Inbound{
		ID:          1,
		Tag:         proxy.Tag(proxy.VLESS, link.Port),
		Protocol:    proxy.VLESS,
		Port:        link.Port,
		Enabled:     true,
		Settings:    link.Settings,
		Secret:      link.Secret,
		RealityDial: fmt.Sprintf("127.0.0.1:%d", decoyPort),
	}
	vlessClient := proxy.Client{ID: 1, UUID: link.UUID}
	cfg, err := proxy.ServerConfig([]proxy.Inbound{vlessIn}, []proxy.Client{vlessClient}, "", proxy.ACMEOptions{}, proxy.Routing{})
	if err != nil {
		t.Fatal(err)
	}
	cfg = allowLoopback(t, cfg)

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	if err := m.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)

	clientOut := link.Outbound("127.0.0.1")
	clientOut["tag"] = "vless-vision-out"
	socksPort, clientLog := startClientSingBox(t, bin, clientOut)
	proxyURL, err := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatal(err)
	}

	hc := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxyURL),
			DisableKeepAlives:   true,
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS13},
			MaxIdleConnsPerHost: 1,
		},
	}

	// Run enough independent Vision TLS 1.3 connections to span large spliced reads/writes + clean closes.
	for i := 0; i < 40; i++ {
		resp, err := hc.Get(target.URL)
		if err != nil {
			t.Fatalf("conn %d failed: %v\nserver logs:\n%s\nclient logs:\n%s",
				i, err, strings.Join(m.Logs(), "\n"), clientLog.String())
		}
		if i%5 == 0 {
			// Early close mid-download to exercise VisionConn.Close() / CloseWrite() after splice.
			_, _ = io.CopyN(io.Discard, resp.Body, 16*1024)
			_ = resp.Body.Close()
			continue
		}
		got, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("conn %d read failed: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("conn %d payload mismatch: got %d want %d bytes", i, len(got), len(payload))
		}
	}

	if errs := errorLines(m.Logs()); len(errs) > 0 {
		t.Fatalf("unexpected ERROR logs on VLESS+Vision server:\n%s", strings.Join(errs, "\n"))
	}
	if strings.Contains(clientLog.String(), "ERROR") {
		t.Fatalf("unexpected ERROR logs on VLESS+Vision client:\n%s", clientLog.String())
	}
}

func mustInboundSpec(t *testing.T, in proxy.Inbound, clients []proxy.Client) any {
	t.Helper()
	cfg, err := proxy.ServerConfig([]proxy.Inbound{in}, clients, "", proxy.ACMEOptions{}, proxy.Routing{})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Inbounds []any `json:"inbounds"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed.Inbounds[0]
}
