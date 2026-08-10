// Package rolodex provides a client interface for the Rolodex DNS server.
// The Rolodex systemd unit is a boot service managed entirely by systemd —
// the systemcontroller does not install, start, stop, or restart it.
// This package writes the rolodex configuration file, waits for DNS
// readiness, and reports service status.
package rolodex

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// DefaultResolutionMode is the upstream resolution strategy written to
	// rolodex.yml when Config.ResolutionMode is unset.
	DefaultResolutionMode = ResolutionModeAuto
)

// DefaultForwarders are the upstream DNS forwarder addresses written to
// rolodex.yml when Config.Forwarders is not set.
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
	// ("host:port") written to rolodex.yml. Defaults to
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
	// ResolutionMode selects how rolodex resolves unmatched queries: "auto"
	// (the default: roots, then DoH/DoT, then Forwarders, then a public
	// resolver — sticking to whichever tier last worked), "recursive"
	// (iterative from the root servers only, no fallback), or "forward"
	// (forward to Forwarders). Defaults to DefaultResolutionMode.
	ResolutionMode string
	// RBL and DNSBL are the two blocklist provider lists rendered into
	// rolodex.yml. They seed the manager at boot from the persisted
	// configuration; SetBlocklists replaces them when an operator changes one.
	// The zero value of each renders exactly the "disabled, no providers"
	// section this file has always carried.
	RBL   Blocklist
	DNSBL Blocklist
}

// Manager provides rolodex configuration and status reporting. The Rolodex
// systemd unit is a boot service — this manager does not install, start,
// stop, or restart it.
type Manager struct {
	cfg Config
	// mu guards the blocklist lists alone. They are the only configuration
	// this manager holds that an HTTP handler writes after construction
	// (POST /dns/rbl and POST /dns/dnsbl), and they carry slices — two
	// concurrent saves racing on one is a data race, not a torn scalar.
	mu sync.RWMutex
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

// resolutionMode returns the configured upstream resolution mode, defaulting
// to DefaultResolutionMode ("auto").
func (m *Manager) resolutionMode() string {
	if m.cfg.ResolutionMode != "" {
		return m.cfg.ResolutionMode
	}
	return DefaultResolutionMode
}

// ResolutionMode returns the mode that would be written to rolodex.yml.
func (m *Manager) ResolutionMode() string {
	return m.resolutionMode()
}

// ValidResolutionMode reports whether mode is one rolodex understands.
func ValidResolutionMode(mode string) bool {
	return mode == ResolutionModeAuto ||
		mode == ResolutionModeRecursive ||
		mode == ResolutionModeForward
}

// SetResolutionMode changes the mode this manager renders into rolodex.yml.
// Callers must follow with RewriteConfig + a unit restart for it to take
// effect on the running server.
func (m *Manager) SetResolutionMode(mode string) {
	m.cfg.ResolutionMode = mode
}

// Blocklists returns the RBL and DNSBL provider lists this manager renders
// into rolodex.yml.
func (m *Manager) Blocklists() (rbl, dnsbl Blocklist) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg.RBL.clone(), m.cfg.DNSBL.clone()
}

// SetBlocklists changes the provider lists this manager renders into
// rolodex.yml. Callers follow with RewriteConfig.
//
// Unlike SetResolutionMode, this needs NO unit restart: the live server is
// programmed over gRPC by the same handler, so bouncing DNS to apply a change
// that has already taken effect would drop every in-flight resolution for
// nothing. The file exists so a rolodex that restarts on its own — a crash
// under Restart=always, a system-services refresh, the restart a
// resolution-mode change performs — comes back with the lists still
// configured, because rolodex persists none of this itself.
func (m *Manager) SetBlocklists(rbl, dnsbl Blocklist) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.RBL = rbl.clone()
	m.cfg.DNSBL = dnsbl.clone()
}

