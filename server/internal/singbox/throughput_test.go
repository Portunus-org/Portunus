package singbox

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/PaulichP/Portunus/server/internal/proxy"
)

// TestThroughput measures single-connection download speed through each
// data-plane protocol over loopback, i.e. what sing-box costs in CPU on this
// machine (server and client share it, so a real server does better). Opt-in:
//
//	PORTUNUS_TEST_SINGBOX=/path/to/sing-box PORTUNUS_TEST_THROUGHPUT=1 \
//	  go test ./internal/singbox/ -run Throughput -v
func TestThroughput(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" || os.Getenv("PORTUNUS_TEST_THROUGHPUT") == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX and PORTUNUS_TEST_THROUGHPUT not set")
	}
	const size = 1 << 30 // 1 GiB per protocol
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chunk := make([]byte, 1<<20)
	src := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		for sent := 0; sent < size; sent += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go src.Serve(ln)
	defer src.Close()

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	m.Env = []string{"SING_QUIC_STATELESS_RESET_KEY=" + hex.EncodeToString(make([]byte, 32))}
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	var inbounds []proxy.Inbound
	for i, p := range []proxy.Protocol{proxy.VLESS, proxy.Hysteria2, proxy.Shadowsocks} {
		set, sec, err := proxy.Provision(p, proxy.ProvisionOptions{ServerName: "localhost"})
		if err != nil {
			t.Fatal(err)
		}
		port := freePort(t)
		inbounds = append(inbounds, proxy.Inbound{ID: int64(i + 1), Tag: proxy.Tag(p, port), Protocol: p, Port: port, Enabled: true, Settings: set, Secret: sec})
	}
	client := proxy.Client{ID: 1, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}
	cfg, err := proxy.ServerConfig(inbounds, []proxy.Client{client}, "127.0.0.1:"+strconv.Itoa(freePort(t)), proxy.ACMEOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, allowLoopback(t, cfg)); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)

	// Emulate a 1 vCPU server: pin every sing-box server thread to one core
	// (the client stays unpinned). PORTUNUS_TEST_SERVER_CPU=2 pins to CPU 2.
	if cpu := os.Getenv("PORTUNUS_TEST_SERVER_CPU"); cpu != "" {
		if out, err := exec.Command("taskset", "-a", "-pc", cpu, strconv.Itoa(m.Status().PID)).CombinedOutput(); err != nil {
			t.Fatalf("taskset: %v %s", err, out)
		}
		t.Logf("server pinned to CPU %s", cpu)
	}

	// VLESS+REALITY borrows the handshake of its default target site, so this
	// needs internet access for the handshake; the payload stays on loopback.
	for _, in := range inbounds {
		t.Run(string(in.Protocol), func(t *testing.T) {
			socks := freePort(t)
			ob := proxy.ClientOutbound("127.0.0.1", in, client)
			ccfg, _ := json.Marshal(map[string]any{
				"log":       map[string]any{"level": "error"},
				"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": socks}},
				"outbounds": []any{ob},
				"route":     map[string]any{"final": ob["tag"]},
			})
			path := filepath.Join(t.TempDir(), "client.json")
			os.WriteFile(path, ccfg, 0o600)
			cmd := exec.Command(bin, "run", "-c", path)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			time.Sleep(time.Second)

			proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
			hc := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
			start := time.Now()
			resp, err := hc.Get("http://" + ln.Addr().String() + "/")
			if err != nil {
				t.Fatal(err)
			}
			n, err := io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if err != nil || n != size {
				t.Fatalf("read %d of %d bytes: %v", n, size, err)
			}
			d := time.Since(start)
			t.Logf("%-12s %6.0f Mbit/s (1 GiB in %s, one connection)", in.Protocol, float64(size)*8/d.Seconds()/1e6, d.Round(10*time.Millisecond))
		})
	}
}
