// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/metrics"
)

// newMetricsTestServer returns a Server with the caddy passthrough disabled, so
// a unit test never reaches for a real admin API (there is none in a test
// process, and the attempt would cost every scrape its dial timeout).
func newMetricsTestServer(sup *stubSupervisor) *Server {
	srv := NewServer(sup, 443, 80, "")
	srv.caddyMetricsURL = ""
	return srv
}

// scrapeBody drives the real HTTP handler rather than calling collectMetrics,
// so the assertions cover the exposition-format rendering and the status code a
// scrape actually sees.
func scrapeBody(t *testing.T, srv *Server) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, MetricsPath, nil)
	srv.MetricsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("scrape returned %d, want 200:\n%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != metrics.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, metrics.ContentType)
	}
	return rec.Body.String()
}

// sampleValue returns the value of the first line whose metric name and label
// clause match the given prefix, or "" when the series is absent. Matching on
// the full prefix (not a substring) keeps a family whose name is a prefix of
// another from answering for it.
func sampleValue(body, prefix string) string {
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		rest, ok := strings.CutPrefix(line, prefix)
		if !ok {
			continue
		}
		if rest, ok = strings.CutPrefix(rest, " "); ok {
			return rest
		}
	}
	return ""
}

// TestMetricsExportsProcessFamilies covers the two families every other one is
// read against: a scrape with no routes at all must still say the ingress is up
// and when it started, or a dead ingress and a boot-time one look the same.
func TestMetricsExportsProcessFamilies(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	body := scrapeBody(t, srv)

	if got := sampleValue(body, "townos_ingress_up"); got != "1" {
		t.Errorf("townos_ingress_up = %q, want 1:\n%s", got, body)
	}
	if got := sampleValue(body, "townos_ingress_start_time_seconds"); got == "" || got == "0" {
		t.Errorf("townos_ingress_start_time_seconds = %q, want a Unix timestamp:\n%s", got, body)
	}
	for _, want := range []string{
		"# TYPE townos_ingress_up gauge",
		"# TYPE townos_ingress_reloads_total counter",
		"# TYPE townos_ingress_routes gauge",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q:\n%s", want, body)
		}
	}
}

// TestMetricsSeedsCounterSeries is the alerting contract. metrics.Render omits a
// counter family with no samples, so an unseeded reloads_total would be absent
// until the first failure — and `rate(...{result="failure"}[5m])` would return
// nothing at all, meaning the alert cannot fire until after the event it exists
// to catch has already happened.
func TestMetricsSeedsCounterSeries(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	body := scrapeBody(t, srv)

	for _, series := range []string{
		`townos_ingress_reloads_total{result="failure"}`,
		`townos_ingress_route_changes_total{op="add"}`,
		`townos_ingress_route_changes_total{op="remove"}`,
		`townos_ingress_route_changes_total{op="set"}`,
		`townos_ingress_dropped_total{kind="route",reason="hostname"}`,
		`townos_ingress_dropped_total{kind="path_backend",reason="duplicate"}`,
	} {
		if got := sampleValue(body, series); got != "0" {
			t.Errorf("%s = %q, want a seeded 0:\n%s", series, got, body)
		}
	}
}

// TestMetricsCountsRouteChangesAndReloads asserts the tallies follow the RPCs.
// Bootstrap reloads too, which is why the reload count is checked as "more than
// the mutations" rather than equal to them.
func TestMetricsCountsRouteChangesAndReloads(t *testing.T) {
	sup := &stubSupervisor{}
	srv := newMetricsTestServer(sup)
	ctx := context.Background()

	if err := srv.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := srv.AddRoute(ctx, &ingresspb.AddRouteRequest{
		Route: route("a.asdf.home", "ba:1", "/c/a"),
	}); err != nil {
		t.Fatalf("AddRoute: %v", err)
	}
	if _, err := srv.RemoveRoute(ctx, &ingresspb.RemoveRouteRequest{Hostname: "a.asdf.home"}); err != nil {
		t.Fatalf("RemoveRoute: %v", err)
	}
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	body := scrapeBody(t, srv)
	for series, want := range map[string]string{
		`townos_ingress_route_changes_total{op="add"}`:    "1",
		`townos_ingress_route_changes_total{op="remove"}`: "1",
		`townos_ingress_route_changes_total{op="set"}`:    "1",
		// Bootstrap reloads too, so four mutations of state produce four
		// reloads: one at boot and one per RPC.
		`townos_ingress_reloads_total{result="success"}`: "4",
		`townos_ingress_reloads_total{result="failure"}`: "0",
	} {
		if got := sampleValue(body, series); got != want {
			t.Errorf("%s = %q, want %q:\n%s", series, got, want, body)
		}
	}
	if got := sampleValue(body, "townos_ingress_last_reload_success_time_seconds"); got == "" || got == "0" {
		t.Errorf("last-success gauge = %q, want a Unix timestamp:\n%s", got, body)
	}
}

