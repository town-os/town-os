// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// metricLine returns the value of the first sample line whose prefix matches,
// and whether one was found. It reads the scrape the way Prometheus does —
// line-oriented text — rather than reaching into the collector, so a change
// that breaks the wire format fails here.
func metricLine(scrape, prefix string) (string, bool) {
	for line := range strings.SplitSeq(scrape, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if after, found := strings.CutPrefix(line, prefix); found {
			return strings.TrimSpace(after), true
		}
	}
	return "", false
}

// TestMetricsEndpointServesExpositionFormat scrapes over real HTTP, through the
// real middleware chain, and checks the shape Prometheus requires: a HELP line,
// a TYPE line, and a sample, for a metric that is always present.
func TestMetricsEndpointServesExpositionFormat(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)
	ctx := context.Background()

	scrape, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	for _, want := range []string{
		"# HELP townos_up ",
		"# TYPE townos_up gauge",
		"townos_up 1",
		"# TYPE townos_start_time_seconds gauge",
	} {
		if !strings.Contains(scrape, want) {
			t.Errorf("scrape missing %q:\n%s", want, scrape)
		}
	}
}

// The unit gauges are the headline "what is running" question. A package unit
// must be counted under the state systemd reports, and a per-unit series must
// name it, so an operator can see WHICH service is down rather than only that
// one is.
func TestMetricsReportsPackageUnitStates(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	id := packages.PackageIdentity{Repo: "repo-a", Name: "metricsvc", Version: "1.0"}
	unit := systemd.UnitName(id.Repo, id.Name, id.Version)
	sd.Units = []systemd.UnitStatus{{Name: unit, ActiveState: "active", SubState: "running"}}

	c := initSystemControllerSystemdTest(t, sd, id)

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	if got, ok := metricLine(scrape, `townos_package_units{state="active"}`); !ok || got != "1" {
		t.Errorf("active package units = %q (found=%v), want 1:\n%s", got, ok, scrape)
	}
	// Every bucket is emitted even at zero: a gauge that vanishes at zero is
	// indistinguishable from one the box stopped reporting, so "no failed
	// units" would look exactly like "unit collection is broken".
	if got, ok := metricLine(scrape, `townos_package_units{state="failed"}`); !ok || got != "0" {
		t.Errorf("failed bucket = %q (found=%v), want an explicit 0:\n%s", got, ok, scrape)
	}
	if !strings.Contains(scrape, `townos_package_unit_active{unit="`+unit+`"} 1`) {
		t.Errorf("per-unit series missing for %s:\n%s", unit, scrape)
	}
}

// A failed unit is the case somebody alerts on, so it must land in the failed
// bucket rather than being folded into "not active".
func TestMetricsReportsFailedUnits(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	id := packages.PackageIdentity{Repo: "repo-a", Name: "brokensvc", Version: "1.0"}
	unit := systemd.UnitName(id.Repo, id.Name, id.Version)
	sd.Units = []systemd.UnitStatus{{Name: unit, ActiveState: "failed", SubState: "failed"}}

	c := initSystemControllerSystemdTest(t, sd, id)

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if got, ok := metricLine(scrape, `townos_package_units{state="failed"}`); !ok || got != "1" {
		t.Errorf("failed package units = %q (found=%v), want 1:\n%s", got, ok, scrape)
	}
	if !strings.Contains(scrape, `townos_package_unit_active{unit="`+unit+`"} 0`) {
		t.Errorf("failed unit should report active=0:\n%s", scrape)
	}
}

// A unit left behind by an uninstalled package must not be counted, or the box
// looks like it is running something it is not — the same filter the status
// ping applies.
func TestMetricsExcludesUnitsOfUninstalledPackages(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	installed := packages.PackageIdentity{Repo: "repo-a", Name: "present", Version: "1.0"}
	sd.Units = []systemd.UnitStatus{
		{Name: systemd.UnitName("repo-a", "present", "1.0"), ActiveState: "active"},
		{Name: systemd.UnitName("repo-a", "leftover", "1.0"), ActiveState: "active"},
	}

	c := initSystemControllerSystemdTest(t, sd, installed)

	scrape, err := c.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if got, ok := metricLine(scrape, `townos_package_units{state="active"}`); !ok || got != "1" {
		t.Errorf("active package units = %q, want only the installed one:\n%s", got, scrape)
	}
	if strings.Contains(scrape, "leftover") {
		t.Errorf("a leftover unit was reported:\n%s", scrape)
	}
}

