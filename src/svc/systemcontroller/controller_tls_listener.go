// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	cryptotls "crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	townostls "gitea.com/town-os/town-os/src/tls"
)

// TLS for the control plane's own listener.
//
// :5309 carries the login password on every sign-in and a bearer token on every
// request after that, and it is LAN-facing — reachable by every device on the
// network and by every WireGuard peer. Served as plain HTTP, all of it is
// readable by anything on the path. That is the one piece of Town OS traffic
// with no transport protection at all: packages and pages are already
// terminated by the ingress with a local-CA leaf.
//
// So the same CA terminates this listener, and the leaf is issued exactly like
// a package's — from the box's own root, with the names and addresses the box
// answers on as SANs, fetchable by any client through the public GET
// /tls/ca.crt.
//
// It is OFF by default, and that is a sequencing decision rather than a
// hedge. A browser that has not been given the box's CA cannot complete an XHR
// to an untrusted certificate, and unlike a navigation there is no interstitial
// to click through — the UI would simply stop working, with no way to reach the
// screen that explains why. The UI is also served over plain HTTP today (it is
// the ingress's default :80 backend), so a box that turned this on without
// installing the CA first would go from "unencrypted" to "down". The operator
// installs the CA, then sets TOWN_OS_TLS=1.
const (
	// EnvTLS enables TLS on the controller listener using the local CA.
	EnvTLS = "TOWN_OS_TLS"
	// EnvTLSCert and EnvTLSKey point at an operator-supplied certificate and
	// key, for a box fronted by a name with a publicly trusted cert. Setting
	// both enables TLS on its own; the local CA is not consulted.
	EnvTLSCert = "TOWN_OS_TLS_CERT" //nolint:gosec // G101 -- env var name, not a credential
	EnvTLSKey  = "TOWN_OS_TLS_KEY"  //nolint:gosec // G101 -- env var name, not a credential
	// EnvTLSSANs adds comma-separated names or IPs to the generated leaf, for
	// a box reached by a name this cannot derive (a CNAME, a router-assigned
	// DHCP name).
	EnvTLSSANs = "TOWN_OS_TLS_SANS"

	// ControllerTLSDirName is the leaf directory under the tls subvolume. A
	// directory of its own, beside the per-package ones, so the same
	// idempotent IssueLeaf applies and nothing special-cases it.
	ControllerTLSDirName = "controller"
)

// ControllerTLSRequested reports whether the operator asked for TLS.
func ControllerTLSRequested() bool {
	if certFile, keyFile := os.Getenv(EnvTLSCert), os.Getenv(EnvTLSKey); certFile != "" && keyFile != "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvTLS))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// ControllerTLSHostnames is the SAN set for the controller's leaf: every name
// and address a client can legitimately reach this listener on.
//
// Loopback is included because localhost is a first-class caller here — the
// localhostOrAuth routes exist for it — and a certificate that does not cover
// 127.0.0.1 turns every one of those into a handshake failure.
func ControllerTLSHostnames(extra []string) []string {
	names := []string{"localhost", "127.0.0.1", "::1"}

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		names = append(names, hostname, hostname+".local")
	}
	if ipv4, ipv6 := InternalInterfaceIPs(); ipv4 != "" || ipv6 != "" {
		if ipv4 != "" {
			names = append(names, ipv4)
		}
		if ipv6 != "" {
			names = append(names, ipv6)
		}
	}
	for raw := range strings.SplitSeq(os.Getenv(EnvTLSSANs), ",") {
		if san := strings.TrimSpace(raw); san != "" {
			names = append(names, san)
		}
	}
	names = append(names, extra...)

	// Deduplicate, preserving order so the SAN set is stable across boots and
	// IssueLeaf's "already covers exactly this set" check keeps no-opping.
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// ControllerTLSConfig builds the listener's TLS configuration.
//
// With TOWN_OS_TLS_CERT and TOWN_OS_TLS_KEY set it loads those. Otherwise it
// ensures the local CA exists and issues (or reuses) the controller leaf under
// the tls subvolume. Returns nil when TLS was not requested, which is the
// signal to serve plain HTTP.
func ControllerTLSConfig(btrfsBase string, extraSANs []string) (*cryptotls.Config, error) {
	if !ControllerTLSRequested() {
		return nil, nil //nolint:nilnil // nil config is the documented "serve plain HTTP" signal
	}

	certFile, keyFile := os.Getenv(EnvTLSCert), os.Getenv(EnvTLSKey)
	if certFile != "" && keyFile != "" {
		pair, err := cryptotls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load %s/%s: %w", EnvTLSCert, EnvTLSKey, err)
		}
		return &cryptotls.Config{
			Certificates: []cryptotls.Certificate{pair},
			MinVersion:   cryptotls.VersionTLS12,
		}, nil
	}

	if btrfsBase == "" {
		return nil, fmt.Errorf("%s is set but no btrfs base path is configured to hold the CA", EnvTLS)
	}

	tlsDir := filepath.Join(btrfsBase, TLSSubvolume)
	ca, err := townostls.EnsureCA(tlsDir)
	if err != nil {
		return nil, fmt.Errorf("ensure local CA: %w", err)
	}

	leafDir := filepath.Join(tlsDir, ControllerTLSDirName)
	hostnames := ControllerTLSHostnames(extraSANs)
	if err := ca.IssueLeaf(leafDir, hostnames); err != nil {
		return nil, fmt.Errorf("issue controller leaf: %w", err)
	}

	pair, err := cryptotls.LoadX509KeyPair(
		filepath.Join(leafDir, townostls.LeafCertFileName),
		filepath.Join(leafDir, townostls.LeafKeyFileName),
	)
	if err != nil {
		return nil, fmt.Errorf("load controller leaf: %w", err)
	}

	return &cryptotls.Config{
		Certificates: []cryptotls.Certificate{pair},
		MinVersion:   cryptotls.VersionTLS12,
	}, nil
}

// ControllerTLSScheme is the URL scheme the controller is reachable on, for
// anything that has to render its own address.
func ControllerTLSScheme() string {
	if ControllerTLSRequested() {
		return "https"
	}
	return "http"
}

// ListenAddrSANs returns the host part of a listen address as a SAN, if it has
// one. ":5309" contributes nothing; "10.0.0.5:5309" contributes that address,
// since it is precisely what a client will dial and therefore what its TLS
// stack will check the certificate against.
func ListenAddrSANs(listenAddr string) []string {
	host, _, err := net.SplitHostPort(listenAddr)
	if err != nil || host == "" {
		return nil
	}
	return []string{host}
}
