// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package ingressctl is the systemcontroller-side lifecycle controller for the
// shared ingress (:443 SNI + :80 Host router): it generates, installs, and
// (re)starts the ingress systemd unit and exposes the gRPC socket path the
// systemcontroller dials to program routes. It lives apart from the ingress
// package proper so the in-container town-os-ingress binary — built
// CGO_ENABLED=0 — never imports src/systemd (which pulls in cgo via sdjournal).
package ingressctl

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/systemd"
)

const (
	// DefaultKey is the system-service key (and thus unit/container name suffix)
	// for the ingress: town-os-system--ingress.service / town-os-system--ingress.
	DefaultKey = "ingress"
	// SocketName is the gRPC Unix socket filename inside the data dir.
	SocketName = "ingress.sock"
	// HTTPSPort is the host port the ingress publishes for HTTPS.
	HTTPSPort = "443"
	// HTTPPort is the host port the ingress publishes for HTTP (Host-routed:
	// pages served directly, packages redirected to HTTPS, everything else to
	// the default backend / UI).
	HTTPPort = "80"
	// DefaultUIKey is the system-service key of the UI container the ingress
	// reverse-proxies to for hosts not matched by a route on :80. Kept here so
	// the CGO-free town-os-ingress binary never has to import package naming.
	DefaultUIKey = "ui"

	// MetricsHost is the host address the ingress's Prometheus endpoint is
	// published on. Loopback, never the LAN: the scrape names every hostname the
	// box serves and which of them have no certificate yet, and Prometheus runs
	// --net host so it reaches this with no podman-network hop — the same
	// arrangement node-exporter and the controller's own endpoint use.
	MetricsHost = "127.0.0.1"

	// containerDataDir is where the host data dir (holding the socket) is mounted.
	containerDataDir = "/data"
	// containerSocketPath is the socket path inside the container.
	containerSocketPath = "/data/" + SocketName
	// containerTLSMount is where the shared TLS subvolume is mounted (read-only).
	// Route cert dirs are paths under this mount, matching the leaf paths the
	// systemcontroller already issues.
	containerTLSMount = "/etc/town-os/tls"
)

// Config holds the configuration for the ingress Manager.
type Config struct {
	// Systemd installs/starts the ingress unit and queries its status.
	Systemd systemd.Manager
	// DataDir is the host directory mounted at /data; the ingress creates its
	// gRPC socket here so the systemcontroller (which also mounts the btrfs) can
	// dial it via SocketPath().
	DataDir string
	// TLSHostDir is the host TLS subvolume mounted read-only at
	// /etc/town-os/tls (the same tree the network controller mounts).
	TLSHostDir string
	// Image is the container image reference for the ingress.
	Image string
	// PullNever adds --pull=never to podman run (for locally-built images).
	PullNever bool
	// Key overrides the service key (defaults to DefaultKey). Tests set a unique
	// value to avoid colliding with a production ingress.
	Key string
	// NetworkName overrides the podman ingress network (defaults to
	// systemd.IngressNetworkName).
	NetworkName string
	// HostPort overrides the published HTTPS port. Defaults to 443. Integration
	// tests set an ephemeral port so a test ingress never collides with the
	// production ingress (or another test) on the privileged :443 — IRON RULE.
	HostPort int
	// HTTPHostPort overrides the published HTTP port. Defaults to 80. Integration
	// tests set an ephemeral port so a test ingress never collides on the
	// privileged :80 — IRON RULE.
	HTTPHostPort int
	// MetricsPort overrides the port the ingress serves its Prometheus endpoint
	// on, published on MetricsHost. Defaults to ingress.DefaultMetricsPort.
	// Unprivileged, but a fixed port in the host namespace all the same, so the
	// harness relocates it exactly as it relocates the other two — IRON RULE.
	MetricsPort int
	// DefaultBackend overrides the :80 fallback backend for unmatched hosts.
	// Defaults to the UI container (town-os-system--ui:80). Tests that do not run
	// a UI set it to "-" to disable the fallback vhost entirely.
	DefaultBackend string
	// EnableIPv6 makes the ingress serve dual-stack: the podman network is
	// created with --ipv6 and the HTTPS port is also published on [::] so AAAA
	// clients reach the same caddy. Set by the systemcontroller only when the
	// host has a global IPv6 (otherwise podman network create --ipv6 would fail
	// and the unit would not start), so IPv4-only hosts emit today's unit.
	EnableIPv6 bool
}

