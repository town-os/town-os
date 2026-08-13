package monitoring

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// dashboardDoc is the slice of a rendered dashboard these tests read.
type dashboardDoc struct {
	Title  string `json:"title"`
	UID    string `json:"uid"`
	Panels []struct {
		Title       string `json:"title"`
		Transparent bool   `json:"transparent"`
		GridPos     struct {
			H int `json:"h"`
			W int `json:"w"`
			X int `json:"x"`
			Y int `json:"y"`
		} `json:"gridPos"`
		ID          int `json:"id"`
		FieldConfig struct {
			Defaults struct {
				Unit   string   `json:"unit"`
				Min    *float64 `json:"min"`
				Max    *float64 `json:"max"`
				Custom struct {
					Stacking struct {
						Mode string `json:"mode"`
					} `json:"stacking"`
				} `json:"custom"`
			} `json:"defaults"`
			Overrides []any `json:"overrides"`
		} `json:"fieldConfig"`
		Targets []struct {
			Datasource *struct {
				Type string `json:"type"`
				UID  string `json:"uid"`
			} `json:"datasource"`
			Expr         string `json:"expr"`
			LegendFormat string `json:"legendFormat"`
			RefID        string `json:"refId"`
		} `json:"targets"`
	} `json:"panels"`
}

func parseDashboard(t *testing.T, raw string) dashboardDoc {
	t.Helper()
	var doc dashboardDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("dashboard is not valid JSON: %v\n%s", err, raw)
	}
	return doc
}

// TestGrafanaDashboardsCoversEveryDashboard asserts the registry is the
// complete set and that each entry is internally consistent: the uid in
// the JSON is the uid the UI is told to deep-link to. A registry entry
// whose declared uid disagrees with its rendered one sends the iframe to a
// dashboard that does not exist, and Grafana reports that as a page rather
// than an error anything can see.
func TestGrafanaDashboardsCoversEveryDashboard(t *testing.T) {
	t.Parallel()

	dashboards := GrafanaDashboards([]string{"sda3"})
	if len(dashboards) < 3 {
		t.Fatalf("expected at least the overview, DNS and controller dashboards, got %d", len(dashboards))
	}

	seenUID := map[string]bool{}
	seenFile := map[string]bool{}
	for _, d := range dashboards {
		if !strings.HasSuffix(d.Filename, ".json") {
			t.Errorf("dashboard %q filename %q must end in .json; the provisioner globs the directory", d.UID, d.Filename)
		}
		if seenFile[d.Filename] {
			t.Errorf("two dashboards write to %q; one silently overwrites the other", d.Filename)
		}
		seenFile[d.Filename] = true

		if seenUID[d.UID] {
			t.Errorf("duplicate dashboard uid %q; Grafana refuses to provision the second", d.UID)
		}
		seenUID[d.UID] = true

		doc := parseDashboard(t, d.JSON)
		if doc.UID != d.UID {
			t.Errorf("dashboard %q renders uid %q; the UI deep-link would 404", d.Filename, doc.UID)
		}
		if doc.Title != d.Title {
			t.Errorf("dashboard %q renders title %q, registry says %q", d.Filename, doc.Title, d.Title)
		}
		if len(doc.Panels) == 0 {
			t.Errorf("dashboard %q has no panels", d.Filename)
		}
	}

	if !seenUID[OverviewDashboardUID] {
		t.Errorf("registry is missing the overview dashboard (%s)", OverviewDashboardUID)
	}
	if !seenUID[DNSDashboardUID] {
		t.Errorf("registry is missing the DNS dashboard (%s)", DNSDashboardUID)
	}
	if !seenUID[ControllerDashboardUID] {
		t.Errorf("registry is missing the controller dashboard (%s)", ControllerDashboardUID)
	}
}

