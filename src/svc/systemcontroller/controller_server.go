package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	"github.com/labstack/echo/v5/middleware"
)

// Defaults for the network poller. Internal IP is checked every tick;
// external IP is fetched every defaultPollerExternalEvery ticks (12 × 5 min
// = once per hour, matching the pre-poller cadence). A new internal IP must
// be observed on defaultPollerStableTicks consecutive ticks before it is
// promoted, so a flapping veth or DHCP renewal blip does not trigger a
// spurious DNS reconcile.
const (
	defaultPollerInternalIPInterval = 5 * time.Minute
	defaultPollerExternalEvery      = 12
	defaultPollerStableTicks        = 3
	// DNS drift-repair tick. ReconcileDNS is a diff against rolodex — the
	// normal case is a no-op, so polling hourly is cheap and catches any
	// drift introduced by a rolodex restart or a dropped install/uninstall
	// DNS call without waiting for the next systemcontroller restart.
	defaultPollerDNSInterval = 1 * time.Hour

	// Object-storage convergence tick.
	//
	// Every comment in the gfeh reconcile path calls the periodic reconcile its
	// backstop — "the next reconcile will try again", "the periodic reconcile is
	// the backstop", "the names are published by the next reconcile" — and until
	// this existed there was no next reconcile. ReconcileGfeh ran once at boot
	// and again after network CRUD, so a partition that lost a race at boot (the
	// image still pulling, podman not yet up, the ingress network not yet
	// created) stayed down until somebody restarted the controller. That is the
	// "object storage never works on boot" report: it is not that boot is wrong,
	// it is that boot was the only attempt.
	//
	// Five minutes rather than the DNS tick's hour: this is the pass that gets a
	// partition running at all, and an hour of dead object storage after a cold
	// boot is indistinguishable from broken. Cheap at steady state — an
	// unchanged config and unit means no restart, and a republish only happens
	// when the set of answering partitions actually moved.
	defaultPollerGfehInterval = 5 * time.Minute

	// externalIPFetchTimeout bounds a single ipinfo.io request (DNS + TCP
	// connect + TLS handshake + body read). 30s is deliberately generous:
	// a DNS-degraded box routinely spent >10s here pre-fix, and a silent
	// failure would leave the dashboard missing external_ip for up to an
	// hour until the next hourly poll.
	externalIPFetchTimeout = 30 * time.Second
	defaultExternalIPURL   = "https://ipinfo.io/json"
)

// externalIPStartupBackoffs controls the startup retry cadence for the
// external-IP fetch. The first attempt happens immediately; subsequent
// attempts wait for the listed duration before firing. Chosen so a
// transient DNS/TLS hiccup at boot is retried within minutes rather
// than waiting for the hourly poll. Overridable via the
// externalIPStartupBackoffs field on serverBase for tests.
var defaultExternalIPStartupBackoffs = []time.Duration{
	0,
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
}

type serverBase struct {
	ServerConfig

	externalIP   atomic.Value // stores string
	internalIP   atomic.Value // stores string
	internalIPv6 atomic.Value // stores string (host's global IPv6, or "")

	// Network-poller state. pollerMu guards the mutable fields below.
	// pollerPendingInternal and pollerPendingTicks track an internal-IP
	// candidate that has not yet been seen on enough consecutive ticks to
	// be promoted into internalIP. pollerExternalTickCnt counts internal
	// ticks until the next external-IP fetch.
	pollerMu              sync.Mutex
	pollerPendingInternal string
	pollerPendingTicks    int
	pollerExternalTickCnt int

	// Tunables and overrides. Zero values mean "use the constants above".
	// pollerInternalDiscoverer and pollerReconcileDNS are nil in production
	// and set by unit tests to drive tickNetworkPoll synchronously without
	// touching real interfaces or rolodex.
	pollerInternalIPInterval time.Duration
	pollerExternalEvery      int
	pollerStableTicks        int
	pollerDNSInterval        time.Duration
	pollerGfehInterval       time.Duration
	pollerInternalDiscoverer   func() string
	pollerInternalV6Discoverer func() string
	pollerReconcileDNS         func(ctx context.Context, oldIP, newIP string) error
	// pollerDNSReconciler is nil in production (the tick calls ReconcileDNS
	// via rolodex); tests inject a fake to observe invocations without
	// wiring up a full rolodex.
	pollerDNSReconciler func(ctx context.Context) error

	// gfehLastReady is the set of object-storage partitions that were answering
	// at the last tick, sorted. The tick republishes on a difference against
	// this rather than against the start of its own pass — see tickGfehPoll.
	// Its own mutex: the gfeh tick runs on a separate goroutine from the
	// network poller, and nothing else reads this.
	gfehMu        sync.Mutex
	gfehLastReady []string

	// externalIPURL overrides defaultExternalIPURL; tests set it to an
	// httptest.Server URL. externalIPStartupBackoffs overrides
	// defaultExternalIPStartupBackoffs; tests shrink the waits. An empty
	// slice disables the startup retry entirely.
	externalIPURL             string
	externalIPStartupBackoffs []time.Duration

	// extFailWarnLogged tracks whether the "external IP fetch failed"
	// Warn log has already fired. We log the FIRST failure at Warn so a
	// DNS-degraded box surfaces the problem at default log level, then
	// drop to Debug for subsequent failures so the journal doesn't fill
	// up while the hourly poll keeps retrying.
	extFailMu        sync.Mutex
	extFailWarnLogged bool
}

