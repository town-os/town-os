package systemcontroller

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"gitea.com/town-os/town-os/src/metrics"
	"github.com/labstack/echo/v5"
)

// MetricsPath is where the Prometheus endpoint is served.
const MetricsPath = "/metrics"

// processStart is when this process came up, exported as
// townos_start_time_seconds. Uptime is deliberately not exported as its own
// metric: Prometheus computes it as `time() - townos_start_time_seconds`, and a
// duration the box increments itself would drift against the scraper's clock,
// which is the clock every other duration on the dashboard is in.
var processStart = time.Now()

// metricsState holds the process-lifetime counters. Point-in-time values are
// not kept here — they are read from the managers per scrape, because a cached
// copy of "how many units are active" is a second source of truth that can
// disagree with systemd, and disagreeing quietly is worse than being slow.
//
// inFlight is the one exception, and it is not a cached copy of anything: no
// manager knows how many requests are mid-flight, and the number only exists
// while the middleware is holding it.
type metricsState struct {
	auditEvents  *metrics.CounterVec
	httpRequests *metrics.CounterVec
	httpSeconds  *metrics.CounterVec
	inFlight     atomic.Int64
}

// metricsCounters returns the counter set, building it on first use.
//
// It lives on the handler set rather than at package scope so two test servers
// in one process cannot see each other's tallies, and it is lazy for the same
// reason the OAuth and login stores are: every test constructs a handler set
// directly, with no constructor that could have initialized it.
func (s *SystemControllerHandlers) metricsCounters() *metricsState {
	s.metricsOnce.Do(func() {
		s.metricsStore = &metricsState{
			auditEvents: metrics.NewCounterVec(
				"townos_audit_events_total",
				"Audit events recorded since this process started, by outcome.",
				"result",
			),
			httpRequests: metrics.NewCounterVec(
				"townos_http_requests_total",
				"HTTP requests served since this process started, by method and status class.",
				"method", "status",
			),
			// The same label tuple as the request counter, deliberately: the
			// two are only useful divided by one another, and a sum whose
			// labels do not match its count cannot be divided at all without
			// aggregating one side away first.
			httpSeconds: metrics.NewCounterVec(
				"townos_http_request_seconds_total",
				"Seconds spent serving HTTP requests since this process started, by method and status class.",
				"method", "status",
			),
		}
	})
	return s.metricsStore
}

// recordAuditEvent tallies one audit row as it is written.
//
// It counts what the audit log records, not what the audit log contains: the
// log is queried with a time filter for the dashboard's "recent errors" pill,
// and a counter is what answers "how often is this happening" without a
// database round trip per scrape. Both are exported — townos_audit_events_total
// and townos_audit_recent_errors — because they answer different questions and
// neither derives the other.
func (s *SystemControllerHandlers) recordAuditEvent(success bool) {
	result := "success"
	if !success {
		result = "failure"
	}
	s.metricsCounters().auditEvents.Inc(result)
}

// statusClass buckets a status code as 2xx/3xx/4xx/5xx.
//
// The class rather than the exact code, because the label is otherwise
// unbounded in the direction that matters: every distinct code becomes a
// permanent series, and a control plane answering 400, 401, 403, 404, 409, 422
// across a few dozen routes multiplies out fast for a question nobody asks of a
// household box. "Are we serving errors" is a class question; the exact code is
// already in the audit log and the request log.
func statusClass(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	case code >= 200:
		return "2xx"
	}
	return "other"
}

// metricsMiddleware tallies every served request: its count, how long it took,
// and how many are in flight while it runs.
//
// It records after next() so the status is the one actually written, and it
// returns the handler's error untouched — an observer that swallowed an error
// would change the behavior it exists to watch.
//
// The scrape endpoint drops out before anything is recorded, not after:
// Prometheus polls it every 15s, which would dominate its own request counter,
// and — because a scrape is in flight for the whole of the collection it
// triggers — would pin the in-flight gauge at one on a box serving nothing at
// all.
//
// The in-flight gauge is incremented before next() and decremented in a defer
// rather than after it: a handler that panics still unwinds through the defer,
// and a gauge that leaked a count per panic would climb forever and read as a
// wedged control plane long after the box recovered.
func (s *SystemControllerHandlers) metricsMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if c.Request().URL.Path == MetricsPath {
			return next(c)
		}

		state := s.metricsCounters()
		state.inFlight.Add(1)
		defer state.inFlight.Add(-1)

		start := time.Now()
		err := next(c)
		elapsed := time.Since(start)
		// A handler that returns an error has not written its status yet —
		// Echo's error handler renders it after this middleware unwinds — so
		// the recorded status would be the pre-error 200. Take the code from
		// the error instead, defaulting to 500 for a non-HTTP error, which is
		// what the problem-detail handler will send.
		status := http.StatusOK
		if resp, ok := c.Response().(*echo.Response); ok {
			status = resp.Status
		}
		if err != nil {
			status = http.StatusInternalServerError
			var he *echo.HTTPError
			if errors.As(err, &he) {
				status = he.Code
			}
		}
		method, class := c.Request().Method, statusClass(status)
		state.httpRequests.Inc(method, class)
		state.httpSeconds.Add(elapsed.Seconds(), method, class)
		return err
	}
}

// metricsHandler serves the Prometheus text exposition format.
//
// It never fails a scrape over a manager it could not read. A collector that
// returns 500 because one subsystem is sick makes every other metric disappear
// at exactly the moment they are wanted — the box reads as entirely dead rather
// than partly degraded — so a family that could not be gathered is logged and
// omitted and the rest of the scrape is served.
func (s *SystemControllerHandlers) metricsHandler(c *echo.Context) error {
	families := s.collectMetrics(c.Request().Context())
	c.Response().Header().Set(echo.HeaderContentType, metrics.ContentType)
	c.Response().WriteHeader(http.StatusOK)
	if err := metrics.Render(c.Response(), families); err != nil {
		slog.Error("rendering metrics", "error", err)
	}
	return nil
}

// unixSeconds renders a time as fractional Unix seconds, the convention for a
// *_start_time_seconds gauge.
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(time.Second)
}

// MetricsScrapeTarget turns the controller's listen address into the "host:port"
// Prometheus should scrape, or "" when it cannot be derived.
//
// The host is rewritten to localhost in every case where the listener is not
// already pinned to one specific address. A bare ":5309" or "0.0.0.0:5309" is a
// wildcard bind, not an address anything can connect to, and handing either to
// Prometheus verbatim yields a target that is permanently down — which reads as
// a broken controller rather than a misconfigured job. A listener explicitly
// pinned to one interface is left alone, because rewriting that to localhost
// would aim the scrape at an address the controller is deliberately not on.
//
// Deriving this from the same string that configures the listener is the point:
// the harness relocates the controller with TOWN_OS_LISTEN, and a target
// recomposed from a default port would silently scrape whichever instance
// happened to own 5309 — a test box reporting a dev box's metrics, or neither.
func MetricsScrapeTarget(listenAddr string) string {
	if listenAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
