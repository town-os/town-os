//go:build podman

package integration_test

import (
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func initPodmanSystemdTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	sd := systemd.NewManager()
	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(mock, nil, nil, sd)
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestPodmanSystemdListUnits(t *testing.T) {
	c := initPodmanSystemdTest(t)

	units, err := c.ListUnits()
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	if len(units) == 0 {
		t.Fatal("expected at least 1 unit from real systemd")
	}

	found := false
	for _, u := range units {
		if u.Name == "town-os-test.service" {
			found = true
			if u.LoadState != "loaded" {
				t.Fatalf("expected LoadState 'loaded', got %q", u.LoadState)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected town-os-test.service in unit list")
	}
}

func TestPodmanSystemdStartStop(t *testing.T) {
	c := initPodmanSystemdTest(t)

	// Ensure stopped first.
	_ = c.SetUnitStatus("town-os-test.service", systemd.Stop)

	// Start.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	units, err := c.ListUnits()
	if err != nil {
		t.Fatalf("ListUnits after start: %v", err)
	}

	found := false
	for _, u := range units {
		if u.Name == "town-os-test.service" {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected active after start, got %q", u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatal("town-os-test.service not found after start")
	}

	// Stop.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Stop); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	units, err = c.ListUnits()
	if err != nil {
		t.Fatalf("ListUnits after stop: %v", err)
	}

	for _, u := range units {
		if u.Name == "town-os-test.service" {
			if u.ActiveState != "inactive" {
				t.Fatalf("expected inactive after stop, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-test.service not found after stop")
}

func TestPodmanSystemdRestart(t *testing.T) {
	c := initPodmanSystemdTest(t)

	// Ensure started.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := c.SetUnitStatus("town-os-test.service", systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	units, err := c.ListUnits()
	if err != nil {
		t.Fatalf("ListUnits after restart: %v", err)
	}

	for _, u := range units {
		if u.Name == "town-os-test.service" {
			if u.ActiveState != "active" {
				t.Fatalf("expected active after restart, got %q", u.ActiveState)
			}
			return
		}
	}
	t.Fatal("town-os-test.service not found after restart")
}

func TestPodmanSystemdEnableDisable(t *testing.T) {
	c := initPodmanSystemdTest(t)

	if err := c.SetUnitStatus("town-os-test.service", systemd.Disable); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	// Re-enable so subsequent tests are unaffected.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
}

func TestPodmanSystemdLogReplay(t *testing.T) {
	c := initPodmanSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay("town-os-test.service")
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
			if e.SystemdUnit != "town-os-test.service" {
				t.Fatalf("expected SystemdUnit 'town-os-test.service', got %q", e.SystemdUnit)
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

func TestPodmanSystemdLogReplayFields(t *testing.T) {
	c := initPodmanSystemdTest(t)

	// Ensure the service has been started at least once.
	if err := c.SetUnitStatus("town-os-test.service", systemd.Start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(time.Second)

	ch, err := c.LogReplay("town-os-test.service")
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
