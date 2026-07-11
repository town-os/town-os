package systemcontroller

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// httpPortNames is the set of semantic port-name labels (lowercased,
// from the yaml `network.{external,internal}` map key) that mark a port
// as an HTTP service the NC should TLS-terminate. Package authors who
// name a port "http" get TLS regardless of the numeric container port
// they picked — this matters for any package whose port is chosen at
// install time (gitea's @httpport@ auto-generates into the 10000–60000
// range, which no fixed allowlist can cover).
var httpPortNames = map[string]bool{
	"http":  true,
	"https": true,
}

// httpsPortName is the semantic name marking an ingress port whose backend
// itself speaks HTTPS (e.g. an app that only serves TLS). The ingress proxies to
// such a backend over https with internal-hop verification disabled.
const httpsPortName = "https"

// isHTTPSNamedPort reports whether a port is explicitly named `https` — i.e. its
// backend serves TLS and the ingress must reverse-proxy to it over https.
func isHTTPSNamedPort(containerPort uint16, compiled *packages.Package) bool {
	if compiled == nil {
		return false
	}
	if name, ok := compiled.Network.ExternalNames[containerPort]; ok && strings.ToLower(name) == httpsPortName {
		return true
	}
	if name, ok := compiled.Network.InternalNames[containerPort]; ok && strings.ToLower(name) == httpsPortName {
		return true
	}
	return false
}

// TLSSubvolume is the btrfs subvolume under the root that holds the local
// CA and every per-package leaf certificate. It is pre-created by reconcile
// alongside the other reserved subvolumes (installed/, uninstalled/, etc.)
// and is exclusively managed by the systemcontroller — nothing else writes
// to it.
const TLSSubvolume = "tls"

// TLSLeavesDir is the subdirectory under the TLS subvolume where leaf certs
// live. A cert for package `default/nginx/1.0` ends up at
// `<btrfs>/tls/leaves/default/nginx/1.0/{cert.pem,key.pem}`.
const TLSLeavesDir = "leaves"

// TLSContainerMount is the path the network controller container sees the
// TLS directory at. It is constant across packages and versions so a single
// bind mount from `<btrfs>/tls` into the NC container satisfies every leaf
// the NC's tls_proxy.go needs to read.
const TLSContainerMount = "/etc/town-os/tls"

// hostTLSBase returns the host-side path to the TLS subvolume under the
// given btrfs root. Empty when btrfsBase is empty (test mode / disabled).
func hostTLSBase(btrfsBase string) string {
	if btrfsBase == "" {
		return ""
	}
	return filepath.Join(btrfsBase, TLSSubvolume)
}

// hostTLSLeafDir returns the host-side directory where a leaf cert for the
// given package identity is issued. Each install/reconcile call writes
// (idempotently) into this directory.
func hostTLSLeafDir(btrfsBase, repoName, pkgName, version string) string {
	return filepath.Join(btrfsBase, TLSSubvolume, TLSLeavesDir, repoName, pkgName, version)
}

// containerTLSLeafDir returns the path the NC container sees a package's
// leaf cert at. This is what gets stored in the state file's CertPath so
// the NC's tls_proxy.go can read cert.pem/key.pem from inside the mount.
func containerTLSLeafDir(repoName, pkgName, version string) string {
	return TLSContainerMount + "/" + TLSLeavesDir + "/" + repoName + "/" + pkgName + "/" + version
}

// suppliesHTTP reports whether a package declares itself as an HTTP endpoint
// via `supplies: ["http"]`. The match is case-sensitive: the Town OS spec
// canonicalizes supplies entries to lowercase.
func suppliesHTTP(supplies []string) bool {
	return slices.Contains(supplies, "http")
}

