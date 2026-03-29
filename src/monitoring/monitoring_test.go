// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestMonitoringPackageIdentity(t *testing.T) {
	id := MonitoringPackageIdentity()
	if id.Repo != MonitoringRepo {
		t.Fatalf("expected repo %q, got %q", MonitoringRepo, id.Repo)
	}
	if id.Name != MonitoringPackageName {
		t.Fatalf("expected name %q, got %q", MonitoringPackageName, id.Name)
	}
	if id.Version != MonitoringVersion {
		t.Fatalf("expected version %q, got %q", MonitoringVersion, id.Version)
	}
}

func TestGenerateManifestUPlot(t *testing.T) {
	manifest := GenerateManifest(BackendUPlot, "")

	if !strings.Contains(manifest, PrometheusImage) {
		t.Fatal("uplot manifest should contain prometheus image")
	}
	if !strings.Contains(manifest, "5308") {
		t.Fatal("uplot manifest should expose port 5308")
	}
	if !strings.Contains(manifest, "host.containers.internal:9100") {
		t.Fatal("uplot manifest should scrape node-exporter via host gateway")
	}
	if strings.Contains(manifest, GrafanaImage) {
		t.Fatal("uplot manifest should not contain grafana image")
	}
}

func TestGenerateManifestUPlotCustomNodeExporterPort(t *testing.T) {
	manifest := GenerateManifest(BackendUPlot, "19100")
	if !strings.Contains(manifest, "host.containers.internal:19100") {
		t.Fatal("manifest should use custom node exporter port")
	}
}

func TestGenerateManifestGrafana(t *testing.T) {
	manifest := GenerateManifest(BackendGrafana, "")

	if !strings.Contains(manifest, GrafanaImage) {
		t.Fatal("grafana manifest should contain grafana image")
	}
	if !strings.Contains(manifest, "5308") {
		t.Fatal("grafana manifest should expose port 5308")
	}
	if !strings.Contains(manifest, "GF_AUTH_ANONYMOUS_ENABLED") {
		t.Fatal("grafana manifest should include anonymous auth config")
	}
}

func TestGenerateManifestDefaultBackend(t *testing.T) {
	manifest := GenerateManifest("", "")
	if !strings.Contains(manifest, PrometheusImage) {
		t.Fatal("empty backend should default to uplot (prometheus)")
	}
}

func TestEnsureMonitoringPackage(t *testing.T) {
	repoBase := t.TempDir()

	if err := EnsureMonitoringPackage(repoBase, BackendUPlot, ""); err != nil {
		t.Fatalf("EnsureMonitoringPackage: %v", err)
	}

	pkgFile := filepath.Join(repoBase, MonitoringRepo, packages.PackagesDir, MonitoringPackageName, MonitoringVersion+".yaml")
	data, err := os.ReadFile(pkgFile)
	if err != nil {
		t.Fatalf("read package file: %v", err)
	}

	if !strings.Contains(string(data), PrometheusImage) {
		t.Fatal("written package should contain prometheus image")
	}
}

func TestEnsureMonitoringPackageOverwrites(t *testing.T) {
	repoBase := t.TempDir()

	// Write uplot first.
	if err := EnsureMonitoringPackage(repoBase, BackendUPlot, ""); err != nil {
		t.Fatalf("EnsureMonitoringPackage (uplot): %v", err)
	}

	// Overwrite with grafana.
	if err := EnsureMonitoringPackage(repoBase, BackendGrafana, ""); err != nil {
		t.Fatalf("EnsureMonitoringPackage (grafana): %v", err)
	}

	pkgFile := filepath.Join(repoBase, MonitoringRepo, packages.PackagesDir, MonitoringPackageName, MonitoringVersion+".yaml")
	data, err := os.ReadFile(pkgFile)
	if err != nil {
		t.Fatalf("read package file: %v", err)
	}

	if !strings.Contains(string(data), GrafanaImage) {
		t.Fatal("overwritten package should contain grafana image")
	}
}

func TestInstallMonitoringPackage(t *testing.T) {
	repoBase := t.TempDir()

	// Must write manifest first.
	if err := EnsureMonitoringPackage(repoBase, BackendUPlot, ""); err != nil {
		t.Fatalf("EnsureMonitoringPackage: %v", err)
	}

	inst := packages.NewInstallManager(repoBase)

	installed, err := InstallMonitoringPackage(inst)
	if err != nil {
		t.Fatalf("InstallMonitoringPackage: %v", err)
	}
	if !installed {
		t.Fatal("expected package to be newly installed")
	}

	// Second call should be a no-op.
	installed, err = InstallMonitoringPackage(inst)
	if err != nil {
		t.Fatalf("InstallMonitoringPackage (2nd): %v", err)
	}
	if installed {
		t.Fatal("expected package already installed")
	}
}

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

func TestGenerateManifestIsValidYAML(t *testing.T) {
	for _, backend := range []string{BackendUPlot, BackendGrafana} {
		manifest := GenerateManifest(backend, "")

		// Verify it parses as valid YAML.
		var raw map[string]interface{}
		if err := yaml.Unmarshal([]byte(manifest), &raw); err != nil {
			t.Fatalf("backend %s: invalid YAML: %v", backend, err)
		}

		if _, ok := raw["image"]; !ok {
			t.Fatalf("backend %s: expected image field", backend)
		}
	}
}
