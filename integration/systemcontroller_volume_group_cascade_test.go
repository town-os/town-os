// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// TestSystemControllerCascadeVolumeGroupDeleteRealSystemd exercises the
// POST /storage/remove-package-volume-group path end-to-end:
//
//  1. Install a real package (core/nginx@1.0) through the HTTP pipeline.
//     The install creates its systemd unit on real systemd and its volumes
//     on mock btrfs, matching the production flow minus the host
//     filesystem.
//  2. Verify the installed package shows up in both ListPackageVolumes
//     (the UI's "Package Volumes" tree) and ListUnits.
//  3. Call RemovePackageVolumeGroup(repo, name) — the cascade-delete the
//     UI wires to the top-level delete button.
//  4. Assert every package volume for core/nginx is gone and the unit's
//     active state is no longer "active" (the Stop signal made it).
func TestSystemControllerCascadeVolumeGroupDeleteRealSystemd(t *testing.T) {
	t.Parallel()
	c := initSystemControllerInstallRealSystemdTest(t)

	unitName := systemd.UnitName("core", "nginx", "1.0")

	// Unconditional teardown, regardless of where the test fails. The
	// cascade under test is expected to stop/disable the unit, but this
	// belt-and-braces pass mops up units installed before the cascade
	// ran or left behind by a mid-test panic.
	t.Cleanup(func() {
		cleanup := systemd.NewManager()
		ctx := context.Background()
		for _, name := range systemd.PackageUnitNames("core", "nginx", "1.0", packages.PortMap{8180: 80}, packages.PortMap{}) {
			if err := cleanup.SetStatus(ctx, name, systemd.Stop); err != nil {
				t.Logf("cleanup stop %s: %v", name, err)
			}
			if err := cleanup.SetStatus(ctx, name, systemd.Disable); err != nil {
				t.Logf("cleanup disable %s: %v", name, err)
			}
			if err := cleanup.UninstallUnit(ctx, name); err != nil {
				t.Logf("cleanup uninstall %s: %v", name, err)
			}
		}
		// Also remove the on-disk unit file just in case.
		_ = os.Remove("/etc/systemd/system/" + unitName) //nolint:errcheck // best-effort cleanup
	})

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0 — creates real systemd unit plus mock-btrfs
	// subvolume for its declared html volume.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "cascade-test",
		"port":     "8180",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Volume must be visible in the UI's grouping before the cascade runs.
	groups, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes before cascade: %v", err)
	}
	var foundGroup bool
	for _, g := range groups {
		if g.Repo == "core" && g.EffectiveName == "nginx" {
			foundGroup = true
			if len(g.Volumes) == 0 {
				t.Fatalf("expected at least one volume in core/nginx group, got none")
			}
			break
		}
	}
	if !foundGroup {
		t.Fatalf("expected core/nginx in ListPackageVolumes before cascade, got %+v", groups)
	}

	// Cascade: stop the unit + purge every volume under
	// installed/core/nginx/.
	if err := c.RemovePackageVolumeGroup(context.TODO(), "core", "nginx", "", false); err != nil {
		t.Fatalf("RemovePackageVolumeGroup: %v", err)
	}

	// No package-volume group should remain for core/nginx.
	groupsAfter, err := c.ListPackageVolumes(context.TODO(), false)
	if err != nil {
		t.Fatalf("ListPackageVolumes after cascade: %v", err)
	}
	for _, g := range groupsAfter {
		if g.Repo == "core" && g.EffectiveName == "nginx" {
			t.Fatalf("core/nginx still present after cascade delete: %+v", g)
		}
	}

	// Unit must no longer be running. It may still be loaded (systemd
	// keeps the unit definition around until the unit file is removed —
	// the cascade only stops, it does not uninstall), so we assert on
	// ActiveState ≠ "active" rather than the unit disappearing entirely.
	sd := systemd.NewManager()
	states, err := sd.GetUnitStates(context.TODO(), []string{unitName})
	if err != nil {
		t.Fatalf("GetUnitStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 unit state, got %d", len(states))
	}
	if states[0].ActiveState == "active" || states[0].ActiveState == "activating" {
		t.Fatalf("expected unit %s to be stopped after cascade, got ActiveState=%q SubState=%q",
			unitName, states[0].ActiveState, states[0].SubState)
	}
}
