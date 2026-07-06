package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/wireguard"
	"github.com/labstack/echo/v5"
)

// machineIDPath is the file whose contents seed per-network IPAM. Overridable
// in tests. A stable seed keeps each box's overlay subnets collision-free
// across reboots.
var machineIDPath = "/etc/machine-id"

// networkIPAMSeed returns a stable seed for subnet derivation: the systemd
// machine-id when available, else the hostname, else a constant. It never
// returns "" so wireguard.SubnetForNetwork always succeeds.
func networkIPAMSeed() string {
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
}

type RemoveNetworkPeerRequest struct {
	Network   string `json:"network"`
	PublicKey string `json:"public_key"`
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
			Interface: wireguard.InterfaceName(n.Name),
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

	created.PrivateKey = ""
	return c.JSON(200, NetworkView{Network: *created, Interface: wireguard.InterfaceName(created.Name)})
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

	stored, err := nm.AddPeer(&account.NetworkPeer{
		Network:   n.Name,
		PublicKey: publicKey,
		Name:      strings.TrimSpace(req.Name),
		AllowedIP: allowedIP,
		Endpoint:  strings.TrimSpace(req.Endpoint),
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
		Config:     s.renderPeerDeviceConfig(n, subnet, peerAddr, privateKey),
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
