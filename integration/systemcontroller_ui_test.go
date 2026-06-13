// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
)

func uiTestImage(t *testing.T) string {
	t.Helper()
	img := os.Getenv("UI_IMAGE")
	if img == "" {
		// The UI test image is built locally from Containerfile.ui by the
		// test harness (make ui-image) and injected via UI_IMAGE; the quay
		// tags are production-only (per-arch latest-x86_64 / latest-aarch64),
		// so there is no plain pullable fallback. rc.latest must never be
		// used for testing.
		t.Skip("UI_IMAGE not set; run via make test-integration")
	}
	ensureImagePulled(img)
	return img
}

func TestUIContainerRealStartAndAccessible(t *testing.T) {
	t.Parallel()
	sd := systemd.NewManager()
	mgr := ui.NewManager(ui.Config{
		Systemd: sd,
		Image:   uiTestImage(t),
	})

	ctx := context.Background()
	if err := mgr.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		unitName := systemd.SystemServiceUnitName("ui")
		if err := sd.SetStatus(ctx, unitName, systemd.Stop); err != nil {
			t.Logf("cleanup SetStatus(stop): %v", err)
		}
		if err := sd.UninstallUnit(ctx, unitName); err != nil {
			t.Logf("cleanup UninstallUnit: %v", err)
		}
	})

	var status ui.Status
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		status = mgr.Status(ctx)
		if status.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !status.Running {
		t.Fatal("expected UI running after Start")
	}
}
