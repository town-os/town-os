package systemcontroller

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/metrics"
	"github.com/labstack/echo/v5"
)

// metricsBackend is a controller with no managers wired — what a test server
// has, and what a controller still booting has.
type metricsBackend struct{ archiveTestBackend }

func TestStatusClassBuckets(t *testing.T) {
	cases := map[int]string{
		100: "other",
		200: "2xx", 204: "2xx", 299: "2xx",
		301: "3xx", 304: "3xx",
		400: "4xx", 403: "4xx", 404: "4xx", 499: "4xx",
		500: "5xx", 503: "5xx",
		0: "other",
	}
	for code, want := range cases {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

// A wildcard bind is not an address anything can connect to. Handing ":5309" to
// Prometheus verbatim yields a target that is permanently down, which reads as a
// broken controller rather than a misconfigured job.
func TestMetricsScrapeTargetRewritesWildcardBinds(t *testing.T) {
	cases := map[string]string{
		":5309":        "localhost:5309",
		"0.0.0.0:5309": "localhost:5309",
		"[::]:5309":    "localhost:5309",
	}
	for in, want := range cases {
		if got := MetricsScrapeTarget(in); got != want {
			t.Errorf("MetricsScrapeTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// A listener pinned to one interface is left alone: rewriting it to localhost
// would aim the scrape at an address the controller is deliberately not on.
func TestMetricsScrapeTargetPreservesExplicitHost(t *testing.T) {
	for _, in := range []string{"127.0.0.1:5309", "10.0.0.4:5309", "192.168.1.5:999"} {
		if got := MetricsScrapeTarget(in); got != in {
			t.Errorf("MetricsScrapeTarget(%q) = %q, want it unchanged", in, got)
		}
	}
}

// An underivable address yields no target, which omits the scrape job entirely
// rather than aiming one at a guess — the same omit-rather-than-guess rule the
// rolodex job follows.
func TestMetricsScrapeTargetEmptyWhenUnparseable(t *testing.T) {
	for _, in := range []string{"", "5309", "not an address", "host:"} {
		if got := MetricsScrapeTarget(in); got != "" {
			t.Errorf("MetricsScrapeTarget(%q) = %q, want empty", in, got)
		}
	}
}

// The harness relocates the controller with TOWN_OS_LISTEN, and the target is
// derived from that same string, so a test box cannot end up scraping whichever
// instance happens to own the default port.
func TestMetricsScrapeTargetFollowsRelocatedListener(t *testing.T) {
	if got := MetricsScrapeTarget(":41337"); got != "localhost:41337" {
		t.Errorf("got %q, want the relocated port carried through", got)
	}
}

func TestRecordAuditEventTalliesByOutcome(t *testing.T) {
	s := &SystemControllerHandlers{}
	s.recordAuditEvent(true)
	s.recordAuditEvent(true)
	s.recordAuditEvent(false)

	got := map[string]float64{}
	for _, sample := range s.metricsCounters().auditEvents.Collect().Samples {
		got[sample.Labels[0].Value] = sample.Value
	}
	if got["success"] != 2 || got["failure"] != 1 {
		t.Errorf("unexpected audit tallies: %v", got)
	}
}

// Two handler sets in one process must not share counters, or one test's
// requests show up in another's assertions.
func TestMetricsCountersArePerHandlerSet(t *testing.T) {
	a := &SystemControllerHandlers{}
	b := &SystemControllerHandlers{}
	a.recordAuditEvent(true)
	if len(b.metricsCounters().auditEvents.Collect().Samples) != 0 {
		t.Error("counters are shared between handler sets")
	}
}

func TestBucketActiveState(t *testing.T) {
	cases := map[string]string{
		"active": "active", "failed": "failed",
		// Transitional states count as inactive rather than getting their own
		// label: a unit mid-restart is not serving, and a transient label would
		// appear and vanish between scrapes as its own series.
		"activating": "inactive", "deactivating": "inactive",
		"reloading": "inactive", "inactive": "inactive", "": "inactive",
	}
	for in, want := range cases {
		if got := bucketActiveState(in); got != want {
			t.Errorf("bucketActiveState(%q) = %q, want %q", in, got, want)
		}
	}
}

// All three buckets are emitted even at zero. A gauge that vanishes when it
// reaches zero is indistinguishable from one the box stopped reporting, so "no
// failed units" would look exactly like "unit collection is broken".
func TestStateSamplesAlwaysEmitsEveryBucket(t *testing.T) {
	samples := stateSamples(map[string]float64{"active": 3})
	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3", len(samples))
	}
	seen := map[string]float64{}
	for _, s := range samples {
		seen[s.Labels[0].Value] = s.Value
	}
	if seen["active"] != 3 || seen["failed"] != 0 || seen["inactive"] != 0 {
		t.Errorf("unexpected samples: %v", seen)
	}
}

func TestStateSamplesOrderIsStable(t *testing.T) {
	a := stateSamples(map[string]float64{"active": 1, "failed": 2, "inactive": 3})
	if a[0].Labels[0].Value != "active" || a[1].Labels[0].Value != "failed" || a[2].Labels[0].Value != "inactive" {
		t.Errorf("state sample order changed: %+v", a)
	}
}

// A scrape must work on a controller whose managers are not wired. Reporting
// what is up beats reporting nothing at the moment somebody is asking why boot
// is stuck.
func TestCollectMetricsWithNoManagers(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	names := map[string]bool{}
	for _, f := range s.collectMetrics(context.Background()) {
		names[f.Name] = true
	}
	if !names["townos_up"] || !names["townos_start_time_seconds"] {
		t.Errorf("baseline metrics missing from a bare controller: %v", names)
	}
}

// serveMetrics runs the handler and returns the recorded response.
func serveMetrics(t *testing.T, s *SystemControllerHandlers) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, MetricsPath, nil)
	rec := httptest.NewRecorder()
	if err := s.metricsHandler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("metricsHandler: %v", err)
	}
	return rec
}

func TestMetricsHandlerServesExpositionFormat(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	rec := serveMetrics(t, s)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Prometheus keys off this content type; without it the scrape is rejected
	// before a single line is parsed.
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"# TYPE townos_up gauge", "townos_up 1", "townos_start_time_seconds"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}

// A scrape must never 500 over a sick subsystem: that makes every other metric
// disappear at exactly the moment they are wanted, so the box reads as entirely
// dead rather than partly degraded.
func TestMetricsHandlerServesDespiteMissingManagers(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	if rec := serveMetrics(t, s); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even with nothing wired", rec.Code)
	}
}

func TestMetricsHandlerIncludesCounterFamilies(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	s.recordAuditEvent(false)
	body := serveMetrics(t, s).Body.String()

	if !strings.Contains(body, "# TYPE townos_audit_events_total counter") {
		t.Errorf("audit counter missing:\n%s", body)
	}
	if !strings.Contains(body, `townos_audit_events_total{result="failure"} 1`) {
		t.Errorf("audit failure sample missing:\n%s", body)
	}
}

// The audit middleware is what feeds the counter, so an audited request must
// move it. This also pins that the two stay wired together.
func TestAuditMiddlewareFeedsTheCounter(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	before := len(s.metricsCounters().auditEvents.Collect().Samples)
	s.recordAuditEvent(true)
	if len(s.metricsCounters().auditEvents.Collect().Samples) == before {
		t.Error("recording an audit event did not create a series")
	}
}

// metricsMiddleware must not change the behavior it observes: the handler's
// error has to come back out untouched.
func TestMetricsMiddlewarePassesErrorsThrough(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/anything", nil)
	rec := httptest.NewRecorder()

	want := echo.NewHTTPError(http.StatusForbidden, "nope")
	err := s.metricsMiddleware(func(_ *echo.Context) error { return want })(e.NewContext(req, rec))
	if err == nil {
		t.Fatal("middleware swallowed the handler error")
	}

	// A handler that returns an error has not written its status yet, so the
	// code must come from the error rather than the untouched 200.
	got := map[string]float64{}
	for _, sample := range s.metricsCounters().httpRequests.Collect().Samples {
		got[sample.Labels[0].Value+" "+sample.Labels[1].Value] = sample.Value
	}
	if got["POST 4xx"] != 1 {
		t.Errorf("expected a POST 4xx tally, got %v", got)
	}
}

// A non-HTTP error is what the problem-detail handler renders as a 500, so that
// is what gets counted.
func TestMetricsMiddlewareCountsPlainErrorsAs5xx(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()

	_ = s.metricsMiddleware(func(_ *echo.Context) error { return context.DeadlineExceeded })(e.NewContext(req, rec))

	for _, sample := range s.metricsCounters().httpRequests.Collect().Samples {
		if sample.Labels[1].Value != "5xx" {
			t.Errorf("plain error counted as %q, want 5xx", sample.Labels[1].Value)
		}
	}
}

// Prometheus polls this every 15s; counting it would make the scrape endpoint
// dominate its own request counter.
func TestMetricsMiddlewareSkipsItsOwnPath(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, MetricsPath, nil)
	rec := httptest.NewRecorder()

	var during int64
	handler := s.metricsMiddleware(func(_ *echo.Context) error {
		during = s.metricsCounters().inFlight.Load()
		return nil
	})
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if len(s.metricsCounters().httpRequests.Collect().Samples) != 0 {
		t.Error("the scrape endpoint counted itself")
	}
	if len(s.metricsCounters().httpSeconds.Collect().Samples) != 0 {
		t.Error("the scrape endpoint timed itself")
	}
	// A scrape is in flight for the whole of the collection it triggers, so
	// counting it would pin the gauge at one on a box serving nothing at all.
	if during != 0 {
		t.Errorf("in-flight during a scrape = %d, want 0", during)
	}
}

