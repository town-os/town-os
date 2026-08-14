package monitoring

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestControllerDashboardPanels walks the built controller dashboard and
// asserts the properties a panel needs to render at all: an object-form
// datasource ref with the pinned uid (Grafana 13+ silently shows "No data"
// for the legacy string form), a non-empty expression, a unique refId per
// panel, and a grid position that does not overlap its neighbour.
func TestControllerDashboardPanels(t *testing.T) {
	t.Parallel()

	raw := ControllerDashboard()
	if strings.Contains(raw, `"datasource": "Prometheus"`) {
		t.Fatal("controller dashboard contains a legacy string-form datasource ref; Grafana 13 cannot resolve it")
	}
	doc := parseDashboard(t, raw)

	if len(doc.Panels) != len(controllerPanels()) {
		t.Fatalf("rendered %d panels, spec declares %d", len(doc.Panels), len(controllerPanels()))
	}

	occupied := map[[2]int]string{}
	ids := map[int]bool{}
	titles := map[string]bool{}
	for i, p := range doc.Panels {
		if p.Title == "" {
			t.Errorf("panel %d has no title", i)
		}
		// Titles are the key the uPlot twin is matched on, both by the
		// frontend tests and by an operator switching backends, so a
		// duplicate is not cosmetic.
		if titles[p.Title] {
			t.Errorf("two panels are titled %q", p.Title)
		}
		titles[p.Title] = true

		if !p.Transparent {
			t.Errorf("panel %q is not transparent; it will not blend with the iframe background", p.Title)
		}
		if p.FieldConfig.Defaults.Unit == "" {
			t.Errorf("panel %q has no unit; values render as bare floats", p.Title)
		}
		if p.FieldConfig.Overrides == nil {
			t.Errorf("panel %q has a null overrides list; Grafana fails schema migration on null", p.Title)
		}
		if ids[p.ID] {
			t.Errorf("panel %q reuses id %d", p.Title, p.ID)
		}
		ids[p.ID] = true

		key := [2]int{p.GridPos.X, p.GridPos.Y}
		if prev, ok := occupied[key]; ok {
			t.Errorf("panel %q overlaps %q at x=%d y=%d", p.Title, prev, p.GridPos.X, p.GridPos.Y)
		}
		occupied[key] = p.Title
		if p.GridPos.W != dashboardPanelWidth || p.GridPos.H != dashboardPanelHeight {
			t.Errorf("panel %q sized %dx%d, want %dx%d", p.Title, p.GridPos.W, p.GridPos.H, dashboardPanelWidth, dashboardPanelHeight)
		}

		if len(p.Targets) == 0 {
			t.Errorf("panel %q has no targets", p.Title)
		}
		refIDs := map[string]bool{}
		for j, tgt := range p.Targets {
			if tgt.Datasource == nil {
				t.Errorf("panel %q target %d has no datasource object", p.Title, j)
				continue
			}
			if tgt.Datasource.Type != "prometheus" || tgt.Datasource.UID != GrafanaDatasourceUID {
				t.Errorf("panel %q target %d datasource = %+v, want prometheus/%s", p.Title, j, *tgt.Datasource, GrafanaDatasourceUID)
			}
			if tgt.Expr == "" {
				t.Errorf("panel %q target %d has an empty expression", p.Title, j)
			}
			if tgt.LegendFormat == "" {
				t.Errorf("panel %q target %d has no legendFormat; the series renders unlabelled", p.Title, j)
			}
			if refIDs[tgt.RefID] {
				t.Errorf("panel %q reuses refId %q", p.Title, tgt.RefID)
			}
			refIDs[tgt.RefID] = true
		}
	}
}

// TestControllerDashboardPinsTheBinaryHealthAxis pins the Service Health
// panel to 0..1. The metric is a boolean per unit, and an autoscaled binary
// series draws a wholly healthy box as noise around 1.0 — the panel would
// look alarming precisely when nothing is wrong.
func TestControllerDashboardPinsTheBinaryHealthAxis(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		if p.Title != "Service Health" {
			continue
		}
		lo, hi := p.FieldConfig.Defaults.Min, p.FieldConfig.Defaults.Max
		if lo == nil || hi == nil {
			t.Fatalf("Service Health autoscales (min=%v max=%v), want 0..1", lo, hi)
		}
		if *lo != 0 || *hi != 1 {
			t.Fatalf("Service Health axis = %v..%v, want 0..1", *lo, *hi)
		}
		return
	}
	t.Fatal("controller dashboard has no Service Health panel")
}

