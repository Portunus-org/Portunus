// Command portunus-node is all a node needs: the agent that takes a
// configuration from the panel and keeps sing-box running with it. It has no
// web interface, no database and no accounts; the panel binary (`portunus`)
// carries those.
//
// Configuration, all from the environment:
//
//	PORTUNUS_JOIN              the token from the panel (required)
//	PORTUNUS_DATA_DIR          where the last configuration is kept
//	PORTUNUS_SINGBOX_BIN       sing-box binary (default: from PATH)
//	PORTUNUS_SINGBOX_MEMLIMIT  soft memory limit of sing-box (256MiB)
//	PORTUNUS_LOG_LEVEL         debug, info (default), warn, error
//	PORTUNUS_LOG_FORMAT        json (default) or text
//	PORTUNUS_UPDATE_DIR        directory shared with the updater
//
// The same image is also the updater, the container that replaces the panel
// or a node with a newer release (internal/selfupdate):
//
//	portunus-node updater <container>
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/selfupdate"
	"github.com/Portunus-org/Portunus/server/internal/version"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.Version, version.Commit)
		return
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("PORTUNUS_LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var log *slog.Logger
	if env("PORTUNUS_LOG_FORMAT", "json") == "text" {
		log = slog.New(slog.NewTextHandler(os.Stderr, opts))
	} else {
		log = slog.New(slog.NewJSONHandler(os.Stderr, opts))
	}
	slog.SetDefault(log)
	if len(os.Args) > 1 && os.Args[1] == "updater" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		target := ""
		if len(os.Args) > 2 {
			target = os.Args[2]
		}
		if err := selfupdate.Run(ctx, selfupdate.Options{
			Target: target,
			Dir:    env("PORTUNUS_UPDATE_DIR", selfupdate.DefaultDir),
			Socket: env("PORTUNUS_DOCKER_SOCKET", "/run/docker.sock"),
			Log:    log,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "portunus-node updater:", err)
			os.Exit(1)
		}
		return
	}
	err := agent.Run(agent.RunOptions{
		Join:          os.Getenv("PORTUNUS_JOIN"),
		DataDir:       env("PORTUNUS_DATA_DIR", "./data"),
		SingBoxBin:    env("PORTUNUS_SINGBOX_BIN", "sing-box"),
		SingBoxMemory: env("PORTUNUS_SINGBOX_MEMLIMIT", "256MiB"),
		UpdateDir:     env("PORTUNUS_UPDATE_DIR", selfupdate.DefaultDir),
		Log:           log,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "portunus-node:", err)
		os.Exit(1)
	}
}
