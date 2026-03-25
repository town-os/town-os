// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
)

func TestPackageManifestReturnsYAML(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := &packages.RepositoryRoot{
		BaseDir: t.TempDir(),
		Git:     &git.GoGitClient{Home: t.TempDir()},
	}
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	yamlContent := "image: nginx:1.0\ndescription: test package\n"
	writeTestPackage(t, rr.BaseDir, "repo-a", "nginx", "1.0", yamlContent)

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(PackageIdentityRequest{Repo: "repo-a", Name: "nginx", Version: "1.0"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/packages/manifest"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "text/x-yaml; charset=utf-8" {
		t.Fatalf("expected Content-Type %q, got %q", "text/x-yaml; charset=utf-8", ct)
	}

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}

	if string(got) != yamlContent {
		t.Fatalf("expected body %q, got %q", yamlContent, string(got))
	}
}

func TestPackageManifestNotFound(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := &packages.RepositoryRoot{
		BaseDir: t.TempDir(),
		Git:     &git.GoGitClient{Home: t.TempDir()},
	}
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(PackageIdentityRequest{Repo: "repo-a", Name: "nonexistent", Version: "9.9"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/packages/manifest"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPackageManifestMissingFields(t *testing.T) {
	mock := storage.InitBtrFSMock()
	rr := &packages.RepositoryRoot{
		BaseDir: t.TempDir(),
		Git:     &git.GoGitClient{Home: t.TempDir()},
	}

	ts := InitTestServer(ServerConfig{Storage: mock, RepositoryRoot: rr})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(PackageIdentityRequest{Repo: "repo-a", Name: "nginx"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/packages/manifest"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
