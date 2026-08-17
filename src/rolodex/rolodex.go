// Package rolodex provides a client interface for the Rolodex DNS server.
// The Rolodex systemd unit is a boot service managed entirely by systemd —
// the systemcontroller does not install, start, stop, or restart it.
// This package holds the rolodex settings Town OS owns, programs them into the
// running server over gRPC (rolodex keeps them in memory only), waits for DNS
// readiness, and reports service status. It writes no configuration file: the
// only file rolodex reads is the bootstrap config the install image renders.
package rolodex

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"gitea.com/town-os/town-os/src/systemd"
)

const (
	// DefaultDNSPort is the default port for DNS queries.
	DefaultDNSPort = "53"
	// DefaultGRPCSocket is the default filename for the gRPC Unix socket.
	DefaultGRPCSocket = "rolodex.sock"
	// DNSLoopback is the loopback address used for local DNS resolution.
	// Using 127.0.0.2 avoids conflicts with systemd-resolved (127.0.0.53)
	// and other services commonly bound to 127.0.0.1.
	DNSLoopback = "127.0.0.2"
	// DefaultMetricsPort is the port rolodex serves its Prometheus text
	// exposition on. 9153 is CoreDNS's convention for a DNS exporter and is
	// rolodex's own default; it is written explicitly so the scrape target
	// Town OS configures cannot drift from what rolodex binds.
	DefaultMetricsPort = "9153"

	// ResolutionModeAuto is a tiered fallback chain: iterate from the root
	// servers first, then DoH/DoT, then the local forwarder, then a public
	// resolver on :53 — sticking to whichever tier last worked. It keeps the
	// privacy of recursion wherever the network permits it and degrades instead
	// of failing where it does not, which is why it is both rolodex's default
	// and ours.
	ResolutionModeAuto = "auto"
	// ResolutionModeRecursive resolves unmatched queries iteratively from the
	// root servers and NOTHING ELSE. It has no fallback: on a network that
	// filters or hijacks outbound :53 (hotel, captive portal, some ISPs) every
	// external name SERVFAILs. Choose it only to guarantee no query ever reaches
	// a third party.
	ResolutionModeRecursive = "recursive"
	// ResolutionModeForward forwards unmatched queries to the configured
	// upstream forwarders (legacy behavior). Used by forwarding tests.
	ResolutionModeForward = "forward"
	// DefaultResolutionMode is the upstream resolution strategy programmed
	// into rolodex when Config.ResolutionMode is unset.
	DefaultResolutionMode = ResolutionModeAuto
)

// DefaultForwarders are the upstream DNS forwarder addresses programmed into
// rolodex when Config.Forwarders is not set.
var DefaultForwarders = []string{"8.8.8.8:53", "8.8.4.4:53"}

// SystemService describes the rolodex system service metadata.
type SystemService struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
	UnitName    string `json:"unit_name"`
}

// Status represents the current state of the rolodex container.
type Status struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Running bool   `json:"running"`
	Port    string `json:"port"`
}

