// Package wireguard generates WireGuard keypairs and renders wg-quick-style
// interface configuration. It performs no interface control itself: the
// systemcontroller writes the rendered config to the host-shared network-state
// directory and a generated systemd unit brings the kernel interface up/down.
// This keeps the systemcontroller container free of host network-namespace
// requirements.
package wireguard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"golang.org/x/crypto/curve25519"
)

// saltedKey mixes an instance salt into a derivation input.
//
// The empty salt returns the bare key, so every salted derivation reproduces
// its historical unsalted value bit-for-bit on a production box. That is the
// whole contract: a salt is an opt-in for instances that share a network
// namespace with another Town OS, never something a real box carries. If this
// ever stops being an identity for "", live interfaces get renamed and stored
// subnets stop matching the devices they were allocated for.
func saltedKey(salt, key string) string {
	if salt == "" {
		return key
	}
	return salt + "|" + key
}

// InterfaceName derives a stable, kernel-legal (<=15 char) WireGuard interface
// name for a network. wg-quick derives the interface from the config filename,
// so the config is written as "<InterfaceName>.conf". The name is a hash of the
// network name so it is stable across create order and independent of how many
// networks currently exist.
//
// salt differentiates instances that share a network namespace. A kernel
// interface name is namespace-global, so a `make test-full` box and a `make dev`
// box — both running --net host — otherwise derive the same "townXXXX" for the
// same network name and the second one to come up fails to create its device.
// Production passes "" and gets the historical name; see saltedKey.
//
// The salt only widens the hash input, so the result is the same 8 characters
// regardless of how long the salt is — it can never push the name past the
// kernel's 15-character limit.
func InterfaceName(salt, networkName string) string {
	h := sha256.Sum256([]byte(saltedKey(salt, networkName)))
	return "town" + hex.EncodeToString(h[:2]) // "town" + 4 hex = 8 chars
}

// PeerConfig describes one WireGuard peer entry.
type PeerConfig struct {
	// PublicKey is the peer's base64-encoded Curve25519 public key.
	PublicKey string
	// AllowedIPs is the comma-separated set of CIDRs routed to this peer
	// (typically the peer's single overlay address as a /32).
	AllowedIPs string
	// Endpoint is an optional host:port the peer can be reached at.
	Endpoint string
	// Keepalive is the persistent-keepalive interval in seconds (0 = off).
	Keepalive int
}

// InterfaceConfig describes a WireGuard interface for a single network.
type InterfaceConfig struct {
	// PrivateKey is the interface's base64-encoded Curve25519 private key.
	PrivateKey string
	// Address is the interface's overlay address in CIDR form (e.g. 10.90.12.1/24).
	Address string
	// ListenPort is the UDP port the interface listens on.
	ListenPort int
	// Peers are the peers permitted on this interface.
	Peers []PeerConfig
}

// GenerateKeypair returns a fresh base64-encoded Curve25519 (private, public)
// keypair suitable for WireGuard. The private key is clamped per the X25519
// specification before deriving the public key.
func GenerateKeypair() (privateKey, publicKey string, err error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("generate wireguard private key: %w", err)
	}

	// Clamp the scalar as required by X25519.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return "", "", fmt.Errorf("derive wireguard public key: %w", err)
	}

	return base64.StdEncoding.EncodeToString(priv[:]),
		base64.StdEncoding.EncodeToString(pub), nil
}

// PublicKeyFromPrivate derives the base64-encoded public key for a
// base64-encoded WireGuard private key.
func PublicKeyFromPrivate(privateKey string) (string, error) {
	priv, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return "", fmt.Errorf("decode wireguard private key: %w", err)
	}
	if len(priv) != 32 {
		return "", fmt.Errorf("wireguard private key must be 32 bytes, got %d", len(priv))
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return "", fmt.Errorf("derive wireguard public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// RenderInterfaceConfig renders a wg-quick-style configuration document for the
// interface. Peers are emitted in the order supplied by the caller (callers
// pass a deterministically sorted slice so the output is stable).
//
// A field carrying a newline is DROPPED rather than emitted: the document is
// executed by `wg-quick up` as root, and wg-quick decides which section it is
// in — and therefore whether a PostUp line is a hook it will eval — from the
// file's own content. See validate.go for why that is the whole attack.
//
// Dropping rather than erroring is deliberate. Callers write this file during
// reconcile, where the alternative to rendering something is rendering nothing
// and taking the whole overlay down; and the values reaching here have already
// been validated at the API boundary, so anything this catches is a row that
// predates the check or arrived around it. Silently executing such a row every
// boot is the failure mode worth removing, so the peer is skipped and the fact
// is logged loudly.
func RenderInterfaceConfig(cfg InterfaceConfig) string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	// The interface's own fields are server-generated (a keypair this package
	// made, a subnet it derived), so an unsafe value here means the database
	// was written by something other than this code path. Emit the key as empty
	// rather than dropping the line: wg-quick needs the field to exist, and an
	// interface that fails to come up is a better outcome than one that comes up
	// running somebody's hook.
	privateKey := cfg.PrivateKey
	if !safeConfigValue(privateKey) {
		slog.Error("wireguard: refusing to render an interface private key containing newlines")
		privateKey = ""
	}
	fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	if cfg.Address != "" && safeConfigValue(cfg.Address) {
		fmt.Fprintf(&b, "Address = %s\n", cfg.Address)
	}
	if cfg.ListenPort != 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", cfg.ListenPort)
	}

	for _, p := range cfg.Peers {
		if !safePeer(p) {
			slog.Error("wireguard: dropping a peer whose configuration would restructure the wg-quick document; "+
				"wg-quick executes PostUp/PreUp hooks as root",
				"allowed_ips", strconv.Quote(p.AllowedIPs))
			continue
		}
		b.WriteString("\n[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
		if p.AllowedIPs != "" {
			fmt.Fprintf(&b, "AllowedIPs = %s\n", p.AllowedIPs)
		}
		if p.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
		}
		if p.Keepalive != 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", p.Keepalive)
		}
	}

	return b.String()
}
