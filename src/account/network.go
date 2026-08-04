package account

import (
	"errors"
	"regexp"
	"time"
)

// DefaultNetworkName is the always-present default network. It maps to the
// default DNS TLD (dns_tld, "home") and can never be removed.
const DefaultNetworkName = "home"

// DefaultNetwork is the home network as it exists before anything configures
// it: named, enabled, and carrying no WireGuard transport at all.
//
// The empty Subnet/keys/ListenPort are the truth rather than a placeholder --
// the home network is DNS-only (applyNetworkTransport tears down any interface
// it finds on it), so a derived subnet and keypair would be fields nothing ever
// reads. Its TLD is the bare default; the controller reconciles it to the
// dns_tld setting at boot, which is the only place that setting is known.
func DefaultNetwork() *Network {
	return &Network{Name: DefaultNetworkName, TLD: DefaultNetworkName, Enabled: true}
}

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
	Network   string `json:"network"`
	PublicKey string `json:"public_key"`
	Name      string `json:"name"`
	AllowedIP string `json:"allowed_ip"`
	Endpoint  string `json:"endpoint"`
	// Rolodex reports whether this peer runs a rolodex DNS server on its overlay
	// address. When true, the box registers the peer's overlay address as a
	// per-TLD forwarder so names under the network's shared TLD that are
	// authoritative on the peer resolve across the overlay. Non-rolodex peers
	// (phones, laptops) leave this false.
	Rolodex bool `json:"rolodex"`
	// CreatedBy is the username of the account that enrolled this peer, or empty
	// for peers added by a localhost/legacy path. It is the ownership key: a
	// network-only account may refresh (and the operator audit) only the peers
	// it created, so a scoped account cannot keep another account's peer alive.
	CreatedBy string `json:"created_by,omitempty"`
	// ExpiresAt is when this enrollment lapses and the reaper removes it. A nil
	// value means the peer never expires — permanent peers such as rolodex
	// servers and operator-added devices. A long-lived client (the portal)
	// carries a TTL here and refreshes it before it elapses; an abandoned
	// device's peer expires on its own, so the additive peers/add endpoint
	// cannot silently accumulate dead peers and burn overlay addresses.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
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
	// SetTLD repoints a network at a different DNS TLD. It exists for the home
	// network, whose TLD is the dns_tld setting: the row is seeded before that
	// setting can be read, and `POST /dns/tld` can change it afterwards, so the
	// controller reconciles the two at boot. Returns [ErrNetworkNotFound] if the
	// network does not exist.
	SetTLD(name, tld string) error
	Count() (int, error)

	AddPeer(p *NetworkPeer) (*NetworkPeer, error)
	RemovePeer(network, publicKey string) error
	ListPeers(network string) ([]NetworkPeer, error)

	// RefreshPeer extends a peer's expiry to expiresAt. This is the heartbeat
	// that keeps a long-lived enrollment (the portal) alive across the TTL
	// window. Returns [ErrNetworkPeerNotFound] if the peer does not exist.
	RefreshPeer(network, publicKey string, expiresAt time.Time) error
	// ReapExpiredPeers deletes every peer whose expiry is non-nil and at or
	// before now, returning the removed peers so the caller can tear down their
	// runtime state (the live WireGuard device, rolodex DNS forwarders). Peers
	// with no expiry are never reaped.
	ReapExpiredPeers(now time.Time) ([]NetworkPeer, error)
}