// Config holds the configuration for the rolodex manager.
type Config struct {
	// Systemd is used to query unit status (read-only).
	Systemd systemd.Manager
	// DataDir is the directory where rolodex configuration and data are stored.
	DataDir string
	// Image is the container image reference for the rolodex server.
	Image string
	// UnixSocketPath is the host path to the gRPC Unix socket.
	UnixSocketPath string
	// DNSPort is the host-side port for DNS queries. If empty,
	// DefaultDNSPort ("53") is used.
	DNSPort string
	// MetricsPort is the host-side port rolodex serves its Prometheus
	// /metrics endpoint on, bound to DNSLoopback. If empty,
	// DefaultMetricsPort ("9153") is used.
	//
	// Like DNSPort this is relocatable because the rolodex container runs
	// --net host: the listener lands in the host namespace, so a test box and
	// a `make dev` box would otherwise fight over the same port and one of
	// them would crash-loop under Restart=always — IRON RULE.
	MetricsPort string
	// Key overrides the service key used for systemd unit and container
	// naming. Defaults to "rolodex". Tests should set a unique value to
	// avoid colliding with a production rolodex service.
	Key string
	// Forwarders overrides the upstream DNS forwarder addresses
	// ("host:port") programmed into rolodex. Defaults to
	// DefaultForwarders. Consulted in "forward" mode, and as the
	// local-forwarder tier of "auto"; in "recursive" mode rolodex resolves
	// from the roots and ignores them.
	// Tests point this at a local stub DNS server so forwarding works
	// without internet access (captive networks block direct queries to
	// public resolvers).
	Forwarders []string
	// LocalForwarders replaces the forwarder list with the resolvers this
	// box's own network handed it (HostResolvers), for a network that blocks
	// external DNS. Off by default: the local resolver sees every name the
	// household looks up, which is exactly what resolving from the roots
	// avoids, so it is the operator's call and not a silent fallback.
	//
	// It is not a resolution mode of its own — it changes WHICH addresses the
	// local tier holds, and the mode still decides whether that tier is
	// consulted. In "auto" it is reached only after the roots and the
	// encrypted upstreams have failed, which is the "external DNS is blocked"
	// case; in "forward" it is the only upstream; in "recursive" it is unused.
	//
	// When discovery finds nothing usable the configured (or default)
	// forwarders are kept, so turning this on can never leave the local tier
	// pointing at nothing.
	LocalForwarders bool
	// ResolvConfPaths overrides the files host-resolver discovery reads, in
	// order. Empty (the normal case) means the system paths. Tests point this
	// at a fixture so discovery is a property of the test rather than of
	// whatever resolver the machine running it happens to have.
	ResolvConfPaths []string
	// RouteTablePath overrides the routing table discovery reads to find the
	// default gateway. Empty (the normal case) means the system path. Tests
	// point this at a fixture for the same reason as ResolvConfPaths.
	RouteTablePath string
	// ForwarderProbe overrides how a discovered candidate is proven to answer
	// queries. Nil (the normal case) means a real DNS query.
	//
	// Every test that exercises discovery MUST set this. Left nil, discovery
	// queries whatever the test machine's gateway is: a test that reaches the
	// network, whose result depends on the network it reached, and which on a
	// box where the gateway does resolve would pass no matter what the code
	// under it did.
	ForwarderProbe func(ctx context.Context, addr string) bool
	// ForwarderProbeTimeout overrides the ceiling on one round of probing.
	// Zero means DefaultForwarderProbeTimeout.
	ForwarderProbeTimeout time.Duration
	// ForwarderProbeTTL overrides how long a validated list is reused before
	// the probes run again. Zero means DefaultForwarderProbeTTL.
	ForwarderProbeTTL time.Duration
	// ResolutionMode selects how rolodex resolves unmatched queries: "auto"
	// (the default: roots, then DoH/DoT, then Forwarders, then a public
	// resolver — sticking to whichever tier last worked), "recursive"
	// (iterative from the root servers only, no fallback), or "forward"
	// (forward to Forwarders). Defaults to DefaultResolutionMode.
	ResolutionMode string
	// DNSBL is the blocklist provider list programmed into rolodex. It seeds
	// the manager at boot from the persisted configuration; SetBlocklist
	// replaces it when an operator changes it. The zero value is "disabled, no
	// providers", which is also rolodex's own default.
	DNSBL Blocklist
}

// Manager provides rolodex configuration and status reporting. The Rolodex
// systemd unit is a boot service — this manager does not install, start,
// stop, or restart it.
type Manager struct {
	cfg Config
	// mu guards the configuration an HTTP handler writes after construction —
	// the blocklist (POST /dns/dnsbl), the local-forwarder flag (POST
	// /settings/set) — and the discovery cache below. Each carries a slice or
	// is read on a path a background tick also runs, so two of these racing is
	// a data race, not a torn scalar.
	mu sync.RWMutex
	// discovered is the last validated local-forwarder list, and discoveredAt
	// when it was probed. Cached because ProgramRolodex re-pushes the
	// forwarders on every tick and GET /dns/status reports them on request:
	// probing on each of those would pay the probe timeout on a schedule, and
	// the answer only changes when the network does. SetLocalForwarders drops
	// it so an operator toggling the setting is not served a stale list.
	discovered   []string
	discoveredAt time.Time
}

// NewManager creates a new rolodex Manager with the given configuration.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Key returns the service key used for systemd unit and container naming.
func (m *Manager) Key() string {
	return m.key()
}

// key returns the configured key or "rolodex" as default.
func (m *Manager) key() string {
	if m.cfg.Key != "" {
		return m.cfg.Key
	}
	return "rolodex"
}

// SocketPath returns the configured gRPC Unix socket path.
func (m *Manager) SocketPath() string {
	return m.cfg.UnixSocketPath
}

// dnsPort returns the configured host-side DNS port, defaulting to DefaultDNSPort.
func (m *Manager) dnsPort() string {
	if m.cfg.DNSPort != "" {
		return m.cfg.DNSPort
	}
	return DefaultDNSPort
}

