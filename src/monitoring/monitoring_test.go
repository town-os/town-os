// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// --- Node Exporter tests (unchanged) ---

func TestNodeExporterUnitConfig(t *testing.T) {
	cfg := NodeExporterUnitConfig("")

	if cfg.Key != "node-exporter" {
		t.Fatalf("expected key node-exporter, got %q", cfg.Key)
	}
	if cfg.Image != NodeExporterImage {
		t.Fatalf("expected image %q, got %q", NodeExporterImage, cfg.Image)
	}

	// Check that --net host is in args.
	found := false
	for i, arg := range cfg.Args {
		if arg == "--net" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "host" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected --net host in args")
	}
}

func TestNodeExporterUnitConfigCustomPort(t *testing.T) {
	cfg := NodeExporterUnitConfig("19100")

	found := false
	for _, cmd := range cfg.Command {
		if strings.Contains(cmd, ":19100") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected custom port 19100 in command")
	}
}

func TestStartNodeExporter(t *testing.T) {
	sd := systemd.InitMockManager()

	if err := StartNodeExporter(t.Context(), sd, ""); err != nil {
		t.Fatalf("StartNodeExporter: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("node-exporter")
	if _, ok := sd.InstalledUnits[unitName]; !ok {
		t.Fatalf("expected unit %s to be installed", unitName)
	}

	calls := sd.GetCalls()
	installCount := 0
	enableCount := 0
	restartCount := 0
	for _, c := range calls {
		switch c.Method {
		case "InstallUnit":
			installCount++
		case "SetStatus":
			if len(c.Args) >= 2 {
				if c.Args[1] == systemd.Enable {
					enableCount++
				}
				if c.Args[1] == systemd.Restart {
					restartCount++
				}
			}
		}
	}

	if installCount != 1 {
		t.Fatalf("expected 1 InstallUnit, got %d", installCount)
	}
	if enableCount != 1 {
		t.Fatalf("expected 1 Enable, got %d", enableCount)
	}
	if restartCount != 1 {
		t.Fatalf("expected 1 Restart, got %d", restartCount)
	}
}

func TestStartNodeExporterInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission

	err := StartNodeExporter(t.Context(), sd, "")
	if err == nil {
		t.Fatal("expected error when InstallUnit fails")
	}
}

func TestNodeExporterSystemService(t *testing.T) {
	svc := NodeExporterSystemService("")

	if svc.Key != "node-exporter" {
		t.Fatalf("expected key node-exporter, got %q", svc.Key)
	}
	if svc.Image != NodeExporterImage {
		t.Fatalf("expected image %q, got %q", NodeExporterImage, svc.Image)
	}
	if svc.Port != NodeExporterPort {
		t.Fatalf("expected port %q, got %q", NodeExporterPort, svc.Port)
	}
}

func TestNodeExporterSystemServiceCustomPort(t *testing.T) {
	svc := NodeExporterSystemService("19100")
	if svc.Port != "19100" {
		t.Fatalf("expected custom port 19100, got %q", svc.Port)
	}
}

// --- Prometheus tests ---

func TestPrometheusUnitConfig(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusUnitConfig(btrfsBase)

	if cfg.Key != "prometheus" {
		t.Fatalf("expected key prometheus, got %q", cfg.Key)
	}
	if cfg.Image != PrometheusImage {
		t.Fatalf("expected image %q, got %q", PrometheusImage, cfg.Image)
	}

	// Check port mapping.
	foundPort := false
	for i, arg := range cfg.Args {
		if arg == "-p" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "9090:9090" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatalf("expected -p 9090:9090 in args, got %v", cfg.Args)
	}

	// Check volume mounts reference the btrfs base.
	foundConfig := false
	foundData := false
	for i, arg := range cfg.Args {
		if arg == "-v" && i+1 < len(cfg.Args) {
			if strings.Contains(cfg.Args[i+1], "prometheus-config:/etc/prometheus") {
				foundConfig = true
			}
			if strings.Contains(cfg.Args[i+1], "prometheus-data:/prometheus") {
				foundData = true
			}
		}
	}
	if !foundConfig {
		t.Fatalf("expected config volume mount, got %v", cfg.Args)
	}
	if !foundData {
		t.Fatalf("expected data volume mount, got %v", cfg.Args)
	}

	// Check VolumeDirs.
	if len(cfg.VolumeDirs) != 2 {
		t.Fatalf("expected 2 VolumeDirs, got %d", len(cfg.VolumeDirs))
	}
}

func TestPrometheusUnitConfigAlwaysUsesPort9090(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusUnitConfig(btrfsBase)

	// The unit always maps 9090:9090; the node exporter port only affects
	// the prometheus.yml config.
	foundPort := false
	for i, arg := range cfg.Args {
		if arg == "-p" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "9090:9090" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatal("expected -p 9090:9090")
	}
}

func TestWritePrometheusConfig(t *testing.T) {
	btrfsBase := t.TempDir()

	if err := WritePrometheusConfig(btrfsBase, ""); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}

	configFile := filepath.Join(btrfsBase, "monitoring", "prometheus-config", "prometheus.yml")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "host.containers.internal:9100") {
		t.Fatal("config should scrape node-exporter on default port 9100")
	}
	if !strings.Contains(content, "localhost:9090") {
		t.Fatal("config should scrape prometheus itself")
	}
}

