// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package monitoring

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
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

// TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices pins the
// diskstats device-exclude override so node_exporter keeps emitting
// metrics for the exact device shapes our Disk I/O panel queries:
// partitions (sda3, nvme0n1p3), whole disks, and loop devices (used by
// the integration-test btrfs loopback). The upstream default excludes
// all of those and leaves the panel empty.
func TestNodeExporterUnitConfigDiskstatsExcludeAllowsRealDevices(t *testing.T) {
	cfg := NodeExporterUnitConfig("")

	var flag string
	for _, cmd := range cfg.Command {
		if rest, ok := strings.CutPrefix(cmd, "--collector.diskstats.device-exclude="); ok {
			flag = rest
		}
	}
	if flag == "" {
		t.Fatalf("expected --collector.diskstats.device-exclude in node-exporter command, got %v", cfg.Command)
	}
	if flag != DiskstatsDeviceExclude {
		t.Fatalf("diskstats device-exclude drifted: got %q want %q", flag, DiskstatsDeviceExclude)
	}

	re, err := regexp.Compile(flag)
	if err != nil {
		t.Fatalf("device-exclude regex does not compile: %v", err)
	}

	keep := []string{"sda", "sda3", "nvme0n1", "nvme0n1p3", "loop0", "dm-0", "vda1", "xvda2"}
	for _, d := range keep {
		if re.MatchString(d) {
			t.Errorf("device-exclude %q should not match real device %q", flag, d)
		}
	}

	drop := []string{"ram0", "ram15", "fd0"}
	for _, d := range drop {
		if !re.MatchString(d) {
			t.Errorf("device-exclude %q should match pseudo-device %q", flag, d)
		}
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

// argsContainNetHost reports whether a podman run arg slice contains the
// adjacent "--net", "host" pair.
func argsContainNetHost(args []string) bool {
	for i, a := range args {
		if a == "--net" && i+1 < len(args) && args[i+1] == "host" {
			return true
		}
	}
	return false
}

// commandListenAddr returns the value of the --web.listen-address flag in a
// node_exporter / prometheus command slice, or "".
func commandListenAddr(command []string) string {
	for _, c := range command {
		if rest, ok := strings.CutPrefix(c, "--web.listen-address="); ok {
			return rest
		}
	}
	return ""
}

// TestNodeExporterListensOnLoopback pins node-exporter to 127.0.0.1: it runs in
// the host netns (required for host metrics) but must be private — only
// Prometheus (also host netns) scrapes it over the loopback, nothing on the LAN.
func TestNodeExporterListensOnLoopback(t *testing.T) {
	if got := commandListenAddr(NodeExporterUnitConfig("").Command); got != "127.0.0.1:"+NodeExporterPort {
		t.Fatalf("node-exporter must listen on 127.0.0.1:%s (private), got %q", NodeExporterPort, got)
	}
}

// --- Prometheus tests (host-networked system service) ---

func TestPrometheusUnitConfig(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := PrometheusUnitConfig(btrfsBase)

	if cfg.Key != "prometheus" {
		t.Fatalf("expected key prometheus, got %q", cfg.Key)
	}
	if cfg.Image != PrometheusImage {
		t.Fatalf("expected image %q, got %q", PrometheusImage, cfg.Image)
	}

	// Runs in the host netns (so it can scrape node-exporter over loopback).
	if !argsContainNetHost(cfg.Args) {
		t.Fatalf("expected --net host in args, got %v", cfg.Args)
	}

	// Binds loopback only (private): never :9090 / 0.0.0.0.
	if got := commandListenAddr(cfg.Command); got != "127.0.0.1:"+PrometheusPort {
		t.Fatalf("prometheus must listen on 127.0.0.1:%s (private), got %q", PrometheusPort, got)
	}

	// Config + data bind mounts reference the btrfs base.
	argStr := strings.Join(cfg.Args, " ")
	if !strings.Contains(argStr, "prometheus-config:/etc/prometheus:ro") {
		t.Fatalf("expected config bind mount, got %v", cfg.Args)
	}
	if !strings.Contains(argStr, "prometheus-data:/prometheus") {
		t.Fatalf("expected data bind mount, got %v", cfg.Args)
	}
	if len(cfg.VolumeDirs) != 2 {
		t.Fatalf("expected 2 VolumeDirs, got %d", len(cfg.VolumeDirs))
	}

	// Data dir must be chowned to prometheus uid:gid before start.
	dataDir := filepath.Join(btrfsBase, "monitoring", "prometheus-data")
	wantChown := "/bin/chown 65534:65534 " + dataDir
	found := false
	for _, pre := range cfg.ExecStartPre {
		if pre == wantChown {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ExecStartPre %q, got %v", wantChown, cfg.ExecStartPre)
	}
}

func TestPrometheusGeneratesSystemServiceUnit(t *testing.T) {
	btrfsBase := t.TempDir()
	uf := systemd.GenerateSystemServiceUnit(PrometheusUnitConfig(btrfsBase))

	expectedSvcName := systemd.SystemServiceUnitName("prometheus")
	if uf.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, uf.Name)
	}

	svc := uf.Content
	if !strings.Contains(svc, PrometheusImage) {
		t.Fatal("unit should reference prometheus image")
	}
	if !strings.Contains(svc, "Restart=always") {
		t.Fatal("unit should restart always")
	}
	if !strings.Contains(svc, "prometheus-config:/etc/prometheus:ro") {
		t.Fatal("unit should have config bind mount")
	}
	dataDir := filepath.Join(btrfsBase, "monitoring", "prometheus-data")
	if !strings.Contains(svc, "ExecStartPre=/bin/chown 65534:65534 "+dataDir+"\n") {
		t.Fatalf("unit should chown data dir, got:\n%s", svc)
	}
	// A host-net system service must not create a podman network.
	if strings.Contains(svc, "network create") {
		t.Fatalf("host-net prometheus must not create a podman network, got:\n%s", svc)
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
	// Both targets are on the host loopback now (all monitoring runs --net host).
	if !strings.Contains(content, "localhost:9100") {
		t.Fatal("config should scrape node-exporter on default port 9100 over loopback")
	}
	if !strings.Contains(content, "localhost:9090") {
		t.Fatal("config should scrape prometheus itself")
	}
	if strings.Contains(content, "host.containers.internal") {
		t.Fatalf("config must not use the host gateway hairpin, got:\n%s", content)
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

	if !strings.Contains(string(data), "localhost:19100") {
		t.Fatal("config should use custom node exporter port 19100 over loopback")
	}
}

func TestStartPrometheus(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	if err := StartPrometheus(t.Context(), sd, btrfsBase, ""); err != nil {
		t.Fatalf("StartPrometheus: %v", err)
	}

	svcUnit := systemd.SystemServiceUnitName("prometheus")
	content, ok := sd.InstalledUnits[svcUnit]
	if !ok {
		t.Fatalf("expected service unit %s to be installed", svcUnit)
	}
	if !strings.Contains(content, "127.0.0.1:"+PrometheusPort) {
		t.Fatalf("prometheus should listen on loopback, got:\n%s", content)
	}

	// Host-net system service: no NC unit, no socket units.
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-network.service") {
			t.Fatalf("prometheus must not install an NC unit, got %s", name)
		}
		if strings.Contains(name, ".socket") {
			t.Fatalf("prometheus must not install a socket unit, got %s", name)
		}
	}

	configFile := filepath.Join(btrfsBase, "monitoring", "prometheus-config", "prometheus.yml")
	if _, err := os.Stat(configFile); err != nil {
		t.Fatalf("prometheus.yml should exist: %v", err)
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
	uf := systemd.GenerateSystemServiceUnit(PrometheusUnitConfig(t.TempDir()))
	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("prometheus unit %q should be a system service unit", uf.Name)
	}
}

// --- Monitoring UI tests (host-networked system service) ---

func TestUPlotUnitConfig(t *testing.T) {
	cfg := UPlotUnitConfig("nc:test")

	if cfg.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", cfg.Key)
	}
	if cfg.Image != "nc:test" {
		t.Fatalf("expected image nc:test, got %q", cfg.Image)
	}
	if !cfg.PullNever {
		t.Fatal("uPlot socat image is local; expected PullNever")
	}
	if !argsContainNetHost(cfg.Args) {
		t.Fatalf("expected --net host in args, got %v", cfg.Args)
	}

	cmdStr := strings.Join(cfg.Command, " ")
	if !strings.Contains(cmdStr, "socat") {
		t.Fatalf("expected socat in command, got %v", cfg.Command)
	}
	if !strings.Contains(cmdStr, "TCP-LISTEN:5308") {
		t.Fatalf("expected socat on port 5308, got %v", cfg.Command)
	}
	// Prometheus is loopback-only on the host netns, so the socat (also host
	// netns) reaches it at 127.0.0.1 — never the old cross-network gateway.
	if !strings.Contains(cmdStr, "TCP:127.0.0.1:9090") {
		t.Fatalf("expected socat target 127.0.0.1:9090, got %v", cfg.Command)
	}
	if strings.Contains(cmdStr, "host.containers.internal") {
		t.Fatalf("socat must not use the host gateway hairpin, got %v", cfg.Command)
	}
}

