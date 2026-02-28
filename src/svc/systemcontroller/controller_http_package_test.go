package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPListPackagesEmpty(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// create empty packages dir
	pkgDir := filepath.Join(rr.BaseDir, "repo-a", packages.PackagesDir)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("os.MkdirAll %q: %v", pkgDir, err)
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 packages, got %d", len(pkgs.Entries))
	}
}

func TestHTTPListPackagesPopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "redis", "7.0", "image: redis:7.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	// results are sorted by name
	if pkgs.Entries[0].Repo != "repo-a" || pkgs.Entries[0].Name != "nginx" || pkgs.Entries[0].Version != "2.0" {
		t.Fatalf("expected repo-a/nginx@2.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "repo-a" || pkgs.Entries[1].Name != "redis" || pkgs.Entries[1].Version != "7.0" {
		t.Fatalf("expected repo-a/redis@7.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
}

func TestHTTPListPackagesMultipleRepos(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u1, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-a.git", err)
	}
	u2, err := url.Parse("https://example.com/repo-b.git")
	if err != nil {
		t.Fatalf("url.Parse %q: %v", "https://example.com/repo-b.git", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u1},
		{Name: "repo-b", URL: *u2},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-b", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-b", "nginx", "3.0", "image: nginx:3.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(pkgs.Entries))
	}

	// nginx should be 3.0 (higher version from repo-b wins)
	if pkgs.Entries[0].Repo != "repo-b" || pkgs.Entries[0].Name != "nginx" || pkgs.Entries[0].Version != "3.0" {
		t.Fatalf("expected repo-b/nginx@3.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
	if pkgs.Entries[1].Repo != "repo-b" || pkgs.Entries[1].Name != "redis" || pkgs.Entries[1].Version != "7.0" {
		t.Fatalf("expected repo-b/redis@7.0, got %s/%s@%s", pkgs.Entries[1].Repo, pkgs.Entries[1].Name, pkgs.Entries[1].Version)
	}
}

func TestHTTPListPackagesWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 for POST on GET-only route")
	}
}

// --- GetPackageQuestions HTTP endpoint tests ---

func TestHTTPGetPackageQuestionsPopulated(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "What hostname?"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname?" {
		t.Fatalf("expected hostname query %q, got %q", "What hostname?", questions["hostname"].Query)
	}
	if questions["hostname"].Type != packages.Hostname {
		t.Fatalf("expected hostname type %q, got %q", packages.Hostname, questions["hostname"].Type)
	}
	if questions["port"].Query != "What port?" {
		t.Fatalf("expected port query %q, got %q", "What port?", questions["port"].Query)
	}
	if questions["port"].Type != packages.Port {
		t.Fatalf("expected port type %q, got %q", packages.Port, questions["port"].Type)
	}
}

func TestHTTPGetPackageQuestionsNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// create empty packages dir
	pkgDir := filepath.Join(rr.BaseDir, "repo-a", packages.PackagesDir)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("os.MkdirAll: %v", err)
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	_, err = c.GetPackageQuestions(context.TODO(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestHTTPGetPackageQuestionsBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "packages/questions"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestHTTPGetPackageQuestionsWrongMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, testRoute(t, ts.Server.URL, "packages/questions"), nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 for GET on POST-only route")
	}
}

