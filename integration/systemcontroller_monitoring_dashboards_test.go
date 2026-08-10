// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
)

// dashboardTargets is the slice of a provisioned dashboard this file reads.
type dashboardTargets struct {
	UID    string `json:"uid"`
	Title  string `json:"title"`
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
}

// TestGrafanaProvisioningWritesEveryDashboard proves the provisioning
// directory a real boot fills carries every dashboard in the registry, as
// parseable JSON with the uid the web UI deep-links to.
//
// The unit tests prove the registry renders; this proves the writer put it
// where Grafana looks. A dashboard that renders perfectly and is never
// written is not a broken panel — it is a "Dashboard not found" page
// inside the monitoring iframe, on a box where nothing logged an error.
func TestGrafanaProvisioningWritesEveryDashboard(t *testing.T) {
	t.Parallel()

	btrfsBase := t.TempDir()
	devices := []string{"sda3"}

	if err := monitoring.WriteGrafanaProvisioningFiles(btrfsBase, devices, monitoring.Ports{}); err != nil {
		t.Fatalf("WriteGrafanaProvisioningFiles: %v", err)
	}

	jsonDir := filepath.Join(btrfsBase, "monitoring", "grafana-provisioning", "dashboard-json")
	for _, want := range monitoring.GrafanaDashboards(devices) {
		raw, err := os.ReadFile(filepath.Join(jsonDir, want.Filename)) //nolint:gosec // path under t.TempDir()
		if err != nil {
			t.Errorf("dashboard %q not provisioned: %v", want.Filename, err)
			continue
		}
		var got dashboardTargets
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("dashboard %q is not valid JSON (Grafana would skip it entirely): %v", want.Filename, err)
			continue
		}
		if got.UID != want.UID {
			t.Errorf("dashboard %q has uid %q on disk, UI links to %q", want.Filename, got.UID, want.UID)
		}
		if len(got.Panels) == 0 {
			t.Errorf("dashboard %q was provisioned with no panels", want.Filename)
		}
	}

	// The provider scans the directory, so anything else in it is also
	// provisioned; assert the directory is exactly the registry.
	entries, err := os.ReadDir(jsonDir)
	if err != nil {
		t.Fatalf("read dashboard-json: %v", err)
	}
	if len(entries) != len(monitoring.GrafanaDashboards(devices)) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dashboard-json holds %v, want exactly the registered dashboards", names)
	}
}

// TestDashboardQueriesParseInPrometheus runs every panel expression from
// every provisioned dashboard past a real Prometheus.
//
// This is the check nothing cheaper can make. A dashboard is JSON, so a
// malformed PromQL expression inside it is not a syntax error anywhere —
// the file provisions, the dashboard loads, the panel draws its axes, and
// it renders "No data" for the rest of its life. That is indistinguishable
// from a quiet resolver, which is precisely the state an operator opens
// the DNS dashboard to rule out.
//
// The Grafana rate macro is expanded to a literal first: Prometheus has no
// macros, and the uPlot frontend pins the same literal for the same reason.
func TestDashboardQueriesParseInPrometheus(t *testing.T) {
	t.Parallel()

	base := prometheusScrapeDir(t)
	promPort := findFreePort(t)

	// No target needs to be up: this asks Prometheus to parse and evaluate
	// the expressions, not to have data for them. Every job is pointed at a
	// free port so a concurrent run's services are never scraped. IRON RULE.
	if err := monitoring.WritePrometheusConfig(base, monitoring.Ports{
		Prometheus:   promPort,
		NodeExporter: findFreePort(t),
	}); err != nil {
		t.Fatalf("WritePrometheusConfig: %v", err)
	}
	startPrometheusForScrape(t, base, promPort)

	// Wider than the container-start helpers' budget: this phase waits up
	// to two minutes for Prometheus to answer at all and then evaluates
	// every expression on every dashboard, each with its own timeout.
	ctx := testContext(t, 5*time.Minute)
	queryURL := "http://127.0.0.1:" + promPort + "/api/v1/query"
	waitForPrometheusReady(ctx, t, queryURL)

	checked := 0
	for _, dash := range monitoring.GrafanaDashboards([]string{"sda3"}) {
		var doc dashboardTargets
		if err := json.Unmarshal([]byte(dash.JSON), &doc); err != nil {
			t.Errorf("dashboard %q is not valid JSON: %v", dash.Filename, err)
			continue
		}
		for _, panel := range doc.Panels {
			for i, tgt := range panel.Targets {
				expr := strings.ReplaceAll(tgt.Expr, "$__rate_interval", "5m")
				if err := prometheusAcceptsQuery(ctx, queryURL, expr); err != nil {
					t.Errorf("%s panel %q target %d: %v\nexpr: %s", dash.Filename, panel.Title, i, err, expr)
				}
				checked++
			}
		}
	}

	// A registry that rendered no targets would let every assertion above
	// vacuously pass.
	if checked == 0 {
		t.Fatal("no panel expressions were checked")
	}
	t.Logf("validated %d dashboard expressions", checked)
}

// waitForPrometheusReady polls the query API until it answers, so a query
// racing container startup is not reported as a bad expression.
func waitForPrometheusReady(ctx context.Context, t *testing.T, queryURL string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Fatalf("context cancelled waiting for prometheus: %v", ctx.Err())
		case <-time.After(time.Second):
		}
		if err := prometheusAcceptsQuery(ctx, queryURL, "up"); err == nil {
			return
		} else {
			lastErr = err
		}
	}
	t.Fatalf("prometheus never became ready: %v", lastErr)
}

// prometheusAcceptsQuery submits one expression and reports whether
// Prometheus evaluated it. An empty result is success — the point is that
// the expression is well-formed, not that the box has data for it.
func prometheusAcceptsQuery(ctx context.Context, queryURL, expr string) (err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	form := url.Values{"query": {expr}}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, queryURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build query request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("query prometheus: %w", err)
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return fmt.Errorf("read query response: %w", readErr)
	}

	var parsed struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
	}
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr != nil {
		return fmt.Errorf("decode query response (status %d): %s", resp.StatusCode, truncateForLog(string(body)))
	}
	if parsed.Status != "success" {
		return fmt.Errorf("prometheus rejected the expression: %s: %s", parsed.ErrorType, parsed.Error)
	}
	return nil
}
