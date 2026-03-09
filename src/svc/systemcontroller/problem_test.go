// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

func TestErrorToProblemDetail_BareError(t *testing.T) {
	pd := errorToProblemDetail(errors.New("disk full"))
	if pd.Status != 500 {
		t.Fatalf("expected status 500, got %d", pd.Status)
	}
	if pd.Detail != "disk full" {
		t.Fatalf("expected detail %q, got %q", "disk full", pd.Detail)
	}
	if pd.Title != "Internal Server Error" {
		t.Fatalf("expected title %q, got %q", "Internal Server Error", pd.Title)
	}
}

func TestErrorToProblemDetail_HTTPError(t *testing.T) {
	he := echo.NewHTTPError(403, "admin access required")
	pd := errorToProblemDetail(he)
	if pd.Status != 403 {
		t.Fatalf("expected status 403, got %d", pd.Status)
	}
	if pd.Detail != "admin access required" {
		t.Fatalf("expected detail %q, got %q", "admin access required", pd.Detail)
	}
}

func TestErrorToProblemDetail_AlreadyProblemDetail(t *testing.T) {
	orig := NewProblemDetail(422, "validation failed")
	pd := errorToProblemDetail(orig)
	if pd != orig {
		t.Fatal("expected same ProblemDetail to pass through")
	}
}

func TestProblemDetailJSON(t *testing.T) {
	pd := NewProblemDetail(404, "not found")
	data, err := json.Marshal(pd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	err = json.Unmarshal(data, &m)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m["type"] != "about:blank#404" {
		t.Fatalf("expected type %q, got %q", "about:blank#404", m["type"])
	}
	if m["title"] != "Not Found" {
		t.Fatalf("expected title %q, got %q", "Not Found", m["title"])
	}
	statusVal, ok := m["status"].(float64)
	if !ok {
		t.Fatalf("expected status to be float64, got %T", m["status"])
	}
	if int(statusVal) != 404 {
		t.Fatalf("expected status 404, got %v", m["status"])
	}
	if m["detail"] != "not found" {
		t.Fatalf("expected detail %q, got %q", "not found", m["detail"])
	}
}

func TestProblemDetailResponse_ContentType(t *testing.T) {
	c, _ := initTestClient(t)

	// POST with invalid JSON to trigger a decode error
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.BaseURL+"/storage/create",
		strings.NewReader("{invalid"),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}()

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("expected Content-Type application/problem+json, got %q", ct)
	}
}

func TestProblemDetailResponse_ErrorBody(t *testing.T) {
	c, _ := initTestClient(t)

	// POST with invalid JSON to trigger an error
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		c.BaseURL+"/storage/create",
		strings.NewReader("{invalid"),
	)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}()

	if resp.StatusCode < 400 {
		t.Fatalf("expected error status, got %d", resp.StatusCode)
	}

	var pd ProblemDetailError
	err = json.NewDecoder(resp.Body).Decode(&pd)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if pd.Status == 0 {
		t.Fatal("expected non-zero status")
	}
	if pd.Detail == "" {
		t.Fatal("expected non-empty detail")
	}
	if pd.Type == "" {
		t.Fatal("expected non-empty type")
	}
	if pd.Title == "" {
		t.Fatal("expected non-empty title")
	}
}

func TestProblemDetailResponse_InvalidName(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = c.ModifyFilesystem(context.TODO(), "test-vol", storage.Filesystem{Name: "/invalid"})
	if err == nil {
		t.Fatal("expected error for invalid filesystem name")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}

	if pe.Problem.Detail == "" {
		t.Fatal("expected non-empty detail in ProblemError")
	}
}

func TestSanitizeProblemDetail_RedactsPassword(t *testing.T) {
	pd := NewProblemDetail(500, `failed: password=s3cret123 in request`)
	pd = sanitizeProblemDetail(pd)
	if strings.Contains(pd.Detail, "s3cret123") {
		t.Fatalf("expected password to be redacted, got %q", pd.Detail)
	}
	if !strings.Contains(pd.Detail, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] in detail, got %q", pd.Detail)
	}
}

func TestSanitizeProblemDetail_RedactsCredential(t *testing.T) {
	pd := NewProblemDetail(500, `credential: abc123 was rejected`)
	pd = sanitizeProblemDetail(pd)
	if strings.Contains(pd.Detail, "abc123") {
		t.Fatalf("expected credential to be redacted, got %q", pd.Detail)
	}
}

func TestSanitizeProblemDetail_RedactsToken(t *testing.T) {
	pd := NewProblemDetail(500, `token=eyJhbGciOi was invalid`)
	pd = sanitizeProblemDetail(pd)
	if strings.Contains(pd.Detail, "eyJhbGciOi") {
		t.Fatalf("expected token to be redacted, got %q", pd.Detail)
	}
}

