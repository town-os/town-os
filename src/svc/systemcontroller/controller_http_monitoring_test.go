// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestHTTPMonitoringStatusWithoutMonitoring(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	resp, err := c.getClient(context.TODO(), "monitoring/status")
	if err != nil {
		t.Fatalf("getClient: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHTTPMonitoringStatusUPlotBackend(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		ScrapeTargetsFunc: noScrapeTargets,
	})
	t.Cleanup(ts.Close)

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
	if !status.MonitoringUI {
		t.Fatal("expected monitoring-ui running")
	}
}

func TestHTTPMonitoringStatusMonitoringUIStopped(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	// The monitoring-ui unit is inactive (e.g. mid-restart after a
	// backend switch). The API must report MonitoringUI=false so the
	// settings UI can keep polling.
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "activating"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		ScrapeTargetsFunc: noScrapeTargets,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.MonitoringUI {
		t.Fatal("expected monitoring-ui not active")
	}
}

func TestHTTPMonitoringStatusGrafanaBackend(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendGrafana,
		ScrapeTargetsFunc: noScrapeTargets,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	status, err := c.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.Backend != monitoring.BackendGrafana {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendGrafana, status.Backend)
	}
	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if !status.Grafana {
		t.Fatal("expected grafana running")
	}
	if !status.NodeExporter {
		t.Fatal("expected node-exporter running")
	}
	if !status.MonitoringUI {
		t.Fatal("expected monitoring-ui running")
	}
}

func TestHTTPMonitoringStatusUsesTargetedUnitQuery(t *testing.T) {
	// Regression: /monitoring/status must query the three monitoring
	// units by name via GetUnitStates instead of ListUnits. A broad
	// ListUnits call forces systemd to stat every unit file on disk,
	// which on overlayfs hosts floods the kernel log with ESTALE
	// warnings when unrelated unit files are getting copied up.
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		ScrapeTargetsFunc: noScrapeTargets,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.MonitoringStatus(context.TODO()); err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	var foundTargeted bool
	for _, call := range sd.GetCalls() {
		if call.Method == "ListUnits" {
			t.Errorf("monitoringStatus still calls ListUnits; should use GetUnitStates")
		}
		if call.Method == "GetUnitStates" {
			foundTargeted = true
			names, ok := call.Args[0].([]string)
			if !ok {
				t.Fatalf("expected []string arg, got %T", call.Args[0])
			}
			want := map[string]bool{
				systemd.SystemServiceUnitName("prometheus"):    true,
				systemd.SystemServiceUnitName("monitoring-ui"): true,
				systemd.SystemServiceUnitName("node-exporter"): true,
			}
			if len(names) != len(want) {
				t.Fatalf("expected %d names, got %d: %v", len(want), len(names), names)
			}
			for _, n := range names {
				if !want[n] {
					t.Errorf("unexpected queried unit %q", n)
				}
			}
		}
	}
	if !foundTargeted {
		t.Fatal("expected GetUnitStates to be called")
	}
}

// noScrapeTargets is the scrape-health query for tests that are not about it.
//
// It is wired explicitly rather than left nil because nil means the REAL query,
// which connects to Prometheus's port in the host network namespace — a unit
// test must never depend on what is or is not listening there.
func noScrapeTargets(_ context.Context, _ monitoring.Ports) ([]monitoring.ScrapeTarget, error) {
	return nil, nil
}