func TestUPlotDefaultSocatImage(t *testing.T) {
	cfg := UPlotUnitConfig("")
	if cfg.Image != DefaultSocatImage {
		t.Fatalf("empty ncImage should default to %q, got %q", DefaultSocatImage, cfg.Image)
	}
}

func TestUPlotGeneratesSystemServiceUnit(t *testing.T) {
	uf := systemd.GenerateSystemServiceUnit(UPlotUnitConfig("nc:test"))

	expectedSvcName := systemd.SystemServiceUnitName("monitoring-ui")
	if uf.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, uf.Name)
	}
	if !strings.Contains(uf.Content, "socat") {
		t.Fatal("uPlot unit should use socat")
	}
	if strings.Contains(uf.Content, "network create") {
		t.Fatalf("host-net monitoring-ui must not create a podman network, got:\n%s", uf.Content)
	}
}

func TestGrafanaUnitConfig(t *testing.T) {
	btrfsBase := t.TempDir()
	cfg := GrafanaUnitConfig(btrfsBase)

	if cfg.Key != "monitoring-ui" {
		t.Fatalf("expected key monitoring-ui, got %q", cfg.Key)
	}
	if cfg.Image != GrafanaImage {
		t.Fatalf("expected image %q, got %q", GrafanaImage, cfg.Image)
	}
	if !argsContainNetHost(cfg.Args) {
		t.Fatalf("expected --net host in args, got %v", cfg.Args)
	}

	argStr := strings.Join(cfg.Args, " ")
	// Grafana binds :5308 directly (host netns) via GF_SERVER_HTTP_PORT,
	// replacing the old -p 5308:3000 publish.
	if !strings.Contains(argStr, "GF_SERVER_HTTP_PORT="+MonitoringExternalPort) {
		t.Fatalf("expected GF_SERVER_HTTP_PORT=%s, got %v", MonitoringExternalPort, cfg.Args)
	}
	if !strings.Contains(argStr, "GF_AUTH_ANONYMOUS_ENABLED=true") {
		t.Fatal("missing GF_AUTH_ANONYMOUS_ENABLED")
	}
	if !strings.Contains(argStr, "grafana-provisioning:/etc/grafana/provisioning") {
		t.Fatalf("expected provisioning bind mount, got %v", cfg.Args)
	}
	if !strings.Contains(argStr, "grafana-data:/var/lib/grafana") {
		t.Fatalf("expected data bind mount, got %v", cfg.Args)
	}
	if len(cfg.VolumeDirs) != 2 {
		t.Fatalf("expected 2 VolumeDirs, got %d", len(cfg.VolumeDirs))
	}

	// Only the writable data dir is chowned (non-recursively); the read-only
	// provisioning dir is never chowned.
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	provDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	wantChown := "/bin/chown 472:472 " + dataDir
	foundChown := false
	for _, pre := range cfg.ExecStartPre {
		if pre == wantChown {
			foundChown = true
		}
		if strings.Contains(pre, provDir) {
			t.Fatalf("provisioning dir must not be chowned, got %q", pre)
		}
	}
	if !foundChown {
		t.Fatalf("expected ExecStartPre %q, got %v", wantChown, cfg.ExecStartPre)
	}
}

