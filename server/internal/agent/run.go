package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/PaulichP/Portunus/server/internal/singbox"
	"github.com/PaulichP/Portunus/server/internal/version"
)

// RunOptions is what a node needs to know about its own machine. Everything
// else comes from the panel.
type RunOptions struct {
	// Join is the token from the panel (PORTUNUS_JOIN).
	Join string
	// DataDir keeps the last configuration and sing-box's own files.
	DataDir string
	// SingBoxBin is the sing-box binary; SingBoxMemory its GOMEMLIMIT.
	SingBoxBin    string
	SingBoxMemory string
	Log           *slog.Logger
}

// Run is the node side: it takes its configuration from the panel and runs
// sing-box with it, until the process is told to stop.
func Run(o RunOptions) error {
	log := o.Log
	bundle, err := DecodeBundle(o.Join)
	if err != nil {
		return fmt.Errorf("%w (copy the command from the panel: Nodes, then the node's menu)", err)
	}
	tlsCfg, err := bundle.TLSConfig()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Info("starting portunus agent", "version", version.Version, "port", bundle.Port)

	mgr := singbox.New(o.SingBoxBin, filepath.Join(o.DataDir, "sing-box"), log)
	mgr.Env = []string{"GOMEMLIMIT=" + o.SingBoxMemory}
	// A reset key that survives restarts, so QUIC clients of a backup entry
	// notice a reload at once.
	if key, err := resetKey(filepath.Join(o.DataDir, "quic-reset.key")); err == nil {
		mgr.Env = append(mgr.Env, "SING_QUIC_STATELESS_RESET_KEY="+hex.EncodeToString(key))
	}
	if err := mgr.Init(ctx); err != nil {
		return fmt.Errorf("sing-box unavailable: %w", err)
	}
	srv := NewServer(mgr, o.DataDir, log)

	var wg sync.WaitGroup
	wg.Go(func() { mgr.Run(ctx) })
	// Go on serving with the last configuration, panel or no panel.
	srv.Resume(ctx)

	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(bundle.Port)))
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ServeTLS(ln, "", "") }()

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
	httpSrv.Shutdown(shutdownCtx)
	wg.Wait()
	return nil
}

// resetKey reads 32 random bytes from path, creating them once.
func resetKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o600)
}
