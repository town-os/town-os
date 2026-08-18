// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/i18n"
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

// boot carries the state the startup sequence accumulates.
//
// The sequence used to be one 752-line function. It is linear and heavily
// commented, so it was never hard to *read* — but it had no seam: the thirty
// steps DESIGN.md documents were reachable only by booting a whole box, and
// nothing could be exercised, reordered, or reasoned about in isolation.
//
// The split follows the bs.Step() boundaries exactly, because those are
// already the stages the sequence is described in — to the operator watching a
// self-update, and in DESIGN.md. No step moved, no ordering changed, and every
// comment is the one that was there: the ordering constraints they record are
// the whole reason this sequence is fragile (TLS resolved before the bind;
// rolodex programmed only after its interface is up; gfeh names published only
// after the handler swap). Read a reordering of this file as a bug.
//
// Fields are grouped by the stage that populates them, so where a value comes
// from is answerable without searching.
type boot struct {
	// Flags, dereferenced once at parse so no stage carries a *string.
	dbPath           string
	btrfsPath        string
	repoDir          string
	networkStatePath string
	listenAddr       string

	// The observable boot: :5309 is bound before any work happens, answering
	// from a stub that streams progress, and the full router is swapped in at
	// the end without the listener ever closing.
	bs          *systemcontroller.BootStatus
	rootHandler *systemcontroller.RootHandler
	srv         *http.Server
	listenErrCh chan error

	// Process lifetime. The context itself is threaded through the stages as a
	// parameter rather than parked here — a struct-held context outlives the
	// call it belongs to and hides which stage is actually cancellable. Only
	// the cancel func is retained, because close() and the signal handler both
	// need it.
	cancel context.CancelFunc
	tmpDir string

	// Persistent state. Concrete types rather than the interfaces, matching
	// the locals these replaced: several consumers (gfehPublishConfig among
	// them) take the concrete type, and widening here would only push a type
	// assertion down into a caller.
	db          *sql.DB
	acctMgr     *account.SQLiteManager
	sessMgr     *account.SQLiteSessionManager
	auditMgr    *account.SQLiteAuditManager
	settingsMgr *account.SQLiteSettingsManager
	pagesMgr    *account.SQLitePagesManager
	networkMgr  *account.SQLiteNetworkManager

	// Packages and the platform they run on.
	repoBase string
	rr       *packages.RepositoryRoot
	inst     *packages.InstallManager
	st       *storage.BtrFS
	sd       *systemd.SystemdManager

	// Every sibling image tag derives from the one resolved tag.
	tag          string
	ncImage      string
	rolImage     string
	uiImage      string
	gfehImage    string
	ingressImage string

	// DNS.
	rolMgr *rolodex.Manager
	dnsTLD string
	// dnsPort is the port rolodex serves DNS on, empty meaning the default.
	// It is carried past bootServices because the resolved-routing decision it
	// gates has to be made a second time, for the running controller — see
	// resolvedConfigurator.
	dnsPort string

	// Monitoring.
	monPorts       monitoring.Ports
	monBackend     string
	diskDevices    []string
	swapCapability monitoring.SwapCapability

	// System services.
	tlsCA      *townostls.CA
	ingressMgr *ingressctl.Manager
	gfehReg    systemcontroller.GfehRegistry
	uiMgr      *ui.Manager
}

// close releases everything the boot acquired, in the reverse order it was
// acquired.
//
// It replaces four separate `defer`s in the old run(): bs.Done(), the temp-dir
// removal, db.Close(), and cancel(). Those fired LIFO — cancel, close, remove,
// done — and this reproduces that order exactly. Getting it wrong is not
// cosmetic: closing the database before cancelling the context lets a
// still-running poller query a closed handle, and calling bs.Done() first ends
// the boot stream before the failure that is about to be reported reaches it.
//
// Every field is nil-checked because close runs on the failure path too, from
// a defer registered before any of them exist.
func (b *boot) close() error {
	var errs []error

	if b.cancel != nil {
		b.cancel()
	}
	if b.db != nil {
		errs = append(errs, b.db.Close())
	}
	if b.tmpDir != "" {
		errs = append(errs, os.RemoveAll(b.tmpDir))
	}
	if b.bs != nil {
		// Safety net. The success path calls Done() explicitly after the
		// handler swap; Done is idempotent, so a second call here costs one
		// redundant event. What it buys is that a boot which fails partway
		// still closes the stream cleanly instead of leaving a UI watching a
		// sequence that will never finish.
		b.bs.Done()
	}

	return errors.Join(errs...)
}

// start resolves the environment, parses flags, and binds the listener. It is
// everything that happens before the first bs.Step().
func (b *boot) start() error {
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

	b.dbPath, b.btrfsPath, b.repoDir = *dbPath, *btrfsPath, *repoDir
	b.networkStatePath, b.listenAddr = *networkStatePath, *listenAddr

	// Bind :5309 immediately with a minimal boot-status handler so the
	// UI can observe the boot sequence as it runs. The handler is
	// swapped to the full Echo router at the very end of this function;
	// the listener socket itself is never closed across the swap, so
	// in-flight SSE subscribers on /boot-status survive the handoff.
	b.bs = systemcontroller.NewBootStatus()
	b.rootHandler = systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(b.bs))
	b.srv = &http.Server{
		Addr:              b.listenAddr,
		Handler:           b.rootHandler,
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
		if b.btrfsPath != "" {
			if mkErr := os.MkdirAll(b.btrfsPath, 0750); mkErr != nil {
				return fmt.Errorf("create btrfs base for TLS: %w", mkErr)
			}
		}
		tlsCfg, tlsErr := systemcontroller.ControllerTLSConfig(b.btrfsPath, systemcontroller.ListenAddrSANs(b.listenAddr))
		if tlsErr != nil {
			return fmt.Errorf("configure controller TLS: %w", tlsErr)
		}
		b.srv.TLSConfig = tlsCfg
	}

	b.listenErrCh = make(chan error, 1)
	go func() {
		var listenErr error
		if b.srv.TLSConfig != nil {
			// Cert and key are already in TLSConfig.Certificates.
			listenErr = b.srv.ListenAndServeTLS("", "")
		} else {
			listenErr = b.srv.ListenAndServe()
		}
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			b.listenErrCh <- listenErr
		}
		close(b.listenErrCh)
	}()

	return nil
}