func TestHTTPGetPackageQuestionsLatestVersion(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", `image: nginx:1.0
questions:
  hostname:
    query: "Old question"
`)
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", `image: nginx:2.0
questions:
  hostname:
    query: "New question"
    type: hostname
  port:
    query: "What port?"
    type: port
`)

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	questions, err := c.GetPackageQuestions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions from latest version, got %d", len(questions))
	}
	if questions["hostname"].Query != "New question" {
		t.Fatalf("expected latest version question, got %q", questions["hostname"].Query)
	}
}

// --- HTTP GetPackageQuestionsByIdentity tests ---

func TestHTTPGetPackageQuestionsByIdentity(t *testing.T) {
	c, _ := initInstallTestClient(t)

	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(questions))
	}
	if questions["hostname"].Query != "What hostname should nginx serve?" {
		t.Fatalf("expected hostname query, got %q", questions["hostname"].Query)
	}
	if questions["port"].Query != "What external port should nginx listen on?" {
		t.Fatalf("expected port query, got %q", questions["port"].Query)
	}
}

func TestHTTPGetPackageQuestionsByIdentityNoQuestions(t *testing.T) {
	c, _ := initInstallTestClient(t)

	questions, err := c.GetPackageQuestionsByIdentity(context.TODO(), "repo-a", "nginx", "2.0")
	if err != nil {
		t.Fatalf("GetPackageQuestionsByIdentity: %v", err)
	}

	if len(questions) != 0 {
		t.Fatalf("expected 0 questions for nginx@2.0, got %d", len(questions))
	}
}

func TestHTTPGetPackageQuestionsByIdentityNotFound(t *testing.T) {
	c, _ := initInstallTestClient(t)

	_, err := c.GetPackageQuestionsByIdentity(context.TODO(), "repo-a", "nonexistent", "1.0")
	if err == nil {
		t.Fatal("expected error for nonexistent package")
	}
}

func TestHTTPListPackageVersions(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	versions, err := c.ListPackageVersions(context.TODO(), "nginx")
	if err != nil {
		t.Fatalf("ListPackageVersions: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d: %v", len(versions), versions)
	}

	// Should be sorted highest first.
	if versions[0] != "2.0" {
		t.Fatalf("expected first version %q, got %q", "2.0", versions[0])
	}
	if versions[1] != "1.0" {
		t.Fatalf("expected second version %q, got %q", "1.0", versions[1])
	}
}

func TestHTTPListPackagesPagination(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo-a", "alpha", "1.0", "image: alpha:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "bravo", "1.0", "image: bravo:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo-a", "charlie", "1.0", "image: charlie:1.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// First page
	page, err := c.ListPackages(context.TODO(), ListParams{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListPackages page 0: %v", err)
	}
	if len(page.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(page.Entries))
	}
	if !page.HasMore {
		t.Fatal("expected has_more=true")
	}

	// Second page
	page, err = c.ListPackages(context.TODO(), ListParams{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListPackages page 1: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}
	if page.HasMore {
		t.Fatal("expected has_more=false")
	}
}

func TestHTTPListPackagesSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo", URL: *u},
	}

	writeTestPackage(t, rr.BaseDir, "repo", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "repo", "postgres", "16.0", "image: postgres:16.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for "nginx"
	page, err := c.ListPackages(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListPackages search: %v", err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(page.Entries))
	}

	// No match
	page, err = c.ListPackages(context.TODO(), ListParams{Search: "mysql"})
	if err != nil {
		t.Fatalf("ListPackages search no match: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(page.Entries))
	}
}

func TestHTTPListPackagesIncludesInstalledOlderVersions(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	// Only nginx 2.0 is in the repo.
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "2.0", "image: nginx:2.0\n")

	// But nginx 1.0 was previously installed (still registered).
	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{{Repo: "repo-a", Name: "nginx", Version: "1.0"}}

	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	pkgs, err := c.ListPackages(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackages: %v", err)
	}

	// The repo has repo-a/nginx@2.0. The installed list has repo-a/nginx@1.0.
	// ListPackages merges both: since both share the name "nginx", the repo
	// entry (repo-a/nginx@2.0) is already listed, and the installed entry (repo-a/nginx@1.0)
	// has the same name so it should NOT be duplicated (the merge logic dedupes
	// by package name). Therefore we expect exactly 1 entry: repo-a/nginx@2.0.
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 package entry (repo provides latest), got %d: %v", len(pkgs.Entries), pkgs.Entries)
	}

	if pkgs.Entries[0].Repo != "repo-a" || pkgs.Entries[0].Name != "nginx" || pkgs.Entries[0].Version != "2.0" {
		t.Fatalf("expected repo-a/nginx@2.0, got %s/%s@%s", pkgs.Entries[0].Repo, pkgs.Entries[0].Name, pkgs.Entries[0].Version)
	}
}

func writeFeatured(t *testing.T, baseDir, repoName string, featured []string) {
	t.Helper()
	dir := filepath.Join(baseDir, repoName)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("os.MkdirAll %q: %v", dir, err)
	}
	data, err := json.Marshal(featured)
	if err != nil {
		t.Fatalf("json.Marshal featured: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.FeaturedFile), data, 0600); err != nil {
		t.Fatalf("os.WriteFile featured.json: %v", err)
	}
}

func TestHTTPListPackagesByRepo(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
		{Name: "extras", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\n")
	writeTestPackage(t, rr.BaseDir, "extras", "mosquitto", "2.0", "image: eclipse-mosquitto:2.0\n")

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListPackagesByRepo(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackagesByRepo: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 repo groups, got %d", len(groups))
	}

	// Highest-precedence repo comes first (reversed internal order).
	if groups[0].Repo != "extras" {
		t.Fatalf("expected first group to be extras, got %s", groups[0].Repo)
	}
	if len(groups[0].Packages) != 1 {
		t.Fatalf("expected 1 package in extras, got %d", len(groups[0].Packages))
	}
	if groups[0].Packages[0].Name != "mosquitto" {
		t.Fatalf("expected mosquitto in extras, got %s", groups[0].Packages[0].Name)
	}

	if groups[1].Repo != "core" {
		t.Fatalf("expected second group to be core, got %s", groups[1].Repo)
	}
	if len(groups[1].Packages) != 2 {
		t.Fatalf("expected 2 packages in core, got %d", len(groups[1].Packages))
	}
}

func TestHTTPListPackagesByRepoFeatured(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListPackagesByRepo(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListPackagesByRepo: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 repo group, got %d", len(groups))
	}
	if len(groups[0].Featured) != 1 {
		t.Fatalf("expected 1 featured package, got %d", len(groups[0].Featured))
	}
	if groups[0].Featured[0] != "nginx" {
		t.Fatalf("expected featured[0] = %q, got %q", "nginx", groups[0].Featured[0])
	}
}

func TestHTTPListPackagesByRepoFeaturedPreservedOnSearch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx", "redis"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for "nginx" — should filter packages but preserve Featured list.
	groups, err := c.ListPackagesByRepo(context.TODO(), ListParams{Search: "nginx"})
	if err != nil {
		t.Fatalf("ListPackagesByRepo with search: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 repo group, got %d", len(groups))
	}
	if len(groups[0].Packages) != 1 {
		t.Fatalf("expected 1 matching package, got %d", len(groups[0].Packages))
	}
	if groups[0].Packages[0].Name != "nginx" {
		t.Fatalf("expected matching package %q, got %q", "nginx", groups[0].Packages[0].Name)
	}
	// Featured field must be preserved even when search filters are applied.
	if len(groups[0].Featured) != 2 {
		t.Fatalf("expected 2 featured entries preserved through search, got %d", len(groups[0].Featured))
	}
}

func TestHTTPTimezonesEndpointRemoved(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, testRoute(t, ts.Server.URL, "packages/timezones"), nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for removed timezone endpoint")
	}
}

func TestHTTPListChildren(t *testing.T) {
	c, inst := initInstallTestClient(t)

	inst.Children["repo-a/nginx"] = []string{"repo-a/child-a", "repo-a/child-b"}

	children, err := c.ListChildren(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}

	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	if children[0] != "repo-a/child-a" || children[1] != "repo-a/child-b" {
		t.Fatalf("unexpected children: %v", children)
	}
}

func TestHTTPListChildrenEmpty(t *testing.T) {
	c, _ := initInstallTestClient(t)

	children, err := c.ListChildren(context.TODO(), "repo-a", "nginx")
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}

	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestHTTPListUninstalledVolumes(t *testing.T) {
	c, controller := initTestClient(t)

	injectSubvol(t, controller, "uninstalled/mock-repo/nginx/1.0/html", 0)
	injectSubvol(t, controller, "uninstalled/mock-repo/nginx/1.0/logs", 0)

	result, err := c.ListUninstalledVolumes(context.TODO(), "mock-repo", "nginx")
	if err != nil {
		t.Fatalf("ListUninstalledVolumes: %v", err)
	}

	if !result.HasUninstalledVolumes {
		t.Fatal("expected HasUninstalledVolumes=true")
	}

	if len(result.UninstalledVersions) != 1 {
		t.Fatalf("expected 1 uninstalled version, got %d", len(result.UninstalledVersions))
	}
	if result.UninstalledVersions[0] != "1.0" {
		t.Fatalf("expected uninstalled version %q, got %q", "1.0", result.UninstalledVersions[0])
	}
}

func TestHTTPListUninstalledVolumesEmpty(t *testing.T) {
	c, _ := initTestClient(t)

	result, err := c.ListUninstalledVolumes(context.TODO(), "mock-repo", "nginx")
	if err != nil {
		t.Fatalf("ListUninstalledVolumes: %v", err)
	}

	if result.HasUninstalledVolumes {
		t.Fatal("expected HasUninstalledVolumes=false")
	}
}

func TestHTTPListPackagesByRepoSearchNoMatch(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Search for something that doesn't match — group should be excluded entirely.
	groups, err := c.ListPackagesByRepo(context.TODO(), ListParams{Search: "nonexistent"})
	if err != nil {
		t.Fatalf("ListPackagesByRepo search no match: %v", err)
	}

	if len(groups) != 0 {
		t.Fatalf("expected 0 repo groups for non-matching search, got %d", len(groups))
	}
}
