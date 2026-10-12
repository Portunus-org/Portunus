// Command portunus is the Portunus control plane. The same binary will also
// run as a node agent (`portunus agent`) once multihop lands.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/PaulichP/Portunus/server/internal/agent"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PaulichP/Portunus/server/internal/api"
	"github.com/PaulichP/Portunus/server/internal/auth"
	"github.com/PaulichP/Portunus/server/internal/config"
	"github.com/PaulichP/Portunus/server/internal/core"
	"github.com/PaulichP/Portunus/server/internal/db"
	"github.com/PaulichP/Portunus/server/internal/secrets"
	"github.com/PaulichP/Portunus/server/internal/setup"
	"github.com/PaulichP/Portunus/server/internal/singbox"
	"github.com/PaulichP/Portunus/server/internal/store"
	"github.com/PaulichP/Portunus/server/internal/version"
	"github.com/PaulichP/Portunus/server/internal/webui"
)

const usage = `Usage: portunus [command]

Commands:
  serve                      run the panel (default)
  agent                      run as a node of another panel (needs PORTUNUS_JOIN)
  node add NAME ADDRESS [exit] [entry] [standby]
                             register a node and print the command that starts it
  node join NAME             print that command again for an existing node
  healthcheck                probe the local panel's /healthz; exit 0 if healthy
  reset-password [username]  set a new random password (default user: admin)
  backup                     write a backup of the whole panel to standard output
                             (a .tar.gz; the panel keeps running)
  restore FILE               bring the panel back from a backup, or from the copy
                             a standby node keeps, into an empty data directory
  version                    print version information

Configuration is read from PORTUNUS_* environment variables; see docs/DEPLOY.md.
`

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "agent":
		err = runAgent()
	case "node":
		err = nodeCommand(os.Args[2:])
	case "healthcheck":
		err = healthcheck()
	case "reset-password":
		username := "admin"
		if len(os.Args) > 2 {
			username = os.Args[2]
		}
		err = resetPassword(username)
	case "backup":
		err = backupCommand()
	case "restore":
		err = restoreCommand(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("portunus %s (%s, %s)\n", version.Version, version.Commit, version.GoVersion())
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "portunus:", err)
		os.Exit(1)
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stderr, opts))
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting portunus", "version", version.Version, "commit", version.Commit, "data_dir", cfg.DataDir)

	sqlDB, err := db.Open(ctx, cfg.DBPath(), log)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	st := store.New(sqlDB)

	authSvc, err := auth.NewService(st, log, cfg.SessionTTL, cfg.SessionMaxAge)
	if err != nil {
		return err
	}
	if user, pw, err := authSvc.Bootstrap(ctx); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	} else if pw != "" {
		log.Warn("created the initial admin account", "username", user)
		// Kept beside the database until it is replaced at the first sign-in,
		// so that it can be shown again: logs get lost.
		if err := os.WriteFile(cfg.InitialLoginPath(), []byte(pw+"\n"), 0o600); err != nil {
			log.Error("save the initial password", "err", err)
			showInitialLogin(cfg, user, pw)
		}
	}
	announceInitialLogin(ctx, cfg, st)
	go authSvc.RunJanitor(ctx)

	box, created, err := secrets.LoadOrCreate(cfg.MasterKeyPath())
	if err != nil {
		return err
	}
	if created {
		log.Warn("generated a new master key; back it up together with the database", "path", cfg.MasterKeyPath())
	}

	// The data plane. A missing sing-box binary does not stop the panel:
	// it is reported in the UI and in the logs.
	mgr := singbox.New(cfg.SingBoxBin, cfg.SingBoxDir(), log)
	// A stable QUIC stateless-reset key lets Hysteria2/TUIC clients notice a
	// reload at once instead of after their 30 s idle timeout. Read by the
	// patched sing-quic in our sing-box build (docs/DECISIONS.md, D-17).
	mgr.Env = []string{
		"SING_QUIC_STATELESS_RESET_KEY=" + hex.EncodeToString(box.Derive("sing-quic stateless reset")),
		// Its own memory limit, not the panel's (the last value wins).
		"GOMEMLIMIT=" + cfg.SingBoxMemory,
	}
	if err := mgr.Init(ctx); err != nil {
		log.Error("sing-box unavailable; proxy inbounds will not start", "err", err)
	}
	coreSvc := core.New(st, box, mgr, core.Options{
		StatsAPI:   cfg.SingBoxAPI,
		ControlAPI: cfg.SingBoxCtl,
		HostPorts:  setup.ListeningPorts,
		NodeImage:  cfg.NodeImage,
		Backup:     backupSource(cfg),
		GeoURL:     cfg.GeoLookup,
		Reserved:   reservedPorts(cfg),
		// Inside sing-box's own directory: the one place it is sure to read.
		PanelCertDir: filepath.Join(cfg.SingBoxDir(), "panel-cert"),
		ACME: core.ACMEPaths{
			DataDirectory: "acme", // relative to sing-box's working directory
			StorageDir:    filepath.Join(cfg.SingBoxDir(), "acme"),
			Directory:     cfg.ACMEDirectory,
		},
		SubListen: cfg.SubListen,
	}, log)

	var wg sync.WaitGroup
	wg.Go(func() { mgr.Run(ctx) })
	wg.Go(func() { coreSvc.Run(ctx, 30*time.Second) })
	wg.Go(func() { coreSvc.RunNodes(ctx, 30*time.Second) })

	ui, err := webui.New(webui.Dist())
	if err != nil {
		return fmt.Errorf("load web ui: %w", err)
	}
	if webui.Dist() == nil {
		log.Warn("built without web ui (-tags noui); serving API only")
	}

	apiSrv := api.New(cfg, log, st, authSvc, coreSvc, mgr)
	srv := newHTTPServer(apiSrv.Handler(ui), log)

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	if cfg.ListenIsLoopback() {
		log.Info("panel listening on loopback only; reach it through an SSH tunnel",
			"addr", ln.Addr().String(),
			"tunnel", fmt.Sprintf("ssh -L %s:%s user@server", portOf(cfg.Listen), cfg.Listen))
	} else {
		log.Warn("panel is listening on a non-loopback address; make sure it is not reachable from the internet without protection",
			"addr", ln.Addr().String())
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	wg.Go(func() { apiSrv.RunSubscriptions(ctx) })
	wg.Go(func() { apiSrv.RunPanelAccess(ctx) })

	select {
	case err := <-errc:
		stop()
		wg.Wait()
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Stops sing-box (the manager's Run returns once it has exited).
	wg.Wait()
	return nil
}

// runAgent is the node side: it takes its configuration from the panel
// over a mutually authenticated channel and runs sing-box with it.
func runAgent() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	slog.SetDefault(log)

	return agent.Run(agent.RunOptions{
		Join:          os.Getenv("PORTUNUS_JOIN"),
		DataDir:       cfg.DataDir,
		SingBoxBin:    cfg.SingBoxBin,
		SingBoxMemory: cfg.SingBoxMemory,
		Log:           log,
	})
}

// announceInitialLogin prints how to sign in for as long as the admin still
// has the password the panel made up: on every start, so `docker compose
// logs` always has it. Once the password is changed the file is removed (by
// the API) and nothing is printed any more.
func announceInitialLogin(ctx context.Context, cfg *config.Config, st *store.Store) {
	raw, err := os.ReadFile(cfg.InitialLoginPath())
	if err != nil {
		return
	}
	u, err := st.UserByUsername(ctx, "admin")
	if err != nil || !u.MustChangePassword {
		os.Remove(cfg.InitialLoginPath())
		return
	}
	showInitialLogin(cfg, u.Username, strings.TrimSpace(string(raw)))
}

// showInitialLogin writes plain lines rather than a log record: this is for
// a person to read.
func showInitialLogin(cfg *config.Config, user, password string) {
	line := strings.Repeat("=", 64)
	host, port, _ := net.SplitHostPort(cfg.Listen)
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "<address of this server>"
	}
	fmt.Printf("\n%s\n  Portunus is ready.\n\n  Address:   http://%s\n  Login:     %s\n  Password:  %s\n\n", line, net.JoinHostPort(host, port), user, password)
	if cfg.ListenIsLoopback() {
		fmt.Printf("  The panel listens on this server only. From your computer:\n    ssh -N -L %s:%s user@this-server\n  and open http://localhost:%s\n\n", port, cfg.Listen, port)
	}
	fmt.Printf("  You will set your own password at the first sign-in; after that\n  this message is no longer shown.\n%s\n\n", line)
}

