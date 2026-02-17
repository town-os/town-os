package packages

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- Install tests ---

func TestInstallCreatesSymlink(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link := filepath.Join(dir, InstalledDir, "nginx", "1.0.yaml")
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("expected symlink to exist: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected a symlink")
	}

	// Verify the symlink target.
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	expected := filepath.Join("..", "..", "repo-a", PackagesDir, "nginx", "1.0.yaml")
	if target != expected {
		t.Fatalf("expected symlink target %q, got %q", expected, target)
	}

	// Verify the symlink is readable (resolves correctly).
	content, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("could not read through symlink: %v", err)
	}
	if string(content) != "image: nginx:1.0\n" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestInstallCreatesIntermediateDirectories(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, InstalledDir))
	if err != nil {
		t.Fatalf("installed dir missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("installed should be a directory")
	}

	info, err = os.Stat(filepath.Join(dir, InstalledDir, "nginx"))
	if err != nil {
		t.Fatalf("package dir missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("package dir should be a directory")
	}
}

func TestInstallAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	err := mgr.Install("repo-a", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for already installed package")
	}
	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}
}

func TestInstallSourceNotFound(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	err := mgr.Install("repo-a", "nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestInstallMultipleVersions(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-a", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}

	for _, v := range []string{"1.0", "2.0"} {
		link := filepath.Join(dir, InstalledDir, "nginx", v+".yaml")
		if _, err := os.Lstat(link); err != nil {
			t.Fatalf("expected %s symlink: %v", v, err)
		}
	}
}

func TestInstallMultiplePackages(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writePackageYAML(t, dir, "repo-b", "redis", "7.0", "image: redis:7.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-b", "redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"nginx", "redis"} {
		info, err := os.Stat(filepath.Join(dir, InstalledDir, name))
		if err != nil {
			t.Fatalf("expected %s dir: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s should be a directory", name)
		}
	}
}

// --- Uninstall tests ---

func TestUninstallRemovesSymlink(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Uninstall("nginx", "1.0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	link := filepath.Join(dir, InstalledDir, "nginx", "1.0.yaml")
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("expected symlink to be removed")
	}
}

func TestUninstallCleansEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Uninstall("nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	pkgDir := filepath.Join(dir, InstalledDir, "nginx")
	if _, err := os.Stat(pkgDir); !os.IsNotExist(err) {
		t.Fatal("expected empty package directory to be removed")
	}
}

func TestUninstallPreservesOtherVersions(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-a", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}

	if err := mgr.Uninstall("nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, InstalledDir, "nginx", "2.0.yaml")
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected 2.0 symlink to remain: %v", err)
	}

	pkgDir := filepath.Join(dir, InstalledDir, "nginx")
	if _, err := os.Stat(pkgDir); err != nil {
		t.Fatalf("expected package directory to remain: %v", err)
	}
}

func TestUninstallNotInstalled(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	err := mgr.Uninstall("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for not-installed package")
	}
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestUninstallRejectsNonSymlink(t *testing.T) {
	dir := t.TempDir()

	pkgDir := filepath.Join(dir, InstalledDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewInstallManager(dir)
	err := mgr.Uninstall("nginx", "1.0")
	if err == nil {
		t.Fatal("expected error for non-symlink file")
	}
}

// --- ListInstalled tests ---

func TestListInstalledEmpty(t *testing.T) {
	dir := t.TempDir()
	mgr := NewInstallManager(dir)

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestListInstalledPackages(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")
	writePackageYAML(t, dir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-a", "redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	if pkgs[0].Name != "nginx" || pkgs[0].Version != "2.0" {
		t.Fatalf("expected nginx@2.0, got %s@%s", pkgs[0].Name, pkgs[0].Version)
	}
	if pkgs[1].Name != "redis" || pkgs[1].Version != "7.0" {
		t.Fatalf("expected redis@7.0, got %s@%s", pkgs[1].Name, pkgs[1].Version)
	}
}

func TestListInstalledMultipleVersions(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-a", "nginx", "2.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(pkgs))
	}

	if pkgs[0].Version != "1.0" {
		t.Fatalf("expected first version 1.0, got %s", pkgs[0].Version)
	}
	if pkgs[1].Version != "2.0" {
		t.Fatalf("expected second version 2.0, got %s", pkgs[1].Version)
	}
}

func TestListInstalledIgnoresNonSymlinks(t *testing.T) {
	dir := t.TempDir()

	pkgDir := filepath.Join(dir, InstalledDir, "nginx")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte("image: nginx:1.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewInstallManager(dir)
	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages (non-symlinks ignored), got %d", len(pkgs))
	}
}

func TestListInstalledIgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")

	mgr := NewInstallManager(dir)
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, InstalledDir, "nginx", "README.md"), []byte("notes"), 0644); err != nil {
		t.Fatal(err)
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
}

func TestListInstalledSortedByNameThenVersion(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "zookeeper", "1.0", "image: zk:1.0\n")
	writePackageYAML(t, dir, "repo-a", "alpine", "3.18", "image: alpine:3.18\n")
	writePackageYAML(t, dir, "repo-a", "alpine", "3.17", "image: alpine:3.17\n")
	writePackageYAML(t, dir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	mgr := NewInstallManager(dir)
	for _, pkg := range []struct{ repo, name, version string }{
		{"repo-a", "zookeeper", "1.0"},
		{"repo-a", "alpine", "3.18"},
		{"repo-a", "alpine", "3.17"},
		{"repo-a", "nginx", "2.0"},
	} {
		if err := mgr.Install(pkg.repo, pkg.name, pkg.version); err != nil {
			t.Fatal(err)
		}
	}

	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(pkgs))
	}

	expected := []struct{ name, version string }{
		{"alpine", "3.17"},
		{"alpine", "3.18"},
		{"nginx", "2.0"},
		{"zookeeper", "1.0"},
	}
	for i, want := range expected {
		if pkgs[i].Name != want.name || pkgs[i].Version != want.version {
			t.Fatalf("entry %d: expected %s@%s, got %s@%s", i, want.name, want.version, pkgs[i].Name, pkgs[i].Version)
		}
	}
}

// --- Install + Uninstall + ListInstalled lifecycle ---

func TestInstallUninstallListLifecycle(t *testing.T) {
	dir := t.TempDir()
	writePackageYAML(t, dir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writePackageYAML(t, dir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

	mgr := NewInstallManager(dir)

	// Start empty.
	pkgs, err := mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected empty, got %d", len(pkgs))
	}

	// Install two packages.
	if err := mgr.Install("repo-a", "nginx", "1.0"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Install("repo-a", "redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d", len(pkgs))
	}

	// Uninstall one.
	if err := mgr.Uninstall("nginx", "1.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 after uninstall, got %d", len(pkgs))
	}
	if pkgs[0].Name != "redis" {
		t.Fatalf("expected redis to remain, got %s", pkgs[0].Name)
	}

	// Uninstall the other.
	if err := mgr.Uninstall("redis", "7.0"); err != nil {
		t.Fatal(err)
	}

	pkgs, err = mgr.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected 0 after full uninstall, got %d", len(pkgs))
	}
}
