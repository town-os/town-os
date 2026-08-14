// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// selfUpdateFixture stands up the four seams SelfUpdate touches: what image
// this process is running, what the tag names now, whether a pull happens, and
// a systemd to record the restart.
type selfUpdateFixture struct {
	sd        *systemd.MockManager
	btrfsBase string
	pulled    []string
}

func newSelfUpdateFixture(t *testing.T, ref, runningID, tagID string) *selfUpdateFixture {
	t.Helper()
	f := &selfUpdateFixture{sd: systemd.InitMockManager(), btrfsBase: t.TempDir()}

	t.Cleanup(TestSetSelfImage(ref, runningID))
	t.Cleanup(TestSetLocalImageID(func(_ context.Context, _ string) string { return tagID }))
	// The image is always already present: this is a box that has booted
	// before, which is the only situation self-update applies to.
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return true }))
	t.Cleanup(TestSetPullImage(func(_ context.Context, image string) error {
		f.pulled = append(f.pulled, image)
		return nil
	}))
	return f
}

func (f *selfUpdateFixture) marker(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.btrfsBase, SelfUpdateMarkerFilename))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

const (
	selfRef   = "quay.io/town/town:rc.latest-x86_64"
	oldImage  = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	newImage  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	otherHost = "registry.example:5000/town/town:rc.latest-x86_64"
)

// The case the whole file is about: the box is running the image it installed
// with, the floating tag now names a newer one, so the unit is restarted to
// pick it up. Without this the controller is the one service that can never
// update itself, and a box stays on its install-day image forever.
func TestSelfUpdateRestartsWhenTheTagHasMoved(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, newImage)

	if !SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("expected a restart to be requested")
	}
	if !findRestart(f.sd.GetCalls(), systemd.SystemControllerUnitName) {
		t.Fatalf("expected Restart on %q, got calls: %v", systemd.SystemControllerUnitName, f.sd.GetCalls())
	}
	if len(f.pulled) != 1 || f.pulled[0] != selfRef {
		t.Fatalf("expected the running reference to be refreshed, pulls = %v", f.pulled)
	}
	if got := f.marker(t); got != newImage {
		t.Fatalf("marker = %q, want the image the restart was for (%q)", got, newImage)
	}
}

// The steady state — every boot on a box whose tag has not moved. A restart
// here would be a boot loop, one per boot, forever.
func TestSelfUpdateDoesNothingWhenTheTagStillNamesTheRunningImage(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, oldImage)

	if SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("no restart should be requested when nothing moved")
	}
	if findRestart(f.sd.GetCalls(), systemd.SystemControllerUnitName) {
		t.Fatal("the controller unit must not be restarted when the image is current")
	}
	if got := f.marker(t); got != "" {
		t.Fatalf("no restart happened, so no marker should exist; got %q", got)
	}
}

// A pinned reference is an operator pinning the box (or a localhost image in
// dev and test). There is nothing to refresh and nothing to restart for.
func TestSelfUpdateIgnoresPinnedReferences(t *testing.T) {
	for _, ref := range []string{
		"quay.io/town/town:rc.2026-08-13-x86_64",
		"localhost/town-os-test:abc123",
	} {
		f := newSelfUpdateFixture(t, ref, oldImage, newImage)
		if SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
			t.Fatalf("%s is pinned; no restart should be requested", ref)
		}
		if len(f.pulled) != 0 {
			t.Fatalf("%s is pinned; nothing should be pulled, pulls = %v", ref, f.pulled)
		}
	}
}

// The reference comes from podman, not from TOWN_OS_TAG, so a box installed
// from another registry self-updates against the image it actually runs.
func TestSelfUpdateFollowsTheReferenceTheUnitActuallyRuns(t *testing.T) {
	f := newSelfUpdateFixture(t, otherHost, oldImage, newImage)

	if !SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("expected a restart to be requested")
	}
	if len(f.pulled) != 1 || f.pulled[0] != otherHost {
		t.Fatalf("expected %q to be refreshed, pulls = %v", otherHost, f.pulled)
	}
}

// Not under podman: no container, no image, nothing to compare.
func TestSelfUpdateDoesNothingOutsideAContainer(t *testing.T) {
	f := newSelfUpdateFixture(t, "", "", newImage)

	if SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("no restart should be requested when the running image cannot be detected")
	}
}

// The loop guard. A restart that comes back running the same old image (a unit
// that resolves the tag differently than we do) must be tried once, not on
// every boot for the rest of the box's life.
func TestSelfUpdateWillNotRestartTwiceForTheSameImage(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, newImage)

	if !SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("first call should request the restart")
	}
	f.sd.ClearCalls()

	// Second boot: same running image, same target — the restart did not take.
	if SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("second call must not request another restart for the same image")
	}
	if findRestart(f.sd.GetCalls(), systemd.SystemControllerUnitName) {
		t.Fatal("a restart that did not take must not be retried every boot")
	}
}

// ...but a tag that moves AGAIN is a new target, and gets its own attempt.
func TestSelfUpdateRestartsAgainForANewerImage(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, newImage)
	if !SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("first call should request the restart")
	}
	f.sd.ClearCalls()

	const newerImage = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
	t.Cleanup(TestSetLocalImageID(func(_ context.Context, _ string) string { return newerImage }))

	if !SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("a newer image is a new target and must get its own restart")
	}
	if got := f.marker(t); got != newerImage {
		t.Fatalf("marker = %q, want %q", got, newerImage)
	}
}

// A restart systemd refused never happened, so the marker must not survive to
// suppress the next attempt.
func TestSelfUpdateClearsTheMarkerWhenTheRestartIsRefused(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, newImage)
	f.sd.StatusErr = errors.New("systemd is not accepting jobs")

	if SelfUpdate(t.Context(), f.sd, f.btrfsBase) {
		t.Fatal("a refused restart is not a restart")
	}
	if got := f.marker(t); got != "" {
		t.Fatalf("marker should be cleared when no restart happened, got %q", got)
	}
}

// No btrfs base (in-memory dev mode) means no marker file. The restart still
// has to be requested — the guard is an optimisation against a loop, not a
// precondition for updating.
func TestSelfUpdateStillRestartsWithoutAMarkerPath(t *testing.T) {
	f := newSelfUpdateFixture(t, selfRef, oldImage, newImage)

	if !SelfUpdate(t.Context(), f.sd, "") {
		t.Fatal("expected a restart to be requested with no btrfs base")
	}
}