// TestMetricsCountsFailedReloads covers the number an operator is actually
// alerted on. A caddy that rejects the config keeps serving the last good one,
// so nothing observable changes on the box — the failure is invisible unless it
// is counted.
func TestMetricsCountsFailedReloads(t *testing.T) {
	sup := &stubSupervisor{reloadErr: errors.New("caddy rejected the config")}
	srv := newMetricsTestServer(sup)

	if err := srv.Bootstrap(); err == nil {
		t.Fatal("Bootstrap must surface the supervisor's error")
	}

	body := scrapeBody(t, srv)
	if got := sampleValue(body, `townos_ingress_reloads_total{result="failure"}`); got != "1" {
		t.Errorf("failed reloads = %q, want 1:\n%s", got, body)
	}
	if got := sampleValue(body, `townos_ingress_reloads_total{result="success"}`); got != "0" {
		t.Errorf("successful reloads = %q, want 0:\n%s", got, body)
	}
	// Never having succeeded is not the same as having succeeded at the epoch:
	// the gauge is omitted rather than emitted as 0, because `time() - 0` reads
	// as a 56-year-old config.
	if strings.Contains(body, "townos_ingress_last_reload_success_time_seconds") {
		t.Errorf("last-success gauge must be absent until a reload succeeds:\n%s", body)
	}
}

// TestMetricsReportsRoutesByTLSState covers the inventory an operator reads to
// answer "is everything published". The pending bucket is the interesting one: a
// route with no leaf resolves and does not serve, which is otherwise only
// visible by trying the name in a browser.
func TestMetricsReportsRoutesByTLSState(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	ctx := context.Background()

	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		route("local.asdf.home", "b1:1", "/c/local"),
		{Hostname: "public.example.com", Backend: "b2:2", Acme: true},
		{Hostname: "pending.asdf.home", Backend: "b3:3"},
		{Hostname: "paths.asdf.home", Backend: "b4:4", CertDir: "/c/paths", PathBackends: []*ingresspb.PathBackend{
			{Path: "/f", Backend: "b5:5"},
			{Path: "/g", Backend: "b6:6"},
		}},
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	body := scrapeBody(t, srv)
	for series, want := range map[string]string{
		`townos_ingress_routes{tls="local"}`:                              "2",
		`townos_ingress_routes{tls="acme"}`:                               "1",
		`townos_ingress_routes{tls="pending"}`:                            "1",
		`townos_ingress_path_backends`:                                    "2",
		`townos_ingress_route_https_ready{hostname="local.asdf.home"}`:    "1",
		`townos_ingress_route_https_ready{hostname="public.example.com"}`: "1",
		`townos_ingress_route_https_ready{hostname="pending.asdf.home"}`:  "0",
	} {
		if got := sampleValue(body, series); got != want {
			t.Errorf("%s = %q, want %q:\n%s", series, got, want, body)
		}
	}
}

// TestMetricsRouteSamplesAreStable pins the byte-stability two scrapes of an
// unchanged box need to be diffable. The route set is a map, so an unsorted
// collector would reshuffle its samples on every request for no reason.
func TestMetricsRouteSamplesAreStable(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	ctx := context.Background()

	routes := make([]*ingresspb.Route, 0, 8)
	for _, h := range []string{"e", "c", "a", "d", "b", "h", "f", "g"} {
		routes = append(routes, route(h+".asdf.home", "b:1", "/c/"+h))
	}
	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: routes}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	first := scrapeBody(t, srv)
	for range 5 {
		if got := scrapeBody(t, srv); got != first {
			t.Fatalf("two scrapes of an unchanged ingress differ:\nfirst:\n%s\nlater:\n%s", first, got)
		}
	}
}

// TestMetricsCountsDroppedRoutes is why the renderer returns a tally at all. A
// route with a malformed hostname is refused silently from the caller's side —
// accepted over gRPC, logged here, never served — and this counter is the only
// signal that reaches anything watching the box.
func TestMetricsCountsDroppedRoutes(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	ctx := context.Background()

	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{
		{Hostname: "bad host.asdf.home", Backend: "b:1", CertDir: "/c/bad"},
		{Hostname: "ok.asdf.home", Backend: "not a backend", CertDir: "/c/ok"},
		{Hostname: "paths.asdf.home", Backend: "b:1", CertDir: "/c/p", PathBackends: []*ingresspb.PathBackend{
			{Path: "/dup", Backend: "b1:1"},
			{Path: "/dup", Backend: "b2:2"},
			{Path: "not-a-path", Backend: "b3:3"},
		}},
	}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	body := scrapeBody(t, srv)
	for series, want := range map[string]string{
		`townos_ingress_dropped_total{kind="route",reason="hostname"}`:         "1",
		`townos_ingress_dropped_total{kind="route",reason="backend"}`:          "1",
		`townos_ingress_dropped_total{kind="path_backend",reason="duplicate"}`: "1",
		`townos_ingress_dropped_total{kind="path_backend",reason="path"}`:      "1",
	} {
		if got := sampleValue(body, series); got != want {
			t.Errorf("%s = %q, want %q:\n%s", series, got, want, body)
		}
	}
}