func (s *serverBase) GetOAuthAllowPrivate() bool                   { return s.OAuthAllowPrivate }
func (s *serverBase) GetStorage() storage.Storage                 { return s.Storage }
func (s *serverBase) GetRepositoryRoot() *packages.RepositoryRoot { return s.RepositoryRoot }
func (s *serverBase) GetInstaller() packages.Installer            { return s.Installer }
func (s *serverBase) GetSystemdManager() systemd.Manager          { return s.Systemd }
func (s *serverBase) GetAccountManager() account.Manager          { return s.AccountMgr }
func (s *serverBase) GetSessionManager() account.SessionManager   { return s.SessionMgr }
func (s *serverBase) IsAuthDisabled() bool                        { return s.AuthDisabled }
func (s *serverBase) GetAuditManager() account.AuditManager       { return s.AuditMgr }
func (s *serverBase) GetSettingsManager() account.SettingsManager { return s.SettingsMgr }
func (s *serverBase) GetGitClient() git.Client {
	if s.Git != nil {
		return s.Git
	}
	return &git.GoGitClient{}
}
func (s *serverBase) GetAllowedHosts() []string                   { return s.AllowedHosts }
func (s *serverBase) GetDefaultRepoCredentials() (string, string) {
	return s.DefaultRepoUser, s.DefaultRepoPass
}
func (s *serverBase) GetBtrfsBasePath() string          { return s.BtrfsBasePath }
func (s *serverBase) GetNetworkControllerImage() string { return s.NetworkControllerImage }
func (s *serverBase) GetNetworkStatePath() string       { return s.NetworkStatePath }
func (s *serverBase) GetGitCloner() packages.GitCloner {
	if s.GitCloner != nil {
		return s.GitCloner
	}
	return packages.DefaultGitCloner{}
}
func (s *serverBase) GetPagesManager() account.PagesManager       { return s.PagesMgr }
func (s *serverBase) GetNetworkManager() account.NetworkManager   { return s.NetworkMgr }
func (s *serverBase) GetMonitoringBackend() string                { return s.MonitoringBackend }
func (s *serverBase) GetDiskDevices() []string              { return s.DiskDevices }
func (s *serverBase) GetMonitoringPorts() monitoring.Ports  { return s.MonitoringPorts }

// RefreshMonitoringBackend switches the monitoring UI to the given backend
// by regenerating and restarting the monitoring-ui system service. The
// MonitoringBackend field is updated so subsequent reads reflect the change.
func (s *serverBase) RefreshMonitoringBackend(ctx context.Context, backend string) error {
	s.MonitoringBackend = backend
	sd := s.GetSystemdManager()
	if sd == nil {
		return nil
	}
	return monitoring.StartMonitoringUI(ctx, sd, s.Storage, backend, s.BtrfsBasePath, s.NetworkControllerImage, s.DiskDevices, s.MonitoringPorts)
}
// RefreshDNSResolutionMode switches rolodex between resolving unmatched names
// iteratively from the root servers ("recursive") and forwarding them to the
// upstream resolvers already written into rolodex.yml ("forward"). It rewrites
// the config and restarts the rolodex unit so the change takes effect without a
// reboot.
//
// RewriteConfig (not WriteConfig) is deliberate: WriteConfig refuses to
// overwrite a rolodex.yml newer than the systemcontroller binary, and the file
// written at the last boot always is — so WriteConfig would no-op here and the
// setting would never reach rolodex.
func (s *serverBase) RefreshDNSResolutionMode(ctx context.Context, mode string) error {
	if s.Rolodex == nil {
		return nil
	}
	if !rolodex.ValidResolutionMode(mode) {
		return fmt.Errorf("invalid dns resolution mode %q", mode)
	}
	if s.Rolodex.ResolutionMode() == mode {
		return nil
	}

	s.Rolodex.SetResolutionMode(mode)
	written, err := s.Rolodex.RewriteConfig()
	if err != nil {
		return fmt.Errorf("write rolodex config: %w", err)
	}
	if !written {
		return nil
	}

	sd := s.GetSystemdManager()
	if sd == nil {
		return nil
	}
	if err := sd.SetStatus(ctx, s.Rolodex.UnitName(), "restart"); err != nil {
		return fmt.Errorf("restart rolodex: %w", err)
	}
	return nil
}

// RefreshDNSLocalForwarders switches rolodex's forwarder list between the
// public defaults and the resolvers this box's own network handed it — the
// addresses that keep answering on a network that blocks external DNS. It
// rewrites the config and restarts the rolodex unit so the change takes effect
// without a reboot.
//
// Unlike RefreshDNSResolutionMode it does not short-circuit when the flag is
// unchanged: with the flag already on, the discovered addresses themselves can
// have moved (a new DHCP lease, a different network), and re-rendering is how
// that reaches rolodex. RewriteConfig reports whether the bytes actually
// changed, so an identical render still costs no restart.
//
// RewriteConfig (not WriteConfig) for the same reason as the resolution mode:
// the rolodex.yml written at the last boot is always newer than the
// systemcontroller binary, which WriteConfig treats as user-modified and
// refuses to touch.
func (s *serverBase) RefreshDNSLocalForwarders(ctx context.Context, enabled bool) error {
	if s.Rolodex == nil {
		return nil
	}

	s.Rolodex.SetLocalForwarders(enabled)
	written, err := s.Rolodex.RewriteConfig()
	if err != nil {
		return fmt.Errorf("write rolodex config: %w", err)
	}
	if !written {
		return nil
	}

	sd := s.GetSystemdManager()
	if sd == nil {
		return nil
	}
	if err := sd.SetStatus(ctx, s.Rolodex.UnitName(), "restart"); err != nil {
		return fmt.Errorf("restart rolodex: %w", err)
	}
	return nil
}

