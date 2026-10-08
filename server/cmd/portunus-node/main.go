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
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/PaulichP/Portunus/server/internal/agent"
	"github.com/PaulichP/Portunus/server/internal/version"
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
	err := agent.Run(agent.RunOptions{
		Join:          os.Getenv("PORTUNUS_JOIN"),
		DataDir:       env("PORTUNUS_DATA_DIR", "./data"),
		SingBoxBin:    env("PORTUNUS_SINGBOX_BIN", "sing-box"),
		SingBoxMemory: env("PORTUNUS_SINGBOX_MEMLIMIT", "256MiB"),
		Log:           log,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "portunus-node:", err)
		os.Exit(1)
	}
}
