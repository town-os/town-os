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
// townos_up and townos_disk_total_bytes are deliberately absent. The first is
// a scrape-liveness constant — a flat line at 1 is not a panel — and the
// second is the sum of the two series the disk panel already stacks, so
// graphing it would draw the total twice.
func ControllerDashboardMetrics() []string {
	return []string{
		"townos_system_units",
		"townos_package_units",
		"townos_system_unit_active",
		"townos_package_unit_active",
		"townos_http_requests_total",
		"townos_audit_events_total",
		"townos_audit_recent_errors",
		"townos_repository_errors",
		"townos_packages_installed",
		"townos_packages_available",
		"townos_upgrades_available",
		"townos_repositories",
		"townos_disk_used_bytes",
		"townos_disk_available_bytes",
		"townos_accounts",
		"townos_accounts_granted",
		"townos_filesystems",
		"townos_start_time_seconds",
	}
}

// controllerPanels is the controller dashboard's panel set, in grid order
// (two to a row). The uPlot frontend renders the same panels from the same
// expressions in ui/src/components/monitoring/queries.js.
func controllerPanels() []panelSpec {
	zero, one := 0.0, 1.0

	return []panelSpec{
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
			// The panel that says WHICH service is down rather than how many.
			// Pinned to 0..1 because that is the metric's whole range, and an
			// autoscaled binary series draws a healthy box as a wall of noise
			// around 1.0.
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
			Title:   "Audit Events",
			Unit:    "reqps",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerSumBy("result", "townos_audit_events_total"), Legend: "{{result}}"},
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
			Title: "Package Inventory",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_packages_installed"), Legend: "Installed"},
				{Expr: controllerGauge("townos_packages_available"), Legend: "Available"},
				{Expr: controllerGauge("townos_upgrades_available"), Legend: "Upgradable"},
				{Expr: controllerGauge("townos_repositories"), Legend: "Repositories"},
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
			// Stacked because the kinds partition the account list exactly
			// once, so the stack height is the real total. Granted rides along
			// unstacked-in-meaning but not in render — it is a subset of user,
			// which is why it is legended as a subset rather than a kind.
			Title:   "Accounts",
			Unit:    "short",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_accounts"), Legend: "{{kind}}"},
			},
		},
		{
			Title: "Granted Accounts",
			Unit:  "short",
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_accounts_granted"), Legend: "Holding a grant"},
			},
		},
		{
			Title:   "btrfs Subvolumes",
			Unit:    "short",
			Stacked: true,
			Queries: []panelQuery{
				{Expr: controllerGauge("townos_filesystems"), Legend: "{{state}}"},
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
	}
}

// ControllerDashboard returns the Grafana dashboard for the system
// controller itself.
//
// It is a third dashboard rather than more panels on the overview for the
// same reason the DNS one is separate: the overview answers "how is the
// host" (disk, network, CPU, memory) and this answers "how is Town OS" —
// which units are up, what the API is serving, what the inventory looks
// like. They are read at different times, and folding eleven panels into the
// overview would bury the four host panels that are the reason anyone opens
// it.
func ControllerDashboard() string {
	return buildDashboard(ControllerDashboardUID, "Town OS Controller", controllerPanels())
}