func (s *serverBase) GetRolodex() *rolodex.Manager           { return s.Rolodex }
func (s *serverBase) GetIngress() *ingressctl.Manager           { return s.Ingress }
func (s *serverBase) GetUI() *ui.Manager                     { return s.UI }
func (s *serverBase) GetGfehRegistry() GfehRegistry           { return s.GfehRegistry }
func (s *serverBase) GetResolvedConfigurator() func(ctx context.Context, tld, loopbackAddr string) {
	return s.ResolvedConfigurator
}
func (s *serverBase) GetSystemControllerImage() string      { return s.SystemControllerImage }
func (s *serverBase) GetSystemControllerListenAddr() string { return s.SystemControllerListenAddr }
func (s *serverBase) GetBootID() string                     { return s.BootID }
func (s *serverBase) GetTLSCA() *townostls.CA               { return s.TLSCA }
func (s *serverBase) GetImageExtractFunc() func(ctx context.Context, image, directory, targetPath string) error {
	if s.ImageExtractFunc != nil {
		return s.ImageExtractFunc
	}
	return reconcileExtractFromImage
}
func (s *serverBase) GetRolodexClient() rolodex.Client {
	if s.RolodexClient != nil {
		return s.RolodexClient
	}
	if s.Rolodex == nil {
		return nil
	}
	socketPath := s.Rolodex.SocketPath()
	if socketPath == "" {
		return nil
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := rolodex.Dial(dialCtx, socketPath)
	if err != nil {
		slog.Debug(fmt.Sprintf("lazy rolodex dial: %v", err))
		return nil
	}
	s.RolodexClient = c
	return c
}

// GetIngressClient lazily dials the ingress gRPC socket the first time it is
// needed and caches the connection, mirroring GetRolodexClient. Returns nil
// when the ingress manager is unset or the socket cannot be dialed yet.
func (s *serverBase) GetIngressClient() ingress.Client {
	if s.IngressClient != nil {
		return s.IngressClient
	}
	if s.Ingress == nil {
		return nil
	}
	socketPath := s.Ingress.SocketPath()
	if socketPath == "" {
		return nil
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dialCancel()
	c, err := ingress.Dial(dialCtx, socketPath)
	if err != nil {
		slog.Debug(fmt.Sprintf("lazy ingress dial: %v", err))
		return nil
	}
	s.IngressClient = c
	return c
}
func (s *serverBase) GetExternalIP() string {
	v := s.externalIP.Load()
	if v == nil {
		return ""
	}
	if ip, ok := v.(string); ok {
		return ip
	}
	return ""
}

func (s *serverBase) GetInternalIP() string {
	v := s.internalIP.Load()
	if v == nil {
		s.RefreshInternalIP()
		v = s.internalIP.Load()
		if v == nil {
			return ""
		}
	}
	if ip, ok := v.(string); ok {
		return ip
	}
	return ""
}

// GetInternalIPv6 returns the host's global IPv6 address (from the same
// interface as GetInternalIP), or "" when the host has no globally routable
// IPv6. Lazily refreshes the cache on first read, mirroring GetInternalIP.
func (s *serverBase) GetInternalIPv6() string {
	v := s.internalIPv6.Load()
	if v == nil {
		s.RefreshInternalIP()
		v = s.internalIPv6.Load()
		if v == nil {
			return ""
		}
	}
	if ip, ok := v.(string); ok {
		return ip
	}
	return ""
}

// discoverInternalIP returns the IPv4 address of the host's primary physical
// interface, or "" if none can be discovered. Tests inject a stub via
// pollerInternalDiscoverer.
func (s *serverBase) discoverInternalIP() string {
	if s.pollerInternalDiscoverer != nil {
		return s.pollerInternalDiscoverer()
	}
	ipv4, _ := InternalInterfaceIPs()
	return ipv4
}

// discoverInternalIPv6 returns the global IPv6 address of the same interface
// discoverInternalIP selects, or "" if there is none. Tests inject a stub via
// pollerInternalV6Discoverer.
func (s *serverBase) discoverInternalIPv6() string {
	if s.pollerInternalV6Discoverer != nil {
		return s.pollerInternalV6Discoverer()
	}
	_, ipv6 := InternalInterfaceIPs()
	return ipv6
}

// RefreshInternalIP updates the cached internal IPv4 and global IPv6 addresses
// by querying the system's network interfaces. Empty results leave the
// respective cache entry untouched.
func (s *serverBase) RefreshInternalIP() {
	if ip := s.discoverInternalIP(); ip != "" {
		s.internalIP.Store(ip)
	}
	if ip6 := s.discoverInternalIPv6(); ip6 != "" {
		s.internalIPv6.Store(ip6)
	}
}

func (s *serverBase) pollerStableTicksValue() int {
	if s.pollerStableTicks > 0 {
		return s.pollerStableTicks
	}
	return defaultPollerStableTicks
}

func (s *serverBase) pollerExternalEveryValue() int {
	if s.pollerExternalEvery > 0 {
		return s.pollerExternalEvery
	}
	return defaultPollerExternalEvery
}

func (s *serverBase) pollerInternalIntervalValue() time.Duration {
	if s.pollerInternalIPInterval > 0 {
		return s.pollerInternalIPInterval
	}
	return defaultPollerInternalIPInterval
}

func (s *serverBase) pollerDNSIntervalValue() time.Duration {
	if s.pollerDNSInterval > 0 {
		return s.pollerDNSInterval
	}
	return defaultPollerDNSInterval
}

func (s *serverBase) pollerGfehIntervalValue() time.Duration {
	if s.pollerGfehInterval > 0 {
		return s.pollerGfehInterval
	}
	return defaultPollerGfehInterval
}

// tickGfehPoll runs one iteration of the object-storage convergence loop: it
// re-converges the partitions onto the network set, and republishes the derived
// names when — and only when — the set of answering partitions moved.
//
// Both halves matter. The reconcile is what actually brings up a partition that
// failed at boot: it re-renders the config, re-installs and restarts the unit if
// either changed, and waits for the socket. The republish is what makes a
// partition that has just come up *reachable*: its names are asked for at
// rebuild time, so a partition that started after the last rebuild contributes
// nothing to DNS or the ingress until the next one, and the ingress has no other
// periodic pass at all.
//
// Guarded on the ready set rather than run unconditionally: RebuildIngress
// pushes the whole route set and RebuildDNS tears down and rebuilds the zone, so
// doing that every five minutes for no reason is a reload storm and a window in
// which names briefly do not resolve.
//
// **The comparison is against the last tick, not against the start of this
// one.** Most partitions do not come up because this reconcile started them —
// the units carry Restart=always, so systemd brings them back on its own, and a
// cold-boot partition finishes pulling its image and starts answering somewhere
// between two ticks. Comparing the ready set before and after the reconcile
// inside a single tick only ever sees changes the reconcile itself caused, so
// exactly the case this poller exists for — a partition that came up by itself
// after boot gave up publishing — would be silently skipped.
//
// The remembered set starts empty, so the first tick on a box with a live
// partition republishes once. That is deliberate rather than tolerated: boot
// publishes names asynchronously and gives up if nothing answers in time, and
// this is the pass that covers it.
func (s *serverBase) tickGfehPoll(ctx context.Context) {
	reg := s.GetGfehRegistry()
	if reg == nil {
		return
	}

	ReconcileGfehRegistry(ctx, reg)
	ready := GfehReadyNetworks(ctx, reg)

	s.gfehMu.Lock()
	previous := s.gfehLastReady
	changed := !slices.Equal(previous, ready)
	if changed {
		s.gfehLastReady = ready
	}
	s.gfehMu.Unlock()

	if !changed {
		return
	}
	slog.Info("object-storage partitions changed state; republishing their names",
		"was", previous, "now", ready)

	getHandler(ctx, s).reprogramIngress(ctx)
	s.tickDNSPoll(ctx)
}

// tickDNSPoll runs one iteration of the hourly DNS drift-repair loop. It
// builds a ReconcileDNSConfig from the live server state and calls the
// idempotent diff-based ReconcileDNS — the common case is zero mutations
// in rolodex, so polling has essentially no cost at steady state. Tests
// inject pollerDNSReconciler to observe invocations without dialling
// rolodex.
func (s *serverBase) tickDNSPoll(ctx context.Context) {
	if s.pollerDNSReconciler != nil {
		if err := s.pollerDNSReconciler(ctx); err != nil {
			slog.Debug("hourly DNS reconcile (injected)", "error", err)
		}
		return
	}
	rolClient := s.GetRolodexClient() //nolint:contextcheck // GetRolodexClient uses its own short-lived dial context; see onInternalIPChange.
	if rolClient == nil {
		slog.Debug("hourly DNS reconcile: rolodex client unavailable")
		return
	}
	if err := ReconcileDNS(ctx, ReconcileDNSConfig{
		Client:         rolClient,
		Installer:      s.Installer,
		RepositoryRoot: s.RepositoryRoot,
		SettingsMgr:    s.SettingsMgr,
		PagesManager:   s.PagesMgr,
		InternalIP:     s.GetInternalIP(),
		InternalIPv6:   s.GetInternalIPv6(),
		// Without this the hourly drift pass treats every object-storage
		// record as an orphan and deletes it -- so gfeh would work at boot
		// and stop resolving an hour later, which is a far worse failure
		// than never working at all.
		Gfeh: s.GetGfehRegistry(),
	}); err != nil {
		slog.Debug("hourly DNS reconcile", "error", err)
	}
}

// tickNetworkPoll runs one cycle of the network poller. It discovers the
// current internal IPv4, applies "stable for N consecutive ticks" debouncing
// to changes, fires onInternalIPChange when a change is confirmed, and
// fetches the external IP every Nth tick. Exposed (lower-case but
// package-visible) so unit tests can drive the change-detection logic
// synchronously without spinning up the polling goroutine.
func (s *serverBase) tickNetworkPoll(ctx context.Context) {
	discovered := s.discoverInternalIP()

	// Refresh the cached global IPv6 every tick (no debounce — AAAA drift is
	// repaired by the hourly ReconcileDNS, which reads the cached value). Only
	// store non-empty results so a transient interface read does not churn it.
	if v6 := s.discoverInternalIPv6(); v6 != "" {
		s.internalIPv6.Store(v6)
	}

	s.pollerMu.Lock()

	var (
		oldIP, newIP string
		fireChange   bool
	)

	cached := ""
	if v, ok := s.internalIP.Load().(string); ok {
		cached = v
	}

	switch {
	case discovered == "":
		// Could not discover this tick; leave cache and pending state alone.
	case cached == "":
		// Prime the cache without firing a change. This is the first
		// successful discovery — there is no "old IP" to compare against,
		// so treat it as the baseline.
		s.internalIP.Store(discovered)
		s.pollerPendingInternal = ""
		s.pollerPendingTicks = 0
	case discovered == cached:
		// No change; reset any in-flight candidate.
		s.pollerPendingInternal = ""
		s.pollerPendingTicks = 0
	default:
		// Different from cache. Apply the stable-for-N debounce.
		if discovered == s.pollerPendingInternal {
			s.pollerPendingTicks++
		} else {
			s.pollerPendingInternal = discovered
			s.pollerPendingTicks = 1
		}
		if s.pollerPendingTicks >= s.pollerStableTicksValue() {
			oldIP = cached
			newIP = discovered
			fireChange = true
			s.internalIP.Store(discovered)
			s.pollerPendingInternal = ""
			s.pollerPendingTicks = 0
		}
	}

	s.pollerExternalTickCnt++
	fetchExternal := s.pollerExternalTickCnt >= s.pollerExternalEveryValue()
	if fetchExternal {
		s.pollerExternalTickCnt = 0
	}

	s.pollerMu.Unlock()

	if fireChange {
		s.onInternalIPChange(ctx, oldIP, newIP)
	}
	if fetchExternal {
		s.fetchExternalIP(ctx)
	}
}

// onInternalIPChange logs the transition and re-runs DNS reconcile so every
// installed package's A record points at the new address. Failures are
// logged but do not abort the poller — the next confirmed change will retry.
//
// Re-rendering compiled service units that baked in @LOCAL_INTERNAL_HOST@
// is a known gap; it requires a full reconcile pass and is left to a
// follow-up.
func (s *serverBase) onInternalIPChange(ctx context.Context, oldIP, newIP string) {
	if s.pollerReconcileDNS != nil {
		if err := s.pollerReconcileDNS(ctx, oldIP, newIP); err != nil {
			slog.Error("internal IP change handler", "old", oldIP, "new", newIP, "error", err)
			return
		}
		slog.Info("internal IP changed", "old", oldIP, "new", newIP)
		return
	}

	slog.Info("internal IP changed; rebuilding DNS", "old", oldIP, "new", newIP)

	// GetRolodexClient establishes its own short-lived dial context
	// internally; all other callers (controller_dns.go) use the same
	// no-arg form. Passing ctx through would require touching every
	// existing caller for no behaviour change.
	rolClient := s.GetRolodexClient() //nolint:contextcheck // see comment above
	if rolClient == nil {
		slog.Warn("internal IP changed but rolodex client unavailable; package DNS records may be stale", "new", newIP)
		return
	}

	// IP change means every A record's value is wrong — full wipe+rebuild
	// rather than an incremental diff (which would still update records,
	// but one-by-one in map-random order with an NXDOMAIN blip per name).
	if err := RebuildDNS(ctx, ReconcileDNSConfig{
		Client:           rolClient,
		Installer:        s.Installer,
		RepositoryRoot:   s.RepositoryRoot,
		SettingsMgr:      s.SettingsMgr,
		PagesManager:     s.PagesMgr,
		InternalIP:       newIP,
		InternalIPv6:     s.GetInternalIPv6(),
		NetworkStatePath: s.NetworkStatePath,
		BtrfsBasePath:    s.BtrfsBasePath,
		Gfeh:             s.GetGfehRegistry(),
	}); err != nil {
		slog.Error("rebuild DNS after internal IP change", "old", oldIP, "new", newIP, "error", err)
		return
	}

	// Non-default-network packages are dual-homed: their LAN-facing global record
	// also carries the box IP, so it goes stale on a change too. RebuildDNS only
	// covers the global home zone (collectInstalledDNSInfo excludes networked
	// packages), so re-pin the network-TLD LAN records here. No-op when the
	// network manager is unset.
	if err := RebuildNetworkDNS(ctx, ReconcileDNSConfig{
		Client:         rolClient,
		Installer:      s.Installer,
		RepositoryRoot: s.RepositoryRoot,
		SettingsMgr:    s.SettingsMgr,
		NetworkMgr:     s.GetNetworkManager(),
		// Pages on a non-default network carry the box IP in their LAN-facing
		// record too, so they go stale on an IP change exactly like packages.
		PagesManager: s.PagesMgr,
		InternalIP:   newIP,
		InternalIPv6: s.GetInternalIPv6(),
		// Needed so the network TLDs' DANE TLSA pins are republished alongside
		// the A records; empty would silently skip them (collectNetworkTLSA
		// no-ops without a state dir and btrfs base).
		NetworkStatePath: s.NetworkStatePath,
		BtrfsBasePath:    s.BtrfsBasePath,
		Gfeh:             s.GetGfehRegistry(),
	}); err != nil {
		slog.Error("rebuild network DNS after internal IP change", "old", oldIP, "new", newIP, "error", err)
		return
	}

	slog.Info("rebuilt package DNS records after internal IP change", "old", oldIP, "new", newIP)
}

// externalIPURLValue returns the configured external-IP URL or the default.
func (s *serverBase) externalIPURLValue() string {
	if s.externalIPURL != "" {
		return s.externalIPURL
	}
	return defaultExternalIPURL
}

// externalIPStartupBackoffsValue returns the configured startup backoff
// schedule or the default. A non-nil empty slice means "disabled".
func (s *serverBase) externalIPStartupBackoffsValue() []time.Duration {
	if s.externalIPStartupBackoffs != nil {
		return s.externalIPStartupBackoffs
	}
	return defaultExternalIPStartupBackoffs
}

// fetchExternalIPOnce performs a single bounded request to the configured
// ipinfo-compatible endpoint. Returns the IP on success (may be empty if
// the response had no `ip` field) or an error suitable for logging.
func (s *serverBase) fetchExternalIPOnce(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, externalIPFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.externalIPURLValue(), nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	var result struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return result.IP, nil
}

// fetchAndStoreExternalIP runs a single fetch, stores the result on
// success, and logs outcome appropriately. Returns true when a non-empty
// IP was stored. The first failure (while no IP has ever been fetched
// successfully) is logged at Warn so a DNS-degraded box is visible at
// default log level. Subsequent failures are logged at Debug to avoid
// spamming the journal. On success, the first success is logged at Info
// and subsequent changes at Info ("external IP changed").
func (s *serverBase) fetchAndStoreExternalIP(ctx context.Context) bool {
	ip, err := s.fetchExternalIPOnce(ctx)
	if err != nil {
		s.logExternalIPFetchFailure(err)
		return false
	}
	if ip == "" {
		slog.Debug("fetchExternalIP: endpoint returned empty ip field")
		return false
	}
	old := ""
	if v, ok := s.externalIP.Load().(string); ok {
		old = v
	}
	s.externalIP.Store(ip)
	switch {
	case old == "":
		slog.Info("external IP discovered", "ip", ip)
	case old != ip:
		slog.Info("external IP changed", "old", old, "new", ip)
	}
	// A success clears the Warn-logged latch so a future failure after
	// a recovered run logs at Warn again (operator wants to see it).
	s.extFailMu.Lock()
	s.extFailWarnLogged = false
	s.extFailMu.Unlock()
	return true
}

// logExternalIPFetchFailure applies the "first failure loud, rest quiet"
// policy described on extFailWarnLogged. Kept separate so
// fetchAndStoreExternalIP stays linear.
func (s *serverBase) logExternalIPFetchFailure(err error) {
	// If we already have a stored IP, a single transient failure does not
	// warrant a Warn — the dashboard still shows the last-known value.
	if ip, ok := s.externalIP.Load().(string); ok && ip != "" {
		slog.Debug("fetchExternalIP: transient failure while cached IP still valid", "err", err, "cached_ip", ip)
		return
	}
	s.extFailMu.Lock()
	alreadyLogged := s.extFailWarnLogged
	s.extFailWarnLogged = true
	s.extFailMu.Unlock()
	if alreadyLogged {
		slog.Debug("fetchExternalIP: failed again", "err", err)
		return
	}
	slog.Warn(
		"fetchExternalIP: failed and dashboard external_ip will be empty until a later attempt succeeds",
		"url", s.externalIPURLValue(),
		"err", err,
	)
}

// fetchExternalIP preserves the original one-shot API for existing call
// sites (network poller tick and legacy tests) while routing through the
// new log-and-store path. Callers that want startup retries should use
// fetchExternalIPWithStartupBackoff instead.
func (s *serverBase) fetchExternalIP(ctx context.Context) {
	s.fetchAndStoreExternalIP(ctx)
}

// fetchExternalIPWithStartupBackoff runs the immediate attempt plus a
// short cascade of retries (defaults: 30s, 2m, 10m) so a DNS-degraded
// boot recovers in minutes instead of waiting for the hourly tick.
// Returns as soon as a fetch succeeds, ctx is cancelled, or the backoff
// schedule is exhausted. Safe to call on its own goroutine.
func (s *serverBase) fetchExternalIPWithStartupBackoff(ctx context.Context) {
	for _, d := range s.externalIPStartupBackoffsValue() {
		if d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
		if s.fetchAndStoreExternalIP(ctx) {
			return
		}
	}
}

// startNetworkPoller fetches the external IP (with a short retry cascade
// on a dedicated goroutine so startup isn't blocked by a 30s timeout),
// primes the internal-IP cache, and then ticks every pollerInternalIPInterval
// to refresh the internal IP (with debounced change detection that fires
// DNS reconcile on a confirmed change) and to refresh the external IP
// every pollerExternalEvery ticks. All goroutines exit when ctx is cancelled.
func (s *serverBase) startNetworkPoller(ctx context.Context) {
	// Startup external-IP fetch happens on its own goroutine: each attempt
	// can take up to externalIPFetchTimeout and the full backoff schedule
	// runs for ~12m worst case. Blocking startNetworkPoller on that would
	// delay the first internal-IP tick and the HTTP server becoming healthy.
	go s.fetchExternalIPWithStartupBackoff(ctx)
	// Prime the internal-IP cache before any ticks so the first tick is a
	// no-op rather than a spurious "" → realIP "change".
	s.RefreshInternalIP()

	go func() {
		ticker := time.NewTicker(s.pollerInternalIntervalValue())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tickNetworkPoll(ctx)
			}
		}
	}()

	// Hourly DNS drift-repair tick. Runs on its own goroutine so a
	// slow rolodex round-trip can't delay the internal-IP tick (and
	// vice versa). Cheap at steady state — ReconcileDNS is a diff.
	go func() {
		ticker := time.NewTicker(s.pollerDNSIntervalValue())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tickDNSPoll(ctx)
			}
		}
	}()

	// Object-storage convergence tick. Own goroutine so a partition that takes
	// the full socket-readiness wait to answer cannot stall the IP or DNS ticks.
	go func() {
		ticker := time.NewTicker(s.pollerGfehIntervalValue())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tickGfehPoll(ctx)
			}
		}
	}()

	// Expired-peer reaper. Network-only enrollments carry a TTL; a device
	// that stops refreshing must have its peer (and overlay address) reclaimed.
	// Own goroutine so the reap + transport re-render can't stall the IP/DNS
	// ticks. Cheap at steady state — ReapExpiredPeers is a single indexed delete.
	go func() {
		ticker := time.NewTicker(peerReapInterval(getHandler(ctx, s).peerTTL(ctx)))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				getHandler(ctx, s).reapExpiredPeers(ctx)
			}
		}
	}()
}