// The audit counter is what answers "how often is an audited action failing"
// without a database round trip per scrape. It must move when an audited action
// runs, and it must distinguish the outcome.
func TestMetricsCountsAuditEvents(t *testing.T) {
	t.Parallel()
	c := initSystemControllerSettingsTest(t)
	ctx := context.Background()

	// A setting write is audited; a read is not.
	if err := c.SetSetting(ctx, "locale", "en-US"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	scrape, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if !strings.Contains(scrape, "# TYPE townos_audit_events_total counter") {
		t.Errorf("audit counter family missing:\n%s", scrape)
	}
	got, ok := metricLine(scrape, `townos_audit_events_total{result="success"}`)
	if !ok || got == "0" {
		t.Errorf("audited action did not move the counter (got %q, found=%v):\n%s", got, ok, scrape)
	}
}

// Requests are tallied by method and status class, and the tally must include
// requests that were refused — a 403 nobody can explain is exactly what the
// counter exists to surface.
func TestMetricsCountsHTTPRequests(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)
	ctx := context.Background()

	if _, err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	scrape, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	if !strings.Contains(scrape, "# TYPE townos_http_requests_total counter") {
		t.Errorf("request counter family missing:\n%s", scrape)
	}
	if got, ok := metricLine(scrape, `townos_http_requests_total{method="GET",status="2xx"}`); !ok || got == "0" {
		t.Errorf("GET 2xx not tallied (got %q, found=%v):\n%s", got, ok, scrape)
	}
}

// Prometheus polls this every 15s. If the scrape counted itself, the request
// counter would be dominated by monitoring rather than by use — and the number
// would climb on a box nobody was touching.
func TestMetricsScrapeDoesNotCountItself(t *testing.T) {
	t.Parallel()
	c := initSystemControllerTest(t)
	ctx := context.Background()

	// Take a baseline, then scrape repeatedly. If a scrape counted itself, the
	// GET tally would climb with nothing else touching the box.
	if _, err := c.GetMetrics(ctx); err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	baseline, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	before, _ := metricLine(baseline, `townos_http_requests_total{method="GET",status="2xx"}`)

	for range 3 {
		if _, err := c.GetMetrics(ctx); err != nil {
			t.Fatalf("GetMetrics: %v", err)
		}
	}
	after, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	got, _ := metricLine(after, `townos_http_requests_total{method="GET",status="2xx"}`)

	if got != before {
		t.Errorf("GET tally moved from %q to %q across scrapes alone — the scrape is counting itself", before, got)
	}
}

// Two scrapes with nothing changed in between must be byte-identical apart from
// values that legitimately move. An unstable ordering makes two captures
// impossible to diff, which is how these get read during an incident.
func TestMetricsOrderingIsStable(t *testing.T) {
	t.Parallel()

	sd := systemd.InitMockManager()
	id := packages.PackageIdentity{Repo: "repo-a", Name: "stable", Version: "1.0"}
	sd.Units = []systemd.UnitStatus{{Name: systemd.UnitName(id.Repo, id.Name, id.Version), ActiveState: "active"}}
	c := initSystemControllerSystemdTest(t, sd, id)
	ctx := context.Background()

	first, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}
	second, err := c.GetMetrics(ctx)
	if err != nil {
		t.Fatalf("GetMetrics: %v", err)
	}

	firstNames := metricNames(first)
	secondNames := metricNames(second)
	if strings.Join(firstNames, ",") != strings.Join(secondNames, ",") {
		t.Errorf("metric ordering changed between scrapes:\n%v\n%v", firstNames, secondNames)
	}
}

// metricNames returns the family names in the order they appear.
func metricNames(scrape string) []string {
	var names []string
	for line := range strings.SplitSeq(scrape, "\n") {
		if after, found := strings.CutPrefix(line, "# TYPE "); found {
			names = append(names, strings.Fields(after)[0])
		}
	}
	return names
}