// RewriteConfig writes rolodex.yml unconditionally, returning true when the
// bytes actually changed. It is the runtime counterpart to WriteConfig, which
// deliberately refuses to overwrite a config file newer than the
// systemcontroller binary (it treats that as user-modified). That guard is
// correct at boot but wrong for an operator-initiated change: the file written
// at the previous boot is ALWAYS newer than the binary, so WriteConfig would
// silently no-op and the new setting would never reach rolodex.
func (m *Manager) RewriteConfig() (bool, error) {
	if err := os.MkdirAll(m.cfg.DataDir, 0755); err != nil { //nolint:gosec // data dir must be accessible by container process
		return false, fmt.Errorf("create data dir: %w", err)
	}

	config := rolodexConfig(m.configParams())
	configPath := filepath.Join(m.cfg.DataDir, "rolodex.yml")

	if existing, err := os.ReadFile(configPath); err == nil && string(existing) == config { //nolint:gosec // G304 -- configPath is built from the controlled DataDir
		return false, nil
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return false, fmt.Errorf("write config: %w", err)
	}
	return true, nil
}

// UnitName returns the systemd unit that supervises rolodex.
func (m *Manager) UnitName() string {
	return systemd.SystemServiceUnitName(m.key())
}

// forwarders returns the upstream forwarder addresses written to rolodex.yml:
// the host's own resolvers when LocalForwarders is set and discovery found any,
// otherwise the configured list, otherwise DefaultForwarders.
//
// The fallback ordering is deliberate. Discovery reads files that may hold
// nothing usable — a box with no DHCP lease yet, or one whose only nameserver
// line is a loopback stub — and a forwarder list that came back empty would
// silently delete the local tier of the auto chain. Keeping the previous
// addresses degrades to today's behavior instead.
func (m *Manager) forwarders() []string {
	if m.cfg.LocalForwarders {
		if local := HostResolversFrom(m.resolvConfPaths()...); len(local) > 0 {
			return local
		}
	}
	if len(m.cfg.Forwarders) > 0 {
		return m.cfg.Forwarders
	}
	return DefaultForwarders
}

// resolvConfPaths returns the files host-resolver discovery reads, defaulting
// to the system ones.
func (m *Manager) resolvConfPaths() []string {
	if len(m.cfg.ResolvConfPaths) > 0 {
		return m.cfg.ResolvConfPaths
	}
	return hostResolvConfPaths
}

// Forwarders returns the upstream forwarder addresses this manager would write
// into rolodex.yml right now, so the API can report what the box is actually
// configured to fall back to rather than what was asked for.
func (m *Manager) Forwarders() []string {
	return m.forwarders()
}

// LocalForwarders reports whether the forwarder list is taken from the host's
// own resolvers.
func (m *Manager) LocalForwarders() bool {
	return m.cfg.LocalForwarders
}

// SetLocalForwarders changes whether the host's own resolvers are used as the
// forwarder list. Callers must follow with RewriteConfig + a unit restart for
// it to take effect on the running server.
func (m *Manager) SetLocalForwarders(enabled bool) {
	m.cfg.LocalForwarders = enabled
}

