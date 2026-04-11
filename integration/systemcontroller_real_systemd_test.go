// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Real systemd integration tests ---
//
// Each test uses initRealSystemdTest which installs a per-test unique
// oneshot unit and echoes a per-test unique log message. Tests can run
// in parallel against the shared system bus because no two tests share
// a unit name, container name, or log marker.

func TestRealSystemdListUnits(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	units, err := f.Client.ListUnits(context.TODO(), systemcontroller.ListParams{Search: f.UnitName})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) == 0 {
		t.Fatal("expected at least 1 unit from real systemd")
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == f.UnitName {
			found = true
			if u.LoadState != "loaded" {
				t.Fatalf("expected LoadState 'loaded', got %q", u.LoadState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected %s in unit list", f.UnitName)
	}
}

func TestRealSystemdStartStop(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	// Ensure stopped first.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Stop); err != nil {
		t.Logf("SetUnitStatus cleanup: %v", err)
	}

	// Start.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	units, err := f.Client.ListUnits(context.TODO(), systemcontroller.ListParams{Search: f.UnitName})
	if err != nil {
		t.Fatalf("ListUnits after start: %v", err)
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == f.UnitName {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected active after start, got %q", u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("%s not found after start", f.UnitName)
	}

	// Stop.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	units, err = f.Client.ListUnits(context.TODO(), systemcontroller.ListParams{Search: f.UnitName})
	if err != nil {
		t.Fatalf("ListUnits after stop: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == f.UnitName {
			if u.ActiveState != "inactive" {
				t.Fatalf("expected inactive after stop, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatalf("%s not found after stop", f.UnitName)
}

func TestRealSystemdRestart(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	// Ensure started.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	units, err := f.Client.ListUnits(context.TODO(), systemcontroller.ListParams{Search: f.UnitName})
	if err != nil {
		t.Fatalf("ListUnits after restart: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == f.UnitName {
			if u.ActiveState != "active" {
				t.Fatalf("expected active after restart, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatalf("%s not found after restart", f.UnitName)
}

func TestRealSystemdEnableDisableRejected(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Disable); err == nil {
		t.Fatal("expected error for Disable, got nil")
	}

	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Enable); err == nil {
		t.Fatal("expected error for Enable, got nil")
	}
}

func TestRealSystemdLogReplay(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := f.Client.LogReplay(context.TODO(), f.UnitName)
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	var entries []systemd.JournalEntry
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

loop:
	for {
		select {
		case entry, ok := <-ch:
			if !ok {
				break loop
			}
			entries = append(entries, entry)
			if entry.Message == f.Message {
				break loop
			}
		case <-timer.C:
			break loop
		}
	}

	if len(entries) == 0 {
		t.Fatal("expected at least 1 journal entry")
	}

	found := false
	for _, e := range entries {
		if e.Message == f.Message {
			found = true
			if e.SystemdUnit != f.UnitName {
				t.Fatalf("expected SystemdUnit %q, got %q", f.UnitName, e.SystemdUnit)
			}
			break
		}
	}
	if !found {
		var messages []string
		for _, e := range entries {
			messages = append(messages, e.Message)
		}
		t.Fatalf("expected %q in journal entries, got messages: %v", f.Message, messages)
	}
}

func TestRealSystemdLogReplayFields(t *testing.T) {
	t.Parallel()
	f := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := f.Client.SetUnitStatus(context.TODO(), f.UnitName, systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := f.Client.LogReplay(context.TODO(), f.UnitName)
	if err != nil {
		t.Fatalf("LogReplay: %v", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case entry, ok := <-ch:
		if !ok {
			t.Fatal("channel closed without entries")
		}
		if entry.RealtimeTimestamp.IsZero() {
			t.Fatal("expected non-zero RealtimeTimestamp")
		}
		if entry.Hostname == "" {
			t.Fatal("expected non-empty Hostname")
		}
	case <-timer.C:
		t.Fatal("timed out waiting for journal entry")
	}
}
