// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"go.yaml.in/yaml/v4"
)

func testCoreURL() url.URL {
	if raw := os.Getenv(packages.EnvTestRepoCoreURL); raw != "" {
		u, err := url.Parse(raw)
		if err == nil {
			return *u
		}
	}
	return url.URL{Scheme: "https", Host: "github.com", Path: "/town-os/test-packages-core.git"}
}

func testCoreURLString() string {
	u := testCoreURL()
	return u.String()
}

func testExtrasURL() url.URL {
	if raw := os.Getenv(packages.EnvTestRepoExtrasURL); raw != "" {
		u, err := url.Parse(raw)
		if err == nil {
			return *u
		}
	}
	return url.URL{Scheme: "https", Host: "github.com", Path: "/town-os/test-packages-extras.git"}
}

func testExtrasURLString() string {
	u := testExtrasURL()
	return u.String()
}

func repoCredentials() (string, string) {
	return os.Getenv(packages.EnvRepoUsername), os.Getenv(packages.EnvRepoPassword)
}

func newRepoWithCreds(baseDir, name string, u url.URL) (*packages.Repository, error) {
	user, pass := repoCredentials()
	return packages.NewRepository(baseDir, name, u, user, pass, &git.GoGitClient{Home: baseDir})
}

func setupRoot(t *testing.T, repos []packages.Repository) *packages.RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	err = os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600)
	if err != nil {
		t.Fatalf("write repositories file: %v", err)
	}

	root, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}
	return root
}

func TestRepositoryCloneAndLoadPackages(t *testing.T) {
	root := setupRoot(t, []packages.Repository{})

	repo := packages.Repository{Name: "core", URL: testCoreURL()}
	err := root.Add(repo)
	if err != nil {
		t.Fatalf("failed to add core repo: %v", err)
	}

	_, err = newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := repo.LoadPackages(root.BaseDir)
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	if len(pkgs) != 3 {
		t.Fatalf("expected 3 package names, got %d", len(pkgs))
	}

	demoNginxVersions, ok := pkgs["demo-nginx"]
	if !ok {
		t.Fatal("expected demo-nginx package")
	}
	if len(demoNginxVersions) != 1 {
		t.Fatalf("expected 1 demo-nginx version, got %d", len(demoNginxVersions))
	}
	if _, ok := demoNginxVersions["1.0"]; !ok {
		t.Fatal("expected demo-nginx version 1.0")
	}

	nginxVersions, ok := pkgs["nginx"]
	if !ok {
		t.Fatal("expected nginx package")
	}
	if len(nginxVersions) != 2 {
		t.Fatalf("expected 2 nginx versions, got %d", len(nginxVersions))
	}
	if _, ok := nginxVersions["1.0"]; !ok {
		t.Fatal("expected nginx version 1.0")
	}
	if _, ok := nginxVersions["2.0"]; !ok {
		t.Fatal("expected nginx version 2.0")
	}

	redisVersions, ok := pkgs["redis"]
	if !ok {
		t.Fatal("expected redis package")
	}
	if len(redisVersions) != 1 {
		t.Fatalf("expected 1 redis version, got %d", len(redisVersions))
	}
	if _, ok := redisVersions["7.0"]; !ok {
		t.Fatal("expected redis version 7.0")
	}
}

func TestRepositoryLoadAllPackagesMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}

	pkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("failed to load all packages: %v", err)
	}

	if len(pkgs) != 5 {
		t.Fatalf("expected 5 package names (demo-nginx, nginx, redis, postgres, mosquitto), got %d", len(pkgs))
	}

	for _, name := range []string{"demo-nginx", "nginx", "redis", "postgres", "mosquitto"} {
		if _, ok := pkgs[name]; !ok {
			t.Fatalf("expected package %s", name)
		}
	}

	// Verify version counts for known packages.
	if len(pkgs["nginx"]) != 2 {
		t.Fatalf("expected 2 nginx versions, got %d", len(pkgs["nginx"]))
	}
	if _, ok := pkgs["nginx"]["1.0"]; !ok {
		t.Fatal("expected nginx version 1.0")
	}
	if _, ok := pkgs["nginx"]["2.0"]; !ok {
		t.Fatal("expected nginx version 2.0")
	}
	if len(pkgs["redis"]) != 1 {
		t.Fatalf("expected 1 redis version, got %d", len(pkgs["redis"]))
	}
	if _, ok := pkgs["redis"]["7.0"]; !ok {
		t.Fatal("expected redis version 7.0")
	}
}

