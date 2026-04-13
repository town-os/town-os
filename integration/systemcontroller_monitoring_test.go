// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestMonitoringStatusDisabledByDefault(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)

	// When MonitoringBackend is empty, status returns {"status":"disabled"}.
	// The client decodes it as an empty MonitoringStatus (no backend).
	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != "" {
		t.Fatalf("expected empty backend, got %q", status.Backend)
	}
	if status.Prometheus {
		t.Fatal("expected prometheus not running")
	}
	if status.NodeExporter {
		t.Fatal("expected node-exporter not running")
	}
}

func TestMonitoringStatusUPlotBackend(t *testing.T) {
	t.Parallel()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != monitoring.BackendUPlot {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendUPlot, status.Backend)
	}
	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if !status.NodeExporter {
		t.Fatal("expected node-exporter running")
	}
}

func TestMonitoringNodeExporterRealStart(t *testing.T) {
	t.Parallel()
	nePort := findFreePort(t)
	t.Logf("node-exporter port: %s", nePort)

	sd := systemd.NewManager()
	ctx := context.Background()

	// Unique per-test key so multiple copies of this test can run in
	// parallel against the shared system bus without clobbering each
	// other's unit state. The production key "node-exporter" is only
	// used by the real boot path in main.go.
	suffix := strconv.FormatUint(rand.Uint64(), 36)
	cfg := monitoring.NodeExporterUnitConfig(nePort)
	cfg.Key = "node-exporter-test-" + suffix
	uf := systemd.GenerateSystemServiceUnit(cfg)
	unitName := uf.Name

	if err := sd.InstallUnit(ctx, unitName, uf.Content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Stop)    //nolint:errcheck // best-effort cleanup
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Disable) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(cleanupCtx, unitName)              //nolint:errcheck // best-effort cleanup
	})

	// Verify the unit was installed and loaded by real systemd.
	units, err := sd.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == unitName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %s in unit list", unitName)
	}
}

func TestMonitoringPrometheusPackageUnits(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()

	if err := monitoring.StartPrometheus(t.Context(), sd, btrfsBase, "", "nc:test", networkStatePath); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	promUnit := systemd.SystemServiceUnitName("prometheus")
	promContent, ok := sd.InstalledUnits[promUnit]
	if !ok {
		t.Fatalf("expected prometheus unit %s to be installed", promUnit)
	}
	if !systemd.IsSystemServiceUnit(promUnit) {
		t.Fatalf("prometheus unit should be a system service unit")
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

	if !strings.Contains(promContent, monitoring.PrometheusImage) {
		t.Fatalf("prometheus unit should reference prometheus image, got:\n%s", promContent)
	}
	if !strings.Contains(promContent, "prometheus-config:/etc/prometheus") {
		t.Fatalf("prometheus unit should mount config volume, got:\n%s", promContent)
	}
}

func TestMonitoringUIUPlotPackageUnits(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	networkStatePath := t.TempDir()

	if err := monitoring.StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), monitoring.BackendUPlot, "", "nc:test", networkStatePath, nil); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	uiUnit := systemd.SystemServiceUnitName("monitoring-ui")
	uiContent, ok := sd.InstalledUnits[uiUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui unit %s to be installed", uiUnit)
	}

	if !systemd.IsSystemServiceUnit(uiUnit) {
		t.Fatalf("monitoring-ui unit should be a system service unit")
	}
	if !strings.Contains(uiContent, "socat") {
		t.Fatalf("uplot monitoring-ui unit should use socat, got:\n%s", uiContent)
	}
	if !strings.Contains(uiContent, "5308") {
		t.Fatalf("monitoring-ui unit should expose port 5308, got:\n%s", uiContent)
	}
	if !strings.Contains(uiContent, "TCP:host.containers.internal:9090") {
		t.Fatalf("socat must target host.containers.internal:9090 so the forwarder can reach Prometheus on the host, got:\n%s", uiContent)
	}
	if strings.Contains(uiContent, "TCP:127.0.0.1:9090") {
		t.Fatalf("socat must not target 127.0.0.1:9090 (unreachable from inside the monitoring-ui container network), got:\n%s", uiContent)
	}

	// NC unit should be installed.
	ncInstalled := false
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "monitoring-ui-network") {
			ncInstalled = true
			break
		}
	}
	if !ncInstalled {
		t.Fatal("expected NC unit to be installed for uPlot monitoring-ui")
	}
}