// openStores is the boot_controller stage: directories, the database and every
// manager on it, the package repositories, and the image tag everything else
// derives from.
//
// ctx reaches the database now: account.OpenDB and all six InitXManager
// functions take it, so this stage's work is cancellable and bounded by
// shutdown rather than by a root context each query opened for itself.
func (b *boot) openStores(ctx context.Context) error {
	b.bs.Step("boot_controller")

	dir, err := os.MkdirTemp("", "systemcontroller-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	b.tmpDir = dir

	// Ensure required directories exist.
	for _, d := range []string{b.btrfsPath, b.networkStatePath} {
		if d != "" {
			if err := os.MkdirAll(d, 0750); err != nil {
				return fmt.Errorf("create directory %s: %w", d, err)
			}
		}
	}

	// Self-heal: remove any stale town-os.db (and its SQLite sidecar
	// files) left at the btrfs root by older deployments. The runtime
	// DB lives under <btrfsBase>/data/db/system.db, never at the root.
	cleanupStaleRootDB(b.btrfsPath)

	// Reject -db paths that would re-create the file we just cleaned up.
	if err := validateDBPath(b.dbPath, b.btrfsPath); err != nil {
		return err
	}

	dbFile := filepath.Join(dir, "test.db")
	if b.dbPath != "" {
		dbFile = b.dbPath
		if err := os.MkdirAll(filepath.Dir(dbFile), 0750); err != nil {
			return fmt.Errorf("create db directory: %w", err)
		}
	}

	if b.db, err = account.OpenDB(ctx, dbFile); err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	acctMgr, err := account.InitManager(ctx, b.db)
	if err != nil {
		return fmt.Errorf("init account manager: %w", err)
	}
	b.acctMgr = acctMgr

	signingKey, err := generateSigningKey()
	if err != nil {
		return fmt.Errorf("signing key: %w", err)
	}
	if b.sessMgr, err = account.InitSessionManager(ctx, b.db, acctMgr, signingKey); err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	if b.auditMgr, err = account.InitAuditManager(ctx, b.db); err != nil {
		return fmt.Errorf("init audit manager: %w", err)
	}

	if b.settingsMgr, err = account.InitSettingsManager(ctx, b.db); err != nil {
		return fmt.Errorf("init settings manager: %w", err)
	}

	if b.pagesMgr, err = account.InitPagesManager(ctx, b.db); err != nil {
		return fmt.Errorf("init pages manager: %w", err)
	}

	if b.networkMgr, err = account.InitNetworkManager(ctx, b.db); err != nil {
		return fmt.Errorf("init network manager: %w", err)
	}

	if err := b.seedRepositories(); err != nil {
		return err
	}

	rr, err := packages.RepositoryRootFromBase(b.repoBase)
	if err != nil {
		return fmt.Errorf("init repository root: %w", err)
	}
	b.rr = rr

	// packages.RepositoryRoot takes no context: its refresh shells out to
	// go-git with its own timeouts. A different package from the account
	// managers, and not part of this conversion.
	rr.ForceRefresh() //nolint:contextcheck // RepositoryRoot accepts no context

	b.inst = packages.NewInstallManager(b.repoBase)
	b.st = storage.InitBtrFS(b.btrfsPath)
	b.sd = systemd.NewManager()

	// The image tag for the systemcontroller and every sibling image (UI,
	// rolodex, networkcontroller, ingress). rc.latest-<arch> by default; the
	// install build system pins a specific tag via the TOWN_OS_TAG env var on
	// the systemcontroller unit.
	b.tag = resolveImageTag()

	// Network controller image: pulled from quay.io like every other
	// sibling image. NC_IMAGE overrides the derived default (the test and dev
	// harnesses inject localhost/town-os-networkcontroller:<instance-id>,
	// built on the host by the nc-image / nc-image-dev targets).
	b.ncImage = os.Getenv("NC_IMAGE")
	if b.ncImage == "" {
		b.ncImage = "quay.io/town/networkcontroller:" + b.tag
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
				rr.Refresh() //nolint:contextcheck // RepositoryRoot accepts no context
			}
		}
	}()

	// Rolodex is a boot service managed entirely by systemd. The
	// systemcontroller only writes the config file and waits for DNS
	// readiness before proceeding with image pulls.
	b.rolImage = os.Getenv("ROLODEX_IMAGE")
	if b.rolImage == "" {
		b.rolImage = "quay.io/town/rolodex:" + b.tag
	}

	return nil
}

