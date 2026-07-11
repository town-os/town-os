package systemcontroller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/wireguard"
	"github.com/labstack/echo/v5"
)

// resolveInstallNetwork normalizes and validates a requested install network,
// defaulting to "home". Returns an echo HTTP error when the network is unknown.
func (s *SystemControllerHandlers) resolveInstallNetwork(requested string) (string, error) {
	network := strings.TrimSpace(requested)
	if network == "" {
		network = account.DefaultNetworkName
	}
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return network, nil
	}
	if _, err := nm.Get(network); err != nil {
		if errors.Is(err, account.ErrNetworkNotFound) {
			return "", echo.NewHTTPError(400, fmt.Sprintf("network %q does not exist", network))
		}
		return "", echo.NewHTTPError(500, fmt.Sprintf("get network: %v", err))
	}
	return network, nil
}

// networkTLD returns the DNS TLD a package installed into the given network
// resolves under, so its certificate SANs and DANE TLSA records use the same
// TLD as its address records: the global dns_tld for the default network (or an
// unknown/unavailable one), otherwise the network's own TLD. A gitea instance on
// the "fart" network must get a cert for gitea.<repo>.fart, not <...>.home.
func (s *SystemControllerHandlers) networkTLD(network string) string {
	return networkTLDValue(s.Controller.GetNetworkManager(), s.Controller.GetSettingsManager(), network)
}

// networkTLDValue is the free-function core of networkTLD, so the reconcile
// path (which compiles package units without a handler) can resolve the same
// per-network TLD. It maps a package's install network to the TLD its DNS
// names use: the global dns_tld for the default network (or an unknown/
// unavailable one), otherwise the network's own TLD.
func networkTLDValue(nm account.NetworkManager, settingsMgr account.SettingsManager, network string) string {
	if network == "" || network == account.DefaultNetworkName || nm == nil {
		return reconcileDNSTLD(settingsMgr)
	}
	n, err := nm.Get(network)
	if err != nil || n.TLD == "" {
		return reconcileDNSTLD(settingsMgr)
	}
	return n.TLD
}

// networkOverlayIP returns the box's WireGuard overlay address on the given
// network, for use as a certificate SAN so a peer on that network can reach a
// package by raw overlay address (https://10.65.0.1) and not just by name. The
// default network has no WireGuard transport at all (it is DNS-only; see
// applyNetworkTransport), so it — and any unknown network — yields "", which
// collectTLSSans treats as "skip that SAN". Keeping it empty there is what
// stops default-network leaves from churning on every reconcile.
func (s *SystemControllerHandlers) networkOverlayIP(network string) string {
	return networkOverlayIPValue(s.Controller.GetNetworkManager(), network)
}

// networkOverlayIPValue is the free-function core of networkOverlayIP, so the
// reconcile path (which has no handler) can resolve the same address.
func networkOverlayIPValue(nm account.NetworkManager, network string) string {
	if network == "" || network == account.DefaultNetworkName || nm == nil {
		return ""
	}
	n, err := nm.Get(network)
	if err != nil {
		return ""
	}
	addr, ok := overlayIP(n.Address)
	if !ok {
		return ""
	}
	return addr
}

// registerPackageDNSForNetwork plumbs a package's DNS into the one zone that
// matches its install network: the global home zone (registerPackageDNS) for the
// default network, or the network's scoped TLD zone (registerScopedPackageDNS)
// for any other network. A package must appear in exactly one — a non-default
// package leaking into the global home zone is the "resolves as .home" bug.
func (s *SystemControllerHandlers) registerPackageDNSForNetwork(ctx context.Context, network, repo, name string, domains []string) {
	if network == "" || network == account.DefaultNetworkName {
		s.registerPackageDNS(ctx, repo, name, domains)
		return
	}
	s.registerScopedPackageDNS(ctx, network, repo, name, domains)
}