func TestMonitoringUIGrafanaPackageUnits(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()
	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := monitoring.StartMonitoringUI(t.Context(), sd, st, monitoring.BackendGrafana, btrfsBase, "nc:test", networkStatePath, nil); err != nil {
		t.Fatalf("StartMonitoringUI: %v", err)
	}

	uiUnit := systemd.SystemServiceUnitName("monitoring-ui")
	uiContent, ok := sd.InstalledUnits[uiUnit]
	if !ok {
		t.Fatalf("expected monitoring-ui unit %s to be installed", uiUnit)
	}

	if !strings.Contains(uiContent, monitoring.GrafanaImage) {
		t.Fatalf("grafana monitoring-ui unit should use grafana image, got:\n%s", uiContent)
	}

	// The unit must chown the data directory to the Grafana uid or
	// Grafana aborts with "GF_PATHS_DATA is not writable".
	if !strings.Contains(uiContent, "chown -R 472:472") {
		t.Fatalf("grafana monitoring-ui unit should chown data dir to uid 472, got:\n%s", uiContent)
	}

	// Btrfs subvolumes for Grafana's data and provisioning directories
	// must have been created via the storage interface. The mock records
	// full joined paths (btrfsBase/<name>).
	names := map[string]bool{}
	for _, fs := range ctrl.GetFilesystems() {
		names[fs.Name] = true
	}
	dataPath := btrfsBase + "/monitoring/grafana-data"
	provPath := btrfsBase + "/monitoring/grafana-provisioning"
	if !names[dataPath] {
		t.Fatalf("expected %s subvolume to be created, got %v", dataPath, names)
	}
	if !names[provPath] {
		t.Fatalf("expected %s subvolume to be created, got %v", provPath, names)
	}

	// NC unit should be installed.
	ncInstalled := false
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "monitoring-ui-network") {
			ncInstalled = true
			break
		}
	}
	if !ncInstalled {
		t.Fatal("expected NC unit to be installed for grafana")
	}
}

func TestMonitoringPrometheusRealStart(t *testing.T) {
	t.Parallel()
	sd := systemd.NewManager()
	ctx := context.Background()
	btrfsBase := t.TempDir()
	networkStatePath := t.TempDir()
	ncImage := "localhost/town-os-networkcontroller:local"

	// Unique per-test key + btrfsBase (t.TempDir) so multiple copies of
	// this test can run in parallel against the shared system bus
	// without clobbering each other's unit names or bind-mount paths.
	suffix := strconv.FormatUint(rand.Uint64(), 36)
	cfg := monitoring.PrometheusPackageConfig(btrfsBase, ncImage, networkStatePath)
	cfg.SystemServiceKey = "prometheus-test-" + suffix
	unitSet := systemd.GeneratePackageUnits(cfg)
	unitName := unitSet.Service.Name

	// Install service, NC, and socket units and enable the service so
	// it stays loaded in systemd's memory (multi-user.target Wants=
	// prevents GC). We do not start anything — starting would pull
	// the real Prometheus image and run the container. The integration
	// value of this test is verifying that real systemd accepts the
	// unit file format we generate.
	if err := sd.InstallUnit(ctx, unitName, unitSet.Service.Content); err != nil {
		t.Fatalf("InstallUnit service: %v", err)
	}
	if unitSet.NetworkController != nil {
		if err := sd.InstallUnit(ctx, unitSet.NetworkController.Name, unitSet.NetworkController.Content); err != nil {
			t.Fatalf("InstallUnit NC: %v", err)
		}
	}
	for _, sock := range unitSet.Sockets {
		if err := sd.InstallUnit(ctx, sock.Name, sock.Content); err != nil {
			t.Fatalf("InstallUnit socket %s: %v", sock.Name, err)
		}
	}
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable service: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Stop)    //nolint:errcheck // best-effort cleanup
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Disable) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(cleanupCtx, unitName)              //nolint:errcheck // best-effort cleanup
		if unitSet.NetworkController != nil {
			_ = sd.SetStatus(cleanupCtx, unitSet.NetworkController.Name, systemd.Stop) //nolint:errcheck // best-effort cleanup
			_ = sd.UninstallUnit(cleanupCtx, unitSet.NetworkController.Name)           //nolint:errcheck // best-effort cleanup
		}
		for _, sock := range unitSet.Sockets {
			_ = sd.SetStatus(cleanupCtx, sock.Name, systemd.Stop) //nolint:errcheck // best-effort cleanup
			_ = sd.UninstallUnit(cleanupCtx, sock.Name)           //nolint:errcheck // best-effort cleanup
		}
	})

	units, err := sd.ListUnits(ctx)
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == unitName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %s in unit list", unitName)
	}
}
