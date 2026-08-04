// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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
	cfg := monitoring.NodeExporterUnitConfig(monitoring.Ports{NodeExporter: nePort})
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

// upstreamDiskstatsDefaultExclude mirrors the node_exporter default for
// `--collector.diskstats.device-exclude`. It is the pattern our override
// in NodeExporterUnitConfig is supposed to be narrower than; this test
// uses it to find at least one real device in /proc/diskstats that
// would have been filtered out before the override existed, then confirms
// that node_exporter now emits metrics for it.
var upstreamDiskstatsDefaultExclude = regexp.MustCompile(
	`^(ram|loop|fd|(h|s|v|xv)d[a-z]|nvme\d+n\d+p)\d+$`,
)

// TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices starts a
// real node_exporter container against the system bus, scrapes its
// /metrics endpoint, and confirms at least one device excluded by the
// upstream default (a partition or a loop device) appears in
// node_disk_read_bytes_total. Regression coverage for the empty Disk I/O
// panel: /town-os lives on exactly those shapes of device, and without
// the --collector.diskstats.device-exclude override in
// NodeExporterUnitConfig the dashboard queries silently return no series.
func TestMonitoringNodeExporterEmitsDiskMetricsForFilteredDevices(t *testing.T) {
	t.Parallel()

	candidateDevices := diskstatsDevicesMatchingUpstreamDefault(t)
	if len(candidateDevices) == 0 {
		t.Skip("no partition or loop device in /proc/diskstats to verify against")
	}
	t.Logf("candidate filtered-by-default devices in /proc/diskstats: %v", candidateDevices)

	nePort := findFreePort(t)
	t.Logf("node-exporter port: %s", nePort)

	sd := systemd.NewManager()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	suffix := strconv.FormatUint(rand.Uint64(), 36)
	cfg := monitoring.NodeExporterUnitConfig(monitoring.Ports{NodeExporter: nePort})
	cfg.Key = "node-exporter-test-" + suffix
	uf := systemd.GenerateSystemServiceUnit(cfg)
	unitName := uf.Name

	if err := sd.InstallUnit(ctx, unitName, uf.Content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Stop)    //nolint:errcheck // best-effort cleanup
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Disable) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(cleanupCtx, unitName)              //nolint:errcheck // best-effort cleanup
	})
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := sd.SetStatus(ctx, unitName, systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	metricsURL := fmt.Sprintf("http://127.0.0.1:%s/metrics", nePort)
	body := scrapeNodeExporterMetrics(ctx, t, metricsURL)

	seenDevices := parseDiskReadBytesDevices(body)
	t.Logf("devices emitted by node_exporter: %v", seenDevices)

	intersect := intersection(seenDevices, candidateDevices)
	if len(intersect) == 0 {
		t.Fatalf("node_exporter emitted no node_disk_read_bytes_total series for any "+
			"upstream-default-excluded device. candidates=%v emitted=%v flag=%q",
			candidateDevices, seenDevices, monitoring.DiskstatsDeviceExclude)
	}
}

// diskstatsDevicesMatchingUpstreamDefault returns the set of devices in
// /proc/diskstats whose names match the upstream node_exporter default
// exclude regex — exactly the devices we want node_exporter to emit
// metrics for now that we've narrowed the exclude.
func diskstatsDevicesMatchingUpstreamDefault(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		t.Skipf("/proc/diskstats unavailable: %v", err)
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[2]
		if upstreamDiskstatsDefaultExclude.MatchString(name) {
			out = append(out, name)
		}
	}
	return out
}

// scrapeNodeExporterMetrics polls metricsURL until it returns a non-empty
// body or the context expires. node_exporter's systemd unit reports
// active as soon as `podman run` forks, but the container still needs to
// bind the listen port, so a short poll is the right shape here rather
// than a single request.
func scrapeNodeExporterMetrics(ctx context.Context, t *testing.T, metricsURL string) string {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("node_exporter did not serve metrics before deadline (last error: %v)", lastErr)
			return ""
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, metricsURL, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Logf("close response body: %v", closeErr)
		}
		if readErr != nil {
			lastErr = readErr
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if len(body) == 0 {
			lastErr = errors.New("empty body")
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return string(body)
	}
}

var diskReadDeviceLine = regexp.MustCompile(`^node_disk_read_bytes_total\{[^}]*device="([^"]+)"`)

