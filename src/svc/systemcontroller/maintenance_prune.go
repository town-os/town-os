package systemcontroller

import (
	"context"
	"fmt"

	"gitea.com/town-os/town-os/src/systemd"
)

// Constants for the nightly podman prune units. The `town-os-system--`
// prefix surfaces them in the /system-services API and UI alongside
// Prometheus, Node Exporter, and the monitoring UI.
const (
	PodmanPruneUnitKey     = "podman-prune"
	PodmanPruneServiceName = systemd.SystemServiceUnitPrefix + PodmanPruneUnitKey + ".service"
	PodmanPruneTimerName   = systemd.SystemServiceUnitPrefix + PodmanPruneUnitKey + ".timer"

	// podmanPruneSchedule fires daily at 03:17 local time. The minute
	// offset avoids the top-of-the-hour thundering herd with other
	// system timers; the hour is late enough that an admin will not be
	// observing the machine and early enough that any breakage is
	// visible on the morning dashboard.
	podmanPruneSchedule = "*-*-* 03:17:00"
)

// GeneratePodmanPruneUnits returns the .service + .timer pair that runs
// `podman system prune` and `podman image prune` on the host every night.
// The service is oneshot; the timer is persistent so a power-off window
// catches up on the next boot rather than waiting a full day.
//
// Both files live in systemd's system directory and are managed via the
// systemd.Manager interface (same path every other Town OS unit takes),
// so they compose with daemon-reload and ListUnits exactly like
// package-service units.
func GeneratePodmanPruneUnits() (service systemd.UnitFile, timer systemd.UnitFile) {
	service = systemd.UnitFile{
		Name: PodmanPruneServiceName,
		Content: `[Unit]
Description=Town OS: nightly podman prune
After=network-online.target podman.socket

[Service]
Type=oneshot
Environment=CONTAINER_HOST=unix:///run/podman/podman.sock
ExecStart=/usr/bin/podman system prune -af
ExecStart=/usr/bin/podman image prune -af
`,
	}

	timer = systemd.UnitFile{
		Name: PodmanPruneTimerName,
		Content: fmt.Sprintf(`[Unit]
Description=Town OS: nightly podman prune timer

[Timer]
OnCalendar=%s
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, podmanPruneSchedule, PodmanPruneServiceName),
	}
	return service, timer
}

// InstallPodmanPruneUnits writes the prune service + timer, enables the
// timer so it survives reboots, and starts it so the schedule becomes
// active in the current session without waiting for the next boot.
// Errors from any step are returned; callers treat them as non-fatal at
// boot time (the rest of the system still comes up).
func InstallPodmanPruneUnits(ctx context.Context, sd systemd.Manager) error {
	if sd == nil {
		return nil
	}

	service, timer := GeneratePodmanPruneUnits()

	if err := sd.InstallUnit(ctx, service.Name, service.Content); err != nil {
		return fmt.Errorf("install %s: %w", service.Name, err)
	}
	if err := sd.InstallUnit(ctx, timer.Name, timer.Content); err != nil {
		return fmt.Errorf("install %s: %w", timer.Name, err)
	}

	// Enable the timer so it auto-starts on every boot, and start it
	// now so the schedule takes effect without waiting for a restart.
	// The service itself is not enabled — it is triggered by the timer.
	if err := sd.SetStatus(ctx, timer.Name, systemd.Enable); err != nil {
		return fmt.Errorf("enable %s: %w", timer.Name, err)
	}
	if err := sd.SetStatus(ctx, timer.Name, systemd.Start); err != nil {
		return fmt.Errorf("start %s: %w", timer.Name, err)
	}

	return nil
}
