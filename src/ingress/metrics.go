// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package ingress

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/metrics"
)

const (
	// MetricsPath is where the ingress serves its Prometheus endpoint.
	MetricsPath = "/metrics"

	// DefaultMetricsPort is the port the ingress serves MetricsPath on. It is
	// published to the host loopback only (see ingressctl), never to the LAN:
	// the scrape names every hostname the box serves and which of them have no
	// certificate yet, which is a map of what to attack and when.
	//
	// 9146 is not in Prometheus's list of default exporter ports, and it is
	// deliberately nowhere near rolodex's 9153 or node-exporter's 9100 — an
	// off-by-one in a hand-typed override should fail to connect rather than
	// scrape a different service's numbers under this job name.
	DefaultMetricsPort = 9146

	// caddyAdminMetricsURL is the caddy child's own Prometheus endpoint on its
	// admin API. The address is container-local (the admin API is never
	// published — see renderCaddyfileTally), and the Host header it implies is
	// one caddy's origin check accepts, which a "localhost" spelling is not
	// guaranteed to be.
	caddyAdminMetricsURL = "http://127.0.0.1:2019/metrics"

	// caddyScrapeTimeout bounds the admin fetch. A scrape must answer in
	// bounded time or Prometheus records the ingress itself as down — the
	// caddy passthrough is the least important part of this endpoint and must
	// never be what makes the rest of it late.
	caddyScrapeTimeout = 3 * time.Second

	// caddyScrapeLimit bounds how much of caddy's answer is read. The body is
	// echoed into our own response, so an unbounded read would let a wedged
	// child hold a scrape's worth of memory per concurrent request.
	caddyScrapeLimit = 4 << 20
)

// Route TLS buckets, the label values of townos_ingress_routes. "pending" is a
// route with neither an issued leaf nor an ACME issuer — programmed, resolvable,
// and not yet servable over HTTPS, which is a state an operator wants to see the
// size of rather than a state to hide.
const (
	tlsBucketLocal   = "local"
	tlsBucketACME    = "acme"
	tlsBucketPending = "pending"
)

// Route-change operations, the label values of townos_ingress_route_changes_total.
const (
	opSetRoutes   = "set"
	opAddRoute    = "add"
	opRemoveRoute = "remove"
)

// processStart is when this ingress process came up, exported as
// townos_ingress_start_time_seconds. Uptime is not exported as its own metric,
// for the same reason the controller does not export one: Prometheus computes it
// as `time() - townos_ingress_start_time_seconds`, in the scraper's clock rather
// than in a duration the box increments against a clock of its own.
var processStart = time.Now()

// metricsState holds the process-lifetime tallies.
//
// Point-in-time values (how many routes are programmed, how many have a leaf)
// are not kept here. They are read from the route map per scrape, because a
// cached copy of "what is programmed" is a second source of truth that can
// disagree with the Caddyfile actually loaded, and disagreeing quietly is worse
// than recomputing a map of tens of entries every 15 seconds.
type metricsState struct {
	reloads      *metrics.CounterVec
	routeChanges *metrics.CounterVec
	dropped      *metrics.CounterVec
}

// newMetricsState builds the counter set with every label combination seeded at
// zero.
//
// The seeding is the point. A counter family with no samples is omitted from the
// scrape entirely (metrics.Render skips it), so `rate(townos_ingress_reloads_total
// {result="failure"}[5m])` would evaluate to nothing at all until the first
// failure — an alert that cannot fire until the thing it watches has already
// happened once. The label sets here are small, closed, and known at compile
// time, so there is no cardinality cost to declaring them up front.
func newMetricsState() *metricsState {
	m := &metricsState{
		reloads: metrics.NewCounterVec(
			"townos_ingress_reloads_total",
			"Caddy config reloads attempted since this process started, by outcome.",
			"result",
		),
		routeChanges: metrics.NewCounterVec(
			"townos_ingress_route_changes_total",
			"Route programming calls served since this process started, by operation.",
			"op",
		),
		dropped: metrics.NewCounterVec(
			"townos_ingress_dropped_total",
			"Routes and path backends refused by the renderer since this process started.",
			"kind", "reason",
		),
	}
	for _, result := range []string{resultSuccess, resultFailure} {
		m.reloads.Add(0, result)
	}
	for _, op := range []string{opSetRoutes, opAddRoute, opRemoveRoute} {
		m.routeChanges.Add(0, op)
	}
	for _, pair := range [][2]string{
		{dropKindRoute, dropReasonHostname},
		{dropKindRoute, dropReasonBackend},
		{dropKindPathBackend, dropReasonPath},
		{dropKindPathBackend, dropReasonBackend},
		{dropKindPathBackend, dropReasonDuplicate},
	} {
		m.dropped.Add(0, pair[0], pair[1])
	}
	return m
}

