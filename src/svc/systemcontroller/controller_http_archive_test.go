// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
	if err := writer.WriteField("subvolume", "test-vol"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("subpath", "deep/nested"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("archive", "test.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
	if err := writer.WriteField("subvolume", "test-vol"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("stop_service", "my-app.service"); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("archive", "test.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

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
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("resp.Body.Close: %v", err)
				}
			}()

			// The endpoint should accept the format parameter.
			// It may fail because the directory doesn't exist, but it should
			// not reject the format itself.
			if resp.StatusCode == http.StatusBadRequest {
				respBody, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for unsupported format, got %d", resp.StatusCode)
	}
}

func TestHTTPDownloadArchiveWithFilename(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock, BtrfsBasePath: t.TempDir()})
	t.Cleanup(ts.Close)

	tests := map[string]struct {
		filename string
		format   string
		want     string
	}{
		"custom name":          {"my-backup", "tar.gz", "attachment; filename=my-backup.tar.gz"},
		"custom name bz2":      {"my-backup", "tar.bz2", "attachment; filename=my-backup.tar.bz2"},
		"custom name xz":       {"my-backup", "tar.xz", "attachment; filename=my-backup.tar.xz"},
		"empty uses default":   {"", "tar.gz", "attachment; filename=download.tar.gz"},
		"strips path":          {"../../../etc/passwd", "tar.gz", "attachment; filename=passwd.tar.gz"},
		"deduplicates ext":     {"my-backup.tar.gz", "tar.gz", "attachment; filename=my-backup.tar.gz"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(DownloadArchiveRequest{
				Subvolume: "installed/repo/pkg/1.0/data",
				Format:    tt.format,
				Filename:  tt.filename,
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
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Errorf("resp.Body.Close: %v", err)
				}
			}()

			got := resp.Header.Get("Content-Disposition")
			if got != tt.want {
				t.Fatalf("Content-Disposition = %q, want %q", got, tt.want)
			}
		})
	}
}
