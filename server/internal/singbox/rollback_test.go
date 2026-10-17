package singbox

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Portunus-org/Portunus/server/internal/proxy"
)

// A configuration that passes `sing-box check` but cannot start (its port
// belongs to another program) must not take the proxy down: the previous
// configuration comes back, and the bad one is refused with the reason.
func TestManagerRollsBack(t *testing.T) {
	bin := os.Getenv("PORTUNUS_TEST_SINGBOX")
	if bin == "" {
		t.Skip("PORTUNUS_TEST_SINGBOX not set")
	}
	old := settleTime
	settleTime = 1500 * time.Millisecond
	defer func() { settleTime = old }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := New(bin, t.TempDir(), slog.New(slog.DiscardHandler))
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	var restored []byte
	m.OnRollback = func(cfg []byte) { restored = cfg }
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	config := func(port int) []byte {
		set, sec, err := proxy.Provision(proxy.VLESS, proxy.ProvisionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		in := proxy.Inbound{ID: 1, Tag: "vless", Protocol: proxy.VLESS, Port: port, Enabled: true, Settings: set, Secret: sec}
		cfg, err := proxy.ServerConfig([]proxy.Inbound{in}, nil, "", proxy.ACMEOptions{}, proxy.Routing{})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	good := config(freePort(t))
	if err := m.Apply(ctx, good); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, StateRunning)
	time.Sleep(settleTime + 300*time.Millisecond)

	// Another program holds this port.
	busy, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	bad := config(busy.Addr().(*net.TCPAddr).Port)
	if err := m.Apply(ctx, bad); err != nil {
		t.Fatalf("the check cannot know the port is taken: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for m.RejectedError() == "" && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if m.RejectedError() == "" {
		t.Fatalf("no rollback; status %+v\n%v", m.Status(), m.Logs())
	}
	waitState(t, m, StateRunning)
	if !bytes.Equal(m.Config(), good) || !bytes.Equal(restored, good) {
		t.Fatal("the previous configuration was not restored")
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(portOf(t, good)), 2*time.Second); err != nil {
		t.Fatalf("the restored inbound does not listen: %v", err)
	} else {
		c.Close()
	}
	// The same configuration again is refused with the reason, not retried.
	if err := m.Apply(ctx, bad); err == nil {
		t.Fatal("the rejected configuration was accepted again")
	} else {
		t.Log(err)
	}
	if m.Status().Restarts != 0 {
		t.Fatalf("a rollback is not a crash loop: %d restarts", m.Status().Restarts)
	}
	// A different, working configuration clears the note.
	if err := m.Apply(ctx, config(freePort(t))); err != nil || m.RejectedError() != "" {
		t.Fatalf("next configuration: %v %q", err, m.RejectedError())
	}
}

func portOf(t *testing.T, cfg []byte) int {
	t.Helper()
	i := bytes.Index(cfg, []byte(`"listen_port": `))
	if i < 0 {
		t.Fatal("no listen_port in config")
	}
	rest := cfg[i+len(`"listen_port": `):]
	n, err := strconv.Atoi(string(rest[:bytes.IndexAny(rest, ",\n")]))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
