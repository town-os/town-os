package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestHTTPUploadArchiveReservedSubvolume(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("subvolume", "installed/repo/pkg/1.0/data")
	part, _ := writer.CreateFormFile("archive", "test.tar.gz")
	_, _ = part.Write([]byte("fake"))
	_ = writer.Close()

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/storage/upload-archive"), body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should fail with reserved filesystem error.
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error for reserved subvolume, got 200")
	}
}

func TestHTTPDownloadArchiveReservedSubvolume(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(DownloadArchiveRequest{Subvolume: "installed/repo/pkg/1.0/data"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "/storage/download-archive"), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected error for reserved subvolume, got 200")
	}
}
