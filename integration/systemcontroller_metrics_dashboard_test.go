// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initDashboardMetricsTest builds a controller with every manager the metrics
// collector reads from.
//
// The other metrics fixtures deliberately wire a subset — each of those tests
// is about one collector — but this one is about the dashboard, and the
// collector skips any section whose manager is nil. A partial fixture would
// make a panel's metric look absent when it is only unwired, which is the
// failure this test exists to tell apart.
func initDashboardMetricsTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	// One installed package with a running unit, and one system service unit:
	// townos_package_unit_active and townos_system_unit_active are per-unit
	// series, and Render drops a family with no samples entirely, so a box
	// with nothing installed exports neither.
	id := packages.PackageIdentity{Repo: "repo-a", Name: "dashsvc", Version: "1.0"}
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{id}

	sd := systemd.InitMockManager()
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.UnitName(id.Repo, id.Name, id.Version), ActiveState: "active", SubState: "running"},
		{Name: systemd.SystemServiceUnitPrefix + "prometheus.service", ActiveState: "active", SubState: "running"},
	}

	db, err := account.OpenDB(t.Context(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() {
		if cerr := db.Close(); cerr != nil {
			t.Errorf("db.Close: %v", cerr)
		}
	})

	mgr, err := account.InitManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	sessMgr, err := account.InitSessionManager(t.Context(), db, mgr, []byte("test-signing-key-for-sessions-32"))
	if err != nil {
		t.Fatalf("InitSessionManager: %v", err)
	}
	auditMgr, err := account.InitAuditManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitAuditManager: %v", err)
	}
	settingsMgr, err := account.InitSettingsManager(t.Context(), db)
	if err != nil {
		t.Fatalf("InitSettingsManager: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFS("/town-os"),
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		AccountMgr:     mgr,
		SessionMgr:     sessMgr,
		AuditMgr:       auditMgr,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if _, err := c.CreateAccount(t.Context(), "admin", "adminpass", "admin@test.com", "555-0000", "Admin", true); err != nil {
		t.Fatalf("bootstrap CreateAccount: %v", err)
	}
	resp, err := c.Authenticate(t.Context(), "admin", "adminpass")
	if err != nil {
		t.Fatalf("bootstrap Authenticate: %v", err)
	}
	c.Token = resp.Token

	return c
}

// TestControllerDashboardMetricsAreServed scrapes the controller over real
// HTTP and asserts every family the controller dashboard's panels query is
// present in the exposition.
//
// This is the end of the chain the unit tests hold together in pieces: the
// Grafana panels and the uPlot panels are checked against each other and
// against ControllerDashboardMetrics in src/monitoring, but nothing there
// proves the controller emits any of it. A panel naming a family the box does
// not export renders an empty chart — indistinguishable, on a dashboard, from
// a box with nothing to report.
func TestControllerDashboardMetricsAreServed(t *testing.T) {
	t.Parallel()
	c := initDashboardMetricsTest(t)
	ctx := context.Background()

	// A setting write is audited, and that is what gives the audit counter a
	// sample: Render omits a family with no samples rather than emitting it at
	// zero, so an untouched counter is absent from the scrape entirely.
	if err := c.SetSetting(ctx, "locale", "en-US"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	scrape, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	for _, family := range monitoring.ControllerDashboardMetrics() {
		// The trailing space matters: without it townos_accounts would be
		// satisfied by townos_accounts_granted's TYPE line.
		if !strings.Contains(scrape, "# TYPE "+family+" ") {
			t.Errorf("the controller dashboard queries %s, which this scrape does not export:\n%s", family, scrape)
		}
	}
}

// TestControllerDashboardProcessPanelsHaveReadings asserts the panels about
// the controller process itself — CPU, memory, concurrency — carry numbers a
// chart can draw, over a real scrape rather than a direct collector call.
//
// Family presence is not enough here: every one of these is read from the
// kernel or the runtime through a path that can fail quietly (/proc unreadable
// under a hardened container, a statm layout that does not parse), and the
// collector's answer to a failed reading is to omit the family. A zero, or an
// absence, means the reading did not happen — this process demonstrably has
// goroutines, a heap and open descriptors while it is being asked.
func TestControllerDashboardProcessPanelsHaveReadings(t *testing.T) {
	t.Parallel()
	c := initDashboardMetricsTest(t)

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	for _, family := range []string{
		"townos_goroutines",
		"townos_memory_heap_bytes",
		"townos_memory_rss_bytes",
		"townos_open_files",
	} {
		raw, ok := metricLine(scrape, family+" ")
		if !ok {
			t.Errorf("scrape has no %s sample; the controller process panels would be empty:\n%s", family, scrape)
			continue
		}
		value, parseErr := strconv.ParseFloat(raw, 64)
		if parseErr != nil {
			t.Errorf("%s = %q, which is not a number: %v", family, raw, parseErr)
			continue
		}
		if value <= 0 {
			t.Errorf("%s = %v, want a positive reading from a live process", family, value)
		}
	}

	// CPU time may legitimately round to zero on a fast box, so it is checked
	// for presence and type rather than magnitude — as a gauge, Prometheus
	// would draw the cumulative climb instead of rating it.
	if !strings.Contains(scrape, "# TYPE townos_process_cpu_seconds_total counter") {
		t.Errorf("CPU time is not exported as a counter:\n%s", scrape)
	}
	if _, ok := metricLine(scrape, "townos_http_requests_in_flight "); !ok {
		t.Errorf("scrape has no in-flight sample; the gauge must report zero rather than vanish:\n%s", scrape)
	}
}

// TestControllerDashboardLatencyHasBothHalves asserts the two counters the API
// Latency panel divides are both present and labelled identically.
//
// The panel is a ratio, and a ratio is the one panel shape that can be broken
// by a change to either side alone: relabel the seconds counter and the
// division matches nothing, so the panel renders empty on a box that is
// serving fine.
func TestControllerDashboardLatencyHasBothHalves(t *testing.T) {
	t.Parallel()
	c := initDashboardMetricsTest(t)

	// Any authenticated call moves both counters; SetSetting is the one the
	// sibling test already relies on being audited.
	if err := c.SetSetting(context.Background(), "locale", "en-US"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	labels := func(family string) []string {
		var out []string
		for line := range strings.SplitSeq(scrape, "\n") {
			if !strings.HasPrefix(line, family+"{") {
				continue
			}
			if _, rest, ok := strings.Cut(line, "{"); ok {
				if inner, _, closed := strings.Cut(rest, "}"); closed {
					out = append(out, inner)
				}
			}
		}
		slices.Sort(out)
		return out
	}

	requests := labels("townos_http_requests_total")
	seconds := labels("townos_http_request_seconds_total")
	if len(requests) == 0 {
		t.Fatalf("no request samples in the scrape:\n%s", scrape)
	}
	if !slices.Equal(requests, seconds) {
		t.Errorf("the latency panel divides two differently-labelled counters:\nrequests: %v\nseconds:  %v", requests, seconds)
	}
}

// TestControllerDashboardPanelsHaveDataToDraw goes one step further than
// family presence: it asserts the per-unit health series actually name the
// units on the box. The family can be present and still carry nothing an
// operator recognises if the label were dropped or renamed, and the Service
// Health panel legends entirely on that label.
func TestControllerDashboardPanelsHaveDataToDraw(t *testing.T) {
	t.Parallel()
	c := initDashboardMetricsTest(t)

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	pkgUnit := systemd.UnitName("repo-a", "dashsvc", "1.0")
	for _, want := range []string{
		`townos_package_unit_active{unit="` + pkgUnit + `"}`,
		`townos_system_unit_active{unit="` + systemd.SystemServiceUnitPrefix + `prometheus.service"}`,
	} {
		if _, ok := metricLine(scrape, want); !ok {
			t.Errorf("scrape has no %s series; the Service Health panel legends on that label:\n%s", want, scrape)
		}
	}
}
