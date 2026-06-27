// Package rolodex provides a client interface for the Rolodex DNS server.
// The Rolodex systemd unit is a boot service managed entirely by systemd —
// the systemcontroller does not install, start, stop, or restart it.
// This package writes the rolodex configuration file, waits for DNS
// readiness, and reports service status.
package rolodex

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/systemd"
)

const (
	// DefaultDNSPort is the default port for DNS queries.
	DefaultDNSPort = "53"
	// DefaultGRPCSocket is the default filename for the gRPC Unix socket.
	DefaultGRPCSocket = "rolodex.sock"
	// DNSLoopback is the loopback address used for local DNS resolution.
	// Using 127.0.0.2 avoids conflicts with systemd-resolved (127.0.0.53)
	// and other services commonly bound to 127.0.0.1.
	DNSLoopback = "127.0.0.2"
)

// DefaultForwarders are the upstream DNS forwarder addresses written to
// rolodex.yml when Config.Forwarders is not set.
var DefaultForwarders = []string{"8.8.8.8:53", "8.8.4.4:53"}

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

// Config holds the configuration for the rolodex manager.
type Config struct {
	// Systemd is used to query unit status (read-only).
	Systemd systemd.Manager
	// DataDir is the directory where rolodex configuration and data are stored.
	DataDir string
	// Image is the container image reference for the rolodex server.
	Image string
	// UnixSocketPath is the host path to the gRPC Unix socket.
	UnixSocketPath string
	// DNSPort is the host-side port for DNS queries. If empty,
	// DefaultDNSPort ("53") is used.
	DNSPort string
	// Key overrides the service key used for systemd unit and container
	// naming. Defaults to "rolodex". Tests should set a unique value to
	// avoid colliding with a production rolodex service.
	Key string
	// Forwarders overrides the upstream DNS forwarder addresses
	// ("host:port") written to rolodex.yml. Defaults to
	// DefaultForwarders. Tests point this at a local stub DNS server so
	// forwarding works without internet access (captive networks block
	// direct queries to public resolvers).
	Forwarders []string
}

// Manager provides rolodex configuration and status reporting. The Rolodex
// systemd unit is a boot service — this manager does not install, start,
// stop, or restart it.
type Manager struct {
	cfg Config
}

// NewManager creates a new rolodex Manager with the given configuration.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Key returns the service key used for systemd unit and container naming.
func (m *Manager) Key() string {
	return m.key()
}

// key returns the configured key or "rolodex" as default.
func (m *Manager) key() string {
	if m.cfg.Key != "" {
		return m.cfg.Key
	}
	return "rolodex"
}

// SocketPath returns the configured gRPC Unix socket path.
func (m *Manager) SocketPath() string {
	return m.cfg.UnixSocketPath
}

// dnsPort returns the configured host-side DNS port, defaulting to DefaultDNSPort.
func (m *Manager) dnsPort() string {
	if m.cfg.DNSPort != "" {
		return m.cfg.DNSPort
	}
	return DefaultDNSPort
}

// forwarders returns the configured upstream forwarder addresses, defaulting
// to DefaultForwarders.
func (m *Manager) forwarders() []string {
	if len(m.cfg.Forwarders) > 0 {
		return m.cfg.Forwarders
	}
	return DefaultForwarders
}

// rolodexConfig returns the canonical rolodex YAML configuration with the
// given DNS port and upstream forwarders. The bind address is always
// DNSLoopback (127.0.0.2) because the rolodex container runs with --net host.
func rolodexConfig(port string, forwarders []string) string {
	var fwd strings.Builder
	for _, f := range forwarders {
		fmt.Fprintf(&fwd, "  - %q\n", f)
	}
	return fmt.Sprintf(`database_path: /data/rolodex.db
resolution:
  mode: forward
dns:
  bind:
    - udp: "%s:%s"
    - tcp: "%s:%s"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
%srbl:
  enabled: false
  providers: []
`, DNSLoopback, port, DNSLoopback, port, fwd.String())
}

// executableMtime is the function used to get the systemcontroller binary's
// modification time. Replaceable in tests.
var executableMtime = defaultExecutableMtime

func defaultExecutableMtime() (time.Time, error) {
	exe, err := os.Executable()
	if err != nil {
		return time.Time{}, fmt.Errorf("resolve executable: %w", err)
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return time.Time{}, fmt.Errorf("stat executable: %w", err)
	}
	return fi.ModTime(), nil
}

// WriteConfig ensures the rolodex YAML configuration file exists and is
// up to date. Returns true if the file was written (created or updated),
// false if it was left unchanged. The file is skipped when it already
// exists, is newer than the systemcontroller binary (user-modified), and
// has the expected content.
func (m *Manager) WriteConfig() (bool, error) {
	if err := os.MkdirAll(m.cfg.DataDir, 0755); err != nil { //nolint:gosec // data dir must be accessible by container process
		return false, fmt.Errorf("create data dir: %w", err)
	}

	config := rolodexConfig(m.dnsPort(), m.forwarders())
	configPath := filepath.Join(m.cfg.DataDir, "rolodex.yml")

	fi, statErr := os.Stat(configPath)
	if statErr == nil {
		// File exists — check if it's newer than our binary.
		exeMtime, exeErr := executableMtime()
		if exeErr == nil && fi.ModTime().After(exeMtime) {
			// Config was modified after the SC image was built; don't overwrite.
			return false, nil
		}

		// File is older than (or same age as) our binary — check content.
		existing, readErr := os.ReadFile(configPath) //nolint:gosec // G304 -- configPath is constructed from the controlled DataDir config field
		if readErr == nil && string(existing) == config {
			return false, nil
		}
	}

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil { //nolint:gosec // config must be readable by container process
		return false, fmt.Errorf("write config: %w", err)
	}

	return true, nil
}

// WaitForDNSReady polls the DNS TCP port until it accepts connections or the
// context is cancelled. This should be called at startup to ensure the DNS
// server is ready before proceeding with operations that depend on DNS
// (e.g., pulling container images).
func (m *Manager) WaitForDNSReady(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	addr := net.JoinHostPort(DNSLoopback, m.dnsPort())
	var dialer net.Dialer

	for {
		conn, err := dialer.DialContext(waitCtx, "tcp", addr)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				slog.Debug("close DNS probe connection", "error", closeErr)
			}
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("DNS server at %s not ready: %w", addr, waitCtx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// SystemServices returns metadata for the rolodex system service.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{
		{
			Key:         m.key(),
			DisplayName: "Rolodex DNS",
			Image:       m.cfg.Image,
			Port:        m.dnsPort(),
			UnitName:    systemd.SystemServiceUnitName(m.key()),
		},
	}
}

// Status returns the current state of the rolodex container by querying
// systemd unit state. It uses GetUnitStates to avoid enumerating all unit
// files on disk, which on hosts with overlayfs root causes per-file inode
// lookups that can flood the kernel log with ESTALE warnings.
func (m *Manager) Status(ctx context.Context) Status {
	running := false
	unitName := systemd.SystemServiceUnitName(m.key())
	units, err := m.cfg.Systemd.GetUnitStates(ctx, []string{unitName})
	if err == nil {
		for _, u := range units {
			if u.Name == unitName {
				running = u.ActiveState == "active"
				break
			}
		}
	}

	return Status{
		Name:    systemd.SystemServiceContainerName(m.key()),
		Image:   m.cfg.Image,
		Running: running,
		Port:    m.dnsPort(),
	}
}
