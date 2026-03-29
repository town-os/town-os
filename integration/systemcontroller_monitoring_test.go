// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
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

	monUnitName := systemd.UnitName(
		monitoring.MonitoringRepo,
		monitoring.MonitoringPackageName,
		monitoring.MonitoringVersion,
	)
	sd.Units = []systemd.UnitStatus{
		{Name: monUnitName, ActiveState: "active"},
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
	nePort := findFreePort(t)
	t.Logf("node-exporter port: %s", nePort)

	sd := systemd.NewManager()
	ctx := context.Background()

	if err := monitoring.StartNodeExporter(ctx, sd, nePort); err != nil {
		t.Fatalf("StartNodeExporter: %v", err)
	}

	unitName := systemd.SystemServiceUnitName("node-exporter")
	t.Cleanup(func() {
		_ = sd.SetStatus(ctx, unitName, systemd.Stop) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(ctx, unitName)            //nolint:errcheck // best-effort cleanup
	})

	// Verify the unit was installed.
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

func TestMonitoringPackageReconcileUnits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create repositories.json.
	repoData, err := json.Marshal([]any{})
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "repositories.json"), repoData, 0600); err != nil {
		t.Fatalf("write repos: %v", err)
	}

	// Write and install the monitoring package.
	if err := monitoring.EnsureMonitoringPackage(dir, monitoring.BackendUPlot, ""); err != nil {
		t.Fatalf("EnsureMonitoringPackage: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	if _, err := monitoring.InstallMonitoringPackage(inst); err != nil {
		t.Fatalf("InstallMonitoringPackage: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	sd := systemd.InitMockManager()

	if err := systemcontroller.Reconcile(t.Context(), systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                storage.InitBtrFSMock(),
		Systemd:                sd,
		BtrfsBasePath:          t.TempDir(),
		NetworkControllerImage: "localhost/town-os-networkcontroller:local",
		NetworkStatePath:       t.TempDir(),
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify NC unit was installed and has port 5308.
	ncUnit := systemd.NetworkControllerUnitName(
		monitoring.MonitoringRepo, monitoring.MonitoringPackageName, monitoring.MonitoringVersion,
	)
	ncContent, ok := sd.InstalledUnits[ncUnit]
	if !ok {
		t.Fatalf("expected NC unit %s to be installed", ncUnit)
	}
	if !strings.Contains(ncContent, "5308") {
		t.Fatalf("NC unit should expose port 5308, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "podman network create") {
		t.Fatalf("NC unit should create network, got:\n%s", ncContent)
	}

	// Verify service unit was installed.
	svcUnit := systemd.UnitName(
		monitoring.MonitoringRepo, monitoring.MonitoringPackageName, monitoring.MonitoringVersion,
	)
	svcContent, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected service unit %s to be installed", svcUnit)
	}

	// Both NC and service must create the network (boot race safety).
	expectedNet := systemd.NetworkName(
		monitoring.MonitoringRepo, monitoring.MonitoringPackageName, monitoring.MonitoringVersion,
	)
	if !strings.Contains(svcContent, "podman network create "+expectedNet) {
		t.Fatalf("service unit should idempotently create network, got:\n%s", svcContent)
	}
	if !strings.Contains(ncContent, "podman network create "+expectedNet) {
		t.Fatalf("NC unit should create network, got:\n%s", ncContent)
	}

	// Service must NOT rm -f network — NC owns cleanup.
	if strings.Contains(svcContent, "podman network rm") {
		t.Fatalf("service should not rm -f network when NC exists, got:\n%s", svcContent)
	}

	// Verify prometheus image and command args.
	if !strings.Contains(svcContent, monitoring.PrometheusImage) {
		t.Fatalf("service should reference prometheus image, got:\n%s", svcContent)
	}
	if !strings.Contains(svcContent, "--config.file=/etc/prometheus/prometheus.yml") {
		t.Fatalf("service should include prometheus command, got:\n%s", svcContent)
	}
}