// metricsPort returns the configured metrics port, defaulting to
// DefaultMetricsPort.
func (m *Manager) metricsPort() string {
	if m.cfg.MetricsPort != "" {
		return m.cfg.MetricsPort
	}
	return DefaultMetricsPort
}

// MetricsPort returns the port rolodex serves /metrics on.
func (m *Manager) MetricsPort() string {
	return m.metricsPort()
}

// MetricsAddr returns the "host:port" Prometheus must scrape to collect
// rolodex's metrics. It is the single value both sides agree on: this manager
// writes it into rolodex.yml as the listener's bind address, and the caller
// hands the same string to the Prometheus scrape config, so the target cannot
// drift from the listener.
//
// The address is DNSLoopback rather than 127.0.0.1: rolodex already owns that
// loopback address for DNS, and Prometheus runs --net host too, so it reaches
// it with no podman-network hop — exactly like the node-exporter target.
func (m *Manager) MetricsAddr() string {
	return net.JoinHostPort(DNSLoopback, m.metricsPort())
}

// resolutionModeLocked returns the configured upstream resolution mode,
// defaulting to DefaultResolutionMode ("auto"). The caller must already hold
// m.mu; it is named for that because forwarders() reads the mode inside the
// RLock it takes for the rest of the config, and sync.RWMutex is documented as
// deadlocking on recursive read locking whenever a writer arrives between the
// two.
func (m *Manager) resolutionModeLocked() string {
	if m.cfg.ResolutionMode != "" {
		return m.cfg.ResolutionMode
	}
	return DefaultResolutionMode
}

// ResolutionMode returns the mode that would be written to rolodex.yml.
func (m *Manager) ResolutionMode() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resolutionModeLocked()
}

// ValidResolutionMode reports whether mode is one rolodex understands.
func ValidResolutionMode(mode string) bool {
	return mode == ResolutionModeAuto ||
		mode == ResolutionModeRecursive ||
		mode == ResolutionModeForward
}

// SetResolutionMode changes the mode this manager holds. It is what
// ProgramRolodex pushes to the running server, at boot and after every rolodex
// restart; a caller changing it at runtime programs the server itself so the
// change takes effect immediately, with no unit restart.
//
// Locked because the mode is no longer read only when rolodex.yml is rendered:
// forwarders() consults it on every Forwarders() call, which is every settings
// write and every ProgramRolodex pass, so the HTTP handler storing a mode races
// the reconcile goroutine resolving the list without it.
func (m *Manager) SetResolutionMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.ResolutionMode = mode
}

// Blocklist returns the DNSBL provider list this manager holds, which is what
// gets programmed into the running rolodex.
func (m *Manager) Blocklist() Blocklist {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.DNSBL.clone()
}

// SetBlocklist changes the provider list this manager holds, so that the
// next programming pass — boot, or the tick after a rolodex restart — pushes
// these rather than the ones this process started with.
//
// Unlike SetResolutionMode, this needs NO unit restart: the live server is
// programmed over gRPC by the same handler, so bouncing DNS to apply a change
// that has already taken effect would drop every in-flight resolution for
// nothing. The file exists so a rolodex that restarts on its own — a crash
// under Restart=always, a system-services refresh, the restart a
// resolution-mode change performs — comes back with the lists still
// configured, because rolodex persists none of this itself.
func (m *Manager) SetBlocklist(dnsbl Blocklist) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.DNSBL = dnsbl.clone()
}

// UnitName returns the systemd unit that supervises rolodex.
func (m *Manager) UnitName() string {
	return systemd.SystemServiceUnitName(m.key())
}

