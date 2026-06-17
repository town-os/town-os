// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"gitea.com/town-os/town-os/src/systemd"
)

// PagesServeCaddyDir holds the static Caddyfile for the standalone pages service
// (distinct from the legacy pages-caddy ingress config the file-mounted ingress
// used to consume).
const PagesServeCaddyDir = "pages-serve-caddy"

// pagesServeCaddyfile is the fixed config for the pages static-file service: a
// single :80 site that serves /srv/<first-label-of-Host> via file_server. The
// map directive extracts the leftmost DNS label of the request Host (the page
// name) regardless of TLD depth, so the shared ingress can route every page
// FQDN here without the pages service ever being reprogrammed per page.
const pagesServeCaddyfile = `:80 {
	map {host} {page} {
		~^([^.]+)\.  ${1}
	}
	root * /srv/{page}
	file_server
}
`

// StartPagesService renders the static pages Caddyfile and installs, enables,
// and (re)starts the town-os-system--pages container — a plain Caddy static
// file server on the shared ingress network. The ingress reverse-proxies each
// page FQDN to it (see buildIngressRoutes/pagesBackend). Page content lives on
// the same btrfs dirs the page CRUD handlers populate (pages subvolumes +
// webroot symlinks), mounted read-only.
func StartPagesService(ctx context.Context, sd systemd.Manager, btrfsBase, image string) error {
	if sd == nil || btrfsBase == "" {
		return nil
	}
	if image == "" {
		image = DefaultCaddyImage
	}

	// Ensure the mount sources exist before podman binds them.
	if err := EnsurePagesWebroot(btrfsBase); err != nil {
		return fmt.Errorf("ensure pages webroot: %w", err)
	}
	pagesDir := filepath.Join(btrfsBase, PagesVolumePrefix)
	if err := os.MkdirAll(pagesDir, 0755); err != nil { //nolint:gosec // page content dir, served read-only
		return fmt.Errorf("ensure pages dir: %w", err)
	}
	caddyDir := filepath.Join(btrfsBase, PagesServeCaddyDir)
	if err := os.MkdirAll(caddyDir, 0755); err != nil { //nolint:gosec // caddy config dir
		return fmt.Errorf("ensure pages caddy dir: %w", err)
	}
	caddyfilePath := filepath.Join(caddyDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, []byte(pagesServeCaddyfile), 0644); err != nil { //nolint:gosec // caddy config readable by container
		return fmt.Errorf("write pages Caddyfile: %w", err)
	}

	webroot := filepath.Join(btrfsBase, PagesWebrootDir)
	unit := systemd.GenerateSystemServiceUnit(systemd.SystemServiceUnitConfig{
		Key:          PagesServiceKey,
		Description:  "Pages (static file server)",
		Image:        image,
		VolumeDirs:   []string{webroot, pagesDir, caddyDir},
		ExecStartPre: []string{"-/usr/bin/podman network create " + systemd.IngressNetworkName},
		Args: []string{
			"--net", systemd.IngressNetworkName,
			"-v", webroot + ":/srv:ro,z",
			"-v", pagesDir + ":/data/pages:ro,z",
			"-v", caddyfilePath + ":/etc/caddy/Caddyfile:ro,z",
		},
	})
	if err := sd.InstallUnit(ctx, unit.Name, unit.Content); err != nil {
		return fmt.Errorf("install pages unit: %w", err)
	}
	if err := sd.SetStatus(ctx, unit.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable pages unit: %w", err)
	}
	if err := sd.SetStatus(ctx, unit.Name, systemd.Stop); err != nil {
		slog.Debug("stop pages before restart (may not be running)", "unit", unit.Name, "error", err)
	}
	if err := sd.SetStatus(ctx, unit.Name, systemd.Start); err != nil {
		return fmt.Errorf("start pages unit: %w", err)
	}
	return nil
}
