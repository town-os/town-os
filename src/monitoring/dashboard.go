package monitoring

import (
	"encoding/json"
	"fmt"
)

// OverviewDashboardUID, DNSDashboardUID and ControllerDashboardUID are the
// stable uids Grafana deep-links are built from. They are constants for the
// same reason RolodexJobName is: the web UI's iframe URL names one of them,
// and a uid that moves orphans the link without any error anywhere — Grafana
// simply serves a "dashboard not found" page inside the frame.
const (
	OverviewDashboardUID   = "town-os-overview"
	DNSDashboardUID        = "town-os-dns"
	ControllerDashboardUID = "town-os-controller"
)

// GrafanaDashboard is one provisioned dashboard: the file it is written to
// under dashboard-json/, the uid the UI deep-links to, the title Grafana
// shows, and the rendered JSON.
type GrafanaDashboard struct {
	Filename string
	UID      string
	Title    string
	JSON     string
}

// GrafanaDashboards returns every dashboard the provisioner writes. It is
// the single list the writer iterates, so adding a dashboard is one entry
// here rather than another hardcoded os.WriteFile in
// WriteGrafanaProvisioningFiles — the shape that previously left the file
// writer as the de-facto registry, where a second dashboard could only be
// added by editing code that has nothing to do with dashboards.
//
// diskDevices parameterises the overview dashboard's Disk I/O panel; see
// TownOSOverviewDashboard.
func GrafanaDashboards(diskDevices []string) []GrafanaDashboard {
	return []GrafanaDashboard{
		{
			Filename: "town-os-overview.json",
			UID:      OverviewDashboardUID,
			Title:    "Town OS Overview",
			JSON:     TownOSOverviewDashboard(diskDevices),
		},
		{
			Filename: "town-os-dns.json",
			UID:      DNSDashboardUID,
			Title:    "Town OS DNS",
			JSON:     RolodexDashboard(),
		},
		{
			Filename: "town-os-controller.json",
			UID:      ControllerDashboardUID,
			Title:    "Town OS Controller",
			JSON:     ControllerDashboard(),
		},
	}
}

// GrafanaRateInterval is the rate window dashboard queries use. Grafana
// expands the macro per panel from the scrape interval and the selected
// time range; the uPlot frontend, which has no macro expansion, pins the
// literal in ui/src/components/monitoring/queries.js instead.
const GrafanaRateInterval = "$__rate_interval"

// dashboardPanelHeight and dashboardPanelWidth lay panels out two to a row
// on Grafana's 24-column grid.
const (
	dashboardPanelHeight = 8
	dashboardPanelWidth  = 12
)

// panelQuery is one series expression on a panel.
type panelQuery struct {
	// Expr is the PromQL, already carrying GrafanaRateInterval where a rate
	// window is needed.
	Expr string
	// Legend is the Grafana legendFormat: literal text, or {{label}} to
	// name one series per label value.
	Legend string
}

// panelSpec describes a timeseries panel in the terms that actually vary
// between them. Everything else — the axis styling, the transparent
// background, the compact list legend — is identical across the whole
// dashboard and is filled in by buildPanel, so a new panel cannot
// accidentally style itself differently.
type panelSpec struct {
	Title   string
	Unit    string
	Stacked bool
	// Min and Max pin the y axis. Both nil means autoscale.
	Min     *float64
	Max     *float64
	Queries []panelQuery
}

// datasourceRef is the object-form datasource reference. Grafana 13+ cannot
// resolve the legacy string form in panel targets — the panel renders "No
// data" with no error — so every target carries this shape.
type datasourceRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type panelTarget struct {
	Datasource   datasourceRef `json:"datasource"`
	Expr         string        `json:"expr"`
	LegendFormat string        `json:"legendFormat"`
	RefID        string        `json:"refId"`
}

type panelStacking struct {
	Group string `json:"group"`
	Mode  string `json:"mode"`
}

type panelCustom struct {
	AxisBorderShow    bool              `json:"axisBorderShow"`
	DrawStyle         string            `json:"drawStyle"`
	FillOpacity       int               `json:"fillOpacity"`
	GradientMode      string            `json:"gradientMode"`
	LineInterpolation string            `json:"lineInterpolation"`
	LineWidth         int               `json:"lineWidth"`
	PointSize         int               `json:"pointSize"`
	ShowPoints        string            `json:"showPoints"`
	SpanNulls         bool              `json:"spanNulls"`
	Stacking          panelStacking     `json:"stacking"`
	ThresholdsStyle   map[string]string `json:"thresholdsStyle"`
}

type thresholdStep struct {
	Color string  `json:"color"`
	Value float64 `json:"value"`
}

type panelThresholds struct {
	Mode  string          `json:"mode"`
	Steps []thresholdStep `json:"steps"`
}

type panelDefaults struct {
	Color      map[string]string `json:"color"`
	Custom     panelCustom       `json:"custom"`
	Max        *float64          `json:"max,omitempty"`
	Min        *float64          `json:"min,omitempty"`
	Thresholds panelThresholds   `json:"thresholds"`
	Unit       string            `json:"unit"`
}

type panelFieldConfig struct {
	Defaults panelDefaults `json:"defaults"`
	// Overrides is always an empty array rather than omitted: Grafana
	// tolerates a missing key but writes null back into a saved dashboard,
	// and a null there fails schema migration on later versions.
	Overrides []any `json:"overrides"`
}

type panelGridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