// nodeCommand registers a node from the command line, next to a running
// panel, and prints what to run on the new server. The panel notices the
// node at its next round (within half a minute).
func nodeCommand(args []string) error {
	const usage = "usage: portunus node add NAME ADDRESS [exit] [entry] [standby]\n       portunus node join NAME"
	if len(args) < 2 || (args[0] != "add" && args[0] != "join") || (args[0] == "add" && len(args) < 3) {
		return errors.New(usage)
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	sqlDB, err := db.Open(ctx, cfg.DBPath(), log)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	box, _, err := secrets.LoadOrCreate(cfg.MasterKeyPath())
	if err != nil {
		return err
	}
	c := core.New(store.New(sqlDB), box, singbox.New(cfg.SingBoxBin, cfg.SingBoxDir(), log), core.Options{
		NodeImage: cfg.NodeImage,
		Offline:   true,
	}, log)

	var n core.Node
	if args[0] == "join" {
		// The command of a node that already exists, to start it again.
		nodes, err := c.Nodes(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, candidate := range nodes {
			if candidate.Kind == core.NodeAgent && strings.EqualFold(candidate.Name, args[1]) {
				n, found = candidate, true
			}
		}
		if !found {
			return fmt.Errorf("no node named %q", args[1])
		}
	} else {
		in := core.NodeInput{Kind: core.NodeAgent, Name: args[1], Address: args[2], Enabled: true}
		for _, role := range args[3:] {
			switch role {
			case "exit":
				in.IsExit = true
			case "entry":
				in.IsEntry = true
			case "standby":
				in.Standby = true
			default:
				return fmt.Errorf("unknown role %q: use exit, entry and standby", role)
			}
		}
		if !in.IsExit && !in.IsEntry && !in.Standby {
			in.IsExit = true
		}
		if n, err = c.CreateNode(ctx, in); err != nil {
			return err
		}
	}
	join, err := c.NodeJoin(ctx, n.ID)
	if err != nil {
		return err
	}
	if join.CopyImage != "" {
		fmt.Printf("# 1. On this server: copy the image\n%s\n\n# 2. On %s: start the node\n", join.CopyImage, n.Address)
	}
	fmt.Println(join.Run)
	return nil
}

// persistentKey reads 32 random bytes from path, creating them once.
func persistentKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o600)
}

func newHTTPServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// reservedPorts are TCP ports the panel itself listens on; inbounds may not
// use them (host networking means they share the host's port space).
func reservedPorts(cfg *config.Config) []core.ReservedPort {
	var out []core.ReservedPort
	add := func(addr, what string) {
		if p, err := strconv.Atoi(portOf(addr)); err == nil {
			out = append(out, core.ReservedPort{Port: p, What: what})
		}
	}
	add(cfg.Listen, "panel")
	add(cfg.SingBoxAPI, "sing-box stats API")
	add(cfg.SingBoxCtl, "sing-box control API")
	if cfg.SubListen != "" {
		add(cfg.SubListen, "subscriptions")
	}
	return out
}

func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}

// healthcheck lets the container probe itself without shipping curl.
func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(cfg.Listen)
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: HTTP %d", resp.StatusCode)
	}
	return nil
}

func resetPassword(username string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, cfg.DBPath(), log)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	st := store.New(sqlDB)

	authSvc, err := auth.NewService(st, log, cfg.SessionTTL, cfg.SessionMaxAge)
	if err != nil {
		return err
	}
	pw, err := authSvc.ResetPassword(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("user %q does not exist", username)
	}
	if err != nil {
		return err
	}
	st.Audit(ctx, store.AuditEntry{Actor: "cli", Action: "auth.password_reset", Target: username})
	if username == "admin" {
		// The password from the first start is no longer the one.
		os.Remove(cfg.InitialLoginPath())
	}
	fmt.Printf("New password for %s: %s\nAll sessions of this user were revoked; a password change is required on next login.\n", username, pw)
	return nil
}
