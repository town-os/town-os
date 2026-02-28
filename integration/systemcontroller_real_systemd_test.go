package integration_test

import (
	"context"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Real systemd integration tests ---

func TestRealSystemdListUnits(t *testing.T) {
	c := initRealSystemdTest(t)

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units.Entries) == 0 {
		t.Fatal("expected at least 1 unit from real systemd")
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			found = true
			if u.LoadState != "loaded" {
				t.Fatalf("expected LoadState 'loaded', got %q", u.LoadState)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected town-os-package--repo-test-1.0.service in unit list")
	}
}

func TestRealSystemdStartStop(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure stopped first.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Stop); err != nil {
		t.Logf("SetUnitStatus cleanup: %v", err)
	}

	// Start.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after start: %v", err)
	}

	found := false
	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected active after start, got %q", u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatal("town-os-package--repo-test-1.0.service not found after start")
	}

	// Stop.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	units, err = c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after stop: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			if u.ActiveState != "inactive" {
				t.Fatalf("expected inactive after stop, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-package--repo-test-1.0.service not found after stop")
}

func TestRealSystemdRestart(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure started.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "repo-test"})
	if err != nil {
		t.Fatalf("ListUnits after restart: %v", err)
	}

	for _, u := range units.Entries {
		if u.Name == "town-os-package--repo-test-1.0.service" {
			if u.ActiveState != "active" {
				t.Fatalf("expected active after restart, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-package--repo-test-1.0.service not found after restart")
}

func TestRealSystemdEnableDisableRejected(t *testing.T) {
	c := initRealSystemdTest(t)

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Disable); err == nil {
		t.Fatal("expected error for Disable, got nil")
	}

	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Enable); err == nil {
		t.Fatal("expected error for Enable, got nil")
	}
}

func TestRealSystemdLogReplay(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-test-1.0.service")
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
			if entry.Message == "town-os-test-message" {
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
		if e.Message == "town-os-test-message" {
			found = true
			if e.SystemdUnit != "town-os-package--repo-test-1.0.service" {
				t.Fatalf("expected SystemdUnit 'town-os-package--repo-test-1.0.service', got %q", e.SystemdUnit)
			}
			break
		}
	}
	if !found {
		var messages []string
		for _, e := range entries {
			messages = append(messages, e.Message)
		}
		t.Fatalf("expected 'town-os-test-message' in journal entries, got messages: %v", messages)
	}
}

func TestRealSystemdLogReplayFields(t *testing.T) {
	c := initRealSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus(context.TODO(), "town-os-package--repo-test-1.0.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay(context.TODO(), "town-os-package--repo-test-1.0.service")
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