// Reload outcomes, the label values of townos_ingress_reloads_total.
const (
	resultSuccess = "success"
	resultFailure = "failure"
)

// metricsCounters returns the counter set, building it on first use.
//
// Lazy for the same reason the controller's is: a Server built as a struct
// literal (which no production path does, but a future test might) must not
// panic on the first mutation, and a nil *metricsState would.
func (s *Server) metricsCounters() *metricsState {
	s.metricsOnce.Do(func() { s.metricsStore = newMetricsState() })
	return s.metricsStore
}

// recordTally folds one render's refusals into the drop counter.
func (s *Server) recordTally(t renderTally) {
	counters := s.metricsCounters()
	for key, n := range t.dropped {
		counters.dropped.Add(n, key[0], key[1])
	}
}

// recordReloadLocked tallies one reload attempt and, on success, stamps when it
// happened. The caller must hold s.mu, which is what guards the lastReloadOK
// write.
func (s *Server) recordReloadLocked(err error) {
	result := resultSuccess
	if err != nil {
		result = resultFailure
	} else {
		s.lastReloadOK = time.Now()
	}
	s.metricsCounters().reloads.Inc(result)
}

// collectMetrics gathers one scrape of the ingress's own state.
//
// caddyUp is whether the caddy child answered its admin endpoint on this
// scrape; it is passed in rather than probed here so the fetch happens once per
// request and its body can be appended to the same response.
func (s *Server) collectMetrics(caddyUp bool) []metrics.Metric {
	families := []metrics.Metric{
		metrics.Gauge("townos_ingress_up",
			"Always 1 while the ingress is serving. Absent when it is not.", 1),
		metrics.Gauge("townos_ingress_start_time_seconds",
			"Unix time at which this ingress process started.", unixSeconds(processStart)),
		metrics.Gauge("townos_ingress_caddy_up",
			"1 when the caddy child answered its admin API on this scrape, 0 otherwise.",
			boolValue(caddyUp)),
	}

	counters := s.metricsCounters()
	families = append(families,
		counters.reloads.Collect(),
		counters.routeChanges.Collect(),
		counters.dropped.Collect(),
	)

	return append(families, s.collectRouteMetrics()...)
}

// collectRouteMetrics reports what is currently programmed: how many routes
// exist in each TLS state, whether each individual one can serve HTTPS, and how
// many path backends are in play.
//
// The per-hostname gauge is bounded by what the box publishes — a package, a
// page, or an object-storage view each contribute one — which for a household
// box is tens of series, the same trade the controller makes with
// townos_package_unit_active. It is worth the cardinality because "one name is
// not serving" is the question this endpoint exists to answer, and a bucket
// count cannot say which name.
func (s *Server) collectRouteMetrics() []metrics.Metric {
	s.mu.Lock()
	routes := make([]*ingresspb.Route, 0, len(s.routes))
	for _, r := range s.routes {
		routes = append(routes, r)
	}
	lastReloadOK := s.lastReloadOK
	s.mu.Unlock()

	// Sorted by hostname so two consecutive scrapes of an unchanged box are
	// byte-identical and can be diffed; map iteration order is random.
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].GetHostname() < routes[j].GetHostname()
	})

	buckets := map[string]float64{tlsBucketLocal: 0, tlsBucketACME: 0, tlsBucketPending: 0}
	readySamples := make([]metrics.Sample, 0, len(routes))
	var pathBackends float64

	for _, r := range routes {
		bucket := tlsBucketPending
		switch {
		case r.GetAcme():
			bucket = tlsBucketACME
		case r.GetCertDir() != "":
			bucket = tlsBucketLocal
		}
		buckets[bucket]++
		readySamples = append(readySamples,
			metrics.Labelled(boolValue(bucket != tlsBucketPending), "hostname", r.GetHostname()))
		pathBackends += float64(len(r.GetPathBackends()))
	}

	out := []metrics.Metric{
		metrics.GaugeVec("townos_ingress_routes",
			"Programmed ingress routes by TLS state.", []metrics.Sample{
				metrics.Labelled(buckets[tlsBucketLocal], "tls", tlsBucketLocal),
				metrics.Labelled(buckets[tlsBucketACME], "tls", tlsBucketACME),
				metrics.Labelled(buckets[tlsBucketPending], "tls", tlsBucketPending),
			}),
		metrics.GaugeVec("townos_ingress_route_https_ready",
			"1 when a programmed route can serve HTTPS (has a leaf or an ACME issuer), 0 otherwise.",
			readySamples),
		metrics.Gauge("townos_ingress_path_backends",
			"Path-scoped backends across all programmed routes.", pathBackends),
	}

	// Omitted rather than emitted as zero when no reload has ever succeeded: a
	// zero here is 1970, and `time() - townos_ingress_last_reload_success_time_seconds`
	// would read as a 56-year-old config rather than as no config yet.
	if !lastReloadOK.IsZero() {
		out = append(out, metrics.Gauge("townos_ingress_last_reload_success_time_seconds",
			"Unix time of the last caddy reload that succeeded.", unixSeconds(lastReloadOK)))
	}
	return out
}