func TestRepositoryCompileLoadedPackage(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	nginx, ok := pkgs["nginx"]["1.0"]
	if !ok {
		t.Fatal("expected nginx 1.0")
	}

	compiled, err := nginx.Compile(packages.Responses{
		"hostname": "example",
		"port":     "8080",
	})
	if err != nil {
		t.Fatalf("failed to compile nginx package: %v", err)
	}

	if compiled.Image != "docker.io/library/nginx:1.26-alpine" {
		t.Fatalf("expected image docker.io/library/nginx:1.26-alpine, got %s", compiled.Image)
	}

	if compiled.Environment["NGINX_HOST"] != "example" {
		t.Fatalf("expected NGINX_HOST=example, got %s", compiled.Environment["NGINX_HOST"])
	}

	if compiled.Network.External[8080] != 80 {
		t.Fatalf("expected external port 8080->80, got %v", compiled.Network.External)
	}
}

func TestRepositoryCompileRedisCommand(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	redis, ok := pkgs["redis"]["7.0"]
	if !ok {
		t.Fatal("expected redis 7.0")
	}

	compiled, err := redis.Compile(packages.Responses{
		"port":      "6379",
		"password":  "secret",
		"maxmemory": "100mb",
	})
	if err != nil {
		t.Fatalf("failed to compile redis package: %v", err)
	}

	if len(compiled.Command) != 3 {
		t.Fatalf("expected 3 command args, got %d: %v", len(compiled.Command), compiled.Command)
	}
	if compiled.Command[0] != "redis-server" {
		t.Fatalf("expected command[0] = redis-server, got %s", compiled.Command[0])
	}
	if compiled.Command[1] != "--bind" {
		t.Fatalf("expected command[1] = --bind, got %s", compiled.Command[1])
	}
	if compiled.Command[2] != "0.0.0.0" {
		t.Fatalf("expected command[2] = 0.0.0.0, got %s", compiled.Command[2])
	}
}

func TestRepositoryCompileNginxNoCommand(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	nginx, ok := pkgs["nginx"]["1.0"]
	if !ok {
		t.Fatal("expected nginx 1.0")
	}

	compiled, err := nginx.Compile(packages.Responses{
		"hostname": "example",
		"port":     "8080",
	})
	if err != nil {
		t.Fatalf("failed to compile nginx package: %v", err)
	}

	if len(compiled.Command) != 0 {
		t.Fatalf("expected no command args for nginx, got %v", compiled.Command)
	}
}

func TestGetPackageQuestionsFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	questions, err := root.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("GetPackageQuestions: %v", err)
	}

	// nginx 2.0 is the latest version and should have hostname and port questions
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}

	hostname, ok := questions["hostname"]
	if !ok {
		t.Fatal("expected hostname question")
	}
	if hostname.Query == "" {
		t.Fatal("expected non-empty hostname query")
	}
	if hostname.Type != packages.Hostname {
		t.Fatalf("expected hostname type %q, got %q", packages.Hostname, hostname.Type)
	}

	port, ok := questions["port"]
	if !ok {
		t.Fatal("expected port question")
	}
	if port.Query == "" {
		t.Fatal("expected non-empty port query")
	}
	if port.Type != packages.Port {
		t.Fatalf("expected port type %q, got %q", packages.Port, port.Type)
	}
}

func TestGetPackageQuestionsFromMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras: %v", err)
	}

	// redis from core
	redisQ, err := root.GetPackageQuestions("redis")
	if err != nil {
		t.Fatalf("GetPackageQuestions redis: %v", err)
	}
	if _, ok := redisQ["password"]; !ok {
		t.Fatal("expected redis password question")
	}

	// postgres from extras
	pgQ, err := root.GetPackageQuestions("postgres")
	if err != nil {
		t.Fatalf("GetPackageQuestions postgres: %v", err)
	}
	for _, key := range []string{"user", "password", "dbname"} {
		if _, ok := pgQ[key]; !ok {
			t.Fatalf("expected postgres %s question", key)
		}
	}

	// nonexistent
	_, err = root.GetPackageQuestions("nonexistent")
	if !errors.Is(err, packages.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestListPackagesSingleRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages (demo-nginx, nginx, redis), got %d", len(pkgs))
	}

	// results are sorted by name
	if pkgs[0] != "core/demo-nginx@1.0" {
		t.Fatalf("expected core/demo-nginx@1.0, got %s", pkgs[0])
	}
	if pkgs[1] != "core/nginx@2.0" {
		t.Fatalf("expected core/nginx@2.0, got %s", pkgs[1])
	}
	if pkgs[2] != "core/redis@7.0" {
		t.Fatalf("expected core/redis@7.0, got %s", pkgs[2])
	}
}

func TestListPackagesMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	if len(pkgs) != 5 {
		t.Fatalf("expected 5 packages (demo-nginx, mosquitto, nginx, postgres, redis), got %d", len(pkgs))
	}

	// verify sorted and all expected names present
	found := map[string]bool{}
	for _, p := range pkgs {
		pi, err := packages.ParsePackageIdentity(p)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", p, err)
		}
		found[pi.Name] = true
	}

	for _, want := range []string{"demo-nginx", "mosquitto", "nginx", "postgres", "redis"} {
		if !found[want] {
			t.Fatalf("expected package %s to be present", want)
		}
	}

	// verify core packages still have correct latest versions
	pkgSet := map[string]bool{}
	for _, p := range pkgs {
		pkgSet[p] = true
	}
	if !pkgSet["core/nginx@2.0"] {
		t.Fatal("expected core/nginx@2.0")
	}
	if !pkgSet["core/redis@7.0"] {
		t.Fatal("expected core/redis@7.0")
	}
}

func TestListPackagesPreferenceOrder(t *testing.T) {
	// extras first, then core — extras takes precedence on ties
	root := setupRoot(t, []packages.Repository{
		{Name: "extras", URL: testExtrasURL()},
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	// same 5 packages regardless of order
	if len(pkgs) != 5 {
		t.Fatalf("expected 5 packages, got %d", len(pkgs))
	}

	// verify all expected names and versions present
	foundNames := map[string]bool{}
	for _, p := range pkgs {
		pi, err := packages.ParsePackageIdentity(p)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", p, err)
		}
		foundNames[pi.Name] = true
	}

	for _, name := range []string{"demo-nginx", "mosquitto", "nginx", "postgres", "redis"} {
		if !foundNames[name] {
			t.Fatalf("expected package %s to be present", name)
		}
	}

	// verify correct latest versions
	pkgSet := map[string]bool{}
	for _, p := range pkgs {
		pkgSet[p] = true
	}
	if !pkgSet["core/nginx@2.0"] {
		t.Fatal("expected core/nginx@2.0")
	}
	if !pkgSet["core/redis@7.0"] {
		t.Fatal("expected core/redis@7.0")
	}
}

// --- Installed package integration tests ---

func TestInstalledInstallFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("failed to install nginx 1.0: %v", err)
	}

	// Verify hard link resolves and the YAML is readable.
	link := filepath.Join(root.BaseDir, packages.InstalledDir, "core", "nginx", "1.0.yaml")
	content, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("could not read installed hard link: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty YAML content through hard link")
	}
}

func TestInstalledListFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	err = mgr.Install("core", "nginx", "nginx", "2.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}
	err = mgr.Install("core", "redis", "redis", "7.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("failed to list installed: %v", err)
	}

	if len(pkgs) != 3 {
		t.Fatalf("expected 3 installed entries, got %d", len(pkgs))
	}

	// Sorted: core/nginx@1.0, core/nginx@2.0, core/redis@7.0
	expected := []string{"core/nginx@1.0", "core/nginx@2.0", "core/redis@7.0"}
	for i, want := range expected {
		if pkgs[i] != want {
			t.Fatalf("entry %d: expected %s, got %s", i, want, pkgs[i])
		}
	}
}

func TestInstalledUninstallFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	err = mgr.Install("core", "nginx", "nginx", "2.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}
	err = mgr.Install("core", "redis", "redis", "7.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install redis@7.0: %v", err)
	}

	err = mgr.Uninstall("core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("failed to uninstall: %v", err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 installed after uninstall, got %d", len(pkgs))
	}
	if pkgs[0] != "core/redis@7.0" {
		t.Fatalf("expected core/redis@7.0 to remain, got %s", pkgs[0])
	}

	// Uninstalling again should fail.
	err = mgr.Uninstall("core", "nginx", "2.0")
	if err == nil {
		t.Fatal("expected error uninstalling already-removed package")
	}
	if !errors.Is(err, packages.ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestInstalledMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Install from different repos.
	err = mgr.Install("core", "nginx", "nginx", "2.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@2.0 from core: %v", err)
	}
	err = mgr.Install("extras", "postgres", "postgres", "16.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install postgres@16.0 from extras: %v", err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed packages, got %d", len(pkgs))
	}

	// Verify actual content.
	pkgSet := map[string]bool{}
	for _, p := range pkgs {
		pkgSet[p] = true
	}
	if !pkgSet["core/nginx@2.0"] {
		t.Fatal("expected core/nginx@2.0 in installed list")
	}
	if !pkgSet["extras/postgres@16.0"] {
		t.Fatal("expected extras/postgres@16.0 in installed list")
	}

	// Both hard links should resolve.
	for _, p := range pkgs {
		pi, err := packages.ParsePackageIdentity(p)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", p, err)
		}
		link := filepath.Join(root.BaseDir, packages.InstalledDir, pi.Repo, pi.Name, pi.Version+".yaml")
		_, err = os.ReadFile(link)
		if err != nil {
			t.Fatalf("hard link for %s does not resolve: %v", p, err)
		}
	}
}

func TestInstalledLifecycleWithRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Empty.
	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled (initial): %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected empty, got %d", len(pkgs))
	}

	// Install.
	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1, got %d", len(pkgs))
	}

	// Duplicate should fail.
	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error for duplicate install")
	}
	if !errors.Is(err, packages.ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}

	// Uninstall.
	err = mgr.Uninstall("core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("Uninstall nginx@1.0: %v", err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 after uninstall, got %d", len(pkgs))
	}

	// Can re-install after uninstall.
	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("re-install failed: %v", err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled after re-install: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 after re-install, got %d", len(pkgs))
	}
}