// registerScopedPackageDNS best-effort dual-homes a non-default network
// package's DNS so it is reachable from BOTH the network's WireGuard overlay and
// the local (LAN) network — split-horizon by the querying source:
//
//   - SCOPED records (rolodex scope, served to overlay peers) point the
//     package's FQDNs at the box's overlay address, reachable over the tunnel;
//   - GLOBAL records (served to loopback/LAN clients) point the same FQDNs at
//     the box's internal LAN address, reachable on the local network.
//
// The box's ingress listens on all interfaces, so once a client resolves to an
// address it can route to (overlay IP over WireGuard, LAN IP over the LAN) it
// reaches the same package. Without the global records a LAN client resolves
// only the overlay IP and cannot connect. No-op for the default network (whose
// package DNS is already the global home zone) or when rolodex/networks are
// unavailable.
func (s *SystemControllerHandlers) registerScopedPackageDNS(ctx context.Context, network, repo, name string, domains []string) {
	if network == "" || network == account.DefaultNetworkName {
		return
	}
	rc := s.Controller.GetRolodexClient()
	nm := s.Controller.GetNetworkManager()
	if rc == nil || nm == nil {
		return
	}
	n, err := nm.Get(network)
	if err != nil {
		return
	}
	addr, ok := overlayIP(n.Address)
	if !ok {
		return
	}
	if err := rolodex.EnsureNetworkScope(ctx, rc, n.Name, n.TLD+"."); err != nil {
		logNonFatal("ensure scope for scoped dns", err)
		return
	}

	base := name + "." + repo + "." + n.TLD + "."
	names := []string{base}
	for _, d := range domains {
		names = append(names, d+"."+base)
	}
	// Overlay-facing scoped records (WireGuard peers).
	for _, fqdn := range names {
		rec := &upstream.DnsRecord{Name: fqdn, RecordType: upstream.RecordTypeA, Value: addr, Ttl: 300}
		if err := rc.AddScopedRecord(ctx, n.Name, rec); err != nil {
			logNonFatal("add scoped record "+fqdn, err)
		}
	}
	// LAN-facing global records (loopback/LAN clients), pointing the same FQDNs
	// at the box's internal address under the network's own TLD. This is the
	// same helper the default network uses, so the local view is consistent.
	ipv4 := s.Controller.GetInternalIP()
	ipv6 := s.Controller.GetInternalIPv6()
	if ipv4 == "" && ipv6 == "" {
		return
	}
	// A bare global A record resolves on the LAN without any authoritative zone:
	// rolodex's LAN->owning-scope fallback treats the network TLD (owned by the
	// scope EnsureNetworkScope created above) as authoritative for loopback/LAN
	// sources, so the global record wins for LAN clients and unmatched names in
	// the TLD yield an authoritative NXDOMAIN instead of leaking upstream. No
	// global SOA/NS zone is published (it would only duplicate the scoped apex).
	if err := rolodex.RegisterPackageDNS(ctx, rc, repo, name, n.TLD, ipv4, ipv6, domains); err != nil {
		logNonFatal("register global dns for network package", err)
	}
}

// unregisterScopedPackageDNS is the inverse of registerScopedPackageDNS: it
// removes both the overlay-facing scoped records and the LAN-facing global
// records for a non-default network package. Best-effort no-op for the default
// network (whose records live in the global home zone, cleaned by
// unregisterPackageDNS) or when rolodex/networks are unavailable.
func (s *SystemControllerHandlers) unregisterScopedPackageDNS(ctx context.Context, network, repo, name string, domains []string) {
	if network == "" || network == account.DefaultNetworkName {
		return
	}
	rc := s.Controller.GetRolodexClient()
	nm := s.Controller.GetNetworkManager()
	if rc == nil || nm == nil {
		return
	}
	n, err := nm.Get(network)
	if err != nil {
		return
	}

	base := name + "." + repo + "." + n.TLD + "."
	names := []string{base}
	for _, d := range domains {
		names = append(names, d+"."+base)
	}
	// Overlay-facing scoped records (nil opts removes every type for the name).
	for _, fqdn := range names {
		if _, err := rc.RemoveScopedRecord(ctx, n.Name, fqdn, nil); err != nil {
			logNonFatal("remove scoped record "+fqdn, err)
		}
	}
	// LAN-facing global records under the network TLD.
	if err := rolodex.UnregisterPackageDNS(ctx, rc, repo, name, n.TLD, domains); err != nil {
		logNonFatal("unregister global dns for network package", err)
	}
}

// logNonFatal logs a best-effort operation failure at debug level. Network
// transport and rolodex scope operations are non-fatal: the persisted network
// record is the source of truth and boot reconcile converges regardless.
func logNonFatal(what string, err error) {
	slog.Debug(fmt.Sprintf("%s: %v", what, err))
}

