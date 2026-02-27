package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPRebuildGitMissingFields(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RebuildGitRequest{Repo: "core"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/packages/rebuild-git"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHTTPRebuildGitNoInstaller(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(RebuildGitRequest{Repo: "core", Name: "nginx", Version: "1.0"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/packages/rebuild-git"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}
