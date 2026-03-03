package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func run() (err error) {
	dbPath := flag.String("db", "", "path to persistent SQLite database file (default: ephemeral temp DB)")
	btrfsPath := flag.String("btrfs", "", "base path for btrfs subvolume operations")
	repoDir := flag.String("repo-dir", "", "base directory for git repositories (default: ephemeral temp dir)")
	networkControllerBin := flag.String("networkcontroller-bin", "/town-os-networkcontroller", "path to the town-os-networkcontroller binary")
	networkStatePath := flag.String("network-state", "/var/run/town-os", "directory for per-package network state files")
	networkMode := flag.String("network-mode", "", "container network mode: empty uses -p mappings; host uses --net host")
	listenAddr := flag.String("listen", ":5309", "address to listen on")
	flag.Parse()

	// Allow env var fallback when flag is not set.
	if *networkMode == "" {
		if env := os.Getenv("TOWN_OS_NETWORK_MODE"); env != "" {
			*networkMode = env
		}
	}

	if *listenAddr == ":5309" {
		if env := os.Getenv("TOWN_OS_LISTEN"); env != "" {
			*listenAddr = env
		}
	}

	dir, err := os.MkdirTemp("", "systemcontroller-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		rerr := os.RemoveAll(dir)
		if rerr != nil && err == nil {
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

	signingKey, err := generateSigningKey()
	if err != nil {
		return fmt.Errorf("signing key: %w", err)
	}
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

	pagesMgr, err := account.InitPagesManager(db)
	if err != nil {
		return fmt.Errorf("init pages manager: %w", err)
	}

	repoBase := dir
	if *repoDir != "" {
		repoBase = *repoDir
		err := os.MkdirAll(repoBase, 0750)
		if err != nil {
			return fmt.Errorf("create repo dir: %w", err)
		}
	}

	repoFile := filepath.Join(repoBase, packages.RepositoriesFile)
	_, err = os.Stat(repoFile)
	if os.IsNotExist(err) {
		var defaults []packages.Repository
		if os.Getenv("TOWN_OS_TEST") != "" {
			defaults = packages.TestRepositories()
		} else {
			defaults = packages.DefaultRepositories()
			if os.Getenv("DEBUG") != "" {
				defaults = append(packages.TestRepositories(), defaults...)
			}
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
		err = os.WriteFile(repoFile, repoData, 0600)
		if err != nil {
			return fmt.Errorf("write repositories file: %w", err)
		}
	}
	rr, err := packages.RepositoryRootFromBase(repoBase)
	if err != nil {
		return fmt.Errorf("init repository root: %w", err)
	}

	rr.ForceRefresh()

	inst := packages.NewInstallManager(repoBase)
	st := storage.InitBtrFS(*btrfsPath)
	sd := systemd.NewManager()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start a background goroutine to periodically refresh repositories.
	go func() {
		ticker := time.NewTicker(packages.DefaultRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rr.Refresh() //nolint:contextcheck // Refresh is a time-gated method that does not accept context
			}
		}
	}()

	err = systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:                inst,
		RepositoryRoot:           rr,
		Storage:                  st,
		Systemd:                  sd,
		SettingsMgr:              settingsMgr,
		PagesManager:             pagesMgr,
		BtrfsBasePath:            *btrfsPath,
		NetworkControllerBinPath: *networkControllerBin,
		NetworkStatePath:         *networkStatePath,
		NetworkMode:              *networkMode,
	})
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	// Start the monitoring stack (Prometheus + Node Exporter + Grafana).
	monDataDir := filepath.Join(repoBase, "monitoring")
	monMgr := monitoring.NewManager(monitoring.Config{
		Runner:  monitoring.PodmanRunner{},
		DataDir: monDataDir,
	})
	if err := monMgr.Start(ctx); err != nil {
		// Non-fatal: monitoring failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "monitoring: %v\n", err)
		monMgr = nil
	}

	handler := systemcontroller.NewHandler(systemcontroller.ServerConfig{
		Storage:                  st,
		RepositoryRoot:           rr,
		Installer:                inst,
		Systemd:                  sd,
		AccountMgr:               acctMgr,
		SessionMgr:               sessMgr,
		AuditMgr:                 auditMgr,
		SettingsMgr:              settingsMgr,
		PagesMgr:                 pagesMgr,
		DefaultRepoUser:          os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass:          os.Getenv(packages.EnvRepoPassword),
		BtrfsBasePath:            *btrfsPath,
		NetworkControllerBinPath: *networkControllerBin,
		NetworkStatePath:         *networkStatePath,
		NetworkMode:              *networkMode,
		Monitoring:               monMgr,
	})

	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		cancel()
		shutdownErr := srv.Shutdown(context.Background())
		if shutdownErr != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", shutdownErr)
		}
	}()

	fmt.Fprintf(os.Stderr, "systemcontroller: listening on %s\n", *listenAddr)
	err = srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen: %w", err)
	}

	return nil
}

// generateSigningKey returns a fresh random 32-byte JWT signing key. The key
// is never persisted so that all sessions are implicitly invalidated on each
// service restart.
func generateSigningKey() ([]byte, error) {
	if env := os.Getenv("TOWN_OS_SIGNING_KEY"); env != "" {
		return []byte(env), nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}

	return key, nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