// collectTLSSans builds the SAN list for a package's leaf cert. The list
// always includes:
//   - the computed PACKAGE_DNS (e.g. gitea.default.home)
//   - any extra domains the package declared via `network.domains`
//   - `localhost` + `127.0.0.1` for host-loopback probes
//   - the systemcontroller's current internal (LAN) IP, and its global IPv6
//     when present, so a browser on the home network hitting
//     `https://192.168.1.88:<port>` or `https://[2001:db8::1]:<port>`
//     directly matches the cert, not just the mDNS/rolodex name
//   - the box's WireGuard overlay IP on the package's install network, when it
//     has one, so a peer on that network hitting `https://10.65.0.1` by raw
//     address validates too. The same leaf therefore serves the LAN and the
//     overlay: the shared ingress listens on all interfaces and SNI-selects
//     this vhost from either side.
//
// All three IPs are optional (empty string skips them) because the SAN set feeds
// IssueLeaf's idempotency check — a boot that can't discover an address would
// otherwise churn the cert from "with IP" → "without IP" and back on every
// reconcile. A v4-only host (internalIPv6 == "") and a default-network package
// (overlayIP == "") get the same SAN set as before, so existing leaves are not
// re-issued.
func collectTLSSans(packageDNS string, extraDomains []string, internalIP, internalIPv6, overlayIP string) []string {
	sans := make([]string, 0, 6+len(extraDomains))
	if packageDNS != "" {
		sans = append(sans, packageDNS)
	}
	sans = append(sans, extraDomains...)
	sans = append(sans, "localhost", "127.0.0.1")
	if internalIP != "" {
		sans = append(sans, internalIP)
	}
	if internalIPv6 != "" {
		sans = append(sans, internalIPv6)
	}
	if overlayIP != "" {
		sans = append(sans, overlayIP)
	}
	return sans
}

// httpContainerPorts is the allowlist of container-side ports the NC will
// TLS-wrap when a package supplies "http". It must stay permissive enough
// to cover every default HTTP-supplying package (nginx → 80, gitea →
// 3000, matrix → 8008, mattermost → 8065, plex → 32400, immich → 2283,
// etc.) while excluding non-HTTP ports that a mixed package might also
// expose — most importantly SSH (22), which would break if wrapped in
// TLS. Packages whose HTTP port is not in this set will stay plaintext
// until the set is extended or a per-port opt-in annotation is added.
var httpContainerPorts = map[uint16]bool{
	80:    true,
	2283:  true, // immich
	3000:  true, // gitea (default)
	5000:  true,
	8000:  true,
	8008:  true, // matrix / synapse
	8065:  true, // mattermost
	8080:  true,
	8888:  true,
	32400: true, // plex
}

// isHTTPPort reports whether a single container-side port should be
// TLS-wrapped. It prefers the semantic yaml name (`http` in network.
// external/internal) over the numeric allowlist — so a package that
// declares `network.internal: { http: "@httpport@" }` gets TLS whether
// @httpport@ lands on 3000 (the default), 8443 (user-picked), or an
// auto-generated 38895. Packages that didn't migrate to semantic names
// still get TLS via the numeric fallback below for canonical container
// ports (80, 2283, 3000, 8008, 8065, 32400, …).
func isHTTPPort(containerPort uint16, compiled *packages.Package) bool {
	if compiled != nil {
		if name, ok := compiled.Network.ExternalNames[containerPort]; ok && httpPortNames[strings.ToLower(name)] {
			return true
		}
		if name, ok := compiled.Network.InternalNames[containerPort]; ok && httpPortNames[strings.ToLower(name)] {
			return true
		}
	}
	return httpContainerPorts[containerPort]
}

// hasHTTPPort reports whether any port in the state file is an HTTP
// port per isHTTPPort. Used before issuing a leaf cert so we don't
// mint certs for packages whose only ports are non-HTTP (e.g. a
// package that supplies http but exposes SSH on a funky container
// port).
func hasHTTPPort(state *networkcontroller.PackageNetworkState, compiled *packages.Package) bool {
	for _, p := range state.Ports {
		if isHTTPPort(p.InternalPort, compiled) {
			return true
		}
	}
	return false
}

// isPublicFQDN reports whether name is a real public fully-qualified domain
// (eligible for ACME) rather than an internal Town OS name. A name is treated
// as internal when it is a bare subdomain label (no dot), ends in the internal
// TLD, is localhost, or is an IP literal. Public FQDNs are resolved by the
// user's real DNS (not rolodex), so they get an ACME-managed cert and no DANE
// TLSA record.
func isPublicFQDN(name, tld string) bool {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" || strings.EqualFold(name, "localhost") {
		return false
	}
	if net.ParseIP(name) != nil {
		return false
	}
	if !strings.Contains(name, ".") {
		return false // bare subdomain label → internal
	}
	if tld != "" && (strings.HasSuffix(name, "."+tld) || name == tld) {
		return false
	}
	return true
}

