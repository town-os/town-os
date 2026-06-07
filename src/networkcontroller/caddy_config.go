// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package networkcontroller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// CaddySite is one https vhost rendered into the Caddyfile. The target is
// the podman DNS name the NC resolves upstream against, exactly like the old
// in-process TLS forwarder did via its byte tunnel.
//
// Each terminated TLS port yields a port-keyed (catch-all) site that serves
// the local-CA leaf — this covers SNI access by the internal DNS name (DANE-
// validated via the published TLSA record) as well as direct-by-IP HTTPS.
// When the port also carries public FQDNs, one extra host-keyed site per
// FQDN is emitted with ACME=true so Caddy obtains a publicly-trusted cert;
// host-keyed sites win over the catch-all by SNI on the same port.
type CaddySite struct {
	ExternalPort uint16
	Target       string
	InternalPort uint16
	CertPath     string // directory containing cert.pem + key.pem (file-cert sites only)
	CertHash     string // sha256 of cert.pem content; empty means "do not embed"
	Host         string // non-empty => host-keyed site; empty => port-keyed catch-all
	ACME         bool   // true => manage the cert via an ACME issuer (ignores CertPath)
}

// CollectCaddySites walks a set of PackageNetworkState values and returns
// one CaddySite per port where TLS=true. Non-TLS ports are left for socat.
// The resulting slice is sorted by external port so the rendered Caddyfile
// is deterministic across reconciles — stable output is what lets the NC
// decide "no change, no reload needed" by comparing the file content.
// Exported so integration tests in other packages can drive the renderer
// end-to-end without spinning up an NC container; production callers
// inside this package go through it via the shared receiver.
func CollectCaddySites(states []*PackageNetworkState) []CaddySite {
	var sites []CaddySite
	for _, st := range states {
		if st == nil || st.ContainerName == "" {
			continue
		}
		for _, p := range st.Ports {
			// Passthrough ports are raw-forwarded by socat so the TLS
			// stream reaches the backend untouched — they are never
			// terminated by Caddy. Non-forwarded / non-TLS ports are
			// likewise not Caddy's concern.
			if !p.Forward || !p.TLS || p.Passthrough {
				continue
			}
			// Port-keyed catch-all site serving the local-CA leaf (DANE).
			sites = append(sites, CaddySite{
				ExternalPort: p.ExternalPort,
				Target:       st.ContainerName,
				InternalPort: p.InternalPort,
				CertPath:     p.CertPath,
				CertHash:     hashCertFile(p.CertPath),
			})
			// Extra host-keyed ACME sites for any public FQDNs on this port.
			if p.PublicDomain {
				for _, name := range p.SNINames {
					if name == "" {
						continue
					}
					sites = append(sites, CaddySite{
						ExternalPort: p.ExternalPort,
						Target:       st.ContainerName,
						InternalPort: p.InternalPort,
						Host:         name,
						ACME:         true,
					})
				}
			}
		}
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].ExternalPort != sites[j].ExternalPort {
			return sites[i].ExternalPort < sites[j].ExternalPort
		}
		// Port-keyed catch-all (empty Host) sorts before host-keyed sites;
		// host-keyed sites sort by name for deterministic output.
		return sites[i].Host < sites[j].Host
	})
	return sites
}

// hashCertFile returns the sha256 hex of certDir/cert.pem. Empty string if
// the directory or file is missing — the caller stamps whatever comes back
// into the Caddyfile, and an unreadable cert simply produces an empty
// cert-hash comment, which is still stable across reconciles.
func hashCertFile(certDir string) string {
	if certDir == "" {
		return ""
	}
	certPath := filepath.Join(certDir, "cert.pem")
	data, err := os.ReadFile(certPath) //nolint:gosec // G304 -- path derived from trusted TLS base
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// RenderCaddyfile produces a complete Caddyfile for the given site list.
// The top stanza disables Caddy's automatic HTTPS (we manage certs
// ourselves via the Town OS CA) and its admin API (we reload by invoking
// the caddy CLI, not by poking the admin socket). Each site block hard-
// codes the leaf cert/key paths and reverse-proxies plaintext to the
// container's HTTP port.
//
// A `# cert-hash:` comment is embedded inside every site block. When the
// cert rotates on disk, the hash changes, the rendered bytes differ, and
// the NC's Caddyfile-change detector fires a caddy reload. Without this
// line Caddy would keep serving the cached (old) cert until the process
// restarted, because Caddy does not re-read file-based certs per handshake.
func RenderCaddyfile(sites []CaddySite) []byte {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	buf.WriteString("\tauto_https off\n")
	buf.WriteString("\tadmin off\n")
	// H3 is disabled: our podman port mapping only forwards TCP
	// (`-p NNN:NNN` defaults to /tcp in podman), so UDP 38895 etc. on
	// the host are unreachable. Without this, Caddy advertises
	// `Alt-Svc: h3=":NNN"` on every response, the browser caches it,
	// switches to H3 on the next request, and hangs on the missing UDP
	// listener. For LAN-only traffic H3's connection-migration upside
	// is not worth the cross-netns UDP plumbing — disable the protocol
	// entirely rather than half-advertise it.
	buf.WriteString("\tservers {\n")
	buf.WriteString("\t\tprotocols h1 h2\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n")
	for _, s := range sites {
		if !s.ACME && s.CertPath == "" {
			// A file-cert TLS port without a cert path is a bug upstream —
			// skip it rather than write a site block that will make caddy
			// refuse to reload the whole config. ACME sites need no cert
			// file (Caddy obtains one), so they are exempt.
			continue
		}
		// net.JoinHostPort yields ":443" for the empty-host catch-all and
		// "app.example.com:443" for host-keyed sites.
		addr := "https://" + net.JoinHostPort(s.Host, strconv.Itoa(int(s.ExternalPort)))
		fmt.Fprintf(&buf, "\n%s {\n", addr)
		if s.ACME {
			// Caddy manages a publicly-trusted cert via ACME for this host,
			// even with global auto_https off, because an explicit issuer is
			// declared. No file cert and no cert-hash: rotation is Caddy's job.
			buf.WriteString("\ttls {\n\t\tissuer acme\n\t}\n")
		} else {
			certPath := filepath.Join(s.CertPath, "cert.pem")
			keyPath := filepath.Join(s.CertPath, "key.pem")
			fmt.Fprintf(&buf, "\t# cert-hash: %s\n", s.CertHash)
			fmt.Fprintf(&buf, "\ttls %s %s\n", certPath, keyPath)
		}
		fmt.Fprintf(&buf, "\treverse_proxy %s:%d\n", s.Target, s.InternalPort)
		buf.WriteString("}\n")
	}
	return buf.Bytes()
}
