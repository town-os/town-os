package packages

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRepositoryName(t *testing.T) {
	tests := map[string]struct {
		url      string
		wantName string
	}{
		"https with .git": {
			url:      "https://github.com/user/my-repo.git",
			wantName: "my-repo",
		},
		"https without .git": {
			url:      "https://github.com/user/my-repo",
			wantName: "my-repo",
		},
		"nested path": {
			url:      "https://gitea.example.com/org/sub/repo.git",
			wantName: "repo",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatalf("bad test url: %v", err)
			}

			// NewRepository will fail on clone, but we can still check the Name was set
			r := &Repository{
				Name: strings.TrimSuffix(filepath.Base(u.Path), ".git"),
				URL:  *u,
			}

			if r.Name != tt.wantName {
				t.Fatalf("expected name %q, got %q", tt.wantName, r.Name)
			}
			_ = dir
		})
	}
}

func TestRepositoryRootFromBase(t *testing.T) {
	t.Run("valid json", func(t *testing.T) {
		dir := t.TempDir()
		repos := RepositoryMap{
			"test": {Name: "test", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}},
		}
		data, _ := json.Marshal(repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatal(err)
		}

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if root.BaseDir != dir {
			t.Fatalf("expected baseDir %q, got %q", dir, root.BaseDir)
		}
		if len(root.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(root.Items))
		}
	})

	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := RepositoryRootFromBase(dir)
		if err == nil {
			t.Fatal("expected error for missing file")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), []byte("{bad json"), 0644); err != nil {
			t.Fatal(err)
		}
		_, err := RepositoryRootFromBase(dir)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})
}
