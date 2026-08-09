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

// defaultVersionTag is the default image tag: rc.latest for this host's
// architecture. rc tags are partitioned per architecture (rc.latest-x86_64 /
// rc.latest-aarch64, pushed natively from each host); archTag() maps the Go
// runtime arch to the registry tag suffix used on both supported architectures.
func defaultVersionTag() string {
	return "rc.latest-" + archTag()
}

// resolveImageTag returns the image tag used for the systemcontroller and every
// sibling image it pulls (UI, rolodex, network controller, ingress). It is
// rc.latest-<arch> by default, so a system update always pulls the newest
// images; the install image build system pins a specific tag by setting the
// TOWN_OS_TAG env var on the systemcontroller systemd unit. The former
// compile-time main.Version pin and the /town-os.tag file were removed because a
// stale value in either one silently held every sibling image back on an old tag
// even after the controller itself advanced.
func resolveImageTag() string {
	if tag := strings.TrimSpace(os.Getenv("TOWN_OS_TAG")); tag != "" {
		return tag
	}
	return defaultVersionTag()
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

// gfehPublishConfig is what publishGfehNames needs to re-derive the record and
// route sets once the partitions are answering.
type gfehPublishConfig struct {
	Registry         systemcontroller.GfehRegistry
	Rolodex          *rolodex.Manager
	Ingress          *ingressctl.Manager
	Installer        *packages.InstallManager
	RepositoryRoot   *packages.RepositoryRoot
	PagesMgr         account.PagesManager
	NetworkMgr       account.NetworkManager
	SettingsMgr      account.SettingsManager
	TLSCA            *townostls.CA
	BtrfsBasePath    string
	NetworkStatePath string
	TLD              string
}

// publishGfehNames waits for the object-storage partitions to come up, then
// folds their names into DNS and the ingress.
//
// Runs after the handler swap because that is the earliest a partition can
// finish starting: gfehd polls /status/ping, which answers 503 until the full
// router is live. Everything DNS-related in the boot sequence has already run
// by then, so without this a partition's names would first appear on the hourly
// reconcile.
//
// ReconcileDNS rather than RebuildDNS: this is an incremental add to a zone
// that is already serving, and tearing it down would blip every package and
// page on the box to publish some object-storage records.
//
// Entirely best-effort. A partition that never comes up costs its own names and
// nothing else.
func publishGfehNames(ctx context.Context, cfg gfehPublishConfig) {
	if !waitForGfehPartitions(ctx, cfg.Registry) {
		fmt.Fprintf(os.Stderr, "gfeh: no partition became ready; its names will be published by the next reconcile\n")
		return
	}

	dnsCfg := systemcontroller.ReconcileDNSConfig{
		Installer:        cfg.Installer,
		RepositoryRoot:   cfg.RepositoryRoot,
		SettingsMgr:      cfg.SettingsMgr,
		PagesManager:     cfg.PagesMgr,
		NetworkMgr:       cfg.NetworkMgr,
		InternalIP:       getInternalIP(),
		InternalIPv6:     getInternalIPv6(),
		NetworkStatePath: cfg.NetworkStatePath,
		BtrfsBasePath:    cfg.BtrfsBasePath,
		Gfeh:             cfg.Registry,
	}

	if cfg.Rolodex != nil {
		rolClient, dialErr := rolodex.Dial(ctx, cfg.Rolodex.SocketPath())
		if dialErr != nil {
			fmt.Fprintf(os.Stderr, "gfeh: dial rolodex: %v\n", dialErr)
		} else {
			dnsCfg.Client = rolClient
			if err := systemcontroller.ReconcileDNS(ctx, dnsCfg); err != nil {
				fmt.Fprintf(os.Stderr, "gfeh: reconcile DNS: %v\n", err)
			}
			if err := systemcontroller.RebuildNetworkDNS(ctx, dnsCfg); err != nil {
				fmt.Fprintf(os.Stderr, "gfeh: rebuild network DNS: %v\n", err)
			}
			if closeErr := rolClient.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "gfeh: close rolodex client: %v\n", closeErr)
			}
		}
	}

	if cfg.Ingress != nil {
		ic, dialErr := ingress.Dial(ctx, cfg.Ingress.SocketPath())
		if dialErr != nil {
			fmt.Fprintf(os.Stderr, "gfeh: dial ingress: %v\n", dialErr)
			return
		}
		if err := systemcontroller.RebuildIngress(ctx, ic, cfg.PagesMgr, cfg.NetworkMgr,
			cfg.Installer, cfg.Registry, cfg.TLSCA, cfg.BtrfsBasePath, cfg.NetworkStatePath,
			cfg.TLD, getInternalIP()); err != nil {
			fmt.Fprintf(os.Stderr, "gfeh: rebuild ingress: %v\n", err)
		}
		if closeErr := ic.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "gfeh: close ingress client: %v\n", closeErr)
		}
	}
}

