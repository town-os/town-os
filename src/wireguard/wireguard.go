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
	"strings"

	"golang.org/x/crypto/curve25519"
)

// InterfaceName derives a stable, kernel-legal (<=15 char) WireGuard interface
// name for a network. wg-quick derives the interface from the config filename,
// so the config is written as "<InterfaceName>.conf". The name is a hash of the
// network name so it is stable across create order and independent of how many
// networks currently exist.
func InterfaceName(networkName string) string {
	h := sha256.Sum256([]byte(networkName))
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
func RenderInterfaceConfig(cfg InterfaceConfig) string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", cfg.PrivateKey)
	if cfg.Address != "" {
		fmt.Fprintf(&b, "Address = %s\n", cfg.Address)
	}
	if cfg.ListenPort != 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", cfg.ListenPort)
	}

	for _, p := range cfg.Peers {
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
