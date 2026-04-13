// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Node Exporter tests (unchanged — still a system service) ---

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

// --- Prometheus tests (uses GeneratePackageUnits) ---

func TestPrometheusPackageConfig(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusPackageConfig(btrfsBase, "nc:test", "/run/state")

	if cfg.SystemServiceKey != "prometheus" {
		t.Fatalf("expected SystemServiceKey prometheus, got %q", cfg.SystemServiceKey)
	}
	if cfg.Image != PrometheusImage {
		t.Fatalf("expected image %q, got %q", PrometheusImage, cfg.Image)
	}
	if cfg.NetworkControllerImage != "nc:test" {
		t.Fatalf("expected NC image nc:test, got %q", cfg.NetworkControllerImage)
	}

	// Only port 9090. Port 5308 belongs to the monitoring-ui service.
	if _, ok := cfg.External[9090]; !ok {
		t.Fatal("expected external port 9090")
	}
	if _, ok := cfg.External[5308]; ok {
		t.Fatal("prometheus should NOT have port 5308 (that belongs to monitoring-ui)")
	}

	// Volume mounts reference btrfs base.
	foundConfig := false
	foundData := false
	for _, hv := range cfg.HostVolumeMounts {
		if strings.Contains(hv.HostPath, "prometheus-config") && hv.ContainerPath == "/etc/prometheus" {
			foundConfig = true
		}
		if strings.Contains(hv.HostPath, "prometheus-data") && hv.ContainerPath == "/prometheus" {
			foundData = true
		}
	}
	if !foundConfig {
		t.Fatal("expected prometheus-config host volume mount")
	}
	if !foundData {
		t.Fatal("expected prometheus-data host volume mount")
	}

	if len(cfg.MkdirPaths) != 2 {
		t.Fatalf("expected 2 MkdirPaths, got %d", len(cfg.MkdirPaths))
	}
	if !cfg.RestartAlways {
		t.Fatal("expected RestartAlways=true")
	}
	if !cfg.StartLimitIntervalZero {
		t.Fatal("expected StartLimitIntervalZero=true")
	}
}

func TestPrometheusGeneratesPackageUnits(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusPackageConfig(btrfsBase, "nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	expectedSvcName := systemd.SystemServiceUnitName("prometheus")
	if units.Service.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, units.Service.Name)
	}

	if units.NetworkController == nil {
		t.Fatal("prometheus should have a network controller unit")
	}
	if !strings.Contains(units.NetworkController.Name, "prometheus-network") {
		t.Fatalf("NC unit name should contain prometheus-network, got %q", units.NetworkController.Name)
	}

	// 1 socket (9090 only).
	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}

	svc := units.Service.Content
	if !strings.Contains(svc, PrometheusImage) {
		t.Fatal("unit should reference prometheus image")
	}
	if !strings.Contains(svc, "Restart=always") {
		t.Fatal("unit should restart always")
	}
	if !strings.Contains(svc, "prometheus-config:/etc/prometheus") {
		t.Fatal("unit should have config host volume mount")
	}
	if !strings.Contains(svc, "chown") {
		t.Fatal("unit should have chown ExecStartPre")
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
	networkStatePath := t.TempDir()

	if err := StartPrometheus(t.Context(), sd, btrfsBase, "", "nc:test", networkStatePath); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("prometheus")
	if _, ok := sd.InstalledUnits[svcUnit]; !ok {
		t.Fatalf("expected service unit %s to be installed", svcUnit)
	}

	// NC unit should be installed.
	ncInstalled := false
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "prometheus-network") {
			ncInstalled = true
			break
		}
	}
	if !ncInstalled {
		t.Fatal("expected NC unit to be installed")
	}

	// 1 socket unit (9090).
	sockInstalled := 0
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-tcp.socket") {
			sockInstalled++
		}
	}
	if sockInstalled != 1 {
		t.Fatalf("expected 1 socket unit, got %d installed", sockInstalled)
	}

	configFile := filepath.Join(btrfsBase, "monitoring", "prometheus-config", "prometheus.yml")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatalf("prometheus.yml should exist: %v", err)
	}

	stateFiles, readErr := os.ReadDir(networkStatePath)
	if readErr != nil {
		t.Fatalf("read state dir: %v", readErr)
	}
	if len(stateFiles) == 0 {
		t.Fatal("expected NC state file to be written")
	}
}

func TestStartPrometheusInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()

	err := StartPrometheus(t.Context(), sd, btrfsBase, "", "nc:test", networkStatePath)
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
	cfg := PrometheusPackageConfig(btrfsBase, "nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	if !systemd.IsSystemServiceUnit(units.Service.Name) {
		t.Fatalf("prometheus unit %q should be a system service unit", units.Service.Name)
	}
}

// --- Monitoring UI tests (both uPlot and Grafana use GeneratePackageUnits) ---

func TestUPlotPackageConfig(t *testing.T) {
	cfg := UPlotPackageConfig("nc:test", "/run/state")

	if cfg.SystemServiceKey != "monitoring-ui" {
		t.Fatalf("expected SystemServiceKey monitoring-ui, got %q", cfg.SystemServiceKey)
	}
	if cfg.Image != "nc:test" {
		t.Fatalf("expected image nc:test, got %q", cfg.Image)
	}

	// Port 5308.
	if _, ok := cfg.External[5308]; !ok {
		t.Fatal("expected external port 5308")
	}

	// Command should include socat.
	cmdStr := strings.Join(cfg.Command, " ")
	if !strings.Contains(cmdStr, "socat") {
		t.Fatalf("expected socat in command, got %v", cfg.Command)
	}
	if !strings.Contains(cmdStr, "TCP-LISTEN:5308") {
		t.Fatalf("expected socat on port 5308, got %v", cfg.Command)
	}
	if !strings.Contains(cmdStr, "TCP:host.containers.internal:9090") {
		t.Fatalf("expected socat target host.containers.internal:9090, got %v", cfg.Command)
	}
	if strings.Contains(cmdStr, "127.0.0.1:9090") {
		t.Fatalf("socat target must not be 127.0.0.1:9090 (unreachable from inside the monitoring-ui container network), got %v", cfg.Command)
	}
}

func TestUPlotDefaultSocatImage(t *testing.T) {
	cfg := UPlotPackageConfig("", "/run/state")
	if cfg.Image != DefaultSocatImage {
		t.Fatalf("empty ncImage should default to %q, got %q", DefaultSocatImage, cfg.Image)
	}
	if cfg.NetworkControllerImage != DefaultSocatImage {
		t.Fatalf("empty ncImage should also set NetworkControllerImage to %q, got %q", DefaultSocatImage, cfg.NetworkControllerImage)
	}
}

func TestUPlotGeneratesPackageUnits(t *testing.T) {
	cfg := UPlotPackageConfig("nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	expectedSvcName := systemd.SystemServiceUnitName("monitoring-ui")
	if units.Service.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, units.Service.Name)
	}

	if units.NetworkController == nil {
		t.Fatal("uPlot monitoring-ui should have an NC unit")
	}

	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}

	if !strings.Contains(units.Service.Content, "socat") {
		t.Fatal("uPlot unit should use socat")
	}
}

func TestGrafanaPackageConfig(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := GrafanaPackageConfig(btrfsBase, "nc:test", "/run/state")

	if cfg.SystemServiceKey != "monitoring-ui" {
		t.Fatalf("expected SystemServiceKey monitoring-ui, got %q", cfg.SystemServiceKey)
	}
	if cfg.Image != GrafanaImage {
		t.Fatalf("expected image %q, got %q", GrafanaImage, cfg.Image)
	}

	if _, ok := cfg.External[5308]; !ok {
		t.Fatal("expected external port 5308")
	}
	if cfg.External[5308] != 3000 {
		t.Fatalf("expected 5308→3000, got 5308→%d", cfg.External[5308])
	}

	if cfg.Environment["GF_AUTH_ANONYMOUS_ENABLED"] != "true" {
		t.Fatal("missing GF_AUTH_ANONYMOUS_ENABLED")
	}

	if len(cfg.HostVolumeMounts) != 2 {
		t.Fatalf("expected 2 host volume mounts, got %d", len(cfg.HostVolumeMounts))
	}

	if len(cfg.MkdirPaths) != 2 {
		t.Fatalf("expected 2 MkdirPaths, got %d", len(cfg.MkdirPaths))
	}

	// Grafana runs as uid 472 inside the container and fails with
	// "GF_PATHS_DATA is not writable" unless the bind-mounted data
	// directory is owned by that uid. Two ExecStartPre chown commands
	// are required: one for the data dir and one for the provisioning
	// dir.
	if len(cfg.ExecStartPreExtra) != 2 {
		t.Fatalf("expected 2 ExecStartPreExtra entries, got %d", len(cfg.ExecStartPreExtra))
	}
	for _, cmd := range cfg.ExecStartPreExtra {
		if !strings.Contains(cmd, "chown -R 472:472") {
			t.Fatalf("expected chown -R 472:472 in ExecStartPreExtra, got %q", cmd)
		}
	}
}

