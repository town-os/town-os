package monitoring

import "fmt"

// rolodexSelector is the label selector every DNS panel query carries. It
// is built from RolodexJobName rather than written out, so the job label
// the scrape config emits and the one the dashboards select on cannot
// drift apart — a mismatch is not an error anywhere, it is eight panels
// that render "No data" on a box whose DNS is working fine.
func rolodexSelector() string {
	return fmt.Sprintf("{job=%q}", RolodexJobName)
}

// rolodexRate wraps a counter in a rate() over the Grafana rate-interval
// macro, with the job selector applied.
func rolodexRate(metric string) string {
	return fmt.Sprintf("rate(%s%s[%s])", metric, rolodexSelector(), GrafanaRateInterval)
}

// rolodexSumBy is the common shape: rate a counter, then sum it by one
// label so the panel gets one series per label value.
func rolodexSumBy(label, metric string) string {
	return fmt.Sprintf("sum by (%s) (%s)", label, rolodexRate(metric))
}

// rolodexSum rates a counter and sums away every label.
func rolodexSum(metric string) string {
	return fmt.Sprintf("sum(%s)", rolodexRate(metric))
}

// rolodexQuantile builds a latency quantile from a rolodex histogram.
// Summing by le before histogram_quantile is mandatory, not stylistic:
// the raw bucket series carry a proto label, and quantiling them
// unaggregated yields one line per transport rather than the box-wide
// latency the panel is titled for.
func rolodexQuantile(q float64, metric string) string {
	return fmt.Sprintf("histogram_quantile(%g, sum by (le) (%s))", q, rolodexRate(metric+"_bucket"))
}

// RolodexDashboardMetrics lists every rolodex metric family the DNS
// dashboard queries, without the _bucket suffix the histogram query adds.
// It exists so a test can assert the pinned rolodex image actually exports
// them: a panel naming a metric the daemon does not emit is invisible —
// Grafana renders an empty chart, exactly like an idle box.
func RolodexDashboardMetrics() []string {
	return []string{
		"rolodex_dns_queries_total",
		"rolodex_dns_query_duration_seconds",
		"rolodex_dns_answers_total",
		"rolodex_dns_cache_hits_total",
		"rolodex_dns_cache_negative_hits_total",
		"rolodex_dns_cache_misses_total",
		"rolodex_dns_cache_entries",
		"rolodex_dns_cache_negative_entries",
		"rolodex_dns_blocklist_cache_entries",
		"rolodex_dns_blocklist_blocks_total",
		"rolodex_dns_blocklist_allowlisted_total",
		"rolodex_dns_blocklist_refusals_total",
		"rolodex_dns_upstream_tier_wins_total",
		"rolodex_dns_upstream_tier_failures_total",
		"rolodex_dns_upstream_exhausted_total",
		"rolodex_dns_traffic_bytes_total",
	}
}

// cacheHitRatioExpr is the percentage of response-cache lookups that were
// served from cache, counting negative (NXDOMAIN/NODATA) hits as hits —
// a cached negative answer saved an upstream round trip just as a positive
// one did.
//
// The denominator is deliberately not clamped. An idle box divides zero by
// zero, which Prometheus renders as a gap in the line; clamping to 1 would
// instead draw a confident 0% hit ratio for a cache that was never asked
// anything.
func cacheHitRatioExpr() string {
	hits := rolodexSum("rolodex_dns_cache_hits_total")
	negative := rolodexSum("rolodex_dns_cache_negative_hits_total")
	misses := rolodexSum("rolodex_dns_cache_misses_total")
	return fmt.Sprintf("100 * (%s + %s) / (%s + %s + %s)", hits, negative, hits, negative, misses)
}