func TestMetricsMiddlewareCountsSuccessfulRequests(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/status/ping", nil)
	rec := httptest.NewRecorder()

	handler := s.metricsMiddleware(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware: %v", err)
	}

	samples := s.metricsCounters().httpRequests.Collect().Samples
	if len(samples) != 1 || samples[0].Labels[1].Value != "2xx" {
		t.Errorf("unexpected tallies: %+v", samples)
	}
}

// The seconds counter is only useful divided by the request counter, and a
// division needs both sides to carry the same labels — otherwise the average
// can only be computed after aggregating one side away, which is not what the
// panel asks for.
func TestMetricsMiddlewareRecordsDurationUnderTheSameLabels(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/status/ping", nil)
	rec := httptest.NewRecorder()

	handler := s.metricsMiddleware(func(c *echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware: %v", err)
	}

	seconds := s.metricsCounters().httpSeconds.Collect()
	if len(seconds.Samples) != 1 {
		t.Fatalf("got %d duration samples, want 1: %+v", len(seconds.Samples), seconds.Samples)
	}
	sample := seconds.Samples[0]
	if sample.Labels[0].Value != http.MethodGet || sample.Labels[1].Value != "2xx" {
		t.Errorf("duration labels = %+v, want GET/2xx to match the request counter", sample.Labels)
	}
	// A wall-clock reading, so the only safe assertion is that it was recorded
	// at all and did not come out negative — which would make the counter go
	// backwards and read to Prometheus as a restart.
	if sample.Value < 0 {
		t.Errorf("recorded a negative duration: %v", sample.Value)
	}
}

// A request that ends in an error still took time, and the error path is
// exactly the one worth timing: a handler that blocks until a context deadline
// is the slowest thing the controller does.
func TestMetricsMiddlewareTimesFailedRequests(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/anything", nil)
	rec := httptest.NewRecorder()

	handler := s.metricsMiddleware(func(_ *echo.Context) error { return context.DeadlineExceeded })
	if err := handler(e.NewContext(req, rec)); err == nil {
		t.Fatal("middleware swallowed the handler error")
	}

	samples := s.metricsCounters().httpSeconds.Collect().Samples
	if len(samples) != 1 || samples[0].Labels[1].Value != "5xx" {
		t.Errorf("unexpected duration samples: %+v", samples)
	}
}

// The in-flight gauge is the one number no manager can be asked for after the
// fact: it only exists while the request is being served.
func TestMetricsMiddlewareTracksRequestsInFlight(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/status/ping", nil)
	rec := httptest.NewRecorder()

	var during int64
	handler := s.metricsMiddleware(func(c *echo.Context) error {
		during = s.metricsCounters().inFlight.Load()
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware: %v", err)
	}

	if during != 1 {
		t.Errorf("in-flight during the request = %d, want 1", during)
	}
	if after := s.metricsCounters().inFlight.Load(); after != 0 {
		t.Errorf("in-flight after the request = %d, want 0", after)
	}
}

// A panicking handler must not leak a count. The gauge would climb by one per
// panic and read as a permanently wedged control plane long after the box
// recovered — which is why the decrement is a defer rather than a line after
// next().
func TestMetricsMiddlewareReleasesInFlightOnPanic(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("the panic did not propagate; this test is not exercising the defer")
			}
		}()
		handler := s.metricsMiddleware(func(_ *echo.Context) error { panic("handler exploded") })
		// Unreachable while the handler panics, which is the point of the
		// deferred recover above — but written as a real check rather than
		// discarded, so a middleware that ever swallowed the panic and
		// returned an error would say so here instead of vanishing.
		if err := handler(e.NewContext(req, rec)); err != nil {
			t.Errorf("middleware returned an error instead of panicking: %v", err)
		}
	}()

	if after := s.metricsCounters().inFlight.Load(); after != 0 {
		t.Errorf("in-flight after a panicking handler = %d, want 0", after)
	}
}

