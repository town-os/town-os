// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPackage(t *testing.T) {
	t.Run("loads a single package", func(t *testing.T) {
		dir := t.TempDir()
		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", `image: nginx:1.0
environment:
  FOO: bar
volumes:
  data:
    mountpoint: /var/lib/data
    quota: 2gb
`)
		root := &RepositoryRoot{BaseDir: dir}
		ip, err := root.LoadPackage("repo-a", "nginx", "1.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ip.Image.URL != "nginx:1.0" {
			t.Fatalf("expected image %q, got %q", "nginx:1.0", ip.Image.URL)
		}
		if ip.Volumes["data"].Mountpoint != "/var/lib/data" {
			t.Fatalf("expected mountpoint %q, got %q", "/var/lib/data", ip.Volumes["data"].Mountpoint)
		}
		if ip.Volumes["data"].Quota != "2gb" {
			t.Fatalf("expected quota %q, got %q", "2gb", ip.Volumes["data"].Quota)
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		root := &RepositoryRoot{BaseDir: dir}
		_, err := root.LoadPackage("repo-a", "nginx", "1.0")
		if err == nil {
			t.Fatal("expected error for missing package")
		}
	})

	t.Run("invalid yaml", func(t *testing.T) {
		dir := t.TempDir()
		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: [bad yaml")
		root := &RepositoryRoot{BaseDir: dir}
		_, err := root.LoadPackage("repo-a", "nginx", "1.0")
		if err == nil {
			t.Fatal("expected error for invalid yaml")
		}
	})
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

		if ip.Image.URL != "nginx:1.0" {
			t.Fatalf("expected image %q, got %q", "nginx:1.0", ip.Image.URL)
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
		err := os.WriteFile(notesPath, []byte("hello"), 0600)
		if err != nil {
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

	t.Run("missing packages dir returns empty", func(t *testing.T) {
		dir := t.TempDir()
		r := &Repository{Name: "myrepo", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/myrepo.git"}}

		// create the repo dir but not the packages subdir
		err := os.MkdirAll(filepath.Join(dir, "myrepo"), 0750)
		if err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		pkgs, err := r.LoadPackages(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(pkgs) != 0 {
			t.Fatalf("expected empty package table, got %d entries", len(pkgs))
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
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
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
		if pkg.Image.URL != "nginx:2.0" {
			t.Fatalf("expected image nginx:2.0, got %s", pkg.Image.URL)
		}
	})

	t.Run("latest across multiple repos", func(t *testing.T) {
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
		if pkg.Image.URL != "nginx:3.0" {
			t.Fatalf("expected image nginx:3.0, got %s", pkg.Image.URL)
		}
	})

	t.Run("preferred repo wins on tie", func(t *testing.T) {
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
		if pkg.Image.URL != "nginx:2.0-from-a" {
			t.Fatalf("expected preferred repo (repo-a) image, got %s", pkg.Image.URL)
		}
	})

	t.Run("package not found", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		_, _, err = root.LatestPackage("nonexistent")
		if !errors.Is(err, ErrPackageNotFound) {
			t.Fatalf("expected ErrPackageNotFound, got %v", err)
		}
	})
}

func TestGetPackageQuestions(t *testing.T) {
	t.Run("single repo", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "What hostname?"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		questions, err := root.GetPackageQuestions("nginx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(questions) != 2 {
			t.Fatalf("expected 2 questions, got %d", len(questions))
		}

		if questions["hostname"].Query != "What hostname?" {
			t.Fatalf("expected hostname query %q, got %q", "What hostname?", questions["hostname"].Query)
		}
		if questions["hostname"].Type != Hostname {
			t.Fatalf("expected hostname type %q, got %q", Hostname, questions["hostname"].Type)
		}
		if questions["port"].Query != "What port?" {
			t.Fatalf("expected port query %q, got %q", "What port?", questions["port"].Query)
		}
		if questions["port"].Type != Port {
			t.Fatalf("expected port type %q, got %q", Port, questions["port"].Type)
		}
	})

	t.Run("picks latest version questions", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "Old hostname question"
`)
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", `image: nginx:2.0
questions:
  hostname:
    query: "New hostname question"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		questions, err := root.GetPackageQuestions("nginx")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(questions) != 2 {
			t.Fatalf("expected 2 questions (from version 2.0), got %d", len(questions))
		}
		if questions["hostname"].Query != "New hostname question" {
			t.Fatalf("expected new hostname question, got %q", questions["hostname"].Query)
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		_, err = root.GetPackageQuestions("nonexistent")
		if !errors.Is(err, ErrPackageNotFound) {
			t.Fatalf("expected ErrPackageNotFound, got %v", err)
		}
	})

	t.Run("no questions returns empty map", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		questions, err := root.GetPackageQuestions("redis")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(questions) != 0 {
			t.Fatalf("expected nil or empty questions, got %v", questions)
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
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
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
		if pkgs[0] != "repo-a/nginx@2.0" {
			t.Fatalf("expected repo-a/nginx@2.0, got %s", pkgs[0])
		}
		if pkgs[1] != "repo-a/redis@7.0" {
			t.Fatalf("expected repo-a/redis@7.0, got %s", pkgs[1])
		}
	})

	t.Run("multiple repos no overlap", func(t *testing.T) {
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

		pkgs, err := root.ListPackages()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(pkgs) != 2 {
			t.Fatalf("expected 2 packages, got %d", len(pkgs))
		}

		p0, err := ParsePackageIdentity(pkgs[0])
		if err != nil {
			t.Fatalf("ParsePackageIdentity(%q): %v", pkgs[0], err)
		}
		p1, err := ParsePackageIdentity(pkgs[1])
		if err != nil {
			t.Fatalf("ParsePackageIdentity(%q): %v", pkgs[1], err)
		}
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
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
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
		if pkgs[0] != "repo-a/nginx@2.0" {
			t.Fatalf("expected repo-a/nginx@2.0, got %s", pkgs[0])
		}
	})

	t.Run("later repo higher version wins", func(t *testing.T) {
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
		if pkgs[0] != "repo-b/nginx@3.0" {
			t.Fatalf("expected repo-b/nginx@3.0, got %s", pkgs[0])
		}
	})

	t.Run("no packages returns empty", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// create the packages dir but leave it empty
		err = os.MkdirAll(filepath.Join(dir, "repo-a", PackagesDir), 0750)
		if err != nil {
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
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
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
			p, err := ParsePackageIdentity(pkg)
			if err != nil {
				t.Fatalf("ParsePackageIdentity(%q): %v", pkg, err)
			}
			expected := []string{"alpine", "nginx", "zookeeper"}
			if p.Name != expected[i] {
				t.Fatalf("expected package %d to be %q, got %q", i, expected[i], p.Name)
			}
		}
	})
}

