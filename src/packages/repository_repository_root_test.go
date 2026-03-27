// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepositoryRootFromBase(t *testing.T) {
	t.Run("valid json", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "test", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
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
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), []byte("{bad json"), 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err = RepositoryRootFromBase(dir)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})
}

func TestRepositoryRootAdd(t *testing.T) {
	t.Run("add new repository", func(t *testing.T) {
		root := newTestRoot(t)
		repo := Repository{Name: "new-repo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/new-repo.git"}}

		err := root.Add(repo)
		if err != nil {
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

	t.Run("add duplicate name", func(t *testing.T) {
		root := newTestRoot(t)
		repo := Repository{Name: "existing", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/other.git"}}

		err := root.Add(repo)
		if err == nil {
			t.Fatal("expected error for duplicate name")
		}
	})

	t.Run("preserves insertion order", func(t *testing.T) {
		dir := t.TempDir()
		data := marshalJSON(t, []Repository{})
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		names := []string{"charlie", "alpha", "bravo"}
		for _, name := range names {
			p, err := url.JoinPath("/", name+".git")
			if err != nil {
				t.Fatalf("url.JoinPath for %q: %v", name, err)
			}
			err = root.Add(Repository{Name: name, URL: url.URL{Scheme: "https", Host: "example.com", Path: p}})
			if err != nil {
				t.Fatalf("Add(%q): %v", name, err)
			}
		}

		reloaded, err := RepositoryRootFromBase(root.BaseDir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}
		for i, name := range names {
			if reloaded.Items[i].Name != name {
				t.Fatalf("expected item %d to be %q, got %q", i, name, reloaded.Items[i].Name)
			}
		}
	})
}

func TestRepositoryRootRemove(t *testing.T) {
	t.Run("remove existing repository", func(t *testing.T) {
		root := newTestRoot(t)

		err := root.Remove("existing")
		if err != nil {
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

	t.Run("remove nonexistent name", func(t *testing.T) {
		root := newTestRoot(t)

		err := root.Remove("nope")
		if err == nil {
			t.Fatal("expected error for nonexistent name")
		}
	})
}

func TestRepositoryRootGet(t *testing.T) {
	root := newTestRoot(t)

	r, ok := root.Get("existing")
	if !ok {
		t.Fatal("expected to find existing repo")
	}
	if r.Name != "existing" {
		t.Fatalf("expected name %q, got %q", "existing", r.Name)
	}

	_, ok = root.Get("nope")
	if ok {
		t.Fatal("expected not to find nonexistent repo")
	}
}

func TestRepositoryRootList(t *testing.T) {
	t.Run("returns all repositories", func(t *testing.T) {
		root := newTestRoot(t)

		repos, err := root.List()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repos) != 1 {
			t.Fatalf("expected 1 repo, got %d", len(repos))
		}

		if repos[0].Name != "existing" {
			t.Fatalf("expected name %q, got %q", "existing", repos[0].Name)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		dir := t.TempDir()
		data := marshalJSON(t, []Repository{})
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		repos, err := root.List()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(repos) != 0 {
			t.Fatalf("expected 0 repos, got %d", len(repos))
		}
	})

	t.Run("returns copy", func(t *testing.T) {
		root := newTestRoot(t)

		repos, err := root.List()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		repos[0].Name = "mutated"

		if root.Items[0].Name != "existing" {
			t.Fatal("List should return a copy, not a reference")
		}
	})

	t.Run("reflects add and remove", func(t *testing.T) {
		dir := t.TempDir()
		data := marshalJSON(t, []Repository{})
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		err = root.Add(Repository{Name: "alpha", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/alpha.git"}})
		if err != nil {
			t.Fatalf("Add alpha: %v", err)
		}
		err = root.Add(Repository{Name: "bravo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/bravo.git"}})
		if err != nil {
			t.Fatalf("Add bravo: %v", err)
		}

		repos, err := root.List()
		if err != nil {
			t.Fatalf("List after add: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected 2 repos, got %d", len(repos))
		}

		err = root.Remove("alpha")
		if err != nil {
			t.Fatalf("Remove alpha: %v", err)
		}

		repos, err = root.List()
		if err != nil {
			t.Fatalf("List after remove: %v", err)
		}
		if len(repos) != 1 {
			t.Fatalf("expected 1 repo after remove, got %d", len(repos))
		}
		if repos[0].Name != "bravo" {
			t.Fatalf("expected bravo to remain, got %s", repos[0].Name)
		}
	})
}

func TestRepositoryRootLoadAllPackages(t *testing.T) {
	t.Run("merges across repositories", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-b", "redis", "7.0", "image: redis:7.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
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

// --- Refresh caching tests ---

func TestRepositoryRootRefreshCaching(t *testing.T) {
	t.Run("Refresh skips when recent", func(t *testing.T) {
		dir := t.TempDir()
		writeTestRepos(t, dir, []Repository{})

		rr, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rr.RefreshInterval = time.Minute
		rr.LastRefreshed = time.Now()

		// This should be a no-op since we just set LastRefreshed.
		rr.Refresh()

		// Verify the timestamp file was NOT written (since refresh was skipped).
		fn := filepath.Join(dir, LastRefreshedFile)
		if _, err := os.Stat(fn); err == nil {
			t.Fatal("expected no last_refreshed file since refresh was skipped")
		}
	})

	t.Run("Refresh runs when interval exceeded", func(t *testing.T) {
		dir := t.TempDir()
		writeTestRepos(t, dir, []Repository{})

		rr, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rr.RefreshInterval = 0 // Always allow refresh.

		rr.Refresh()

		if rr.LastRefreshed.IsZero() {
			t.Fatal("expected LastRefreshed to be set after refresh")
		}

		fn := filepath.Join(dir, LastRefreshedFile)
		if _, err := os.Stat(fn); err != nil {
			t.Fatalf("expected last_refreshed file to exist: %v", err)
		}
	})

	t.Run("ForceRefresh ignores interval", func(t *testing.T) {
		dir := t.TempDir()
		writeTestRepos(t, dir, []Repository{})

		rr, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rr.RefreshInterval = time.Hour
		rr.LastRefreshed = time.Now()

		rr.ForceRefresh()

		fn := filepath.Join(dir, LastRefreshedFile)
		if _, err := os.Stat(fn); err != nil {
			t.Fatalf("expected last_refreshed file to exist after ForceRefresh: %v", err)
		}
	})

	t.Run("LastRefreshed persists and loads", func(t *testing.T) {
		dir := t.TempDir()
		writeTestRepos(t, dir, []Repository{})

		rr, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		rr.RefreshInterval = 0
		rr.ForceRefresh()

		saved := rr.LastRefreshed

		// Load fresh.
		rr2, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error on reload: %v", err)
		}

		if rr2.LastRefreshed.IsZero() {
			t.Fatal("expected loaded LastRefreshed to be non-zero")
		}

		// Should be within 1 second of the saved time.
		diff := saved.Sub(rr2.LastRefreshed)
		if diff < 0 {
			diff = -diff
		}
		if diff > time.Second {
			t.Fatalf("expected loaded time %v to be close to saved time %v", rr2.LastRefreshed, saved)
		}
	})

	t.Run("default interval is set", func(t *testing.T) {
		dir := t.TempDir()
		writeTestRepos(t, dir, []Repository{})

		rr, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rr.RefreshInterval != DefaultRefreshInterval {
			t.Fatalf("expected default interval %v, got %v", DefaultRefreshInterval, rr.RefreshInterval)
		}
	})
}

func TestSaveLastRefreshedSuccess(t *testing.T) {
	dir := t.TempDir()
	rr := &RepositoryRoot{
		BaseDir:       dir,
		LastRefreshed: time.Now(),
	}

	err := rr.saveLastRefreshed()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify the file was created with restricted permissions.
	fn := filepath.Join(dir, LastRefreshedFile)
	fi, err := os.Stat(fn)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("expected permission 0600, got %o", fi.Mode().Perm())
	}
}

func TestSaveLastRefreshedError(t *testing.T) {
	rr := &RepositoryRoot{
		BaseDir:       "/nonexistent/path/that/does/not/exist",
		LastRefreshed: time.Now(),
	}

	err := rr.saveLastRefreshed()
	if err == nil {
		t.Fatal("expected error writing to nonexistent directory")
	}
}

func TestForceRefreshContinuesOnSaveTimestampError(t *testing.T) {
	// Use a read-only directory so saveLastRefreshed fails.
	dir := t.TempDir()
	writeTestRepos(t, dir, []Repository{})

	rr, err := RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	// Point BaseDir at a nonexistent path so saveLastRefreshed fails,
	// but keep Items empty so init() is never called.
	rr.BaseDir = "/nonexistent/path/that/does/not/exist"
	rr.Items = nil

	rr.ForceRefresh()

	// Refresh should still have updated LastRefreshed despite the save error.
	if rr.LastRefreshed.IsZero() {
		t.Fatal("expected LastRefreshed to be set even when save fails")
	}
}

func TestCachedLoadPackagesReturnsCachedResult(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "myrepo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr := &RepositoryRoot{
		BaseDir: dir,
		Items:   []Repository{{Name: "myrepo"}},
	}

	// First call should load from disk.
	pkgs1, err := rr.cachedLoadPackages(&rr.Items[0])
	if err != nil {
		t.Fatalf("first cachedLoadPackages: %v", err)
	}
	if _, ok := pkgs1["nginx"]; !ok {
		t.Fatal("expected nginx package in first load")
	}

	// Remove the YAML file — second call should still return cached result.
	if err := os.Remove(filepath.Join(repoDir, "1.0.yaml")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	pkgs2, err := rr.cachedLoadPackages(&rr.Items[0])
	if err != nil {
		t.Fatalf("second cachedLoadPackages: %v", err)
	}
	if _, ok := pkgs2["nginx"]; !ok {
		t.Fatal("expected nginx package from cache after file removal")
	}
}

func TestInvalidatePackageCacheForcesReload(t *testing.T) {
	dir := t.TempDir()
	repoDir := filepath.Join(dir, "myrepo", PackagesDir, "nginx")
	if err := os.MkdirAll(repoDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rr := &RepositoryRoot{
		BaseDir: dir,
		Items:   []Repository{{Name: "myrepo"}},
	}

	// Populate cache.
	if _, err := rr.cachedLoadPackages(&rr.Items[0]); err != nil {
		t.Fatalf("cachedLoadPackages: %v", err)
	}

	// Add a new package version.
	if err := os.WriteFile(filepath.Join(repoDir, "2.0.yaml"), []byte("image: nginx:2.0\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Without invalidation, cache should not see new version.
	pkgs, err := rr.cachedLoadPackages(&rr.Items[0])
	if err != nil {
		t.Fatalf("cachedLoadPackages: %v", err)
	}
	if _, ok := pkgs["nginx"]["2.0"]; ok {
		t.Fatal("cache should not contain 2.0 before invalidation")
	}

	// After invalidation, new version should appear.
	rr.InvalidatePackageCache()

	pkgs, err = rr.cachedLoadPackages(&rr.Items[0])
	if err != nil {
		t.Fatalf("cachedLoadPackages after invalidate: %v", err)
	}
	if _, ok := pkgs["nginx"]["2.0"]; !ok {
		t.Fatal("expected nginx 2.0 after cache invalidation")
	}
}
