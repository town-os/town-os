// Package rolodex manages a Rolodex privacy-first DNS server as a system
// service. The server runs as a systemd-supervised podman container with
// Restart=always, providing DNS management capabilities via gRPC over a Unix
// socket.
package rolodex

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	// DNSPort is the host-side port for DNS queries. If empty,
	// DefaultDNSPort ("53") is used. The container always binds 0.0.0.0:53
	// internally; this only controls the -p host mapping.
	DNSPort string
	// ResolvedConfDir, when set, is the path to the systemd-resolved
	// drop-in directory (e.g. /etc/systemd/resolved.conf.d). Start
	// writes a drop-in that enables MulticastDNS in systemd-resolved
	// for .local hostname advertisement and enables mDNS on the
	// interface bound to PublicAddr.
	ResolvedConfDir string
	// PublicAddr, when set, adds an additional -p binding on this address
	// so that DNS is reachable from the LAN (e.g. "192.168.5.9").
	// DNSLoopback is always bound regardless.
	PublicAddr string
	// Key overrides the service key used for systemd unit and container
	// naming. Defaults to "rolodex". Tests should set a unique value to
	// avoid colliding with a production rolodex service.
	Key string
}

// Manager controls the lifecycle of the rolodex DNS server.
type Manager struct {
	cfg              Config
	mu             sync.Mutex
	origResolvConf []byte // saved on Start, restored on Stop
	origResolvLink string // symlink target if resolv.conf was a symlink
}

// NewManager creates a new rolodex Manager with the given configuration.
// Call Start to boot the rolodex service.
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

// SetPublicAddr updates the public address and returns true if it changed.
// The caller should call Start again to apply the new binding.
func (m *Manager) SetPublicAddr(addr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.PublicAddr == addr {
		return false
	}
	m.cfg.PublicAddr = addr
	return true
}

// Start writes the rolodex config, compares the desired systemd unit with
// the installed one, and restarts the service only when the unit content
// has changed (e.g. public address binding added) or the unit does not
// exist yet. The Rolodex systemd unit is installed on the USB image and
// started at boot before the systemcontroller; this method only manages
// configuration updates. In production mode (!Local), resolv.conf is
// rewritten to point at the rolodex loopback address. Returns true if
// the service was restarted.
func (m *Manager) Start(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Configure systemd-resolved for mDNS hostname advertisement.
	// This enables .local resolution (e.g. town-os.local) via
	// systemd-resolved on the physical network interface.
	if err := m.configureResolvedMDNS(ctx); err != nil {
		return false, fmt.Errorf("configure resolved mDNS: %w", err)
	}

	if err := m.writeConfig(); err != nil {
		return false, fmt.Errorf("write rolodex config: %w", err)
	}

	// Compare desired unit content with what is currently installed.
	// Only restart when the unit has changed (e.g. public address
	// binding was added) or does not exist yet.
	restarted := false
	for _, unit := range m.unitConfigs() {
		uf := systemd.GenerateSystemServiceUnit(unit)

		current, readErr := m.cfg.Systemd.ReadUnit(uf.Name)
		unitChanged := readErr != nil || current != uf.Content

		if unitChanged {
			if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
				return false, fmt.Errorf("install unit %s: %w", uf.Name, err)
			}
			if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
				return false, fmt.Errorf("enable unit %s: %w", uf.Name, err)
			}
			if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
				return false, fmt.Errorf("restart unit %s: %w", uf.Name, err)
			}
			restarted = true
		}
	}

	if !m.cfg.Local {
		if err := m.setResolv(); err != nil {
			return restarted, fmt.Errorf("set resolv.conf: %w", err)
		}
	}

	return restarted, nil
}

// WaitForDNSReady polls the DNS TCP port until it accepts connections or the
// context is cancelled. This should be called after Start to ensure the DNS
// server inside the container is actually ready to serve queries before
// proceeding with operations that depend on DNS (e.g., pulling container
// images).
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

