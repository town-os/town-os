package integration_test

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

var (
	coreURL   = url.URL{Scheme: "https", Host: "gitea.com", Path: "/town-os/test-packages-core.git"}
	extrasURL = url.URL{Scheme: "https", Host: "gitea.com", Path: "/town-os/test-packages-extras.git"}
)

func setupRoot(t *testing.T, repos []packages.Repository) *packages.RepositoryRoot {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0644); err != nil {
		t.Fatal(err)
	}

	root, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}
	return root
}

func TestRepositoryCloneAndLoadPackages(t *testing.T) {
	root := setupRoot(t, []packages.Repository{})

	repo := packages.Repository{Name: "test-packages-core", URL: coreURL}
	if err := root.Add(repo); err != nil {
		t.Fatalf("failed to add core repo: %v", err)
	}

	_, err := packages.NewRepository(root.BaseDir, coreURL)
	if err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := repo.LoadPackages(root.BaseDir)
	if err != nil {
		t.Fatalf("failed to load packages: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 package names, got %d", len(pkgs))
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
		{Name: "test-packages-core", URL: coreURL},
		{Name: "test-packages-extras", URL: extrasURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}
	if _, err := packages.NewRepository(root.BaseDir, extrasURL); err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}

	pkgs, err := root.LoadAllPackages()
	if err != nil {
		t.Fatalf("failed to load all packages: %v", err)
	}

	if len(pkgs) != 4 {
		t.Fatalf("expected 4 package names (nginx, redis, postgres, mosquitto), got %d", len(pkgs))
	}

	for _, name := range []string{"nginx", "redis", "postgres", "mosquitto"} {
		if _, ok := pkgs[name]; !ok {
			t.Fatalf("expected package %s", name)
		}
	}
}

func TestRepositoryCompileLoadedPackage(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
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
		"hostname": "example.com",
		"port":     "8080",
	})
	if err != nil {
		t.Fatalf("failed to compile nginx package: %v", err)
	}

	if compiled.Image != "nginx:1.0" {
		t.Fatalf("expected image nginx:1.0, got %s", compiled.Image)
	}

	if compiled.Environment["NGINX_HOST"] != "example.com" {
		t.Fatalf("expected NGINX_HOST=example.com, got %s", compiled.Environment["NGINX_HOST"])
	}

	if compiled.Network.External[8080] != 80 {
		t.Fatalf("expected external port 8080->80, got %v", compiled.Network.External)
	}
}

func TestListPackagesSingleRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages (nginx, redis), got %d", len(pkgs))
	}

	// results are sorted by name
	if pkgs[0].Name != "nginx" {
		t.Fatalf("expected first package to be nginx, got %s", pkgs[0].Name)
	}
	if pkgs[0].Version != "2.0" {
		t.Fatalf("expected nginx latest version 2.0, got %s", pkgs[0].Version)
	}

	if pkgs[1].Name != "redis" {
		t.Fatalf("expected second package to be redis, got %s", pkgs[1].Name)
	}
	if pkgs[1].Version != "7.0" {
		t.Fatalf("expected redis latest version 7.0, got %s", pkgs[1].Version)
	}
}

func TestListPackagesMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
		{Name: "test-packages-extras", URL: extrasURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}
	if _, err := packages.NewRepository(root.BaseDir, extrasURL); err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	if len(pkgs) != 4 {
		t.Fatalf("expected 4 packages (mosquitto, nginx, postgres, redis), got %d", len(pkgs))
	}

	// verify sorted and all expected names present
	names := make([]string, len(pkgs))
	for i, p := range pkgs {
		names[i] = p.Name
	}

	expected := []string{"mosquitto", "nginx", "postgres", "redis"}
	for i, want := range expected {
		if names[i] != want {
			t.Fatalf("expected package %d to be %q, got %q (full list: %v)", i, want, names[i], names)
		}
	}

	// verify core packages still have correct latest versions
	for _, p := range pkgs {
		switch p.Name {
		case "nginx":
			if p.Version != "2.0" {
				t.Fatalf("expected nginx@2.0, got %s", p.Version)
			}
		case "redis":
			if p.Version != "7.0" {
				t.Fatalf("expected redis@7.0, got %s", p.Version)
			}
		}
	}
}

func TestListPackagesPreferenceOrder(t *testing.T) {
	// extras first, then core — extras takes precedence on ties
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-extras", URL: extrasURL},
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, extrasURL); err != nil {
		t.Fatalf("failed to clone extras repo: %v", err)
	}
	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	pkgs, err := root.ListPackages()
	if err != nil {
		t.Fatalf("failed to list packages: %v", err)
	}

	// same 4 packages regardless of order
	if len(pkgs) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs))
	}

	// verify all expected names present
	found := map[string]bool{}
	for _, p := range pkgs {
		found[p.Name] = true
	}

	for _, name := range []string{"mosquitto", "nginx", "postgres", "redis"} {
		if !found[name] {
			t.Fatalf("expected package %s to be present", name)
		}
	}
}

