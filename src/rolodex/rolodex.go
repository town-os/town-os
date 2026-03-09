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
	cfg            Config
	mu             sync.Mutex
	origResolvConf []byte // saved on Start, restored on Stop
}

// NewManager creates a new rolodex Manager with the given configuration.
// Call Start to boot the rolodex service.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// SocketPath returns the configured gRPC Unix socket path.
func (m *Manager) SocketPath() string {
	return m.cfg.UnixSocketPath
}

// Start writes the rolodex config, rewrites resolv.conf (if configured),
// and installs/enables/starts the systemd unit. Systemd handles restarts
// via Restart=always — no health loop needed.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writeConfig(); err != nil {
		return fmt.Errorf("write rolodex config: %w", err)
	}

	if err := m.setResolv(); err != nil {
		return fmt.Errorf("set resolv.conf: %w", err)
	}

	for _, unit := range m.unitConfigs() {
		uf := systemd.GenerateSystemServiceUnit(unit)
		if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
			return fmt.Errorf("install unit %s: %w", uf.Name, err)
		}
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable unit %s: %w", uf.Name, err)
		}
		// Stop before Start to ensure the unit picks up the new
		// configuration. Ignore stop errors — the unit may not be running.
		_ = m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop)
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
			return fmt.Errorf("start unit %s: %w", uf.Name, err)
		}
	}

	return nil
}

// Stop stops the rolodex systemd unit and restores the original resolv.conf.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	unitName := systemd.SystemServiceUnitName("rolodex")
	if err := m.cfg.Systemd.SetStatus(ctx, unitName, systemd.Stop); err != nil {
		return fmt.Errorf("stop unit %s: %w", unitName, err)
	}

	if err := m.restoreResolv(); err != nil {
		return fmt.Errorf("restore resolv.conf: %w", err)
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
			"-p", "5300:53/tcp",
			"-p", "5300:53/udp",
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

// setResolv saves the current resolv.conf and rewrites it to use DNSLoopback.
func (m *Manager) setResolv() error {
	if m.cfg.ResolvConfPath == "" {
		return nil
	}

	orig, err := os.ReadFile(m.cfg.ResolvConfPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", m.cfg.ResolvConfPath, err)
	}
	m.origResolvConf = orig

	if err := os.WriteFile(m.cfg.ResolvConfPath, fmt.Appendf(nil, "nameserver %s\n", DNSLoopback), 0644); err != nil { //nolint:gosec // resolv.conf must be world-readable
		return fmt.Errorf("write %s: %w", m.cfg.ResolvConfPath, err)
	}

	return nil
}

// restoreResolv restores the original resolv.conf saved by setResolv.
func (m *Manager) restoreResolv() error {
	if m.cfg.ResolvConfPath == "" || m.origResolvConf == nil {
		return nil
	}

	if err := os.WriteFile(m.cfg.ResolvConfPath, m.origResolvConf, 0644); err != nil { //nolint:gosec // resolv.conf must be world-readable
		return fmt.Errorf("write %s: %w", m.cfg.ResolvConfPath, err)
	}
	m.origResolvConf = nil

	return nil
}