// seedRepositories writes the default repository list when there is none yet.
func (b *boot) seedRepositories() error {
	b.repoBase = b.tmpDir
	if b.repoDir != "" {
		b.repoBase = b.repoDir
		if err := os.MkdirAll(b.repoBase, 0750); err != nil {
			return fmt.Errorf("create repo dir: %w", err)
		}
	}

	repoFile := filepath.Join(b.repoBase, packages.RepositoriesFile)
	if _, err := os.Stat(repoFile); !os.IsNotExist(err) {
		return nil
	}

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
	if err := os.WriteFile(repoFile, repoData, 0600); err != nil {
		return fmt.Errorf("write repositories file: %w", err)
	}
	return nil
}

// bootDNS is the boot_dns stage: build the rolodex.Manager from the stored
// settings, wait for the already-running rolodex to answer, and point
// systemd-resolved at it. It writes no config file and restarts nothing — see
// the block below. It also resolves the remaining image names and the
// monitoring settings, which have to be known before the pull set is assembled.
//
// Non-fatal throughout: a box with no DNS still boots, and says so on stderr.
func (b *boot) bootDNS(ctx context.Context) error {
	b.bs.Step("boot_dns")

	rolDataDir := filepath.Join(b.btrfsPath, "rolodex")
	if err := os.MkdirAll(rolDataDir, 0750); err != nil {
		return fmt.Errorf("create rolodex data dir: %w", err)
	}

	// The resolution mode is a user-facing setting: "auto" (the default: roots,
	// then DoH/DoT, then the forwarders, then a public resolver), "recursive"
	// (roots only, no fallback), or "forward" (straight to the forwarders). An
	// invalid stored value is ignored so a bad setting can never be programmed
	// into the running server, which rejects a mode it does not know rather
	// than falling back to its default.
	resolutionMode := rolodex.DefaultResolutionMode
	if v, modeErr := b.settingsMgr.Get(ctx, "dns_resolution_mode"); modeErr == nil && rolodex.ValidResolutionMode(v) {
		resolutionMode = v
	}

	// When set, the forwarder list is discovered from the host's own resolv.conf
	// rather than being the public defaults — the addresses that keep answering
	// on a network that blocks external DNS. Discovery runs on every boot, so a
	// box that changed networks picks up the new resolver without the operator
	// touching anything. An unparseable stored value is read as off: the safe
	// direction is the one that does not hand the local network every name the
	// household looks up.
	localForwarders := false
	if v, fwdErr := b.settingsMgr.Get(ctx, "dns_local_forwarders"); fwdErr == nil {
		if parsed, parseErr := strconv.ParseBool(strings.TrimSpace(v)); parseErr == nil {
			localForwarders = parsed
		}
	}

	// The operator's own forwarder list, which may name any transport. An
	// unparseable entry is dropped rather than failing the boot: this is the
	// box's resolver, and refusing to start over one bad forwarder would take
	// DNS down for everything on it. Empty leaves Config.Forwarders nil, which
	// Manager.forwarders reads as DefaultForwarders.
	var configuredForwarders []string
	if v, fwdErr := b.settingsMgr.Get(ctx, "dns_forwarders"); fwdErr == nil {
		for _, spec := range rolodex.SplitForwarderSpecs(v) {
			if _, parseErr := rolodex.ParseForwarder(spec); parseErr != nil {
				slog.Warn("ignoring unparseable dns forwarder", "spec", spec, "error", parseErr)
				continue
			}
			configuredForwarders = append(configuredForwarders, spec)
		}
	}

	// Empty (the normal case) means rolodex.DefaultDNSPort — see ports.go for
	// why the integration harness relocates it.
	dnsPort := dnsPortFromEnv()
	b.dnsPort = dnsPort

	// The blocklist provider list comes off the persisted settings, because
	// rolodex keeps it in memory only and comes back from any restart with
	// every configured provider switched off. It is carried on the manager so
	// the gRPC push below — and the re-assert in RebuildDNS — have the
	// operator's list to push rather than an empty one.
	storedDNSBL := systemcontroller.StoredBlocklist(ctx, b.settingsMgr)

	b.rolMgr = rolodex.NewManager(rolodex.Config{
		Systemd:         b.sd,
		DataDir:         rolDataDir,
		Image:           b.rolImage,
		UnixSocketPath:  filepath.Join(rolDataDir, "rolodex.sock"),
		ResolutionMode:  resolutionMode,
		DNSPort:         dnsPort,
		MetricsPort:     rolodexMetricsPortFromEnv(),
		Forwarders:      configuredForwarders,
		LocalForwarders: localForwarders,
		DNSBL:           storedDNSBL,
	})
	// Nothing is written to rolodex.yml here, and nothing restarts rolodex.
	// That file belongs to the install image, which renders the two settings
	// that cannot be programmed at runtime — the bind list, from host
	// addresses only it can enumerate, and the metrics listener rolodex opens
	// once at startup. Everything Town OS owns (forwarders, resolution mode,
	// both blocklists) is pushed into the running server instead, below.
	//
	// This ordering is why rolodex starts before the systemcontroller at all:
	// the controller cannot pull an image until something resolves names, so
	// rolodex is already serving from its own config by the time this runs.

	// Wait for DNS readiness.
	//
	// The overwhelmingly likely cause of this failing is rolodex.yml being
	// absent or binding something other than DNSLoopback: rolodex opens that
	// listener once, from that file, and nothing here can program it (see the
	// note below on what this repo does and does not own). The unit reads as
	// active either way, because the container starts fine and only the
	// listener is missing — so the path is named here rather than left to be
	// rediscovered from a bare timeout. Non-fatal: a box that cannot resolve
	// still has an API worth reaching to fix it.
	if err := b.rolMgr.WaitForDNSReady(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "rolodex DNS readiness: %v (check %s for a dns.bind on %s)\n",
			err, filepath.Join(rolDataDir, "rolodex.yml"), rolodex.DNSLoopback)
	}

	// The settings rolodex does not persist — forwarders, resolution mode,
	// both blocklists — are programmed in reconcileDNSAndNetworks, which is
	// where this boot already has a dialed rolodex client.

	// Configure systemd-resolved to route the TLD to rolodex so
	// inter-package DNS resolution works (container -> aardvark ->
	// resolved -> rolodex for .tld queries). Non-fatal.
	b.dnsTLD = "home"
	if v, tldErr := b.settingsMgr.Get(ctx, "dns_tld"); tldErr == nil && v != "" {
		b.dnsTLD = v
	}
	// resolved can only route a domain to a resolver on :53, so this is skipped
	// entirely when rolodex has been relocated off the standard port. Pointing
	// resolved at DNSLoopback in that case would blackhole every .tld query.
	if fn := b.resolvedConfigurator(); fn != nil {
		fn(ctx, b.dnsTLD, rolodex.DNSLoopback)
	} else {
		fmt.Fprintf(os.Stderr, "rolodex DNS on non-standard port %s; skipping systemd-resolved routing\n", dnsPort)
	}

	b.resolveRemainingImages()
	b.readMonitoringSettings(ctx)
	return nil
}

