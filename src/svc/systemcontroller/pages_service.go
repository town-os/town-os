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
// single :80 site that serves /srv/<full-request-host> via file_server. Page
// content directories (and the webroot symlinks under /srv) are named by the
// page's served FQDN, so keying on the whole Host — not just the leftmost
// label — means two pages whose first labels collide (blog.a.com vs
// blog.b.com) resolve to distinct roots. The shared ingress preserves the
// original Host when proxying here, so no per-page reprogramming is needed.
const pagesServeCaddyfile = `:80 {
	root * /srv/{host}
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
	// The generated object-storage index pages. A second content root beside
	// the pages one, mounted at the path the webroot symlinks point into —
	// see the header comment in gfeh_index.go for why the index does not simply
	// live under pages/. Created here so podman has something to bind even on a
	// box with no partitions yet; an unmounted source would fail the unit.
	indexDir := gfehIndexRoot(btrfsBase)
	if err := os.MkdirAll(indexDir, 0755); err != nil { //nolint:gosec // served read-only by the pages container
		return fmt.Errorf("ensure gfeh index dir: %w", err)
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
		VolumeDirs:   []string{webroot, pagesDir, caddyDir, indexDir},
		ExecStartPre: []string{"-/usr/bin/podman network create " + systemd.IngressNetworkName},
		Args: []string{
			"--net", systemd.IngressNetworkName,
			"-v", webroot + ":/srv:ro,z",
			"-v", pagesDir + ":/data/pages:ro,z",
			"-v", indexDir + ":" + GfehIndexContainerDir + ":ro,z",
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
