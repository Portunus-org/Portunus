package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Portunus-org/Portunus/server/internal/agent"
	"github.com/Portunus-org/Portunus/server/internal/backup"
	"github.com/Portunus-org/Portunus/server/internal/config"
	"github.com/Portunus-org/Portunus/server/internal/core"
	"github.com/Portunus-org/Portunus/server/internal/db"
	"github.com/Portunus-org/Portunus/server/internal/secrets"
	"github.com/Portunus-org/Portunus/server/internal/singbox"
	"github.com/Portunus-org/Portunus/server/internal/store"
)

func backupSource(cfg *config.Config) backup.Source {
	return backup.Source{DataDir: cfg.DataDir, DBPath: cfg.DBPath(), MasterKeyPath: cfg.MasterKeyPath()}
}

// backupCommand writes everything a panel consists of to stdout as a
// .tar.gz, while the panel keeps running. Unpacked into an empty data
// directory on any server (`portunus restore`), it is the same panel: the
// nodes do not care where the panel calls them from.
//
// The archive holds every secret of the installation. Keep it accordingly.
func backupCommand() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if stat, err := os.Stdout.Stat(); err == nil && stat.Mode()&os.ModeCharDevice != 0 {
		return errors.New("backup writes an archive to standard output; redirect it to a file:\n  docker compose exec -T portunus portunus backup > portunus-backup.tgz")
	}
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, cfg.DBPath(), slog.New(slog.DiscardHandler))
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	src := backupSource(cfg)
	src.Snapshot = store.New(sqlDB).Snapshot
	return backup.Write(ctx, os.Stdout, src)
}

// restoreCommand brings a panel back from a backup into an empty data
// directory: either an archive made by `portunus backup` ("-" reads it from
// standard input), or the sealed copy a standby node keeps. In the second
// case the recovery key is asked for, and the server takes the place of the
// main one (core.PromoteNode).
func restoreCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: portunus restore FILE     (\"-\": standard input)")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if _, err := os.Stat(cfg.DBPath()); err == nil {
		return fmt.Errorf("there already is a panel in %s; a backup is restored into an empty one", cfg.DataDir)
	}
	var data []byte
	if args[0] == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(args[0])
	}
	if err != nil {
		return err
	}
	if backup.Sealed(data) {
		key := os.Getenv("PORTUNUS_RECOVERY_KEY")
		if key == "" {
			if args[0] == "-" {
				return errors.New("set PORTUNUS_RECOVERY_KEY: the backup is sealed and standard input is taken")
			}
			fmt.Fprint(os.Stderr, "Recovery key: ")
			key, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}
		if data, err = backup.Open(data, key); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if err := backup.Unpack(bytes.NewReader(data), cfg.DataDir); err != nil {
		return err
	}
	fmt.Println("The panel's data is restored.")

	// A copy from a node says which node it was kept on.
	var copyInfo agent.PanelCopy
	if raw, err := os.ReadFile(filepath.Join(filepath.Dir(args[0]), agent.PanelCopyInfo)); err != nil || json.Unmarshal(raw, &copyInfo) != nil || copyInfo.Node == 0 {
		fmt.Println("Start it (docker compose up -d) and set this server's address in Settings.")
		return nil
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sqlDB, err := db.Open(ctx, cfg.DBPath(), log)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	box, _, err := secrets.LoadOrCreate(cfg.MasterKeyPath())
	if err != nil {
		return err
	}
	c := core.New(store.New(sqlDB), box, singbox.New(cfg.SingBoxBin, cfg.SingBoxDir(), log), core.Options{Offline: true}, log)
	n, err := c.PromoteNode(ctx, copyInfo.Node)
	if err != nil {
		return fmt.Errorf("the data is restored, but this server could not take the main one's place: %w", err)
	}
	fmt.Printf("This server (%s, until now the node %q) is the main one from here on.\n", n.Address, n.Name)
	fmt.Println(strings.TrimSpace(`
Links and profiles that clients already have for this server keep working.
Next:
  1. Remove the node's container and start the panel:
       sudo docker rm -f portunus-node && sudo docker compose up -d
  2. Sign in with your usual login and password.
  3. If the old main server ever returns, do not start its panel again.`))
	return nil
}