// TestControllerDashboardPinsTheDiskFillAxis pins the fill panel to 0..100.
// The whole point of the panel is that the slope is comparable between boxes
// and between weeks; autoscaled, a climb from 4% to 5% draws exactly like one
// from 90% to 99%.
func TestControllerDashboardPinsTheDiskFillAxis(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		if p.Title != "Town OS Disk Fill" {
			continue
		}
		lo, hi := p.FieldConfig.Defaults.Min, p.FieldConfig.Defaults.Max
		if lo == nil || hi == nil {
			t.Fatalf("Town OS Disk Fill autoscales (min=%v max=%v), want 0..100", lo, hi)
		}
		if *lo != 0 || *hi != 100 {
			t.Fatalf("Town OS Disk Fill axis = %v..%v, want 0..100", *lo, *hi)
		}
		if !strings.Contains(p.Targets[0].Expr, "townos_disk_total_bytes") {
			t.Errorf("fill is not measured against the filesystem size:\n%s", p.Targets[0].Expr)
		}
		return
	}
	t.Fatal("controller dashboard has no Town OS Disk Fill panel")
}

// TestControllerDashboardCPUAxisIsUncapped is the mirror of the test above: a
// process can use more than one core, so capping CPU at 100 would clip exactly
// the runaway the panel exists to show.
func TestControllerDashboardCPUAxisIsUncapped(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		if p.Title != "Controller CPU" {
			continue
		}
		if ceiling := p.FieldConfig.Defaults.Max; ceiling != nil {
			t.Fatalf("Controller CPU caps its axis at %v; a multi-core spike would be clipped", *ceiling)
		}
		return
	}
	t.Fatal("controller dashboard has no Controller CPU panel")
}

// TestControllerDashboardLatencyIsSecondsOverRequests pins the shape of the
// average: the seconds counter on top, the request counter that shares its
// labels underneath, both rated and summed by the same label.
//
// Inverting it, or dividing by a differently-labelled denominator, does not
// fail anywhere — Prometheus returns a number either way. It is just the wrong
// number, on a panel titled with a unit that makes it look plausible.
func TestControllerDashboardLatencyIsSecondsOverRequests(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		if p.Title != "API Latency" {
			continue
		}
		expr := p.Targets[0].Expr
		num, den, ok := strings.Cut(expr, " / ")
		if !ok {
			t.Fatalf("API Latency is not a ratio:\n%s", expr)
		}
		if !strings.Contains(num, "townos_http_request_seconds_total") {
			t.Errorf("API Latency numerator is not the seconds counter:\n%s", num)
		}
		if !strings.Contains(den, "townos_http_requests_total") {
			t.Errorf("API Latency denominator is not the request counter:\n%s", den)
		}
		// Both sides must aggregate by the same label, or the division falls
		// back to matching on the full label set and silently drops series.
		if !strings.Contains(num, "sum by (method)") || !strings.Contains(den, "sum by (method)") {
			t.Errorf("API Latency does not aggregate both sides by method:\n%s", expr)
		}
		return
	}
	t.Fatal("controller dashboard has no API Latency panel")
}

// TestControllerDashboardSelectsTheScrapeJob asserts every controller query
// carries the job selector built from ControllerJobName. The scrape config
// emits that label; a dashboard selecting a different one renders a tab of
// empty panels on a box that is working perfectly.
func TestControllerDashboardSelectsTheScrapeJob(t *testing.T) {
	t.Parallel()

	want := `{job="` + ControllerJobName + `"}`
	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		for j, tgt := range p.Targets {
			if !strings.Contains(tgt.Expr, want) {
				t.Errorf("panel %q target %d does not select %s:\n%s", p.Title, j, want, tgt.Expr)
			}
		}
	}
}

// TestControllerDashboardRateWindowsUseTheMacro asserts every rate() is
// windowed on $__rate_interval rather than a literal. A literal window
// shorter than the scrape interval yields no samples at all, and one longer
// than the panel's range flattens every spike.
func TestControllerDashboardRateWindowsUseTheMacro(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, ControllerDashboard())
	for _, p := range doc.Panels {
		for j, tgt := range p.Targets {
			if !strings.Contains(tgt.Expr, "rate(") {
				continue // gauge panel; no window to check
			}
			if !strings.Contains(tgt.Expr, "["+GrafanaRateInterval+"]") {
				t.Errorf("panel %q target %d rates on a literal window:\n%s", p.Title, j, tgt.Expr)
			}
		}
	}
}

