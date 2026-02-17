package packages

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
)

// --- Interface conformance ---

func TestRepositoryRootImplementsRepositoryManager(t *testing.T) {
	var _ RepositoryManager = (*RepositoryRoot)(nil)
}

func TestMockRepositoryManagerImplementsRepositoryManager(t *testing.T) {
	var _ RepositoryManager = (*MockRepositoryManager)(nil)
}

// --- helpers ---

func testRepo(name string) Repository {
	return Repository{
		Name: name,
		URL:  url.URL{Scheme: "https", Host: "example.com", Path: "/" + name + ".git"},
	}
}

// --- Add tests ---

func TestMockRepositoryManagerAdd(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m.Items))
	}

	if m.Items[0].Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", m.Items[0].Name)
	}
}

func TestMockRepositoryManagerAddDuplicate(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := m.Add(testRepo("core")); err == nil {
		t.Fatal("expected error for duplicate add")
	}
}

func TestMockRepositoryManagerAddErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.AddErr = injected
	if err := m.Add(testRepo("core")); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	if len(m.Items) != 0 {
		t.Fatalf("expected 0 items after error, got %d", len(m.Items))
	}
}

// --- Remove tests ---

func TestMockRepositoryManagerRemove(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := m.Remove("core"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(m.Items))
	}
}

func TestMockRepositoryManagerRemovePreservesOthers(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Add(testRepo("extras")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := m.Remove("core"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(m.Items))
	}
	if m.Items[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %s", m.Items[0].Name)
	}
}

func TestMockRepositoryManagerRemoveNotFound(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Remove("nope"); err == nil {
		t.Fatal("expected error for nonexistent remove")
	}
}

func TestMockRepositoryManagerRemoveErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m.RemoveErr = injected
	if err := m.Remove("core"); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}

	if len(m.Items) != 1 {
		t.Fatalf("expected 1 item after error, got %d", len(m.Items))
	}
}

// --- Get tests ---

func TestMockRepositoryManagerGet(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := m.Get("core")
	if !ok {
		t.Fatal("expected to find core")
	}
	if r.Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", r.Name)
	}
}

func TestMockRepositoryManagerGetNotFound(t *testing.T) {
	m := InitMockRepositoryManager()

	_, ok := m.Get("nope")
	if ok {
		t.Fatal("expected not to find nonexistent repo")
	}
}

// --- List tests ---

func TestMockRepositoryManagerList(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Add(testRepo("extras")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repos, err := m.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	if repos[0].Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", repos[0].Name)
	}
	if repos[1].Name != "extras" {
		t.Fatalf("expected name %q, got %q", "extras", repos[1].Name)
	}
}

func TestMockRepositoryManagerListEmpty(t *testing.T) {
	m := InitMockRepositoryManager()

	repos, err := m.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(repos))
	}
}

func TestMockRepositoryManagerListReturnsCopy(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repos, err := m.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repos[0].Name = "mutated"

	if m.Items[0].Name != "core" {
		t.Fatal("List should return a copy, not a reference")
	}
}

func TestMockRepositoryManagerListErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.ListReposErr = injected
	_, err := m.List()
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Refresh tests ---

func TestMockRepositoryManagerRefresh(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Refresh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMockRepositoryManagerRefreshErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.RefreshErr = injected
	if err := m.Refresh(); err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- LoadAllPackages tests ---

func TestMockRepositoryManagerLoadAllPackagesEmpty(t *testing.T) {
	m := InitMockRepositoryManager()

	pkgs, err := m.LoadAllPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestMockRepositoryManagerLoadAllPackages(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
		"2.0": {Image: "nginx:2.0"},
	}
	m.Packages["redis"] = map[string]InputPackage{
		"7.0": {Image: "redis:7.0"},
	}

	pkgs, err := m.LoadAllPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 package names, got %d", len(pkgs))
	}

	if len(pkgs["nginx"]) != 2 {
		t.Fatalf("expected 2 nginx versions, got %d", len(pkgs["nginx"]))
	}

	if pkgs["nginx"]["1.0"].Image != "nginx:1.0" {
		t.Fatalf("expected nginx:1.0, got %s", pkgs["nginx"]["1.0"].Image)
	}
}

func TestMockRepositoryManagerLoadAllPackagesReturnsCopy(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
	}

	pkgs, err := m.LoadAllPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgs["nginx"]["1.0"] = InputPackage{Image: "mutated"}

	if m.Packages["nginx"]["1.0"].Image != "nginx:1.0" {
		t.Fatal("LoadAllPackages should return a copy, not a reference")
	}
}

func TestMockRepositoryManagerLoadAllPackagesErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.LoadAllErr = injected
	_, err := m.LoadAllPackages()
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- ListPackages tests ---