func TestGrafanaGeneratesSystemServiceUnit(t *testing.T) {
	btrfsBase := t.TempDir()
	uf := systemd.GenerateSystemServiceUnit(GrafanaUnitConfig(btrfsBase))

	expectedSvcName := systemd.SystemServiceUnitName("monitoring-ui")
	if uf.Name != expectedSvcName {
		t.Fatalf("expected service unit %q, got %q", expectedSvcName, uf.Name)
	}

	svc := uf.Content
	if !strings.Contains(svc, GrafanaImage) {
		t.Fatalf("grafana unit should reference grafana image, got:\n%s", svc)
	}
	if !strings.Contains(svc, "GF_AUTH_ANONYMOUS_ENABLED=true") {
		t.Fatal("grafana unit should have env vars")
	}
	// The data directory must be chowned (non-recursively) to the Grafana uid
	// before the container starts, otherwise Grafana aborts with
	// "GF_PATHS_DATA is not writable". Only the top directory is chowned —
	// Grafana creates its own subdirectories inside as uid 472.
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	if !strings.Contains(svc, "ExecStartPre=/bin/chown 472:472 "+dataDir+"\n") {
		t.Fatalf("grafana unit should chown (non-recursive) data dir to 472:472, got:\n%s", svc)
	}
	if strings.Contains(svc, "chown -R") {
		t.Fatalf("grafana unit should NOT recursively chown, got:\n%s", svc)
	}
	// The provisioning dir is read-only to Grafana — no chown for it at all.
	provDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning")
	if strings.Contains(svc, "chown 472:472 "+provDir) {
		t.Fatalf("grafana unit should NOT chown provisioning dir, got:\n%s", svc)
	}
	// A host-net system service must not create a podman network.
	if strings.Contains(svc, "network create") {
		t.Fatalf("host-net grafana must not create a podman network, got:\n%s", svc)
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
	// Passing nil storage must fall back to plain directories and still
	// create the paths on disk so the subsequent WriteGrafanaProvisioningFiles
	// call and the systemd ExecStartPre mkdir have something valid to
	// operate on.
	btrfsBase := t.TempDir()
	if err := EnsureGrafanaStorage(nil, btrfsBase); err != nil {
		t.Fatalf("nil storage should fall back to plain dirs: %v", err)
	}

	for _, suffix := range []string{"grafana-data", "grafana-provisioning"} {
		path := filepath.Join(btrfsBase, "monitoring", suffix)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected %s to exist after fallback: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s should be a directory", path)
		}
	}
}