// resolvedConfigurator returns the function that points systemd-resolved at
// rolodex for the box's TLD, or nil when this box must not have one.
//
// Boot is not the only caller: changing the TLD re-points resolved at the new
// domain, from the running controller, hours later — so the decision boot makes
// here has to be the same one the controller is handed in its ServerConfig, and
// a nil there is what makes /dns/tld skip the call. It was not: boot guarded its
// own call and then wired the raw rolodex.ConfigureResolvedRouting into the
// controller regardless, so a relocated rolodex stayed unrouted only until
// somebody renamed the TLD.
//
// nil is returned for exactly one reason, and it is the reason a per-domain
// resolved server address carries no port: with rolodex on anything but :53,
// resolved would send every query for the TLD to 127.0.0.2:53, where nothing is
// listening, and blackhole a domain that would otherwise have fallen through to
// the normal resolver path.
func (b *boot) resolvedConfigurator() func(ctx context.Context, tld, loopbackAddr string) {
	if !dnsPortIsDefault(b.dnsPort) {
		return nil
	}
	return rolodex.ConfigureResolvedRouting
}

// resolveRemainingImages derives the UI and gfeh image names.
//
// Both are resolved here, well above the services that use them, for one
// reason: they have to be in the boot pull set assembled at the top of
// bootServices. gfeh was not, and it was the only system-service image that
// was not — on a cold box that meant the very first `podman run` in the unit
// did the pull, a Rust daemon's worth of layers, while the socket-readiness
// wait counted down, expired, and left object storage reading as down for the
// rest of the boot.
func (b *boot) resolveRemainingImages() {
	// Derive UI image. When UI_IMAGE is explicitly empty the UI container
	// is skipped entirely (useful in dev where bun serves the UI).
	b.uiImage = os.Getenv("UI_IMAGE")
	if _, uiSet := os.LookupEnv("UI_IMAGE"); !uiSet {
		b.uiImage = "quay.io/town/ui:" + b.tag
	}

	// Object storage: one gfeh partition per network, each its own daemon.
	//
	// GFEH_IMAGE explicitly empty skips it entirely (dev mode), the same
	// LookupEnv convention UI_IMAGE and INGRESS_IMAGE use — Getenv would make
	// an empty value mean "use the default" and there would be no off switch.
	b.gfehImage = os.Getenv("GFEH_IMAGE")
	if _, gfehSet := os.LookupEnv("GFEH_IMAGE"); !gfehSet {
		b.gfehImage = "quay.io/town/gfeh:" + b.tag
	}

	// Ingress: the shared :443 SNI router. Skipped when INGRESS_IMAGE is
	// explicitly set to empty (dev mode).
	b.ingressImage = os.Getenv("INGRESS_IMAGE")
	if _, set := os.LookupEnv("INGRESS_IMAGE"); !set {
		b.ingressImage = "quay.io/town/ingress:" + b.tag
	}
}