func TestSanitizeProblemDetail_PreservesNonSensitive(t *testing.T) {
	detail := "btrfs qgroup limit: quota not enabled"
	pd := NewProblemDetail(500, detail)
	pd = sanitizeProblemDetail(pd)
	if pd.Detail != detail {
		t.Fatalf("expected detail to be unchanged, got %q", pd.Detail)
	}
}

func TestProblemDetailImplementsInterfaces(t *testing.T) {
	pd := NewProblemDetail(500, "test")

	// Implements error
	var _ error = pd

	// Implements HTTPStatusCoder
	var _ echo.HTTPStatusCoder = pd
}

// --- Client-side tests ---

func TestReadProblemDetail_ValidJSON(t *testing.T) {
	pd := NewProblemDetail(http.StatusInternalServerError, "disk full")
	body, err := json.Marshal(pd)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}

	err = readProblemDetail(resp, http.MethodPost, "/storage/create")
	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}
	if pe.Problem.Detail != "disk full" {
		t.Fatalf("expected detail %q, got %q", "disk full", pe.Problem.Detail)
	}
	if pe.Method != http.MethodPost {
		t.Fatalf("expected method POST, got %q", pe.Method)
	}
	if pe.Path != "/storage/create" {
		t.Fatalf("expected path /storage/create, got %q", pe.Path)
	}
}

func TestReadProblemDetail_NonJSON(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("Bad Gateway")),
	}

	err := readProblemDetail(resp, "GET", "/status/ping")
	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}
	if pe.Problem.Status != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d", http.StatusBadGateway, pe.Problem.Status)
	}
	if pe.Problem.Detail != "Bad Gateway" {
		t.Fatalf("expected detail %q, got %q", "Bad Gateway", pe.Problem.Detail)
	}
}

func TestReadProblemDetail_LegacyEcho(t *testing.T) {
	body := `{"message": "missing authorization token"}`
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	err := readProblemDetail(resp, "GET", "/account")
	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}
	if pe.Problem.Detail != "missing authorization token" {
		t.Fatalf("expected detail %q, got %q", "missing authorization token", pe.Problem.Detail)
	}
}

func TestReadProblemDetail_EmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	err := readProblemDetail(resp, "POST", "/test")
	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}
	if pe.Problem.Detail != "status 500" {
		t.Fatalf("expected fallback detail, got %q", pe.Problem.Detail)
	}
}

func TestProblemError_Is(t *testing.T) {
	pe := &ProblemError{
		Method:  "GET",
		Path:    "/test",
		Problem: *NewProblemDetail(500, "test error"),
	}

	if !errors.Is(pe, ErrUnsuccessfulStatus) {
		t.Fatal("expected errors.Is(pe, ErrUnsuccessfulStatus) to be true")
	}
}

func TestProblemError_ErrorMessage(t *testing.T) {
	pe := &ProblemError{
		Method:  "POST",
		Path:    "/storage/create",
		Problem: *NewProblemDetail(500, "btrfs qgroup limit: quota not enabled"),
	}

	expected := "POST /storage/create: btrfs qgroup limit: quota not enabled"
	if pe.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, pe.Error())
	}
}

func TestClientParsesProblemDetail(t *testing.T) {
	c, _ := initTestClient(t)

	// ModifyFilesystem with an invalid name triggers a 500/400 with a real error detail
	err := c.ModifyFilesystem(context.TODO(), "nonexistent", storage.Filesystem{Name: "/invalid"})
	if err == nil {
		t.Fatal("expected error")
	}

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("expected ProblemError, got %T: %v", err, err)
	}

	if pe.Problem.Detail == "" {
		t.Fatal("expected non-empty detail")
	}

	// Should also satisfy errors.Is for backward compat
	if !errors.Is(err, ErrUnsuccessfulStatus) {
		t.Fatal("expected errors.Is(err, ErrUnsuccessfulStatus) to be true")
	}
}

func TestNewProblemDetail(t *testing.T) {
	pd := NewProblemDetail(http.StatusBadRequest, "bad input")
	if pd.Type != "about:blank#400" {
		t.Fatalf("expected type about:blank#400, got %q", pd.Type)
	}
	if pd.Title != "Bad Request" {
		t.Fatalf("expected title %q, got %q", "Bad Request", pd.Title)
	}
	if pd.Status != 400 {
		t.Fatalf("expected status 400, got %d", pd.Status)
	}
	if pd.Detail != "bad input" {
		t.Fatalf("expected detail %q, got %q", "bad input", pd.Detail)
	}
}
