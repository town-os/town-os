// Package monitoring manages an integrated Prometheus, Node Exporter, and
// Grafana monitoring stack. The stack runs as systemd-supervised podman
// containers (system services) with Restart=always, providing system metrics
// via Grafana dashboards embedded in the web UI.
package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gitea.com/town-os/town-os/src/systemd"
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
)

// ContainerStatus represents the state of a single monitoring container.
type ContainerStatus struct {
	// Name is the container name (e.g. "town-os-system--prometheus").
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

// SystemService describes a system service managed by the monitoring stack.
type SystemService struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
	Image       string `json:"image"`
	Port        string `json:"port"`
	UnitName    string `json:"unit_name"`
}

// Config holds the configuration for the monitoring stack.
type Config struct {
	// Systemd manages systemd unit lifecycle.
	Systemd systemd.Manager
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
	cfg Config
	mu  sync.Mutex
}

// NewManager creates a new monitoring Manager with the given configuration.
// Call Start to boot the monitoring stack.
func NewManager(cfg Config) *Manager {
	return &Manager{cfg: cfg}
}

// Start writes monitoring configs and installs systemd units for each service.
// Systemd handles restarts via Restart=always — no health loop needed.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.writeConfigs(); err != nil {
		return fmt.Errorf("write monitoring configs: %w", err)
	}

	for _, unit := range m.unitConfigs() {
		uf := systemd.GenerateSystemServiceUnit(unit)
		if err := m.cfg.Systemd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
			return fmt.Errorf("install unit %s: %w", uf.Name, err)
		}
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
			return fmt.Errorf("enable unit %s: %w", uf.Name, err)
		}
		if err := m.cfg.Systemd.SetStatus(ctx, uf.Name, systemd.Start); err != nil {
			return fmt.Errorf("start unit %s: %w", uf.Name, err)
		}
	}

	return nil
}

// Stop is a no-op — system services persist across controller restarts.
func (m *Manager) Stop() {}

// Status returns the current state of all monitoring containers by querying
// systemd unit states.
func (m *Manager) Status(ctx context.Context) Status {
	unitStates := map[string]bool{}
	units, err := m.cfg.Systemd.ListUnits(ctx)
	if err == nil {
		for _, u := range units {
			if systemd.IsSystemServiceUnit(u.Name) {
				unitStates[u.Name] = u.ActiveState == "active"
			}
		}
	}

	return Status{
		Prometheus: ContainerStatus{
			Name:    systemd.SystemServiceContainerName("prometheus"),
			Image:   PrometheusImage,
			Running: unitStates[systemd.SystemServiceUnitName("prometheus")],
			Port:    m.cfg.prometheusHostPort(),
		},
		NodeExporter: ContainerStatus{
			Name:    systemd.SystemServiceContainerName("node-exporter"),
			Image:   NodeExporterImage,
			Running: unitStates[systemd.SystemServiceUnitName("node-exporter")],
			Port:    m.cfg.nodeExporterHostPort(),
		},
		Grafana: ContainerStatus{
			Name:    systemd.SystemServiceContainerName("grafana"),
			Image:   GrafanaImage,
			Running: unitStates[systemd.SystemServiceUnitName("grafana")],
			Port:    m.cfg.grafanaHostPort(),
		},
	}
}

// SystemServices returns metadata for all system services managed by this stack.
func (m *Manager) SystemServices() []SystemService {
	return []SystemService{
		{
			Key:         "prometheus",
			DisplayName: "Prometheus",
			Image:       PrometheusImage,
			Port:        m.cfg.prometheusHostPort(),
			UnitName:    systemd.SystemServiceUnitName("prometheus"),
		},
		{
			Key:         "node-exporter",
			DisplayName: "Node Exporter",
			Image:       NodeExporterImage,
			Port:        m.cfg.nodeExporterHostPort(),
			UnitName:    systemd.SystemServiceUnitName("node-exporter"),
		},
		{
			Key:         "grafana",
			DisplayName: "Grafana",
			Image:       GrafanaImage,
			Port:        m.cfg.grafanaHostPort(),
			UnitName:    systemd.SystemServiceUnitName("grafana"),
		},
	}
}

