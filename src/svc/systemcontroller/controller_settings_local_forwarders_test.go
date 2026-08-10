// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/account"
)

// The setting is read back with strconv.ParseBool everywhere it is consumed —
// boot, the settings handler, the UI. A value that will not parse is read as
// off, so an unvalidated write would look accepted and change nothing.
func TestDNSLocalForwardersIsWiredIntoSettingsValidators(t *testing.T) {
	t.Parallel()

	v, ok := settingsValidators["dns_local_forwarders"]
	if !ok {
		t.Fatal("dns_local_forwarders has no validator; an unparseable value would be stored and silently read as off")
	}

	for _, good := range []string{"true", "false", "1", "0", "TRUE", " true "} {
		if err := v(good); err != nil {
			t.Errorf("validator(%q) = %v, want nil", good, err)
		}
	}
	for _, bad := range []string{"", "yes", "on", "192.168.1.1"} {
		if err := v(bad); err == nil {
			t.Errorf("validator(%q) = nil, want an error", bad)
		}
	}
}

// Off is the default and the direction that matters. The local resolver sees
// every name the household looks up, which is what resolving from the roots
// exists to avoid — a box must not make that trade on the operator's behalf.
func TestDNSLocalForwardersDefaultIsOff(t *testing.T) {
	t.Parallel()

	got, ok := account.DefaultSettings["dns_local_forwarders"]
	if !ok {
		t.Fatal("dns_local_forwarders is not seeded in DefaultSettings")
	}
	if got != "false" {
		t.Fatalf("default dns_local_forwarders = %q, want %q", got, "false")
	}
}
