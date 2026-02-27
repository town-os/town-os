package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// gzipMagic is a minimal gzip header that passes magic-byte detection.
var gzipMagic = []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00}

func TestHTTPUploadArchiveInstalledVolume(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("subvolume", "installed/repo/pkg/1.0/data"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	part, err := writer.CreateFormFile("archive", "test.tar.gz")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(gzipMagic); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

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

	// Should not fail with reserved filesystem error anymore.
	// It may fail for other reasons (tar unpack on fake data) but should not be 403/reserved.
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("installed volumes should be allowed for archive upload")
	}
}

func TestHTTPDownloadArchiveInstalledVolume(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: t.TempDir()})
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

	// Should not fail with reserved filesystem error anymore.
	// May fail for other reasons (directory not found) but not due to reserved check.
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("installed volumes should be allowed for archive download")
	}
}

func TestHTTPUploadArchiveWithSubpath(t *testing.T) {
	mock := storage.InitBtrFSMock()
	basePath := t.TempDir()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: basePath})
	t.Cleanup(ts.Close)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("subvolume", "test-vol")
	_ = writer.WriteField("subpath", "deep/nested")
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

	// The request should be accepted (subpath is valid); it may fail during
	// actual unpack because the tar data is fake, but the subpath parameter
	// should be accepted by the handler.
}

func TestHTTPUploadArchiveWithStopService(t *testing.T) {
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	basePath := t.TempDir()
	ts := InitTestServer(ServerConfig{Storage: mock, Systemd: sd, BtrfsBasePath: basePath})
	t.Cleanup(ts.Close)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("subvolume", "test-vol")
	_ = writer.WriteField("stop_service", "my-app.service")
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

	// Verify that the systemd mock recorded the stop call.
	calls := sd.GetCalls()
	found := false
	for _, c := range calls {
		if c.Method == "SetStatus" && len(c.Args) >= 2 {
			unit, _ := c.Args[0].(string)
			action, _ := c.Args[1].(systemd.StatusAction)
			if unit == "my-app.service" && action == systemd.Stop {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected SetStatus(stop) call on the mock systemd manager")
	}
}

func TestHTTPDownloadArchiveWithFormat(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: t.TempDir()})
	t.Cleanup(ts.Close)

	for _, format := range []string{"tar.gz", "tar.bz2", "tar.xz"} {
		t.Run(format, func(t *testing.T) {
			body, err := json.Marshal(DownloadArchiveRequest{
				Subvolume: "installed/repo/pkg/1.0/data",
				Format:    format,
			})
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

			// The endpoint should accept the format parameter.
			// It may fail because the directory doesn't exist, but it should
			// not reject the format itself.
			if resp.StatusCode == http.StatusBadRequest {
				respBody, _ := io.ReadAll(resp.Body)
				if strings.Contains(string(respBody), "unsupported download format") {
					t.Fatalf("format %q should be supported", format)
				}
			}
		})
	}
}

func TestHTTPDownloadArchiveInvalidFormat(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: t.TempDir()})
	t.Cleanup(ts.Close)

	body, err := json.Marshal(DownloadArchiveRequest{
		Subvolume: "test-vol",
		Format:    "tar.zst",
	})
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

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported format, got %d", resp.StatusCode)
	}
}