func TestRepositoryList(t *testing.T) {
	root := setupRoot(t, []packages.Repository{})

	// Empty initially.
	repos, err := root.List()
	if err != nil {
		t.Fatalf("List (initial): %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}

	// Add repos.
	err = root.Add(packages.Repository{Name: "core", URL: testCoreURL()})
	if err != nil {
		t.Fatalf("Add core: %v", err)
	}
	err = root.Add(packages.Repository{Name: "extras", URL: testExtrasURL()})
	if err != nil {
		t.Fatalf("Add extras: %v", err)
	}

	repos, err = root.List()
	if err != nil {
		t.Fatalf("List (after add): %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	names := map[string]bool{}
	for _, r := range repos {
		names[r.Name] = true
	}
	if !names["core"] {
		t.Fatal("expected core")
	}
	if !names["extras"] {
		t.Fatal("expected extras")
	}

	// Remove one.
	err = root.Remove("core")
	if err != nil {
		t.Fatalf("Remove core: %v", err)
	}

	repos, err = root.List()
	if err != nil {
		t.Fatalf("List (after remove): %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo after remove, got %d", len(repos))
	}
	if repos[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %s", repos[0].Name)
	}

	// Returns a copy.
	repos[0].Name = "mutated"
	recheck, err := root.List()
	if err != nil {
		t.Fatalf("List (recheck): %v", err)
	}
	if recheck[0].Name != "extras" {
		t.Fatal("List should return a copy, not a reference")
	}
}

func TestRepositoryListPersistence(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	// List from original root.
	repos, err := root.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	// Reload from disk and verify List() matches.
	reloaded, err := packages.RepositoryRootFromBase(root.BaseDir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	reloadedRepos, err := reloaded.List()
	if err != nil {
		t.Fatalf("reloaded List: %v", err)
	}
	if len(reloadedRepos) != 2 {
		t.Fatalf("expected 2 repos from reloaded, got %d", len(reloadedRepos))
	}
	if reloadedRepos[0].Name != repos[0].Name || reloadedRepos[1].Name != repos[1].Name {
		t.Fatalf("reloaded repos don't match: got %v", reloadedRepos)
	}
}

func TestRepositoryAddAndRemovePersistence(t *testing.T) {
	root := setupRoot(t, []packages.Repository{})

	err := root.Add(packages.Repository{Name: "core", URL: testCoreURL()})
	if err != nil {
		t.Fatalf("failed to add core: %v", err)
	}
	err = root.Add(packages.Repository{Name: "extras", URL: testExtrasURL()})
	if err != nil {
		t.Fatalf("failed to add extras: %v", err)
	}

	// reload and verify both exist
	reloaded, err := packages.RepositoryRootFromBase(root.BaseDir)
	if err != nil {
		t.Fatalf("failed to reload: %v", err)
	}
	if len(reloaded.Items) != 2 {
		t.Fatalf("expected 2 repos after add, got %d", len(reloaded.Items))
	}

	// remove one
	err = reloaded.Remove("extras")
	if err != nil {
		t.Fatalf("failed to remove extras: %v", err)
	}

	// reload again and verify only one remains
	reloaded, err = packages.RepositoryRootFromBase(root.BaseDir)
	if err != nil {
		t.Fatalf("failed to reload after remove: %v", err)
	}
	if len(reloaded.Items) != 1 {
		t.Fatalf("expected 1 repo after remove, got %d", len(reloaded.Items))
	}
	if _, ok := reloaded.Get("core"); !ok {
		t.Fatal("expected core repo to remain")
	}
}

func TestInstalledSymlinkContentMatchesSource(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	// Read through hard link.
	link := filepath.Join(root.BaseDir, packages.InstalledDir, "core", "nginx", "1.0.yaml")
	linkContent, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read through hard link: %v", err)
	}

	// Read source directly.
	source := filepath.Join(root.BaseDir, "core", packages.PackagesDir, "nginx", "1.0.yaml")
	sourceContent, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source file: %v", err)
	}

	if string(linkContent) != string(sourceContent) {
		t.Fatalf("hard link content does not match source:\nlink:   %s\nsource: %s", linkContent, sourceContent)
	}
}

func TestInstalledEmptyDirCleanupAfterUninstall(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Install two versions of the same package.
	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}
	err = mgr.Install("core", "nginx", "nginx", "2.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@2.0: %v", err)
	}

	pkgDir := filepath.Join(root.BaseDir, packages.InstalledDir, "core", "nginx")

	// Verify directory exists.
	_, err = os.Stat(pkgDir)
	if err != nil {
		t.Fatalf("expected nginx directory to exist: %v", err)
	}

	// Uninstall first version — directory should remain.
	err = mgr.Uninstall("core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("Uninstall nginx@1.0: %v", err)
	}
	_, err = os.Stat(pkgDir)
	if err != nil {
		t.Fatalf("expected nginx directory to still exist after partial uninstall: %v", err)
	}

	// Uninstall second version — directory should be cleaned up.
	err = mgr.Uninstall("core", "nginx", "2.0")
	if err != nil {
		t.Fatalf("Uninstall nginx@2.0: %v", err)
	}
	_, err = os.Stat(pkgDir)
	if !os.IsNotExist(err) {
		t.Fatalf("expected nginx directory to be removed after full uninstall, got err: %v", err)
	}
}

func TestInstalledInstallNonexistentPackage(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Nonexistent package name.
	err = mgr.Install("core", "nonexistent", "nonexistent", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error installing nonexistent package")
	}

	// Existing package, nonexistent version.
	err = mgr.Install("core", "nginx", "nginx", "99.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error installing nonexistent version")
	}

	// Nonexistent repo name.
	err = mgr.Install("no-such-repo", "nginx", "nginx", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected error installing from nonexistent repo")
	}

	// Verify nothing got installed.
	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed after failed installs, got %d", len(pkgs))
	}
}

