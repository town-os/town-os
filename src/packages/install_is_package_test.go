// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"os"
	"path/filepath"
	"testing"
)

// --- IsPackageChanged tests ---

func TestIsPackageChangedSameInode(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install (creates hard link).
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if changed {
		t.Fatal("expected package to NOT be changed (same inode)")
	}
}

func TestIsPackageChangedDifferentInode(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install (creates hard link).
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Simulate upstream change: remove and recreate the repo file (breaks hard link).
	if err := os.Remove(repoFile); err != nil {
		t.Fatalf("Remove repo file: %v", err)
	}
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.1\n"), 0600); err != nil {
		t.Fatalf("WriteFile repo updated: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected package to be changed (different inode)")
	}
}

func TestIsPackageChangedRepoFileMissing(t *testing.T) {
	dir := t.TempDir()
	m := NewInstallManager(dir)

	// Create repo package file.
	repoDir := filepath.Join(dir, "test-repo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("MkdirAll repo: %v", err)
	}
	repoFile := filepath.Join(repoDir, "1.0.yaml")
	if err := os.WriteFile(repoFile, []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile repo: %v", err)
	}

	// Install.
	if err := m.Install("test-repo", "nginx", "1.0", Responses{}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Remove repo file (simulates package removed from repo).
	if err := os.Remove(repoFile); err != nil {
		t.Fatalf("Remove repo file: %v", err)
	}

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected package to be changed (repo file missing)")
	}
}

func TestMockIsPackageChanged(t *testing.T) {
	m := InitMockInstallManager()
	m.ChangedPackages["test-repo/nginx@1.0"] = true

	changed, err := m.IsPackageChanged("test-repo", "nginx", "1.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if !changed {
		t.Fatal("expected true for changed package")
	}

	changed, err = m.IsPackageChanged("test-repo", "nginx", "2.0")
	if err != nil {
		t.Fatalf("IsPackageChanged: %v", err)
	}
	if changed {
		t.Fatal("expected false for unchanged package")
	}
}