// forwarders returns the upstream forwarder addresses programmed into rolodex:
// the host's own resolvers when discovery is in play and found any that ANSWER,
// otherwise the configured list, otherwise DefaultForwarders.
//
// The fallback ordering is deliberate. Discovery reads files that may hold
// nothing usable — a box with no DHCP lease yet, or one whose only nameserver
// line is a loopback stub — and a forwarder list that came back empty would
// silently delete the local tier of the auto chain. Keeping the previous
// addresses degrades to today's behavior instead.
//
// "Found any" means "proved any". A candidate is only kept once it has resolved
// a name through it; see ForwarderDiscovery. An address that is merely
// configured is exactly the thing that was wrong on a filtered network, where
// the local tier held two public resolvers that could not be reached and every
// query that got that far paid both timeouts on its way to SERVFAIL.
//
// AUTO DISCOVERS ON ITS OWN, without LocalForwarders being set. The flag stays
// the explicit opt-in it always was for the other modes; `auto` no longer waits
// for it, and here is why the privacy argument that made it opt-in does not
// reach this mode:
//
// LocalForwarders is off by default because the local resolver sees every name
// the household looks up, which is the thing resolving from the roots exists to
// avoid. That reasoning holds wherever the local tier answers ORDINARY traffic.
// In `auto` it never does. The tier is reached only after the roots AND the
// encrypted upstreams have both failed, so by the time anything consults it the
// private paths are already gone — the choice on offer is not "roots or the
// local resolver", it is "the local resolver or SERVFAIL". Leaving the tier
// pointed at DefaultForwarders on a network that filters :53 to its own servers
// does not buy privacy, it just guarantees the failure: those are precisely the
// addresses such a network drops, so every query that gets that far pays both
// timeouts and dies anyway. Nothing is protected by the box refusing to notice
// the one resolver that answers.
//
// In `forward` the tier IS the only upstream and takes every query always, so
// the trade is real and the flag still governs it. In `recursive` the list is
// unused. An operator who wants no local resolver in the path at all under any
// circumstance asks for `recursive`, which is that request stated directly;
// `dns_forwarders` remains the way to name the tier's contents by hand, and it
// still wins over discovery below.
//
// Precisely what changes for the flag in `auto`: turning it OFF no longer
// suppresses discovery WHEN THERE IS NO EXPLICIT LIST to fall back on. It is
// still consulted otherwise — with `dns_forwarders` set, flag-off uses that list
// and flag-on overrides it with discovery, which is what the flag has always
// meant. The flag is only inert in the case where honoring "off" would mean
// programming DefaultForwarders into a tier reached solely because everything
// else already failed.
//
// It has to be inert in exactly that case, because the flag cannot express
// "explicitly off" separately from "never set" — it is a bool defaulting to
// false, so both are the same stored value, and honoring the first would mean
// never discovering for the second, which is the box this exists for. An
// operator wanting no local resolver in the path says so with `recursive`, or
// names the tier's contents with `dns_forwarders`.
func (m *Manager) forwarders(ctx context.Context) []string {
	m.mu.RLock()
	local := m.cfg.LocalForwarders
	configured := m.cfg.Forwarders
	auto := m.resolutionModeLocked() == ResolutionModeAuto
	m.mu.RUnlock()

	// An explicit list wins over discovery in auto: it is the operator naming
	// the tier's contents, and silently overriding it would answer a question
	// they did not ask. With LocalForwarders deliberately set, discovery keeps
	// winning as it always has — that flag IS the request to derive the list.
	if local || (auto && len(configured) == 0) {
		if found := m.discoverForwarders(ctx); len(found) > 0 {
			return found
		}
	}
	if len(configured) > 0 {
		return configured
	}
	return DefaultForwarders
}

// discoverForwarders returns the validated local forwarders, probing at most
// once per ForwarderProbeTTL.
//
// Two callers arriving together on a cold cache both probe. That is left alone
// rather than serialized behind the lock: probing is idempotent and holding a
// mutex across a network round trip would block every reader of the blocklist
// and the local-forwarder flag for the probe timeout, which is a worse failure
// than one redundant round of queries.
func (m *Manager) discoverForwarders(ctx context.Context) []string {
	ttl := m.cfg.ForwarderProbeTTL
	if ttl <= 0 {
		ttl = DefaultForwarderProbeTTL
	}

	m.mu.RLock()
	cached, at := m.discovered, m.discoveredAt
	m.mu.RUnlock()
	if !at.IsZero() && time.Since(at) < ttl {
		return cached
	}

	found := ForwarderDiscovery{
		ResolvConfPaths: m.cfg.ResolvConfPaths,
		RouteTablePath:  m.cfg.RouteTablePath,
		ProbeTimeout:    m.cfg.ForwarderProbeTimeout,
		Probe:           m.cfg.ForwarderProbe,
	}.Validated(ctx)

	m.mu.Lock()
	m.discovered, m.discoveredAt = found, time.Now()
	m.mu.Unlock()

	return found
}

// Forwarders returns the upstream forwarder addresses this manager would
// program into rolodex right now, so the API can report what the box is
// actually configured to fall back to rather than what was asked for.
//
// It takes a context because answering can require probing the candidates, and
// a caller that has given up should not leave a round of DNS queries running
// behind it.
func (m *Manager) Forwarders(ctx context.Context) []string {
	return m.forwarders(ctx)
}

// LocalForwarders reports whether the forwarder list is taken from the host's
// own resolvers.
func (m *Manager) LocalForwarders() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.LocalForwarders
}