func TestWritePrometheusConfigCustomPort(t *testing.T) {
	btrfsBase := t.TempDir()

	if err := WritePrometheusConfig(btrfsBase, "19100"); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}

	configFile := filepath.Join(btrfsBase, "monitoring", "prometheus-config", "prometheus.yml")
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	if !strings.Contains(string(data), "host.containers.internal:19100") {
		t.Fatal("config should use custom node exporter port 19100")
	}
}

func TestStartPrometheus(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	if err := StartPrometheus(t.Context(), sd, btrfsBase, ""); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("prometheus")
	if _, ok := sd.InstalledUnits[unitName]; !ok {
		t.Fatalf("expected unit %s to be installed", unitName)
	}

	// Verify config was written.
	configFile := filepath.Join(btrfsBase, "monitoring", "prometheus-config", "prometheus.yml")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatalf("prometheus.yml should exist: %v", err)
	}

	// Verify install/enable/restart sequence.
	calls := sd.GetCalls()
	var enableCount, restartCount int
	for _, c := range calls {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			if c.Args[1] == systemd.Enable {
				enableCount++
			}
			if c.Args[1] == systemd.Restart {
				restartCount++
			}
		}
	}
	if enableCount != 1 {
		t.Fatalf("expected 1 Enable, got %d", enableCount)
	}
	if restartCount != 1 {
		t.Fatalf("expected 1 Restart, got %d", restartCount)
	}
}

func TestStartPrometheusInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission
	btrfsBase := t.TempDir()

	err := StartPrometheus(t.Context(), sd, btrfsBase, "")
	if err == nil {
		t.Fatal("expected error when InstallUnit fails")
	}
}

func TestPrometheusSystemService(t *testing.T) {
	svc := PrometheusSystemService()

	if svc.Key != "prometheus" {
		t.Fatalf("expected key prometheus, got %q", svc.Key)
	}
	if svc.Image != PrometheusImage {
		t.Fatalf("expected image %q, got %q", PrometheusImage, svc.Image)
	}
	if svc.Port != PrometheusPort {
		t.Fatalf("expected port %q, got %q", PrometheusPort, svc.Port)
	}
	if svc.UnitName != systemd.SystemServiceUnitName("prometheus") {
		t.Fatalf("expected unit name %q, got %q", systemd.SystemServiceUnitName("prometheus"), svc.UnitName)
	}
}

func TestPrometheusUnitIsSystemService(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusUnitConfig(btrfsBase)
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("prometheus unit %q should be a system service unit", uf.Name)
	}
	if !strings.Contains(uf.Content, PrometheusImage) {
		t.Fatal("unit should reference prometheus image")
	}
	if !strings.Contains(uf.Content, "--config.file=/etc/prometheus/prometheus.yml") {
		t.Fatal("unit should include prometheus command args")
	}
	if !strings.Contains(uf.Content, "Restart=always") {
		t.Fatal("unit should restart always")
	}
}

// --- Monitoring UI tests ---

func TestMonitoringUIUnitConfigUPlot(t *testing.T) {
	cfg := MonitoringUIUnitConfig(BackendUPlot, "")

	if cfg.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", cfg.Key)
	}
	if cfg.Image != SocatImage {
		t.Fatalf("expected image %q, got %q", SocatImage, cfg.Image)
	}

	// Should use host networking.
	foundHost := false
	for i, arg := range cfg.Args {
		if arg == "--net" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "host" {
			foundHost = true
		}
	}
	if !foundHost {
		t.Fatalf("uplot mode should use --net host, got %v", cfg.Args)
	}

	// Command should include socat forwarding 5308 to 9090.
	cmdStr := strings.Join(cfg.Command, " ")
	if !strings.Contains(cmdStr, "socat TCP-LISTEN:5308") {
		t.Fatalf("expected socat on port 5308, got %v", cfg.Command)
	}
	if !strings.Contains(cmdStr, "TCP:127.0.0.1:9090") {
		t.Fatalf("expected socat target 127.0.0.1:9090, got %v", cfg.Command)
	}
}

