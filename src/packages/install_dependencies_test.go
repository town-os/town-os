package packages

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSaveDependenciesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	// Create the installed directory structure and a fake package file so
	// Install doesn't fail on missing source.
	repoName := "default"
	pkgName := "myapp"

	pkgDir := filepath.Join(dir, repoName, PackagesDir, pkgName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx\n"), 0600); err != nil {
		t.Fatal(err)
	}

	deps := map[string]DependencyRecord{
		"db": {
			EffectiveName: "myapp--dep--db",
			Package:       "postgres",
			Repo:          "default",
			Version:       "15.0",
		},
		"cache": {
			EffectiveName: "myapp--dep--cache",
			Package:       "redis",
			Repo:          "default",
			Version:       "7.0",
		},
	}

	if err := mgr.SaveDependencies(repoName, pkgName, deps); err != nil {
		t.Fatalf("SaveDependencies: %v", err)
	}

	loaded, err := mgr.LoadDependencies(repoName, pkgName)
	if err != nil {
		t.Fatalf("LoadDependencies: %v", err)
	}

	if len(loaded) != len(deps) {
		t.Fatalf("expected %d deps, got %d", len(deps), len(loaded))
	}

	for key, want := range deps {
		got, ok := loaded[key]
		if !ok {
			t.Errorf("missing key %q", key)
			continue
		}
		if got.EffectiveName != want.EffectiveName {
			t.Errorf("key %q: effective_name = %q, want %q", key, got.EffectiveName, want.EffectiveName)
		}
		if got.Package != want.Package {
			t.Errorf("key %q: package = %q, want %q", key, got.Package, want.Package)
		}
		if got.Repo != want.Repo {
			t.Errorf("key %q: repo = %q, want %q", key, got.Repo, want.Repo)
		}
		if got.Version != want.Version {
			t.Errorf("key %q: version = %q, want %q", key, got.Version, want.Version)
		}
	}
}

func TestLoadDependenciesFileNotExist(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	deps, err := mgr.LoadDependencies("default", "nonexistent")
	if err != nil {
		t.Fatalf("LoadDependencies: %v", err)
	}
	if deps != nil {
		t.Errorf("expected nil, got %v", deps)
	}
}

func TestSaveDependenciesOverwrite(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	repoName := "default"
	pkgName := "myapp"

	original := map[string]DependencyRecord{
		"db": {EffectiveName: "myapp--dep--db", Package: "postgres", Repo: "default", Version: "14.0"},
	}

	if err := mgr.SaveDependencies(repoName, pkgName, original); err != nil {
		t.Fatal(err)
	}

	updated := map[string]DependencyRecord{
		"db": {EffectiveName: "myapp--dep--db", Package: "postgres", Repo: "default", Version: "15.0"},
	}

	if err := mgr.SaveDependencies(repoName, pkgName, updated); err != nil {
		t.Fatal(err)
	}

	loaded, err := mgr.LoadDependencies(repoName, pkgName)
	if err != nil {
		t.Fatal(err)
	}

	if loaded["db"].Version != "15.0" {
		t.Errorf("expected version 15.0, got %q", loaded["db"].Version)
	}
}

func TestSaveDependenciesConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	repoName := "default"
	pkgName := "myapp"

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			deps := map[string]DependencyRecord{
				"db": {
					EffectiveName: "myapp--dep--db",
					Package:       "postgres",
					Repo:          "default",
					Version:       "1.0",
				},
			}
			if err := mgr.SaveDependencies(repoName, pkgName, deps); err != nil {
				t.Errorf("concurrent save %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	loaded, err := mgr.LoadDependencies(repoName, pkgName)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(loaded))
	}
}

func TestMockSaveDependencies(t *testing.T) {
	mock := InitMockInstallManager()

	deps := map[string]DependencyRecord{
		"db": {EffectiveName: "myapp--dep--db", Package: "postgres", Repo: "default", Version: "15.0"},
	}

	if err := mock.SaveDependencies("default", "myapp", deps); err != nil {
		t.Fatal(err)
	}

	loaded, err := mock.LoadDependencies("default", "myapp")
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(loaded))
	}
	if loaded["db"].Version != "15.0" {
		t.Errorf("expected version 15.0, got %q", loaded["db"].Version)
	}
}

func TestMockLoadDependenciesNotSet(t *testing.T) {
	mock := InitMockInstallManager()

	deps, err := mock.LoadDependencies("default", "myapp")
	if err != nil {
		t.Fatal(err)
	}
	if deps != nil {
		t.Errorf("expected nil, got %v", deps)
	}
}
