package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
	"gitea.com/town-os/town-os/src/ui"
)

// Version is set at build time via ldflags (e.g. -ldflags "-X main.Version=v1.0.0").
var Version string

// archTag maps Go's runtime.GOARCH (amd64/arm64) to the per-arch image tag
// suffix the make pipeline pushes (x86_64/aarch64, the uname -m form). The
// registry tag suffix deliberately differs from Go's GOARCH spelling, so the
// mapping must be explicit rather than using runtime.GOARCH directly.
func archTag() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

// defaultVersionTag is the last-resort image tag when no tag was baked in at
// build time. rc tags are partitioned per architecture (rc.latest-x86_64 /
// rc.latest-aarch64, pushed natively from each host); archTag() maps the Go
// runtime arch to the registry tag suffix used on both supported architectures.
func defaultVersionTag() string {
	return "rc.latest-" + archTag()
}

// HostPodmanSocket is the unix socket URL of the host podman that the
// systemcontroller container bind-mounts in at /run/podman/podman.sock.
// Set into the CONTAINER_HOST env var at startup so every podman
// invocation from this process (and any child process it forks)
// automatically routes through the host socket instead of landing in
// the systemcontroller container's isolated podman storage.
const HostPodmanSocket = "unix:///run/podman/podman.sock"

// DefaultNetworkStatePath is the default value for the -network-state
// flag. It must point to a directory that the systemcontroller container
// and the host share so that NC containers (created on the host via
// CONTAINER_HOST) can bind-mount the same path the systemcontroller
// writes state files into. The install-repo systemd unit must
// bind-mount /run/town-os from the host into the systemcontroller
// container at the same path; without that, NC containers fail to start
// with "statfs /run/town-os: no such file or directory".
const DefaultNetworkStatePath = "/run/town-os"

// setupPodmanEnv sets CONTAINER_HOST in the current process environment
// so that every subsequent `podman` invocation defaults to --url
// HostPodmanSocket. Child processes forked via os/exec inherit the
// same environment. The install-repo systemd unit should also set
// Environment=CONTAINER_HOST=... for visibility in systemctl output,
// but this call is the runtime source of truth.
func setupPodmanEnv() error {
	return os.Setenv("CONTAINER_HOST", HostPodmanSocket)
}

