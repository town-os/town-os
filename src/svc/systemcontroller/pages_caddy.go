// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gitea.com/town-os/town-os/src/systemd"
)

const (
	PagesUnitName      = "town-os-pages.service"
	PagesContainerName = "town-os-pages"
	PagesWebrootDir    = "pages-webroot"
	PagesCaddyDir      = "pages-caddy"
	DefaultCaddyImage  = "docker.io/library/caddy:latest"
)

// PagesUnitConfig holds the parameters needed to generate the Caddy systemd unit.
type PagesUnitConfig struct {
	BtrfsBasePath string
	CaddyImage    string
	ContainerName string // defaults to PagesContainerName when empty
}

// PageCaddySite is one rendered page vhost: a hostname served over HTTPS from
// the page's static webroot. Internal hostnames pin the local-CA leaf at
// CertDir; public FQDNs (ACME=true) let Caddy obtain a publicly-trusted cert.
type PageCaddySite struct {
	Name     string // page name; content is served from /srv/<Name>
	Hostname string // SNI host the site is keyed by
	ACME     bool   // true => tls { issuer acme }; false => file-pinned local leaf
	CertDir  string // container path to the leaf dir (cert.pem/key.pem); ACME ignores it
}

// PackageIngressSite is one package backend fronted by the shared ingress: an
// HTTPS vhost keyed by the package's FQDN that reverse-proxies to the service
// container's HTTP port. The ingress terminates TLS with the package's leaf (or
// ACME for a public FQDN) so the package no longer needs to publish its own host
// port — every HTTP package answers on the one :443 listener by SNI.
type PackageIngressSite struct {
	Hostname string // SNI host the site is keyed by (e.g. gitea.default.home)
	ACME     bool   // true => tls { issuer acme }; false => file-pinned local leaf
	CertDir  string // container path to the leaf dir (cert.pem/key.pem); ACME ignores it
	Backend  string // reverse_proxy target, "container:port" on the ingress network
}

// ingressVHost writes one `https://<hostname>` site block: the TLS directive
// (a file-pinned local-CA leaf, or an explicit ACME issuer for a public FQDN —
// Caddy honors the issuer even with auto_https off) followed by the supplied
// body lines (a file_server root for pages, a reverse_proxy for packages).
func ingressVHost(b *strings.Builder, hostname string, acme bool, certDir, body string) {
	fmt.Fprintf(b, "\nhttps://%s {\n", hostname)
	if acme {
		b.WriteString("\ttls {\n\t\tissuer acme\n\t}\n")
	} else {
		fmt.Fprintf(b, "\ttls %s/cert.pem %s/key.pem\n", certDir, certDir)
	}
	b.WriteString(body)
	b.WriteString("}\n")
}

// GeneratePagesCaddyfile renders the ingress config for pages only. Retained for
// callers/tests that don't front packages; see GenerateIngressCaddyfile.
func GeneratePagesCaddyfile(sites []PageCaddySite) string {
	return GenerateIngressCaddyfile(sites, nil)
}

// GenerateIngressCaddyfile renders the shared :443 ingress: one HTTPS vhost per
// page (static file_server) and per HTTP package (reverse_proxy to its backend),
// keyed by hostname (SNI). auto_https is disabled because certs are managed
// explicitly. A site with no issued leaf yet (non-ACME, empty CertDir) is skipped
// so a half-provisioned entry never makes caddy reject the whole config.
func GenerateIngressCaddyfile(pages []PageCaddySite, pkgs []PackageIngressSite) string {
	var b strings.Builder
	// auto_https off: certs are managed explicitly. protocols h1 h2: the ingress
	// only publishes TCP (-p 443:443), so H3/QUIC over UDP is unreachable —
	// without this, Caddy advertises Alt-Svc h3 and clients hang on the missing
	// UDP listener (same rationale as the network controller's Caddyfile).
	b.WriteString("{\n\tauto_https off\n\tadmin off\n\tservers {\n\t\tprotocols h1 h2\n\t}\n}\n")
	for _, s := range pages {
		if s.Hostname == "" || s.Name == "" {
			continue
		}
		if !s.ACME && s.CertDir == "" {
			continue
		}
		ingressVHost(&b, s.Hostname, s.ACME, s.CertDir,
			fmt.Sprintf("\troot * /srv/%s\n\tfile_server\n", s.Name))
	}
	for _, s := range pkgs {
		if s.Hostname == "" || s.Backend == "" {
			continue
		}
		if !s.ACME && s.CertDir == "" {
			continue
		}
		ingressVHost(&b, s.Hostname, s.ACME, s.CertDir,
			fmt.Sprintf("\treverse_proxy %s\n", s.Backend))
	}
	return b.String()
}

