package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	townostls "gitea.com/town-os/town-os/src/tls"
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

// run is the boot sequence. Each stage lives in boot.go and the order here is
// the order DESIGN.md documents; the stages that are non-fatal report on
// stderr and return nothing.
//
// The three fatal ones are the ones that make a serving box meaningless if
// they fail: no stores, no reconciled packages, no router.
func run() (err error) {
	b := &boot{}
	// One cleanup rather than four scattered defers, so the release order is
	// stated in one place. It runs on every path, including a failure before
	// anything was acquired.
	defer func() { err = errors.Join(err, b.close()) }()

	// The process-lifetime context. Created here rather than inside a stage so
	// it can be a parameter to each of them: every background goroutine the
	// boot starts exits through it, and close() cancels it first.
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	if err := b.start(); err != nil {
		return err
	}
	if err := b.openStores(ctx); err != nil {
		return err
	}
	if err := b.bootDNS(ctx); err != nil {
		return err
	}
	b.bootServices(ctx)
	if err := b.reconcile(ctx); err != nil {
		return err
	}
	b.reconcileDNSAndNetworks(ctx)
	b.programIngress(ctx)
	b.startUI(ctx)
	b.freshness(ctx)

	return b.serve(ctx)
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
// container).
//
// It is the same lookup the self-update stage does, so it is the same code:
// two answers to "which image am I" that could disagree is precisely how a box
// decides to restart for an upgrade it has already applied.
func getContainerImageID(ctx context.Context) string {
	return systemcontroller.RunningImageID(ctx)
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

// ensureImage makes a container image available on the host's podman before a
// unit that runs it starts: a pinned tag is fetched only when missing, a
// floating one (rc.latest-<arch> and the rest of the "latest" family) is
// re-pulled so a boot picks up what the tag points at now rather than what it
// pointed at the first time this box booted. See systemcontroller.EnsureImage
// for why that distinction is the whole point of the boot pull set.
//
// Every podman invocation picks up CONTAINER_HOST from the process environment
// (set by setupPodmanEnv at startup) so operations act on the host's image
// store via /run/podman/podman.sock instead of the systemcontroller container's
// isolated storage. This is a variable so tests can replace the implementation
// without requiring podman.
var ensureImage = systemcontroller.EnsureImage

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
func coreBootImages(ncImage, uiImage, gfehImage, ingressImage, monBackend string) []string {
	images := []string{
		ncImage,
		monitoring.PrometheusImage,
		monitoring.NodeExporterImage,
	}
	// The ingress belongs here for the same reason gfeh does, and was missing
	// for just as long: it is started at boot, its unit runs --pull=missing, and
	// on a cold box that means the ingress pulls itself inside `podman run`
	// while its own readiness wait counts down. The pages service runs on this
	// image too, so one omission stalls both.
	//
	// The monitoring UI needs no entry of its own: on the uPlot backend it runs
	// the NC image (monitoring.DefaultSocatImage), which is already first in
	// this list.
	for _, optional := range []string{uiImage, gfehImage, ingressImage} {
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
