// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
)

// ingressCaddyfileExists reports whether the shared ingress Caddyfile has been
// written yet — i.e. the ingress is already in use (pages or an HTTP package).
// Used to avoid spinning the ingress up when uninstalling a package that never
// had a vhost.
func ingressCaddyfileExists(btrfsBase string) bool {
	if btrfsBase == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(btrfsBase, PagesCaddyDir, "Caddyfile"))
	return err == nil
}

// ingressHostPorts returns the host-port keys (external/internal map keys) the
// shared :443 ingress fronts for this package: ports explicitly named `http`
// that the package supplies as http, excluding passthrough and direct ports.
// Allowlisted-numeric HTTP ports are NOT included — they keep terminating on the
// per-package NC. This matches exactly the ports applyTLSToPorts flags as
// Ingress, so GeneratePackageUnits can drop them from the per-package NC's host
// publishing (-p / sockets) — the ingress owns them on :443.
func ingressHostPorts(compiled *packages.Package, supplies []string) map[uint16]bool {
	if compiled == nil || !suppliesHTTP(supplies) {
		return nil
	}
	out := map[uint16]bool{}
	mark := func(pm packages.PortMap) {
		for host, container := range pm {
			if compiled.Network.DirectPorts[host] {
				continue
			}
			if compiled.Network.TLSModes[host] == packages.TLSModePassthrough {
				continue
			}
			if isHTTPNamedPort(container, compiled) {
				out[host] = true
			}
		}
	}
	mark(compiled.Network.External)
	mark(compiled.Network.Internal)
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectPackageIngressSites builds the reverse_proxy vhosts the shared :443
// ingress serves for installed HTTP packages. For each package it reads the
// network state file the systemcontroller already writes and emits one site per
// TLS-terminated, non-passthrough port, keyed by the package FQDN
// (<name>.<repo>.<tld>) and proxying to the service container on the ingress
// network. Passthrough ports own their TLS end to end and are left on their own
// per-package forwarder, so they are skipped here. The installer need only list
// installed identifiers (FreshnessLister).
func collectPackageIngressSites(installer FreshnessLister, stateDir, tld string) []PackageIngressSite {
	if installer == nil || stateDir == "" {
		return nil
	}
	installed, err := installer.ListInstalled()
	if err != nil {
		return nil
	}
	var sites []PackageIngressSite
	for _, p := range installed {
		pi, perr := packages.ParsePackageIdentity(p)
		if perr != nil {
			continue
		}
		// Dependencies are internal to their parent and never fronted directly.
		if packages.IsDependency(pi.Name) {
			continue
		}
		statePath := filepath.Join(stateDir, fmt.Sprintf("%s-%s-%s.json", pi.Repo, pi.Name, pi.Version))
		data, rerr := os.ReadFile(statePath) //nolint:gosec // G304 -- path derived from the trusted network-state dir
		if rerr != nil {
			continue
		}
		var st networkcontroller.PackageNetworkState
		if uerr := json.Unmarshal(data, &st); uerr != nil {
			continue
		}
		fqdn := pi.Name + "." + pi.Repo + "." + tld
		for _, port := range st.Ports {
			if !port.Ingress || st.ContainerName == "" {
				continue
			}
			sites = append(sites, PackageIngressSite{
				Hostname: fqdn,
				ACME:     port.PublicDomain,
				CertDir:  port.CertPath,
				Backend:  fmt.Sprintf("%s:%d", st.ContainerName, port.InternalPort),
			})
			// One HTTPS vhost per package FQDN; the first terminated port is the
			// package's HTTP endpoint. Additional terminated ports would collide
			// on the same hostname, so stop after the first.
			break
		}
	}
	return sites
}