func TestGrafanaGeneratesPackageUnits(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := GrafanaPackageConfig(btrfsBase, "nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	expectedSvcName := systemd.SystemServiceUnitName("monitoring-ui")
	if units.Service.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, units.Service.Name)
	}

	if units.NetworkController == nil {
		t.Fatal("grafana should have a network controller unit")
	}

	if len(units.Sockets) != 1 {
		t.Fatalf("expected 1 socket unit, got %d", len(units.Sockets))
	}

	svc := units.Service.Content
	if !strings.Contains(svc, GrafanaImage) {
		t.Fatalf("grafana unit should reference grafana image, got:\n%s", svc)
	}
	if !strings.Contains(svc, "GF_AUTH_ANONYMOUS_ENABLED=true") {
		t.Fatal("grafana unit should have env vars")
	}
	// The data directory must be chowned to the Grafana uid before the
	// container starts, otherwise Grafana aborts with "GF_PATHS_DATA is
	// not writable".
	if !strings.Contains(svc, "chown -R 472:472 "+filepath.Join(btrfsBase, "monitoring", "grafana-data")) {
		t.Fatalf("grafana unit should chown data dir to uid 472, got:\n%s", svc)
	}
	if !strings.Contains(svc, "chown -R 472:472 "+filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")) {
		t.Fatalf("grafana unit should chown provisioning dir to uid 472, got:\n%s", svc)
	}
}

func TestEnsureGrafanaStorage(t *testing.T) {
	btrfsBase := t.TempDir()
	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
		t.Fatalf("EnsureGrafanaStorage: %v", err)
	}

	// The mock records full paths (btrfsBase/<name>). Check that both
	// Grafana subvolumes were created on a fresh base.
	names := map[string]bool{}
	for _, fs := range ctrl.GetFilesystems() {
		names[fs.Name] = true
	}
	dataPath := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	provPath := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	if !names[dataPath] {
		t.Fatalf("expected subvolume %q to be created, got %v", dataPath, names)
	}
	if !names[provPath] {
		t.Fatalf("expected subvolume %q to be created, got %v", provPath, names)
	}
}

func TestEnsureGrafanaStorageIdempotent(t *testing.T) {
	btrfsBase := t.TempDir()
	// Pre-create the data directory on disk (simulating a previous run
	// where the path exists as either a subvolume or a plain directory).
	// EnsureGrafanaStorage must not attempt to create a subvolume on top
	// of an existing path, which would fail with "path already exists".
	existing := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	if err := os.MkdirAll(existing, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
		t.Fatalf("EnsureGrafanaStorage: %v", err)
	}

	// grafana-data was pre-existing, so the only NEW subvolume created
	// under the grafana prefix should be grafana-provisioning. (An
	// intermediate "monitoring" subvolume is also created by the btrfs
	// auto-nesting logic, which we ignore here.)
	dataPath := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	provPath := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	names := map[string]bool{}
	for _, fs := range ctrl.GetFilesystems() {
		names[fs.Name] = true
	}
	if names[dataPath] {
		t.Fatalf("grafana-data already existed on disk and must not be recreated as a subvolume, got %v", names)
	}
	if !names[provPath] {
		t.Fatalf("expected grafana-provisioning subvolume to be created, got %v", names)
	}
}

func TestEnsureGrafanaStorageNilStorage(t *testing.T) {
	// Passing nil storage must be a no-op (so callers can disable btrfs
	// subvolume creation and fall back to the plain ExecStartPre mkdir).
	if err := EnsureGrafanaStorage(nil, t.TempDir()); err != nil {
		t.Fatalf("nil storage should be a no-op: %v", err)
	}
}