// TestMetricsCountsEachDropOnce guards the double-count a per-vhost validation
// would cause: a page route is HTTPS-ready AND ServeHttp, so it renders two site
// blocks from one set of path backends.
func TestMetricsCountsEachDropOnce(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})
	ctx := context.Background()

	if _, err := srv.SetRoutes(ctx, &ingresspb.SetRoutesRequest{Routes: []*ingresspb.Route{{
		Hostname:  "page.asdf.home",
		Backend:   "pages:80",
		CertDir:   "/c/page",
		ServeHttp: true,
		PathBackends: []*ingresspb.PathBackend{
			{Path: "/bad path", Backend: "b:1"},
		},
	}}}); err != nil {
		t.Fatalf("SetRoutes: %v", err)
	}

	body := scrapeBody(t, srv)
	if got := sampleValue(body, `townos_ingress_dropped_total{kind="path_backend",reason="path"}`); got != "1" {
		t.Errorf("one bad path across two vhosts counted %q times, want 1:\n%s", got, body)
	}
}

// TestMetricsPassesThroughCaddyMetrics covers the passthrough: caddy's own
// families describe the process that serves every request the box answers, and
// its admin API is container-local, so this endpoint is the only way they reach
// Prometheus.
func TestMetricsPassesThroughCaddyMetrics(t *testing.T) {
	const caddyBody = "# HELP caddy_admin_http_requests_total Counter.\n" +
		"# TYPE caddy_admin_http_requests_total counter\n" +
		`caddy_admin_http_requests_total{code="200"} 7` + "\n"

	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(caddyBody)); err != nil {
			t.Errorf("write caddy stub body: %v", err)
		}
	}))
	defer admin.Close()

	srv := NewServer(&stubSupervisor{}, 443, 80, "")
	srv.caddyMetricsURL = admin.URL + MetricsPath

	body := scrapeBody(t, srv)
	if !strings.Contains(body, `caddy_admin_http_requests_total{code="200"} 7`) {
		t.Errorf("caddy's families were not passed through:\n%s", body)
	}
	if got := sampleValue(body, "townos_ingress_caddy_up"); got != "1" {
		t.Errorf("townos_ingress_caddy_up = %q, want 1 when the admin API answered:\n%s", got, body)
	}
	// Our own families must still be there and must come first: the format
	// forbids interleaving, and appending after a complete render is what keeps
	// them apart.
	if !strings.Contains(body, "townos_ingress_up") {
		t.Errorf("the ingress's own families went missing alongside caddy's:\n%s", body)
	}
	if strings.Index(body, "townos_ingress_up") > strings.Index(body, "caddy_admin_http_requests_total") {
		t.Errorf("caddy's families were emitted before the ingress's own:\n%s", body)
	}
}

// TestMetricsSurvivesUnreachableCaddy is the rule the controller's endpoint
// follows for the same reason: an ingress whose child is wedged is exactly when
// the route counts are wanted, and a scrape that failed as a unit would take
// them away at that moment.
func TestMetricsSurvivesUnreachableCaddy(t *testing.T) {
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer admin.Close()

	srv := NewServer(&stubSupervisor{}, 443, 80, "")
	srv.caddyMetricsURL = admin.URL + MetricsPath

	body := scrapeBody(t, srv) // fatals on a non-200
	if got := sampleValue(body, "townos_ingress_caddy_up"); got != "0" {
		t.Errorf("townos_ingress_caddy_up = %q, want 0 when the admin API refused:\n%s", got, body)
	}
	if got := sampleValue(body, "townos_ingress_up"); got != "1" {
		t.Errorf("the ingress's own metrics must survive a sick child, got %q:\n%s", got, body)
	}
}

// TestMetricsHandlerServesOnlyMetricsPath keeps a typo'd Prometheus job from
// looking healthy: a handler that answered on every path would return the body
// for a URL this service does not have.
func TestMetricsHandlerServesOnlyMetricsPath(t *testing.T) {
	srv := newMetricsTestServer(&stubSupervisor{})

	rec := httptest.NewRecorder()
	srv.MetricsHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metricz", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /metricz returned %d, want 404:\n%s", rec.Code, rec.Body.String())
	}
}
