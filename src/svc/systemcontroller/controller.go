package systemcontroller

import (
	"context"
	"net/http"
	"sync"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
	"gitea.com/town-os/town-os/src/ui"
	"github.com/labstack/echo/v5"
)

// The backend is decomposed into role interfaces below, and
// systemControllerBackend is their composition.
//
// This is a naming exercise, not a behavior change: the composed method set is
// byte-for-byte what it was, every handler still receives the whole thing
// through s.Controller, and no call site changes. What it buys is that the
// subsystems now have names, so a helper CAN take the two or three methods it
// actually uses instead of the world.
//
// **New handlers and helpers should take the narrowest role interface they
// need as a parameter**, rather than reaching through s.Controller. That is the
// only way this stops growing. It reached forty-four methods because there was
// never a smaller thing to ask for: adding a subsystem meant adding a getter
// here, which meant every implementation and every fake grew a method, and no
// function anywhere declared what it actually depended on. `interfacebloat` is
// deliberately disabled in .golangci.yml, so nothing was going to say so.
//
// Retrofitting the existing handlers is deliberately NOT part of this: it would
// touch every file in the package for no behavior change and bury the parts of
// this branch that do change behavior. The boundary is drawn; adoption is
// incremental.

// storageBackend is btrfs volumes and where they live on disk.
type storageBackend interface {
	GetStorage() storage.Storage
	GetBtrfsBasePath() string
}

// packageBackend is the package repository and the installed-package record.
type packageBackend interface {
	GetRepositoryRoot() *packages.RepositoryRoot
	GetInstaller() packages.Installer
	GetGitClient() git.Client
	GetGitCloner() packages.GitCloner
	GetDefaultRepoCredentials() (string, string)
	GetImageExtractFunc() func(ctx context.Context, image, directory, targetPath string) error
	// GetContainerExecFunc returns the function used to run a package's
	// post_install commands inside its container. Never nil in production;
	// tests substitute a recorder so the install path needs no podman.
	GetContainerExecFunc() ContainerExecFunc
}

// authBackend is who the caller is and what they may do.
type authBackend interface {
	GetAccountManager() account.Manager
	GetSessionManager() account.SessionManager
	// IsAuthDisabled reports whether every authentication and authorization
	// check is turned off. See ServerConfig.AuthDisabled — it is deliberately
	// not inferred from GetSessionManager() being nil. (Named Is... rather
	// than matching the field: serverBase embeds ServerConfig, so a method
	// spelled AuthDisabled would collide with the field it reads.)
	IsAuthDisabled() bool
	GetAuditManager() account.AuditManager
	GetSettingsManager() account.SettingsManager
	GetAllowedHosts() []string
}

// networkBackend is the WireGuard overlays, the addresses this box answers on,
// and the per-package network state the network controllers read.
type networkBackend interface {
	GetNetworkManager() account.NetworkManager
	GetNetworkControllerImage() string
	GetNetworkStatePath() string
	GetExternalIP() string
	GetInternalIP() string
	GetInternalIPv6() string
}

// dnsBackend is rolodex: the zone, and the knobs that re-render its config.
type dnsBackend interface {
	GetRolodex() *rolodex.Manager
	GetRolodexClient() rolodex.Client
	GetResolvedConfigurator() func(ctx context.Context, tld, loopbackAddr string)
	// RefreshDNSResolutionMode switches rolodex between recursive-from-roots
	// and forwarding to its upstream resolvers, taking effect immediately.
	RefreshDNSResolutionMode(ctx context.Context, mode string) error
	// RefreshDNSLocalForwarders switches rolodex's forwarder list between the
	// public defaults and the resolvers this box's own network handed it,
	// taking effect immediately.
	RefreshDNSLocalForwarders(ctx context.Context, enabled bool) error
	// RefreshDNSForwarders replaces the operator's upstream forwarder list on
	// the running server. Entries may name any transport; empty restores the
	// public defaults.
	RefreshDNSForwarders(ctx context.Context, value string) error
}

