package packages

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFeaturedValid(t *testing.T) {
	dir := t.TempDir()
	repoName := "test-repo"
	r := &Repository{Name: repoName}

	repoDir := filepath.Join(dir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil { //nolint:gosec // test directory
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, FeaturedFile), []byte(`["nginx","redis","postgres"]`), 0644); err != nil { //nolint:gosec // test file
		t.Fatalf("WriteFile: %v", err)
	}

	featured, err := r.LoadFeatured(dir)
	if err != nil {
		t.Fatalf("LoadFeatured: %v", err)
	}

	if len(featured) != 3 {
		t.Fatalf("expected 3 featured items, got %d", len(featured))
	}
	if featured[0] != "nginx" || featured[1] != "redis" || featured[2] != "postgres" {
		t.Fatalf("unexpected featured items: %v", featured)
	}
}

func TestLoadFeaturedMissingFile(t *testing.T) {
	dir := t.TempDir()
	r := &Repository{Name: "nonexistent-repo"}

	featured, err := r.LoadFeatured(dir)
	if err != nil {
		t.Fatalf("LoadFeatured should not error for missing file: %v", err)
	}
	if featured != nil {
		t.Fatalf("expected nil for missing file, got %v", featured)
	}
}

func TestLoadFeaturedEmptyList(t *testing.T) {
	dir := t.TempDir()
	repoName := "test-repo"
	r := &Repository{Name: repoName}

	repoDir := filepath.Join(dir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil { //nolint:gosec // test directory
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, FeaturedFile), []byte(`[]`), 0644); err != nil { //nolint:gosec // test file
		t.Fatalf("WriteFile: %v", err)
	}

	featured, err := r.LoadFeatured(dir)
	if err != nil {
		t.Fatalf("LoadFeatured: %v", err)
	}

	if len(featured) != 0 {
		t.Fatalf("expected 0 featured items, got %d", len(featured))
	}
}

func TestLoadFeaturedInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	repoName := "test-repo"
	r := &Repository{Name: repoName}

	repoDir := filepath.Join(dir, repoName)
	if err := os.MkdirAll(repoDir, 0755); err != nil { //nolint:gosec // test directory
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoDir, FeaturedFile), []byte(`{invalid json}`), 0644); err != nil { //nolint:gosec // test file
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := r.LoadFeatured(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
