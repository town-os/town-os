// Package monitoring manages an integrated Prometheus, Node Exporter, and
// Grafana monitoring stack. The stack is started automatically when the
// Control Plane Service boots and provides system metrics via Grafana
// dashboards embedded in the web UI.
package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const (
	// PrometheusImage is the container image reference for Prometheus.
	PrometheusImage = "quay.io/prometheus/prometheus:latest"
	// NodeExporterImage is the container image reference for Node Exporter.
	NodeExporterImage = "quay.io/prometheus/node-exporter:latest"
	// GrafanaImage is the container image reference for Grafana.
	GrafanaImage = "docker.io/grafana/grafana:latest"

	// PrometheusPort is the default internal port used by Prometheus.
	PrometheusPort = "9090"
	NodeExporterPort = "9100"
	GrafanaPort      = "3000"

	// containerPrefix is prepended to all monitoring container names to avoid
	// collisions with user-installed packages.
	containerPrefix = "town-os-monitoring-"

	// healthCheckInterval controls how often the manager verifies that each
	// container is still running and restarts any that have stopped.
	healthCheckInterval = 30 * time.Second
)

// ContainerStatus represents the state of a single monitoring container.
type ContainerStatus struct {
	// Name is the container name (e.g. "town-os-monitoring-grafana").
	Name string `json:"name"`
	// Image is the container image reference.
	Image string `json:"image"`
	// Running is true when the container is in a running state.
	Running bool `json:"running"`
	// Port is the host port the container listens on.
	Port string `json:"port"`
}

// Status aggregates the state of all three monitoring containers.
type Status struct {
	Prometheus   ContainerStatus `json:"prometheus"`
	NodeExporter ContainerStatus `json:"node_exporter"`
	Grafana      ContainerStatus `json:"grafana"`
}

// Runner abstracts container lifecycle operations so that tests can supply
// a mock implementation without requiring a real podman installation.
type Runner interface {
	// Run starts a container with the given arguments. The args slice contains
	// the full argument list passed to `podman run` (excluding the `podman run`
	// prefix itself).
	Run(ctx context.Context, args []string) error
	// Stop stops and removes a container by name. It is not an error if the
	// container does not exist.
	Stop(ctx context.Context, name string) error
	// IsRunning returns true if the named container exists and is running.
	IsRunning(ctx context.Context, name string) (bool, error)
}

// PodmanRunner implements Runner using the podman CLI.
type PodmanRunner struct{}

// Run starts a container by executing `podman run` with the provided args.
//
// Parameters:
//   - ctx: context for cancellation and timeout.
//   - args: full argument list for `podman run` (image, ports, volumes, etc.).
func (PodmanRunner) Run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "podman", append([]string{"run"}, args...)...) //nolint:gosec // G204 -- args from trusted Config
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Stop stops and removes a container by name. It is a no-op if the container
// does not exist.
//
// Parameters:
//   - ctx: context for cancellation and timeout.
//   - name: the container name to stop and remove.
func (PodmanRunner) Stop(ctx context.Context, name string) error {
	stop := exec.CommandContext(ctx, "podman", "stop", "-t", "10", name) //nolint:gosec // G204 -- name from trusted caller
	stop.Stdout = os.Stderr
	stop.Stderr = os.Stderr
	_ = stop.Run() // best-effort stop before remove

	rm := exec.CommandContext(ctx, "podman", "rm", "-f", name) //nolint:gosec // G204 -- name from trusted caller
	rm.Stdout = os.Stderr
	rm.Stderr = os.Stderr
	_ = rm.Run() // best-effort removal

	return nil
}

// IsRunning returns true if the named container exists and is in a running state.
//
// Parameters:
//   - ctx: context for cancellation and timeout.
//   - name: the container name to check.
func (PodmanRunner) IsRunning(ctx context.Context, name string) (bool, error) {
	cmd := exec.CommandContext(ctx, "podman", "inspect", "--format", "{{.State.Running}}", name) //nolint:gosec // G204 -- name from trusted caller
	out, err := cmd.Output()
	if err != nil {
		return false, nil //nolint:nilerr // container not found is not an error
	}
	return string(out) == "true\n", nil
}