// peerReapInterval derives how often to sweep for expired peers from the TTL:
// a quarter of the TTL so a lapsed peer lingers at most ~TTL/4 past expiry,
// clamped to [1m, 15m] so neither a tiny nor an enormous TTL yields a pathological
// sweep rate.
func peerReapInterval(ttl time.Duration) time.Duration {
	interval := ttl / 4
	if interval < time.Minute {
		return time.Minute
	}
	if interval > 15*time.Minute {
		return 15 * time.Minute
	}
	return interval
}

func parseLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelError
	}
}

// originAllowed reports whether a cross-origin request may be served.
//
// The rule this replaces was "the Origin's hostname equals the Host header's
// hostname", meant to permit the UI on :80 calling the API on :5309. It does
// permit that — and it permits anything else, because BOTH halves come from the
// same attacker-chosen URL: point `box.evil.example` at the box's LAN address
// and a browser sends Origin: http://box.evil.example and Host:
// box.evil.example:5309, which match. That is the DNS-rebinding shape, and with
// AllowCredentials it hands a drive-by page the bootstrap window
// (POST /account/create answers unauthenticated while no enabled admin exists).
//
// So the Host header is checked against what this box may legitimately be
// called before it is used as evidence of anything: its own hostname, the
// loopback and LAN addresses it answers on, and whatever the operator
// configured. An IP literal is accepted on its own — an address cannot be
// aliased by DNS, so `http://192.168.1.10/` reaching `http://192.168.1.10:5309`
// is the same box by construction, which is the common way this is actually
// used.
func originAllowed(c *echo.Context, origin string, allowedHosts []string) bool {
	if origin == "" {
		return false
	}
	if os.Getenv("DEBUG") != "" {
		return true
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := u.Hostname()
	if originHost == "" {
		return false
	}

	for _, h := range allowedHosts {
		if strings.EqualFold(originHost, h) {
			return true
		}
	}

	// Cross-port from the same host, but only when the Host header names
	// something this box is entitled to be called.
	reqHost := c.Request().Host
	if reqHost == "" {
		return false
	}
	if h, _, splitErr := net.SplitHostPort(reqHost); splitErr == nil {
		reqHost = h
	}
	if !strings.EqualFold(originHost, reqHost) {
		return false
	}
	return hostIsSelf(reqHost, allowedHosts)
}

// hostIsSelf reports whether a Host header names this box rather than an
// arbitrary DNS name that merely resolves to it.
func hostIsSelf(host string, allowedHosts []string) bool {
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return true
	}
	for _, h := range allowedHosts {
		if strings.EqualFold(host, h) {
			return true
		}
	}
	return false
}

