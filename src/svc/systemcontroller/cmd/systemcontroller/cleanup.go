package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// staleRootDBName is the basename of a SQLite file that older builds (or
// out-of-tree scripts) sometimes left at the btrfs root. It is not a path
// the systemcontroller has ever written to in the current codebase, but
// historical leftovers do appear on real devices, so we delete them on
// every boot.
const staleRootDBName = "town-os.db"

// staleRootDBSuffixes covers the SQLite WAL/SHM/journal sidecar files
// that may live next to a stale `town-os.db`. The empty entry handles
// the main file itself.
var staleRootDBSuffixes = []string{"", "-wal", "-shm", "-journal"}

// cleanupStaleRootDB removes any stray `<btrfsBase>/town-os.db` (and its
// SQLite sidecar files) left over from prior deployments. The btrfs root
// is meant to hold subvolumes only — the runtime DB lives under
// `<btrfsBase>/data/db/system.db` per the install-repo systemd unit.
//
// The function is best-effort and idempotent: missing files are ignored,
// per-file failures are logged but never returned, and the function
// returns no error so a misbehaving filesystem cannot block startup.
func cleanupStaleRootDB(btrfsBase string) {
	if btrfsBase == "" {
		return
	}
	for _, suffix := range staleRootDBSuffixes {
		path := filepath.Join(btrfsBase, staleRootDBName+suffix)
		if err := os.Remove(path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("cleanup stale root DB", "path", path, "error", err)
			}
			continue
		}
		slog.Info("removed stale file at btrfs root", "path", path)
	}
}

// validateDBPath rejects -db values that would re-create the stale
// `<btrfsBase>/town-os.db` file we just cleaned up. Callers should run
// this after parsing flags but before opening the database. Returns an
// error suitable for fatal startup failure.
func validateDBPath(dbPath, btrfsBase string) error {
	if dbPath == "" || btrfsBase == "" {
		return nil
	}
	stale := filepath.Join(btrfsBase, staleRootDBName)
	cleanDB, err := filepath.Abs(filepath.Clean(dbPath))
	if err != nil {
		return fmt.Errorf("resolve -db path: %w", err)
	}
	cleanStale, err := filepath.Abs(filepath.Clean(stale))
	if err != nil {
		return fmt.Errorf("resolve stale db path: %w", err)
	}
	if cleanDB == cleanStale {
		return fmt.Errorf("-db must not be %s — that path is reserved as a stale-leftover marker; use a subdirectory under -btrfs (e.g. %s/data/db/system.db)", cleanStale, btrfsBase)
	}
	return nil
}