// Manager controls the lifecycle of the ingress container. Unlike rolodex (a
// shipped boot service), the ingress unit is generated, installed, enabled, and
// started by the systemcontroller — mirroring the UI manager.
type Manager struct {
	cfg Config
}

// NewManager creates a new ingress Manager. Call Start to boot the service and
// Dial(SocketPath()) to program it.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

func (m *Manager) key() string {
	if m.cfg.Key != "" {
		return m.cfg.Key
	}
	return DefaultKey
}

// Key returns the service key used for systemd unit and container naming.
func (m *Manager) Key() string { return m.key() }

func (m *Manager) network() string {
	if m.cfg.NetworkName != "" {
		return m.cfg.NetworkName
	}
	return systemd.IngressNetworkName
}

// hostPort returns the published HTTPS port, defaulting to 443.
func (m *Manager) hostPort() int {
	if m.cfg.HostPort != 0 {
		return m.cfg.HostPort
	}
	return 443
}

// httpHostPort returns the published HTTP port, defaulting to 80.
func (m *Manager) httpHostPort() int {
	if m.cfg.HTTPHostPort != 0 {
		return m.cfg.HTTPHostPort
	}
	return 80
}

// metricsPort returns the port the ingress serves its Prometheus endpoint on,
// defaulting to ingress.DefaultMetricsPort.
func (m *Manager) metricsPort() int {
	if m.cfg.MetricsPort != 0 {
		return m.cfg.MetricsPort
	}
	return ingress.DefaultMetricsPort
}

// MetricsAddr returns the "host:port" Prometheus should scrape this ingress at.
//
// Both ends of the scrape are built from this one function — the unit publishes
// MetricsAddrFor(m.metricsPort()) and monitoring's job targets the manager's
// MetricsAddr() — for the same single-source-of-truth reason
// rolodex.Manager.MetricsAddr() exists. A target recomposed from the default
// port would scrape whichever ingress happened to hold 9146 in the shared host
// namespace: a test box reporting a dev box's routes, or neither.
func (m *Manager) MetricsAddr() string {
	return MetricsAddrFor(m.metricsPort())
}

// MetricsAddrFor is MetricsAddr for a port that has not been handed to a
// Manager yet. It exists because the boot path has to write the Prometheus
// scrape config before it constructs the ingress manager, and building that
// address by hand at the one call site is how the two would drift. A port of 0
// means the default, matching Config.MetricsPort.
func MetricsAddrFor(port int) string {
	if port == 0 {
		port = ingress.DefaultMetricsPort
	}
	return net.JoinHostPort(MetricsHost, strconv.Itoa(port))
}

// defaultBackend returns the :80 fallback backend the ingress reverse-proxies
// for unmatched hosts. Empty disables the fallback vhost (Config.DefaultBackend
// set to "-"); the default is the UI container on port 80.
func (m *Manager) defaultBackend() string {
	switch m.cfg.DefaultBackend {
	case "-":
		return ""
	case "":
		return systemd.SystemServiceContainerName(DefaultUIKey) + ":80"
	default:
		return m.cfg.DefaultBackend
	}
}

// SocketPath returns the host path to the ingress gRPC Unix socket.
func (m *Manager) SocketPath() string {
	return filepath.Join(m.cfg.DataDir, SocketName)
}

// SystemService describes the ingress system service so the controller can list
// it and include it in system updates (image pull + unit restart), mirroring
// the UI, rolodex, and monitoring system services.
type SystemService struct {
	Key         string
	DisplayName string
	Image       string
	Port        string
	UnitName    string
}