func (b *boot) readMonitoringSettings(ctx context.Context) {
	// Host ports for the three monitoring system services. The zero value means
	// the production defaults; the harness relocates them (see ports.go).
	//
	// The controller's own scrape scheme is taken from the SOCKET, not from the
	// TLS decision made earlier in this boot. Both are supposed to say the same
	// thing, and on a deployed box they did not — prometheus.yml claimed https
	// while :5309 served cleartext, so every scrape of the controller failed and
	// nothing anywhere said so. The listener has been bound since start(), so
	// there is something to ask by the time this runs; b.srv.TLSConfig is the
	// fallback for the case where it cannot be reached at all.
	configuredTLS := b.srv.TLSConfig != nil
	observedTLS := systemcontroller.ListenerSpeaksTLS(ctx, b.listenAddr, configuredTLS)
	if msg := systemcontroller.SchemeDisagreement(observedTLS, configuredTLS); msg != "" {
		fmt.Fprintf(os.Stderr, "controller listener: %s\n", msg)
	}
	// b.ingressImage is resolved by resolveRemainingImages, which runs
	// immediately before this — the ingress scrape target has to be decided here
	// rather than in startIngress, because prometheus.yml is written by
	// startMonitoring and the ingress starts after it.
	b.monPorts = withScrapeTargets(monitoringPortsFromEnv(), b.rolMgr, b.listenAddr,
		ingressMetricsTarget(b.ingressImage), observedTLS)

	// Determine monitoring backend (uplot or grafana).
	b.monBackend = monitoring.BackendUPlot
	if v, settingsErr := b.settingsMgr.Get(ctx, "monitoring_backend"); settingsErr == nil && v != "" {
		b.monBackend = v
	}

	// Discover the block devices backing the btrfs filesystem at btrfsPath
	// so the monitoring dashboards can sum node_disk_* metrics over only
	// those devices. Non-fatal: on failure the Disk I/O panel renders
	// empty rather than aggregating unrelated host disks.
	// monitoring.BtrfsDevices shells out to `btrfs filesystem show` on its own
	// root context; it is not one of the account managers this change covers.
	diskDevices, diskErr := monitoring.BtrfsDevices(b.btrfsPath) //nolint:contextcheck // BtrfsDevices accepts no context
	if diskErr != nil {
		fmt.Fprintf(os.Stderr, "btrfs disk device discovery: %v\n", diskErr)
	}
	b.diskDevices = diskDevices

	// Whether this pool can host a swapfile at all. Probed ONCE here, with the
	// device list just discovered, because it cannot change without
	// re-formatting the pool and the alternative is a `btrfs filesystem df`
	// subprocess on every status poll. Live usage is read per request from
	// /proc/swaps instead; only this static half is cached.
	//
	// It is worth reporting because on any multi-disk box the answer is a
	// permanent no — btrfs cannot swap on a multi-device filesystem — and
	// without this the user just finds no swap and no explanation.
	b.swapCapability = monitoring.ProbeSwapCapability(b.btrfsPath, diskDevices) //nolint:contextcheck // ProbeSwapCapability accepts no context
	if !b.swapCapability.Supported {
		fmt.Fprintf(os.Stderr, "swapfile unavailable on %s: %s (devices=%d profiles=%v)\n",
			b.btrfsPath, b.swapCapability.Reason, b.swapCapability.Devices, b.swapCapability.DataProfiles)
	}
}

// bootServices is the boot_services stage: pull the core images, start
// monitoring, ensure the CA, and bring up the ingress, pages, and the
// object-storage partitions. Everything here is non-fatal.
func (b *boot) bootServices(ctx context.Context) {
	b.bs.Step("boot_services")

	// The controller's own image first, and before anything else in this
	// stage: EnsureImage below keeps every OTHER service current, but the
	// process running it is started by a unit that is deliberately
	// --pull=missing, so without this the controller itself stays on the
	// image the box was installed with — including whatever bug this boot
	// was supposed to fix. When the tag has moved, the restart happens here
	// rather than after a full boot whose work is about to be discarded.
	if systemcontroller.SelfUpdate(ctx, b.sd, b.btrfsPath) {
		systemcontroller.AwaitSelfRestart(ctx)
	}

	parallelEnsureImages(ctx, coreBootImages(b.ncImage, b.uiImage, b.gfehImage, b.ingressImage, b.monBackend))

	// Tear down obsolete monitoring units from the previous (NC + socket)
	// design before starting the new host-net services. On an in-place
	// upgrade the leftover NC containers still hold -p 9090:9090 / -p
	// 5308:5308, so Prometheus and the socat would crash-loop with
	// "address already in use" until these are removed. Best-effort and
	// idempotent (a no-op on fresh installs).
	monitoring.CleanupLegacyMonitoringUnits(ctx, b.sd)

	b.startMonitoring(ctx)

	// Install the nightly podman prune timer. Non-fatal: if the units
	// cannot be written, the system still boots — it just accumulates
	// image cruft until the next restart fixes it.
	if err := systemcontroller.InstallPodmanPruneUnits(ctx, b.sd); err != nil {
		fmt.Fprintf(os.Stderr, "podman prune timer: %v\n", err)
	}

	// There is deliberately no daily-update timer installed here. That timer
	// (town-os-update.timer) ships with the installer and is enabled at image
	// build time, so a box has it from first boot rather than acquiring it from
	// a controller that must already be running. The controller's half of that
	// contract is the ScheduledRefreshQuery marker and the auto_update_enabled
	// setting, both in maintenance_update.go.

	// Ensure the local TLS CA exists. This has to happen before reconcile
	// so reconcile can issue leaf certs for HTTP-supplying packages as it
	// walks installed units. The btrfs `tls` subvolume is created by
	// reconcile itself, but EnsureCA falls back to os.MkdirAll so the
	// path is usable even when the subvolume layout is not yet in place
	// (e.g. very first boot on a fresh btrfs).
	if caVal, caErr := townostls.EnsureCA(filepath.Join(b.btrfsPath, systemcontroller.TLSSubvolume)); caErr != nil {
		fmt.Fprintf(os.Stderr, "tls ca: %v\n", caErr)
	} else {
		b.tlsCA = caVal
	}

	b.startIngress(ctx)
	b.startGfeh(ctx)
}

