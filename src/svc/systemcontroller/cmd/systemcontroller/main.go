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
	networkStatePath := flag.String("network-state", "/var/run/town-os", "directory for per-package network state files")
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

	// Derive rolodex image so we can start DNS before building the NC image.
	rolImage := os.Getenv("ROLODEX_IMAGE")
	if rolImage == "" {
		rolImage = "quay.io/town/rolodex:" + tag
	}

	// Pull the rolodex image only if it is not already loaded (e.g. pre-loaded
	// in test/dev containers where DNS may not be available yet).
	if err := ensureImage(ctx, rolImage); err != nil {
		fmt.Fprintf(os.Stderr, "pull %s: %v\n", rolImage, err)
	}

	// Start the rolodex DNS server before building the NC image so that
	// DNS is available for `apk add` inside the podman build.
	rolDataDir := filepath.Join(*btrfsPath, "rolodex")
	if err := os.MkdirAll(rolDataDir, 0750); err != nil {
		return fmt.Errorf("create rolodex data dir: %w", err)
	}
	rolLocal := os.Getenv("ROLODEX_LOCAL") != ""
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:         sd,
		DataDir:         rolDataDir,
		Image:           rolImage,
		Local:           rolLocal,
		UnixSocketPath:  filepath.Join(rolDataDir, "rolodex.sock"),
		ResolvConfPath:  "/etc/resolv.conf",
		ResolvedConfDir: "/etc/systemd/resolved.conf.d",
		PublicAddr:      getInternalIP(),
	})
	if err := rolMgr.Start(ctx); err != nil {
		// Non-fatal: rolodex failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "rolodex: %v\n", err)
		rolMgr = nil
	}

	// Build the network controller container image locally. This must
	// happen before reconciliation so the image is available when package
	// units are installed. Rolodex is started above so DNS works for
	// `apk add` inside the build.
	// Non-fatal: if the network is unavailable at boot the pull/build
	// will fail, but the system controller still starts. The image will
	// be built on the next restart once the network is up.
	ncImage, err := buildNetworkControllerImage(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "network controller image: %v\n", err)
	}
	if ncImage == "" {
		ncImage = "localhost/town-os-networkcontroller:local"
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

	// Derive UI image.
	uiImage := os.Getenv("UI_IMAGE")
	if uiImage == "" {
		uiImage = "quay.io/town/ui:" + tag
	}

	// Determine monitoring backend (uplot or grafana).
	monBackend := "uplot"
	if v, settingsErr := settingsMgr.Get("monitoring_backend"); settingsErr == nil && v != "" {
		monBackend = v
	}

	// Pull remaining system service container images (non-fatal).
	// Skip images that are already loaded (e.g. pre-loaded in test/dev
	// containers where DNS may not be available).
	coreImages := []string{
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
		uiImage,
	}
	if monBackend == "grafana" {
		coreImages = append(coreImages, monitoring.GrafanaImage)
	}
	for _, img := range coreImages {
		if err := ensureImage(ctx, img); err != nil {
			fmt.Fprintf(os.Stderr, "pull %s: %v\n", img, err)
		}
	}

	// Start the monitoring stack. When backend is "uplot", only Prometheus
	// and Node Exporter are started (Grafana is skipped, saving ~771 MB).
	monDataDir := filepath.Join(repoBase, "monitoring")
	monMgr := monitoring.NewManager(monitoring.Config{
		Systemd: sd,
		DataDir: monDataDir,
	})
	var monErr error
	if monBackend == "grafana" {
		monErr = monMgr.Start(ctx)
	} else {
		monErr = monMgr.StartCore(ctx)
	}
	if monErr != nil {
		// Non-fatal: monitoring failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "monitoring: %v\n", monErr)
		monMgr = nil
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

	handler := systemcontroller.NewHandler(ctx, systemcontroller.ServerConfig{
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
		BtrfsBasePath:          *btrfsPath,
		NetworkControllerImage: ncImage,
		NetworkStatePath:       *networkStatePath,
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

// ensureImage checks whether a container image is loaded locally and pulls it
// from the registry when it is not. This is a variable so tests can replace
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
// locally using podman build. The NC binary (/town-os-networkcontroller) is
// baked into the systemcontroller image at build time, so rebuilding on every
// startup guarantees the NC image always matches the running systemcontroller.
func buildNetworkControllerImage(ctx context.Context) (string, error) {
	const imageName = "localhost/town-os-networkcontroller:local"

	buildDir, err := os.MkdirTemp("", "nc-image-build-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
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
		return "", fmt.Errorf("write Containerfile: %w", err)
	}

	const ncBinaryPath = "/town-os-networkcontroller"
	destPath := filepath.Join(buildDir, "town-os-networkcontroller")

	src, err := os.Open(ncBinaryPath)
	if err != nil {
		return "", fmt.Errorf("open NC binary %s: %w", ncBinaryPath, err)
	}

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755) //nolint:gosec // G304 -- destPath is constructed from a controlled temp dir
	if err != nil {
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return "", fmt.Errorf("create NC binary copy: %w", err)
	}

	if _, err := dst.ReadFrom(src); err != nil {
		if closeErr := dst.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC dest: %v", closeErr))
		}
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return "", fmt.Errorf("copy NC binary: %w", err)
	}

	if err := dst.Close(); err != nil {
		if closeErr := src.Close(); closeErr != nil {
			slog.Error(fmt.Sprintf("close NC source: %v", closeErr))
		}
		return "", fmt.Errorf("close NC binary copy: %w", err)
	}

	if err := src.Close(); err != nil {
		return "", fmt.Errorf("close NC source: %w", err)
	}

	// Pull the base image only if it is not already loaded (e.g. pre-loaded
	// in test/dev containers). --pull=never in the build step below avoids a
	// second pull attempt.
	const baseImage = "docker.io/library/alpine:latest"
	if err := ensureImage(ctx, baseImage); err != nil {
		return "", err
	}

	out, err := exec.CommandContext(ctx, "podman", "build", "--pull=never", "-t", imageName, "-f", "Containerfile", buildDir).CombinedOutput() //nolint:gosec // G204 -- buildDir is a controlled temp path
	if err != nil {
		return "", fmt.Errorf("podman build: %w: %s", err, string(out))
	}

	slog.Info("built network controller image: " + imageName)

	return imageName, nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "systemcontroller: %v\n", err)
		os.Exit(1)
	}
}