// internalDomains returns the subset of domains that are internal subdomain
// labels (not public FQDNs). Only these belong in rolodex — public FQDNs are
// resolved by the user's real DNS and would otherwise be registered as bogus
// `<public.fqdn>.<name>.<repo>.<tld>` internal records.
func internalDomains(domains []string, tld string) []string {
	if len(domains) == 0 {
		return domains
	}
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		if isPublicFQDN(d, tld) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// packageFQDN is the single source of truth for a package's DNS name: the name
// its A record, its leaf certificate SAN, its DANE TLSA owner, and its shared
// :443 ingress vhost must all agree on. tld is the package's *install network's*
// TLD (networkTLDValue), not the global dns_tld — a package on the "fart"
// network is gitea.default.fart, never gitea.default.home. Deriving these four
// names from one function is what keeps the ingress from serving a vhost the
// cert is not valid for.
func packageFQDN(repoName, pkgName, tld string) string {
	return pkgName + "." + repoName + "." + tld
}

// publicDomains returns the package's network.domains entries that are public
// FQDNs (per isPublicFQDN). These drive ACME host-keyed Caddy sites.
func publicDomains(compiled *packages.Package, tld string) []string {
	if compiled == nil {
		return nil
	}
	var out []string
	for _, d := range compiled.Network.Domains {
		if isPublicFQDN(d, tld) {
			out = append(out, d)
		}
	}
	return out
}

// applyTLSToPorts mutates state.Ports in place to set up TLS handling:
//
//   - A port marked passthrough (network.tls_mode: passthrough) is left as a
//     raw socat forward (TLS=false) so the TLS stream — SNI included — reaches
//     the backend untouched; Passthrough is set for clarity and so the NC's
//     Caddy collector skips it.
//   - An HTTP port (per isHTTPPort) that is not passthrough is TLS-terminated:
//     TLS=true and CertPath points at the container-side leaf directory. When
//     the package declares public-FQDN domains, the port is additionally
//     marked PublicDomain with those names so the NC renders host-keyed ACME
//     sites alongside the local-CA (DANE) catch-all.
//   - Non-HTTP ports (SSH, raw TCP) are left as plaintext socat forwarders.
//
// The caller must have issued the leaf before calling this function.
func applyTLSToPorts(state *networkcontroller.PackageNetworkState, certPath string, compiled *packages.Package, tld string) {
	publicFQDNs := publicDomains(compiled, tld)
	for i := range state.Ports {
		host := state.Ports[i].ExternalPort
		if compiled != nil && compiled.Network.TLSModes[host] == packages.TLSModePassthrough {
			// Backend owns the cert end to end; NC raw-forwards via socat.
			state.Ports[i].Passthrough = true
			continue
		}
		if !isHTTPPort(state.Ports[i].InternalPort, compiled) {
			continue
		}
		state.Ports[i].TLS = true
		state.Ports[i].CertPath = certPath
		// Every HTTP port the NC would TLS-terminate is instead fronted by the
		// shared :443 ingress — the network controller is the authority for the
		// externally-reachable port, and a proxied HTTP service is reachable on
		// 443, not its internal port. Flag Ingress (DANE pinned on _443; see
		// buildTLSAEntries), stop the per-package NC forwarding it, and mark
		// BackendTLS for ports named `https` (HTTPS backends). Raw socat-forwarded
		// ports keep their own external port; they are not HTTP-terminated here.
		state.Ports[i].Ingress = true
		state.Ports[i].Forward = false
		state.Ports[i].BackendTLS = isHTTPSNamedPort(state.Ports[i].InternalPort, compiled)
		if len(publicFQDNs) > 0 {
			state.Ports[i].PublicDomain = true
			state.Ports[i].SNINames = publicFQDNs
		}
	}
}

// tlsaValue computes the DANE TLSA RDATA pinning the leaf at certPEMPath.
// The form is "3 1 1 <hex>": usage 3 (DANE-EE, the end-entity cert directly),
// selector 1 (SubjectPublicKeyInfo, so a re-issue keeping the same key keeps
// the record valid), matching 1 (SHA-256 of the SPKI). Returns "" with an
// error if the cert cannot be read or parsed; callers treat a missing value
// as "skip publishing TLSA for this port".
func tlsaValue(certPEMPath string) (string, error) {
	data, err := os.ReadFile(certPEMPath) //nolint:gosec // G304 -- path derived from the trusted TLS subvolume
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block in %s", certPEMPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse leaf cert: %w", err)
	}
	spki, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshal public key: %w", err)
	}
	sum := sha256.Sum256(spki)
	return "3 1 1 " + hex.EncodeToString(sum[:]), nil
}

