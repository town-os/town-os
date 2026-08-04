// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/ingress/ingressctl"
	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
)

// unitRestarted reports whether the mock systemd recorded a Restart for unit.
func unitRestarted(sd *systemd.MockManager, unit string) bool {
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" || len(call.Args) != 2 {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok || name != unit {
			continue
		}
		if act, ok := call.Args[1].(systemd.StatusAction); ok && act == systemd.Restart {
			return true
		}
	}
	return false
}

// TestSystemUpdatePullsAndRestartsEverySystemImage drives POST
// /system-services/refresh — the "refresh system services" / system-update
// facility — end-to-end with EVERY system service configured (monitoring's
// node-exporter/prometheus/monitoring-ui, rolodex, ui, ingress, and the
// systemcontroller itself), and asserts the update pulls every one of their
// images and restarts every one of their units.
//
// This is the regression guard for system updates silently skipping a service:
// ingress in particular was omitted from the system-service set, so its image
// was never re-pulled on a system update. All images must be covered.
func TestSystemUpdatePullsAndRestartsEverySystemImage(t *testing.T) {
	t.Parallel()

	// Capture every image the update facility pulls, and the order it pulls
	// them in. rc.latest must never be referenced in tests, so each service gets
	// a neutral fake tag.
	var (
		mu        sync.Mutex
		pulled    = map[string]int{}
		pullOrder []string
	)
	restore := systemcontroller.TestSetPullImage(func(_ context.Context, image string) error {
		mu.Lock()
		pulled[image]++
		pullOrder = append(pullOrder, image)
		mu.Unlock()
		return nil
	})
	t.Cleanup(restore)

	sd := systemd.InitMockManager()
	dataDir := t.TempDir()
	btrfsBase := t.TempDir()

	const (
		uiImg   = "quay.io/town/ui:testtag"
		rolImg  = "quay.io/town/rolodex:testtag"
		ingImg  = "quay.io/town/ingress:testtag"
		ncImg   = "quay.io/town/networkcontroller:testtag"
		townImg = "quay.io/town/town:testtag"
	)

	uiMgr := ui.NewManager(ui.Config{Systemd: sd, Image: uiImg})
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolImg,
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		Key:            "rolodex-updtest",
	})
	ingMgr := ingressctl.NewManager(ingressctl.Config{
		Systemd: sd,
		DataDir: dataDir,
		Image:   ingImg,
		Key:     "ingress-updtest",
	})

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                    storage.InitBtrFSMock(),
		Systemd:                    sd,
		MonitoringBackend:          monitoring.BackendUPlot,
		NetworkControllerImage:     ncImg,
		UI:                         uiMgr,
		Rolodex:                    rolMgr,
		Ingress:                    ingMgr,
		BtrfsBasePath:              btrfsBase,
		SystemControllerImage:      townImg,
		SystemControllerListenAddr: ":5309",
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.Server.URL+"/system-services/refresh", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Logf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Every system service's image must have been pulled. The expected set is
	// derived from the same managers the controller uses, so it tracks any
	// image-path change.
	wantImages := map[string]string{
		"systemcontroller": townImg,
		"rolodex":          rolMgr.SystemServices()[0].Image,
		"ui":               uiMgr.SystemServices()[0].Image,
		"ingress":          ingMgr.SystemServices()[0].Image,
		"node-exporter":    monitoring.NodeExporterSystemService(monitoring.Ports{}).Image,
		"prometheus":       monitoring.PrometheusSystemService(monitoring.Ports{}).Image,
		"monitoring-ui":    monitoring.MonitoringUISystemService(monitoring.BackendUPlot, ncImg, monitoring.Ports{}).Image,
	}

	mu.Lock()
	defer mu.Unlock()
	for svc, img := range wantImages {
		if pulled[img] == 0 {
			t.Errorf("system update did not pull the %s image %q; pulled=%v", svc, img, pulled)
		}
	}

	// Dependency order: the systemcontroller (anchor) is pulled first, rolodex
	// (DNS) second, then everything else.
	if len(pullOrder) < 2 {
		t.Fatalf("expected at least the anchor + rolodex pulled, got %v", pullOrder)
	}
	if pullOrder[0] != townImg {
		t.Errorf("systemcontroller image must be pulled first, got order %v", pullOrder)
	}
	if pullOrder[1] != rolImg {
		t.Errorf("rolodex image must be pulled second, got order %v", pullOrder)
	}
	// Every other service is pulled only after the anchor and rolodex.
	for i, img := range pullOrder {
		if i >= 2 && (img == townImg || img == rolImg) {
			t.Errorf("anchor/rolodex must not be pulled among the others (index %d): %v", i, pullOrder)
		}
	}

	// Every non-controller system-service unit must have been restarted so the
	// freshly-pulled image takes effect. (The controller restarts itself via a
	// delayed self-restart, covered separately.)
	wantRestarts := map[string]string{
		"rolodex":       rolMgr.SystemServices()[0].UnitName,
		"ui":            uiMgr.SystemServices()[0].UnitName,
		"ingress":       ingMgr.SystemServices()[0].UnitName,
		"node-exporter": monitoring.NodeExporterSystemService(monitoring.Ports{}).UnitName,
		"prometheus":    monitoring.PrometheusSystemService(monitoring.Ports{}).UnitName,
		"monitoring-ui": monitoring.MonitoringUISystemService(monitoring.BackendUPlot, ncImg, monitoring.Ports{}).UnitName,
	}
	for svc, unit := range wantRestarts {
		if !unitRestarted(sd, unit) {
			t.Errorf("system update did not restart the %s unit %q", svc, unit)
		}
	}
}
