// Package rolodex manages a Rolodex privacy-first DNS server as a system
// service. The server runs as a systemd-supervised podman container with
// Restart=always, providing DNS management capabilities via gRPC over a Unix
// socket.
package rolodex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gitea.com/town-os/town-os/src/systemd"
)

const (
	// DefaultDNSPort is the default port for DNS queries.
	DefaultDNSPort = "53"
	// DefaultGRPCSocket is the default filename for the gRPC Unix socket.
	DefaultGRPCSocket = "rolodex.sock"
	// DefaultDataDir is the default root-filesystem path for rolodex data.
	DefaultDataDir = "/var/lib/town-os/rolodex"
	// DNSLoopback is the loopback address used for local DNS resolution.
	// Using 127.0.0.2 avoids conflicts with systemd-resolved (127.0.0.53)
	// and other services commonly bound to 127.0.0.1.
	DNSLoopback = "127.0.0.2"
)

// SystemService describes the rolodex system service metadata.
type SystemService struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
	UnitName    string `json:"unit_name"`
}

// Status represents the current state of the rolodex container.
type Status struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Running bool   `json:"running"`
	Port    string `json:"port"`
}

// Config holds the configuration for the rolodex service.
type Config struct {
	// Systemd manages systemd unit lifecycle.
	Systemd systemd.Manager
	// DataDir is the directory where rolodex configuration and data are stored.
	DataDir string
	// Image is the container image reference for the rolodex server.
	Image string
	// Local enables host networking mode (ROLODEX_LOCAL).
	Local bool
	// UnixSocketPath is the host path to the gRPC Unix socket.
	UnixSocketPath string
	// ResolvConfPath, when set, is the path to resolv.conf. Start
	// rewrites this file to use 127.0.0.1 as the sole nameserver;
	// Stop restores the original contents.
	ResolvConfPath string
}

// Manager controls the lifecycle of the rolodex DNS server.
type Manager struct {
	cfg              Config
	mu               sync.Mutex
	origResolvConf   []byte // saved on Start, restored on Stop
	origResolvLink   string // symlink target if resolv.conf was a symlink
	disabledResolved bool   // true if we stopped/disabled systemd-resolved
}

const resolvedUnit = "systemd-resolved.service"

// NewManager creates a new rolodex Manager with the given configuration.
// Call Start to boot the rolodex service.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// SocketPath returns the configured gRPC Unix socket path.
func (m *Manager) SocketPath() string {
	return m.cfg.UnixSocketPath
}

// Start writes the rolodex config, installs/enables/starts the systemd unit.
// In production mode (!Local), it disables systemd-resolved and rewrites
// resolv.conf before starting rolodex, ensuring exactly one DNS provider is
// active. On failure, it rolls back by re-enabling systemd-resolved.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writeConfig(); err != nil {
		return fmt.Errorf("write rolodex config: %w", err)
	}

	// Install and enable the unit in all modes.
	for _, unit := range m.unitConfigs() {
		uf := systemd.GenerateSystemServiceUnit(unit)
		if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
			return fmt.Errorf("install unit %s: %w", uf.Name, err)
		}
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable unit %s: %w", uf.Name, err)
		}
	}

	if !m.cfg.Local {
		// Production: disable systemd-resolved, rewrite resolv.conf, start rolodex.
		m.disableResolved(ctx)

		if err := m.setResolv(); err != nil {
			m.enableResolved(ctx)
			return fmt.Errorf("set resolv.conf: %w", err)
		}

		for _, unit := range m.unitConfigs() {
			uf := systemd.GenerateSystemServiceUnit(unit)
			// Stop before Start to pick up new configuration. Ignore
			// stop errors — the unit may not be running.
			_ = m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop)
			if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
				_ = m.restoreResolv()
				m.enableResolved(ctx)
				return fmt.Errorf("start unit %s: %w", uf.Name, err)
			}
		}
	} else {
		// Local: start directly (127.0.0.2:53 coexists with resolved).
		for _, unit := range m.unitConfigs() {
			uf := systemd.GenerateSystemServiceUnit(unit)
			_ = m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop)
			if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
				return fmt.Errorf("start unit %s: %w", uf.Name, err)
			}
		}
	}

	return nil
}

// Stop stops the rolodex systemd unit. In production mode, it re-enables
// systemd-resolved and restores resolv.conf BEFORE stopping rolodex so
// there is always a working DNS provider.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.Local {
		// Production: restore system DNS before stopping rolodex.
		m.enableResolved(ctx)

		if err := m.restoreResolv(); err != nil {
			return fmt.Errorf("restore resolv.conf: %w", err)
		}
	}

	unitName := systemd.SystemServiceUnitName("rolodex")
	if err := m.cfg.Systemd.SetStatus(ctx, unitName, systemd.Stop); err != nil {
		return fmt.Errorf("stop unit %s: %w", unitName, err)
	}

	return nil
}

// SystemServices returns metadata for the rolodex system service.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{
		{
			Key:         "rolodex",
			DisplayName: "Rolodex DNS",
			Image:       m.cfg.Image,
			Port:        DefaultDNSPort,
			UnitName:    systemd.SystemServiceUnitName("rolodex"),
		},
	}
}