func TestFindRepoForPackage(t *testing.T) {
	t.Run("found in single repo", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		repoName, err := root.FindRepoForPackage("nginx", "1.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repoName != "repo-a" {
			t.Fatalf("expected repo-a, got %s", repoName)
		}
	})

	t.Run("found in second repo", func(t *testing.T) {
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

		repoName, err := root.FindRepoForPackage("redis", "7.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repoName != "repo-b" {
			t.Fatalf("expected repo-b, got %s", repoName)
		}
	})

	t.Run("preferred repo wins on duplicate", func(t *testing.T) {
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

		writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0-from-a\n")
		writePackageYAML(t, dir, "repo-b", "nginx", "2.0", "image: nginx:2.0-from-b\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		repoName, err := root.FindRepoForPackage("nginx", "2.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repoName != "repo-a" {
			t.Fatalf("expected preferred repo repo-a, got %s", repoName)
		}
	})

	t.Run("not found", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		_, err = root.FindRepoForPackage("nonexistent", "1.0")
		if !errors.Is(err, ErrPackageNotFound) {
			t.Fatalf("expected ErrPackageNotFound, got %v", err)
		}
	})

	t.Run("version not found", func(t *testing.T) {
		dir := t.TempDir()
		repos := []Repository{
			{Name: "repo-a", URL: url.URL{Scheme: "https", Host: "example.com", Path: "/repo-a.git"}},
		}
		data := marshalJSON(t, repos)
		err := os.WriteFile(filepath.Join(dir, RepositoriesFile), data, 0600)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

		root, err := RepositoryRootFromBase(dir)
		if err != nil {
			t.Fatalf("RepositoryRootFromBase: %v", err)
		}

		_, err = root.FindRepoForPackage("nginx", "99.0")
		if !errors.Is(err, ErrPackageNotFound) {
			t.Fatalf("expected ErrPackageNotFound, got %v", err)
		}
	})
}