// buildNetwork assembles a new Network record: it derives the overlay subnet
// from the IPAM seed (systemd machine-id), generates a WireGuard keypair, and
// assigns a listen port from the current network count.
func (s *SystemControllerHandlers) buildNetwork(name, tld string, enabled bool) (*account.Network, error) {
	subnet, err := wireguard.SubnetForNetwork(networkIPAMSeed(), name)
	if err != nil {
		return nil, fmt.Errorf("derive subnet: %w", err)
	}
	priv, pub, err := wireguard.GenerateKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}

	// Derive a name-stable listen port, then probe forward past any port
	// already held by an existing network so remove-then-create can never
	// reuse a live port.
	port := wireguard.ListenPortForName(name)
	if nm := s.Controller.GetNetworkManager(); nm != nil {
		if nets, lerr := nm.List(); lerr == nil {
			used := make(map[int]bool, len(nets))
			for _, e := range nets {
				used[e.ListenPort] = true
			}
			for used[port] {
				port++
			}
		}
	}

	return &account.Network{
		Name:       name,
		TLD:        tld,
		Subnet:     subnet.String(),
		Address:    wireguard.AddressCIDR(subnet),
		PublicKey:  pub,
		PrivateKey: priv,
		ListenPort: port,
		Enabled:    enabled,
	}, nil
}

// networkConfigPath returns the host path of a network's wg-quick config file.
// wg-quick derives the interface name from the file basename.
func networkConfigPath(statePath, networkName string) string {
	return filepath.Join(statePath, wireguard.InterfaceName(networkName)+".conf")
}