// serviceBackend is the system services the controller supervises alongside
// packages: the ingress, the UI, pages, object storage, and monitoring.
type serviceBackend interface {
	GetSystemdManager() systemd.Manager
	GetIngress() *ingressctl.Manager
	GetIngressClient() ingress.Client
	GetUI() *ui.Manager
	GetPagesManager() account.PagesManager
	GetGfehRegistry() GfehRegistry
	GetMonitoringBackend() string
	GetMonitoringPorts() monitoring.Ports
	// GetScrapeTargetsFunc returns the function that asks Prometheus which of
	// its scrape jobs are answering. nil means monitoring.FetchScrapeTargets —
	// the real query against the loopback Prometheus — and a test supplies its
	// own so a status poll never depends on a port in the host namespace.
	GetScrapeTargetsFunc() ScrapeTargetsFunc
	GetDiskDevices() []string
	GetSwapCapability() monitoring.SwapCapability
	RefreshMonitoringBackend(ctx context.Context, backend string) error
	// MonitoringUIPending reports that a backend switch is still fetching its
	// image, so the monitoring-ui unit is still running the OLD backend even
	// though the setting already names the new one.
	MonitoringUIPending() bool
}

// processBackend is what this process is: its own image, its listener, its
// incarnation, and the CA it issues leaves from.
type processBackend interface {
	GetSystemControllerImage() string
	GetSystemControllerListenAddr() string
	GetBootID() string
	GetTLSCA() *townostls.CA
}

type systemControllerBackend interface {
	storageBackend
	packageBackend
	authBackend
	networkBackend
	dnsBackend
	serviceBackend
	processBackend
}

type SystemController interface {
	systemControllerBackend
	Run() error
	Client() (*SystemdClient, error)
}

type SystemControllerHandlers struct {
	Controller systemControllerBackend

	// ctx is the server-scoped context used by background goroutines that
	// must outlive HTTP requests but respect graceful shutdown.
	ctx context.Context //nolint:containedctx // intentional: background goroutines need server lifetime

	// packageMu serializes mutating operations (install, uninstall, purge,
	// enable/disable, rebuild-git) on the same package so that concurrent
	// requests cannot interleave and leave the service in an inconsistent
	// state (e.g. old container still stopping while a new one tries to
	// bind the same ports).
	packageMu sync.Map // "repo/name" → *sync.Mutex

	// ingressRestartMu serializes background restarts of the shared :443 ingress
	// Caddy container so concurrent page/package CRUD never fires overlapping
	// `systemctl restart` calls (the Caddyfile itself is written synchronously).
	ingressRestartMu sync.Mutex

	// oauthStore holds the OAuth device flows currently awaiting approval. They
	// are short-lived and worthless once redeemed, so they never leave memory.
	oauthOnce  sync.Once
	oauthStore *oauthFlows

	// Login throttling state. Built on first use so a handler set constructed
	// without one still works. See controller_auth_throttle.go.
	loginOnce         sync.Once
	loginLimiterStore *loginLimiter
	loginGateStore    *loginGate

	// Prometheus counters, built on first use for the same reason. See
	// controller_metrics.go.
	metricsOnce  sync.Once
	metricsStore *metricsState
}

// oauthFlows returns the pending-device-flow store, creating it on first use so
// that a handler set built without one (every test server) still works.
func (s *SystemControllerHandlers) oauthFlows() *oauthFlows {
	s.oauthOnce.Do(func() { s.oauthStore = newOAuthFlows() })
	return s.oauthStore
}

// lockPackage acquires a per-package mutex and returns an unlock function.
// All mutating package operations must hold this lock for the duration of
// the handler to prevent races between e.g. uninstall+purge and install.
func (s *SystemControllerHandlers) lockPackage(repo, name string) func() {
	key := repo + "/" + name
	actual, _ := s.packageMu.LoadOrStore(key, &sync.Mutex{})
	mu, ok := actual.(*sync.Mutex)
	if !ok {
		// Should never happen: we only store *sync.Mutex values.
		mu = &sync.Mutex{}
		s.packageMu.Store(key, mu)
	}
	mu.Lock()
	return mu.Unlock
}

func getHandler(ctx context.Context, sc systemControllerBackend) *SystemControllerHandlers {
	return &SystemControllerHandlers{Controller: sc, ctx: ctx}
}

// --- Routes ---