// parseDiskReadBytesDevices extracts the distinct `device` label values
// from node_disk_read_bytes_total series in a Prometheus text-format
// exposition body.
func parseDiskReadBytesDevices(body string) []string {
	seen := map[string]struct{}{}
	for line := range strings.SplitSeq(body, "\n") {
		m := diskReadDeviceLine.FindStringSubmatch(line)
		if m != nil {
			seen[m[1]] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// intersection returns the elements present in both slices.
func intersection(a, b []string) []string {
	in := make(map[string]struct{}, len(b))
	for _, v := range b {
		in[v] = struct{}{}
	}
	var out []string
	for _, v := range a {
		if _, ok := in[v]; ok {
			out = append(out, v)
		}
	}
	return out
}

func TestMonitoringPrometheusSystemServiceUnit(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	if err := monitoring.StartPrometheus(t.Context(), sd, btrfsBase, monitoring.Ports{}); err != nil {
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

	// Host-net system service: no NC unit.
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-network.service") {
			t.Fatalf("prometheus must not install an NC unit, got %s", name)
		}
	}

	if !strings.Contains(promContent, monitoring.PrometheusImage) {
		t.Fatalf("prometheus unit should reference prometheus image, got:\n%s", promContent)
	}
	if !strings.Contains(promContent, "prometheus-config:/etc/prometheus") {
		t.Fatalf("prometheus unit should mount config volume, got:\n%s", promContent)
	}
	if !strings.Contains(promContent, "127.0.0.1:9090") {
		t.Fatalf("prometheus unit should listen on the host loopback, got:\n%s", promContent)
	}
}

func TestMonitoringUIUPlotSystemServiceUnit(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()

	if err := monitoring.StartMonitoringUI(t.Context(), sd, storage.InitBtrFSMock(), monitoring.BackendUPlot, "", "nc:test", nil, monitoring.Ports{}); err != nil {
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
	// Prometheus is loopback-only on the host netns; the socat (also host
	// netns) reaches it at 127.0.0.1, not the old cross-network gateway.
	if !strings.Contains(uiContent, "TCP:127.0.0.1:9090") {
		t.Fatalf("socat must target 127.0.0.1:9090 (loopback Prometheus), got:\n%s", uiContent)
	}
	if strings.Contains(uiContent, "host.containers.internal") {
		t.Fatalf("socat must not use the host gateway hairpin, got:\n%s", uiContent)
	}

	// Host-net system service: no NC unit.
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-network.service") {
			t.Fatalf("uPlot monitoring-ui must not install an NC unit, got %s", name)
		}
	}
}

func TestMonitoringUIGrafanaSystemServiceUnit(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	ctrl := storage.InitBtrFSMockController()
	st := storage.InitBtrFSFromController(btrfsBase, ctrl)

	if err := monitoring.StartMonitoringUI(t.Context(), sd, st, monitoring.BackendGrafana, btrfsBase, "nc:test", nil, monitoring.Ports{}); err != nil {
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
	// Grafana binds :5308 directly (host netns), so the datasource can only
	// reach the loopback Prometheus — never the old host gateway hairpin.
	if !strings.Contains(uiContent, "GF_SERVER_HTTP_PORT="+monitoring.MonitoringExternalPort) {
		t.Fatalf("grafana unit should bind :5308 directly via GF_SERVER_HTTP_PORT, got:\n%s", uiContent)
	}

	// The unit must chown the data directory to the Grafana uid or
	// Grafana aborts with "GF_PATHS_DATA is not writable". The chown is
	// non-recursive by design: Grafana creates nested files as 472 itself,
	// so only the top-level mount needs ownership.
	if !strings.Contains(uiContent, "chown 472:472") {
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

	// Host-net system service: no NC unit.
	for name := range sd.InstalledUnits {
		if strings.Contains(name, "-network.service") {
			t.Fatalf("grafana monitoring-ui must not install an NC unit, got %s", name)
		}
	}
}

func TestMonitoringPrometheusRealStart(t *testing.T) {
	t.Parallel()
	sd := systemd.NewManager()
	ctx := context.Background()
	btrfsBase := t.TempDir()

	// Unique per-test key + btrfsBase (t.TempDir) so multiple copies of
	// this test can run in parallel against the shared system bus
	// without clobbering each other's unit names or bind-mount paths.
	suffix := strconv.FormatUint(rand.Uint64(), 36)
	cfg := monitoring.PrometheusUnitConfig(btrfsBase, monitoring.Ports{})
	cfg.Key = "prometheus-test-" + suffix
	uf := systemd.GenerateSystemServiceUnit(cfg)
	unitName := uf.Name

	// Install and enable the service so it stays loaded in systemd's memory
	// (multi-user.target Wants= prevents GC). We do not start anything —
	// starting would pull the real Prometheus image and run the container.
	// The integration value of this test is verifying that real systemd
	// accepts the unit file format we generate.
	if err := sd.InstallUnit(ctx, unitName, uf.Content); err != nil {
		t.Fatalf("InstallUnit service: %v", err)
	}
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable service: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Stop)    //nolint:errcheck // best-effort cleanup
		_ = sd.SetStatus(cleanupCtx, unitName, systemd.Disable) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(cleanupCtx, unitName)              //nolint:errcheck // best-effort cleanup
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

// TestMonitoringCleanupUnitsRemovesRealUnit exercises monitoring.CleanupUnits
// against real systemd: it installs and enables a uniquely-named unit, then
// confirms CleanupUnits stops, disables, and removes it end-to-end. This is the
// mechanism CleanupLegacyMonitoringUnits relies on to tear down the obsolete NC
// / socket units on an in-place upgrade. A unique per-run name keeps concurrent
// test-full runs from colliding (IRON RULE); the fixed legacy names themselves
// are covered by the mock unit test, since installing those shared global names
// here would race across parallel runs.
func TestMonitoringCleanupUnitsRemovesRealUnit(t *testing.T) {
	t.Parallel()
	sd := systemd.NewManager()
	ctx := context.Background()

	suffix := strconv.FormatUint(rand.Uint64(), 36)
	unitName := "town-os-system--monitoring-cleanup-test-" + suffix + ".service"
	unitPath := "/etc/systemd/system/" + unitName
	content := "[Unit]\nDescription=Town OS monitoring cleanup test\n\n" +
		"[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/bin/true\n\n" +
		"[Install]\nWantedBy=multi-user.target\n"

	if err := sd.InstallUnit(ctx, unitName, content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, unitName, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	t.Cleanup(func() {
		// CleanupUnits should already have removed it; belt-and-suspenders in
		// case the assertion below fails.
		_ = sd.SetStatus(context.Background(), unitName, systemd.Stop) //nolint:errcheck // best-effort cleanup
		_ = sd.UninstallUnit(context.Background(), unitName)           //nolint:errcheck // best-effort cleanup
	})

	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("expected unit file %s to exist before cleanup: %v", unitPath, err)
	}

	monitoring.CleanupUnits(ctx, sd, []string{unitName})

	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %s removed after cleanup, stat err = %v", unitPath, err)
	}
}
