package systemcontroller

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/metrics"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// collectMetrics gathers one scrape.
//
// Every section is independent and skipped on error: a metrics endpoint that
// fails as a unit is useless in the situation it exists for. Each collector
// also tolerates a nil manager, because a test server — and a controller still
// booting — has most of them unset, and a scrape during boot should report what
// is up rather than nothing.
func (s *SystemControllerHandlers) collectMetrics(ctx context.Context) []metrics.Metric {
	families := []metrics.Metric{
		metrics.Gauge("townos_up",
			"Always 1 while the system controller is serving. Absent when it is not.", 1),
		metrics.Gauge("townos_start_time_seconds",
			"Unix time at which this system controller process started.", unixSeconds(processStart)),
	}

	counters := s.metricsCounters()
	families = append(families, counters.auditEvents.Collect(), counters.httpRequests.Collect())

	families = append(families, s.collectUnitMetrics(ctx)...)
	families = append(families, s.collectPackageMetrics()...)
	families = append(families, s.collectAccountMetrics()...)
	families = append(families, s.collectStorageMetrics()...)
	families = append(families, s.collectAuditMetrics()...)

	return families
}

// collectUnitMetrics reports how many package and system-service units exist
// and what state they are in — the headline "what is running" question.
//
// Package units are filtered against the installed set exactly as the status
// ping filters them, so a leftover unit from an uninstalled package does not
// inflate the count and make the box look like it is running something it is
// not.
func (s *SystemControllerHandlers) collectUnitMetrics(ctx context.Context) []metrics.Metric {
	sd := s.Controller.GetSystemdManager()
	if sd == nil {
		return nil
	}
	units, err := sd.ListUnits(ctx)
	if err != nil {
		slog.Error("metrics: listing units", "error", err)
		return nil
	}

	installedUnits := map[string]struct{}{}
	if inst := s.Controller.GetInstaller(); inst != nil {
		pkgs, listErr := inst.ListInstalled()
		if listErr != nil {
			slog.Error("metrics: listing installed packages", "error", listErr)
		}
		for _, pkg := range pkgs {
			pi, parseErr := packages.ParsePackageIdentity(pkg)
			if parseErr != nil {
				continue
			}
			installedUnits[systemd.UnitName(pi.Repo, pi.Name, pi.Version)] = struct{}{}
		}
	}

	pkgStates := map[string]float64{"active": 0, "failed": 0, "inactive": 0}
	sysStates := map[string]float64{"active": 0, "failed": 0, "inactive": 0}
	// Per-unit state, so an operator can see WHICH service is down rather than
	// only that one is. Cardinality is bounded by what the box has installed,
	// which for a household box is tens, not thousands.
	var pkgUnitSamples []metrics.Sample
	var sysUnitSamples []metrics.Sample

	for _, u := range units {
		state := bucketActiveState(u.ActiveState)
		if systemd.IsSystemServiceUnit(u.Name) {
			// Skip NC and socket units: they are plumbing for a service that is
			// counted in its own right, and counting them would make a healthy
			// box look like it runs three times the services it does.
			if strings.HasSuffix(u.Name, "-network.service") || strings.HasSuffix(u.Name, ".socket") {
				continue
			}
			sysStates[state]++
			sysUnitSamples = append(sysUnitSamples,
				metrics.Labelled(boolValue(u.ActiveState == "active"), "unit", u.Name))
			continue
		}
		if !systemd.IsPackageServiceUnit(u.Name) {
			continue
		}
		if _, ok := installedUnits[u.Name]; !ok {
			continue
		}
		pkgStates[state]++
		pkgUnitSamples = append(pkgUnitSamples,
			metrics.Labelled(boolValue(u.ActiveState == "active"), "unit", u.Name))
	}

	return []metrics.Metric{
		metrics.GaugeVec("townos_package_units",
			"Installed package service units by systemd active state.", stateSamples(pkgStates)),
		metrics.GaugeVec("townos_system_units",
			"Town OS system service units by systemd active state.", stateSamples(sysStates)),
		metrics.GaugeVec("townos_package_unit_active",
			"1 when an installed package's service unit is active, 0 otherwise.", pkgUnitSamples),
		metrics.GaugeVec("townos_system_unit_active",
			"1 when a Town OS system service unit is active, 0 otherwise.", sysUnitSamples),
	}
}

// bucketActiveState folds systemd's active states into the three an operator
// acts on. Anything transitional (activating, deactivating, reloading) counts
// as inactive rather than getting its own label: a unit mid-restart is not
// serving, and a transient label would otherwise appear and vanish between
// scrapes as its own series.
func bucketActiveState(state string) string {
	switch state {
	case "active", "failed":
		return state
	}
	return "inactive"
}

// stateSamples renders the fixed state buckets in a stable order.
//
// The order is spelled out rather than taken from map iteration so a scrape is
// byte-stable, and all three are emitted even at zero: a gauge that vanishes
// when it reaches zero is indistinguishable from one the box stopped reporting,
// so "no failed units" would look exactly like "unit collection is broken".
func stateSamples(counts map[string]float64) []metrics.Sample {
	out := make([]metrics.Sample, 0, 3)
	for _, state := range []string{"active", "failed", "inactive"} {
		out = append(out, metrics.Labelled(counts[state], "state", state))
	}
	return out
}

