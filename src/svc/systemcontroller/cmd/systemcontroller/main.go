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

// HostPodmanSocket is the unix socket URL of the host podman that the
// systemcontroller container bind-mounts in at /run/podman/podman.sock.
// Set into the CONTAINER_HOST env var at startup so every podman
// invocation from this process (and any child process it forks)
// automatically routes through the host socket instead of landing in
// the systemcontroller container's isolated podman storage.
const HostPodmanSocket = "unix:///run/podman/podman.sock"

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
	networkStatePath := flag.String("network-state", "/var/run/town-os", "directory for per-package network state files")
	listenAddr := flag.String("listen", ":5309", "address to listen on")
	flag.Parse()

	if *listenAddr == ":5309" {
		if env := os.Getenv("TOWN_OS_LISTEN"); env != "" {
			*listenAddr = env
		}
	}

	const ncImage = "localhost/town-os-networkcontroller:local"

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

	// Build the network controller image on the host as the very first
	// runtime operation. All system services and package NC units run on
	// the host's podman, so the image must exist in the host's image
	// store. The build uses --dns=8.8.8.8 so it does not depend on
	// rolodex or any other service being ready.
	// Non-fatal: if the network is unavailable the build will fail, but
	// the system controller still starts. The image will be built on the
	// next restart once the network is up.
	{
		buildCtx, buildCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		buildErr := buildNetworkControllerImage(buildCtx)
		buildCancel()
		if buildErr != nil {
			fmt.Fprintf(os.Stderr, "network controller image: %v\n", buildErr)
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

	// Derive UI image.
	uiImage := os.Getenv("UI_IMAGE")
	if uiImage == "" {
		uiImage = "quay.io/town/ui:" + tag
	}

	// Determine monitoring backend (uplot or grafana).
	monBackend := monitoring.BackendUPlot
	if v, settingsErr := settingsMgr.Get("monitoring_backend"); settingsErr == nil && v != "" {
		monBackend = v
	}

	// Pull container images (non-fatal).
	coreImages := []string{
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
		uiImage,
	}
	if monBackend == monitoring.BackendGrafana {
		coreImages = append(coreImages, monitoring.GrafanaImage)
	}
	for _, img := range coreImages {
		if err := ensureImage(ctx, img); err != nil {
			fmt.Fprintf(os.Stderr, "pull %s: %v\n", img, err)
		}
	}

	// Start monitoring system services (all non-fatal).
	if err := monitoring.StartNodeExporter(ctx, sd, ""); err != nil {
		fmt.Fprintf(os.Stderr, "node-exporter: %v\n", err)
	}
	if err := monitoring.StartPrometheus(ctx, sd, *btrfsPath, "", ncImage, *networkStatePath); err != nil {
		fmt.Fprintf(os.Stderr, "prometheus: %v\n", err)
	}
	if err := monitoring.StartMonitoringUI(ctx, sd, monBackend, *btrfsPath, ncImage, *networkStatePath); err != nil {
		fmt.Fprintf(os.Stderr, "monitoring-ui: %v\n", err)
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
		MonitoringBackend:          monBackend,
		Rolodex:                    rolMgr,
		UI:                         uiMgr,
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

// buildNetworkControllerImage builds the network controller container image
// on the host's podman via the CONTAINER_HOST socket (set at startup by
// setupPodmanEnv). The NC binary (/town-os-networkcontroller) is baked
// into the systemcontroller image at build time, so rebuilding on every
// startup guarantees the NC image always matches the running
// systemcontroller. After the build completes, the function re-checks
// that the image is visible to host podman and returns an error if it
// is not — turning a silent mis-routed build into a loud boot-time
// failure.
func buildNetworkControllerImage(ctx context.Context) error {
	const imageName = "localhost/town-os-networkcontroller:local"

	buildDir, err := os.MkdirTemp("", "nc-image-build-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() {
		if rerr := os.RemoveAll(buildDir); rerr != nil {
			slog.Error(fmt.Sprintf("cleanup NC build dir: %v", rerr))
		}
	}()

	containerfile := `FROM docker.io/library/alpine:latest
RUN apk add --no-cache socat
COPY town-os-networkcontroller /town-os-networkcontroller
CMD ["/town-os-networkcontroller"]
`
	if err := os.WriteFile(filepath.Join(buildDir, "Containerfile"), []byte(containerfile), 0600); err != nil {
		return fmt.Errorf("write Containerfile: %w", err)
	}

	const ncBinaryPath = "/town-os-networkcontroller"
	destPath := filepath.Join(buildDir, "town-os-networkcontroller")

	src, err := os.Open(ncBinaryPath)
	if err != nil {
		return fmt.Errorf("open NC binary %s: %w", ncBinaryPath, err)
	}

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755) //nolint:gosec // G304 -- destPath is constructed from a controlled temp dir
	if err != nil {
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return fmt.Errorf("create NC binary copy: %w", err)
	}

	if _, err := dst.ReadFrom(src); err != nil {
		if closeErr := dst.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC dest: %v", closeErr))
		}
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return fmt.Errorf("copy NC binary: %w", err)
	}

	if err := dst.Close(); err != nil {
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return fmt.Errorf("close NC binary copy: %w", err)
	}

	if err := src.Close(); err != nil {
		return fmt.Errorf("close NC source: %w", err)
	}

	// Pull the base image on the host only if it is not already loaded.
	// --pull=never in the build step below avoids a second pull attempt.
	const baseImage = "docker.io/library/alpine:latest"
	if err := ensureImage(ctx, baseImage); err != nil {
		return err
	}

	out, err := exec.CommandContext(ctx, "podman", "build", "--pull=never", "--dns=8.8.8.8", "-t", imageName, "-f", "Containerfile", buildDir).CombinedOutput() //nolint:gosec // G204 -- buildDir is a controlled temp path
	if err != nil {
		return fmt.Errorf("podman build: %w: %s", err, string(out))
	}

	// Verify the image actually landed on the host's podman. Catches the
	// silent failure mode where CONTAINER_HOST is unset or the socket
	// bind mount is missing: the build would succeed against whatever
	// local storage the container happens to have and the image would
	// be invisible to the host systemd units that reference it.
	if existsErr := exec.CommandContext(ctx, "podman", "image", "exists", imageName).Run(); existsErr != nil { //nolint:gosec // G204 -- imageName is a constant
		return fmt.Errorf("NC image %s not visible to host podman after build (CONTAINER_HOST unset or socket bind mount missing?): %w", imageName, existsErr)
	}

	slog.Info("built network controller image: " + imageName)

	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
