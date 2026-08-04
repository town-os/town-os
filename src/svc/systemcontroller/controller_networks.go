package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/wireguard"
	"github.com/labstack/echo/v5"
)

// machineIDPath is the file whose contents seed per-network IPAM. Overridable
// in tests. A stable seed keeps each box's overlay subnets collision-free
// across reboots.
var machineIDPath = "/etc/machine-id"

// EnvWireGuardSalt names the environment variable that differentiates this
// instance's WireGuard identity — interface name, UDP listen port, and overlay
// subnet — from another Town OS sharing the same network namespace.
//
// All three of those live in the network namespace, and the test and dev
// containers both run --net host, so without a salt a `make test-full` box and a
// `make dev` box derive an identical interface name and listen port for the same
// network name: the second one to come up cannot create its device or bind its
// port, and its overlay is simply dead. Two concurrent test worktrees collide
// the same way — IRON RULE.
//
// Unset on a real box, which is what keeps production names unchanged.
const EnvWireGuardSalt = "TOWN_OS_WG_SALT"

// wireGuardSalt is the instance salt, read once from the environment. A package
// var (rather than a plumbed-through config field) mirrors machineIDPath above:
// both are boot-constant properties of the box's identity that several free
// functions here need, and both are settable by tests.
var wireGuardSalt = os.Getenv(EnvWireGuardSalt)

// NetworkInterfaceName is the WireGuard interface name this instance uses for a
// network, with the salt already applied.
//
// Exported for the integration tests, which live outside this package and have
// to name the same device the controller does. Deriving it there through
// wireguard.InterfaceName means also remembering to pass the salt, and the
// value that is easiest to pass is "" — which is correct on a production box
// and wrong in every container the tests run in, where a salt is precisely what
// stops two checkouts fighting over one kernel device. Naming the pairing once,
// here, is what keeps a test from asserting against a device nothing created.
func NetworkInterfaceName(network string) string {
	return wireguard.InterfaceName(wireGuardSalt, network)
}

// networkIPAMSeed returns a stable seed for subnet derivation: the systemd
// machine-id when available, else the hostname, else a constant. It never
// returns "" so wireguard.SubnetForNetwork always succeeds.
//
// The salt is folded in here rather than passed to SubnetForNetwork separately,
// because a salt is precisely more box identity — the same role the machine-id
// already plays — and this keeps the subnet derivation to one seed argument.
// Note that /etc/machine-id is generated per container boot, so two containers
// usually differ anyway; usually is not a guarantee worth resting a shared
// routing table on, and the hostname fallback is not distinct at all.
func networkIPAMSeed() string {
	return saltIPAMSeed(wireGuardSalt, rawIPAMSeed())
}

// saltIPAMSeed mixes the instance salt into an IPAM seed. An empty salt returns
// the seed untouched so an existing box keeps the subnets it already allocated.
func saltIPAMSeed(salt, seed string) string {
	if salt == "" {
		return seed
	}
	return salt + "|" + seed
}