func TestInstalledInstallAllAvailablePackages(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras: %v", err)
	}

	// Load all packages to know what's available.
	allPkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("LoadAllPackages: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Install every version of every package.
	// Track which repo provides each package by loading per-repo.
	for _, repoName := range []string{"core", "extras"} {
		repo, ok := root.Get(repoName)
		if !ok {
			t.Fatalf("expected repo %s", repoName)
		}
		repoPkgs, err := repo.LoadPackages(root.BaseDir)
		if err != nil {
			t.Fatalf("LoadPackages %s: %v", repoName, err)
		}
		for name, versions := range repoPkgs {
			for version := range versions {
				err = mgr.Install(repoName, name, name, version, packages.Responses{})
				if err != nil {
					t.Fatalf("Install %s@%s from %s: %v", name, version, repoName, err)
				}
			}
		}
	}

	installed, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	// Count total expected versions.
	totalVersions := 0
	for _, versions := range allPkgs {
		totalVersions += len(versions)
	}

	if len(installed) != totalVersions {
		t.Fatalf("expected %d installed entries, got %d", totalVersions, len(installed))
	}

	// Verify sorted order: by repo, then name, then version.
	for i := 1; i < len(installed); i++ {
		prev, err := packages.ParsePackageIdentity(installed[i-1])
		if err != nil {
			t.Fatalf("ParsePackageIdentity(%q): %v", installed[i-1], err)
		}
		curr, err := packages.ParsePackageIdentity(installed[i])
		if err != nil {
			t.Fatalf("ParsePackageIdentity(%q): %v", installed[i], err)
		}
		if prev.Repo > curr.Repo {
			t.Fatalf("not sorted by repo at index %d: %s > %s", i, prev.Repo, curr.Repo)
		}
		if prev.Repo == curr.Repo && prev.Name > curr.Name {
			t.Fatalf("not sorted by name at index %d: %s > %s", i, prev.Name, curr.Name)
		}
		if prev.Repo == curr.Repo && prev.Name == curr.Name && packages.CompareVersions(prev.Version, curr.Version) >= 0 {
			t.Fatalf("not sorted by version at index %d: %s >= %s", i, installed[i-1], installed[i])
		}
	}

	// Verify all hard links resolve.
	for _, p := range installed {
		pi, err := packages.ParsePackageIdentity(p)
		if err != nil {
			t.Fatalf("invalid package identity %q: %v", p, err)
		}
		link := filepath.Join(root.BaseDir, packages.InstalledDir, pi.Repo, pi.Name, pi.Version+".yaml")
		content, err := os.ReadFile(link)
		if err != nil {
			t.Fatalf("hard link for %s does not resolve: %v", p, err)
		}
		if len(content) == 0 {
			t.Fatalf("expected non-empty content for %s", p)
		}
	}
}

func TestInstalledListBeforeAnyInstalls(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	// Do not clone or install anything — the installed dir does not exist.
	mgr := packages.NewInstallManager(root.BaseDir)

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled on missing dir: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 installed when dir missing, got %d", len(pkgs))
	}
}