// rolodexConfig returns the canonical rolodex YAML configuration with the
// given DNS port, upstream forwarders, resolution mode, and metrics port. The
// bind address is always DNSLoopback (127.0.0.2) because the rolodex container
// runs with --net host.
//
// The metrics section is written unconditionally so Prometheus always has a
// rolodex target: DNS is the subsystem whose failures are hardest to see from
// the outside (a SERVFAIL looks like a broken app), and the counters that make
// the split-horizon pipeline legible — which stage answered, which upstream
// tier is live, cache hit rate — exist only here. It is opt-in upstream and
// absent by default, so writing it is what turns it on. The endpoint is
// unauthenticated plain HTTP, which is why it binds a loopback address and is
// never published to the LAN. A rolodex older than 0.4.3 does not know the key
// and ignores it (its config struct does not deny unknown fields), so the only
// cost of an old image is a scrape target that reads as down.
//
// resolution.mode defaults to "auto": rolodex tries the root servers first and
// falls back through DoH/DoT, the local forwarder, and a public :53 resolver,
// sticking to whichever tier last worked. Bare "recursive" has no fallback — on
// a network that filters or hijacks outbound :53 every external name SERVFAILs,
// and because the resolver sends a single un-retransmitted datagram per server,
// even ordinary packet loss surfaces as SERVFAIL. Auto keeps recursion's privacy
// where the network allows it and degrades instead of failing where it does not.
// The mode is written explicitly (rather than left to rolodex's own default) so
// Town OS behavior does not move when upstream changes its default. The
// forwarders are still written; they are consulted only in "forward" mode and as
// auto's local-forwarder tier.
//
// Both blocklist sections are written unconditionally, and both are rendered
// from Town OS's persisted configuration rather than pinned to "disabled, no
// providers". Rolodex holds the provider lists in memory only — it seeds them
// from this file and persists nothing a gRPC SetRblConfig/SetDnsblConfig call
// changes — so this file is the only thing a restarting rolodex reads them back
// from. The dnsbl section previously was not written at all, which left the
// domain blocklist with no way to be restored even in principle. See
// renderBlocklist.
func rolodexConfig(p rolodexConfigParams) string {
	mode := p.Mode
	if mode == "" {
		mode = DefaultResolutionMode
	}
	metricsPort := p.MetricsPort
	if metricsPort == "" {
		metricsPort = DefaultMetricsPort
	}
	var fwd strings.Builder
	for _, f := range p.Forwarders {
		fmt.Fprintf(&fwd, "  - %q\n", f)
	}
	return fmt.Sprintf(`database_path: /data/rolodex.db
dns:
  bind:
    - udp: "%s:%s"
    - tcp: "%s:%s"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
%sresolution:
  mode: %s
%s%smetrics:
  bind: "%s:%s"
`,
		DNSLoopback, p.Port, DNSLoopback, p.Port,
		fwd.String(), mode,
		renderBlocklist("rbl", p.RBL), renderBlocklist("dnsbl", p.DNSBL),
		DNSLoopback, metricsPort,
	)
}

// rolodexConfigParams is what rolodexConfig renders. It is a struct rather
// than a positional list because six same-typed strings in a row is a bug
// waiting to be introduced by a transposition the compiler cannot see.
type rolodexConfigParams struct {
	Port        string
	Forwarders  []string
	Mode        string
	MetricsPort string
	RBL         Blocklist
	DNSBL       Blocklist
}

// configParams collects everything rolodexConfig needs from this manager,
// taking the blocklist read lock once.
func (m *Manager) configParams() rolodexConfigParams {
	rbl, dnsbl := m.Blocklists()
	return rolodexConfigParams{
		Port:        m.dnsPort(),
		Forwarders:  m.forwarders(),
		Mode:        m.resolutionMode(),
		MetricsPort: m.metricsPort(),
		RBL:         rbl,
		DNSBL:       dnsbl,
	}
}

// executableMtime is the function used to get the systemcontroller binary's
// modification time. Replaceable in tests.
var executableMtime = defaultExecutableMtime

func defaultExecutableMtime() (time.Time, error) {
	exe, err := os.Executable()
	if err != nil {
		return time.Time{}, fmt.Errorf("resolve executable: %w", err)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return time.Time{}, fmt.Errorf("stat executable: %w", err)
	}
	return fi.ModTime(), nil
}

// WriteConfig ensures the rolodex YAML configuration file exists and is
// up to date. Returns true if the file was written (created or updated),
// false if it was left unchanged. The file is skipped when it already
// exists, is newer than the systemcontroller binary (user-modified), and
// has the expected content.
func (m *Manager) WriteConfig() (bool, error) {
	if err := os.MkdirAll(m.cfg.DataDir, 0755); err != nil { //nolint:gosec // data dir must be accessible by container process
		return false, fmt.Errorf("create data dir: %w", err)
	}

	config := rolodexConfig(m.configParams())
	configPath := filepath.Join(m.cfg.DataDir, "rolodex.yml")

	fi, statErr := os.Stat(configPath)
	if statErr == nil {
		// File exists — check if it's newer than our binary.
		exeMtime, exeErr := executableMtime()
		if exeErr == nil && fi.ModTime().After(exeMtime) {
			// Config was modified after the SC image was built; don't overwrite.
			return false, nil
		}

		// File is older than (or same age as) our binary — check content.
		existing, readErr := os.ReadFile(configPath) //nolint:gosec // G304 -- configPath is constructed from the controlled DataDir config field
		if readErr == nil && string(existing) == config {
			return false, nil
		}
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return false, fmt.Errorf("write config: %w", err)
	}

	return true, nil
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
