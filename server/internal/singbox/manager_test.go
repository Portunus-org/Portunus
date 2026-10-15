package singbox

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitState(t *testing.T, m *Manager, want State) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status().State == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("state %q, want %q; logs:\n%v", m.Status().State, want, m.Logs())
}

// TestManagerLifecycle needs a sing-box binary built with with_v2ray_api:
// PORTUNUS_TEST_SINGBOX=/path/to/sing-box go test ./internal/singbox/
func TestManagerLifecycle(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	flushed := 0
	m.BeforeReload = func(context.Context) { flushed++ }
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()

	set, sec, err := proxy.Provision(proxy.VLESS, proxy.ProvisionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	in := proxy.Inbound{ID: 1, Tag: proxy.Tag(proxy.VLESS, port), Protocol: proxy.VLESS, Port: port, Enabled: true, Settings: set, Secret: sec}
	api := "127.0.0.1:" + strconv.Itoa(freePort(t))
	clients := []proxy.Client{{ID: 1, UUID: proxy.NewUUID(), Password: proxy.NewPassword()}}

	cfg, err := proxy.ServerConfig([]proxy.Inbound{in}, clients, api, proxy.ACMEOptions{}, proxy.Routing{})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)
	pid := m.Status().PID

	stats := NewStatsClient(api)
	if _, err := stats.QueryUsers(ctx); err != nil {
		t.Fatalf("stats query: %v", err)
	}

	// Same config: no-op.
	if err := m.Apply(ctx, cfg); err != nil || flushed != 0 {
		t.Fatalf("re-apply of identical config: err=%v flushed=%d", err, flushed)
	}

	// Changed config: reload in place (same process, counters flushed first).
	clients = append(clients, proxy.Client{ID: 2, UUID: proxy.NewUUID(), Password: proxy.NewPassword()})
	cfg2, _ := proxy.ServerConfig([]proxy.Inbound{in}, clients, api, proxy.ACMEOptions{}, proxy.Routing{})
	if err := m.Apply(ctx, cfg2); err != nil {
		t.Fatal(err)
	}
	if flushed != 1 || m.Status().PID != pid {
		t.Fatalf("reload: flushed=%d pid %d -> %d", flushed, pid, m.Status().PID)
	}

	// Invalid config is rejected and the running one kept.
	if err := m.Apply(ctx, []byte(`{"inbounds":[{"type":"nope"}]}`)); err == nil {
		t.Fatal("invalid config accepted")
	}
	if string(m.Config()) != string(cfg2) {
		t.Fatal("running config replaced by an invalid one")
	}

	// A requested restart is not a failure.
	if err := m.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)
	if st := m.Status(); st.Restarts != 0 || st.PID == pid {
		t.Fatalf("after Restart: restarts=%d pid=%d (was %d)", st.Restarts, st.PID, pid)
	}

	// nil config stops sing-box.
	if err := m.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateStopped)

	cancel()
	<-done
}
