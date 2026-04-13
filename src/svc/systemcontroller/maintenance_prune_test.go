package systemcontroller

import (
	"context"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

func TestGeneratePodmanPruneUnitsContent(t *testing.T) {
	service, timer := GeneratePodmanPruneUnits()

	if service.Name != "town-os-system--podman-prune.service" {
		t.Errorf("service name = %q", service.Name)
	}
	if timer.Name != "town-os-system--podman-prune.timer" {
		t.Errorf("timer name = %q", timer.Name)
	}

	wantInService := []string{
		"Type=oneshot",
		"ExecStart=/usr/bin/podman system prune -af",
		"ExecStart=/usr/bin/podman image prune -af",
		"Environment=CONTAINER_HOST=unix:///run/podman/podman.sock",
		"After=network-online.target podman.socket",
	}
	for _, want := range wantInService {
		if !strings.Contains(service.Content, want) {
			t.Errorf("service missing %q:\n%s", want, service.Content)
		}
	}

	wantInTimer := []string{
		"OnCalendar=*-*-* 03:17:00",
		"Persistent=true",
		"Unit=town-os-system--podman-prune.service",
		"WantedBy=timers.target",
	}
	for _, want := range wantInTimer {
		if !strings.Contains(timer.Content, want) {
			t.Errorf("timer missing %q:\n%s", want, timer.Content)
		}
	}
}

func TestInstallPodmanPruneUnitsWritesAndEnablesTimer(t *testing.T) {
	sd := systemd.InitMockManager()

	if err := InstallPodmanPruneUnits(context.Background(), sd); err != nil {
		t.Fatalf("InstallPodmanPruneUnits: %v", err)
	}

	// Both unit files must be installed.
	if _, err := sd.ReadUnit(PodmanPruneServiceName); err != nil {
		t.Errorf("service not installed: %v", err)
	}
	if _, err := sd.ReadUnit(PodmanPruneTimerName); err != nil {
		t.Errorf("timer not installed: %v", err)
	}

	// The timer must be enabled and started; the service must not be
	// directly started (the timer owns that lifecycle).
	var (
		timerEnabled bool
		timerStarted bool
		serviceStart bool
	)
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" {
			continue
		}
		unit, ok := call.Args[0].(string)
		if !ok {
			continue
		}
		action, ok := call.Args[1].(systemd.StatusAction)
		if !ok {
			continue
		}
		switch {
		case unit == PodmanPruneTimerName && action == systemd.Enable:
			timerEnabled = true
		case unit == PodmanPruneTimerName && action == systemd.Start:
			timerStarted = true
		case unit == PodmanPruneServiceName && action == systemd.Start:
			serviceStart = true
		}
	}
	if !timerEnabled {
		t.Error("timer was not enabled")
	}
	if !timerStarted {
		t.Error("timer was not started")
	}
	if serviceStart {
		t.Error("service was directly started; the timer should own that")
	}
}

func TestInstallPodmanPruneUnitsNilSystemd(t *testing.T) {
	if err := InstallPodmanPruneUnits(context.Background(), nil); err != nil {
		t.Errorf("nil systemd should be a no-op, got %v", err)
	}
}
