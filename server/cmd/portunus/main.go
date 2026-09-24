// Command portunus is the Portunus control plane. The same binary will also
// run as a node agent (`portunus agent`) once multihop lands.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/PaulichP/Portunus/server/internal/api"
	"github.com/PaulichP/Portunus/server/internal/auth"
	"github.com/PaulichP/Portunus/server/internal/config"
	"github.com/PaulichP/Portunus/server/internal/db"
	"github.com/PaulichP/Portunus/server/internal/store"
	"github.com/PaulichP/Portunus/server/internal/version"
	"github.com/PaulichP/Portunus/server/internal/webui"
)

const usage = `Usage: portunus [command]

Commands:
  serve                      run the panel (default)
  healthcheck                probe the local panel's /healthz; exit 0 if healthy
  reset-password [username]  set a new random password (default user: admin)
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
	case "healthcheck":
		err = healthcheck()
	case "reset-password":
		username := "admin"
		if len(os.Args) > 2 {
			username = os.Args[2]
		}
		err = resetPassword(username)
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
		// Printed exactly once, on first start. Lost it? `portunus reset-password`.
		log.Warn("created initial admin account — change this password after first login",
			"username", user, "password", pw)
	}
	go authSvc.RunJanitor(ctx)

	ui, err := webui.New(webui.Dist())
	if err != nil {
		return fmt.Errorf("load web ui: %w", err)
	}
	if webui.Dist() == nil {
		log.Warn("built without web ui (-tags noui); serving API only")
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           api.New(cfg, log, st, authSvc).Handler(ui),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

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

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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
	fmt.Printf("New password for %s: %s\nAll sessions of this user were revoked; a password change is required on next login.\n", username, pw)
	return nil
}