// Status returns the current state of the rolodex container by querying
// systemd unit state.
func (m *Manager) Status(ctx context.Context) Status {
	running := false
	units, err := m.cfg.Systemd.ListUnits(ctx)
	if err == nil {
		unitName := systemd.SystemServiceUnitName("rolodex")
		for _, u := range units {
			if u.Name == unitName {
				running = u.ActiveState == "active"
				break
			}
		}
	}

	return Status{
		Name:    systemd.SystemServiceContainerName("rolodex"),
		Image:   m.cfg.Image,
		Running: running,
		Port:    DefaultDNSPort,
	}
}

// writeConfig writes the rolodex YAML configuration file to DataDir.
func (m *Manager) writeConfig() error {
	if err := os.MkdirAll(m.cfg.DataDir, 0755); err != nil { //nolint:gosec // data dir must be accessible by container process
		return fmt.Errorf("create data dir: %w", err)
	}

	config := `database_path: /data/rolodex.db
dns:
  udp_bind: "0.0.0.0:53"
  tcp_bind: "0.0.0.0:53"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "8.8.8.8:53"
  - "8.8.4.4:53"
rbl:
  enabled: false
  providers: []
`

	configPath := filepath.Join(m.cfg.DataDir, "rolodex.yml")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// unitConfigs returns the systemd unit configurations for the rolodex service.
func (m *Manager) unitConfigs() []systemd.SystemServiceUnitConfig {
	args := []string{
		"-v", m.cfg.DataDir + ":/data",
	}

	if m.cfg.Local {
		args = append([]string{
			"-p", DNSLoopback + ":53:53/tcp",
			"-p", DNSLoopback + ":53:53/udp",
		}, args...)
	} else {
		args = append([]string{
			"-p", "53:53/tcp",
			"-p", "53:53/udp",
		}, args...)
	}

	return []systemd.SystemServiceUnitConfig{
		{
			Key:         "rolodex",
			Description: "Rolodex DNS",
			Image:       m.cfg.Image,
			Args:        args,
			Command:     []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
			VolumeDirs:  []string{m.cfg.DataDir},
		},
	}
}

// disableResolved stops and disables systemd-resolved. Errors are ignored
// because the system may not have systemd-resolved installed.
func (m *Manager) disableResolved(ctx context.Context) {
	_ = m.cfg.Systemd.SetStatus(ctx, resolvedUnit, systemd.Stop)
	_ = m.cfg.Systemd.SetStatus(ctx, resolvedUnit, systemd.Disable)
	m.disabledResolved = true
}

// enableResolved re-enables and starts systemd-resolved if it was previously
// disabled by disableResolved. Errors are ignored (best-effort).
func (m *Manager) enableResolved(ctx context.Context) {
	if !m.disabledResolved {
		return
	}
	_ = m.cfg.Systemd.SetStatus(ctx, resolvedUnit, systemd.Enable)
	_ = m.cfg.Systemd.SetStatus(ctx, resolvedUnit, systemd.Start)
	m.disabledResolved = false
}

// setResolv saves the current resolv.conf state and rewrites it to point at
// 127.0.0.1 (rolodex). Handles both regular files and symlinks (e.g.
// systemd-resolved's /run/systemd/resolve/stub-resolv.conf link).
func (m *Manager) setResolv() error {
	if m.cfg.ResolvConfPath == "" {
		return nil
	}

	fi, err := os.Lstat(m.cfg.ResolvConfPath)
	if err != nil {
		return fmt.Errorf("stat %s: %w", m.cfg.ResolvConfPath, err)
	}

	if fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(m.cfg.ResolvConfPath)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", m.cfg.ResolvConfPath, err)
		}
		m.origResolvLink = target
		m.origResolvConf = nil
		if err := os.Remove(m.cfg.ResolvConfPath); err != nil {
			return fmt.Errorf("remove symlink %s: %w", m.cfg.ResolvConfPath, err)
		}
	} else {
		data, err := os.ReadFile(m.cfg.ResolvConfPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", m.cfg.ResolvConfPath, err)
		}
		m.origResolvConf = data
		m.origResolvLink = ""
	}

	if err := os.WriteFile(m.cfg.ResolvConfPath, []byte("nameserver 127.0.0.1\n"), 0644); err != nil { //nolint:gosec // resolv.conf must be world-readable
		return fmt.Errorf("write %s: %w", m.cfg.ResolvConfPath, err)
	}

	return nil
}

// restoreResolv restores the original resolv.conf saved by setResolv.
// If it was a symlink, the symlink is recreated; otherwise the original
// content is written back.
func (m *Manager) restoreResolv() error {
	if m.cfg.ResolvConfPath == "" {
		return nil
	}

	if m.origResolvLink != "" {
		if err := os.Remove(m.cfg.ResolvConfPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", m.cfg.ResolvConfPath, err)
		}
		if err := os.Symlink(m.origResolvLink, m.cfg.ResolvConfPath); err != nil {
			return fmt.Errorf("symlink %s -> %s: %w", m.cfg.ResolvConfPath, m.origResolvLink, err)
		}
		m.origResolvLink = ""
	} else if m.origResolvConf != nil {
		if err := os.WriteFile(m.cfg.ResolvConfPath, m.origResolvConf, 0644); err != nil { //nolint:gosec // resolv.conf must be world-readable
			return fmt.Errorf("write %s: %w", m.cfg.ResolvConfPath, err)
		}
		m.origResolvConf = nil
	}

	return nil
}