func TestEnsureGrafanaStorageSubvolumeErrorFallsBack(t *testing.T) {
	// When the storage layer refuses to create a subvolume (for example
	// when the parent directory already exists as a plain directory, as
	// it does on the live system after StartPrometheus has run its
	// ExecStartPre mkdir), EnsureGrafanaStorage must fall back to
	// os.MkdirAll so Grafana can still boot.
	btrfsBase := t.TempDir()
	// Pre-create the parent "monitoring" directory so callers see the
	// exact on-disk layout that the systemcontroller sees at boot.
	if err := os.MkdirAll(filepath.Join(btrfsBase, "monitoring"), 0755); err != nil {
		t.Fatalf("mkdir monitoring parent: %v", err)
	}

	st := &failingStorage{err: errors.New("simulated btrfs failure")}

	if err := EnsureGrafanaStorage(st, btrfsBase); err != nil {
		t.Fatalf("EnsureGrafanaStorage should fall back, got: %v", err)
	}

	for _, suffix := range []string{"grafana-data", "grafana-provisioning"} {
		path := filepath.Join(btrfsBase, "monitoring", suffix)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected fallback dir %s to exist: %v", path, err)
		}
	}
}

// failingStorage is a minimal storage.Storage that fails CreateFilesystem
// with an injected error. All other methods are unused in these tests.
type failingStorage struct {
	err error
}

func (f *failingStorage) CreateFilesystem(_ storage.Filesystem) error { return f.err }
func (f *failingStorage) ModifyFilesystem(_ string, _ storage.Filesystem) error {
	return storage.ErrUnimplemented
}
func (f *failingStorage) RemoveFilesystem(_ string) error { return storage.ErrUnimplemented }
func (f *failingStorage) ListFilesystems(_ string) ([]storage.Filesystem, error) {
	return nil, storage.ErrUnimplemented
}
func (f *failingStorage) FilesystemNames(_ string) ([]string, error) {
	return nil, storage.ErrUnimplemented
}
func (f *failingStorage) RenameFilesystem(_, _ string) error   { return storage.ErrUnimplemented }
func (f *failingStorage) SnapshotFilesystem(_, _ string) error { return storage.ErrUnimplemented }
func (f *failingStorage) DiskUsage() (storage.DiskUsage, error) {
	return storage.DiskUsage{}, storage.ErrUnimplemented
}