// Stop restores resolv.conf to the systemd-resolved stub in production
// mode. The Rolodex systemd unit is a boot service and is NOT stopped
// here — it continues running independently of the systemcontroller.
func (m *Manager) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.cfg.Local {
		if err := m.restoreResolv(); err != nil {
			return fmt.Errorf("restore resolv.conf: %w", err)
		}
	}

	return nil
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
// systemd unit state.
func (m *Manager) Status(ctx context.Context) Status {
	running := false
	units, err := m.cfg.Systemd.ListUnits(ctx)
	if err == nil {
		unitName := systemd.SystemServiceUnitName(m.key())
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

	port := m.dnsPort()
	portArgs := []string{
		"-p", DNSLoopback + ":" + port + ":53/tcp",
		"-p", DNSLoopback + ":" + port + ":53/udp",
	}
	if m.cfg.PublicAddr != "" {
		portArgs = append(portArgs,
			"-p", m.cfg.PublicAddr+":"+port+":53/tcp",
			"-p", m.cfg.PublicAddr+":"+port+":53/udp",
		)
	}
	args = append(portArgs, args...)

	cfg := systemd.SystemServiceUnitConfig{
		Key:         m.key(),
		Description: "Rolodex DNS",
		Image:       m.cfg.Image,
		Args:        args,
		Command:     []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
		VolumeDirs:  []string{m.cfg.DataDir},
	}

	// When managing resolv.conf, add unit directives so systemd handles
	// DNS switchover automatically. ExecStartPre points resolv.conf at
	// the rolodex loopback address; ExecStopPost restores the
	// systemd-resolved stub so DNS keeps working if rolodex stops or
	// crashes.
	if m.cfg.ResolvConfPath != "" && !m.cfg.Local {
		cfg.ExecStartPre = []string{
			fmt.Sprintf(`/bin/sh -c 'printf "nameserver %s\n" > %s'`, DNSLoopback, m.cfg.ResolvConfPath),
		}
		cfg.ExecStopPost = []string{
			"-/bin/ln -sf /run/systemd/resolve/stub-resolv.conf " + m.cfg.ResolvConfPath,
			"-/bin/systemctl reload-or-restart systemd-resolved",
		}
	}

	return []systemd.SystemServiceUnitConfig{cfg}
}

// setResolv saves the current resolv.conf state and rewrites it to point at
// DNSLoopback (127.0.0.2). Handles both regular files and symlinks (e.g.
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

	if err := os.WriteFile(m.cfg.ResolvConfPath, []byte("nameserver "+DNSLoopback+"\n"), 0644); err != nil { //nolint:gosec // resolv.conf must be world-readable
		return fmt.Errorf("write %s: %w", m.cfg.ResolvConfPath, err)
	}

	return nil
}

// configureResolvedMDNS writes a systemd-resolved drop-in that enables
// MulticastDNS for .local hostname advertisement, reloads the resolved
// configuration, and enables mDNS on the interface bound to PublicAddr.
func (m *Manager) configureResolvedMDNS(ctx context.Context) error {
	if m.cfg.ResolvedConfDir == "" {
		return nil
	}

	if err := os.MkdirAll(m.cfg.ResolvedConfDir, 0755); err != nil { //nolint:gosec // system config dir
		return fmt.Errorf("create resolved conf dir: %w", err)
	}

	dropIn := "[Resolve]\nDNSStubListener=yes\nDNSStubListenerExtra=\nMulticastDNS=yes\n"
	dropInPath := filepath.Join(m.cfg.ResolvedConfDir, "townos.conf")
	if err := os.WriteFile(dropInPath, []byte(dropIn), 0644); err != nil { //nolint:gosec // system config must be world-readable
		return fmt.Errorf("write resolved drop-in: %w", err)
	}

	// Reload systemd-resolved and enable per-interface mDNS only when
	// running as root. These are global system commands — systemctl and
	// resolvectl talk to the real host systemd, not the temp directory
	// the drop-in was written to. Without this guard, unit tests would
	// trigger polkit authentication prompts by hitting the host's
	// systemd-resolved. The systemcontroller always runs as root in
	// production. The drop-in file is still written regardless so
	// resolved picks up the config on its next reload/restart.
	if os.Getuid() == 0 {
		reloadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(reloadCtx, "systemctl", "reload", "systemd-resolved").CombinedOutput(); err != nil {
			slog.Warn(fmt.Sprintf("reload systemd-resolved: %v: %s", err, out))
		}

		// Enable mDNS on the physical interface so the hostname is
		// advertised on the LAN (networkd may default to mDNS=no).
		if iface := interfaceForIP(m.cfg.PublicAddr); iface != "" {
			mdnsCtx, mdnsCancel := context.WithTimeout(ctx, 5*time.Second)
			defer mdnsCancel()
			out, err := exec.CommandContext(mdnsCtx, "resolvectl", "mdns", iface, "yes").CombinedOutput() //nolint:gosec // iface is from net.Interfaces(), not user input
			if err != nil {
				slog.Warn(fmt.Sprintf("resolvectl mdns %s yes: %v: %s", iface, err, out))
			}
		}
	}

	return nil
}

// interfaceForIP returns the name of the network interface that holds the
// given IPv4 address, or "" if not found.
func interfaceForIP(addr string) string {
	if addr == "" {
		return ""
	}
	target := net.ParseIP(addr)
	if target == nil {
		return ""
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(target) {
				return iface.Name
			}
		}
	}
	return ""
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