func (s *SystemControllerHandlers) configureRoutes(e *echo.Echo) {
	// Public
	e.Add("GET", "/status/ping", s.ping)
	e.Add("GET", "/tls/ca.crt", s.getTLSCA)
	// Prometheus scrapes this over loopback, which is why it is localhost-or-
	// admin rather than public: it aggregates account counts, disk usage, and
	// which services are down — a map of what to attack and when the box is
	// least able to resist. Prometheus runs --net host, so loopback reaches it
	// with no podman-network hop, exactly like the node-exporter target.
	e.Add("GET", MetricsPath, s.metricsHandler, s.localhostOrAdmin)
	e.Add("POST", "/account/authenticate", s.authenticateAccount)

	// Self-authenticated (handlers do own token validation)
	e.Add("GET", "/account/sessions", s.listSessions)
	e.Add("GET", "/account/me", s.sessionUsername)
	e.Add("POST", "/account/session/revoke", s.revokeSession)

	// Authenticated (requireAuth)
	e.Add("POST", "/storage/create", s.createFilesystem, s.requireAuth)
	e.Add("POST", "/storage/modify", s.modifyFilesystem, s.requireAuth)
	e.Add("POST", "/storage/remove", s.removeFilesystem, s.requireAuth)
	e.Add("POST", "/storage", s.listFilesystems, s.requireAuth)
	e.Add("POST", "/storage/package-volumes", s.listPackageVolumes, s.requireAuth)
	// The picker behind a `shared_volume` question: volumes installed packages
	// have marked `exported: true` and so offer to the rest of the box.
	e.Add("POST", "/storage/exported-volumes", s.exportedVolumes, s.requireAuth)

	// Object storage partitions. These four paths are a CONTRACT with gfeh
	// (TOWNOS_CONTRACT.md in the gfeh repo) and gfeh's `make check-townos-sync`
	// verifies them against this file — the method, the path, and the body
	// shape. They exist separately from /storage/* because createFilesystem
	// rewrites every submitted name to user/<name> unconditionally, so that
	// route cannot produce a volume under the gfeh/ prefix.
	//
	// Creation is admin-only because provisioning a partition is also creating
	// the root of a permission tree; everything below that root is self-service.
	// Deliberately absent from networkOnlyAllowedRoutes: the allowlist is
	// fail-closed, and a scoped account holding a live tunnel into the overlay
	// has no business provisioning storage.
	e.Add("POST", "/gfeh/partitions/create", s.createGfehPartition, s.requireAdmin)
	e.Add("POST", "/gfeh/partitions/modify", s.modifyGfehPartition, s.requireAdmin)
	e.Add("POST", "/gfeh/partitions/remove", s.removeGfehPartition, s.requireAdmin)
	e.Add("POST", "/gfeh/partitions", s.listGfehPartitions, s.requireAuth)

	// The UI's view of object storage. gfehd's admin surface is a Unix socket
	// that checks no credential -- filesystem permissions are its whole access
	// control -- so a browser cannot reach it and these proxy to it after
	// authenticating the caller. Reads are requireAuth (GET /account already
	// exposes the account list, so who holds which grant is no more sensitive).
	//
	// Writes are requireObjectStorage: an administrator, or an account an
	// administrator gave the object-storage capability to. Running a
	// partition's user database is a day-to-day job -- adding a family member,
	// revoking a share -- and requiring full control of the box to do it means
	// the only person who can is the one who can also uninstall every package.
	// Provisioning the partitions themselves stays admin-only above.
	e.Add("GET", "/gfeh", s.listGfeh, s.requireAuth)
	e.Add("GET", "/gfeh/principals", s.listGfehPrincipals, s.requireAuth)
	e.Add("POST", "/gfeh/principals/add", s.addGfehPrincipal, s.requireObjectStorage)
	e.Add("POST", "/gfeh/principals/remove", s.removeGfehPrincipal, s.requireObjectStorage)
	e.Add("GET", "/gfeh/grants", s.listGfehGrants, s.requireAuth)
	e.Add("POST", "/gfeh/grants/add", s.addGfehGrant, s.requireObjectStorage)
	e.Add("POST", "/gfeh/grants/revoke", s.revokeGfehGrant, s.requireObjectStorage)
	e.Add("GET", "/gfeh/exposures", s.listGfehExposures, s.requireAuth)
	e.Add("POST", "/gfeh/exposures/withdraw", s.withdrawGfehExposure, s.requireObjectStorage)

	e.Add("POST", "/storage/remove-package-volume", s.removePackageVolume, s.requireAdmin)
	e.Add("POST", "/storage/remove-package-volume-group", s.removePackageVolumeGroup, s.requireAdmin)

	// A repository is a code-supply channel, not a bookmark: whatever a
	// repository serves becomes systemd units, container images, and volume
	// seeds on this box the moment somebody installs from it. Registering one
	// is therefore an administrator's decision — the same one /repository/move
	// already required, which is what made the other three reading as
	// requireAuth look like an oversight rather than a policy. Listing stays
	// open; knowing which repositories are configured grants nothing.
	e.Add("POST", "/repository/add", s.addRepository, s.requireAdmin)
	e.Add("POST", "/repository/remove", s.removeRepository, s.requireAdmin)
	e.Add("POST", "/repository/move", s.moveRepository, s.requireAdmin)
	e.Add("POST", "/repository/refresh", s.refreshRepositories, s.requireAdmin)
	e.Add("GET", "/repository", s.listRepositories, s.requireAuth)

	e.Add("GET", "/packages", s.listPackages, s.requireAuth)
	e.Add("GET", "/packages/by-repo", s.listPackagesByRepo, s.requireAuth)
	e.Add("GET", "/packages/featured", s.listFeaturedPackages, s.requireAuth)
	e.Add("POST", "/packages/versions", s.listPackageVersions, s.requireAuth)
	e.Add("GET", "/packages/installed", s.listInstalled, s.requireAuth)
	// Saved responses are the answers to a package's questions, and a question
	// of `type: secret` or `type: oauth` is answered with a credential — a
	// database password, a Plex token. These two return them verbatim, so they
	// are admin-only; both are called from the install dialog, which is an
	// administrator's screen already.
	//
	// /packages/installed/info stays requireAuth because the dashboard renders
	// every service's notes for every account. It redacts instead: a non-admin
	// gets the notes and nothing else. See getInstalledInfo.
	e.Add("POST", "/packages/installed/info", s.getInstalledInfo, s.requireAuth)
	e.Add("POST", "/packages/responses", s.getResponses, s.requireAdmin)
	e.Add("POST", "/packages/last-responses", s.getLastResponses, s.requireAdmin)
	e.Add("POST", "/packages/manifest", s.packageManifest, s.requireAuth)
	e.Add("POST", "/packages/clear-last-responses", s.clearLastResponses, s.requireAdmin)

	e.Add("GET", "/systemd/units", s.listUnits, s.localhostOrAuth)
	e.Add("GET", "/systemd/units-tree", s.listUnitsTree, s.localhostOrAuth)
	// The journal is admin-only: a package's ExecStart line carries its whole
	// environment, so `-e POSTGRES_PASSWORD=…` is in the log of every unit
	// start, and the `unit` parameter is free-form (empty or __system__ means
	// the entire host journal). See localhostOrAdmin.
	e.Add("GET", "/systemd/logs", s.logReplay, s.localhostOrAdmin)
	e.Add("GET", "/systemd/logs/tail", s.logTail, s.localhostOrAdmin)
	e.Add("GET", "/systemd/logs/tree", s.treeLogReplay, s.localhostOrAdmin)
	e.Add("GET", "/systemd/logs/tree/tail", s.treeLogTail, s.localhostOrAdmin)

	e.Add("POST", "/account/create", s.createAccount)
	e.Add("POST", "/account", s.getAccount, s.requireAuth)
	e.Add("POST", "/account/update", s.updateAccount, s.requireAuth)
	e.Add("GET", "/account", s.listAccounts, s.requireAuth)

	// Admin (requireAdmin, which implies auth)
	e.Add("POST", "/packages/questions", s.getPackageQuestions, s.requireAdmin)
	e.Add("POST", "/packages/questions/identity", s.getPackageQuestionsByIdentity, s.requireAdmin)
	e.Add("POST", "/packages/children", s.listChildren, s.requireAuth)
	// An oauth question is answered by running a device flow from the install
	// dialog. Admin-only: starting a flow makes the controller fetch a URL the
	// package named, and completing one yields a credential.
	e.Add("POST", "/packages/oauth/start", s.startOAuth, s.requireAdmin)
	e.Add("POST", "/packages/oauth/poll", s.pollOAuth, s.requireAdmin)
	e.Add("POST", "/packages/install-preview", s.installPreview, s.requireAuth)
	e.Add("POST", "/packages/install", s.installPackage, s.requireAdmin)
	e.Add("POST", "/packages/uninstall", s.uninstallPackage, s.requireAdmin)
	e.Add("POST", "/packages/purge-volumes", s.purgeVolumes, s.requireAdmin)
	e.Add("POST", "/packages/uninstalled-volumes", s.listUninstalledVolumes, s.requireAuth)
	e.Add("POST", "/packages/purge-uninstalled-volumes", s.purgeUninstalledVolumes, s.requireAdmin)
	e.Add("GET", "/packages/upgrades", s.listUpgrades, s.requireAuth)
	e.Add("POST", "/packages/upgrades/dismiss", s.dismissUpgrades, s.requireAdmin)
	e.Add("POST", "/packages/disable", s.disablePackage, s.requireAdmin)
	e.Add("POST", "/packages/enable", s.enablePackage, s.requireAdmin)
	e.Add("POST", "/systemd/status", s.setUnitStatus, s.requireAdmin)
	e.Add("POST", "/systemd/status/tree", s.setUnitStatusTree, s.requireAdmin)
	e.Add("POST", "/account/disable", s.disableAccount, s.requireAdmin)
	e.Add("POST", "/account/enable", s.enableAccount, s.requireAdmin)
	// Audit detail is the sanitized request body of every administrative
	// action, which includes install bodies — and an install body is a map of
	// question answers. The sanitizer redacts what it can recognize; the access
	// check is what stops the rest from being readable by anyone with a login.
	e.Add("POST", "/audit/log", s.listAuditLog, s.localhostOrAdmin)
	e.Add("GET", "/settings", s.getSettings, s.requireAdmin)
	e.Add("POST", "/settings/get", s.getSetting, s.requireAdmin)
	e.Add("POST", "/settings/set", s.setSetting, s.requireAdmin)
	e.Add("POST", "/storage/upload-archive", s.uploadArchive, s.requireAdmin)
	e.Add("POST", "/storage/download-archive", s.downloadArchive, s.requireAdmin)
	e.Add("POST", "/packages/rebuild-git", s.rebuildGit, s.requireAdmin)
	e.Add("POST", "/pages/create", s.createPage, s.requireAdmin)
	e.Add("POST", "/pages/update", s.updatePage, s.requireAdmin)
	e.Add("POST", "/pages/remove", s.removePage, s.requireAdmin)
	e.Add("POST", "/pages/rebuild", s.rebuildPage, s.requireAdmin)
	e.Add("POST", "/pages/upload", s.uploadPageArchive, s.requireAdmin)
	e.Add("GET", "/pages", s.listPages, s.requireAuth)

	e.Add("GET", "/vm-images", s.listVMImages, s.requireAuth)
	e.Add("POST", "/vm-images/upload", s.uploadVMImage, s.requireAdmin)
	e.Add("POST", "/vm-images/delete", s.deleteVMImage, s.requireAdmin)

	e.Add("GET", "/locales", s.listLocales, s.requireAuth)

	// Monitoring
	e.Add("GET", "/monitoring/status", s.monitoringStatus, s.requireAuth)

	// System Services
	e.Add("GET", "/system-services", s.listSystemServices, s.localhostOrAuth)
	e.Add("POST", "/system-services/status", s.setSystemServiceStatus, s.requireAdmin)
	// localhostOrAdmin, not requireAdmin: the daily update timer
	// (maintenance_update.go) POSTs here over loopback with no credentials to
	// hold. Off-box callers still need admin. The exemption is sound only
	// because the source address cannot be spoofed onto loopback from the
	// network — the same reasoning the metrics route above relies on.
	e.Add("POST", "/system-services/refresh", s.refreshSystemServices, s.localhostOrAdmin)

	// DNS
	e.Add("GET", "/dns/status", s.dnsStatus, s.requireAuth)
	e.Add("GET", "/dns/records", s.listDNSRecords, s.requireAuth)
	e.Add("POST", "/dns/records/add", s.addDNSRecord, s.requireAdmin)
	e.Add("POST", "/dns/records/remove", s.removeDNSRecord, s.requireAdmin)
	e.Add("GET", "/dns/tld", s.getDNSTLD, s.requireAuth)
	e.Add("POST", "/dns/tld", s.setDNSTLD, s.requireAdmin)
	e.Add("POST", "/dns/setup", s.setupDNS, s.requireAdmin)

	// DNSBL (spam/malware/ad blocklists via rolodex)
	e.Add("GET", "/dns/dnsbl", s.getDnsblConfig, s.requireAuth)
	e.Add("POST", "/dns/dnsbl", s.setDnsblConfig, s.requireAdmin)
	// The local blocklist keeps its historical /dns/rbl/local paths: they are a
	// published HTTP contract with the UI, and the entries themselves are
	// unchanged — only the RBL provider lookups that once shared the prefix are
	// gone.
	e.Add("GET", "/dns/rbl/local", s.listLocalBlocklistEntries, s.requireAuth)
	e.Add("POST", "/dns/rbl/local/add", s.addLocalBlocklistEntry, s.requireAdmin)
	e.Add("POST", "/dns/rbl/local/remove", s.removeLocalBlocklistEntry, s.requireAdmin)
	// The DNSBL allowlist: the escape hatch out of every name-based block.
	e.Add("GET", "/dns/dnsbl/allowlist", s.listDnsblAllowlistEntries, s.requireAuth)
	e.Add("POST", "/dns/dnsbl/allowlist/add", s.addDnsblAllowlistEntry, s.requireAdmin)
	e.Add("POST", "/dns/dnsbl/allowlist/remove", s.removeDnsblAllowlistEntry, s.requireAdmin)
	e.Add("GET", "/dns/services", s.listDNSServices, s.requireAuth)
	e.Add("POST", "/dns/services/set", s.setDNSService, s.requireAdmin)

	// Networks (per-network WireGuard overlays paired with DNS)
	e.Add("GET", "/networks", s.listNetworks, s.requireAuth)
	e.Add("GET", "/networks/peers", s.listNetworkPeers, s.requireAuth)
	// Deliberately absent from networkOnlyAllowedRoutes: this aggregates every
	// account's peers and observed source addresses across every network, which a
	// scoped network-only account has no business enumerating. The allowlist is
	// fail-closed, so omitting it here is what denies it.
	e.Add("GET", "/networks/peers/connected", s.listConnectedPeers, s.requireAdmin)
	e.Add("POST", "/networks/create", s.createNetwork, s.requireAdmin)
	e.Add("POST", "/networks/remove", s.removeNetwork, s.requireAdmin)
	e.Add("POST", "/networks/enable", s.enableNetwork, s.requireAdmin)
	e.Add("POST", "/networks/disable", s.disableNetwork, s.requireAdmin)
	// peers/add and peers/refresh admit network-only accounts (scope and
	// ownership enforced in the handlers), not just admins.
	e.Add("POST", "/networks/peers/add", s.addNetworkPeer, s.requirePeerEnroll)
	e.Add("POST", "/networks/peers/refresh", s.refreshNetworkPeer, s.requirePeerEnroll)
	e.Add("POST", "/networks/peers/remove", s.removeNetworkPeer, s.requireAdmin)
}

