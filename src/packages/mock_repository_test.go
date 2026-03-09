// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"errors"
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

func testRepo(t *testing.T, name string) Repository {
	t.Helper()
	p, err := url.JoinPath("/", name+".git")
	if err != nil {
		t.Fatalf("url.JoinPath for %q: %v", name, err)
	}
	return Repository{
		Name: name,
		URL:  url.URL{Scheme: "https", Host: "example.com", Path: p},
	}
}

// --- Add tests ---

func TestMockRepositoryManagerAdd(t *testing.T) {
	m := InitMockRepositoryManager()

	err := m.Add(testRepo(t, "core"))
	if err != nil {
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

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = m.Add(testRepo(t, "core"))
	if err == nil {
		t.Fatal("expected error for duplicate add")
	}
}

func TestMockRepositoryManagerAddErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := errors.New("injected error")

	m.AddErr = injected
	err := m.Add(testRepo(t, "core"))
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	if len(m.Items) != 0 {
		t.Fatalf("expected 0 items after error, got %d", len(m.Items))
	}
}

// --- Remove tests ---

func TestMockRepositoryManagerRemove(t *testing.T) {
	m := InitMockRepositoryManager()

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = m.Remove("core")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(m.Items))
	}
}

func TestMockRepositoryManagerRemovePreservesOthers(t *testing.T) {
	m := InitMockRepositoryManager()

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = m.Add(testRepo(t, "extras"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = m.Remove("core")
	if err != nil {
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

	err := m.Remove("nope")
	if err == nil {
		t.Fatal("expected error for nonexistent remove")
	}
}

func TestMockRepositoryManagerRemoveErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := errors.New("injected error")

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m.RemoveErr = injected
	err = m.Remove("core")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}

	if len(m.Items) != 1 {
		t.Fatalf("expected 1 item after error, got %d", len(m.Items))
	}
}

// --- Get tests ---

func TestMockRepositoryManagerGet(t *testing.T) {
	m := InitMockRepositoryManager()

	err := m.Add(testRepo(t, "core"))
	if err != nil {
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

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = m.Add(testRepo(t, "extras"))
	if err != nil {
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

	err := m.Add(testRepo(t, "core"))
	if err != nil {
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
	injected := errors.New("injected error")

	m.ListReposErr = injected
	_, err := m.List()
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Refresh tests ---

func TestMockRepositoryManagerRefresh(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Refresh()

	errs := m.RefreshErrors()
	if errs != nil {
		t.Fatalf("expected nil errors, got %v", errs)
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
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}},
	}
	m.Packages["redis"] = map[string]InputPackage{
		"7.0": {Image: InputPackageImage{URL: "redis:7.0"}},
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

	if pkgs["nginx"]["1.0"].Image.URL != "nginx:1.0" {
		t.Fatalf("expected nginx:1.0, got %s", pkgs["nginx"]["1.0"].Image.URL)
	}
}

func TestMockRepositoryManagerLoadAllPackagesReturnsCopy(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
	}

	pkgs, err := m.LoadAllPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkgs["nginx"]["1.0"] = InputPackage{Image: InputPackageImage{URL: "mutated"}}

	if m.Packages["nginx"]["1.0"].Image.URL != "nginx:1.0" {
		t.Fatal("LoadAllPackages should return a copy, not a reference")
	}
}

func TestMockRepositoryManagerLoadAllPackagesErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := errors.New("injected error")

	m.LoadAllErr = injected
	_, err := m.LoadAllPackages()
	if !errors.Is(err, injected) {
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
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}},
	}
	m.Packages["redis"] = map[string]InputPackage{
		"7.0": {Image: InputPackageImage{URL: "redis:7.0"}},
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
	injected := errors.New("injected error")

	m.ListPackagesErr = injected
	_, err := m.ListPackages()
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- LatestPackage tests ---

func TestMockRepositoryManagerLatestPackage(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}},
	}

	pkg, version, err := m.LatestPackage("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if version != "2.0" {
		t.Fatalf("expected version 2.0, got %s", version)
	}
	if pkg.Image.URL != "nginx:2.0" {
		t.Fatalf("expected image nginx:2.0, got %s", pkg.Image.URL)
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
	injected := errors.New("injected error")

	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
	}

	m.LatestErr = injected
	_, _, err := m.LatestPackage("nginx")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- GetPackageQuestions tests ---

