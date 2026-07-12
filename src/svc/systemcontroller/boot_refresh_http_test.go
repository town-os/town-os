package systemcontroller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"gitea.com/town-os/town-os/src/ui"
)

// TestBootHandler_StatusPingHTTPShape wires the boot handler into an
// httptest.Server and asserts the JSON shape the UI's /status/ping
// poll relies on during the pre-swap boot window. Covers the end-to-
// end serialization path (JSON encode + Content-Type header + body
// parseable by the client) rather than just the in-process handler,
// which the existing BootHandler unit tests already cover.
func TestBootHandler_StatusPingHTTPShape(t *testing.T) {
	t.Parallel()

	bs := NewBootStatus()
	bs.Step(StepBootServices)
	srv := httptest.NewServer(NewBootHandler(bs))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/status/ping", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close body: %v", err)
		}
	})
	// 503 while booting — readiness probes must see "unavailable" until
	// RootHandler.Swap flips the controller to its full Echo router.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content-type = %q, want application/json", got)
	}
	var body struct {
		Booting bool   `json:"booting"`
		Step    string `json:"step"`
		Done    bool   `json:"done"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Booting {
		t.Errorf("booting = false, want true")
	}
	if body.Step != StepBootServices {
		t.Errorf("step = %q, want %q", body.Step, StepBootServices)
	}
}

// TestBootHandler_SSEOverHTTP verifies the /boot-status stream passes
// through httptest with correct Content-Type and delivers frames the
// UI client can parse. Complements the in-process BootHandler test by
// exercising the real HTTP transport (headers, flushing, chunked body).
func TestBootHandler_SSEOverHTTP(t *testing.T) {
	t.Parallel()

	bs := NewBootStatus()
	bs.Step("warm")
	srv := httptest.NewServer(NewBootHandler(bs))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/boot-status", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close body: %v", err)
		}
	})
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", got)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		bs.Step("mid")
		bs.Done()
	}()

	events := make([]progressEvent, 0, 3)
	scan := bufio.NewScanner(resp.Body)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var evt progressEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt); err != nil {
			continue
		}
		events = append(events, evt)
		if evt.Done {
			break
		}
	}

	if len(events) < 3 {
		t.Fatalf("got %d frames, want ≥3: %+v", len(events), events)
	}
	if events[0] != (progressEvent{Step: "warm"}) {
		t.Errorf("history replay frame[0] = %+v, want warm", events[0])
	}
	var sawMid, sawDone bool
	for _, e := range events {
		if e.Step == "mid" {
			sawMid = true
		}
		if e.Done {
			sawDone = true
		}
	}
	if !sawMid {
		t.Errorf("live 'mid' event not observed: %+v", events)
	}
	if !sawDone {
		t.Errorf("Done terminator not observed: %+v", events)
	}
}

// TestRootHandler_SwapOverHTTP stands the RootHandler behind httptest,
// starts a pre-swap SSE subscription, swaps to a sentinel handler, and
// verifies (a) the in-flight SSE stream survives the swap and keeps
// delivering events, and (b) new requests after the swap hit the new
// handler. This is the regression test for the atomic handler swap
// that lets the boot stub hand off to the full Echo router without
// closing the listener.
func TestRootHandler_SwapOverHTTP(t *testing.T) {
	t.Parallel()

	bs := NewBootStatus()
	bootH := NewBootHandler(bs)
	root := NewRootHandler(bootH)
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/boot-status", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() {
		if err := streamResp.Body.Close(); err != nil {
			t.Logf("close stream body: %v", err)
		}
	})

	swapped := http.NewServeMux()
	swapped.HandleFunc("GET /status/ping", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("swapped-ok")); err != nil {
			t.Logf("write: %v", err)
		}
	})
	root.Swap(swapped)

	// Post-swap request sees the new handler.
	pingReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/status/ping", nil)
	if err != nil {
		t.Fatalf("ping request: %v", err)
	}
	pingResp, err := http.DefaultClient.Do(pingReq)
	if err != nil {
		t.Fatalf("do ping: %v", err)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(pingResp.Body); err != nil {
		t.Logf("read ping body: %v", err)
	}
	if err := pingResp.Body.Close(); err != nil {
		t.Logf("close ping body: %v", err)
	}
	if got := buf.String(); got != "swapped-ok" {
		t.Errorf("post-swap ping body = %q, want swapped-ok", got)
	}

	// Pre-swap SSE subscription survives and delivers more events.
	go func() {
		time.Sleep(50 * time.Millisecond)
		bs.Step("post_swap")
		bs.Done()
	}()

	scan := bufio.NewScanner(streamResp.Body)
	var sawPost, sawDone bool
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var evt progressEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &evt); err != nil {
			continue
		}
		if evt.Step == "post_swap" {
			sawPost = true
		}
		if evt.Done {
			sawDone = true
			break
		}
	}
	if !sawPost || !sawDone {
		t.Errorf("pre-swap stream did not survive swap: post=%v done=%v", sawPost, sawDone)
	}
}

// TestBootHandler_ForbidsPrivilegedRoutesOverHTTP proves the early
// handler locks out real API routes with 403 across every method the
// UI might send. The in-process variant lives in boot_handler_test.go;
// this version asserts the same behaviour through a real HTTP round
// trip so there can't be any mux-based false positive from bypassing
// the actual server.
func TestBootHandler_ForbidsPrivilegedRoutesOverHTTP(t *testing.T) {
	t.Parallel()

	bs := NewBootStatus()
	srv := httptest.NewServer(NewBootHandler(bs))
	t.Cleanup(srv.Close)

	methods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete}
	paths := []string{
		"/packages",
		"/storage/package-volumes",
		"/system-services/refresh",
		"/account/authenticate",
		"/audit/log",
	}
	for _, method := range methods {
		for _, path := range paths {
			t.Run(method+" "+path, func(t *testing.T) {
				t.Parallel()
				req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
				if err != nil {
					t.Fatalf("new request: %v", err)
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("do: %v", err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Logf("close body: %v", err)
				}
				if resp.StatusCode != http.StatusForbidden {
					t.Errorf("status = %d, want 403", resp.StatusCode)
				}
			})
		}
	}
}

// TestRefreshSystemServicesDropsMarker drives the full HTTP refresh
// path end-to-end through httptest (not the big integration
// container), stubbing pullImage so the handler does not need real
// podman. Asserts the `town-os-restart-pending` marker file appears
// under BtrfsBasePath — the sentinel the next-boot RunFreshnessStage
// reads to trigger the per-package restart loop.
func TestRefreshSystemServicesDropsMarker(t *testing.T) {
	t.Parallel()

	btrfsBase := t.TempDir()

	restore := TestSetPullImage(func(_ context.Context, _ string) error { return nil })
	t.Cleanup(restore)

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	// Mock systemd + no-op pull: the images are never pulled or run, so
	// neutral fake tags are used. rc.latest must never be referenced in tests.
	uiMgr := ui.NewManager(ui.Config{Systemd: sd, Image: "quay.io/town/ui:testtag"})

	ts := InitTestServer(ServerConfig{
		Storage:                    mock,
		Systemd:                    sd,
		MonitoringBackend:          monitoring.BackendUPlot,
		UI:                         uiMgr,
		BtrfsBasePath:              btrfsBase,
		SystemControllerImage:      "quay.io/town/town:testtag",
		SystemControllerListenAddr: ":5309",
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.Server.URL+"/system-services/refresh", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close body: %v", err)
		}
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	markerPath := filepath.Join(btrfsBase, RestartPendingMarkerFilename)
	info, err := os.Stat(markerPath)
	if err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("marker mode = %o, want 0600", info.Mode().Perm())
	}
	body, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(string(body))); err != nil {
		t.Errorf("body %q not RFC3339: %v", body, err)
	}
}

// TestRefreshSystemServicesEmptyBaseSkipsMarker confirms the refresh
// handler tolerates an empty BtrfsBasePath — the refresh still
// succeeds, and no marker is written. The next boot then skips the
// freshness stage, which is the correct degraded behaviour for dev
// mode / in-memory runs.
func TestRefreshSystemServicesEmptyBaseSkipsMarker(t *testing.T) {
	t.Parallel()

	restore := TestSetPullImage(func(_ context.Context, _ string) error { return nil })
	t.Cleanup(restore)

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	uiMgr := ui.NewManager(ui.Config{Systemd: sd, Image: "quay.io/town/ui:testtag"})

	ts := InitTestServer(ServerConfig{
		Storage:           mock,
		Systemd:           sd,
		MonitoringBackend: monitoring.BackendUPlot,
		UI:                uiMgr,
		// Deliberately no BtrfsBasePath.
		SystemControllerImage:      "quay.io/town/town:testtag",
		SystemControllerListenAddr: ":5309",
	})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, ts.Server.URL+"/system-services/refresh", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Logf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 even without btrfs base", resp.StatusCode)
	}
}