// --- Server infrastructure ---

type ServerConfig struct {
	Storage                  storage.Storage
	RepositoryRoot           *packages.RepositoryRoot
	Installer                packages.Installer
	Systemd                  systemd.Manager
	AccountMgr               account.Manager
	SessionMgr               account.SessionManager
	AuditMgr                 account.AuditManager
	SettingsMgr              account.SettingsManager
	Git                      git.Client
	AllowedHosts             []string
	DefaultRepoUser          string
	DefaultRepoPass          string
	BtrfsBasePath          string
	NetworkControllerImage string
	NetworkStatePath       string
	// DiskDevices lists kernel device basenames (e.g. "sda3", "nvme0n1p3")
	// of every block device backing the btrfs filesystem mounted at
	// BtrfsBasePath. Discovered once at startup via monitoring.BtrfsDevices
	// and surfaced through /monitoring/status so the uPlot frontend and
	// Grafana dashboard can sum node_disk_* metrics over only those
	// devices. Nil when discovery fails.
	DiskDevices []string
	// SwapCapability records whether the btrfs pool at BtrfsBasePath can host
	// a swapfile at all, probed once at startup (the layout cannot change
	// without re-formatting). Reported through /status/ping so a box that can
	// never have swap — any multi-disk pool, since btrfs refuses to swap on a
	// multi-device filesystem — explains itself instead of silently having
	// none. Live usage is read per request; only this static half is cached.
	SwapCapability monitoring.SwapCapability
	// MonitoringPorts are the host ports the three monitoring system services
	// bind. The zero value means the documented defaults (9100/9090/5308); the
	// integration harness sets ephemeral ports so a test box never collides
	// with a dev or production box in the shared host network namespace.
	MonitoringPorts monitoring.Ports
	// ScrapeTargetsFunc asks Prometheus which of its scrape jobs are
	// answering, for /monitoring/status. nil means the real query
	// (monitoring.FetchScrapeTargets) against the loopback Prometheus; a test
	// sets it so a status poll never reaches for a port in the host namespace.
	ScrapeTargetsFunc ScrapeTargetsFunc
	PagesMgr                 account.PagesManager
	NetworkMgr               account.NetworkManager
	GitCloner                packages.GitCloner
	MonitoringBackend        string
	Rolodex                  *rolodex.Manager
	RolodexClient            rolodex.Client
	Ingress                  *ingressctl.Manager
	IngressClient            ingress.Client
	UI                       *ui.Manager
	// GfehRegistry is the set of running object-storage partitions, one per
	// network. Nil when GFEH_IMAGE is empty (dev mode), in which case every
	// gfeh route reports that object storage is not configured and the name
	// collectors contribute nothing.
	GfehRegistry GfehRegistry
	ImageExtractFunc         func(ctx context.Context, image, directory, targetPath string) error
	// ContainerExecFunc runs a package's post_install commands inside its
	// container. nil selects PodmanContainerExec.
	ContainerExecFunc ContainerExecFunc
	// ResolvedConfigurator is called after DNS reconcile or TLD change to
	// configure systemd-resolved routing for the TLD. When nil, the call
	// is skipped. Set to rolodex.ConfigureResolvedRouting in production.
	ResolvedConfigurator func(ctx context.Context, tld, loopbackAddr string)
	// SystemControllerImage is the container image reference that the
	// systemcontroller's own systemd unit runs. When non-empty, the
	// systemcontroller is listed as a system service in the /system-services
	// API and UI. Set from main.go using the image tag from resolveImageTag()
	// (TOWN_OS_TAG env var, else the rc.latest-<arch> default).
	SystemControllerImage string
	// SystemControllerListenAddr is the address the systemcontroller's
	// HTTP server listens on (e.g. ":5309"). Used for display in the
	// /system-services entry.
	SystemControllerListenAddr string

	// BootID identifies this process incarnation. main.go sets it from
	// BootStatus.BootID() so the full router's /status/ping reports the
	// same id the boot stub did. The refresh UI captures the id before
	// asking for a restart and waits for it to change — without it, the
	// still-alive outgoing process is indistinguishable from the booted
	// incoming one (both answer ping 200 and 404 /boot-status).
	BootID string

	// OAuthAllowPrivate lets an OAuth device flow call a private or loopback
	// address. It exists so tests can point a flow at an httptest server; in
	// production it stays false, and packages.CheckOAuthAddr refuses to let the
	// controller -- which runs as root on the host -- be aimed at the host's own
	// network by a URL a package named.
	OAuthAllowPrivate bool

	// TLSCA is the local X.509 root used to issue per-package leaf certs
	// for HTTP-supplying packages. nil disables TLS termination and leaves
	// HTTP endpoints as plain TCP forwarders.
	TLSCA *townostls.CA

	// AuthDisabled turns off every authentication and authorization check,
	// serving each route as though the caller were an administrator.
	//
	// It exists for tests, and it is a field rather than an inference for one
	// reason: the auth middleware used to derive the same condition from
	// SessionMgr being nil, which made "authentication is not configured" and
	// "authentication is wide open" the same state. The whole authorization
	// surface then sat one unset field away from admitting everybody, and
	// nothing in the type system said so — a production path that failed to
	// wire a session manager would have served /account/create and
	// /packages/install to an anonymous caller with no error anywhere.
	//
	// Stated explicitly, a missing session manager is a configuration failure
	// that InitServer refuses to start with, and only a caller who asked for
	// no auth gets none. InitTestServer asks for it on behalf of the ~230 test
	// call sites that never install a session manager; a test that does
	// install one keeps its auth enforced exactly as before.
	AuthDisabled bool
}

func withContext(parent context.Context, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-parent.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
		handler.ServeHTTP(w, r.WithContext(ctx)) //nolint:contextcheck // parent is monitored via goroutine above
	})
}
