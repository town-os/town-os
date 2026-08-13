// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// The daily update timer itself lives in the installer
// (../install/systemd/town-os-update.{timer,service}), so there are no unit
// files to assert here. What the controller owns is the marker that identifies
// a scheduled call and the meaning of the setting's stored value — and those
// are what these tests pin.

// TestScheduledRefreshQueryIsStable guards the one string the shipped unit and
// this package have to agree on.
//
// Nothing enforces the agreement at build time: the unit is a literal in
// another repository. Renaming this constant would leave that unit sending a
// marker nothing reads, so every scheduled refresh would ignore
// auto_update_enabled and update anyway — a box whose owner turned updates off
// would keep updating, and no test or build would say so.
func TestScheduledRefreshQueryIsStable(t *testing.T) {
	t.Parallel()

	if ScheduledRefreshQuery != "scheduled" {
		t.Errorf("ScheduledRefreshQuery = %q; ../install/systemd/town-os-update.service POSTs ?scheduled=1 and must be changed in the same commit", ScheduledRefreshQuery)
	}
}

// TestAutoUpdateDisabledValue pins which stored values stop a scheduled update.
//
// The asymmetry is deliberate and is the point of the test: "off" is a closed
// list, everything else is on. An unrecognised value must not stop a box
// updating, because the installer ships only the systemcontroller and rolodex
// images and a box that stops pulling stops acquiring its own services. A typo
// in a settings row should cost one extra pull, not the rest of the system.
func TestAutoUpdateDisabledValue(t *testing.T) {
	t.Parallel()

	for _, v := range []string{"0", "false", "off", "no", "FALSE", " Off ", "No"} {
		if !autoUpdateDisabledValue(v) {
			t.Errorf("autoUpdateDisabledValue(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"1", "true", "on", "yes", "", "banana", "2"} {
		if autoUpdateDisabledValue(v) {
			t.Errorf("autoUpdateDisabledValue(%q) = true; only an explicit off may disable updates", v)
		}
	}
}

// TestAutoUpdateSettingDefaultsOn asserts a box updates itself unless told not
// to.
//
// The installer ships only the systemcontroller and rolodex images; every other
// system-service image is pulled on the box. Defaulting this off would ship
// boxes that never acquire the rest of their services.
func TestAutoUpdateSettingDefaultsOn(t *testing.T) {
	t.Parallel()

	got, ok := account.DefaultSettings[account.AutoUpdateKey]
	if !ok {
		t.Fatalf("%s is not registered in DefaultSettings", account.AutoUpdateKey)
	}
	if got != "1" {
		t.Errorf("%s default = %q, want \"1\"", account.AutoUpdateKey, got)
	}
}
