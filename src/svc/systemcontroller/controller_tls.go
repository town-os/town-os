package systemcontroller

import (
	"path/filepath"
	"slices"
	"strings"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
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
	"http": true,
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
//   - the systemcontroller's current internal (LAN) IP so a browser on
//     the home network hitting `https://192.168.1.88:<port>` directly
//     matches the cert, not just the mDNS/rolodex name
//
// The internal IP is optional (empty string skips it) because the SAN
// set feeds IssueLeaf's idempotency check — a boot that can't discover
// the LAN IP would otherwise churn the cert from "with IP" → "without
// IP" and back on every reconcile.
func collectTLSSans(packageDNS string, extraDomains []string, internalIP string) []string {
	sans := make([]string, 0, 4+len(extraDomains))
	if packageDNS != "" {
		sans = append(sans, packageDNS)
	}
	sans = append(sans, extraDomains...)
	sans = append(sans, "localhost", "127.0.0.1")
	if internalIP != "" {
		sans = append(sans, internalIP)
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

// applyTLSToPorts mutates state.Ports in place, turning on TLS for every
// port whose container-side InternalPort is an HTTP port (per
// isHTTPPort) and setting CertPath to the container-side leaf
// directory. Non-HTTP ports (SSH, raw TCP, etc.) are left as plaintext
// socat forwarders. The caller must have issued the leaf before
// calling this function.
func applyTLSToPorts(state *networkcontroller.PackageNetworkState, certPath string, compiled *packages.Package) {
	for i := range state.Ports {
		if !isHTTPPort(state.Ports[i].InternalPort, compiled) {
			continue
		}
		state.Ports[i].TLS = true
		state.Ports[i].CertPath = certPath
	}
}

// issueLeafForPackage issues (or refreshes) a leaf cert for the given
// package identity. Returns the container-internal path the NC will see
// the directory at, so the caller can stamp it into the state file. When
// ca is nil the function is a no-op and returns "". internalIP may be
// empty when the caller can't discover a LAN address (boot-time race);
// see collectTLSSans for why that's treated as "skip that SAN" rather
// than "fail".
func issueLeafForPackage(ca *townostls.CA, btrfsBase, repoName, pkgName, version string, compiled *packages.Package, packageDNS, internalIP string) (string, error) {
	if ca == nil || btrfsBase == "" {
		return "", nil
	}
	var domains []string
	if compiled != nil {
		domains = compiled.Network.Domains
	}
	sans := collectTLSSans(packageDNS, domains, internalIP)
	hostDir := hostTLSLeafDir(btrfsBase, repoName, pkgName, version)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		return "", err
	}
	return containerTLSLeafDir(repoName, pkgName, version), nil
}