// MetricsHandler returns the HTTP handler serving MetricsPath.
//
// A mux rather than a bare handler so anything else on this listener answers
// 404 instead of the metrics body: the port is published to the host loopback,
// and a scrape that answered on every path would make a typo'd Prometheus job
// look healthy while naming a path this service does not have.
func (s *Server) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(MetricsPath, s.serveMetrics)
	return mux
}

// serveMetrics writes the exposition-format body: this process's own families
// first, then caddy's own, verbatim.
//
// It never fails a scrape. The status line goes out before anything can go
// wrong, and a caddy that could not be reached costs one gauge
// (townos_ingress_caddy_up) rather than the whole response — the same rule the
// controller's endpoint follows, for the same reason. An ingress whose child is
// wedged is exactly when the route counts are wanted, and a 500 would take them
// away at that moment.
func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	caddyBody, caddyErr := s.fetchCaddyMetrics(r.Context())
	if caddyErr != nil {
		// Debug, not error: the admin API is unreachable for a few hundred
		// milliseconds every time caddy restarts, and a scrape every 15s would
		// otherwise fill the journal with a condition the gauge already reports.
		slog.Debug("ingress: caddy admin metrics unavailable", "error", caddyErr)
	}

	w.Header().Set("Content-Type", metrics.ContentType)
	w.WriteHeader(http.StatusOK)
	if err := metrics.Render(w, s.collectMetrics(caddyErr == nil)); err != nil {
		slog.Error("ingress: rendering metrics", "error", err)
		return
	}
	if caddyErr != nil {
		return
	}
	if _, err := w.Write(caddyBody); err != nil {
		slog.Error("ingress: writing caddy metrics", "error", err)
	}
}

// fetchCaddyMetrics reads the caddy child's own Prometheus endpoint.
//
// Passing it through is what makes this endpoint worth scraping beyond route
// bookkeeping: caddy's families (its Go runtime, its admin API, and its HTTP
// server metrics) describe the process that actually serves every request the
// box answers, and its admin API is container-local, so nothing outside this
// container can reach it. The families are namespaced `caddy_*`/`go_*`/
// `process_*` and cannot collide with the `townos_ingress_*` ones rendered
// alongside them.
//
// The body is passed through unparsed. Re-rendering it would mean parsing the
// exposition format back into families and emitting it again, which can only
// lose information (caddy exports histograms, which src/metrics deliberately
// does not model) for no gain.
func (s *Server) fetchCaddyMetrics(ctx context.Context) (body []byte, err error) {
	url := s.caddyMetricsURL
	if url == "" {
		return nil, errors.New("caddy metrics passthrough disabled")
	}

	reqCtx, cancel := context.WithTimeout(ctx, caddyScrapeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := caddyMetricsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, resp.Body.Close())
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("caddy admin metrics returned " + resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, caddyScrapeLimit))
}

// caddyMetricsClient is a dedicated client with its own timeout rather than
// http.DefaultClient, which has none: a caddy child that accepts the connection
// and then stops writing would otherwise hold a scrape open forever.
var caddyMetricsClient = &http.Client{Timeout: caddyScrapeTimeout}

// unixSeconds renders a time as fractional Unix seconds, the convention for a
// *_time_seconds gauge.
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(time.Second)
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