// monitoringStatusServer builds a controller whose three monitoring units are
// active and whose scrape-health query is fetch.
func monitoringStatusServer(t *testing.T, fetch ScrapeTargetsFunc) *SystemdClient {
	t.Helper()

	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	ts := InitTestServer(ServerConfig{
		Storage:           storage.InitBtrFSMock(),
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		ScrapeTargetsFunc: fetch,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c
}

// TestHTTPMonitoringStatusReportsDownScrapeJobs is the whole point of the
// endpoint carrying targets at all.
//
// Every unit being active says nothing about whether Prometheus can scrape
// them: both metrics failures this box has shipped had every unit up, an empty
// `systemctl --failed`, and a dashboard drawing an empty chart — which is what
// an idle service looks like too. The target list is the only place the
// difference exists, and LastError is the only place the reason does.
func TestHTTPMonitoringStatusReportsDownScrapeJobs(t *testing.T) {
	c := monitoringStatusServer(t, func(_ context.Context, _ monitoring.Ports) ([]monitoring.ScrapeTarget, error) {
		return []monitoring.ScrapeTarget{
			{Job: monitoring.ControllerJobName, Instance: "127.0.0.1:5309", Health: monitoring.TargetHealthUp},
			{Job: monitoring.RolodexJobName, Instance: "127.0.0.2:9153", Health: monitoring.TargetHealthDown,
				LastError: "connection refused"},
			{Job: "node-exporter", Instance: "127.0.0.1:9100", Health: monitoring.TargetHealthUnknown},
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := c.MonitoringStatus(ctx)
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if len(status.ScrapeTargets) != 3 {
		t.Fatalf("expected 3 scrape targets, got %d: %+v", len(status.ScrapeTargets), status.ScrapeTargets)
	}
	if len(status.DownJobs) != 1 || status.DownJobs[0] != monitoring.RolodexJobName {
		t.Errorf("down_jobs = %v, want [%s]", status.DownJobs, monitoring.RolodexJobName)
	}
	// A target Prometheus has not reached yet is new, not broken — reporting it
	// as down would make every boot look like an outage for one interval.
	for _, target := range status.ScrapeTargets {
		if target.Job != monitoring.RolodexJobName {
			continue
		}
		if target.LastError == "" {
			t.Error("the failing target came back with no error; 'down' with no reason is not actionable")
		}
	}
	if status.ScrapeTargetsError != "" {
		t.Errorf("scrape_targets_error = %q on a successful query", status.ScrapeTargetsError)
	}
}

// TestHTTPMonitoringStatusSaysWhyItCouldNotAsk keeps the two answers apart: a
// Prometheus that could not be reached must not read as a Prometheus with
// nothing to report, which is the exact conflation this endpoint exists to end.
func TestHTTPMonitoringStatusSaysWhyItCouldNotAsk(t *testing.T) {
	c := monitoringStatusServer(t, func(_ context.Context, _ monitoring.Ports) ([]monitoring.ScrapeTarget, error) {
		return nil, errors.New("connection refused")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := c.MonitoringStatus(ctx)
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	if status.ScrapeTargetsError == "" {
		t.Error("a failed query reported no error; an empty target list would read as a healthy stack")
	}
	if len(status.ScrapeTargets) != 0 || len(status.DownJobs) != 0 {
		t.Errorf("a failed query still reported targets: %+v / %v", status.ScrapeTargets, status.DownJobs)
	}
}

// TestHTTPMonitoringStatusSkipsTargetsWhenPrometheusIsStopped: with Prometheus
// down, its unit state already says so, and a scrape error on top of that would
// read as a second, different fault.
func TestHTTPMonitoringStatusSkipsTargetsWhenPrometheusIsStopped(t *testing.T) {
	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.SystemServiceUnitName("prometheus"), ActiveState: "inactive"},
		{Name: systemd.SystemServiceUnitName("monitoring-ui"), ActiveState: "active"},
		{Name: systemd.SystemServiceUnitName("node-exporter"), ActiveState: "active"},
	}

	var asked bool
	ts := InitTestServer(ServerConfig{
		Storage:           storage.InitBtrFSMock(),
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		ScrapeTargetsFunc: func(_ context.Context, _ monitoring.Ports) ([]monitoring.ScrapeTarget, error) {
			asked = true
			return nil, nil
		},
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status, err := c.MonitoringStatus(ctx)
	if err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}
	if asked {
		t.Error("queried a stopped Prometheus for its targets")
	}
	if status.ScrapeTargetsError != "" {
		t.Errorf("scrape_targets_error = %q with Prometheus stopped; its unit state already says that", status.ScrapeTargetsError)
	}
}

func TestMockClientMonitoringStatus(t *testing.T) {
	m := InitMockClient()

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	if status.Prometheus {
		t.Fatal("expected prometheus not running in default mock")
	}
}

func TestMockClientMonitoringStatusCustomResponse(t *testing.T) {
	m := InitMockClient()
	m.MonitoringStatusResp = &monitoring.MonitoringStatus{
		Backend:    monitoring.BackendUPlot,
		Prometheus: true,
	}

	status, err := m.MonitoringStatus(context.TODO())
	if err != nil {
		t.Fatalf("MockClient.MonitoringStatus: %v", err)
	}

	if !status.Prometheus {
		t.Fatal("expected prometheus running")
	}
	if status.Backend != monitoring.BackendUPlot {
		t.Fatalf("expected backend %q, got %q", monitoring.BackendUPlot, status.Backend)
	}
}

func TestMockClientMonitoringStatusErrorInjection(t *testing.T) {
	m := InitMockClient()
	m.MonitoringStatusErr = errors.New("injected error")

	_, err := m.MonitoringStatus(context.TODO())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockClientMonitoringStatusCallLog(t *testing.T) {
	m := InitMockClient()

	if _, err := m.MonitoringStatus(context.TODO()); err != nil {
		t.Fatalf("MonitoringStatus: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Method != "MonitoringStatus" {
		t.Fatalf("expected method MonitoringStatus, got %q", calls[0].Method)
	}
}