func configureRouter(ctx context.Context, sc systemControllerBackend) http.Handler {
	handlers := getHandler(ctx, sc)
	e := echo.New()
	e.HTTPErrorHandler = ProblemDetailHTTPErrorHandler()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel()}))
	e.Logger = logger
	slog.SetDefault(logger)
	e.Use(middleware.RequestLogger())
	// Private Network Access is answered only for an origin CORS would accept.
	// Registered BEFORE the CORS middleware so it still runs on a preflight,
	// which CORS answers itself without calling further down the chain.
	//
	// Echoing the header unconditionally, as this did, hands every origin on
	// the internet the browser's permission to reach a private address — the
	// one protection PNA exists to add on top of CORS.
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if c.Request().Header.Get("Access-Control-Request-Private-Network") == "true" {
				if origin := c.Request().Header.Get("Origin"); originAllowed(c, origin, sc.GetAllowedHosts()) {
					c.Response().Header().Set("Access-Control-Allow-Private-Network", "true")
				}
			}
			return next(c)
		}
	})
	allowedHosts := sc.GetAllowedHosts()
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		UnsafeAllowOriginFunc: func(c *echo.Context, origin string) (string, bool, error) {
			if originAllowed(c, origin, allowedHosts) {
				return origin, true, nil
			}
			return "", false, nil
		},
		AllowMethods:     []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
	}))
	// Request tallies, registered outermost of the three so a request denied by
	// either gate below is still counted. A 403 the operator cannot explain is
	// exactly the kind of thing the counter exists to make visible, and one
	// recorded only on the requests that got through would hide it.
	e.Use(handlers.metricsMiddleware)
	e.Use(handlers.auditMiddleware)
	// Fail-closed gate for grant-holding accounts. Registered after audit so a
	// denied request is still recorded, and before the routes so a scoped account
	// is confined regardless of any single route's own middleware.
	e.Use(handlers.grantAllowlist)
	handlers.configureRoutes(e)
	return e
}