type panelLegend struct {
	Calcs       []string `json:"calcs"`
	DisplayMode string   `json:"displayMode"`
	Placement   string   `json:"placement"`
	ShowLegend  bool     `json:"showLegend"`
}

type panelOptions struct {
	Legend  panelLegend       `json:"legend"`
	Tooltip map[string]string `json:"tooltip"`
}

type timeseriesPanel struct {
	FieldConfig panelFieldConfig `json:"fieldConfig"`
	GridPos     panelGridPos     `json:"gridPos"`
	ID          int              `json:"id"`
	Options     panelOptions     `json:"options"`
	Targets     []panelTarget    `json:"targets"`
	Title       string           `json:"title"`
	Transparent bool             `json:"transparent"`
	Type        string           `json:"type"`
}

type grafanaDashboardDoc struct {
	Annotations   map[string]any    `json:"annotations"`
	Editable      bool              `json:"editable"`
	GraphTooltip  int               `json:"graphTooltip"`
	ID            *int              `json:"id"`
	Links         []any             `json:"links"`
	Panels        []timeseriesPanel `json:"panels"`
	Refresh       string            `json:"refresh"`
	SchemaVersion int               `json:"schemaVersion"`
	Tags          []string          `json:"tags"`
	Templating    map[string]any    `json:"templating"`
	Time          map[string]string `json:"time"`
	Timepicker    map[string]any    `json:"timepicker"`
	Timezone      string            `json:"timezone"`
	Title         string            `json:"title"`
	UID           string            `json:"uid"`
	Version       int               `json:"version"`
}

// buildPanel renders one panel spec at the given zero-based position,
// laying panels out two to a row.
func buildPanel(spec panelSpec, index int) timeseriesPanel {
	fill := 10
	stacking := panelStacking{Group: "A", Mode: "none"}
	if spec.Stacked {
		fill = 20
		stacking.Mode = "normal"
	}

	targets := make([]panelTarget, 0, len(spec.Queries))
	for i, q := range spec.Queries {
		targets = append(targets, panelTarget{
			Datasource:   datasourceRef{Type: "prometheus", UID: GrafanaDatasourceUID},
			Expr:         q.Expr,
			LegendFormat: q.Legend,
			// refIds are A, B, C… Grafana requires them unique per panel;
			// beyond 26 targets this would repeat, which no panel here
			// comes close to and which the builder test pins.
			RefID: string(rune('A' + i%26)),
		})
	}

	return timeseriesPanel{
		FieldConfig: panelFieldConfig{
			Defaults: panelDefaults{
				Color: map[string]string{"mode": "palette-classic"},
				Custom: panelCustom{
					DrawStyle:         "line",
					FillOpacity:       fill,
					GradientMode:      "none",
					LineInterpolation: "smooth",
					LineWidth:         1,
					PointSize:         5,
					ShowPoints:        "never",
					Stacking:          stacking,
					ThresholdsStyle:   map[string]string{"mode": "off"},
				},
				Max: spec.Max,
				Min: spec.Min,
				Thresholds: panelThresholds{
					Mode:  "absolute",
					Steps: []thresholdStep{{Color: "green", Value: 0}},
				},
				Unit: spec.Unit,
			},
			Overrides: []any{},
		},
		GridPos: panelGridPos{
			H: dashboardPanelHeight,
			W: dashboardPanelWidth,
			X: (index % 2) * dashboardPanelWidth,
			Y: (index / 2) * dashboardPanelHeight,
		},
		ID: index + 1,
		Options: panelOptions{
			// A list legend rather than a table one: a table legend on a
			// per-label panel (rcode, tier, source) runs the rows off the
			// bottom of an 8-row panel at 1080p.
			Legend: panelLegend{
				Calcs:       []string{"lastNotNull"},
				DisplayMode: "list",
				Placement:   "bottom",
				ShowLegend:  true,
			},
			Tooltip: map[string]string{"mode": "multi", "sort": "desc"},
		},
		Targets:     targets,
		Title:       spec.Title,
		Transparent: true,
		Type:        "timeseries",
	}
}

// buildDashboard renders a dashboard from its panel specs. It marshals
// rather than concatenating a template, so a panel cannot ship malformed
// JSON — which for Grafana is not a broken panel but a dashboard that
// fails provisioning entirely and never appears at all.
func buildDashboard(uid, title string, specs []panelSpec) string {
	panels := make([]timeseriesPanel, 0, len(specs))
	for i, spec := range specs {
		panels = append(panels, buildPanel(spec, i))
	}

	doc := grafanaDashboardDoc{
		Annotations:   map[string]any{},
		Editable:      false,
		GraphTooltip:  0,
		ID:            nil,
		Links:         []any{},
		Panels:        panels,
		Refresh:       "30s",
		SchemaVersion: 42,
		Tags:          []string{},
		Templating:    map[string]any{"list": []any{}},
		Time:          map[string]string{"from": "now-6h", "to": "now"},
		Timepicker: map[string]any{
			"hidden":            false,
			"refresh_intervals": []string{"5s", "10s", "30s", "1m", "5m", "15m", "30m", "1h", "2h", "1d"},
		},
		Timezone: "browser",
		Title:    title,
		UID:      uid,
		Version:  1,
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		// Unreachable: every field above is a plain marshalable type. A
		// panic here would take down a boot over a dashboard, so the
		// failure is rendered as a dashboard Grafana can still load and
		// that says what happened, rather than as a crash.
		return fmt.Sprintf(`{"title":%q,"uid":%q,"panels":[],"schemaVersion":42}`, title+" (render failed)", uid)
	}
	return string(out)
}
