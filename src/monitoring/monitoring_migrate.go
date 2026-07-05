package monitoring

import (
	"context"
	"log/slog"

	"gitea.com/town-os/town-os/src/systemd"
)

// legacyMonitoringUnits returns the systemd unit names created by the previous
// (network-controller + socket-activation) monitoring design for Prometheus and
// the monitoring UI. The current design runs both as plain host-networked
// system services with no NC and no socket, so on an in-place upgrade these
// leftover units MUST be torn down: their NC containers keep -p 9090:9090 /
// -p 5308:5308 bound, and the new host-net Prometheus / socat then crash-loop
// with "listen tcp 127.0.0.1:9090: bind: address already in use".
func legacyMonitoringUnits() []string {
	prom := systemd.SystemServiceContainerName("prometheus")  // town-os-system--prometheus
	ui := systemd.SystemServiceContainerName("monitoring-ui") // town-os-system--monitoring-ui
	return []string{
		prom + "-network.service",
		prom + "-" + PrometheusPort + "-tcp.socket",
		ui + "-network.service",
		ui + "-" + MonitoringExternalPort + "-tcp.socket",
	}
}

// CleanupUnits stops, disables, and removes each named unit, best-effort. It is
// idempotent: units that are already gone (Stop/Disable report "not loaded",
// UninstallUnit's os.Remove reports "not exist") are skipped. Every failure is
// logged at debug rather than returned, so a partial cleanup never blocks the
// caller. Stop is issued first so a running unit releases any published host
// port before the file is removed.
func CleanupUnits(ctx context.Context, sd systemd.Manager, units []string) {
	for _, unit := range units {
		if err := sd.SetStatus(ctx, unit, systemd.Stop); err != nil {
			slog.Debug("stop obsolete unit (may be absent)", "unit", unit, "error", err)
		}
		if err := sd.SetStatus(ctx, unit, systemd.Disable); err != nil {
			slog.Debug("disable obsolete unit (may be absent)", "unit", unit, "error", err)
		}
		if err := sd.UninstallUnit(ctx, unit); err != nil {
			slog.Debug("remove obsolete unit (may be absent)", "unit", unit, "error", err)
		}
	}
}

// CleanupLegacyMonitoringUnits tears down the obsolete NC and socket units from
// the previous monitoring design. It MUST run before StartPrometheus and
// StartMonitoringUI so the legacy NC releases :9090 / :5308 before the new
// host-net services bind them.
func CleanupLegacyMonitoringUnits(ctx context.Context, sd systemd.Manager) {
	CleanupUnits(ctx, sd, legacyMonitoringUnits())
}