// ErrAuthNotConfigured is returned by NewHandler for a config that installs no
// session manager without setting AuthDisabled.
var ErrAuthNotConfigured = errors.New("no session manager and AuthDisabled is not set: refusing to serve without authentication")

// NewHandler creates an http.Handler for the given ServerConfig.
// The system hostname is automatically added to AllowedHosts.
// The external IP poller is started in the background using the provided context.
//
// It returns ErrAuthNotConfigured rather than a handler when there is no
// session manager and auth was not explicitly disabled. The middleware refuses
// that state per-request too, but a box that boots and then 500s every
// authenticated route is a confusing outage; refusing to start says what is
// wrong once, in the journal, at the moment it can still be fixed.
func NewHandler(ctx context.Context, cfg ServerConfig) (http.Handler, error) {
	if cfg.SessionMgr == nil && !cfg.AuthDisabled {
		return nil, ErrAuthNotConfigured
	}

	cfg.AllowedHosts = append(cfg.AllowedHosts, "localhost")
	if hostname, err := os.Hostname(); err == nil {
		// The bare hostname plus the two qualified forms a browser on the LAN
		// actually reaches this box by: mDNS, and the box's own DNS TLD.
		//
		// Enumerated rather than matched by suffix. A rule like "any name whose
		// first label is the hostname" would accept townos.evil.example, which
		// an attacker can simply register — and pointing it at the box's LAN
		// address is the whole DNS-rebinding move originAllowed exists to
		// refuse. Named access beyond these is an operator's call, through
		// AllowedHosts.
		cfg.AllowedHosts = append(cfg.AllowedHosts, hostname, hostname+".local")
		if cfg.SettingsMgr != nil {
			if tld, err := cfg.SettingsMgr.Get(ctx, "dns_tld"); err == nil && tld != "" {
				cfg.AllowedHosts = append(cfg.AllowedHosts, hostname+"."+tld)
			}
		}
	}
	sb := &serverBase{ServerConfig: cfg}
	sb.startNetworkPoller(ctx)
	return configureRouter(ctx, sb), nil
}