func TestInstalledSeparateManagersShareState(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}

	mgr1 := packages.NewInstallManager(root.BaseDir)
	mgr2 := packages.NewInstallManager(root.BaseDir)

	// Install from mgr1.
	err = mgr1.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("mgr1 Install nginx@1.0: %v", err)
	}

	// List from mgr2 — should see what mgr1 installed.
	pkgs, err := mgr2.ListInstalled()
	if err != nil {
		t.Fatalf("mgr2 ListInstalled: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected mgr2 to see 1 installed, got %d", len(pkgs))
	}
	if pkgs[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs[0])
	}

	// Duplicate from mgr2 should fail.
	err = mgr2.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err == nil {
		t.Fatal("expected duplicate install error from mgr2")
	}
	if !errors.Is(err, packages.ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled from mgr2, got %v", err)
	}

	// Uninstall from mgr2.
	err = mgr2.Uninstall("core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("mgr2 Uninstall nginx@1.0: %v", err)
	}

	// mgr1 should see it gone.
	pkgs, err = mgr1.ListInstalled()
	if err != nil {
		t.Fatalf("mgr1 ListInstalled after mgr2 uninstall: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 after mgr2 uninstall, got %d", len(pkgs))
	}
}

// --- FindRepoForPackage integration tests ---

func TestFindRepoForPackageSingleRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	repoName, err := root.FindRepoForPackage("nginx", "1.0")
	if err != nil {
		t.Fatalf("FindRepoForPackage nginx@1.0: %v", err)
	}
	if repoName != "core" {
		t.Fatalf("expected core, got %s", repoName)
	}
}

func TestFindRepoForPackageMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
		{Name: "extras", URL: testExtrasURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}
	_, err = newRepoWithCreds(root.BaseDir, "extras", testExtrasURL())
	if err != nil {
		t.Fatalf("failed to clone extras: %v", err)
	}

	// nginx is in core
	repoName, err := root.FindRepoForPackage("nginx", "2.0")
	if err != nil {
		t.Fatalf("FindRepoForPackage nginx@2.0: %v", err)
	}
	if repoName != "core" {
		t.Fatalf("expected core for nginx, got %s", repoName)
	}

	// postgres is in extras
	repoName, err = root.FindRepoForPackage("postgres", "16.0")
	if err != nil {
		t.Fatalf("FindRepoForPackage postgres@16.0: %v", err)
	}
	if repoName != "extras" {
		t.Fatalf("expected extras for postgres, got %s", repoName)
	}
}

func TestFindRepoForPackageNotFound(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}

	_, err = root.FindRepoForPackage("nonexistent", "1.0")
	if !errors.Is(err, packages.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestFindRepoForPackageVersionNotFound(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}

	_, err = root.FindRepoForPackage("nginx", "99.0")
	if !errors.Is(err, packages.ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestInstalledCompileThroughSymlink(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "core", URL: testCoreURL()},
	})

	_, err := newRepoWithCreds(root.BaseDir, "core", testCoreURL())
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	err = mgr.Install("core", "nginx", "nginx", "1.0", packages.Responses{})
	if err != nil {
		t.Fatalf("Install nginx@1.0: %v", err)
	}

	// Read YAML through the installed hard link.
	link := filepath.Join(root.BaseDir, packages.InstalledDir, "core", "nginx", "1.0.yaml")
	f, err := os.Open(link)
	if err != nil {
		t.Fatalf("open installed hard link: %v", err)
	}
	t.Cleanup(func() {
		err := f.Close()
		if err != nil {
			t.Errorf("close installed hard link: %v", err)
		}
	})

	var ip packages.InputPackage
	err = yaml.NewDecoder(f).Decode(&ip)
	if err != nil {
		t.Fatalf("decode YAML through symlink: %v", err)
	}

	// Compile with responses and verify the result.
	compiled, err := ip.Compile(packages.Responses{
		"hostname": "webserver",
		"port":     "9090",
	})
	if err != nil {
		t.Fatalf("Compile through symlink: %v", err)
	}

	if compiled.Image != "docker.io/library/nginx:1.26-alpine" {
		t.Fatalf("expected image docker.io/library/nginx:1.26-alpine, got %s", compiled.Image)
	}
	if compiled.Environment["NGINX_HOST"] != "webserver" {
		t.Fatalf("expected NGINX_HOST=webserver, got %s", compiled.Environment["NGINX_HOST"])
	}
	if compiled.Network.External[9090] != 80 {
		t.Fatalf("expected external port 9090->80, got %v", compiled.Network.External)
	}
}