// TestControllerDashboardMetricsAreTheOnesQueried keeps the declared list
// honest in both directions, so the scrape test below cannot pass while a
// panel sits empty.
func TestControllerDashboardMetricsAreTheOnesQueried(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, m := range ControllerDashboardMetrics() {
		declared[m] = true
	}

	doc := parseDashboard(t, ControllerDashboard())
	used := map[string]bool{}
	for _, p := range doc.Panels {
		for _, tgt := range p.Targets {
			for _, m := range townosMetricNames(tgt.Expr) {
				used[m] = true
				if !declared[m] {
					t.Errorf("panel %q queries %s, which ControllerDashboardMetrics does not declare", p.Title, m)
				}
			}
		}
	}
	for m := range declared {
		if !used[m] {
			t.Errorf("ControllerDashboardMetrics declares %s, which no panel queries", m)
		}
	}
}

// TestControllerDashboardMetricsAreExported asserts every family the panels
// name is one the controller's own collector actually emits.
//
// The Go source is read rather than a live scrape being taken: the exporter
// is in another package with a large dependency surface (systemd, btrfs, a
// database), and the question here is only whether the two lists of names
// agree. An integration test scrapes the real endpoint; this catches the
// typo before the container is ever built.
func TestControllerDashboardMetricsAreExported(t *testing.T) {
	t.Parallel()

	exported := map[string]bool{}
	dir := filepath.Join("..", "svc", "systemcontroller")
	// Globbed rather than a hand-listed pair of filenames: the collector is
	// split by what it reads from (units, packages, the process itself), and a
	// hardcoded list turns "the collector grew a file" into "the dashboard
	// queries a family nothing exports" — a failure that points at the
	// dashboard when the dashboard is right. Test files are excluded so a name
	// that only ever appears in an assertion cannot pass for an export.
	sources, err := filepath.Glob(filepath.Join(dir, "controller_metrics*.go"))
	if err != nil {
		t.Fatalf("glob controller metrics sources: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("no controller metrics sources found under %s", dir)
	}
	for _, path := range sources {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range townosMetricNames(string(raw)) {
			exported[m] = true
		}
	}

	for _, m := range ControllerDashboardMetrics() {
		if !exported[m] {
			t.Errorf("the controller dashboard queries %s, which %s does not export", m, dir)
		}
	}
}

// TestControllerDashboardMirroredInFrontendQueries reads the uPlot
// frontend's query module and asserts it names exactly the townos metrics
// the Grafana dashboard does.
//
// Same drift guard, and same reasoning, as
// TestRolodexDashboardMirroredInFrontendQueries: the two frontends are
// separate code in separate languages rendering the same dashboard, and a
// panel added to one and forgotten in the other is not a build failure — it
// is a box where the answer to "is anything failing" depends on which
// backend the operator happens to be running.
func TestControllerDashboardMirroredInFrontendQueries(t *testing.T) {
	t.Parallel()

	// The package lives at src/monitoring; the UI is two levels up.
	path := filepath.Join("..", "..", "ui", "src", "components", "monitoring", "queries.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	js := string(raw)

	frontend := map[string]bool{}
	for _, m := range townosMetricNames(js) {
		frontend[m] = true
	}

	for _, m := range ControllerDashboardMetrics() {
		if !frontend[m] {
			t.Errorf("%s is queried by the Grafana controller dashboard but not by %s", m, path)
		}
	}
	for m := range frontend {
		if !slices.Contains(ControllerDashboardMetrics(), m) {
			t.Errorf("%s is queried by %s but not by the Grafana controller dashboard", m, path)
		}
	}

	if !strings.Contains(js, `{job="`+ControllerJobName+`"}`) {
		t.Errorf("%s does not select the %s scrape job", path, ControllerJobName)
	}
}

// townosMetricNames extracts every townos_* identifier from a string. Unlike
// its rolodex twin there is no histogram suffix to normalise: the controller
// exports counters and gauges only.
func townosMetricNames(s string) []string {
	const prefix = "townos_"
	var out []string
	seen := map[string]bool{}
	for i := 0; i < len(s); {
		idx := strings.Index(s[i:], prefix)
		if idx < 0 {
			break
		}
		start := i + idx
		end := start
		for end < len(s) && isMetricNameByte(s[end]) {
			end++
		}
		name := s[start:end]
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = end
	}
	return out
}
