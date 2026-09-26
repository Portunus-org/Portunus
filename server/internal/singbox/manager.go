// Package singbox supervises the sing-box data plane: it validates and
// writes configs, runs sing-box as a child process, reloads it on change,
// restarts it when it crashes, and reads per-user traffic counters.
package singbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type State string

const (
	StateStopped  State = "stopped"  // no config to run (no enabled inbounds)
	StateStarting State = "starting" // process launched, not yet confirmed alive
	StateRunning  State = "running"
	StateFailed   State = "failed" // exited unexpectedly; restarting with backoff
)

type Status struct {
	State     State     `json:"state"`
	Version   string    `json:"version"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
	Restarts  int       `json:"restarts"`
	LastError string    `json:"lastError,omitempty"`
	// ConfigAppliedAt is when the current config was last written and loaded.
	ConfigAppliedAt time.Time `json:"configAppliedAt,omitzero"`
}

type Manager struct {
	bin string
	dir string
	log *slog.Logger

	// Env is added to sing-box's environment.
	Env []string

	// BeforeReload runs right before sing-box is reloaded or stopped, while
	// the old instance is still up — used to flush traffic counters that the
	// restart would otherwise lose.
	BeforeReload func(ctx context.Context)

	mu      sync.Mutex
	status  Status
	current []byte // config currently written to disk
	cmd     *exec.Cmd
	exited  chan struct{} // closed when cmd exits
	wantRun bool
	// restarting marks an exit requested by Restart, so Run does not count
	// it as a failure.
	restarting bool
	logs       *ring
	kick       chan struct{}
}

func New(bin, dir string, log *slog.Logger) *Manager {
	return &Manager{
		bin:    bin,
		dir:    dir,
		log:    log.With("component", "sing-box"),
		status: Status{State: StateStopped},
		logs:   newRing(300),
		kick:   make(chan struct{}, 1),
	}
}

func (m *Manager) configPath() string { return filepath.Join(m.dir, "config.json") }

// Init records the sing-box version and removes a stale config from a
// previous run, so nothing starts before the first Apply.
func (m *Manager) Init(ctx context.Context) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	// Resolve the binary once (PORTUNUS_SINGBOX_BIN or PATH): from here on
	// only this absolute path is executed.
	bin, err := exec.LookPath(m.bin)
	if err != nil {
		return fmt.Errorf("find %s: %w (set PORTUNUS_SINGBOX_BIN)", m.bin, err)
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return err
	}
	m.bin = bin
	out, err := exec.CommandContext(ctx, m.bin, "version").Output() // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- m.bin is the operator-configured binary resolved to an absolute path in Init; arguments are fixed
	if err != nil {
		return fmt.Errorf("run %s version: %w (set PORTUNUS_SINGBOX_BIN)", m.bin, err)
	}
	line, _, _ := strings.Cut(string(out), "\n")
	m.mu.Lock()
	m.status.Version = strings.TrimPrefix(strings.TrimSpace(line), "sing-box version ")
	m.mu.Unlock()
	os.Remove(m.configPath())
	return nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Logs returns the most recent sing-box output lines (never nil).
func (m *Manager) Logs() []string {
	if l := m.logs.lines(); l != nil {
		return l
	}
	return []string{}
}

// Config returns the config currently loaded (nil when stopped).
func (m *Manager) Config() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Apply validates cfg with `sing-box check` and, if it differs from the
// running config, installs it: starting sing-box, reloading it (SIGHUP) or
// stopping it when cfg is nil. An invalid config is rejected and the running
// one is kept.
func (m *Manager) Apply(ctx context.Context, cfg []byte) error {
	m.mu.Lock()
	same := bytes.Equal(cfg, m.current) && (cfg == nil || m.wantRun)
	m.mu.Unlock()
	if same {
		return nil
	}

	if cfg == nil {
		m.stop(ctx)
		return nil
	}

	next := filepath.Join(m.dir, "config.next.json")
	if err := os.WriteFile(next, cfg, 0o600); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, m.bin, "check", "-c", next).CombinedOutput(); err != nil { // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- m.bin is the operator-configured binary resolved to an absolute path in Init; arguments are fixed
		os.Remove(next)
		return fmt.Errorf("sing-box rejected the generated config: %s", strings.TrimSpace(string(out)))
	}

	m.mu.Lock()
	running := m.cmd != nil
	m.mu.Unlock()
	if running && m.BeforeReload != nil {
		m.BeforeReload(ctx)
	}
	if err := os.Rename(next, m.configPath()); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = cfg
	m.status.ConfigAppliedAt = time.Now()
	m.wantRun = true
	if m.cmd != nil {
		// sing-box re-checks and re-creates its instance on SIGHUP. Existing
		// connections are closed; clients reconnect transparently.
		m.log.Info("reloading")
		m.logs.add("[portunus] reloading configuration")
		return m.cmd.Process.Signal(syscall.SIGHUP)
	}
	m.signalLocked()
	return nil
}

// Restart forcibly restarts sing-box with the current config.
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.Lock()
	cmd, exited, want := m.cmd, m.exited, m.wantRun
	m.mu.Unlock()
	if !want {
		return errors.New("sing-box is stopped: there are no enabled inbounds")
	}
	if cmd != nil {
		if m.BeforeReload != nil {
			m.BeforeReload(ctx)
		}
		m.logs.add("[portunus] restart requested")
		m.mu.Lock()
		m.restarting = true
		m.mu.Unlock()
		cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
		}
	}
	m.mu.Lock()
	m.status.LastError = ""
	m.signalLocked()
	m.mu.Unlock()
	return nil
}

func (m *Manager) stop(ctx context.Context) {
	m.mu.Lock()
	m.wantRun = false
	m.current = nil
	cmd, exited := m.cmd, m.exited
	m.mu.Unlock()
	os.Remove(m.configPath())
	if cmd == nil {
		return
	}
	if m.BeforeReload != nil {
		m.BeforeReload(ctx)
	}
	m.log.Info("stopping: no enabled inbounds")
	m.logs.add("[portunus] stopping: no enabled inbounds")
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-exited
	}
}

func (m *Manager) signalLocked() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Run supervises the process until ctx is cancelled, then stops sing-box.
func (m *Manager) Run(ctx context.Context) {
	backoff := time.Second
	for {
		m.mu.Lock()
		want := m.wantRun && m.cmd == nil
		m.mu.Unlock()

		if want {
			started := time.Now()
			err := m.runOnce(ctx)
			if ctx.Err() != nil {
				return
			}
			m.mu.Lock()
			if m.restarting {
				// Planned restart: start again right away.
				m.restarting = false
				m.mu.Unlock()
				backoff = time.Second
				continue
			}
			if m.wantRun {
				// Unexpected exit: record it and retry with backoff.
				m.status.State = StateFailed
				m.status.Restarts++
				if err != nil {
					m.status.LastError = err.Error()
				}
				m.log.Error("sing-box exited", "err", err, "retry_in", backoff)
				m.logs.add(fmt.Sprintf("[portunus] sing-box exited (%v); restarting in %s", err, backoff))
			} else {
				m.status = Status{State: StateStopped, Version: m.status.Version, Restarts: m.status.Restarts}
			}
			retry := m.wantRun
			m.mu.Unlock()

			if time.Since(started) > time.Minute {
				backoff = time.Second
			}
			if retry {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, 30*time.Second)
			}
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-m.kick:
		}
	}
}

func (m *Manager) runOnce(ctx context.Context) error {
	cmd := exec.Command(m.bin, "run", "-c", m.configPath(), "-D", m.dir, "--disable-color") // nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command -- m.bin is the operator-configured binary resolved to an absolute path in Init; arguments are fixed
	cmd.SysProcAttr = sysProcAttr()
	cmd.Env = append(os.Environ(), m.Env...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	go m.pump(pr)

	if err := cmd.Start(); err != nil {
		pw.Close()
		return err
	}
	exited := make(chan struct{})
	m.mu.Lock()
	m.cmd, m.exited = cmd, exited
	m.status.State = StateStarting
	m.status.PID = cmd.Process.Pid
	m.status.StartedAt = time.Now()
	m.mu.Unlock()
	m.log.Info("started", "pid", cmd.Process.Pid)

	// Consider it running once it has survived a moment.
	go func() {
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			m.mu.Lock()
			if m.cmd == cmd {
				m.status.State = StateRunning
				m.status.LastError = ""
			}
			m.mu.Unlock()
		}
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err = <-waitErr:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
			err = <-waitErr
		}
	}
	pw.Close()
	close(exited)
	m.mu.Lock()
	m.cmd, m.exited = nil, nil
	m.status.PID = 0
	m.mu.Unlock()
	return err
}

// pump forwards sing-box output to the ring buffer and the debug log.
func (m *Manager) pump(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		line := sc.Text()
		m.logs.add(line)
		m.log.Debug(line)
	}
}

// ring keeps the last n log lines.
type ring struct {
	mu   sync.Mutex
	buf  []string
	next int
	full bool
}

func newRing(n int) *ring { return &ring{buf: make([]string, n)} }

func (r *ring) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.next] = s
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

func (r *ring) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		return append([]string(nil), r.buf[:r.next]...)
	}
	return append(append([]string(nil), r.buf[r.next:]...), r.buf[:r.next]...)
}