// SystemServices returns metadata for the ingress system service. Ingress runs
// as a host systemd unit just like the UI and rolodex, so it must be part of the
// system-service listing and the system-update facility; omitting it here is why
// ingress would otherwise never be re-pulled or restarted on a system update.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{{
		Key:         m.key(),
		DisplayName: "Ingress",
		Image:       m.cfg.Image,
		Port:        strconv.Itoa(m.hostPort()),
		UnitName:    systemd.SystemServiceUnitName(m.key()),
	}}
}

// unitConfig builds the systemd unit config for the ingress container: it joins
// the ingress network (creating it if needed), publishes :443 and :80 plus the
// loopback-only Prometheus endpoint, mounts the data dir (for the socket)
// read-write and the TLS subvolume read-only, and runs the ingress binary
// against the in-container socket. The :80 listener is Host-routed (pages served
// directly, packages redirected to HTTPS, everything else to the default backend
// / UI).
func (m *Manager) unitConfig() systemd.SystemServiceUnitConfig {
	port := strconv.Itoa(m.hostPort())
	httpPort := strconv.Itoa(m.httpHostPort())
	metricsPort := strconv.Itoa(m.metricsPort())

	// Dual-stack: create the network with --ipv6 and also publish on [::] so
	// AAAA clients reach caddy. Gated on EnableIPv6 because `podman network
	// create --ipv6` fails on a host without IPv6.
	netCreate := "-/usr/bin/podman network create " + m.network()
	args := []string{"-p", port + ":" + port, "-p", httpPort + ":" + httpPort}
	if m.cfg.EnableIPv6 {
		netCreate = "-/usr/bin/podman network create --ipv6 " + m.network()
		args = append(args, "-p", "[::]:"+port+":"+port, "-p", "[::]:"+httpPort+":"+httpPort)
	}
	// The Prometheus endpoint is published with an explicit host IP, unlike
	// :443/:80 above, which are deliberately interface-agnostic. It is the one
	// listener here that is for the box itself rather than for its clients, and
	// binding it to every interface would put the box's whole route inventory —
	// including which names have no certificate yet — on the LAN and the overlay.
	args = append(args,
		"-p", MetricsHost+":"+metricsPort+":"+metricsPort,
		"--net", m.network(),
		"-v", m.cfg.DataDir+":"+containerDataDir+":z",
		"-v", m.cfg.TLSHostDir+":"+containerTLSMount+":ro,z",
	)

	command := []string{
		"--socket", containerSocketPath,
		"--port", port,
		"--http-port", httpPort,
		"--metrics-port", metricsPort,
	}
	if db := m.defaultBackend(); db != "" {
		command = append(command, "--default-backend", db)
	}

	return systemd.SystemServiceUnitConfig{
		Key:          m.key(),
		Description:  "Ingress (shared :443/:80 Host router)",
		Image:        m.cfg.Image,
		PullNever:    m.cfg.PullNever,
		VolumeDirs:   []string{m.cfg.DataDir},
		ExecStartPre: []string{netCreate},
		Args:         args,
		Command:      command,
	}
}

// Start installs, enables, and (re)starts the ingress systemd unit, mirroring
// the UI manager's stop-before-start so config changes are picked up.
func (m *Manager) Start(ctx context.Context) error {
	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil { //nolint:gosec // data dir must be accessible by the container process
		return fmt.Errorf("create ingress data dir: %w", err)
	}
	uf := systemd.GenerateSystemServiceUnit(m.unitConfig())
	if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		return fmt.Errorf("install unit %s: %w", uf.Name, err)
	}
	if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable unit %s: %w", uf.Name, err)
	}
	if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Stop); err != nil {
		slog.Debug("stop ingress before restart (may not be running)", "unit", uf.Name, "error", err)
	}
	if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
		return fmt.Errorf("start unit %s: %w", uf.Name, err)
	}
	return nil
}

// WaitForReady polls the ingress gRPC Unix socket until it accepts connections
// or the context (capped at 30s) is cancelled. Call before programming routes.
func (m *Manager) WaitForReady(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	socket := m.SocketPath()
	var dialer net.Dialer
	for {
		conn, err := dialer.DialContext(waitCtx, "unix", socket)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				slog.Debug("close ingress probe connection", "error", closeErr)
			}
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("ingress socket %s not ready: %w", socket, waitCtx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}