func TestMonitoringUIUnitConfigGrafana(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := MonitoringUIUnitConfig(BackendGrafana, btrfsBase)

	if cfg.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", cfg.Key)
	}
	if cfg.Image != GrafanaImage {
		t.Fatalf("expected image %q, got %q", GrafanaImage, cfg.Image)
	}

	// Should have port mapping 5308:3000.
	foundPort := false
	for i, arg := range cfg.Args {
		if arg == "-p" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "5308:3000" {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatalf("grafana mode should map 5308:3000, got %v", cfg.Args)
	}

	// Should have grafana env vars.
	foundAnon := false
	for i, arg := range cfg.Args {
		if arg == "-e" && i+1 < len(cfg.Args) && cfg.Args[i+1] == "GF_AUTH_ANONYMOUS_ENABLED=true" {
			foundAnon = true
		}
	}
	if !foundAnon {
		t.Fatal("grafana mode should set GF_AUTH_ANONYMOUS_ENABLED")
	}

	// Should have volume dirs.
	if len(cfg.VolumeDirs) != 2 {
		t.Fatalf("expected 2 VolumeDirs for grafana, got %d", len(cfg.VolumeDirs))
	}
}

func TestMonitoringUIUnitConfigDefaultBackend(t *testing.T) {
	cfg := MonitoringUIUnitConfig("", "")

	// Empty backend defaults to uplot.
	if cfg.Image != SocatImage {
		t.Fatalf("empty backend should default to uplot (socat), got image %q", cfg.Image)
	}
}

func TestWriteGrafanaProvisioningFiles(t *testing.T) {
	btrfsBase := t.TempDir()

	if err := WriteGrafanaProvisioningFiles(btrfsBase); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	// Verify datasource file.
	dsFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml")
	data, err := os.ReadFile(dsFile)
	if err != nil {
		t.Fatalf("read datasource: %v", err)
	}
	if !strings.Contains(string(data), "host.containers.internal:9090") {
		t.Fatal("datasource should point to prometheus via host gateway")
	}

	// Verify dashboard provider file.
	provFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboards", "default.yml")
	data, err = os.ReadFile(provFile)
	if err != nil {
		t.Fatalf("read dashboard provider: %v", err)
	}
	if !strings.Contains(string(data), "dashboard-json") {
		t.Fatal("provider should reference dashboard-json directory")
	}

	// Verify dashboard JSON directory exists.
	jsonDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboard-json")
	info, err := os.Stat(jsonDir)
	if err != nil {
		t.Fatalf("dashboard-json dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("dashboard-json should be a directory")
	}
}

func TestStartMonitoringUI(t *testing.T) {
	sd := systemd.InitMockManager()

	if err := StartMonitoringUI(t.Context(), sd, BackendUPlot, ""); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("monitoring-ui")
	if _, ok := sd.InstalledUnits[unitName]; !ok {
		t.Fatalf("expected unit %s to be installed", unitName)
	}

	// Verify the unit content uses socat.
	content := sd.InstalledUnits[unitName]
	if !strings.Contains(content, "socat") {
		t.Fatalf("uplot mode unit should use socat, got:\n%s", content)
	}
}

func TestStartMonitoringUIGrafana(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	if err := StartMonitoringUI(t.Context(), sd, BackendGrafana, btrfsBase); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("monitoring-ui")
	content, ok := sd.InstalledUnits[unitName]
	if !ok {
		t.Fatalf("expected unit %s to be installed", unitName)
	}

	if !strings.Contains(content, GrafanaImage) {
		t.Fatalf("grafana mode unit should use grafana image, got:\n%s", content)
	}

	// Verify provisioning files were written.
	dsFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml")
	if _, err := os.Stat(dsFile); err != nil {
		t.Fatalf("grafana provisioning should be written: %v", err)
	}
}

func TestStartMonitoringUIInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission

	err := StartMonitoringUI(t.Context(), sd, BackendUPlot, "")
	if err == nil {
		t.Fatal("expected error when InstallUnit fails")
	}
}

func TestMonitoringUISystemServiceUPlot(t *testing.T) {
	svc := MonitoringUISystemService(BackendUPlot)

	if svc.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", svc.Key)
	}
	if svc.Image != SocatImage {
		t.Fatalf("expected socat image for uplot, got %q", svc.Image)
	}
	if svc.Port != MonitoringExternalPort {
		t.Fatalf("expected port %q, got %q", MonitoringExternalPort, svc.Port)
	}
}

func TestMonitoringUISystemServiceGrafana(t *testing.T) {
	svc := MonitoringUISystemService(BackendGrafana)

	if svc.Image != GrafanaImage {
		t.Fatalf("expected grafana image, got %q", svc.Image)
	}
	if svc.Port != MonitoringExternalPort {
		t.Fatalf("expected port %q, got %q", MonitoringExternalPort, svc.Port)
	}
}

func TestMonitoringUIUnitIsSystemService(t *testing.T) {
	cfg := MonitoringUIUnitConfig(BackendUPlot, "")
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", uf.Name)
	}
}

func TestMonitoringUIGrafanaUnitIsSystemService(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := MonitoringUIUnitConfig(BackendGrafana, btrfsBase)
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", uf.Name)
	}
	if !strings.Contains(uf.Content, "5308:3000") {
		t.Fatal("grafana unit should map port 5308:3000")
	}
}
