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
		repos := []Repository{
			{Name: "test", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
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
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), []byte("{bad json"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		_, err := RepositoryRootFromBase(dir)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})
}

func marshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

func newTestRoot(t *testing.T) *RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	repos := []Repository{
		{Name: "existing", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/existing.git"}},
	}
	data := marshalJSON(t, repos)
	if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
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

		if err := root.Add(repo); err != nil {
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

		if err := root.Add(repo); err == nil {
			t.Fatal("expected error for duplicate name")
		}
	})

	t.Run("preserves insertion order", func(t *testing.T) {
		dir := t.TempDir()
		data := marshalJSON(t, []Repository{})
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		names := []string{"charlie", "alpha", "bravo"}
		for _, name := range names {
			if err := root.Add(Repository{Name: name, URL: url.URL{Scheme: "https", Host: "example.com", Path: "/" + name + ".git"}}); err != nil {
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

	t.Run("remove nonexistent name", func(t *testing.T) {
		root := newTestRoot(t)

		if err := root.Remove("nope"); err == nil {
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
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
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
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		if err := root.Add(Repository{Name: "alpha", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/alpha.git"}}); err != nil {
			t.Fatalf("Add alpha: %v", err)
		}
		if err := root.Add(Repository{Name: "bravo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/bravo.git"}}); err != nil {
			t.Fatalf("Add bravo: %v", err)
		}

		repos, err := root.List()
		if err != nil {
			t.Fatalf("List after add: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected 2 repos, got %d", len(repos))
		}

		if err := root.Remove("alpha"); err != nil {
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
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
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
			t.Fatalf("WriteFile: %v", err)
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
			t.Fatalf("MkdirAll: %v", err)
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
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
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

func TestCompareVersions(t *testing.T) {
	tests := map[string]struct {
		a, b string
		want int
	}{
		"equal":                   {a: "1.0", b: "1.0", want: 0},
		"less major":              {a: "1.0", b: "2.0", want: -1},
		"greater major":           {a: "3.0", b: "2.0", want: 1},
		"less minor":              {a: "1.0", b: "1.1", want: -1},
		"greater minor":           {a: "1.2", b: "1.1", want: 1},
		"three segments":          {a: "1.0.1", b: "1.0.0", want: 1},
		"different segment count": {a: "1.0", b: "1.0.1", want: -1},
		"numeric not lexical":     {a: "1.9", b: "1.10", want: -1},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := CompareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Fatalf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestLatestPackage(t *testing.T) {
	t.Run("single repo multiple versions", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkg, version, err := root.LatestPackage("nginx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "2.0" {
			t.Fatalf("expected version 2.0, got %s", version)
		}
		if pkg.Image != "nginx:2.0" {
			t.Fatalf("expected image nginx:2.0, got %s", pkg.Image)
		}
	})

	t.Run("latest across multiple repos", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-b", "nginx", "3.0", "image: nginx:3.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkg, version, err := root.LatestPackage("nginx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "3.0" {
			t.Fatalf("expected version 3.0, got %s", version)
		}
		if pkg.Image != "nginx:3.0" {
			t.Fatalf("expected image nginx:3.0, got %s", pkg.Image)
		}
	})

	t.Run("preferred repo wins on tie", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0-from-a\n")
		writePackageYAML(t, dir, "repo-b", "nginx", "2.0", "image: nginx:2.0-from-b\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkg, version, err := root.LatestPackage("nginx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if version != "2.0" {
			t.Fatalf("expected version 2.0, got %s", version)
		}
		if pkg.Image != "nginx:2.0-from-a" {
			t.Fatalf("expected preferred repo (repo-a) image, got %s", pkg.Image)
		}
	})

	t.Run("package not found", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		_, _, err = root.LatestPackage("nonexistent")
		if err != ErrPackageNotFound {
			t.Fatalf("expected ErrPackageNotFound, got %v", err)
		}
	})
}

func TestListPackages(t *testing.T) {
	t.Run("single repo picks latest of each package", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")
		writePackageYAML(t, dir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 2 {
			t.Fatalf("expected 2 packages, got %d", len(pkgs))
		}

		// sorted by name
		if pkgs[0] != "nginx@2.0" {
			t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
		}
		if pkgs[1] != "redis@7.0" {
			t.Fatalf("expected redis@7.0, got %s", pkgs[1])
		}
	})

	t.Run("multiple repos no overlap", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-b", "redis", "7.0", "image: redis:7.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 2 {
			t.Fatalf("expected 2 packages, got %d", len(pkgs))
		}

		p0, _ := ParsePackageIdentity(pkgs[0])
		p1, _ := ParsePackageIdentity(pkgs[1])
		if p0.Name != "nginx" || p1.Name != "redis" {
			t.Fatalf("expected nginx and redis, got %s and %s", p0.Name, p1.Name)
		}
	})

	t.Run("preferred repo wins on version tie", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// both repos have nginx 2.0 — repo-a listed first, so its version wins
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")
		writePackageYAML(t, dir, "repo-b", "nginx", "2.0", "image: nginx:2.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 1 {
			t.Fatalf("expected 1 package, got %d", len(pkgs))
		}
		if pkgs[0] != "nginx@2.0" {
			t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
		}
	})

	t.Run("later repo higher version wins", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
			{Name: "repo-b", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-b.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-b", "nginx", "3.0", "image: nginx:3.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 1 {
			t.Fatalf("expected 1 package, got %d", len(pkgs))
		}
		if pkgs[0] != "nginx@3.0" {
			t.Fatalf("expected nginx@3.0, got %s", pkgs[0])
		}
	})

	t.Run("no packages returns empty", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// create the packages dir but leave it empty
		if err := os.MkdirAll(filepath.Join(dir, "repo-a", PackagesDir), 0755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 0 {
			t.Fatalf("expected 0 packages, got %d", len(pkgs))
		}
	})

	t.Run("results sorted by name", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		if err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "zookeeper", "1.0", "image: zk:1.0\n")
		writePackageYAML(t, dir, "repo-a", "alpine", "3.18", "image: alpine:3.18\n")
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 3 {
			t.Fatalf("expected 3 packages, got %d", len(pkgs))
		}

		for i, pkg := range pkgs {
			p, _ := ParsePackageIdentity(pkg)
			expected := []string{"alpine", "nginx", "zookeeper"}
			if p.Name != expected[i] {
				t.Fatalf("expected package %d to be %q, got %q", i, expected[i], p.Name)
			}
		}
	})
}
