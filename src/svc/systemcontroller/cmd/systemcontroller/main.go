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
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
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

	var pagesMgr account.PagesManager
	if os.Getenv("TOWN_OS_PAGES") != "" {
		pagesMgr, err = account.InitPagesManager(db)
		if err != nil {
			return fmt.Errorf("init pages manager: %w", err)
		}
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
	// matching tags for sibling images (UI, rolodex, networkcontroller) at
	// runtime. Fallback chain: TOWN_OS_TAG env var → compile-time Version
	// ldflags → /town-os.tag file → "rc.latest".
	tag := "rc.latest"
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

	// Start monitoring system services in parallel (all non-fatal). NC
	// image is already loaded above, so the per-service NC units can come
	// up without a race. Each StartX encapsulates its own NC unit install
	// and ordering, so concurrent calls do not interfere.
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
		if err := monitoring.StartPrometheus(ctx, sd, *btrfsPath, "", ncImage, *networkStatePath); err != nil {
			fmt.Fprintf(os.Stderr, "prometheus: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartMonitoringUI(ctx, sd, st, monBackend, *btrfsPath, ncImage, *networkStatePath, diskDevices); err != nil {
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
		PagesManager:           pagesMgr,
		BtrfsBasePath:          *btrfsPath,
		NetworkControllerImage: ncImage,
		NetworkStatePath:       *networkStatePath,
		InternalIP:             getInternalIP(),
		VersionChanged:         versionChanged,
		TLSCA:                  tlsCA,
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
			dnsErr := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
				Client:         rolClient,
				Installer:      inst,
				RepositoryRoot: rr,
				SettingsMgr:    settingsMgr,
				InternalIP:     getInternalIP(),
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
		UI:                         uiMgr,
		ResolvedConfigurator:       rolodex.ConfigureResolvedRouting,
		SystemControllerImage:      "quay.io/town/town:" + tag,
		SystemControllerListenAddr: *listenAddr,
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
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:govet // shadow is intentional for shutdown scope
		defer shutdownCancel()
		shutdownErr := srv.Shutdown(shutdownCtx)
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

// getInternalIP returns the first non-loopback IPv4 address on a physical
// network interface, or "" if none found. Virtual interfaces (podman, veth,
// cni, docker, br-, virbr, tailscale) are skipped to avoid returning container
// bridge addresses like 10.88.0.1.
func getInternalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if isVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

// isVirtualInterface returns true for interface names that belong to container
// runtimes, virtual bridges, or VPN tunnels.
func isVirtualInterface(name string) bool {
	for _, prefix := range []string{
		"podman", "veth", "cni", "docker", "br-", "virbr", "tailscale",
	} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
