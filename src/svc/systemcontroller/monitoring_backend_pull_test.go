// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// newMonitoringBackendServer returns a serverBase with just enough wiring for
// RefreshMonitoringBackend: a mock systemd to record the unit swap, a mock
// btrfs for Grafana's storage, and a real temp dir for the provisioning files.
func newMonitoringBackendServer(t *testing.T) (*serverBase, *systemd.MockManager) {
	t.Helper()
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	return &serverBase{ServerConfig: ServerConfig{
		Storage:                storage.InitBtrFSFromController(btrfsBase, storage.InitBtrFSMockController()),
		Systemd:                sd,
		BtrfsBasePath:          btrfsBase,
		NetworkControllerImage: "nc:testtag",
		MonitoringBackend:      monitoring.BackendUPlot,
	}}, sd
}

func monitoringUIUnit(t *testing.T, sd *systemd.MockManager) string {
	t.Helper()
	return sd.InstalledUnits[systemd.SystemServiceUnitName("monitoring-ui")]
}

// Switching to Grafana on a box that does not have the image must not swap the
// unit until the image is on disk. The unit is Type=simple, so systemd calls it
// started the instant podman forks: swap first and the box reports Grafana up
// while ~771MB is still downloading, and the dashboard frames a port that is
// answering with nothing (or, worse, with the old backend).
func TestMonitoringBackendSwitchToGrafanaWaitsForTheImage(t *testing.T) {
	s, sd := newMonitoringBackendServer(t)

	// uPlot first, so there is a previous backend to keep serving.
	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendUPlot); err != nil {
		t.Fatalf("RefreshMonitoringBackend(uplot): %v", err)
	}
	if !strings.Contains(monitoringUIUnit(t, sd), "socat") {
		t.Fatalf("expected the socat unit installed first, got:\n%s", monitoringUIUnit(t, sd))
	}

	release := make(chan struct{})
	pulling := make(chan string, 1)
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return false }))
	t.Cleanup(TestSetPullImage(func(ctx context.Context, image string) error {
		pulling <- image
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendGrafana); err != nil {
		t.Fatalf("RefreshMonitoringBackend(grafana): %v", err)
	}

	select {
	case img := <-pulling:
		if img != monitoring.GrafanaImage {
			t.Fatalf("pulled %q, want %q", img, monitoring.GrafanaImage)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the switch never started pulling the grafana image")
	}

	// Mid-pull: the old unit is still the one installed, and the status the UI
	// polls must not claim Grafana is up yet.
	if unit := monitoringUIUnit(t, sd); !strings.Contains(unit, "socat") {
		t.Fatalf("the previous backend must keep serving until the image lands, got:\n%s", unit)
	}
	if !s.MonitoringUIPending() {
		t.Fatal("MonitoringUIPending must be true while the image is downloading")
	}

	close(release)

	deadline := time.Now().Add(10 * time.Second)
	for s.MonitoringUIPending() {
		if time.Now().After(deadline) {
			t.Fatal("the pending flag never cleared after the pull finished")
		}
		time.Sleep(10 * time.Millisecond)
	}

	unit := monitoringUIUnit(t, sd)
	if !strings.Contains(unit, monitoring.GrafanaImage) {
		t.Fatalf("expected the grafana unit after the pull, got:\n%s", unit)
	}
	if s.MonitoringUIPending() {
		t.Fatal("MonitoringUIPending must be false once the unit has been swapped")
	}
}

// The download can outlive the decision: an operator who switches to Grafana,
// changes their mind, and switches back to uPlot must not have Grafana install
// itself over the uPlot unit when the pull finishes minutes later.
func TestMonitoringBackendSwitchDoesNotUndoALaterSwitchBack(t *testing.T) {
	s, sd := newMonitoringBackendServer(t)

	release := make(chan struct{})
	pulling := make(chan string, 1)
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return false }))
	t.Cleanup(TestSetPullImage(func(ctx context.Context, image string) error {
		pulling <- image
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))

	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendGrafana); err != nil {
		t.Fatalf("RefreshMonitoringBackend(grafana): %v", err)
	}
	select {
	case <-pulling:
	case <-time.After(10 * time.Second):
		t.Fatal("the switch never started pulling the grafana image")
	}

	// Switch back while the image is still downloading. This one has nothing
	// to fetch, so it installs the socat unit immediately.
	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendUPlot); err != nil {
		t.Fatalf("RefreshMonitoringBackend(uplot): %v", err)
	}
	if unit := monitoringUIUnit(t, sd); !strings.Contains(unit, "socat") {
		t.Fatalf("expected the socat unit after switching back, got:\n%s", unit)
	}

	close(release)

	deadline := time.Now().Add(10 * time.Second)
	for s.MonitoringUIPending() {
		if time.Now().After(deadline) {
			t.Fatal("the pending flag never cleared after the pull finished")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if unit := monitoringUIUnit(t, sd); strings.Contains(unit, monitoring.GrafanaImage) {
		t.Fatalf("the finished grafana pull overwrote the operator's switch back to uPlot:\n%s", unit)
	}
}

// A failed pull must not leave the flag set: the status endpoint reads it, and
// a stuck "true" is a dashboard that says "starting up" forever.
func TestMonitoringBackendSwitchClearsPendingWhenThePullFails(t *testing.T) {
	s, _ := newMonitoringBackendServer(t)

	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return false }))
	t.Cleanup(TestSetPullImage(func(_ context.Context, _ string) error {
		return context.DeadlineExceeded
	}))

	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendGrafana); err != nil {
		t.Fatalf("RefreshMonitoringBackend(grafana): %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for s.MonitoringUIPending() {
		if time.Now().After(deadline) {
			t.Fatal("the pending flag never cleared after the pull failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The image already being present is the common case (a re-switch, or a box
// that booted with grafana selected and pulled it in the boot set). That path
// stays synchronous: the caller gets the unit swapped, and any error, before it
// returns.
func TestMonitoringBackendSwitchIsSynchronousWhenTheImageIsPresent(t *testing.T) {
	s, sd := newMonitoringBackendServer(t)

	var pulled []string
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return true }))
	t.Cleanup(TestSetPullImage(func(_ context.Context, image string) error {
		pulled = append(pulled, image)
		return nil
	}))

	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendGrafana); err != nil {
		t.Fatalf("RefreshMonitoringBackend(grafana): %v", err)
	}
	if unit := monitoringUIUnit(t, sd); !strings.Contains(unit, monitoring.GrafanaImage) {
		t.Fatalf("expected the grafana unit installed before the call returned, got:\n%s", unit)
	}
	if s.MonitoringUIPending() {
		t.Fatal("nothing was fetched, so nothing is pending")
	}
	if len(pulled) != 0 {
		t.Fatalf("an image already on the box must not be re-pulled by the switch, pulls = %v", pulled)
	}
}

// Switching to uPlot never fetches anything: it runs the NC image, which the
// boot pull set already covers.
func TestMonitoringBackendSwitchToUPlotNeverPulls(t *testing.T) {
	s, sd := newMonitoringBackendServer(t)

	var pulled []string
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return false }))
	t.Cleanup(TestSetPullImage(func(_ context.Context, image string) error {
		pulled = append(pulled, image)
		return nil
	}))

	if err := s.RefreshMonitoringBackend(t.Context(), monitoring.BackendUPlot); err != nil {
		t.Fatalf("RefreshMonitoringBackend(uplot): %v", err)
	}
	if unit := monitoringUIUnit(t, sd); !strings.Contains(unit, "socat") {
		t.Fatalf("expected the socat unit, got:\n%s", unit)
	}
	if len(pulled) != 0 {
		t.Fatalf("the uPlot switch must not pull, pulls = %v", pulled)
	}
}
