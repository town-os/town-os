package account

import (
	"errors"
	"regexp"
	"time"
)

// DefaultNetworkName is the always-present default network. It maps to the
// default DNS TLD (dns_tld, "home") and can never be removed.
const DefaultNetworkName = "home"

var (
	ErrNetworkNotFound      = errors.New("network not found")
	ErrDuplicateNetwork     = errors.New("network already exists")
	ErrNetworkNameRequired  = errors.New("network name is required")
	ErrNetworkNameInvalid   = errors.New("network name must be lowercase alphanumeric with dashes")
	ErrNetworkProtected     = errors.New("the default network cannot be removed")
	ErrNetworkPeerNotFound  = errors.New("network peer not found")
	ErrNetworkPeerKeyReq    = errors.New("peer public key is required")
	ErrDuplicateNetworkPeer = errors.New("peer already exists on this network")
)

// networkNameRegexp constrains network names to DNS-label-safe tokens so they
// can be reused as WireGuard interface suffixes and systemd unit names.
var networkNameRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidNetworkName reports whether name is a legal network identifier.
func ValidNetworkName(name string) bool {
	return name != "" && len(name) <= 32 && networkNameRegexp.MatchString(name)
}

// Network is a named WireGuard overlay paired with a DNS TLD. The box's own
// overlay address is Address (the ".1" host of Subnet). Enabled controls
// whether the WireGuard interface is brought up: when false, remote access to
// services on the network is cut while local DNS resolution and the containers
// themselves keep running.
type Network struct {
	Name       string    `json:"name"`
	TLD        string    `json:"tld"`
	Subnet     string    `json:"subnet"`
	Address    string    `json:"address"`
	PublicKey  string    `json:"public_key"`
	PrivateKey string    `json:"-"`
	ListenPort int       `json:"listen_port"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NetworkPeer is a device permitted on a network's overlay.
type NetworkPeer struct {
	Network   string    `json:"network"`
	PublicKey string    `json:"public_key"`
	Name      string    `json:"name"`
	AllowedIP string    `json:"allowed_ip"`
	Endpoint  string    `json:"endpoint"`
	CreatedAt time.Time `json:"created_at"`
}

// NetworkManager persists networks and their peers. Key generation, subnet
// derivation (from the systemd machine-id) and listen-port assignment happen in
// the caller; the manager is a pure store.
type NetworkManager interface {
	Create(n *Network) (*Network, error)
	Get(name string) (*Network, error)
	List() ([]Network, error)
	Remove(name string) error
	SetEnabled(name string, enabled bool) error
	Count() (int, error)

	AddPeer(p *NetworkPeer) (*NetworkPeer, error)
	RemovePeer(network, publicKey string) error
	ListPeers(network string) ([]NetworkPeer, error)
}
