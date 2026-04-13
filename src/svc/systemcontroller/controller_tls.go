package systemcontroller

import (
	"path/filepath"
	"slices"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	townostls "gitea.com/town-os/town-os/src/tls"
)

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
// always includes the computed PACKAGE_DNS, any domains the package
// declared via `network.domains`, and `localhost`/`127.0.0.1` so the NC
// can be hit from the host even when DNS is not configured yet.
func collectTLSSans(packageDNS string, extraDomains []string) []string {
	sans := make([]string, 0, 3+len(extraDomains))
	if packageDNS != "" {
		sans = append(sans, packageDNS)
	}
	sans = append(sans, extraDomains...)
	sans = append(sans, "localhost", "127.0.0.1")
	return sans
}

// httpContainerPorts is the allowlist of container-side ports the NC will
// TLS-wrap when a package supplies "http". It must stay permissive enough
// to cover every default HTTP-supplying package (nginx → 80, gitea →
// 3000, matrix → 8008, mattermost → 8065, plex → 32400, etc.) while
// excluding non-HTTP ports that a mixed package might also expose — most
// importantly SSH (22), which would break if wrapped in TLS. Packages
// whose HTTP port is not in this set will stay plaintext until the set
// is extended or a per-port opt-in annotation is added.
var httpContainerPorts = map[uint16]bool{
	80:    true,
	3000:  true,
	5000:  true,
	8000:  true,
	8008:  true,
	8065:  true,
	8080:  true,
	8888:  true,
	32400: true,
}

// hasHTTPPort reports whether any port in the state file has an HTTP
// container-side port. Used before issuing a leaf cert so we don't mint
// certs for packages whose only ports are non-HTTP (e.g. a package that
// supplies http but exposes SSH on a funky container port).
func hasHTTPPort(state *networkcontroller.PackageNetworkState) bool {
	for _, p := range state.Ports {
		if httpContainerPorts[p.InternalPort] {
			return true
		}
	}
	return false
}

// applyTLSToPorts mutates state.Ports in place, turning on TLS for every
// port whose container-side InternalPort is an HTTP port and setting
// CertPath to the container-side leaf directory. Non-HTTP ports (SSH,
// raw TCP, etc.) are left as plaintext socat forwarders. The caller must
// have issued the leaf before calling this function.
func applyTLSToPorts(state *networkcontroller.PackageNetworkState, certPath string) {
	for i := range state.Ports {
		if !httpContainerPorts[state.Ports[i].InternalPort] {
			continue
		}
		state.Ports[i].TLS = true
		state.Ports[i].CertPath = certPath
	}
}

// issueLeafForPackage issues (or refreshes) a leaf cert for the given
// package identity. Returns the container-internal path the NC will see
// the directory at, so the caller can stamp it into the state file. When
// ca is nil the function is a no-op and returns "".
func issueLeafForPackage(ca *townostls.CA, btrfsBase, repoName, pkgName, version string, compiled *packages.Package, packageDNS string) (string, error) {
	if ca == nil || btrfsBase == "" {
		return "", nil
	}
	var domains []string
	if compiled != nil {
		domains = compiled.Network.Domains
	}
	sans := collectTLSSans(packageDNS, domains)
	hostDir := hostTLSLeafDir(btrfsBase, repoName, pkgName, version)
	if err := ca.IssueLeaf(hostDir, sans); err != nil {
		return "", err
	}
	return containerTLSLeafDir(repoName, pkgName, version), nil
}
