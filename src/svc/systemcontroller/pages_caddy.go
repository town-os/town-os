package systemcontroller

import (
	"fmt"
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
	DefaultCaddyPort   = "8080"
)

// PagesUnitConfig holds the parameters needed to generate the Caddy systemd unit.
type PagesUnitConfig struct {
	BtrfsBasePath string
	CaddyImage    string
	CaddyPort     string
}

// GenerateCaddyfile returns the Caddyfile content for the pages web server.
func GenerateCaddyfile(port string) string {
	return fmt.Sprintf(`:%s {
    root * /srv
    file_server
    respond / 404
}
`, port)
}

// GeneratePagesUnit generates a systemd service unit for the Caddy container.
func GeneratePagesUnit(cfg PagesUnitConfig) systemd.UnitFile {
	var b strings.Builder

	// [Unit]
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Town OS Pages Web Server\n")
	b.WriteString("After=network-online.target\n")

	// [Service]
	b.WriteString("\n[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman stop -t 10 %s\n", PagesContainerName)
	fmt.Fprintf(&b, "ExecStartPre=-/usr/bin/podman rm -f %s\n", PagesContainerName)
	fmt.Fprintf(&b, "ExecStart=/usr/bin/podman run --name %s --net host", PagesContainerName)
	fmt.Fprintf(&b, " \\\n  -v %s/%s:/data/pages:ro,z", cfg.BtrfsBasePath, PagesVolumePrefix)
	fmt.Fprintf(&b, " \\\n  -v %s/%s:/srv:ro,z", cfg.BtrfsBasePath, PagesWebrootDir)
	fmt.Fprintf(&b, " \\\n  -v %s/%s/Caddyfile:/etc/caddy/Caddyfile:ro,z", cfg.BtrfsBasePath, PagesCaddyDir)
	fmt.Fprintf(&b, " \\\n  %s\n", cfg.CaddyImage)
	fmt.Fprintf(&b, "ExecStop=/usr/bin/podman stop -t 10 %s\n", PagesContainerName)
	b.WriteString("Restart=on-failure\n")

	// [Install]
	b.WriteString("\n[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")

	return systemd.UnitFile{
		Name:    PagesUnitName,
		Content: b.String(),
	}
}

// WriteCaddyfile writes the Caddyfile to {btrfsBasePath}/pages-caddy/Caddyfile.
// Returns the path to the written file.
func WriteCaddyfile(btrfsBasePath, port string) (string, error) {
	dir := filepath.Join(btrfsBasePath, PagesCaddyDir)
	if err := os.MkdirAll(dir, 0755); err != nil { //nolint:gosec // G301 -- caddy config directory
		return "", fmt.Errorf("create caddy dir: %w", err)
	}

	path := filepath.Join(dir, "Caddyfile")
	content := GenerateCaddyfile(port)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil { //nolint:gosec // G306 -- caddy config readable by container
		return "", fmt.Errorf("write Caddyfile: %w", err)
	}

	return path, nil
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
	_ = os.Remove(linkPath)

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

// caddyPort returns the configured Caddy listen port from settings.
func (s *SystemControllerHandlers) caddyPort() string {
	mgr := s.Controller.GetSettingsManager()
	if mgr == nil {
		return DefaultCaddyPort
	}

	val, err := mgr.Get("caddy_port")
	if err != nil || val == "" {
		return DefaultCaddyPort
	}

	return val
}