// applyNetworkTransport renders the WireGuard config for a network, installs its
// systemd unit, and starts or stops the interface based on Enabled. It also
// best-effort ensures the matching rolodex scope. Missing systemd/state-dir
// (unit tests) is a no-op. All failures below the record write are non-fatal.
func (s *SystemControllerHandlers) applyNetworkTransport(ctx context.Context, n *account.Network) error {
	statePath := s.Controller.GetNetworkStatePath()
	sd := s.Controller.GetSystemdManager()

	// Best-effort rolodex scope regardless of transport availability. Every
	// network — including the default/home network — owns its TLD as a rolodex
	// scope home_domain (set by EnsureNetworkScope), which rolodex treats as the
	// network's authoritative owned zone. Owning the TLD is what partitions it
	// away from foreign WireGuard peers: rolodex hides a scope's TLD from peers
	// joined to a different scope, so .home is hidden from every overlay peer
	// even though it has no WireGuard transport of its own.
	if rc := s.Controller.GetRolodexClient(); rc != nil {
		if err := rolodex.EnsureNetworkScope(ctx, rc, n.Name, n.TLD+"."); err != nil {
			logNonFatal("ensure network scope", err)
		}
		// Non-default networks are WireGuard overlays: publish the network TLD's
		// zone apex (SOA/NS/ns1) scoped to the network so the owned zone is
		// authoritative and resolvable on the overlay, bind the box's overlay
		// address into the scope, and reconcile peer forwarders. The default
		// network has NO WireGuard transport — its home zone is global (SetupDNS),
		// and it never binds an overlay address or peers, so no source IP is ever
		// associated with the home scope and .home stays LAN-only.
		if n.Name != account.DefaultNetworkName {
			ns1IP, _ := overlayIP(n.Address)
			if err := rolodex.EnsureScopedTLD(ctx, rc, n.Name, n.TLD, ns1IP, ""); err != nil {
				logNonFatal("ensure scoped TLD zone", err)
			}
			if n.Enabled {
				if addr, ok := overlayIP(n.Address); ok {
					if err := rolodex.BindOverlayAddress(ctx, rc, addr, n.Name); err != nil {
						logNonFatal("bind overlay address", err)
					}
				}
				// Per-TLD peer forwarders: peers that run their own rolodex become
				// forwarders for the shared network TLD, so records authoritative on
				// a peer resolve across the overlay. We also bind each such peer's
				// overlay IP into the scope (symmetric) so the peer's queries to us
				// are answered rather than REFUSED.
				s.reconcilePeerForwarders(ctx, rc, n)
			}
		}
	}

	// The default/home network is LAN-only: it has no WireGuard interface,
	// overlay subnet transport, or peers. Only non-default networks install a
	// WireGuard systemd unit. Tear down any home WG transport left behind by an
	// older build so an upgraded box drops the interface.
	if n.Name == account.DefaultNetworkName {
		s.teardownNetworkTransport(ctx, n.Name)
		return nil
	}

	if statePath == "" || sd == nil {
		return nil
	}

	configPath := networkConfigPath(statePath, n.Name)
	peers, err := s.networkPeerConfigs(n.Name)
	if err != nil {
		return err
	}
	content := wireguard.RenderInterfaceConfig(wireguard.InterfaceConfig{
		PrivateKey: n.PrivateKey,
		Address:    n.Address,
		ListenPort: n.ListenPort,
		Peers:      peers,
	})
	if err := os.MkdirAll(statePath, 0700); err != nil {
		return fmt.Errorf("create network state dir: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("write wireguard config: %w", err)
	}

	unit := systemd.GenerateNetworkUnit(systemd.NetworkUnitConfig{Name: n.Name, ConfigPath: configPath})
	if err := sd.InstallUnit(ctx, unit.Name, unit.Content); err != nil {
		return fmt.Errorf("install network unit: %w", err)
	}

	action := systemd.Restart
	if !n.Enabled {
		action = systemd.Stop
	}
	if err := sd.SetStatus(ctx, unit.Name, action); err != nil {
		return fmt.Errorf("set network unit status: %w", err)
	}
	return nil
}

// teardownNetworkTransport stops and removes a network's systemd unit and its
// config file. Best-effort: errors are logged, not returned.
func (s *SystemControllerHandlers) teardownNetworkTransport(ctx context.Context, name string) {
	sd := s.Controller.GetSystemdManager()
	if sd != nil {
		if err := sd.UninstallUnit(ctx, systemd.NetworkUnitName(name)); err != nil {
			logNonFatal("uninstall network unit", err)
		}
	}
	if statePath := s.Controller.GetNetworkStatePath(); statePath != "" {
		if err := os.Remove(networkConfigPath(statePath, name)); err != nil && !os.IsNotExist(err) {
			logNonFatal("remove wireguard config", err)
		}
	}
}

// networkPeerConfigs loads a network's peers as WireGuard peer configs.
func (s *SystemControllerHandlers) networkPeerConfigs(name string) ([]wireguard.PeerConfig, error) {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return nil, nil
	}
	peers, err := nm.ListPeers(name)
	if err != nil {
		return nil, fmt.Errorf("list peers: %w", err)
	}
	out := make([]wireguard.PeerConfig, 0, len(peers))
	for _, p := range peers {
		pc := wireguard.PeerConfig{PublicKey: p.PublicKey, AllowedIPs: p.AllowedIP, Endpoint: p.Endpoint}
		if p.Endpoint != "" {
			pc.Keepalive = 25
		}
		out = append(out, pc)
	}
	return out, nil
}

// networkUnitRunning reports whether a network's WireGuard unit is active.
func (s *SystemControllerHandlers) networkUnitRunning(ctx context.Context, name string) bool {
	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return false
	}
	states, err := sd.GetUnitStates(ctx, []string{systemd.NetworkUnitName(name)})
	if err != nil || len(states) == 0 {
		return false
	}
	return states[0].ActiveState == "active"
}

// renderPeerDeviceConfig produces a wg-quick config a user can import on a
// device to join the network. PrivateKey is filled only when the server
// generated the keypair; otherwise a placeholder is emitted.
func (s *SystemControllerHandlers) renderPeerDeviceConfig(n *account.Network, subnet netip.Prefix, peerAddr netip.Addr, devicePrivateKey string) string {
	priv := devicePrivateKey
	if priv == "" {
		priv = "REPLACE_WITH_YOUR_PRIVATE_KEY"
	}

	endpointHost := s.Controller.GetExternalIP()
	if endpointHost == "" {
		endpointHost = s.Controller.GetInternalIP()
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", priv)
	fmt.Fprintf(&b, "Address = %s/32\n", peerAddr)
	fmt.Fprintf(&b, "DNS = %s\n", wireguard.LocalAddr(subnet))
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", n.PublicKey)
	if endpointHost != "" {
		fmt.Fprintf(&b, "Endpoint = %s:%d\n", endpointHost, n.ListenPort)
	}
	fmt.Fprintf(&b, "AllowedIPs = %s\n", n.Subnet)
	b.WriteString("PersistentKeepalive = 25\n")
	return b.String()
}

// overlayIP extracts the bare IP (no prefix) from an "a.b.c.d/n" CIDR address.
func overlayIP(cidr string) (string, bool) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return "", false
	}
	return p.Addr().String(), true
}