func TestMockRepositoryManagerListPackagesEmpty(t *testing.T) {
	m := InitMockRepositoryManager()

	pkgs, err := m.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestMockRepositoryManagerListPackages(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
		"2.0": {Image: "nginx:2.0"},
	}
	m.Packages["redis"] = map[string]InputPackage{
		"7.0": {Image: "redis:7.0"},
	}

	pkgs, err := m.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs))
	}

	found := map[string]bool{}
	for _, p := range pkgs {
		found[p] = true
	}

	if !found["nginx@2.0"] {
		t.Fatal("expected nginx@2.0 in list")
	}
	if !found["redis@7.0"] {
		t.Fatal("expected redis@7.0 in list")
	}
}

func TestMockRepositoryManagerListPackagesErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.ListPackagesErr = injected
	_, err := m.ListPackages()
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- LatestPackage tests ---

func TestMockRepositoryManagerLatestPackage(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
		"2.0": {Image: "nginx:2.0"},
	}

	pkg, version, err := m.LatestPackage("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != "2.0" {
		t.Fatalf("expected version 2.0, got %s", version)
	}
	if pkg.Image != "nginx:2.0" {
		t.Fatalf("expected image nginx:2.0, got %s", pkg.Image)
	}
}

func TestMockRepositoryManagerLatestPackageNotFound(t *testing.T) {
	m := InitMockRepositoryManager()

	_, _, err := m.LatestPackage("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestMockRepositoryManagerLatestPackageErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := fmt.Errorf("injected error")

	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
	}

	m.LatestErr = injected
	_, _, err := m.LatestPackage("nginx")
	if err != injected {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Call log tests ---

func TestMockRepositoryManagerCallLog(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.Get("core")
	if _, err := m.List(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Refresh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := m.LoadAllPackages(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := m.ListPackages(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err := m.LatestPackage("nginx"); err != nil && !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Remove("core"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 8 {
		t.Fatalf("expected 8 calls, got %d", len(calls))
	}

	expected := []string{"Add", "Get", "List", "Refresh", "LoadAllPackages", "ListPackages", "LatestPackage", "Remove"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestMockRepositoryManagerCallLogArgs(t *testing.T) {
	m := InitMockRepositoryManager()

	repo := testRepo("core")
	if err := m.Add(repo); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	args := calls[0].Args
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}

	got, ok := args[0].(Repository)
	if !ok {
		t.Fatalf("expected Repository arg, got %T", args[0])
	}
	if got.Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", got.Name)
	}
}

func TestMockRepositoryManagerCallLogReturnsCopy(t *testing.T) {
	m := InitMockRepositoryManager()

	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	calls[0].Method = "mutated"

	if m.Calls[0].Method != "Add" {
		t.Fatal("GetCalls should return a copy, not a reference")
	}
}

// --- Lifecycle ---

func TestMockRepositoryManagerLifecycle(t *testing.T) {
	m := InitMockRepositoryManager()

	// Start empty.
	_, ok := m.Get("core")
	if ok {
		t.Fatal("expected not to find core initially")
	}

	repos, err := m.List()
	if err != nil {
		t.Fatalf("List (initial): %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected 0 repos initially, got %d", len(repos))
	}

	// Add repositories.
	if err := m.Add(testRepo("core")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := m.Add(testRepo("extras")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repos, err = m.List()
	if err != nil {
		t.Fatalf("List (after add): %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(repos))
	}

	// Get an added repo.
	r, ok := m.Get("core")
	if !ok {
		t.Fatal("expected to find core")
	}
	if r.Name != "core" {
		t.Fatalf("expected name %q, got %q", "core", r.Name)
	}

	// Set up packages and list them.
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: "nginx:1.0"},
		"2.0": {Image: "nginx:2.0"},
	}

	pkgs, err := m.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0] != "nginx@2.0" {
		t.Fatalf("expected nginx@2.0, got %s", pkgs[0])
	}

	// Latest package.
	pkg, version, err := m.LatestPackage("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "2.0" {
		t.Fatalf("expected version 2.0, got %s", version)
	}
	if pkg.Image != "nginx:2.0" {
		t.Fatalf("expected image nginx:2.0, got %s", pkg.Image)
	}

	// Remove one repository.
	if err := m.Remove("core"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repos, err = m.List()
	if err != nil {
		t.Fatalf("List (after remove): %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo after remove, got %d", len(repos))
	}
	if repos[0].Name != "extras" {
		t.Fatalf("expected extras to remain, got %s", repos[0].Name)
	}

	// Refresh succeeds.
	if err := m.Refresh(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