// rolodexPanels is the DNS dashboard's panel set, in grid order (two to a
// row). The uPlot frontend renders the same eight panels from the same
// expressions in ui/src/components/monitoring/queries.js.
func rolodexPanels() []panelSpec {
	sel := rolodexSelector()
	zero, hundred := 0.0, 100.0

	return []panelSpec{
		{
			// Response codes, not a bare query count: the total alone
			// cannot distinguish a busy resolver from one SERVFAILing
			// every lookup, and those are the same line.
			Title:   "DNS Queries by Response Code",
			Unit:    "reqps",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: rolodexSumBy("rcode", "rolodex_dns_queries_total"), Legend: "{{rcode}}"},
			},
		},
		{
			Title: "Query Latency",
			Unit:  "s",
			Queries: []panelQuery{
				{Expr: rolodexQuantile(0.5, "rolodex_dns_query_duration_seconds"), Legend: "p50"},
				{Expr: rolodexQuantile(0.95, "rolodex_dns_query_duration_seconds"), Legend: "p95"},
				{Expr: rolodexQuantile(0.99, "rolodex_dns_query_duration_seconds"), Legend: "p99"},
			},
		},
		{
			// Which resolution stage answered: cache, a local record, a
			// network scope, or an upstream tier. This is the panel that
			// says whether the box is answering for itself or forwarding.
			Title:   "Answers by Source",
			Unit:    "reqps",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: rolodexSumBy("source", "rolodex_dns_answers_total"), Legend: "{{source}}"},
			},
		},
		{
			Title:   "Cache Hit Ratio",
			Unit:    "percent",
			Min:     &zero,
			Max:     &hundred,
			Queries: []panelQuery{{Expr: cacheHitRatioExpr(), Legend: "Hit ratio"}},
		},
		{
			Title: "Cache Entries",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: "rolodex_dns_cache_entries" + sel, Legend: "Positive"},
				{Expr: "rolodex_dns_cache_negative_entries" + sel, Legend: "Negative"},
				{Expr: "rolodex_dns_blocklist_cache_entries" + sel, Legend: "Blocklist"},
			},
		},
		{
			// Refusals share this panel with blocks on purpose: a provider
			// answering "stop asking" rather than "listed" is the failure
			// that silently turns a blocklist into an outage, and it only
			// reads as anomalous next to the block rate it replaced.
			Title: "Blocklist Activity",
			Unit:  "reqps",
			Queries: []panelQuery{
				{Expr: rolodexSumBy("kind", "rolodex_dns_blocklist_blocks_total"), Legend: "blocked {{kind}}"},
				{Expr: rolodexSum("rolodex_dns_blocklist_allowlisted_total"), Legend: "allowlisted"},
				{Expr: rolodexSum("rolodex_dns_blocklist_refusals_total"), Legend: "refused"},
			},
		},
		{
			Title: "Upstream Tier Outcomes",
			Unit:  "reqps",
			Queries: []panelQuery{
				{Expr: rolodexSumBy("tier", "rolodex_dns_upstream_tier_wins_total"), Legend: "{{tier}} wins"},
				{Expr: rolodexSumBy("tier", "rolodex_dns_upstream_tier_failures_total"), Legend: "{{tier}} failures"},
				{Expr: rolodexSum("rolodex_dns_upstream_exhausted_total"), Legend: "exhausted"},
			},
		},
		{
			Title: "DNS Traffic",
			Unit:  "Bps",
			Queries: []panelQuery{
				{Expr: rolodexSumBy("direction", "rolodex_dns_traffic_bytes_total"), Legend: "{{direction}}"},
			},
		},
	}
}

// RolodexDashboard returns the Grafana dashboard for the box's DNS server.
// It is a dashboard of its own rather than more panels on the overview
// because the two answer different questions: the overview is about the
// host (disk, network, CPU, memory) and is what an operator watches when
// the box feels slow, while this one is about rolodex and is what they
// open when a name will not resolve. Folding eight DNS panels into the
// overview would bury the four host panels that are the reason anyone
// looks at it.
func RolodexDashboard() string {
	return buildDashboard(DNSDashboardUID, "Town OS DNS", rolodexPanels())
}