// startMonitoring starts the three monitoring system services in parallel.
//
// All three run --net host: node-exporter and Prometheus bind 127.0.0.1
// (private) and the monitoring UI forwards the single LAN port :5308 to
// Prometheus over the loopback — no podman networks, so concurrent starts
// cannot race. All non-fatal.
func (b *boot) startMonitoring(ctx context.Context) {
	var monWG sync.WaitGroup
	monWG.Add(3)
	go func() {
		defer monWG.Done()
		if err := monitoring.StartNodeExporter(ctx, b.sd, b.monPorts); err != nil {
			fmt.Fprintf(os.Stderr, "node-exporter: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartPrometheus(ctx, b.sd, b.btrfsPath, b.monPorts); err != nil {
			fmt.Fprintf(os.Stderr, "prometheus: %v\n", err)
		}
	}()
	go func() {
		defer monWG.Done()
		if err := monitoring.StartMonitoringUI(ctx, b.sd, b.st, b.monBackend, b.btrfsPath, b.ncImage, b.diskDevices, b.monPorts); err != nil {
			fmt.Fprintf(os.Stderr, "monitoring-ui: %v\n", err)
		}
	}()
	monWG.Wait()
}

// ingressLocale reads the box's configured language for the ingress unit,
// falling back to the default locale.
//
// The same "locale" row the handlers read through getLocale, and the same
// treatment: a missing row, an unreadable settings manager, or an empty value
// all mean English. It is read here rather than pushed over gRPC because it is
// one box-wide setting rather than route state — and reading it at unit
// generation means a language change reaches the ingress the way every other
// unit change does, by rendering a different unit on the next reconcile.
func (b *boot) ingressLocale(ctx context.Context) string {
	if b.settingsMgr == nil {
		return i18n.DefaultLocale
	}
	if val, err := b.settingsMgr.Get(ctx, "locale"); err == nil && val != "" {
		return val
	}
	return i18n.DefaultLocale
}

// startIngress brings up the shared :443 SNI router and the pages service it
// reverse-proxies to. Programmed over gRPC later (programIngress), plus the
// per-package and page-CRUD handlers.
func (b *boot) startIngress(ctx context.Context) {
	if b.ingressImage == "" {
		return
	}

	b.ingressMgr = ingressctl.NewManager(ingressctl.Config{
		Systemd:    b.sd,
		DataDir:    filepath.Join(b.btrfsPath, "ingress"),
		TLSHostDir: filepath.Join(b.btrfsPath, systemcontroller.TLSSubvolume),
		Image:      b.ingressImage,
		// Serve dual-stack only when the host has a global IPv6 (otherwise
		// `podman network create --ipv6` fails and the unit won't start).
		EnableIPv6: getInternalIPv6() != "",
		// Zero means the privileged production ports (443/80); the harness
		// relocates them so a test ingress cannot collide with a dev or
		// production ingress in the shared host netns (see ports.go).
		HostPort:     envPortInt(EnvIngressHTTPSPort),
		HTTPHostPort: envPortInt(EnvIngressHTTPPort),
		// Read through the same helper the Prometheus target was built from in
		// readMonitoringSettings, so the published port and the scrape target
		// cannot disagree about where this ingress serves /metrics.
		MetricsPort: ingressMetricsPortFromEnv(),
		// The box's language, for the retry page the ingress serves while a
		// backend is down. Non-fatal by construction: an unreadable or unset
		// setting leaves the flag off and the page falls back to en-US, which
		// is the same answer the systemcontroller's own getLocale gives.
		Locale: b.ingressLocale(ctx),
	})
	if startErr := b.ingressMgr.Start(ctx); startErr != nil {
		fmt.Fprintf(os.Stderr, "ingress: %v\n", startErr)
	}

	// Pages: a standalone Caddy static-file service the ingress
	// reverse-proxies to for every page FQDN. Started alongside the ingress.
	if pErr := systemcontroller.StartPagesService(ctx, b.sd, b.btrfsPath, systemcontroller.DefaultCaddyImage); pErr != nil {
		fmt.Fprintf(os.Stderr, "pages service: %v\n", pErr)
	}
}

// startGfeh brings up the object-storage partitions. gfehImage was resolved in
// bootDNS so it could join the boot pull set; by here the image is local and
// the units start against it rather than against a registry.
func (b *boot) startGfeh(ctx context.Context) {
	if b.gfehImage == "" {
		return
	}

	// Gated on the ingress: the four HTTP views publish no host port and
	// are reachable only through it, so starting partitions with no
	// ingress would produce four names per network that nothing serves.
	if b.ingressMgr == nil {
		fmt.Fprintf(os.Stderr, "gfeh: skipped, the ingress is disabled and the HTTP views are only reachable through it\n")
		return
	}

	reg := systemcontroller.NewGfehRegistry(systemcontroller.ReconcileGfehConfig{
		NetworkMgr:    b.networkMgr,
		Storage:       b.st,
		Systemd:       b.sd,
		SettingsMgr:   b.settingsMgr,
		BtrfsBasePath: b.btrfsPath,
		Image:         b.gfehImage,
	})
	systemcontroller.ReconcileGfeh(ctx, reg)
	b.gfehReg = reg
}

// reconcile walks every installed package and restores its runtime state, then
// persists the image SHA so the next boot can detect an upgrade. This is the
// one stage that is fatal: a box whose packages did not reconcile is not a box
// that should start serving.
func (b *boot) reconcile(ctx context.Context) error {
	// Detect whether the systemcontroller image changed since the last
	// run. When it has, reconcile will restart all units whose generated
	// content differs from what is on disk.
	versionFile := filepath.Join(b.btrfsPath, "town-os-version")
	versionChanged := detectVersionChange(ctx, versionFile)

	err := systemcontroller.Reconcile(ctx, systemcontroller.ReconcileConfig{
		Installer:              b.inst,
		RepositoryRoot:         b.rr,
		Storage:                b.st,
		Systemd:                b.sd,
		SettingsMgr:            b.settingsMgr,
		NetworkMgr:             b.networkMgr,
		PagesManager:           b.pagesMgr,
		BtrfsBasePath:          b.btrfsPath,
		NetworkControllerImage: b.ncImage,
		NetworkStatePath:       b.networkStatePath,
		InternalIP:             getInternalIP(),
		VersionChanged:         versionChanged,
		TLSCA:                  b.tlsCA,
		Gfeh:                   b.gfehReg,
		Git:                    &git.GoGitClient{},
		PostUpdateExec: func(ctx context.Context, containerName string, command string) error {
			out, execErr := systemcontroller.PodmanContainerExec(ctx, containerName, command)
			if execErr != nil {
				return execErr
			}
			slog.Info(fmt.Sprintf("post-update %s: %s", containerName, strings.TrimSpace(out)))
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("reconcile: %w", err)
	}

	// Persist current image SHA so the next startup can detect changes.
	persistVersion(ctx, versionFile)
	return nil
}

// reconcileDNSAndNetworks rebuilds the rolodex zone from scratch and brings up
// every enabled network's WireGuard interface, then reconciles object storage
// a second time.
//
// The rebuild is deliberate rather than a diff: startup wipes and rebuilds so
// any drift from a crashed or out-of-sync prior run is discarded. The hourly
// drift-repair poller (ReconcileDNS) takes over once the HTTP server is up.
func (b *boot) reconcileDNSAndNetworks(ctx context.Context) {
	socketPath := b.rolMgr.SocketPath()
	deadline := time.Now().Add(30 * time.Second)
	var rolClient rolodex.Client
	for time.Now().Before(deadline) {
		var dialErr error
		rolClient, dialErr = rolodex.Dial(ctx, socketPath)
		if dialErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if rolClient != nil {
		// Program what rolodex does not persist BEFORE reconciling records.
		// rolodex seeds forwarders, resolution mode and both blocklists from
		// its config file and keeps them in memory only, so until this runs
		// the box is resolving with rolodex's defaults rather than the
		// operator's choices — and the records reconciled below are records in
		// a resolver whose upstreams are not yet the ones Town OS chose.
		if err := systemcontroller.ProgramRolodex(ctx, rolClient, b.rolMgr, b.settingsMgr); err != nil {
			fmt.Fprintf(os.Stderr, "program rolodex: %v\n", err)
		}

		dnsCfg := systemcontroller.ReconcileDNSConfig{
			Client:           rolClient,
			Installer:        b.inst,
			RepositoryRoot:   b.rr,
			SettingsMgr:      b.settingsMgr,
			PagesManager:     b.pagesMgr,
			NetworkMgr:       b.networkMgr,
			InternalIP:       getInternalIP(),
			InternalIPv6:     getInternalIPv6(),
			NetworkStatePath: b.networkStatePath,
			BtrfsBasePath:    b.btrfsPath,
			Gfeh:             b.gfehReg,
			// Issues the leaf rolodex's DoT/DoQ listeners serve. This boot is
			// the first moment the CA exists — rolodex started before it — so
			// it is also the first moment that certificate can stop being
			// self-signed.
			TLSCA: b.tlsCA,
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
		NetworkMgr:       b.networkMgr,
		Systemd:          b.sd,
		NetworkStatePath: b.networkStatePath,
		SettingsMgr:      b.settingsMgr,
		RolodexClient:    rolClient,
	})

	if rolClient != nil {
		if closeErr := rolClient.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "close rolodex client: %v\n", closeErr)
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
	systemcontroller.ReconcileGfehRegistry(ctx, b.gfehReg)
}

// programIngress pushes the full route set (HTTP packages + pages) to the
// shared :443 ingress, at the same point in boot as rolodex.
// Push/declarative: on a fresh ingress this rebuilds everything (same model as
// RebuildDNS).
func (b *boot) programIngress(ctx context.Context) {
	if b.ingressMgr == nil {
		return
	}

	if rdyErr := b.ingressMgr.WaitForReady(ctx); rdyErr != nil {
		fmt.Fprintf(os.Stderr, "ingress readiness: %v\n", rdyErr)
	}
	ic, dialErr := ingress.Dial(ctx, b.ingressMgr.SocketPath())
	if dialErr != nil {
		fmt.Fprintf(os.Stderr, "ingress dial: %v\n", dialErr)
		return
	}
	if irErr := systemcontroller.RebuildIngress(ctx, ic, b.pagesMgr, b.networkMgr, b.inst, b.gfehReg, b.tlsCA, b.btrfsPath, b.networkStatePath, b.dnsTLD, getInternalIP()); irErr != nil {
		fmt.Fprintf(os.Stderr, "rebuild ingress: %v\n", irErr)
	}
	if closeErr := ic.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "close ingress client: %v\n", closeErr)
	}
}

// startUI starts the UI container (Caddy web server). Skipped when UI_IMAGE is
// empty (dev mode — bun serves the UI directly).
func (b *boot) startUI(ctx context.Context) {
	if b.uiImage == "" {
		return
	}
	b.uiMgr = ui.NewManager(ui.Config{Systemd: b.sd, Image: b.uiImage})
	if err := b.uiMgr.Start(ctx); err != nil {
		// Non-fatal: UI failure should not prevent the system
		// controller from starting.
		fmt.Fprintf(os.Stderr, "ui: %v\n", err)
		b.uiMgr = nil
	}
}

// freshness is the restart_packages stage: if the refresh handler left a
// marker in the previous process, restart every installed package unit
// serially so the new systemcontroller sees freshly-started children.
// Per-package events are emitted so the UI can render rolling progress. A
// stale marker left by a crash is harmless (worst case one extra restart cycle
// on next boot).
func (b *boot) freshness(ctx context.Context) {
	if failed, freshErr := systemcontroller.RunFreshnessStage(ctx, b.bs, b.inst, b.sd, b.btrfsPath); freshErr != nil {
		fmt.Fprintf(os.Stderr, "freshness stage: %v\n", freshErr)
	} else if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "freshness stage: %d package(s) failed to restart: %s\n",
			len(failed), strings.Join(failed, ", "))
	}
}