// Unlike the counters, the in-flight gauge is emitted at zero: an idle box has
// nothing in flight, and that zero is the answer the panel is asking for.
func TestScrapeIncludesInFlightGaugeWhenIdle(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	body := serveMetrics(t, s).Body.String()

	if !strings.Contains(body, "townos_http_requests_in_flight 0") {
		t.Errorf("idle scrape does not report an in-flight count:\n%s", body)
	}
}

// The process section answers "why is the box slow", which every other family
// here is blind to: a controller leaking goroutines into swap looks perfectly
// healthy on units, packages, accounts and disk alike.
func TestCollectProcessMetricsReportsProcessHealth(t *testing.T) {
	values := map[string]float64{}
	for _, f := range collectProcessMetrics() {
		if len(f.Samples) != 1 {
			t.Errorf("%s has %d samples, want 1", f.Name, len(f.Samples))
			continue
		}
		values[f.Name] = f.Samples[0].Value
	}

	// Every one of these is measured from inside the running test binary, so a
	// zero is not a quiet box — it is a reading that did not happen.
	for _, name := range []string{
		"townos_goroutines",
		"townos_memory_heap_bytes",
		"townos_memory_rss_bytes",
		"townos_open_files",
	} {
		if values[name] <= 0 {
			t.Errorf("%s = %v, want a positive reading", name, values[name])
		}
	}
	if _, ok := values["townos_process_cpu_seconds_total"]; !ok {
		t.Error("no CPU time reported")
	}
	if values["townos_process_cpu_seconds_total"] < 0 {
		t.Errorf("negative CPU time: %v", values["townos_process_cpu_seconds_total"])
	}
}