// GeneratePagesUnit generates a systemd service unit for the Caddy container.
func GeneratePagesUnit(cfg PagesUnitConfig) systemd.UnitFile {
	containerName := cfg.ContainerName
	if containerName == "" {
		containerName = PagesContainerName
	}

	var b strings.Builder

	// [Unit]
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Town OS Pages Web Server\n")
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", containerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", containerName)
	// The ingress joins the shared network (to reach package backends by name)
	// and publishes :443 on the host. Creating the network here is a boot-order
	// safety net; package units create it too — whoever wins, the other no-ops.
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman network create %s\n", systemd.IngressNetworkName)
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --replace --name %s --net %s -p 443:443", containerName, systemd.IngressNetworkName)
	fmt.Fprintf(&b, " \\\n  -v %s/%s:/data/pages:ro,z", cfg.BtrfsBasePath, PagesVolumePrefix)
	fmt.Fprintf(&b, " \\\n  -v %s/%s:/srv:ro,z", cfg.BtrfsBasePath, PagesWebrootDir)
	fmt.Fprintf(&b, " \\\n  -v %s/%s/Caddyfile:/etc/caddy/Caddyfile:ro,z", cfg.BtrfsBasePath, PagesCaddyDir)
	// Per-page leaf certs live under the shared TLS subvolume (the same tree
	// the network controller mounts); read-only because only the
	// systemcontroller ever writes leaves there.
	fmt.Fprintf(&b, " \\\n  -v %s/%s:%s:ro,z", cfg.BtrfsBasePath, TLSSubvolume, TLSContainerMount)
	fmt.Fprintf(&b, " \\\n  %s\n", cfg.CaddyImage)
	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", containerName)
	b.WriteString("Restart=on-failure\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return systemd.UnitFile{
		Name:    PagesUnitName,
		Content: b.String(),
	}
}

// WritePagesCaddyfile renders a pages-only ingress Caddyfile. Retained for
// callers/tests that don't front packages; see WriteIngressCaddyfile.
func WritePagesCaddyfile(btrfsBasePath string, sites []PageCaddySite) (string, bool, error) {
	return WriteIngressCaddyfile(btrfsBasePath, sites, nil)
}

// WriteIngressCaddyfile renders the shared ingress Caddyfile for the given page
// and package sites and writes it to {btrfsBasePath}/pages-caddy/Caddyfile. It
// returns the path written and whether the on-disk content changed, so callers
// can restart Caddy (which does not hot-reload a file-mounted config) only when
// the config actually changed.
func WriteIngressCaddyfile(btrfsBasePath string, pages []PageCaddySite, pkgs []PackageIngressSite) (string, bool, error) {
	dir := filepath.Join(btrfsBasePath, PagesCaddyDir)
	if err := os.MkdirAll(dir, 0755); err != nil { //nolint:gosec // G301 -- caddy config directory
		return "", false, fmt.Errorf("create caddy dir: %w", err)
	}

	path := filepath.Join(dir, "Caddyfile")
	content := GenerateIngressCaddyfile(pages, pkgs)
	prev, readErr := os.ReadFile(path) //nolint:gosec // G304 -- path under the trusted btrfs base
	if readErr != nil && !os.IsNotExist(readErr) {
		return "", false, fmt.Errorf("read existing Caddyfile: %w", readErr)
	}
	changed := string(prev) != content
	if err := os.WriteFile(path, []byte(content), 0644); err != nil { //nolint:gosec // G306 -- caddy config readable by container
		return "", false, fmt.Errorf("write Caddyfile: %w", err)
	}

	return path, changed, nil
}

// EnsurePagesWebroot creates the {btrfsBasePath}/pages-webroot/ directory.
func EnsurePagesWebroot(btrfsBasePath string) error {
	dir := filepath.Join(btrfsBasePath, PagesWebrootDir)
	return os.MkdirAll(dir, 0755) //nolint:gosec // G301 -- webroot directory
}

// EnsurePageSymlink creates a symlink at {btrfsBasePath}/pages-webroot/{name}
// pointing to /data/pages/{name} (the container-absolute path). Idempotent:
// if the symlink already exists with the correct target, it is a no-op.
func EnsurePageSymlink(btrfsBasePath, pageName string) error {
	linkPath := filepath.Join(btrfsBasePath, PagesWebrootDir, pageName)
	target := "/data/pages/" + pageName

	// Check if symlink already exists with correct target.
	existing, err := os.Readlink(linkPath)
	if err == nil && existing == target {
		return nil
	}

	// Remove any stale entry (file, dir, or wrong symlink).
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		slog.Debug("remove stale symlink", "path", linkPath, "error", err)
	}

	return os.Symlink(target, linkPath)
}

// RemovePageSymlink removes the symlink at {btrfsBasePath}/pages-webroot/{name}.
// No error is returned if the symlink does not exist.
func RemovePageSymlink(btrfsBasePath, pageName string) error {
	linkPath := filepath.Join(btrfsBasePath, PagesWebrootDir, pageName)
	err := os.Remove(linkPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// caddyImage returns the configured Caddy container image from settings.
func (s *SystemControllerHandlers) caddyImage() string {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return DefaultCaddyImage
	}

	val, err := mgr.Get("caddy_image")
	if err != nil || val == "" {
		return DefaultCaddyImage
	}

	return val
}