// --- Installed package integration tests ---

func TestInstalledInstallFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	if err := mgr.Install("test-packages-core", "nginx", "1.0"); err != nil {
		t.Fatalf("failed to install nginx 1.0: %v", err)
	}

	// Verify symlink resolves and the YAML is readable.
	link := filepath.Join(root.BaseDir, packages.InstalledDir, "nginx", "1.0.yaml")
	content, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("could not read installed symlink: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty YAML content through symlink")
	}
}

func TestInstalledListFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	if err := mgr.Install("test-packages-core", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("test-packages-core", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("test-packages-core", "redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("failed to list installed: %v", err)
	}

	if len(pkgs) != 3 {
		t.Fatalf("expected 3 installed entries, got %d", len(pkgs))
	}

	// Sorted: nginx@1.0, nginx@2.0, redis@7.0
	expected := []struct{ name, version string }{
		{"nginx", "1.0"},
		{"nginx", "2.0"},
		{"redis", "7.0"},
	}
	for i, want := range expected {
		if pkgs[i].Name != want.name || pkgs[i].Version != want.version {
			t.Fatalf("entry %d: expected %s@%s, got %s@%s", i, want.name, want.version, pkgs[i].Name, pkgs[i].Version)
		}
	}
}

func TestInstalledUninstallFromRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core repo: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	if err := mgr.Install("test-packages-core", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("test-packages-core", "redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Uninstall("nginx", "2.0"); err != nil {
		t.Fatalf("failed to uninstall: %v", err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 installed after uninstall, got %d", len(pkgs))
	}
	if pkgs[0].Name != "redis" {
		t.Fatalf("expected redis to remain, got %s", pkgs[0].Name)
	}

	// Uninstalling again should fail.
	err = mgr.Uninstall("nginx", "2.0")
	if err == nil {
		t.Fatal("expected error uninstalling already-removed package")
	}
	if !errors.Is(err, packages.ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestInstalledMultipleRepos(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
		{Name: "test-packages-extras", URL: extrasURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}
	if _, err := packages.NewRepository(root.BaseDir, extrasURL); err != nil {
		t.Fatalf("failed to clone extras: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Install from different repos.
	if err := mgr.Install("test-packages-core", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("test-packages-extras", "postgres", "17.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 installed packages, got %d", len(pkgs))
	}

	// Both symlinks should resolve.
	for _, p := range pkgs {
		link := filepath.Join(root.BaseDir, packages.InstalledDir, p.Name, p.Version+".yaml")
		if _, err := os.ReadFile(link); err != nil {
			t.Fatalf("symlink for %s@%s does not resolve: %v", p.Name, p.Version, err)
		}
	}
}

func TestInstalledLifecycleWithRepo(t *testing.T) {
	root := setupRoot(t, []packages.Repository{
		{Name: "test-packages-core", URL: coreURL},
	})

	if _, err := packages.NewRepository(root.BaseDir, coreURL); err != nil {
		t.Fatalf("failed to clone core: %v", err)
	}

	mgr := packages.NewInstallManager(root.BaseDir)

	// Empty.
	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected empty, got %d", len(pkgs))
	}

	// Install.
	if err := mgr.Install("test-packages-core", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1, got %d", len(pkgs))
	}

	// Duplicate should fail.
	err = mgr.Install("test-packages-core", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for duplicate install")
	}
	if !errors.Is(err, packages.ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}

	// Uninstall.
	if err := mgr.Uninstall("nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 after uninstall, got %d", len(pkgs))
	}

	// Can re-install after uninstall.
	if err := mgr.Install("test-packages-core", "nginx", "1.0"); err != nil {
		t.Fatalf("re-install failed: %v", err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 after re-install, got %d", len(pkgs))
	}
}

func TestRepositoryAddAndRemovePersistence(t *testing.T) {
	root := setupRoot(t, []packages.Repository{})

	if err := root.Add(packages.Repository{Name: "test-packages-core", URL: coreURL}); err != nil {
		t.Fatalf("failed to add core: %v", err)
	}
	if err := root.Add(packages.Repository{Name: "test-packages-extras", URL: extrasURL}); err != nil {
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
	if err := reloaded.Remove("test-packages-extras"); err != nil {
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
	if _, ok := reloaded.Get("test-packages-core"); !ok {
		t.Fatal("expected core repo to remain")
	}
}