// serve builds the full router, swaps it in over the already-bound listener,
// and blocks until the server exits.
func (b *boot) serve(ctx context.Context) error {
	handler, err := systemcontroller.NewHandler(ctx, systemcontroller.ServerConfig{
		Storage:                    b.st,
		RepositoryRoot:             b.rr,
		Installer:                  b.inst,
		Systemd:                    b.sd,
		AccountMgr:                 b.acctMgr,
		SessionMgr:                 b.sessMgr,
		AuditMgr:                   b.auditMgr,
		SettingsMgr:                b.settingsMgr,
		PagesMgr:                   b.pagesMgr,
		NetworkMgr:                 b.networkMgr,
		DefaultRepoUser:            os.Getenv(packages.EnvRepoUsername),
		DefaultRepoPass:            os.Getenv(packages.EnvRepoPassword),
		BtrfsBasePath:              b.btrfsPath,
		NetworkControllerImage:     b.ncImage,
		NetworkStatePath:           b.networkStatePath,
		TLSCA:                      b.tlsCA,
		MonitoringBackend:          b.monBackend,
		DiskDevices:                b.diskDevices,
		SwapCapability:             b.swapCapability,
		MonitoringPorts:            b.monPorts,
		Rolodex:                    b.rolMgr,
		Ingress:                    b.ingressMgr,
		UI:                         b.uiMgr,
		GfehRegistry:               b.gfehReg,
		ResolvedConfigurator:       b.resolvedConfigurator(),
		SystemControllerImage:      "quay.io/town/town:" + b.tag,
		SystemControllerListenAddr: b.listenAddr,
		// Same id the boot stub reported on /status/ping, so a refresh
		// client watching across the restart sees one continuous identity
		// per process and can tell this incarnation from its predecessor.
		BootID: b.bs.BootID(),
	})
	// Fatal, and deliberately so: sessMgr is built unconditionally in
	// openStores, so reaching here without one means the boot sequence broke.
	// Swapping in a router that cannot authenticate anybody would leave the box
	// either wide open or answering 500 on every route, and both are worse than
	// saying why and stopping.
	if err != nil {
		return fmt.Errorf("create HTTP handler: %w", err)
	}

	// Atomically swap the root handler from the boot-status stub to the
	// full Echo router. The listener socket has been bound the entire
	// time, so no port flap occurs; SSE subscribers on /boot-status
	// survive the swap and keep streaming until the BootStatus stream
	// closes via Done below.
	b.rootHandler.Swap(handler)
	b.bs.Step("ready")
	b.bs.Done()

	b.publishGfeh(ctx)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	// context.Background() below, NOT the ctx in scope, and the distinction is
	// the whole graceful shutdown. The goroutine cancels ctx to stop the
	// background pollers; deriving the shutdown deadline from it would hand
	// Shutdown an already-cancelled context, which returns immediately and
	// drops every in-flight request instead of draining it. The 30s budget has
	// to be measured from a context that is still alive.
	go func() { //nolint:gosec,contextcheck // G118 -- ctx is deliberately not used; see above
		<-sig
		b.cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		shutdownErr := b.srv.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			fmt.Fprintf(os.Stderr, "shutdown: %v\n", shutdownErr)
		}
	}()

	fmt.Fprintf(os.Stderr, "systemcontroller: listening on %s\n", b.listenAddr)
	// The HTTP server was started in a goroutine at the top of run().
	// Block here until it exits (normal shutdown via SIGINT closes the
	// error channel with no value; a bind/listen failure sends a
	// non-nil error).
	if listenErr, ok := <-b.listenErrCh; ok && listenErr != nil {
		return fmt.Errorf("listen: %w", listenErr)
	}

	return nil
}

// publishGfeh publishes the object-storage names, after the handler swap and
// not with the rest of the DNS and ingress work above.
//
// The ordering is forced: each partition's gfehd waits for /status/ping to
// stop answering 503 before it authenticates and opens its partition, and that
// only happens at the swap. Asking it for GET /v1/names any earlier gets
// nothing, and RebuildDNS having already run means nothing would republish
// them until the hourly reconcile an hour later.
//
// Backgrounded so a partition that never comes up cannot hold the process
// short of serving requests — the router is already live at this point.
func (b *boot) publishGfeh(ctx context.Context) {
	if b.gfehReg == nil {
		return
	}
	go publishGfehNames(ctx, gfehPublishConfig{
		Registry:         b.gfehReg,
		Rolodex:          b.rolMgr,
		Ingress:          b.ingressMgr,
		Installer:        b.inst,
		RepositoryRoot:   b.rr,
		PagesMgr:         b.pagesMgr,
		NetworkMgr:       b.networkMgr,
		SettingsMgr:      b.settingsMgr,
		TLSCA:            b.tlsCA,
		BtrfsBasePath:    b.btrfsPath,
		NetworkStatePath: b.networkStatePath,
		TLD:              b.dnsTLD,
	})
}