// --- TestServer ---

type TestServer struct {
	serverBase

	Server *httptest.Server
	cancel context.CancelFunc
}

// InitTestServer builds an in-process server for tests.
//
// It turns auth off for a config that installs no session manager. Most tests
// are about storage, packages, or DNS and never construct one; before
// ServerConfig.AuthDisabled existed the middleware inferred the same thing from
// SessionMgr being nil, which is exactly the fail-open the flag replaced. Doing
// it here keeps those ~230 call sites working while the production path gets no
// such inference — and a test that DOES install a session manager is untouched,
// so the auth suites still exercise real enforcement.
func InitTestServer(cfg ServerConfig) *TestServer {
	if cfg.SessionMgr == nil {
		cfg.AuthDisabled = true
	}
	ts := &TestServer{}
	ts.ServerConfig = cfg
	ctx, cancel := context.WithCancel(context.Background())
	ts.cancel = cancel
	ts.Server = httptest.NewServer(withContext(ctx, configureRouter(ctx, ts)))
	return ts
}

func (ts *TestServer) Close() {
	ts.cancel()
	ts.Server.Close()
}

// SetExternalIP stores an external IP for testing. This allows integration
// tests to verify the ping response includes the external IP without
// depending on a live ipinfo.io fetch.
func (ts *TestServer) SetExternalIP(ip string) {
	ts.externalIP.Store(ip)
}