// waitForGfehPartitions blocks until at least one partition answers, or the
// deadline passes.
//
// At least one rather than all: a box with several networks should publish the
// partitions that did come up rather than withhold everything because one
// did not. The deadline is longer than gfehd's own startup because it includes
// provisioning -- the daemon authenticates, creates or resizes its subvolume,
// and opens its index before it binds the admin socket.
func waitForGfehPartitions(ctx context.Context, reg systemcontroller.GfehRegistry) bool {
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	for {
		for _, client := range reg.Clients() {
			if _, err := client.Health(waitCtx); err == nil {
				return true
			}
		}
		select {
		case <-waitCtx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
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

	// TLS, when the operator asked for it, has to be resolved BEFORE the bind:
	// the boot-status stream is the first thing a UI connects to, and a
	// listener that starts as HTTP and becomes HTTPS partway through boot would
	// break exactly the client that is watching the boot. Everything this needs
	// is local and fast — a directory, a P-256 keypair, an idempotent leaf —
	// so it costs the observable-boot bind nothing measurable.
	//
	// Fatal rather than a fall back to plain HTTP: an operator who asked for
	// TLS and silently got cleartext is worse off than one whose box refuses to
	// start and says why.
	if systemcontroller.ControllerTLSRequested() {
		if *btrfsPath != "" {
			if mkErr := os.MkdirAll(*btrfsPath, 0750); mkErr != nil {
				return fmt.Errorf("create btrfs base for TLS: %w", mkErr)
			}
		}
		tlsCfg, tlsErr := systemcontroller.ControllerTLSConfig(*btrfsPath, systemcontroller.ListenAddrSANs(*listenAddr))
		if tlsErr != nil {
			return fmt.Errorf("configure controller TLS: %w", tlsErr)
		}
		srv.TLSConfig = tlsCfg
	}

	listenErrCh := make(chan error, 1)
	go func() {
		var listenErr error
		if srv.TLSConfig != nil {
			// Cert and key are already in TLSConfig.Certificates.
			listenErr = srv.ListenAndServeTLS("", "")
		} else {
			listenErr = srv.ListenAndServe()
		}
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			listenErrCh <- listenErr
		}
		close(listenErrCh)
	}()

	bs.Step("boot_controller")
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

	networkMgr, err := account.InitNetworkManager(db)
	if err != nil {
		return fmt.Errorf("init network manager: %w", err)
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

	// The image tag for the systemcontroller and every sibling image (UI,
	// rolodex, networkcontroller, ingress). rc.latest-<arch> by default; the
	// install build system pins a specific tag via the TOWN_OS_TAG env var on
	// the systemcontroller unit.
	tag := resolveImageTag()

	// Network controller image: pulled from quay.io like every other
	// sibling image. NC_IMAGE overrides the derived default (the test and dev
	// harnesses inject localhost/town-os-networkcontroller:<instance-id>,
	// built on the host by the nc-image / nc-image-dev targets).
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

	bs.Step("boot_dns")
	rolDataDir := filepath.Join(*btrfsPath, "rolodex")
	if err := os.MkdirAll(rolDataDir, 0750); err != nil {
		return fmt.Errorf("create rolodex data dir: %w", err)
	}
	// The resolution mode is a user-facing setting: "auto" (the default: roots,
	// then DoH/DoT, then the forwarders, then a public resolver), "recursive"
	// (roots only, no fallback), or "forward" (straight to the forwarders). An
	// invalid stored value is ignored so a bad setting can never render a
	// rolodex.yml that rolodex refuses to start with.
	resolutionMode := rolodex.DefaultResolutionMode
	if v, modeErr := settingsMgr.Get("dns_resolution_mode"); modeErr == nil && rolodex.ValidResolutionMode(v) {
		resolutionMode = v
	}

	// Empty (the normal case) means rolodex.DefaultDNSPort — see ports.go for
	// why the integration harness relocates it.
	dnsPort := dnsPortFromEnv()

	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        rolDataDir,
		Image:          rolImage,
		UnixSocketPath: filepath.Join(rolDataDir, "rolodex.sock"),
		ResolutionMode: resolutionMode,
		DNSPort:        dnsPort,
		MetricsPort:    rolodexMetricsPortFromEnv(),
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
	// resolved can only route a domain to a resolver on :53, so this is skipped
	// entirely when rolodex has been relocated off the standard port. Pointing
	// resolved at DNSLoopback in that case would blackhole every .tld query.
	if dnsPortIsDefault(dnsPort) {
		rolodex.ConfigureResolvedRouting(ctx, dnsTLD, rolodex.DNSLoopback)
	} else {
		fmt.Fprintf(os.Stderr, "rolodex DNS on non-standard port %s; skipping systemd-resolved routing\n", dnsPort)
	}

	// Derive UI image. When UI_IMAGE is explicitly empty the UI container
	// is skipped entirely (useful in dev where bun serves the UI).
	uiImage := os.Getenv("UI_IMAGE")
	if _, uiSet := os.LookupEnv("UI_IMAGE"); !uiSet {
		uiImage = "quay.io/town/ui:" + tag
	}

	// Object storage: one gfeh partition per network, each its own daemon.
	//
	// GFEH_IMAGE explicitly empty skips it entirely (dev mode), the same
	// LookupEnv convention UI_IMAGE and INGRESS_IMAGE use — Getenv would make
	// an empty value mean "use the default" and there would be no off switch.
	//
	// Resolved here, well above the partitions themselves, for one reason: the
	// image has to be in the boot pull set below. It was not, and it is the only
	// system-service image that was not. On a cold box that meant the very first
	// `podman run` in the unit did the pull — a Rust daemon's worth of layers —
	// while the socket-readiness wait counted down, expired, and left object
	// storage reading as down for the rest of the boot.
	gfehImage := os.Getenv("GFEH_IMAGE")
	if _, gfehSet := os.LookupEnv("GFEH_IMAGE"); !gfehSet {
		gfehImage = "quay.io/town/gfeh:" + tag
	}

	// Host ports for the three monitoring system services. The zero value means
	// the production defaults; the harness relocates them (see ports.go).
	monPorts := monitoringPortsFromEnv()
	// Scrape rolodex alongside node-exporter. The address comes from the
	// rolodex manager rather than being recomposed here, so the Prometheus
	// target is by construction the same string rolodex.yml binds — the same
	// single-source-of-truth reason PackageNetworkState.FQDN exists.
	monPorts.RolodexMetrics = rolMgr.MetricsAddr()
	// Scrape the controller's own /metrics, derived from the same -listen value
	// the server binds so the target cannot drift from the listener — and so a
	// relocated harness instance scrapes itself rather than whichever process
	// happens to hold the default port.
	monPorts.ControllerMetrics = systemcontroller.MetricsScrapeTarget(*listenAddr)
	if srv.TLSConfig != nil {
		monPorts.ControllerMetricsScheme = "https"
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

	bs.Step("boot_services")
	parallelEnsureImages(ctx, coreBootImages(ncImage, uiImage, gfehImage, monBackend))

	// Tear down obsolete monitoring units from the previous (NC + socket)
	// design before starting the new host-net services. On an in-place
	// upgrade the leftover NC containers still hold -p 9090:9090 / -p
	// 5308:5308, so Prometheus and the socat would crash-loop with
	// "address already in use" until these are removed. Best-effort and
	// idempotent (a no-op on fresh installs).
	monitoring.CleanupLegacyMonitoringUnits(ctx, sd)

	// Start monitoring system services in parallel (all non-fatal). All three
	// run --net host: node-exporter and Prometheus bind 127.0.0.1 (private) and
	// the monitoring UI forwards the single LAN port :5308 to Prometheus over
	// the loopback — no podman networks, so concurrent starts cannot race.
	var monWG sync.WaitGroup
	monWG.Add(3)
	go func() {
		defer monWG.Done()
		if err := monitoring.StartNodeExporter(ctx, sd, monPorts); err != nil {
			fmt.Fprintf(os.Stderr, "node-exporter: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartPrometheus(ctx, sd, *btrfsPath, monPorts); err != nil {
			fmt.Fprintf(os.Stderr, "prometheus: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartMonitoringUI(ctx, sd, st, monBackend, *btrfsPath, ncImage, diskDevices, monPorts); err != nil {
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
			// Zero means the privileged production ports (443/80); the harness
			// relocates them so a test ingress cannot collide with a dev or
			// production ingress in the shared host netns (see ports.go).
			HostPort:     envPortInt(EnvIngressHTTPSPort),
			HTTPHostPort: envPortInt(EnvIngressHTTPPort),
		})
		if startErr := ingressMgr.Start(ctx); startErr != nil {
			fmt.Fprintf(os.Stderr, "ingress: %v\n", startErr)
		}

		// Pages: a standalone Caddy static-file service the ingress
		// reverse-proxies to for every page FQDN. Started alongside the ingress.
		if pErr := systemcontroller.StartPagesService(ctx, sd, *btrfsPath, systemcontroller.DefaultCaddyImage); pErr != nil {
			fmt.Fprintf(os.Stderr, "pages service: %v\n", pErr)
		}
	}

	// The partitions themselves. gfehImage was resolved above so it could join
	// the boot pull set; by here the image is local and the units start against
	// it rather than against a registry.
	var gfehReg systemcontroller.GfehRegistry
	if gfehImage != "" {
		// Gated on the ingress: the four HTTP views publish no host port and
		// are reachable only through it, so starting partitions with no
		// ingress would produce four names per network that nothing serves.
		if ingressMgr == nil {
			fmt.Fprintf(os.Stderr, "gfeh: skipped, the ingress is disabled and the HTTP views are only reachable through it\n")
		} else {
			reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
				NetworkMgr:    networkMgr,
				Storage:       st,
				Systemd:       sd,
				SettingsMgr:   settingsMgr,
				BtrfsBasePath: *btrfsPath,
				Image:         gfehImage,
			})
			systemcontroller.ReconcileGfeh(ctx, reg)
			gfehReg = reg
		}
	}

	// Detect whether the systemcontroller image changed since the last
	// run. When it has, reconcile will restart all units whose generated
	// content differs from what is on disk.
	versionFile := filepath.Join(*btrfsPath, "town-os-version")
	versionChanged := detectVersionChange(ctx, versionFile)

	err = systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                st,
		Systemd:                sd,
		SettingsMgr:            settingsMgr,
		NetworkMgr:             networkMgr,
		PagesManager:           pagesMgr,
		BtrfsBasePath:          *btrfsPath,
		NetworkControllerImage: ncImage,
		NetworkStatePath:       *networkStatePath,
		InternalIP:             getInternalIP(),
		VersionChanged:         versionChanged,
		TLSCA:                  tlsCA,
		Gfeh:                   gfehReg,
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
			dnsCfg := systemcontroller.ReconcileDNSConfig{
				Client:           rolClient,
				Installer:        inst,
				RepositoryRoot:   rr,
				SettingsMgr:      settingsMgr,
				PagesManager:     pagesMgr,
				NetworkMgr:       networkMgr,
				InternalIP:       getInternalIP(),
				InternalIPv6:     getInternalIPv6(),
				NetworkStatePath: *networkStatePath,
				BtrfsBasePath:    *btrfsPath,
				Gfeh:             gfehReg,
			}
			if dnsErr := systemcontroller.RebuildDNS(ctx, dnsCfg); dnsErr != nil {
				fmt.Fprintf(os.Stderr, "rebuild DNS: %v\n", dnsErr)
			}
			// LAN-facing global records for non-default networks. The
			// scoped/overlay records are established at install and persist; this
			// makes each network package resolvable from loopback/LAN too (a bare
			// global A record, LAN-resolvable via rolodex's owning-scope fallback).
			if netDNSErr := systemcontroller.RebuildNetworkDNS(ctx, dnsCfg); netDNSErr != nil {
				fmt.Fprintf(os.Stderr, "rebuild network DNS: %v\n", netDNSErr)
			}
		} else {
			fmt.Fprintf(os.Stderr, "reconcile DNS: could not connect to rolodex socket\n")
		}

		// Ensure the default network exists and bring up every enabled network's
		// WireGuard interface. Pass the rolodex client (may be nil) so the boot
		// reconcile owns each network's TLD scope in rolodex — including the
		// default/home TLD, which must be an owned scope so it is partitioned away
		// from (hidden from) every WireGuard peer while staying LAN-only. The
		// default network gets no WireGuard transport. Non-fatal: a WG failure
		// must not block boot.
		systemcontroller.ReconcileNetworks(ctx, systemcontroller.ReconcileNetworksConfig{
			NetworkMgr:       networkMgr,
			Systemd:          sd,
			NetworkStatePath: *networkStatePath,
			SettingsMgr:      settingsMgr,
			RolodexClient:    rolClient,
		})

		if rolClient != nil {
			if closeErr := rolClient.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close rolodex client: %v\n", closeErr)
			}
		}
	}

	// Object storage again, now that the networks exist.
	//
	// A partition is per network, and the earlier pass runs BEFORE
	// ReconcileNetworks — which is what creates the default network on a box
	// that has never booted. So on a first boot the earlier pass saw an empty
	// network list, provisioned nothing, and object storage stayed off until
	// some later restart happened to find the network already there. Running it
	// again here is idempotent (an unchanged partition is left alone rather
	// than bounced) and cannot be replaced by moving the first pass, which has
	// to precede Reconcile so a package's names and the partitions' names are
	// derived in one go.
	systemcontroller.ReconcileGfehRegistry(ctx, gfehReg)

	// Program the shared :443 ingress with the full route set (HTTP packages +
	// pages), at the same point in boot as rolodex. Push/declarative: on a
	// fresh ingress this rebuilds everything (same model as RebuildDNS).
	if ingressMgr != nil {
		if rdyErr := ingressMgr.WaitForReady(ctx); rdyErr != nil {
			fmt.Fprintf(os.Stderr, "ingress readiness: %v\n", rdyErr)
		}
		ic, dialErr := ingress.Dial(ctx, ingressMgr.SocketPath())
		if dialErr != nil {
			fmt.Fprintf(os.Stderr, "ingress dial: %v\n", dialErr)
		} else {
			if irErr := systemcontroller.RebuildIngress(ctx, ic, pagesMgr, networkMgr, inst, gfehReg, tlsCA, *btrfsPath, *networkStatePath, dnsTLD, getInternalIP()); irErr != nil {
				fmt.Fprintf(os.Stderr, "rebuild ingress: %v\n", irErr)
			}
			if closeErr := ic.Close(); closeErr != nil {
				fmt.Fprintf(os.Stderr, "close ingress client: %v\n", closeErr)
			}
		}
	}

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
		NetworkMgr:                 networkMgr,
		DefaultRepoUser:            os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass:            os.Getenv(packages.EnvRepoPassword),
		BtrfsBasePath:              *btrfsPath,
		NetworkControllerImage:     ncImage,
		NetworkStatePath:           *networkStatePath,
		TLSCA:                      tlsCA,
		MonitoringBackend:          monBackend,
		DiskDevices:                diskDevices,
		MonitoringPorts:            monPorts,
		Rolodex:                    rolMgr,
		Ingress:                    ingressMgr,
		UI:                         uiMgr,
		GfehRegistry:               gfehReg,
		ResolvedConfigurator:       rolodex.ConfigureResolvedRouting,
		SystemControllerImage:      "quay.io/town/town:" + tag,
		SystemControllerListenAddr: *listenAddr,
		// Same id the boot stub reported on /status/ping, so a refresh
		// client watching across the restart sees one continuous identity
		// per process and can tell this incarnation from its predecessor.
		BootID: bs.BootID(),
	})

	// Atomically swap the root handler from the boot-status stub to the
	// full Echo router. The listener socket has been bound the entire
	// time, so no port flap occurs; SSE subscribers on /boot-status
	// survive the swap and keep streaming until the BootStatus stream
	// closes via Done below.
	rootHandler.Swap(handler)
	bs.Step("ready")
	bs.Done()

	// Object storage publishes its names here, after the swap, and not with
	// the rest of the DNS and ingress work above.
	//
	// The ordering is forced: each partition's gfehd waits for /status/ping to
	// stop answering 503 before it authenticates and opens its partition, and
	// that only happens on the line above. Asking it for GET /v1/names any
	// earlier gets nothing, and RebuildDNS having already run means nothing
	// would republish them until the hourly reconcile an hour later.
	//
	// Backgrounded so a partition that never comes up cannot hold the process
	// short of serving requests -- the router is already live at this point.
	if gfehReg != nil {
		go publishGfehNames(ctx, gfehPublishConfig{
			Registry:         gfehReg,
			Rolodex:          rolMgr,
			Ingress:          ingressMgr,
			Installer:        inst,
			RepositoryRoot:   rr,
			PagesMgr:         pagesMgr,
			NetworkMgr:       networkMgr,
			SettingsMgr:      settingsMgr,
			TLSCA:            tlsCA,
			BtrfsBasePath:    *btrfsPath,
			NetworkStatePath: *networkStatePath,
			TLD:              dnsTLD,
		})
	}

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

// coreBootImages is the set of container images pulled before any system
// service starts.
//
// Every image a boot-time unit references belongs here, and the reason is
// ordering, not speed: a unit whose image is not local does the pull itself
// inside `podman run`, which means the readiness wait that follows is racing a
// registry download. That is precisely how object storage came to be reliably
// down on a cold boot — gfeh was the one system-service image missing from this
// list, so its daemon started by pulling a Rust binary's worth of layers while
// the socket wait timed out under it.
//
// An empty string means the service is disabled for this build (the LookupEnv
// convention UI_IMAGE, INGRESS_IMAGE and GFEH_IMAGE share), and there is
// nothing to pull for something that will not run.
func coreBootImages(ncImage, uiImage, gfehImage, monBackend string) []string {
	images := []string{
		ncImage,
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
	}
	for _, optional := range []string{uiImage, gfehImage} {
		if optional != "" {
			images = append(images, optional)
		}
	}
	// Grafana is ~771 MB and only one of the two monitoring backends, so it is
	// pulled only when it is the selected one.
	if monBackend == monitoring.BackendGrafana {
		images = append(images, monitoring.GrafanaImage)
	}
	return images
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
