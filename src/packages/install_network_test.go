package packages

import "testing"

func TestSaveAndLoadNetwork(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// No file yet → empty (default) network, not an error.
	got, err := m.LoadNetwork("default", "nginx")
	if err != nil {
		t.Fatalf("LoadNetwork: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty default, got %q", got)
	}

	if err := m.SaveNetwork("default", "nginx", "office"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}
	got, err = m.LoadNetwork("default", "nginx")
	if err != nil {
		t.Fatalf("LoadNetwork: %v", err)
	}
	if got != "office" {
		t.Fatalf("network = %q, want office", got)
	}
}

func TestSaveNetworkOverwrites(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	if err := m.SaveNetwork("default", "nginx", "office"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}
	// Reinstall onto a different network overwrites the assignment.
	if err := m.SaveNetwork("default", "nginx", "lab"); err != nil {
		t.Fatalf("SaveNetwork: %v", err)
	}
	got, err := m.LoadNetwork("default", "nginx")
	if err != nil {
		t.Fatalf("LoadNetwork: %v", err)
	}
	if got != "lab" {
		t.Fatalf("network = %q, want lab", got)
	}
}