func run() (err error) {
	if err := setupPodmanEnv(); err != nil {
		return fmt.Errorf("setup podman env: %w", err)
	}

	dbPath := flag.String("db", "", "path to persistent SQLite database file (default: ephemeral temp DB)")
	btrfsPath := flag.String("btrfs", "", "base path for btrfs subvolume operations")
	repoDir := flag.String("repo-dir", "", "base directory for git repositories (default: ephemeral temp dir)")
	networkStatePath := flag.String("network-state", DefaultNetworkStatePath, "directory for per-package network state files")
	listenAddr := flag.String("listen", ":5309", "address to listen on")
	flag.Parse()

	if *listenAddr == ":5309" {
		if env := os.Getenv("TOWN_OS_LISTEN"); env != "" {
			*listenAddr = env
		}
	}

	// Bind :5309 immediately with a minimal boot-status handler so the
	// UI can observe the boot sequence as it runs. The handler is
	// swapped to the full Echo router at the very end of this function;
	// the listener socket itself is never closed across the swap, so
	// in-flight SSE subscribers on /boot-status survive the handoff.
	bs := systemcontroller.NewBootStatus()
	defer bs.Done() // safety net — if bootPhase fails below, close the stream cleanly
	rootHandler := systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(bs))
	srv := &http.Server{
		Addr:              *listenAddr,
		Handler:           rootHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listenErrCh := make(chan error, 1)
	go func() {
		listenErr := srv.ListenAndServe()
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			listenErrCh <- listenErr
		}
		close(listenErrCh)
	}()

	bs.Step("setup_temp_dir")
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
	bs.Step("create_dirs")
	for _, d := range []string{*btrfsPath, *networkStatePath} {
		if d != "" {
			if err := os.MkdirAll(d, 0750); err != nil {
				return fmt.Errorf("create directory %s: %w", d, err)
			}
		}
	}

	// Self-heal: remove any stale town-os.db (and its SQLite sidecar
	// files) left at the btrfs root by older deployments. The runtime
	// DB lives under <btrfsBase>/data/db/system.db, never at the root.
	cleanupStaleRootDB(*btrfsPath)

	// Reject -db paths that would re-create the file we just cleaned up.
	if err := validateDBPath(*dbPath, *btrfsPath); err != nil {
		return err
	}

	dbFile := filepath.Join(dir, "test.db")
	if *dbPath != "" {
		dbFile = *dbPath
		if err := os.MkdirAll(filepath.Dir(dbFile), 0750); err != nil {
			return fmt.Errorf("create db directory: %w", err)
		}
	}

	bs.Step("open_db")
	db, err := account.OpenDB(dbFile)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() {
		err = errors.Join(err, db.Close())
	}()

	bs.Step("init_account_mgr")
	acctMgr, err := account.InitManager(db)
	if err != nil {
		return fmt.Errorf("init account manager: %w", err)
	}

	bs.Step("init_session_mgr")
	signingKey, err := generateSigningKey()
	if err != nil {
		return fmt.Errorf("signing key: %w", err)
	}
	sessMgr, err := account.InitSessionManager(db, acctMgr, signingKey)
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	bs.Step("init_audit_mgr")
	auditMgr, err := account.InitAuditManager(db)
	if err != nil {
		return fmt.Errorf("init audit manager: %w", err)
	}

	bs.Step("init_settings_mgr")
	settingsMgr, err := account.InitSettingsManager(db)
	if err != nil {
		return fmt.Errorf("init settings manager: %w", err)
	}

	bs.Step("init_pages_mgr")
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

	bs.Step("seed_repositories")
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
	bs.Step("init_repo_root")
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
	// matching tags for sibling images (UI, rolodex, networkcontroller) at
	// runtime. Baked rc tags are per-arch (rc.<date>-<arch>), so derived
	// sibling tags are per-arch too. Fallback chain: TOWN_OS_TAG env var →
	// compile-time Version ldflags → /town-os.tag file →
	// "rc.latest-<arch>" (defaultVersionTag).
	tag := defaultVersionTag()
	if envTag := os.Getenv("TOWN_OS_TAG"); envTag != "" {
		tag = envTag
	} else if Version != "" {
		tag = Version
	} else if data, err := os.ReadFile("/town-os.tag"); err == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			tag = t
		}
	}

	// Network controller image: pulled from quay.io like every other
	// sibling image. NC_IMAGE overrides the derived default (used by
	// integration tests to inject localhost/town-os-networkcontroller:local).
	ncImage := os.Getenv("NC_IMAGE")
	if ncImage == "" {
		ncImage = "quay.io/town/networkcontroller:" + tag
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

	// Rolodex is a boot service managed entirely by systemd. The
	// systemcontroller only writes the config file and waits for DNS
	// readiness before proceeding with image pulls.
	rolImage := os.Getenv("ROLODEX_IMAGE")
	if rolImage == "" {
		rolImage = "quay.io/town/rolodex:" + tag
	}

	bs.Step("write_rolodex_config")
	rolDataDir := filepath.Join(*btrfsPath, "rolodex")
	if err := os.MkdirAll(rolDataDir, 0750); err != nil {
		return fmt.Errorf("create rolodex data dir: %w", err)
	}
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        rolDataDir,
		Image:          rolImage,
		UnixSocketPath: filepath.Join(rolDataDir, "rolodex.sock"),
	})
	configWritten, configErr := rolMgr.WriteConfig()
	if configErr != nil {
		fmt.Fprintf(os.Stderr, "rolodex config: %v\n", configErr)
	}

	// Restart rolodex only if the config file was actually written
	// (created or updated). Skip restart when the file was unchanged.
	if configWritten {
		rolUnitName := systemd.SystemServiceUnitName(rolMgr.Key())
		if err := sd.SetStatus(ctx, rolUnitName, systemd.Restart); err != nil {
			fmt.Fprintf(os.Stderr, "rolodex restart: %v\n", err)
		}
	}

	// Wait for DNS readiness.
	bs.Step("wait_rolodex_dns")
	if err := rolMgr.WaitForDNSReady(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "rolodex DNS readiness: %v\n", err)
	}

	// Configure systemd-resolved to route the TLD to rolodex so
	// inter-package DNS resolution works (container -> aardvark ->
	// resolved -> rolodex for .tld queries). Non-fatal.
	dnsTLD := "home"
	if v, tldErr := settingsMgr.Get("dns_tld"); tldErr == nil && v != "" {
		dnsTLD = v
	}
	rolodex.ConfigureResolvedRouting(ctx, dnsTLD, rolodex.DNSLoopback)

	// Derive UI image. When UI_IMAGE is explicitly empty the UI container
	// is skipped entirely (useful in dev where bun serves the UI).
	uiImage := os.Getenv("UI_IMAGE")
	if _, uiSet := os.LookupEnv("UI_IMAGE"); !uiSet {
		uiImage = "quay.io/town/ui:" + tag
	}

	// Determine monitoring backend (uplot or grafana).
	monBackend := monitoring.BackendUPlot
	if v, settingsErr := settingsMgr.Get("monitoring_backend"); settingsErr == nil && v != "" {
		monBackend = v
	}

	// Discover the block devices backing the btrfs filesystem at btrfsPath
	// so the monitoring dashboards can sum node_disk_* metrics over only
	// those devices. Non-fatal: on failure the Disk I/O panel renders
	// empty rather than aggregating unrelated host disks.
	diskDevices, diskErr := monitoring.BtrfsDevices(*btrfsPath)
	if diskErr != nil {
		fmt.Fprintf(os.Stderr, "btrfs disk device discovery: %v\n", diskErr)
	}

	bs.Step("pull_images")
	// Pull container images in parallel (non-fatal). The NC image is
	// included here so the systemd units that reference it have a loaded
	// image ready before they start. Every package NC unit also includes
	// an ExecStartPre --pull=never network-create fallback, so a pull
	// failure here is recoverable on the next boot.
	coreImages := []string{
		ncImage,
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
	}
	if uiImage != "" {
		coreImages = append(coreImages, uiImage)
	}
	if monBackend == monitoring.BackendGrafana {
		coreImages = append(coreImages, monitoring.GrafanaImage)
	}
	parallelEnsureImages(ctx, coreImages)

	bs.Step("start_monitoring")
	// Start monitoring system services in parallel (all non-fatal). All three
	// run --net host: node-exporter and Prometheus bind 127.0.0.1 (private) and
	// the monitoring UI forwards the single LAN port :5308 to Prometheus over
	// the loopback — no podman networks, so concurrent starts cannot race.
	var monWG sync.WaitGroup
	monWG.Add(3)
	go func() {
		defer monWG.Done()
		if err := monitoring.StartNodeExporter(ctx, sd, ""); err != nil {
			fmt.Fprintf(os.Stderr, "node-exporter: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartPrometheus(ctx, sd, *btrfsPath, ""); err != nil {
			fmt.Fprintf(os.Stderr, "prometheus: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartMonitoringUI(ctx, sd, st, monBackend, *btrfsPath, ncImage, diskDevices); err != nil {
			fmt.Fprintf(os.Stderr, "monitoring-ui: %v\n", err)
		}
	}()
	monWG.Wait()

	// Install the nightly podman prune timer. Non-fatal: if the units
	// cannot be written, the system still boots — it just accumulates
	// image cruft until the next restart fixes it.
	if err := systemcontroller.InstallPodmanPruneUnits(ctx, sd); err != nil {
		fmt.Fprintf(os.Stderr, "podman prune timer: %v\n", err)
	}

	// Ensure the local TLS CA exists. This has to happen before reconcile
	// so reconcile can issue leaf certs for HTTP-supplying packages as it
	// walks installed units. The btrfs `tls` subvolume is created by
	// reconcile itself, but EnsureCA falls back to os.MkdirAll so the
	// path is usable even when the subvolume layout is not yet in place
	// (e.g. very first boot on a fresh btrfs).
	var tlsCA *townostls.CA
	if caVal, caErr := townostls.EnsureCA(filepath.Join(*btrfsPath, systemcontroller.TLSSubvolume)); caErr != nil {
		fmt.Fprintf(os.Stderr, "tls ca: %v\n", caErr)
	} else {
		tlsCA = caVal
	}

	// Ingress: the shared :443 SNI router. Started here as a system service and
	// programmed over gRPC (the reconcile_ingress step below, plus the
	// per-package and page-CRUD handlers). Skipped when INGRESS_IMAGE is
	// explicitly set to empty (dev mode).
	ingressImage := os.Getenv("INGRESS_IMAGE")
	if _, set := os.LookupEnv("INGRESS_IMAGE"); !set {
		ingressImage = "quay.io/town/ingress:" + tag
	}
	var ingressMgr *ingressctl.Manager
	if ingressImage != "" {
		ingressMgr = ingressctl.NewManager(ingressctl.Config{
			Systemd:    sd,
			DataDir:    filepath.Join(*btrfsPath, "ingress"),
			TLSHostDir: filepath.Join(*btrfsPath, systemcontroller.TLSSubvolume),
			Image:      ingressImage,
			// Serve dual-stack only when the host has a global IPv6 (otherwise
			// `podman network create --ipv6` fails and the unit won't start).
			EnableIPv6: getInternalIPv6() != "",
		})
		bs.Step("start_ingress")
		if startErr := ingressMgr.Start(ctx); startErr != nil {
			fmt.Fprintf(os.Stderr, "ingress: %v\n", startErr)
		}

		// Pages: a standalone Caddy static-file service the ingress
		// reverse-proxies to for every page FQDN. Started alongside the ingress.
		bs.Step("start_pages")
		if pErr := systemcontroller.StartPagesService(ctx, sd, *btrfsPath, systemcontroller.DefaultCaddyImage); pErr != nil {
			fmt.Fprintf(os.Stderr, "pages service: %v\n", pErr)
		}
	}

	// Detect whether the systemcontroller image changed since the last
	// run. When it has, reconcile will restart all units whose generated
	// content differs from what is on disk.
	versionFile := filepath.Join(*btrfsPath, "town-os-version")
	versionChanged := detectVersionChange(ctx, versionFile)

	bs.Step("reconcile")
	err = systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                st,
		Systemd:                sd,
		SettingsMgr:            settingsMgr,
		PagesManager:           pagesMgr,
		BtrfsBasePath:          *btrfsPath,
		NetworkControllerImage: ncImage,
		NetworkStatePath:       *networkStatePath,
		InternalIP:             getInternalIP(),
		VersionChanged:         versionChanged,
		TLSCA:                  tlsCA,
		Git:                    &git.GoGitClient{},
		PostUpdateExec: func(ctx context.Context, containerName string, command string) error {
			execCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			out, execErr := exec.CommandContext(execCtx, "podman", "exec", containerName, "sh", "-c", command).CombinedOutput() //nolint:gosec // G204 -- containerName and command from trusted package YAML
			if execErr != nil {
				return fmt.Errorf("%w: %s", execErr, string(out))
			}
			slog.Info(fmt.Sprintf("post-update %s: %s", containerName, strings.TrimSpace(string(out))))
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	// Persist current image SHA so the next startup can detect changes.
	persistVersion(ctx, versionFile)

	bs.Step("reconcile_dns")
	// Reconcile DNS state: set up TLD zone and register records for all
	// installed packages. This runs after rolodex is started so the gRPC
	// socket is available.
	{
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
			// Startup: wipe and rebuild rolodex so any drift from a crashed
			// or out-of-sync prior run is discarded. The hourly drift-repair
			// poller (ReconcileDNS) takes over once the HTTP server is up.
			dnsErr := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
				Client:           rolClient,
				Installer:        inst,
				RepositoryRoot:   rr,
				SettingsMgr:      settingsMgr,
				PagesManager:     pagesMgr,
				InternalIP:       getInternalIP(),
				InternalIPv6:     getInternalIPv6(),
				NetworkStatePath: *networkStatePath,
				BtrfsBasePath:    *btrfsPath,
			})
			if dnsErr != nil {
				fmt.Fprintf(os.Stderr, "rebuild DNS: %v\n", dnsErr)
			}
			if closeErr := rolClient.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close rolodex client: %v\n", closeErr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "reconcile DNS: could not connect to rolodex socket\n")
		}
	}

	// Program the shared :443 ingress with the full route set (HTTP packages +
	// pages), at the same point in boot as rolodex. Push/declarative: on a
	// fresh ingress this rebuilds everything (same model as RebuildDNS).
	if ingressMgr != nil {
		bs.Step("reconcile_ingress")
		if rdyErr := ingressMgr.WaitForReady(ctx); rdyErr != nil {
			fmt.Fprintf(os.Stderr, "ingress readiness: %v\n", rdyErr)
		}
		ic, dialErr := ingress.Dial(ctx, ingressMgr.SocketPath())
		if dialErr != nil {
			fmt.Fprintf(os.Stderr, "ingress dial: %v\n", dialErr)
		} else {
			if irErr := systemcontroller.RebuildIngress(ctx, ic, pagesMgr, inst, tlsCA, *btrfsPath, *networkStatePath, dnsTLD, getInternalIP()); irErr != nil {
				fmt.Fprintf(os.Stderr, "rebuild ingress: %v\n", irErr)
			}
			if closeErr := ic.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close ingress client: %v\n", closeErr)
			}
		}
	}

	bs.Step("start_ui_container")
	// Start the UI container (Caddy web server). Skipped when UI_IMAGE
	// is empty (dev mode — bun serves the UI directly).
	var uiMgr *ui.Manager
	if uiImage != "" {
		uiMgr = ui.NewManager(ui.Config{Systemd: sd, Image: uiImage})
		if err := uiMgr.Start(ctx); err != nil {
			// Non-fatal: UI failure should not prevent the system
			// controller from starting.
			fmt.Fprintf(os.Stderr, "ui: %v\n", err)
			uiMgr = nil
		}
	}

	// Freshness stage: if the refresh handler left a marker in the
	// previous process, restart every installed package unit serially
	// so the new systemcontroller sees freshly-started children.
	// Per-package events are emitted so the UI can render rolling
	// progress. A stale marker left by a crash is harmless (worst case
	// one extra restart cycle on next boot).
	if failed, freshErr := systemcontroller.RunFreshnessStage(ctx, bs, inst, sd, *btrfsPath); freshErr != nil {
		fmt.Fprintf(os.Stderr, "freshness stage: %v\n", freshErr)
	} else if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "freshness stage: %d package(s) failed to restart: %s\n",
			len(failed), strings.Join(failed, ", "))
	}

	bs.Step("build_handler")
	handler := systemcontroller.NewHandler(ctx, systemcontroller.ServerConfig{
		Storage:                    st,
		RepositoryRoot:             rr,
		Installer:                  inst,
		Systemd:                    sd,
		AccountMgr:                 acctMgr,
		SessionMgr:                 sessMgr,
		AuditMgr:                   auditMgr,
		SettingsMgr:                settingsMgr,
		PagesMgr:                   pagesMgr,
		DefaultRepoUser:            os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass:            os.Getenv(packages.EnvRepoPassword),
		BtrfsBasePath:              *btrfsPath,
		NetworkControllerImage:     ncImage,
		NetworkStatePath:           *networkStatePath,
		TLSCA:                      tlsCA,
		MonitoringBackend:          monBackend,
		DiskDevices:                diskDevices,
		Rolodex:                    rolMgr,
		Ingress:                    ingressMgr,
		UI:                         uiMgr,
		ResolvedConfigurator:       rolodex.ConfigureResolvedRouting,
		SystemControllerImage:      "quay.io/town/town:" + tag,
		SystemControllerListenAddr: *listenAddr,
	})

	// Atomically swap the root handler from the boot-status stub to the
	// full Echo router. The listener socket has been bound the entire
	// time, so no port flap occurs; SSE subscribers on /boot-status
	// survive the swap and keep streaming until the BootStatus stream
	// closes via Done below.
	rootHandler.Swap(handler)
	bs.Step("ready")
	bs.Done()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:govet // shadow is intentional for shutdown scope
		defer shutdownCancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", shutdownErr)
		}
	}()

	fmt.Fprintf(os.Stderr, "systemcontroller: listening on %s\n", *listenAddr)
	// The HTTP server was started in a goroutine at the top of run().
	// Block here until it exits (normal shutdown via SIGINT closes the
	// error channel with no value; a bind/listen failure sends a
	// non-nil error).
	if listenErr, ok := <-listenErrCh; ok && listenErr != nil {
		return fmt.Errorf("listen: %w", listenErr)
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

// getInternalIP returns the IPv4 address of the host's primary physical
// interface, or "" if none found. It delegates to the systemcontroller
// package's InternalInterfaceIPs so the boot reconcile and the runtime poller
// agree on which interface is authoritative for the host's address.
func getInternalIP() string {
	ipv4, _ := systemcontroller.InternalInterfaceIPs()
	return ipv4
}

// getInternalIPv6 returns the global IPv6 address of the same interface
// getInternalIP selects, or "" when the host has no globally routable IPv6.
// Used to publish AAAA records alongside the IPv4 A records.
func getInternalIPv6() string {
	_, ipv6 := systemcontroller.InternalInterfaceIPs()
	return ipv6
}

// getContainerImageID returns the image digest of the container this process
// is running inside, or an empty string if detection fails (e.g. not in a
// container). It reads the container ID from /run/.containerenv and inspects
// the image via podman.
func getContainerImageID(ctx context.Context) string {
	// Read container ID from /proc/1/cgroup (works in podman rootful).
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return ""
	}
	var containerID string
	for line := range strings.SplitSeq(string(data), "\n") {
		// cgroup v2: "0::/machine.slice/libpod-<id>.scope"
		if _, after, ok := strings.Cut(line, "libpod-"); ok {
			if dotIdx := strings.Index(after, "."); dotIdx > 0 {
				containerID = after[:dotIdx]
			}
		}
	}
	if containerID == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "podman", "inspect", "--format", "{{.Image}}", containerID).Output() //nolint:gosec // G204 -- containerID from /proc
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// detectVersionChange reads the persisted version file and returns true if
// the current image SHA differs (or if the file is missing/unreadable).
func detectVersionChange(ctx context.Context, versionFile string) bool {
	imageID := getContainerImageID(ctx)
	if imageID == "" {
		return false // not in a container or detection failed — skip
	}
	data, err := os.ReadFile(versionFile) //nolint:gosec // G304 -- versionFile from controlled btrfsPath flag
	if err != nil {
		return true // first run or unreadable → treat as changed
	}
	return strings.TrimSpace(string(data)) != imageID
}

