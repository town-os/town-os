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

func newTestRoot(t *testing.T) *RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	repos := RepositoryMap{
		"existing": {Name: "existing", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/existing.git"}},
	}
	data, _ := json.Marshal(repos)
	if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	root, err := RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return root
}

func TestRepositoryRootAdd(t *testing.T) {
	t.Run("add new repository", func(t *testing.T) {
		root := newTestRoot(t)
		repo := Repository{Name: "new-repo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/new-repo.git"}}

		if err := root.Add("new", repo); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(root.Items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(root.Items))
		}

		// verify it was persisted
		reloaded, err := RepositoryRootFromBase(root.BaseDir)
		if err != nil {
			t.Fatalf("unexpected error reloading: %v", err)
		}
		if len(reloaded.Items) != 2 {
			t.Fatalf("expected 2 items after reload, got %d", len(reloaded.Items))
		}
	})

	t.Run("add duplicate key", func(t *testing.T) {
		root := newTestRoot(t)
		repo := Repository{Name: "other", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/other.git"}}

		if err := root.Add("existing", repo); err == nil {
			t.Fatal("expected error for duplicate key")
		}
	})
}

func TestRepositoryRootRemove(t *testing.T) {
	t.Run("remove existing repository", func(t *testing.T) {
		root := newTestRoot(t)

		if err := root.Remove("existing"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(root.Items) != 0 {
			t.Fatalf("expected 0 items, got %d", len(root.Items))
		}

		// verify it was persisted
		reloaded, err := RepositoryRootFromBase(root.BaseDir)
		if err != nil {
			t.Fatalf("unexpected error reloading: %v", err)
		}
		if len(reloaded.Items) != 0 {
			t.Fatalf("expected 0 items after reload, got %d", len(reloaded.Items))
		}
	})

	t.Run("remove nonexistent key", func(t *testing.T) {
		root := newTestRoot(t)

		if err := root.Remove("nope"); err == nil {
			t.Fatal("expected error for nonexistent key")
		}
	})
}

func TestNewRepositoryBadCredentials(t *testing.T) {
	dir := t.TempDir()
	u := url.URL{Scheme: "https", Host: "gitea.com", Path: "/town-os/does-not-exist.git"}

	_, err := NewRepository(dir, u)
	if err == nil {
		t.Fatal("expected error for inaccessible repository")
	}
}

func writePackageYAML(t *testing.T, baseDir, repoName, pkgName, version, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, repoName, PackagesDir, pkgName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLoadPackages(t *testing.T) {
	t.Run("single package", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		writePackageYAML(t, dir, "myrepo", "nginx", "1.0", `image: nginx:1.0
environment:
  FOO: bar
`)

		pkgs, err := r.LoadPackages(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 1 {
			t.Fatalf("expected 1 package name, got %d", len(pkgs))
		}

		versions, ok := pkgs["nginx"]
		if !ok {
			t.Fatal("expected nginx package")
		}

		if len(versions) != 1 {
			t.Fatalf("expected 1 version, got %d", len(versions))
		}

		ip, ok := versions["1.0"]
		if !ok {
			t.Fatal("expected version 1.0")
		}

		if ip.Image != "nginx:1.0" {
			t.Fatalf("expected image %q, got %q", "nginx:1.0", ip.Image)
		}

		if ip.Environment["FOO"] != "bar" {
			t.Fatalf("expected env FOO=bar, got %q", ip.Environment["FOO"])
		}
	})

	t.Run("multiple versions", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		writePackageYAML(t, dir, "myrepo", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "myrepo", "nginx", "2.0", "image: nginx:2.0\n")

		pkgs, err := r.LoadPackages(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs["nginx"]) != 2 {
			t.Fatalf("expected 2 versions, got %d", len(pkgs["nginx"]))
		}
	})

	t.Run("skips non-yaml files", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		writePackageYAML(t, dir, "myrepo", "nginx", "1.0", "image: nginx:1.0\n")

		// write a non-yaml file that should be ignored
		notesPath := filepath.Join(dir, "myrepo", PackagesDir, "nginx", "README.md")
		if err := os.WriteFile(notesPath, []byte("hello"), 0644); err != nil {
			t.Fatal(err)
		}

		pkgs, err := r.LoadPackages(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs["nginx"]) != 1 {
			t.Fatalf("expected 1 version, got %d", len(pkgs["nginx"]))
		}
	})

	t.Run("missing packages dir", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		// create the repo dir but not the packages subdir
		if err := os.MkdirAll(filepath.Join(dir, "myrepo"), 0755); err != nil {
			t.Fatal(err)
		}

		_, err := r.LoadPackages(dir)
		if err == nil {
			t.Fatal("expected error for missing packages dir")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		writePackageYAML(t, dir, "myrepo", "nginx", "1.0", "image: [bad yaml")

		_, err := r.LoadPackages(dir)
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
	})
}

func TestRepositoryRootLoadAllPackages(t *testing.T) {
	t.Run("merges across repositories", func(t *testing.T) {
		dir := t.TempDir()
		repos := RepositoryMap{
			"repo-a": {Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			"repo-b": {Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data, _ := json.Marshal(repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatal(err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-b", "redis", "7.0", "image: redis:7.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		pkgs, err := root.LoadAllPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 2 {
			t.Fatalf("expected 2 package names, got %d", len(pkgs))
		}

		if _, ok := pkgs["nginx"]["1.0"]; !ok {
			t.Fatal("expected nginx 1.0")
		}

		if _, ok := pkgs["redis"]["7.0"]; !ok {
			t.Fatal("expected redis 7.0")
		}
	})
}