func TestStartMonitoringUIGrafanaCreatesSubvolumes(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := StartMonitoringUI(t.Context(), sd, st, BackendGrafana, btrfsBase, "nc:test", nil); err != nil {
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

	// The generated unit must include a non-recursive chown for the
	// writable grafana-data bind-mount, declared via HostVolumeMount.UID/GID.
	svcUnit := systemd.SystemServiceUnitName("monitoring-ui")
	content := sd.InstalledUnits[svcUnit]
	dataDir := filepath.Join(btrfsBase, "monitoring", "grafana-data")
	if !strings.Contains(content, "ExecStartPre=/bin/chown 472:472 "+dataDir+"\n") {
		t.Fatalf("expected non-recursive chown for grafana-data, got:\n%s", content)
	}
	if strings.Contains(content, "chown -R") {
		t.Fatalf("grafana unit should not contain recursive chown, got:\n%s", content)
	}
}

func TestWriteGrafanaProvisioningFiles(t *testing.T) {
	btrfsBase := t.TempDir()

	if err := WriteGrafanaProvisioningFiles(btrfsBase, []string{"sda3", "nvme0n1p3"}); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	dsFile := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "datasources", "prometheus.yml")
	data, err := os.ReadFile(dsFile)
	if err != nil {
		t.Fatalf("read datasource: %v", err)
	}
	// Grafana runs --net host, so its datasource reaches the loopback-only
	// Prometheus at 127.0.0.1:9090, not the old host gateway hairpin.
	if !strings.Contains(string(data), "127.0.0.1:9090") {
		t.Fatal("datasource should point to prometheus on the host loopback")
	}
	if strings.Contains(string(data), "host.containers.internal") {
		t.Fatal("datasource must not use the host gateway hairpin")
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

	// The Town OS Overview dashboard JSON must be written into
	// dashboard-json so Grafana's file provider loads it at startup.
	// Without this file, the default /d/town-os-overview/... URL
	// returns 404 and the monitoring iframe renders as blank.
	dashboardFile := filepath.Join(jsonDir, "town-os-overview.json")
	data, err = os.ReadFile(dashboardFile) //nolint:gosec // test reads a file under t.TempDir()
	if err != nil {
		t.Fatalf("town-os-overview.json should exist: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"uid": "town-os-overview"`) {
		t.Fatalf("dashboard should declare uid town-os-overview, got:\n%s", content[:min(len(content), 200)])
	}
	if !strings.Contains(content, `"title": "Town OS Overview"`) {
		t.Fatal("dashboard should have the Town OS Overview title")
	}
	if !strings.Contains(content, `device=~\"sda3|nvme0n1p3\"`) {
		t.Fatalf("dashboard should embed disk device regex from caller, got:\n%s", content[:min(len(content), 1000)])
	}
}

func TestStartMonitoringUIUPlot(t *testing.T) {
	sd := systemd.InitMockManager()

	if err := StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), BackendUPlot, "", "nc:test", nil); err != nil {
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
	if !strings.Contains(content, "TCP:127.0.0.1:9090") {
		t.Fatalf("uPlot socat should target the loopback Prometheus, got:\n%s", content)
	}
}

func TestStartMonitoringUIGrafana(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	st := storage.InitBtrFSFromController(btrfsBase, storage.InitBtrFSMockController())

	if err := StartMonitoringUI(t.Context(), sd, st, BackendGrafana, btrfsBase, "nc:test", nil); err != nil {
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

	err := StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), BackendUPlot, "", "nc:test", nil)
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
	uf := systemd.GenerateSystemServiceUnit(UPlotUnitConfig("nc:test"))
	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", uf.Name)
	}
}

func TestMonitoringUIGrafanaUnitIsSystemService(t *testing.T) {
	uf := systemd.GenerateSystemServiceUnit(GrafanaUnitConfig(t.TempDir()))
	if !systemd.IsSystemServiceUnit(uf.Name) {
		t.Fatalf("monitoring-ui unit %q should be a system service unit", uf.Name)
	}
}