// TestOverviewDashboardUIDMatchesTemplate pins the constant against the
// hand-written overview template. The template predates the constant, so
// nothing but this test stops the two from drifting.
func TestOverviewDashboardUIDMatchesTemplate(t *testing.T) {
	t.Parallel()
	doc := parseDashboard(t, TownOSOverviewDashboard([]string{"sda3"}))
	if doc.UID != OverviewDashboardUID {
		t.Fatalf("overview template uid = %q, constant = %q", doc.UID, OverviewDashboardUID)
	}
}

// TestRolodexDashboardPanels walks the built DNS dashboard and asserts the
// properties a panel needs to render at all: an object-form datasource ref
// with the pinned uid (Grafana 13+ silently shows "No data" for the legacy
// string form), a non-empty expression, a unique refId per panel, and a
// grid position that does not overlap its neighbour.
func TestRolodexDashboardPanels(t *testing.T) {
	t.Parallel()

	raw := RolodexDashboard()
	if strings.Contains(raw, `"datasource": "Prometheus"`) {
		t.Fatal("DNS dashboard contains a legacy string-form datasource ref; Grafana 13 cannot resolve it")
	}
	doc := parseDashboard(t, raw)

	if len(doc.Panels) != len(rolodexPanels()) {
		t.Fatalf("rendered %d panels, spec declares %d", len(doc.Panels), len(rolodexPanels()))
	}

	occupied := map[[2]int]string{}
	ids := map[int]bool{}
	for i, p := range doc.Panels {
		if p.Title == "" {
			t.Errorf("panel %d has no title", i)
		}
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

// TestRolodexDashboardSelectsTheScrapeJob asserts every DNS query carries
// the job selector built from RolodexJobName. The scrape config emits that
// label; a dashboard selecting a different one renders eight empty panels
// on a box whose DNS is working perfectly.
func TestRolodexDashboardSelectsTheScrapeJob(t *testing.T) {
	t.Parallel()

	want := `{job="` + RolodexJobName + `"}`
	doc := parseDashboard(t, RolodexDashboard())
	for _, p := range doc.Panels {
		for j, tgt := range p.Targets {
			if !strings.Contains(tgt.Expr, want) {
				t.Errorf("panel %q target %d does not select %s:\n%s", p.Title, j, want, tgt.Expr)
			}
		}
	}
}

// TestRolodexDashboardRateWindowsUseTheMacro asserts every rate() in the
// DNS dashboard is windowed on $__rate_interval rather than a literal. A
// literal window shorter than the scrape interval yields no samples at
// all, and one longer than the panel's range flattens every spike.
func TestRolodexDashboardRateWindowsUseTheMacro(t *testing.T) {
	t.Parallel()

	doc := parseDashboard(t, RolodexDashboard())
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

// TestRolodexDashboardMetricsAreTheOnesQueried keeps the list a scrape
// test asserts against honest. The list exists so an integration test can
// prove the pinned rolodex image emits every family the panels name; a
// list that has fallen behind the panels makes that test pass while the
// new panel is empty.
func TestRolodexDashboardMetricsAreTheOnesQueried(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, m := range RolodexDashboardMetrics() {
		declared[m] = true
	}

	doc := parseDashboard(t, RolodexDashboard())
	used := map[string]bool{}
	for _, p := range doc.Panels {
		for _, tgt := range p.Targets {
			for _, m := range rolodexMetricNames(tgt.Expr) {
				used[m] = true
				if !declared[m] {
					t.Errorf("panel %q queries %s, which RolodexDashboardMetrics does not declare", p.Title, m)
				}
			}
		}
	}
	for m := range declared {
		if !used[m] {
			t.Errorf("RolodexDashboardMetrics declares %s, which no panel queries", m)
		}
	}
}

// TestRolodexDashboardMirroredInFrontendQueries reads the uPlot frontend's
// query module and asserts it names exactly the rolodex metrics the
// Grafana dashboard does.
//
// The two frontends are separate code in separate languages rendering the
// same dashboard, and nothing else connects them: a panel added to one and
// forgotten in the other is not a build failure, it is a box where the
// answer to "why is DNS slow" depends on which backend the operator
// happens to be running. This is the same drift guard
// TestBootStepsFrontendInSyncWithBackend applies to the boot stages.
func TestRolodexDashboardMirroredInFrontendQueries(t *testing.T) {
	t.Parallel()

	// The package lives at src/monitoring; the UI is two levels up.
	path := filepath.Join("..", "..", "ui", "src", "components", "monitoring", "queries.js")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	js := string(raw)

	frontend := map[string]bool{}
	for _, m := range rolodexMetricNames(js) {
		frontend[m] = true
	}

	for _, m := range RolodexDashboardMetrics() {
		if !frontend[m] {
			t.Errorf("%s is queried by the Grafana DNS dashboard but not by %s", m, path)
		}
	}
	for m := range frontend {
		if !slices.Contains(RolodexDashboardMetrics(), m) {
			t.Errorf("%s is queried by %s but not by the Grafana DNS dashboard", m, path)
		}
	}

	// The frontend cannot expand $__rate_interval, so it must pin a
	// literal; a macro leaking across would make every rate query a
	// Prometheus parse error and blank the whole tab.
	if strings.Contains(js, GrafanaRateInterval) {
		t.Errorf("%s contains the Grafana macro %s, which Prometheus cannot parse", path, GrafanaRateInterval)
	}
	if !strings.Contains(js, `{job="`+RolodexJobName+`"}`) {
		t.Errorf("%s does not select the %s scrape job", path, RolodexJobName)
	}
}

// rolodexMetricNames extracts every rolodex_dns_* identifier from a string,
// normalising the histogram _bucket suffix back to its family name so the
// two sides compare on the same terms.
func rolodexMetricNames(s string) []string {
	const prefix = "rolodex_dns_"
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
		name := strings.TrimSuffix(s[start:end], "_bucket")
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		i = end
	}
	return out
}

func isMetricNameByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// TestWriteGrafanaProvisioningFilesWritesEveryDashboard proves the writer
// iterates the registry rather than a hardcoded filename: the bug this
// guards is a dashboard that exists in code, is exercised by every unit
// test above, and is never written to disk — so it simply does not exist
// in Grafana.
func TestWriteGrafanaProvisioningFilesWritesEveryDashboard(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if err := WriteGrafanaProvisioningFiles(base, []string{"sda3"}, Ports{}); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	jsonDir := filepath.Join(base, "monitoring", "grafana-provisioning", "dashboard-json")
	for _, d := range GrafanaDashboards([]string{"sda3"}) {
		raw, err := os.ReadFile(filepath.Join(jsonDir, d.Filename))
		if err != nil {
			t.Errorf("dashboard %q was not written: %v", d.Filename, err)
			continue
		}
		doc := parseDashboard(t, string(raw))
		if doc.UID != d.UID {
			t.Errorf("%s on disk has uid %q, want %q", d.Filename, doc.UID, d.UID)
		}
	}

	// The provider points at the directory, so any stray file in it is
	// also provisioned. Assert the directory holds exactly the registry.
	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		t.Fatalf("read dashboard-json dir: %v", err)
	}
	if len(entries) != len(GrafanaDashboards(nil)) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dashboard-json holds %v, want exactly the %d registered dashboards", names, len(GrafanaDashboards(nil)))
	}
}

// TestGrafanaDashboardProviderScansTheDirectory pins the provisioner at
// the directory the writer fills. A provider naming one file would leave
// every dashboard after the first unprovisioned.
func TestGrafanaDashboardProviderScansTheDirectory(t *testing.T) {
	t.Parallel()
	if !strings.Contains(GrafanaDashboardProviderYAML, "path: /etc/grafana/provisioning/dashboard-json") {
		t.Fatalf("dashboard provider does not scan the dashboard-json directory:\n%s", GrafanaDashboardProviderYAML)
	}
}