// Config holds the configuration for the monitoring stack.
type Config struct {
	// Runner executes container lifecycle commands.
	Runner Runner
	// DataDir is the directory where monitoring configuration and data files
	// are stored (prometheus config, grafana provisioning, etc.).
	DataDir string
	// PrometheusPort is the host port for Prometheus. Defaults to "9091" to
	// avoid conflicts with user-installed packages.
	PrometheusPort string
	// NodeExporterPort is the host port for Node Exporter. Defaults to "9101".
	NodeExporterPort string
	// GrafanaPort is the host port for Grafana. Defaults to "3001".
	GrafanaPort string
}

func (c *Config) prometheusHostPort() string {
	if c.PrometheusPort != "" {
		return c.PrometheusPort
	}
	return "9091"
}

func (c *Config) nodeExporterHostPort() string {
	if c.NodeExporterPort != "" {
		return c.NodeExporterPort
	}
	return "9101"
}

func (c *Config) grafanaHostPort() string {
	if c.GrafanaPort != "" {
		return c.GrafanaPort
	}
	return "3001"
}

// Manager controls the lifecycle of the monitoring stack.
type Manager struct {
	cfg    Config
	mu     sync.Mutex
	cancel context.CancelFunc
}

// NewManager creates a new monitoring Manager with the given configuration.
// Call Start to boot the monitoring stack.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Start boots all three monitoring containers and begins a background health
// check loop that restarts any stopped containers. The provided context
// controls the lifetime of the health check loop; cancelling it triggers
// graceful shutdown of all containers.
//
// Parameters:
//   - ctx: parent context; cancellation stops the health check loop and all containers.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writeConfigs(); err != nil {
		return fmt.Errorf("write monitoring configs: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	if err := m.startAll(ctx); err != nil {
		cancel()
		return err
	}

	go m.healthLoop(ctx)
	return nil
}

// Stop gracefully shuts down all monitoring containers.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, name := range m.containerNames() {
		if err := m.cfg.Runner.Stop(ctx, name); err != nil {
			slog.Debug(fmt.Sprintf("monitoring: stop %s: %v", name, err))
		}
	}
}

// Status returns the current state of all monitoring containers.
func (m *Manager) Status(ctx context.Context) Status {
	promRunning, _ := m.cfg.Runner.IsRunning(ctx, containerPrefix+"prometheus")
	neRunning, _ := m.cfg.Runner.IsRunning(ctx, containerPrefix+"node-exporter")
	grafRunning, _ := m.cfg.Runner.IsRunning(ctx, containerPrefix+"grafana")

	return Status{
		Prometheus: ContainerStatus{
			Name:    containerPrefix + "prometheus",
			Image:   PrometheusImage,
			Running: promRunning,
			Port:    m.cfg.prometheusHostPort(),
		},
		NodeExporter: ContainerStatus{
			Name:    containerPrefix + "node-exporter",
			Image:   NodeExporterImage,
			Running: neRunning,
			Port:    m.cfg.nodeExporterHostPort(),
		},
		Grafana: ContainerStatus{
			Name:    containerPrefix + "grafana",
			Image:   GrafanaImage,
			Running: grafRunning,
			Port:    m.cfg.grafanaHostPort(),
		},
	}
}

func (m *Manager) containerNames() []string {
	return []string{
		containerPrefix + "node-exporter",
		containerPrefix + "prometheus",
		containerPrefix + "grafana",
	}
}

func (m *Manager) startAll(ctx context.Context) error {
	// Stop any existing containers first for idempotency.
	for _, name := range m.containerNames() {
		if err := m.cfg.Runner.Stop(ctx, name); err != nil {
			slog.Debug(fmt.Sprintf("monitoring: pre-stop %s: %v", name, err))
		}
	}

	if err := m.startNodeExporter(ctx); err != nil {
		return fmt.Errorf("start node-exporter: %w", err)
	}
	if err := m.startPrometheus(ctx); err != nil {
		return fmt.Errorf("start prometheus: %w", err)
	}
	if err := m.startGrafana(ctx); err != nil {
		return fmt.Errorf("start grafana: %w", err)
	}
	return nil
}