// SetInternalIP stores an internal (LAN) IP for testing. Parallel to
// SetExternalIP — lets integration tests pin a known value that feeds
// leaf-cert SANs, DNS records, and anything else keyed on
// GetInternalIP without depending on the host's live interface list.
//
// It also claims the IPv6 slot, and that is load-bearing rather than tidiness.
// GetInternalIPv6 lazily refreshes an empty cache, and RefreshInternalIP
// rediscovers BOTH families — so the first read of the IPv6 address on a server
// that had only its IPv4 pinned silently replaced that pin with the host's real
// address. A test that pinned 192.168.122.50 then wrote a DNS record got the
// machine's own LAN IP instead, in whichever record happened to be written
// after the first IPv6 read. Claiming both here makes a pin mean what it reads
// as: this box's addresses are fixed, and nothing rediscovers them.
func (ts *TestServer) SetInternalIP(ip string) {
	ts.internalIP.Store(ip)
	// Only when unclaimed, so this is order-independent: pinning the IPv4 after
	// the IPv6 must not erase the IPv6.
	ts.internalIPv6.CompareAndSwap(nil, "")
}

// SetInternalIPv6 stores the host's global IPv6 for testing. Parallel to
// SetInternalIP — lets integration tests pin a known value (or "" to model a
// v4-only host) that feeds AAAA DNS records and leaf-cert SANs without
// depending on the host's live interface list. Storing "" is meaningful:
// GetInternalIPv6 sees a non-nil cache entry and returns "" without falling
// back to live discovery, so a v4-only host can be modelled deterministically.
//
// Claims the IPv4 slot for the same reason SetInternalIP claims this one.
func (ts *TestServer) SetInternalIPv6(ip string) {
	ts.internalIPv6.Store(ip)
	ts.internalIP.CompareAndSwap(nil, "")
}

func (ts *TestServer) Run() error {
	ts.Server.Start()
	return nil
}

func (ts *TestServer) Client() (*SystemdClient, error) {
	return FromClient(ts.Server.Client(), ts.Server.URL)
}

// --- UnixServer ---

type UnixServer struct {
	serverBase

	Socket string
	server *http.Server
	cancel context.CancelFunc
}

func InitUnixServer(sock string, cfg ServerConfig) *UnixServer {
	us := &UnixServer{Socket: sock}
	us.ServerConfig = cfg
	return us
}

func (us *UnixServer) Close() error {
	if us.cancel != nil {
		us.cancel()
	}
	return us.server.Close()
}

func (us *UnixServer) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	us.cancel = cancel
	us.server = &http.Server{Handler: withContext(ctx, configureRouter(ctx, us)), ReadHeaderTimeout: 10 * time.Second}
	us.startNetworkPoller(ctx)
	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "unix", us.Socket)
	if err != nil {
		return fmt.Errorf("could not listen on unix socket %q: %w", us.Socket, err)
	}
	return us.server.Serve(lis)
}

func (us *UnixServer) Client() (*SystemdClient, error) {
	return InitClient(us.Socket)
}
