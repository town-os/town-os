package systemcontroller

import (
	"context"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPListFeaturedPackagesEmpty(t *testing.T) {
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
	// No featured.json

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 0 {
		t.Fatalf("expected 0 featured groups, got %d", len(groups))
	}
}

func TestHTTPListFeaturedPackagesWithFeatured(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\ndescription: Web server\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\ndescription: In-memory cache\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 featured group, got %d", len(groups))
	}
	if groups[0].Repo != "core" {
		t.Fatalf("expected repo %q, got %q", "core", groups[0].Repo)
	}
	if len(groups[0].Packages) != 1 {
		t.Fatalf("expected 1 featured package, got %d", len(groups[0].Packages))
	}
	if groups[0].Packages[0].Name != "nginx" {
		t.Fatalf("expected featured package %q, got %q", "nginx", groups[0].Packages[0].Name)
	}
	if groups[0].Packages[0].Description != "Web server" {
		t.Fatalf("expected description %q, got %q", "Web server", groups[0].Packages[0].Description)
	}
	if groups[0].Packages[0].Installed {
		t.Fatal("expected installed=false")
	}
}

func TestHTTPListFeaturedPackagesIncludesDescription(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\ndescription: A high-performance web server\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\ndescription: Fast key-value store\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx", "redis"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Packages) != 2 {
		t.Fatalf("expected 2 featured packages, got %d", len(groups[0].Packages))
	}

	// Verify descriptions are present.
	for _, pkg := range groups[0].Packages {
		if pkg.Description == "" {
			t.Fatalf("expected description for %s, got empty", pkg.Name)
		}
	}
}

func TestHTTPListFeaturedPackagesInstalledStatus(t *testing.T) {
	mockCtrl := storage.InitBtrFSMockController()
	mockStorage := storage.InitBtrFSFromController("", mockCtrl)
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\ndescription: Web server\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\ndescription: In-memory cache\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx", "redis"})

	inst := packages.InitMockInstallManager()
	inst.Installed = []packages.PackageIdentity{{Repo: "core", Name: "nginx", Version: "1.0"}}

	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Packages) != 2 {
		t.Fatalf("expected 2 featured packages, got %d", len(groups[0].Packages))
	}

	// nginx should be installed; redis should not.
	var foundNginx, foundRedis bool
	for _, pkg := range groups[0].Packages {
		if pkg.Name == "nginx" {
			foundNginx = true
			if !pkg.Installed {
				t.Fatal("expected nginx to be installed")
			}
			if pkg.InstalledVersion != "1.0" {
				t.Fatalf("expected installed_version %q, got %q", "1.0", pkg.InstalledVersion)
			}
		}
		if pkg.Name == "redis" {
			foundRedis = true
			if pkg.Installed {
				t.Fatal("expected redis to not be installed")
			}
		}
	}
	if !foundNginx {
		t.Fatal("nginx not found in results")
	}
	if !foundRedis {
		t.Fatal("redis not found in results")
	}
}

func TestHTTPListFeaturedPackagesMultipleRepos(t *testing.T) {
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
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\ndescription: Web server\n")
	writeTestPackage(t, rr.BaseDir, "extras", "mosquitto", "2.0", "image: eclipse-mosquitto:2.0\ndescription: MQTT broker\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})
	writeFeatured(t, rr.BaseDir, "extras", []string{"mosquitto"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 featured groups, got %d", len(groups))
	}
}

func TestHTTPListFeaturedPackagesExcludesNonFeatured(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "core", URL: *u},
	}
	writeTestPackage(t, rr.BaseDir, "core", "nginx", "1.0", "image: nginx:1.0\ndescription: Web server\n")
	writeTestPackage(t, rr.BaseDir, "core", "redis", "7.0", "image: redis:7.0\ndescription: Cache\n")
	writeTestPackage(t, rr.BaseDir, "core", "postgres", "16.0", "image: postgres:16.0\ndescription: Database\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Packages) != 1 {
		t.Fatalf("expected 1 featured package, got %d", len(groups[0].Packages))
	}
	if groups[0].Packages[0].Name != "nginx" {
		t.Fatalf("expected only nginx as featured, got %q", groups[0].Packages[0].Name)
	}
}

func TestHTTPListFeaturedPackagesRepoWithNoFeaturedExcluded(t *testing.T) {
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
	writeTestPackage(t, rr.BaseDir, "extras", "mosquitto", "2.0", "image: eclipse-mosquitto:2.0\n")
	writeFeatured(t, rr.BaseDir, "core", []string{"nginx"})
	// extras has no featured.json

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	groups, err := c.ListFeaturedPackages(context.TODO())
	if err != nil {
		t.Fatalf("ListFeaturedPackages: %v", err)
	}

	// Only core has featured packages.
	if len(groups) != 1 {
		t.Fatalf("expected 1 featured group (extras has no featured), got %d", len(groups))
	}
	if groups[0].Repo != "core" {
		t.Fatalf("expected group for core, got %s", groups[0].Repo)
	}
}
