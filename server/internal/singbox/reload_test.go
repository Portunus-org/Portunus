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

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

// allowLoopback drops the server rule that rejects private destinations, so
// the test can proxy to a local HTTP server instead of the internet.
func allowLoopback(t *testing.T, cfg []byte) []byte {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(cfg, &v); err != nil {
		t.Fatal(err)
	}
	route := v["route"].(map[string]any)
	route["rules"] = []any{map[string]any{"action": "sniff"}}
	out, _ := json.Marshal(v)
	return out
}

// TestQUICClientsSurviveReload checks that Hysteria2 and TUIC clients recover
// from a server reload within seconds. It needs a sing-box built with the
// sing-quic stateless-reset patch (deploy/patches):
//
//	PORTUNUS_TEST_SINGBOX=/path/to/sing-box go test ./internal/singbox/ -run QUIC
func TestQUICClientsSurviveReload(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	target := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
	tln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go target.Serve(tln)
	defer target.Close()
	targetURL := "http://" + tln.Addr().String() + "/"

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	m.Env = []string{"SING_QUIC_STATELESS_RESET_KEY=" + hex.EncodeToString(make([]byte, 32))}
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	var inbounds []proxy.Inbound
	for i, p := range []proxy.Protocol{proxy.Hysteria2, proxy.TUIC} {
		set, sec, err := proxy.Provision(p, proxy.ProvisionOptions{ServerName: "localhost"})
		if err != nil {
			t.Fatal(err)
		}
		port := freeUDPPort(t)
		inbounds = append(inbounds, proxy.Inbound{ID: int64(i + 1), Tag: proxy.Tag(p, port), Protocol: p, Port: port, Enabled: true, Settings: set, Secret: sec})
	}
	client := proxy.Client{ID: 1, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}
	api := "127.0.0.1:" + strconv.Itoa(freePort(t))

	apply := func(clients []proxy.Client) {
		cfg, err := proxy.ServerConfig(inbounds, clients, api, proxy.ACMEOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Apply(ctx, allowLoopback(t, cfg)); err != nil {
			t.Fatal(err)
		}
	}
	apply([]proxy.Client{client})
	waitState(t, m, StateRunning)

	for _, in := range inbounds {
		t.Run(string(in.Protocol), func(t *testing.T) {
			socks := freePort(t)
			ob := proxy.ClientOutbound("127.0.0.1", in, client)
			cfg, _ := json.Marshal(map[string]any{
				"log":       map[string]any{"level": "error"},
				"inbounds":  []any{map[string]any{"type": "mixed", "listen": "127.0.0.1", "listen_port": socks}},
				"outbounds": []any{ob},
				"route":     map[string]any{"final": ob["tag"]},
			})
			path := filepath.Join(t.TempDir(), "client.json")
			os.WriteFile(path, cfg, 0o600)
			cmd := exec.Command(bin, "run", "-c", path)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()

			proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socks))
			hc := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
			get := func() bool {
				resp, err := hc.Get(targetURL)
				if err != nil {
					return false
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				return resp.StatusCode == 204
			}
			deadline := time.Now().Add(10 * time.Second)
			for !get() {
				if time.Now().After(deadline) {
					t.Fatal("client never connected")
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Any config change reloads sing-box and drops QUIC connections.
			start := time.Now()
			apply([]proxy.Client{client, {ID: 99, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}})
			defer apply([]proxy.Client{client})
			// Apply only signals; give sing-box time to finish reloading so
			// the next request cannot slip through the old connection.
			time.Sleep(1500 * time.Millisecond)
			for !get() {
				if time.Since(start) > 15*time.Second {
					t.Fatalf("client did not recover within 15 s after reload")
				}
				time.Sleep(100 * time.Millisecond)
			}
			elapsed := time.Since(start)
			t.Logf("working again %s after the reload was triggered", elapsed.Round(10*time.Millisecond))
			if elapsed > 5*time.Second {
				t.Fatalf("recovery took %s; stateless reset is not working", elapsed)
			}
		})
	}
}