// persistVersion writes the current container image SHA to the version file.
func persistVersion(ctx context.Context, versionFile string) {
	imageID := getContainerImageID(ctx)
	if imageID == "" {
		return
	}
	if err := os.WriteFile(versionFile, []byte(imageID+"\n"), 0600); err != nil {
		slog.Error(fmt.Sprintf("write version file: %v", err))
	}
}

// ensureImage checks whether a container image is loaded on the host's
// podman and pulls it from the registry when it is not. Every podman
// invocation picks up CONTAINER_HOST from the process environment (set
// by setupPodmanEnv at startup) so operations act on the host's image
// store via /run/podman/podman.sock instead of the systemcontroller
// container's isolated storage. This is a variable so tests can replace
// the implementation without requiring podman.
var ensureImage = func(ctx context.Context, image string) error {
	if err := exec.CommandContext(ctx, "podman", "image", "exists", image).Run(); err == nil { //nolint:gosec // G204 -- image from caller
		return nil // already loaded
	}
	out, pullErr := exec.CommandContext(ctx, "podman", "pull", image).CombinedOutput() //nolint:gosec // G204 -- image from caller
	if pullErr != nil {
		return fmt.Errorf("pull %s: %w: %s", image, pullErr, string(out))
	}
	return nil
}

// parallelEnsureImages runs ensureImage concurrently across the given
// image list with a bounded number of in-flight pulls. Pull failures are
// logged to stderr and never fatal — every caller treats the boot image
// set as best-effort. A channel-based semaphore bounds concurrency so a
// cold image cache cannot saturate the registry or podman socket.
func parallelEnsureImages(ctx context.Context, images []string) {
	const maxConcurrent = 3
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for _, img := range images {
		wg.Add(1)
		go func(img string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := ensureImage(ctx, img); err != nil {
				fmt.Fprintf(os.Stderr, "pull %s: %v\n", img, err)
			}
		}(img)
	}
	wg.Wait()
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