func (m *Manager) startNodeExporter(ctx context.Context) error {
	return m.cfg.Runner.Run(ctx, []string{
		"-d",
		"--name", containerPrefix + "node-exporter",
		"--net", "host",
		"--pid", "host",
		"--cap-add", "SYS_TIME",
		"-v", "/:/host:ro,rslave",
		NodeExporterImage,
		"--path.rootfs=/host",
		"--web.listen-address=:" + m.cfg.nodeExporterHostPort(),
	})
}

func (m *Manager) startPrometheus(ctx context.Context) error {
	configPath := filepath.Join(m.cfg.DataDir, "prometheus.yml")
	dataDir := filepath.Join(m.cfg.DataDir, "prometheus-data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return fmt.Errorf("create prometheus data dir: %w", err)
	}

	return m.cfg.Runner.Run(ctx, []string{
		"-d",
		"--name", containerPrefix + "prometheus",
		"--net", "host",
		"-u", "0",
		"-v", configPath + ":/etc/prometheus/prometheus.yml:ro",
		"-v", dataDir + ":/prometheus:shared,rw",
		PrometheusImage,
		"--config.file=/etc/prometheus/prometheus.yml",
		"--storage.tsdb.path=/prometheus",
		"--storage.tsdb.retention.time=30d",
		"--web.listen-address=:" + m.cfg.prometheusHostPort(),
	})
}

func (m *Manager) startGrafana(ctx context.Context) error {
	provisioningDir := filepath.Join(m.cfg.DataDir, "grafana-provisioning")
	dataDir := filepath.Join(m.cfg.DataDir, "grafana-data")
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return fmt.Errorf("create grafana data dir: %w", err)
	}

	return m.cfg.Runner.Run(ctx, []string{
		"-d",
		"--name", containerPrefix + "grafana",
		"--net", "host",
		"-u", "0",
		"-e", "GF_AUTH_ANONYMOUS_ENABLED=true",
		"-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin",
		"-e", "GF_AUTH_DISABLE_LOGIN_FORM=true",
		"-e", "GF_SECURITY_ALLOW_EMBEDDING=true",
		"-e", "GF_SERVER_ENABLE_GZIP=true",
		"-e", "GF_SERVER_HTTP_PORT=" + m.cfg.grafanaHostPort(),
		"-v", provisioningDir + ":/etc/grafana/provisioning:ro",
		"-v", dataDir + ":/var/lib/grafana:shared,rw",
		GrafanaImage,
	})
}

func (m *Manager) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.ensureRunning(ctx)
		}
	}
}

func (m *Manager) ensureRunning(ctx context.Context) {
	containers := map[string]func(context.Context) error{
		containerPrefix + "node-exporter": m.startNodeExporter,
		containerPrefix + "prometheus":    m.startPrometheus,
		containerPrefix + "grafana":       m.startGrafana,
	}

	for name, startFn := range containers {
		running, err := m.cfg.Runner.IsRunning(ctx, name)
		if err != nil {
			slog.Debug(fmt.Sprintf("monitoring: health check %s: %v", name, err))
			continue
		}
		if !running {
			slog.Info("monitoring: restarting " + name)
			if err := m.cfg.Runner.Stop(ctx, name); err != nil {
				slog.Debug(fmt.Sprintf("monitoring: pre-stop %s: %v", name, err))
			}
			if err := startFn(ctx); err != nil {
				slog.Error(fmt.Sprintf("monitoring: restart %s: %v", name, err))
			}
		}
	}
}

// writeConfigs writes the Prometheus configuration and Grafana provisioning
// files to the data directory.
func (m *Manager) writeConfigs() error {
	if err := os.MkdirAll(m.cfg.DataDir, 0750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	if err := writePrometheusConfig(m.cfg.DataDir, m.cfg.nodeExporterHostPort()); err != nil {
		return fmt.Errorf("prometheus config: %w", err)
	}

	if err := writeGrafanaProvisioning(m.cfg.DataDir, m.cfg.prometheusHostPort()); err != nil {
		return fmt.Errorf("grafana provisioning: %w", err)
	}

	return nil
}