// SetForwarders replaces the operator's configured forwarder list.
//
// An empty list restores DefaultForwarders through Manager.forwarders, which is
// what clearing the setting means. Entries may name any transport; they are
// pushed to rolodex verbatim, which is why they are validated where an operator
// sets them rather than here.
func (m *Manager) SetForwarders(forwarders []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Forwarders = forwarders
}

// SetLocalForwarders changes whether the host's own resolvers are used as the
// forwarder list. The list itself is re-derived on read (see forwarders), and
// programmed into the running server — no unit restart.
//
// The discovery cache is dropped rather than left to expire. An operator
// toggling this has just told the box something about its network, and serving
// them a list probed up to ForwarderProbeTTL ago would answer a question they
// did not ask. It also makes the switch usable as a re-probe: RefreshDNSLocal-
// Forwarders deliberately does not short-circuit on an unchanged flag, and this
// is what gives that call something new to find.
func (m *Manager) SetLocalForwarders(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.LocalForwarders = enabled
	m.discovered, m.discoveredAt = nil, time.Time{}
}

// Generation identifies the current run of the rolodex server, and changes
// exactly when rolodex restarts.
//
// It is the identity of the gRPC socket rolodex binds at startup — device,
// inode and modification time — because that is a restart signal Town OS can
// read with one stat, with no D-Bus round trip, no unit property this codebase
// does not already expose, and no RPC of its own. rolodex unlinks and rebinds
// the socket on every start, so a new run always has a new inode.
//
// Why a restart signal is needed at all: rolodex holds forwarders, resolution
// mode and both blocklists in memory only. It seeds them from its config file
// at startup and persists nothing set over gRPC, so everything Town OS
// programmed is gone the moment the process restarts — a crash under
// Restart=always, a DHCP lease change bouncing the unit, an operator
// restarting it by hand. Comparing this value against the one held at the last
// successful programming is what turns "rolodex restarted" into "reprogram
// it", promptly, without polling rolodex itself.
//
// An empty string means the socket is not there — rolodex is not running, or
// has not bound yet. Callers treat that as "nothing to program", not as a
// change: programming a server that is not up cannot succeed, and recording it
// as a generation would skip the real programming once it does.
func (m *Manager) Generation() string {
	info, err := os.Stat(m.SocketPath())
	if err != nil {
		return ""
	}
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Not Linux, or an unexpected FileInfo: fall back to mtime alone.
		// Coarser (an inode reused within the same nanosecond would be
		// missed) but never wrong in the direction that matters — a restart
		// that takes any measurable time still changes it.
		return strconv.FormatInt(info.ModTime().UnixNano(), 10)
	}
	return fmt.Sprintf("%d:%d:%d", sys.Dev, sys.Ino, info.ModTime().UnixNano())
}

// WaitForDNSReady polls the DNS TCP port until it accepts connections or the
// context is cancelled. This should be called at startup to ensure the DNS
// server is ready before proceeding with operations that depend on DNS
// (e.g., pulling container images).
func (m *Manager) WaitForDNSReady(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	addr := net.JoinHostPort(DNSLoopback, m.dnsPort())
	var dialer net.Dialer

	for {
		conn, err := dialer.DialContext(waitCtx, "tcp", addr)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				slog.Debug("close DNS probe connection", "error", closeErr)
			}
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("DNS server at %s not ready: %w", addr, waitCtx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// SystemServices returns metadata for the rolodex system service.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{
		{
			Key:         m.key(),
			DisplayName: "Rolodex DNS",
			Image:       m.cfg.Image,
			Port:        m.dnsPort(),
			UnitName:    systemd.SystemServiceUnitName(m.key()),
		},
	}
}

// Status returns the current state of the rolodex container by querying
// systemd unit state. It uses GetUnitStates to avoid enumerating all unit
// files on disk, which on hosts with overlayfs root causes per-file inode
// lookups that can flood the kernel log with ESTALE warnings.
func (m *Manager) Status(ctx context.Context) Status {
	running := false
	unitName := systemd.SystemServiceUnitName(m.key())
	units, err := m.cfg.Systemd.GetUnitStates(ctx, []string{unitName})
	if err == nil {
		for _, u := range units {
			if u.Name == unitName {
				running = u.ActiveState == "active"
				break
			}
		}
	}

	return Status{
		Name:    systemd.SystemServiceContainerName(m.key()),
		Image:   m.cfg.Image,
		Running: running,
		Port:    m.dnsPort(),
	}
}
