package systemcontroller

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	townostls "gitea.com/town-os/town-os/src/tls"
	"github.com/labstack/echo/v5"
)

// caBackend is a minimal systemControllerBackend that only wires a TLS CA.
// Reusing archiveTestBackend would drag in storage/settings dependencies
// the handler does not need; this stub mirrors it just enough to compile.
type caBackend struct {
	archiveTestBackend

	ca *townostls.CA
}

func (b *caBackend) GetTLSCA() *townostls.CA { return b.ca }

func TestGetTLSCAServesPEM(t *testing.T) {
	dir := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(dir, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	s := &SystemControllerHandlers{Controller: &caBackend{ca: ca}}

	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/tls/ca.crt", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := s.getTLSCA(c); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-pem-file" {
		t.Errorf("content-type = %q", ct)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(ca.CertPEM) {
		t.Errorf("body does not match CA PEM")
	}
}

func TestGetTLSCAReturns404WhenDisabled(t *testing.T) {
	s := &SystemControllerHandlers{Controller: &caBackend{}}

	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/tls/ca.crt", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := s.getTLSCA(c)
	if err == nil {
		t.Fatal("expected error when CA is nil")
	}
	var httpErr *echo.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("want *echo.HTTPError, got %T", err)
	}
	if httpErr.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", httpErr.Code)
	}
}
