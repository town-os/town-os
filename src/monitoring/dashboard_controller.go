package monitoring

import "fmt"

// controllerSelector is the label selector every controller panel query
// carries, built from ControllerJobName for the same reason the DNS panels
// build theirs from RolodexJobName: the scrape config emits that job label,
// and a dashboard selecting a different one is not an error anywhere — it is
// a tab of empty charts on a box that is working fine.
func controllerSelector() string {
	return fmt.Sprintf("{job=%q}", ControllerJobName)
}

// controllerGauge selects a gauge family for the controller job. Gauges are
// queried bare rather than wrapped in sum(): the controller is a single
// scrape target, so there is nothing to aggregate across, and summing would
// only throw away the labels the legends render.
func controllerGauge(metric string) string {
	return metric + controllerSelector()
}

// controllerRate wraps a counter in a rate() over the Grafana rate-interval
// macro, with the job selector applied.
func controllerRate(metric string) string {
	return fmt.Sprintf("rate(%s%s[%s])", metric, controllerSelector(), GrafanaRateInterval)
}

// controllerSumBy rates a counter and sums it by one label, so the panel
// gets one series per label value rather than one per label combination.
// townos_http_requests_total carries both method and status; a panel about
// status classes that did not sum away method would draw a line per pair.
func controllerSumBy(label, metric string) string {
	return fmt.Sprintf("sum by (%s) (%s)", label, controllerRate(metric))
}

// controllerRatio divides one rated counter by another for the same job, which
// is how an average comes out of a pair of counters: the numerator is a sum of
// observations, the denominator the count of them.
//
// The division is per label value rather than across the whole panel, so the
// result is one average per series instead of one number that hides which side
// of the box is slow.
func controllerRatio(label, numerator, denominator string) string {
	return fmt.Sprintf("sum by (%s) (%s) / sum by (%s) (%s)",
		label, controllerRate(numerator), label, controllerRate(denominator))
}

// controllerPercentExpr renders one gauge as a percentage of another. It is
// deliberately unclamped: a zero denominator is a filesystem the collector
// could not read, and the resulting break in the line is honest, where a
// clamped denominator would draw a confident 0% full.
func controllerPercentExpr(part, whole string) string {
	return fmt.Sprintf("100 * %s / %s", controllerGauge(part), controllerGauge(whole))
}

// controllerUptimeExpr is how long the controller process has been up.
//
// Computed against the scraper's clock rather than exported as its own
// counter — see the note on processStart in controller_metrics.go. A restart
// is the drop to zero, which is the event this panel is really for.
func controllerUptimeExpr() string {
	return "time() - " + controllerGauge("townos_start_time_seconds")
}

// ControllerDashboardMetrics lists every townos metric family the controller
// dashboard queries. It exists for the same reason RolodexDashboardMetrics
// does: a test can then assert the controller's own /metrics endpoint really
// exports them, because a panel naming a family nothing emits renders an
// empty chart that looks exactly like an idle box.
//
// townos_up and townos_packages_available are deliberately absent. The first
// is a scrape-liveness constant — a flat line at 1 is not a panel. The second
// is a catalogue size in the thousands, and on a shared axis it flattens the
// counts beside it (installed, upgradable, repositories) into one line along
// the bottom; what an operator wants from it — "did a repository stop
// answering" — is townos_repository_errors, which has its own panel.
//
// townos_disk_total_bytes is present but only as a denominator: the fill panel
// divides by it, while the bytes panel stacks used and available, which
// already sum to it.
func ControllerDashboardMetrics() []string {
	return []string{
		"townos_system_units",
		"townos_package_units",
		"townos_system_unit_active",
		"townos_package_unit_active",
		"townos_http_requests_total",
		"townos_http_request_seconds_total",
		"townos_http_requests_in_flight",
		"townos_audit_events_total",
		"townos_audit_recent_errors",
		"townos_repository_errors",
		"townos_packages_installed",
		"townos_upgrades_available",
		"townos_repositories",
		"townos_disk_used_bytes",
		"townos_disk_available_bytes",
		"townos_disk_total_bytes",
		"townos_accounts",
		"townos_accounts_granted",
		"townos_filesystems",
		"townos_goroutines",
		"townos_memory_heap_bytes",
		"townos_memory_rss_bytes",
		"townos_open_files",
		"townos_process_cpu_seconds_total",
		"townos_start_time_seconds",
	}
}