func TestMockRepositoryManagerGetPackageQuestions(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}, Questions: map[string]Question{
			"hostname": {Query: "Old hostname?"},
		}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}, Questions: map[string]Question{
			"hostname": {Query: "What hostname?", Type: Hostname},
			"port":     {Query: "What port?", Type: Port},
		}},
	}

	questions, err := m.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected latest version question, got %q", questions["hostname"].Query)
	}
	if questions["port"].Type != Port {
		t.Fatalf("expected port type, got %q", questions["port"].Type)
	}
}

func TestMockRepositoryManagerGetPackageQuestionsNotFound(t *testing.T) {
	m := InitMockRepositoryManager()

	_, err := m.GetPackageQuestions("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestMockRepositoryManagerGetPackageQuestionsErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := errors.New("injected error")

	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}, Questions: map[string]Question{
			"hostname": {Query: "What hostname?"},
		}},
	}

	m.QuestionsErr = injected
	_, err := m.GetPackageQuestions("nginx")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- FindRepoForPackage tests ---

func TestMockRepositoryManagerFindRepoForPackage(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}},
	}

	repoName, err := m.FindRepoForPackage("nginx", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repoName != "mock-repo" {
		t.Fatalf("expected mock-repo, got %s", repoName)
	}
}

func TestMockRepositoryManagerFindRepoForPackageNotFound(t *testing.T) {
	m := InitMockRepositoryManager()

	_, err := m.FindRepoForPackage("nonexistent", "1.0")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestMockRepositoryManagerFindRepoForPackageVersionNotFound(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
	}

	_, err := m.FindRepoForPackage("nginx", "99.0")
	if err == nil {
		t.Fatal("expected error for nonexistent version")
	}
	if !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("expected ErrPackageNotFound, got %v", err)
	}
}

func TestMockRepositoryManagerFindRepoForPackageErrorInjection(t *testing.T) {
	m := InitMockRepositoryManager()
	injected := errors.New("injected error")

	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
	}

	m.FindRepoErr = injected
	_, err := m.FindRepoForPackage("nginx", "1.0")
	if !errors.Is(err, injected) {
		t.Fatalf("expected injected error, got %v", err)
	}
}

// --- Call log tests ---

func TestMockRepositoryManagerCallLog(t *testing.T) {
	m := InitMockRepositoryManager()
	m.Packages["nginx"] = map[string]InputPackage{
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}},
	}

	err := m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.Get("core")
	_, err = m.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.Refresh()
	_, err = m.LoadAllPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = m.ListPackages()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, err = m.LatestPackage("nginx")
	if err != nil && !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = m.GetPackageQuestions("nginx")
	if err != nil && !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err = m.FindRepoForPackage("nginx", "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = m.Remove("core")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := m.GetCalls()
	if len(calls) != 10 {
		t.Fatalf("expected 10 calls, got %d", len(calls))
	}

	expected := []string{"Add", "Get", "List", "Refresh", "LoadAllPackages", "ListPackages", "LatestPackage", "GetPackageQuestions", "FindRepoForPackage", "Remove"}
	for i, want := range expected {
		if calls[i].Method != want {
			t.Fatalf("call %d: expected method %q, got %q", i, want, calls[i].Method)
		}
	}
}

func TestMockRepositoryManagerCallLogArgs(t *testing.T) {
	m := InitMockRepositoryManager()

	repo := testRepo(t, "core")
	err := m.Add(repo)
	if err != nil {
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

	err := m.Add(testRepo(t, "core"))
	if err != nil {
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
	err = m.Add(testRepo(t, "core"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	err = m.Add(testRepo(t, "extras"))
	if err != nil {
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
		"1.0": {Image: InputPackageImage{URL: "nginx:1.0"}, Questions: map[string]Question{
			"hostname": {Query: "What hostname?"},
		}},
		"2.0": {Image: InputPackageImage{URL: "nginx:2.0"}, Questions: map[string]Question{
			"hostname": {Query: "What hostname?", Type: Hostname},
			"port":     {Query: "What port?", Type: Port},
		}},
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
	if pkg.Image.URL != "nginx:2.0" {
		t.Fatalf("expected image nginx:2.0, got %s", pkg.Image.URL)
	}

	// Get package questions.
	questions, err := m.GetPackageQuestions("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(questions) != 2 {
		t.Fatalf("expected 2 questions (from version 2.0), got %d", len(questions))
	}
	if questions["hostname"].Type != Hostname {
		t.Fatalf("expected hostname type, got %q", questions["hostname"].Type)
	}

	// Remove one repository.
	err = m.Remove("core")
	if err != nil {
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
	m.Refresh()
}