func (m *Manager) unitConfigs() []systemd.SystemServiceUnitConfig {
	configPath := filepath.Join(m.cfg.DataDir, "prometheus.yml")
	promDataDir := filepath.Join(m.cfg.DataDir, "prometheus-data")
	provisioningDir := filepath.Join(m.cfg.DataDir, "grafana-provisioning")
	grafDataDir := filepath.Join(m.cfg.DataDir, "grafana-data")

	return []systemd.SystemServiceUnitConfig{
		{
			Key:         "node-exporter",
			Description: "Node Exporter",
			Image:       NodeExporterImage,
			Args: []string{
				"--net", "host",
				"--pid", "host",
				"--cap-add", "SYS_TIME",
				"-v", "/:/host:ro,rslave",
			},
			Command: []string{
				"--path.rootfs=/host",
				"--web.listen-address=:" + m.cfg.nodeExporterHostPort(),
			},
		},
		{
			Key:         "prometheus",
			Description: "Prometheus",
			Image:       PrometheusImage,
			Args: []string{
				"--net", "host",
				"-u", "0",
				"-v", configPath + ":/etc/prometheus/prometheus.yml:ro",
				"-v", promDataDir + ":/prometheus:shared,rw",
			},
			Command: []string{
				"--config.file=/etc/prometheus/prometheus.yml",
				"--storage.tsdb.path=/prometheus",
				"--storage.tsdb.retention.time=30d",
				fmt.Sprintf("--web.listen-address=:%s", m.cfg.prometheusHostPort()),
			},
		},
		{
			Key:         "grafana",
			Description: "Grafana",
			Image:       GrafanaImage,
			Args: []string{
				"--net", "host",
				"-u", "0",
				"-e", "GF_AUTH_ANONYMOUS_ENABLED=true",
				"-e", "GF_AUTH_ANONYMOUS_ORG_ROLE=Admin",
				"-e", "GF_AUTH_DISABLE_LOGIN_FORM=true",
				"-e", "GF_SECURITY_ALLOW_EMBEDDING=true",
				"-e", "GF_USERS_DEFAULT_THEME=light",
				"-e", "GF_SERVER_ENABLE_GZIP=true",
				"-e", fmt.Sprintf("GF_SERVER_HTTP_PORT=%s", m.cfg.grafanaHostPort()),
				"-v", fmt.Sprintf("%s:/etc/grafana/provisioning:ro", provisioningDir),
				"-v", fmt.Sprintf("%s:/var/lib/grafana:shared,rw", grafDataDir),
			},
		},
	}
}

// writeConfigs writes the Prometheus configuration and Grafana provisioning
// files to the data directory.
func (m *Manager) writeConfigs() error {
	if err := os.MkdirAll(m.cfg.DataDir, 0750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	promDataDir := filepath.Join(m.cfg.DataDir, "prometheus-data")
	if err := os.MkdirAll(promDataDir, 0750); err != nil {
		return fmt.Errorf("create prometheus data dir: %w", err)
	}

	grafDataDir := filepath.Join(m.cfg.DataDir, "grafana-data")
	if err := os.MkdirAll(grafDataDir, 0750); err != nil {
		return fmt.Errorf("create grafana data dir: %w", err)
	}

	if err := writePrometheusConfig(m.cfg.DataDir, m.cfg.nodeExporterHostPort()); err != nil {
		return fmt.Errorf("prometheus config: %w", err)
	}

	if err := writeGrafanaProvisioning(m.cfg.DataDir, m.cfg.prometheusHostPort()); err != nil {
		return fmt.Errorf("grafana provisioning: %w", err)
	}

	return nil
}
