package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
)

// Version is set at build time via ldflags (e.g. -ldflags "-X main.Version=v1.0.0").
var Version string

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

	// Ensure required directories exist.
	for _, d := range []string{*btrfsPath, *networkStatePath} {
		if d != "" {
			if err := os.MkdirAll(d, 0750); err != nil {
				return fmt.Errorf("create directory %s: %w", d, err)
			}
		}
	}

	dbFile := filepath.Join(dir, "test.db")
	if *dbPath != "" {
		dbFile = *dbPath
		if err := os.MkdirAll(filepath.Dir(dbFile), 0750); err != nil {
			return fmt.Errorf("create db directory: %w", err)
		}
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
		repoData, err := json.Marshal(defaults) //nolint:gosec // G117 -- Password field is internal config, not user-facing
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

	// Read the tag baked into the image at push time. This lets us derive
	// matching tags for sibling images (UI, rolodex) at runtime.
	tag := "rc.latest"
	if Version != "" {
		tag = Version
	} else if data, err := os.ReadFile("/town-os.tag"); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			tag = t
		}
	}

	// Start a background goroutine to periodically refresh repositories.
	go func() {
		ticker := time.NewTicker(packages.DefaultRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rr.Refresh() //nolint:contextcheck // Refresh does not accept a context
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
		InternalIP:               getInternalIP(),
	})
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	// Derive rolodex image early so we can pull it alongside monitoring images.
	rolImage := os.Getenv("ROLODEX_IMAGE")
	if rolImage == "" {
		rolImage = "quay.io/town/rolodex:" + tag
	}

	// Derive UI image.
	uiImage := os.Getenv("UI_IMAGE")
	if uiImage == "" {
		uiImage = "quay.io/town/ui:" + tag
	}

	// Pull system service container images (non-fatal).
	for _, img := range []string{
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
		monitoring.GrafanaImage,
		rolImage,
		uiImage,
	} {
		if out, err := exec.CommandContext(ctx, "podman", "pull", img).CombinedOutput(); err != nil { //nolint:gosec // G204 -- image constants
			fmt.Fprintf(os.Stderr, "pull %s: %v: %s\n", img, err, string(out))
		}
	}

	// Start the monitoring stack (Prometheus + Node Exporter + Grafana).
	monDataDir := filepath.Join(repoBase, "monitoring")
	monMgr := monitoring.NewManager(monitoring.Config{
		Systemd: sd,
		DataDir: monDataDir,
	})
	if err := monMgr.Start(ctx); err != nil {
		// Non-fatal: monitoring failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "monitoring: %v\n", err)
		monMgr = nil
	}

	// Start the rolodex DNS server. Data lives on the btrfs partition so
	// it persists across rebuilds and is never recreated from scratch.
	rolDataDir := filepath.Join(*btrfsPath, "rolodex")
	if err := os.MkdirAll(rolDataDir, 0750); err != nil {
		return fmt.Errorf("create rolodex data dir: %w", err)
	}
	rolLocal := os.Getenv("ROLODEX_LOCAL") != ""
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        rolDataDir,
		Image:          rolImage,
		Local:          rolLocal,
		UnixSocketPath: filepath.Join(rolDataDir, "rolodex.sock"),
		ResolvConfPath: "/etc/resolv.conf",
		PublicAddr:     getInternalIP(),
	})
	if err := rolMgr.Start(ctx); err != nil {
		// Non-fatal: rolodex failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "rolodex: %v\n", err)
		rolMgr = nil
	}

	// Poll for internal IP changes (DHCP lease renewals) and restart
	// rolodex when the public address changes.
	if rolMgr != nil && !rolLocal {
		go watchInternalIP(ctx, rolMgr)
	}

	// Reconcile DNS state: set up TLD zone and register records for all
	// installed packages. This runs after rolodex is started so the gRPC
	// socket is available.
	if rolMgr != nil {
		socketPath := rolMgr.SocketPath()
		deadline := time.Now().Add(30 * time.Second)
		var rolClient rolodex.Client
		for time.Now().Before(deadline) {
			rolClient, err = rolodex.Dial(ctx, socketPath)
			if err == nil {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if rolClient != nil {
			dnsErr := systemcontroller.ReconcileDNS(ctx, systemcontroller.ReconcileDNSConfig{
				Client:         rolClient,
				Installer:      inst,
				RepositoryRoot: rr,
				SettingsMgr:    settingsMgr,
				InternalIP:     getInternalIP(),
			})
			if dnsErr != nil {
				fmt.Fprintf(os.Stderr, "reconcile DNS: %v\n", dnsErr)
			}
			if closeErr := rolClient.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close rolodex client: %v\n", closeErr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "reconcile DNS: could not connect to rolodex socket\n")
		}
	}

	// Start the UI container (Caddy web server).
	uiMgr := ui.NewManager(ui.Config{Systemd: sd, Image: uiImage})
	if err := uiMgr.Start(ctx); err != nil {
		// Non-fatal: UI failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "ui: %v\n", err)
		uiMgr = nil
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
		Rolodex:                  rolMgr,
		UI:                       uiMgr,
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
		if rolMgr != nil {
			if stopErr := rolMgr.Stop(context.Background()); stopErr != nil {
				fmt.Fprintf(os.Stderr, "rolodex stop: %v\n", stopErr)
			}
		}
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

// getInternalIP returns the first non-loopback IPv4 address, or "" if none found.
func getInternalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return ""
}

// watchInternalIP polls for internal IP changes every 30 seconds and restarts
// rolodex when the public address changes (e.g. DHCP lease renewal).
func watchInternalIP(ctx context.Context, mgr *rolodex.Manager) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newIP := getInternalIP()
			if newIP == "" {
				continue
			}
			if mgr.SetPublicAddr(newIP) {
				slog.Info(fmt.Sprintf("internal IP changed to %s, restarting rolodex", newIP))
				if err := mgr.Start(ctx); err != nil {
					slog.Error(fmt.Sprintf("restart rolodex after IP change: %v", err))
				}
			}
		}
	}
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