func boolValue(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// collectPackageMetrics reports the package inventory: what is installed, what
// the repositories offer, what is upgradable, and which repositories are
// failing to refresh.
func (s *SystemControllerHandlers) collectPackageMetrics() []metrics.Metric {
	var out []metrics.Metric

	var installed []string
	if inst := s.Controller.GetInstaller(); inst != nil {
		pkgs, err := inst.ListInstalled()
		if err != nil {
			slog.Error("metrics: listing installed packages", "error", err)
		} else {
			installed = pkgs
			out = append(out, metrics.Gauge("townos_packages_installed",
				"Packages currently installed.", float64(len(pkgs))))
		}
	}

	if rr := s.Controller.GetRepositoryRoot(); rr != nil {
		if repos, err := rr.List(); err != nil {
			slog.Error("metrics: listing repositories", "error", err)
		} else {
			out = append(out, metrics.Gauge("townos_repositories",
				"Configured package repositories.", float64(len(repos))))
		}
		if pkgs, err := rr.ListPackages(); err != nil {
			slog.Error("metrics: listing available packages", "error", err)
		} else {
			out = append(out, metrics.Gauge("townos_packages_available",
				"Packages offered across all configured repositories.", float64(len(pkgs))))
		}
		// Counted, not labelled by repository name: the useful alert is "any
		// repository is failing", and the name is in the status ping and the
		// repositories screen. A label here would also mint a permanent series
		// per repository that ever broke.
		out = append(out, metrics.Gauge("townos_repository_errors",
			"Repositories whose last refresh failed.", float64(len(rr.RefreshErrors()))))
	}

	if installed != nil {
		out = append(out, metrics.Gauge("townos_upgrades_available",
			"Installed packages with a newer version available.",
			float64(len(s.computeUpgradesFromList(installed)))))
	}

	return out
}

// collectAccountMetrics reports who can log in. Disabled accounts are their own
// bucket rather than being dropped, so an account that was disabled is visibly
// still present rather than appearing to have been deleted.
func (s *SystemControllerHandlers) collectAccountMetrics() []metrics.Metric {
	am := s.Controller.GetAccountManager()
	if am == nil {
		return nil
	}
	list, err := am.List()
	if err != nil {
		slog.Error("metrics: listing accounts", "error", err)
		return nil
	}

	var admins, users, disabled, granted float64
	for _, a := range list {
		switch {
		case a.Disabled:
			disabled++
		case a.Admin:
			admins++
		default:
			users++
		}
		if a.Restricted() {
			granted++
		}
	}

	return []metrics.Metric{
		metrics.GaugeVec("townos_accounts", "Accounts by kind.", []metrics.Sample{
			metrics.Labelled(admins, "kind", "admin"),
			metrics.Labelled(users, "kind", "user"),
			metrics.Labelled(disabled, "kind", "disabled"),
		}),
		metrics.Gauge("townos_accounts_granted",
			"Non-admin accounts holding at least one grant.", granted),
	}
}

// collectStorageMetrics reports subvolume counts by state and the disk the
// whole box runs on — the number a household operator actually runs out of.
func (s *SystemControllerHandlers) collectStorageMetrics() []metrics.Metric {
	st := s.Controller.GetStorage()
	if st == nil {
		return nil
	}
	var out []metrics.Metric

	// FilesystemNames, not ListFilesystems: the latter shells out to
	// `btrfs qgroup show` once per subvolume, which cost the status ping about
	// a second at ~30 subvolumes. A scrape every 15s must not pay that for a
	// quota this never reads.
	if names, err := st.FilesystemNames(""); err != nil {
		slog.Error("metrics: listing filesystem names", "error", err)
	} else {
		counts := map[string]float64{}
		for _, name := range names {
			state, _ := classifyFilesystem(name)
			counts[state]++
		}
		out = append(out, metrics.GaugeVec("townos_filesystems",
			"btrfs subvolumes by namespace.", []metrics.Sample{
				metrics.Labelled(counts["user"], "state", "user"),
				metrics.Labelled(counts["installed"], "state", "installed"),
				metrics.Labelled(counts["uninstalled"], "state", "uninstalled"),
			}))
	}

	if du, err := st.DiskUsage(); err != nil {
		slog.Error("metrics: reading disk usage", "error", err)
	} else {
		out = append(out,
			metrics.Gauge("townos_disk_total_bytes", "Total bytes on the Town OS filesystem.", float64(du.Total)),
			metrics.Gauge("townos_disk_used_bytes", "Used bytes on the Town OS filesystem.", float64(du.Used)),
			metrics.Gauge("townos_disk_available_bytes", "Available bytes on the Town OS filesystem.", float64(du.Available)),
		)
	}

	return out
}

// collectAuditMetrics reports the recent-failure count the dashboard renders as
// its red pill, so an alert can fire on the same number a person would see.
func (s *SystemControllerHandlers) collectAuditMetrics() []metrics.Metric {
	am := s.Controller.GetAuditManager()
	if am == nil {
		return nil
	}
	n, err := am.CountRecentErrors(time.Now().Add(-5 * time.Minute))
	if err != nil {
		slog.Error("metrics: counting recent audit errors", "error", err)
		return nil
	}
	return []metrics.Metric{
		metrics.Gauge("townos_audit_recent_errors",
			"Failed audited actions in the last 5 minutes.", float64(n)),
	}
}
