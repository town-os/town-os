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
	upnpBin := flag.String("upnp-bin", "/town-os-upnp", "path to the town-os-upnp binary")
	networkMode := flag.String("network-mode", "", "container network mode: empty uses -p mappings; host uses --net host")
	flag.Parse()

	// Allow env var fallback when flag is not set.
	if *networkMode == "" {
		if env := os.Getenv("TOWN_OS_NETWORK_MODE"); env != "" {
			*networkMode = env
		}
	}

	dir, err := os.MkdirTemp("", "systemcontroller-*")
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

	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		return fmt.Errorf("init settings manager: %w", err)
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
		defaults := packages.DefaultRepositories()
		if os.Getenv("DEBUG") != "" {
			defaults = append(defaults, packages.TestRepositories()...)
		}
		repoUser := os.Getenv(packages.EnvRepoUsername)
		repoPass := os.Getenv(packages.EnvRepoPassword)
		if repoUser != "" && repoPass != "" {
			for i := range defaults {
				defaults[i].Username = repoUser
				defaults[i].Password = repoPass
			}
		}
		repoData, err := json.Marshal(defaults)
		if err != nil {
			return fmt.Errorf("marshal default repo list: %w", err)
		}
		if err := os.WriteFile(repoFile, repoData, 0644); err != nil {
			return fmt.Errorf("write repositories file: %w", err)
		}
	}
	rr, err := packages.RepositoryRootFromBase(repoBase)
	if err != nil {
		return fmt.Errorf("init repository root: %w", err)
	}

	rr.Refresh()

	inst := packages.NewInstallManager(repoBase)
	st := storage.InitBtrFS(*btrfsPath)
	sd := systemd.NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        st,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
		BtrfsBasePath:  *btrfsPath,
		UPnPBinPath:    *upnpBin,
		NetworkMode:    *networkMode,
	}); err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	handler := systemcontroller.NewHandler(systemcontroller.ServerConfig{
		Storage:         st,
		RepositoryRoot:  rr,
		Installer:       inst,
		Systemd:         sd,
		AccountMgr:      acctMgr,
		SessionMgr:      sessMgr,
		AuditMgr:        auditMgr,
		SettingsMgr:     settingsMgr,
		DefaultRepoUser: os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass: os.Getenv(packages.EnvRepoPassword),
		BtrfsBasePath:   *btrfsPath,
		UPnPBinPath:     *upnpBin,
		NetworkMode:     *networkMode,
	})

	srv := &http.Server{
		Addr:    ":5309",
		Handler: handler,
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		cancel()
		if err := srv.Shutdown(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
		}
	}()

	fmt.Fprintln(os.Stderr, "systemcontroller: listening on :5309")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