// rawIPAMSeed is the unsalted box identity: machine-id, else hostname, else a
// constant.
func rawIPAMSeed() string {
	if data, err := os.ReadFile(machineIDPath); err == nil {
		if s := strings.TrimSpace(string(data)); s != "" {
			return s
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "town-os"
}

// --- request/response types ---

type CreateNetworkRequest struct {
	Name string `json:"name"`
	TLD  string `json:"tld"`
}

type NetworkNameRequest struct {
	Name string `json:"name"`
}

type AddNetworkPeerRequest struct {
	Network   string `json:"network"`
	Name      string `json:"name"`
	PublicKey string `json:"public_key"` // optional: device's own key; generated when empty
	Endpoint  string `json:"endpoint"`   // optional: device's reachable endpoint
	Rolodex   bool   `json:"rolodex"`    // optional: peer runs a rolodex DNS server on its overlay address
}

type RemoveNetworkPeerRequest struct {
	Network   string `json:"network"`
	PublicKey string `json:"public_key"`
}

// RefreshNetworkPeerRequest asks to extend a peer's TTL. The new expiry is
// server-computed as now + peer_ttl; the caller does not choose it.
type RefreshNetworkPeerRequest struct {
	Network   string `json:"network"`
	PublicKey string `json:"public_key"`
}

// RefreshPeerResult reports a peer's new expiry so a client can pace its next
// heartbeat well before the TTL elapses.
type RefreshPeerResult struct {
	Network   string    `json:"network"`
	PublicKey string    `json:"public_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

// NetworkView is the API representation of a network (private key omitted).
type NetworkView struct {
	account.Network

	PeerCount int    `json:"peer_count"`
	Interface string `json:"interface"`
	Running   bool   `json:"running"`
}

// AddPeerResult carries the stored peer plus a ready-to-import device config.
// PrivateKey is populated only when the server generated the keypair.
type AddPeerResult struct {
	Peer       account.NetworkPeer `json:"peer"`
	PrivateKey string              `json:"private_key,omitempty"`
	Config     string              `json:"config"`
}

// --- handlers ---

// listNetworks handles GET /networks.
func (s *SystemControllerHandlers) listNetworks(c *echo.Context) error {
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return c.JSON(200, []NetworkView{})
	}
	nets, err := nm.List()
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list networks: %v", err))
	}

	views := make([]NetworkView, 0, len(nets))
	for _, n := range nets {
		peers, perr := nm.ListPeers(n.Name)
		if perr != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("list peers: %v", perr))
		}
		n.PrivateKey = "" // never expose in the API
		views = append(views, NetworkView{
			Network:   n,
			PeerCount: len(peers),
			Interface: wireguard.InterfaceName(wireGuardSalt, n.Name),
			Running:   s.networkUnitRunning(c.Request().Context(), n.Name),
		})
	}
	return c.JSON(200, views)
}

// createNetwork handles POST /networks/create.
func (s *SystemControllerHandlers) createNetwork(c *echo.Context) error {
	var req CreateNetworkRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	req.Name = strings.TrimSpace(req.Name)
	if !account.ValidNetworkName(req.Name) {
		return echo.NewHTTPError(400, "network name must be lowercase alphanumeric with dashes")
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}

	tld := strings.TrimSpace(strings.ToLower(req.TLD))
	if tld == "" {
		tld = req.Name
	}
	if err := ValidateTLD(tld); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid tld: %v", err))
	}

	// Reject a TLD already claimed by another network.
	existing, err := nm.List()
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list networks: %v", err))
	}
	for _, e := range existing {
		if e.TLD == tld {
			return echo.NewHTTPError(409, fmt.Sprintf("tld %q is already used by network %q", tld, e.Name))
		}
	}

	n, err := s.buildNetwork(req.Name, tld, true)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("build network: %v", err))
	}
	created, err := nm.Create(n)
	if err != nil {
		if errors.Is(err, account.ErrDuplicateNetwork) {
			return echo.NewHTTPError(409, "network already exists")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("create network: %v", err))
	}

	if err := s.applyNetworkTransport(c.Request().Context(), created); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("apply network: %v", err))
	}

	// A network is also a gfeh partition, so provision one and republish the
	// names it contributes. Best-effort: a partition that does not come up is
	// a network without object storage, not a failed network creation, and the
	// next reconcile tries again.
	s.reconcileGfeh(c.Request().Context())

	created.PrivateKey = ""
	return c.JSON(200, NetworkView{Network: *created, Interface: wireguard.InterfaceName(wireGuardSalt, created.Name)})
}

// removeNetwork handles POST /networks/remove.
func (s *SystemControllerHandlers) removeNetwork(c *echo.Context) error {
	var req NetworkNameRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	if req.Name == account.DefaultNetworkName {
		return echo.NewHTTPError(400, "the default network cannot be removed")
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}

	ctx := c.Request().Context()
	// Tear the transport down before dropping the record.
	s.teardownNetworkTransport(ctx, req.Name)

	if err := nm.Remove(req.Name); err != nil {
		switch {
		case errors.Is(err, account.ErrNetworkNotFound):
			return echo.NewHTTPError(404, "network not found")
		case errors.Is(err, account.ErrNetworkProtected):
			return echo.NewHTTPError(400, "the default network cannot be removed")
		default:
			return echo.NewHTTPError(500, fmt.Sprintf("remove network: %v", err))
		}
	}

	if rc := s.Controller.GetRolodexClient(); rc != nil {
		if err := rc.DeleteNetworkScope(ctx, req.Name); err != nil {
			logNonFatal("delete network scope", err)
		}
	}

	// Stop the partition's daemon and drop its ingress routes. The subvolume
	// stays: removing a network says nothing about the bytes stored under it,
	// and deleting them here would make a mistyped name unrecoverable.
	// POST /gfeh/partitions/remove is the operation that says what it does.
	s.reconcileGfeh(ctx)

	return c.JSON(200, map[string]any{"status": "ok", "name": req.Name})
}

// enableNetwork handles POST /networks/enable.
func (s *SystemControllerHandlers) enableNetwork(c *echo.Context) error {
	return s.setNetworkEnabled(c, true)
}

// disableNetwork handles POST /networks/disable.
func (s *SystemControllerHandlers) disableNetwork(c *echo.Context) error {
	return s.setNetworkEnabled(c, false)
}

func (s *SystemControllerHandlers) setNetworkEnabled(c *echo.Context, enabled bool) error {
	var req NetworkNameRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}
	if err := nm.SetEnabled(req.Name, enabled); err != nil {
		if errors.Is(err, account.ErrNetworkNotFound) {
			return echo.NewHTTPError(404, "network not found")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("set network enabled: %v", err))
	}

	n, err := nm.Get(req.Name)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("get network: %v", err))
	}
	if err := s.applyNetworkTransport(c.Request().Context(), n); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("apply network: %v", err))
	}

	return c.JSON(200, map[string]any{"status": "ok", "name": req.Name, "enabled": enabled})
}

// listNetworkPeers handles GET /networks/peers?network=<name>.
func (s *SystemControllerHandlers) listNetworkPeers(c *echo.Context) error {
	network := c.QueryParam("network")
	if network == "" {
		return echo.NewHTTPError(400, "network is required")
	}
	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return c.JSON(200, []account.NetworkPeer{})
	}
	peers, err := nm.ListPeers(network)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list peers: %v", err))
	}
	return c.JSON(200, peers)
}

// addNetworkPeer handles POST /networks/peers/add.
func (s *SystemControllerHandlers) addNetworkPeer(c *echo.Context) error {
	var req AddNetworkPeerRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	if req.Network == "" {
		return echo.NewHTTPError(400, "network is required")
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}
	n, err := nm.Get(req.Network)
	if err != nil {
		if errors.Is(err, account.ErrNetworkNotFound) {
			return echo.NewHTTPError(404, "network not found")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("get network: %v", err))
	}

	// Generate a keypair server-side when the device did not supply a public key.
	publicKey := strings.TrimSpace(req.PublicKey)
	var privateKey string
	if publicKey == "" {
		priv, pub, gerr := wireguard.GenerateKeypair()
		if gerr != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("generate peer key: %v", gerr))
		}
		privateKey, publicKey = priv, pub
	}

	subnet, err := netip.ParsePrefix(n.Subnet)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("parse subnet: %v", err))
	}
	peers, err := nm.ListPeers(n.Name)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("list peers: %v", err))
	}
	used := map[string]bool{wireguard.LocalAddr(subnet).String(): true}
	for _, p := range peers {
		if addr, perr := netip.ParsePrefix(p.AllowedIP); perr == nil {
			used[addr.Addr().String()] = true
		}
	}
	peerAddr, err := wireguard.AllocatePeerAddr(subnet, used, publicKey)
	if err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("allocate peer address: %v", err))
	}
	allowedIP := fmt.Sprintf("%s/32", peerAddr)

	// Attribute the enrollment and, for a non-admin, enforce network scope and
	// stamp a TTL. Such an account may enroll only on a network in its scope,
	// and its peers expire on their own unless refreshed, so an
	// abandoned device cannot hold an overlay address forever. Admin enrollments
	// stay permanent (nil expiry), preserving the pre-TTL behavior.
	acct := s.callingAccount(c)
	var createdBy string
	var expiresAt *time.Time
	if acct != nil {
		createdBy = acct.Username
		if !acct.HoldsEveryGrant() {
			if !acct.MayAdministerNetwork(n.Name) {
				return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthNetworkOnlyNetworkDenied))
			}
			exp := time.Now().Add(s.peerTTL()).UTC()
			expiresAt = &exp
		}
	}

	stored, err := nm.AddPeer(&account.NetworkPeer{
		Network:   n.Name,
		PublicKey: publicKey,
		Name:      strings.TrimSpace(req.Name),
		AllowedIP: allowedIP,
		Endpoint:  strings.TrimSpace(req.Endpoint),
		Rolodex:   req.Rolodex,
		CreatedBy: createdBy,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		if errors.Is(err, account.ErrDuplicateNetworkPeer) {
			return echo.NewHTTPError(409, "peer already exists on this network")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("add peer: %v", err))
	}

	// Re-render the interface config and reload so the new peer takes effect.
	if err := s.applyNetworkTransport(c.Request().Context(), n); err != nil {
		return echo.NewHTTPError(500, fmt.Sprintf("apply network: %v", err))
	}

	return c.JSON(200, AddPeerResult{
		Peer:       *stored,
		PrivateKey: privateKey,
		Config:     s.renderPeerDeviceConfig(n, subnet, peerAddr, privateKey, peerEndpointHost(c)),
	})
}

// removeNetworkPeer handles POST /networks/peers/remove.
func (s *SystemControllerHandlers) removeNetworkPeer(c *echo.Context) error {
	var req RemoveNetworkPeerRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	if req.Network == "" || req.PublicKey == "" {
		return echo.NewHTTPError(400, "network and public_key are required")
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}
	if err := nm.RemovePeer(req.Network, req.PublicKey); err != nil {
		if errors.Is(err, account.ErrNetworkPeerNotFound) {
			return echo.NewHTTPError(404, "peer not found")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("remove peer: %v", err))
	}

	if n, err := nm.Get(req.Network); err == nil {
		if aerr := s.applyNetworkTransport(c.Request().Context(), n); aerr != nil {
			logNonFatal("apply network after peer removal", aerr)
		}
	}
	return c.JSON(200, map[string]any{"status": "ok"})
}

// refreshNetworkPeer handles POST /networks/peers/refresh. It slides a peer's
// TTL forward by peer_ttl — the heartbeat that keeps a long-lived enrollment
// (the portal) alive. A non-admin caller may refresh only a peer it
// enrolled on a network in its scope; an admin may refresh any peer. No
// transport reload is needed: the peer set is unchanged, only its expiry.
func (s *SystemControllerHandlers) refreshNetworkPeer(c *echo.Context) error {
	var req RefreshNetworkPeerRequest
	if err := json.NewDecoder(c.Request().Body).Decode(&req); err != nil {
		return echo.NewHTTPError(400, fmt.Sprintf("invalid request: %v", err))
	}
	if req.Network == "" || req.PublicKey == "" {
		return echo.NewHTTPError(400, "network and public_key are required")
	}

	nm := s.Controller.GetNetworkManager()
	if nm == nil {
		return echo.NewHTTPError(503, "network manager not available")
	}

	if acct := s.callingAccount(c); acct != nil && !acct.HoldsEveryGrant() {
		if !acct.MayAdministerNetwork(req.Network) {
			return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthNetworkOnlyNetworkDenied))
		}
		owned, err := s.peerOwnedBy(nm, req.Network, req.PublicKey, acct.Username)
		if err != nil {
			return echo.NewHTTPError(500, fmt.Sprintf("list peers: %v", err))
		}
		if !owned {
			return echo.NewHTTPError(403, i18n.T(s.getLocale(), i18n.MsgAuthWireGuardPeerNotOwned))
		}
	}

	expiresAt := time.Now().Add(s.peerTTL()).UTC()
	if err := nm.RefreshPeer(req.Network, req.PublicKey, expiresAt); err != nil {
		if errors.Is(err, account.ErrNetworkPeerNotFound) {
			return echo.NewHTTPError(404, "peer not found")
		}
		return echo.NewHTTPError(500, fmt.Sprintf("refresh peer: %v", err))
	}

	return c.JSON(200, RefreshPeerResult{Network: req.Network, PublicKey: req.PublicKey, ExpiresAt: expiresAt})
}

// peerOwnedBy reports whether the named peer exists on the network and was
// enrolled by username. A missing peer reports false (not owned), so the caller
// returns the ownership 403 rather than leaking existence — refresh of an absent
// peer and refresh of someone else's peer look identical to a scoped account.
func (s *SystemControllerHandlers) peerOwnedBy(nm account.NetworkManager, network, publicKey, username string) (bool, error) {
	peers, err := nm.ListPeers(network)
	if err != nil {
		return false, err
	}
	for _, p := range peers {
		if p.PublicKey == publicKey {
			return p.CreatedBy == username, nil
		}
	}
	return false, nil
}

// peerTTL returns the configured peer enrollment lifetime. It reads the peer_ttl
// setting (raw seconds) and falls back to two hours if the setting is missing,
// blank, unparseable, or non-positive — a corrupt setting must never yield a
// zero TTL that would expire every enrollment on the next reaper tick.
func (s *SystemControllerHandlers) peerTTL() time.Duration {
	const fallback = 2 * time.Hour
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return fallback
	}
	val, err := mgr.Get("peer_ttl")
	if err != nil {
		return fallback
	}
	secs, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || secs <= 0 {
		return fallback
	}
	return time.Duration(secs) * time.Second
}
