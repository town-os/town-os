package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestWrongHTTPMethod(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodGet, testRoute(t, ts.Server.URL, "storage/create"), nil)
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

func TestNonexistentRoute(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "nonexistent"), bytes.NewBufferString("{}"))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
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
		t.Fatal("expected non-200 for nonexistent route")
	}
}

func TestEmptyBody(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "storage/create"), bytes.NewBufferString(""))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
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
		t.Fatal("expected non-200 for empty body")
	}
}
