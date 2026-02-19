package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func run() (err error) {
	dbPath := flag.String("db", "", "path to persistent SQLite database file (default: ephemeral temp DB)")
	btrfsPath := flag.String("btrfs", "", "base path for btrfs subvolume operations")
	repoDir := flag.String("repo-dir", "", "base directory for git repositories (default: ephemeral temp dir)")
	flag.Parse()

	dir, err := os.MkdirTemp("", "testserver-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil && err == nil {
			err = rerr
		}
	}()

	dbFile := filepath.Join(dir, "test.db")
	if *dbPath != "" {
		dbFile = *dbPath
	}

	db, err := account.OpenDB(dbFile)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		err = errors.Join(err, db.Close())
	}()

	acctMgr, err := account.InitManager(db)
	if err != nil {
		return fmt.Errorf("init account manager: %w", err)
	}

	signingKey := []byte("test-signing-key-for-sessions-32")
	sessMgr, err := account.InitSessionManager(db, acctMgr, signingKey)
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		return fmt.Errorf("init audit manager: %w", err)
	}

	repoBase := dir
	if *repoDir != "" {
		repoBase = *repoDir
		if err := os.MkdirAll(repoBase, 0755); err != nil {
			return fmt.Errorf("create repo dir: %w", err)
		}
	}

	repoFile := filepath.Join(repoBase, packages.RepositoriesFile)
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		repoData, err := json.Marshal([]packages.Repository{})
		if err != nil {
			return fmt.Errorf("marshal empty repo list: %w", err)
		}
		if err := os.WriteFile(repoFile, repoData, 0644); err != nil {
			return fmt.Errorf("write repositories file: %w", err)
		}
	}
	rr, err := packages.RepositoryRootFromBase(repoBase)
	if err != nil {
		return fmt.Errorf("init repository root: %w", err)
	}

	inst := packages.NewInstallManager(repoBase)

	handler := systemcontroller.NewHandler(systemcontroller.ServerConfig{
		Storage:         storage.InitBtrFS(*btrfsPath),
		RepositoryRoot:  rr,
		Installer:       inst,
		Systemd:         systemd.NewManager(),
		AccountMgr:      acctMgr,
		SessionMgr:      sessMgr,
		AuditMgr:        auditMgr,
		DefaultRepoUser: os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass: os.Getenv(packages.EnvRepoPassword),
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		cancel()
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	fmt.Fprintln(os.Stderr, "listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}

	_ = ctx
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "testserver: %v\n", err)
		os.Exit(1)
	}
}