func TestStartMonitoringUIGrafanaCreatesSubvolumes(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()

	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := StartMonitoringUI(t.Context(), sd, st, BackendGrafana, btrfsBase, "nc:test", networkStatePath); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	names := map[string]bool{}
	for _, fs := range ctrl.GetFilesystems() {
		names[fs.Name] = true
	}
	if !names[filepath.Join(btrfsBase, "monitoring", "grafana-data")] {
		t.Fatalf("expected grafana-data subvolume, got %v", names)
	}
	if !names[filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")] {
		t.Fatalf("expected grafana-provisioning subvolume, got %v", names)
	}

	// The generated unit must include the chown ExecStartPre lines.
	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	content := sd.InstalledUnits[svcUnit]
	if !strings.Contains(content, "chown -R 472:472") {
		t.Fatalf("expected chown ExecStartPre in unit, got:\n%s", content)
	}
}

func TestWriteGrafanaProvisioningFiles(t *testing.T) {
	btrfsBase := t.TempDir()

	if err := WriteGrafanaProvisioningFiles(btrfsBase); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	dsFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml")
	data, err := os.ReadFile(dsFile)
	if err != nil {
		t.Fatalf("read datasource: %v", err)
	}
	if !strings.Contains(string(data), "host.containers.internal:9090") {
		t.Fatal("datasource should point to prometheus via host gateway")
	}

	provFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboards", "default.yml")
	data, err = os.ReadFile(provFile)
	if err != nil {
		t.Fatalf("read dashboard provider: %v", err)
	}
	if !strings.Contains(string(data), "dashboard-json") {
		t.Fatal("provider should reference dashboard-json directory")
	}

	jsonDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboard-json")
	info, err := os.Stat(jsonDir)
	if err != nil {
		t.Fatalf("dashboard-json dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("dashboard-json should be a directory")
	}
}

func TestStartMonitoringUIUPlot(t *testing.T) {
	sd := systemd.InitMockManager()
	networkStatePath := t.TempDir()

	if err := StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), BackendUPlot, "", "nc:test", networkStatePath); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	content, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected unit %s to be installed", svcUnit)
	}
	if !strings.Contains(content, "socat") {
		t.Fatal("uPlot mode unit should use socat")
	}
}

func TestStartMonitoringUIGrafana(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()
	st := storage.InitBtrFSFromController(btrfsBase, storage.InitBtrFSMockController())

	if err := StartMonitoringUI(t.Context(), sd, st, BackendGrafana, btrfsBase, "nc:test", networkStatePath); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	content, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected service unit %s to be installed", svcUnit)
	}
	if !strings.Contains(content, GrafanaImage) {
		t.Fatal("grafana mode should use grafana image")
	}

	dsFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml")
	if _, err := os.Stat(dsFile); err != nil {
		t.Fatalf("grafana provisioning should be written: %v", err)
	}
}

func TestStartMonitoringUIInstallError(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.InstallUnitErr = os.ErrPermission

	err := StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), BackendUPlot, "", "nc:test", t.TempDir())
	if err == nil {
		t.Fatal("expected error when InstallUnit fails")
	}
}

func TestMonitoringUISystemServiceUPlot(t *testing.T) {
	svc := MonitoringUISystemService(BackendUPlot, "nc:test")

	if svc.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", svc.Key)
	}
	if svc.Image != "nc:test" {
		t.Fatalf("expected nc image for uplot, got %q", svc.Image)
	}
	if svc.Port != MonitoringExternalPort {
		t.Fatalf("expected port %q, got %q", MonitoringExternalPort, svc.Port)
	}
}

func TestMonitoringUISystemServiceGrafana(t *testing.T) {
	svc := MonitoringUISystemService(BackendGrafana, "")

	if svc.Image != GrafanaImage {
		t.Fatalf("expected grafana image, got %q", svc.Image)
	}
	if svc.Port != MonitoringExternalPort {
		t.Fatalf("expected port %q, got %q", MonitoringExternalPort, svc.Port)
	}
}

func TestMonitoringUIUnitIsSystemService(t *testing.T) {
	cfg := UPlotPackageConfig("nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	if !systemd.IsSystemServiceUnit(units.Service.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", units.Service.Name)
	}
}

func TestMonitoringUIGrafanaUnitIsSystemService(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := GrafanaPackageConfig(btrfsBase, "nc:test", "/run/state")
	units := systemd.GeneratePackageUnits(cfg)

	if !systemd.IsSystemServiceUnit(units.Service.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", units.Service.Name)
	}
}