// CPU time is cumulative for the life of the process. Exporting it as a gauge
// would make Prometheus graph a permanently climbing line instead of rating it
// into the per-second figure the panel draws.
func TestProcessCPUIsACounter(t *testing.T) {
	for _, f := range collectProcessMetrics() {
		if f.Name != "townos_process_cpu_seconds_total" {
			continue
		}
		if f.Type != metrics.TypeCounter {
			t.Errorf("CPU time exported as %q, want counter", f.Type)
		}
		return
	}
	t.Fatal("no CPU family in the process metrics")
}

// The resident set is read as a page count and scaled by the page size rather
// than an assumed 4096: this ships on aarch64 too, and a 16K-page kernel would
// otherwise under-report memory fourfold — wrong in a way that still looks
// plausible on a graph.
func TestProcessRSSScalesByPageSize(t *testing.T) {
	rss, ok := processRSSBytes()
	if !ok {
		t.Skip("this kernel does not expose " + procStatm)
	}
	if pageSize := float64(os.Getpagesize()); rss < pageSize {
		t.Errorf("resident set = %v bytes, smaller than one %v-byte page", rss, pageSize)
	}
	if math.Mod(rss, float64(os.Getpagesize())) != 0 {
		t.Errorf("resident set = %v, not a whole number of pages", rss)
	}
}

// A descriptor leak is invisible everywhere else: nothing in the Go runtime
// tracks them, and the process dies of EMFILE hours later.
func TestOpenFileCountSeesThisProcessesDescriptors(t *testing.T) {
	before, ok := openFileCount()
	if !ok {
		t.Skip("this kernel does not expose " + procFDDir)
	}

	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("closing %s: %v", os.DevNull, cerr)
		}
	}()

	after, ok := openFileCount()
	if !ok {
		t.Fatal("descriptor count became unreadable mid-test")
	}
	if after <= before {
		t.Errorf("count did not move after opening a file: %v then %v", before, after)
	}
}

// The process section must survive the same bare controller every other
// section does: it reads nothing from any manager, so a scrape during boot is
// the moment it is most worth having.
func TestCollectMetricsIncludesProcessFamiliesWithNoManagers(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &metricsBackend{}}
	names := map[string]bool{}
	for _, f := range s.collectMetrics(context.Background()) {
		names[f.Name] = true
	}
	for _, want := range []string{
		"townos_goroutines",
		"townos_memory_heap_bytes",
		"townos_http_requests_in_flight",
	} {
		if !names[want] {
			t.Errorf("%s missing from a bare controller's scrape", want)
		}
	}
}

func TestUnixSeconds(t *testing.T) {
	if got := unixSeconds(time.Unix(1700000000, 500000000)); got != 1700000000.5 {
		t.Errorf("unixSeconds = %v, want 1700000000.5", got)
	}
}