// reconcilePeerForwarders makes a network's per-TLD forwarder set equal the
// overlay addresses of its peers that run rolodex, and binds each such peer's
// overlay IP into the scope so cross-box resolution is symmetric. All steps are
// best-effort/non-fatal: the persisted network + peer rows are the source of
// truth and a later reconcile converges.
func (s *SystemControllerHandlers) reconcilePeerForwarders(ctx context.Context, rc rolodex.Client, n *account.Network) {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return
	}
	peers, err := nm.ListPeers(n.Name)
	if err != nil {
		logNonFatal("list network peers", err)
		return
	}
	fwds := make([]string, 0, len(peers))
	for _, p := range peers {
		if !p.Rolodex {
			continue
		}
		ip, ok := overlayIP(p.AllowedIP)
		if !ok {
			continue
		}
		fwds = append(fwds, net.JoinHostPort(ip, "53"))
		if err := rolodex.BindOverlayAddress(ctx, rc, ip, n.Name); err != nil {
			logNonFatal("bind peer overlay", err)
		}
	}
	if err := rolodex.ReconcileTldForwarders(ctx, rc, n.Name, n.TLD+".", fwds); err != nil {
		logNonFatal("reconcile tld forwarders", err)
	}
}

// ensureDefaultNetwork creates the "home" network row if it does not exist,
// deriving its TLD from the dns_tld setting. Idempotent.
func (s *SystemControllerHandlers) ensureDefaultNetwork() error {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return nil
	}
	if _, err := nm.Get(account.DefaultNetworkName); err == nil {
		return nil
	} else if !errors.Is(err, account.ErrNetworkNotFound) {
		return fmt.Errorf("get default network: %w", err)
	}

	n, err := s.buildNetwork(account.DefaultNetworkName, s.getDNSTLDValue(), true)
	if err != nil {
		return err
	}
	if _, err := nm.Create(n); err != nil && !errors.Is(err, account.ErrDuplicateNetwork) {
		return fmt.Errorf("create default network: %w", err)
	}
	return nil
}

// reconcileNetworks ensures the default network exists and (re)applies the
// WireGuard transport for every network at boot, starting enabled interfaces
// and leaving disabled ones down. All failures are non-fatal.
func (s *SystemControllerHandlers) reconcileNetworks(ctx context.Context) {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return
	}
	if err := s.ensureDefaultNetwork(); err != nil {
		logNonFatal("ensure default network", err)
	}
	nets, err := nm.List()
	if err != nil {
		logNonFatal("list networks for reconcile", err)
		return
	}
	for i := range nets {
		if err := s.applyNetworkTransport(ctx, &nets[i]); err != nil {
			logNonFatal("apply network "+nets[i].Name, err)
		}
	}
}

// ReconcileNetworksConfig carries the dependencies the boot-time network
// reconcile needs.
type ReconcileNetworksConfig struct {
	NetworkMgr       account.NetworkManager
	Systemd          systemd.Manager
	NetworkStatePath string
	SettingsMgr      account.SettingsManager
	RolodexClient    rolodex.Client
}

// ReconcileNetworks is the boot-time entry point (mirrors RebuildDNS). It
// ensures the default network exists and brings every enabled network's
// WireGuard interface up. All failures are non-fatal.
func ReconcileNetworks(ctx context.Context, cfg ReconcileNetworksConfig) {
	sb := &serverBase{ServerConfig: ServerConfig{
		NetworkMgr:       cfg.NetworkMgr,
		Systemd:          cfg.Systemd,
		NetworkStatePath: cfg.NetworkStatePath,
		SettingsMgr:      cfg.SettingsMgr,
		RolodexClient:    cfg.RolodexClient,
	}}
	getHandler(ctx, sb).reconcileNetworks(ctx)
}
