package systemcontroller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestHTTPMultiRepoInstallSameName(t *testing.T) {
	c, inst := initMultiRepoTestClient(t)

	// Install nginx from repo-a (explicit repo).
	pr1, pw1 := io.Pipe()
	go func() {
		pw1.CloseWithError(json.NewEncoder(pw1).Encode(InstallRequest{
			Repo: "repo-a", Name: "nginx", Version: "1.0", Responses: packages.Responses{},
		}))
	}()
	req1, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.BaseURL+"/packages/install", pr1)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req1.Header.Set("Content-Type", "application/json")

	resp1, err := c.HTTP.Do(req1)
	if err != nil {
		t.Fatalf("HTTP POST install repo-a: %v", err)
	}
	defer func() {
		if err := resp1.Body.Close(); err != nil {
			t.Errorf("resp1.Body.Close: %v", err)
		}
	}()
	if resp1.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("expected 200 for repo-a, got %d: %s", resp1.StatusCode, body)
	}

	// Install nginx from repo-b (explicit repo).
	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(json.NewEncoder(pw).Encode(InstallRequest{
			Repo: "repo-b", Name: "nginx", Version: "1.0", Responses: packages.Responses{},
		}))
	}()
	req2, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, c.BaseURL+"/packages/install", pr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req2)
	if err != nil {
		t.Fatalf("HTTP POST install repo-b: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 for repo-b, got %d: %s", resp.StatusCode, body)
	}

	// MockInstallManager should have 2 installs with distinct repos.
	installed := inst.Installed
	if len(installed) != 2 {
		t.Fatalf("expected 2 installs, got %d", len(installed))
	}

	repos := map[string]bool{}
	for _, p := range installed {
		repos[p.Repo] = true
	}
	if !repos["repo-a"] || !repos["repo-b"] {
		t.Fatalf("expected installs from both repo-a and repo-b, got %v", installed)
	}
}

func TestHTTPMultiRepoUninstallIsolation(t *testing.T) {
	c, inst := initMultiRepoTestClient(t)

	// Pre-seed both repos installed.
	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install repo-a: %v", err)
	}
	if err := inst.Install("repo-b", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install repo-b: %v", err)
	}

	// Uninstall repo-a.
	if err := c.UninstallPackage(context.TODO(), "repo-a", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage repo-a: %v", err)
	}

	// ListInstalled should return only repo-b.
	page, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(page.Entries))
	}
	if page.Entries[0] != "repo-b/nginx@1.0" {
		t.Fatalf("expected repo-b/nginx@1.0, got %s", page.Entries[0])
	}
}

func TestHTTPMultiRepoGetResponsesIsolation(t *testing.T) {
	c, inst := initMultiRepoTestClient(t)

	// Pre-seed with different responses per repo.
	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{"port": "80"}); err != nil {
		t.Fatalf("pre-install repo-a: %v", err)
	}
	if err := inst.Install("repo-b", "nginx", "1.0", packages.Responses{"port": "9090"}); err != nil {
		t.Fatalf("pre-install repo-b: %v", err)
	}

	respA, err := c.GetResponses(context.TODO(), "repo-a", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses repo-a: %v", err)
	}
	if respA["port"] != "80" {
		t.Fatalf("expected repo-a port=80, got %s", respA["port"])
	}

	respB, err := c.GetResponses(context.TODO(), "repo-b", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses repo-b: %v", err)
	}
	if respB["port"] != "9090" {
		t.Fatalf("expected repo-b port=9090, got %s", respB["port"])
	}
}

func TestHTTPMultiRepoDisableIsolation(t *testing.T) {
	c, inst := initMultiRepoTestClient(t)

	// Pre-seed both repos installed.
	if err := inst.Install("repo-a", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install repo-a: %v", err)
	}
	if err := inst.Install("repo-b", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install repo-b: %v", err)
	}

	// Disable only repo-a/nginx.
	if err := c.DisablePackage(context.TODO(), "repo-a", "nginx"); err != nil {
		t.Fatalf("DisablePackage repo-a: %v", err)
	}

	// repo-a should be disabled, repo-b should not.
	disabledA, err := inst.IsDisabled("repo-a", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled repo-a: %v", err)
	}
	if !disabledA {
		t.Fatal("expected repo-a/nginx to be disabled")
	}

	disabledB, err := inst.IsDisabled("repo-b", "nginx")
	if err != nil {
		t.Fatalf("IsDisabled repo-b: %v", err)
	}
	if disabledB {
		t.Fatal("expected repo-b/nginx to NOT be disabled")
	}
}