// controllerPanels is the controller dashboard's panel set, in grid order
// (two to a row). The uPlot frontend renders the same panels from the same
// expressions in ui/src/components/monitoring/queries.js.
//
// The order is the order an operator reads them in when something is wrong:
// what is down, what the API is doing, what is failing, what the disk looks
// like, how the controller process itself is holding up — and only then the
// inventory, which is a screen elsewhere in the UI and a slow-moving line
// here. Panels whose series never move on a working box sit at the bottom
// rather than in the first two rows, where they crowd out the ones that do.
func controllerPanels() []panelSpec {
	zero, one, hundred := 0.0, 1.0, 100.0

	return []panelSpec{
		{
			// The panel that says WHICH service is down rather than how many,
			// which is why it leads. Pinned to 0..1 because that is the
			// metric's whole range, and an autoscaled binary series draws a
			// healthy box as a wall of noise around 1.0.
			Title: "Service Health",
			Unit:  "short",
			Min:   &zero,
			Max:   &one,
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_system_unit_active"), Legend: "{{unit}}"},
				{Expr: controllerGauge("townos_package_unit_active"), Legend: "{{unit}}"},
			},
		},
		{
			// System and package units on one panel, unstacked: they are two
			// separate totals, and stacking them would draw a combined height
			// that counts nothing anybody administers.
			Title: "Service Units by State",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_system_units"), Legend: "system {{state}}"},
				{Expr: controllerGauge("townos_package_units"), Legend: "package {{state}}"},
			},
		},
		{
			// By status class, not a bare request total: the total alone
			// cannot tell a busy control plane from one 500ing every call,
			// and those are the same line.
			Title:   "API Requests by Status",
			Unit:    "reqps",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerSumBy("status", "townos_http_requests_total"), Legend: "{{status}}"},
			},
		},
		{
			// Mean seconds per request, by method: the seconds counter divided
			// by the request counter that shares its labels. It is the panel
			// that separates "the box is busy" from "the box is stuck", which
			// the request-rate panel above cannot do — a control plane serving
			// two requests a second answers identically whether each takes 5ms
			// or 5s.
			//
			// A mean rather than a quantile because the exposition here carries
			// no histograms: buckets would be a second family per route and a
			// bucket layout guessed in advance, for a box whose API is answered
			// by one process a household is waiting on. The mean moves when
			// something starts blocking, which is the question being asked.
			Title: "API Latency",
			Unit:  "s",
			Queries: []panelQuery{
				{
					Expr:   controllerRatio("method", "townos_http_request_seconds_total", "townos_http_requests_total"),
					Legend: "{{method}}",
				},
			},
		},
		{
			// The two counts that mean something is wrong right now, on one
			// panel because an operator checking "is anything broken" should
			// not have to know which subsystem to look under. Both are gauges
			// over a recent window, so a return to zero is a recovery rather
			// than a counter that stopped climbing.
			Title: "Recent Failures",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_audit_recent_errors"), Legend: "Audit failures (5m)"},
				{Expr: controllerGauge("townos_repository_errors"), Legend: "Repository refresh errors"},
			},
		},
		{
			Title:   "Audit Events",
			Unit:    "reqps",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerSumBy("result", "townos_audit_events_total"), Legend: "{{result}}"},
			},
		},
		{
			// Used and available rather than used and total: stacked, the two
			// sum to the filesystem size, so the panel shows both the fill and
			// the headroom without a third series restating either.
			Title:   "Town OS Disk Usage",
			Unit:    "bytes",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_disk_used_bytes"), Legend: "Used"},
				{Expr: controllerGauge("townos_disk_available_bytes"), Legend: "Available"},
			},
		},
		{
			// The same disk as a percentage, pinned to 0..100, because the
			// bytes panel beside it cannot answer "how close is this to full"
			// without the reader doing arithmetic against an axis whose scale
			// depends on the box. A pinned axis also makes the slope readable:
			// on an autoscaled percentage, a climb from 4% to 5% and one from
			// 90% to 99% look the same.
			Title: "Town OS Disk Fill",
			Unit:  "percent",
			Min:   &zero,
			Max:   &hundred,
			Queries: []panelQuery{
				{Expr: controllerPercentExpr("townos_disk_used_bytes", "townos_disk_total_bytes"), Legend: "Used"},
			},
		},
		{
			// Not pinned to 100: this is CPU per core-second, so a controller
			// genuinely using two cores reads 200, and an axis capped at 100
			// would silently clip exactly the runaway it exists to show.
			Title: "Controller CPU",
			Unit:  "percent",
			Min:   &zero,
			Queries: []panelQuery{
				{Expr: "100 * " + controllerRate("townos_process_cpu_seconds_total"), Legend: "CPU"},
			},
		},
		{
			// Heap beside resident set, because the gap between them is the
			// diagnosis: heap climbing means the controller is holding objects
			// it should have dropped, while RSS climbing over a flat heap means
			// the memory went somewhere the Go allocator does not account for —
			// cgo, or the journal reader's buffers.
			Title: "Controller Memory",
			Unit:  "bytes",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_memory_heap_bytes"), Legend: "Heap"},
				{Expr: controllerGauge("townos_memory_rss_bytes"), Legend: "Resident"},
			},
		},
		{
			// Three counts that are flat on a healthy box and climb without
			// bound on a leaking one. They share a panel because they usually
			// move together — a handler that never returns holds a goroutine, a
			// descriptor, and an in-flight request each — and because any one
			// of them alone is a single line an operator would not open a tab
			// to see.
			Title: "Controller Concurrency",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_goroutines"), Legend: "Goroutines"},
				{Expr: controllerGauge("townos_open_files"), Legend: "Open files"},
				{Expr: controllerGauge("townos_http_requests_in_flight"), Legend: "Requests in flight"},
			},
		},
		{
			// Uptime is the restart detector: the sawtooth is the signal, not
			// the height. A controller that is quietly crash-looping under
			// Restart=always looks healthy on every other panel here.
			Title: "Controller Uptime",
			Unit:  "s",
			Queries: []panelQuery{
				{Expr: controllerUptimeExpr(), Legend: "Uptime"},
			},
		},
		{
			// One inventory panel rather than two, and without the catalogue
			// size: installed packages, the upgrades waiting, the repositories
			// they come from, and the subvolumes underneath them are all counts
			// in the tens, so they share an axis legibly and answer "what is on
			// this box" in one place.
			Title: "Inventory",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_packages_installed"), Legend: "Installed packages"},
				{Expr: controllerGauge("townos_upgrades_available"), Legend: "Upgradable"},
				{Expr: controllerGauge("townos_repositories"), Legend: "Repositories"},
				{Expr: controllerGauge("townos_filesystems"), Legend: "{{state}} subvolumes"},
			},
		},
		{
			// Unstacked, unlike before: the kinds do partition the account
			// list, but the grant count is a subset of the user bucket, and a
			// stacked panel would draw it as a fourth kind sitting on top —
			// making the stack height larger than the number of accounts that
			// exist. Unstacked, the subset reads as what it is.
			Title: "Accounts",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_accounts"), Legend: "{{kind}}"},
				{Expr: controllerGauge("townos_accounts_granted"), Legend: "holding a grant"},
			},
		},
	}
}

// ControllerDashboard returns the Grafana dashboard for the system
// controller itself.
//
// It is a third dashboard rather than more panels on the overview for the
// same reason the DNS one is separate: the overview answers "how is the
// host" (disk, network, CPU, memory) and this answers "how is Town OS" —
// which units are up, what the API is serving, how the controller process
// itself is holding up. They are read at different times, and folding fourteen
// panels into the overview would bury the four host panels that are the reason
// anyone opens it.
func ControllerDashboard() string {
	return buildDashboard(ControllerDashboardUID, "Town OS Controller", controllerPanels())
}