// buildTLSAEntries derives the DANE TLSA records for an installed package by
// reading its on-disk network state file (the single source of truth for which
// ports the NC terminates) and pinning the issued leaf. One entry is produced
// per (internal FQDN × terminated, non-passthrough port). Public-FQDN domains
// are excluded — those are served via ACME, not DANE. Returns nil when the
// package has no terminated ports, no state file, or no leaf yet.
func buildTLSAEntries(stateDir, btrfsBase, repo, name, version, tld string, domains []string) ([]rolodex.TLSAEntry, error) {
	if stateDir == "" || btrfsBase == "" {
		return nil, nil
	}
	statePath := filepath.Join(stateDir, fmt.Sprintf("%s-%s-%s.json", repo, name, version))
	data, err := os.ReadFile(statePath) //nolint:gosec // G304 -- path derived from the trusted network-state dir
	if err != nil {
		return nil, err
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("unmarshal state %s: %w", statePath, err)
	}

	var ports []uint16
	for _, p := range st.Ports {
		if p.TLS && !p.Passthrough {
			// Ingress ports are served on :443 by the shared ingress, so their
			// DANE TLSA is pinned on _443._tcp.<fqdn> regardless of the declared
			// host port.
			if p.Ingress {
				ports = append(ports, PagesHTTPSPort)
			} else {
				ports = append(ports, p.ExternalPort)
			}
		}
	}
	if len(ports) == 0 {
		return nil, nil
	}

	value, err := tlsaValue(filepath.Join(hostTLSLeafDir(btrfsBase, repo, name, version), "cert.pem"))
	if err != nil {
		return nil, err
	}

	base := name + "." + repo + "." + tld
	fqdns := []string{base}
	for _, d := range domains {
		if isPublicFQDN(d, tld) {
			continue
		}
		fqdns = append(fqdns, d+"."+base)
	}

	entries := make([]rolodex.TLSAEntry, 0, len(fqdns)*len(ports))
	for _, fqdn := range fqdns {
		for _, port := range ports {
			entries = append(entries, rolodex.TLSAEntry{Name: fqdn, Port: port, Value: value})
		}
	}
	return entries, nil
}

// issueLeafForPackage issues (or refreshes) a leaf cert for the given
// package identity. Returns the container-internal path the NC will see
// the directory at, so the caller can stamp it into the state file. When
// ca is nil the function is a no-op and returns "". internalIP may be
// empty when the caller can't discover a LAN address (boot-time race);
// see collectTLSSans for why that's treated as "skip that SAN" rather
// than "fail". overlayIP is the box's WireGuard address on the package's
// install network (empty for the default network), so a peer on that
// network can also reach the package by raw overlay address.
func issueLeafForPackage(ca *townostls.CA, btrfsBase, repoName, pkgName, version string, compiled *packages.Package, packageDNS, internalIP, overlayIP string) (string, error) {
	if ca == nil || btrfsBase == "" {
		return "", nil
	}
	var domains []string
	if compiled != nil {
		domains = compiled.Network.Domains
	}
	// Pair the leaf's IPv6 SAN to the same interface that yields internalIP, so
	// a direct https://[v6-literal] dial matches. Local lookup avoids threading
	// the v6 through every install/reconcile signature; gated empty on v4-only
	// hosts so the SAN set (and thus the issued cert) is unchanged there.
	_, internalIPv6 := InternalInterfaceIPs()
	sans := collectTLSSans(packageDNS, domains, internalIP, internalIPv6, overlayIP)
	hostDir := hostTLSLeafDir(btrfsBase, repoName, pkgName, version)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		return "", err
	}
	return containerTLSLeafDir(repoName, pkgName, version), nil
}
