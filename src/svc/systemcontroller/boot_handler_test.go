package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBootHandler_PingReturnsBootingShape(t *testing.T) {
	bs := NewBootStatus()
	bs.Step("initializing")
	h := NewBootHandler(bs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/status/ping", nil))

	// 503 while booting — external readiness probes must NOT treat the
	// stub as "ready".
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var body bootPingResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !body.Booting {
		t.Errorf("booting = false, want true")
	}
	if body.Step != "initializing" {
		t.Errorf("step = %q, want initializing", body.Step)
	}
	if body.Done {
		t.Errorf("done = true, want false")
	}
}

func TestBootHandler_PingFlipsBootingFalseAfterDone(t *testing.T) {
	bs := NewBootStatus()
	bs.Done()
	h := NewBootHandler(bs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/status/ping", nil))

	// Status flips to 200 once Done fires — only now is the controller
	// legitimately ready for external readiness probes.
	if rec.Code != http.StatusOK {
		t.Errorf("status after Done = %d, want 200", rec.Code)
	}
	var body bootPingResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Booting || !body.Done {
		t.Errorf("want {booting:false, done:true}, got %+v", body)
	}
}

func TestBootHandler_ForbidsOtherPaths(t *testing.T) {
	bs := NewBootStatus()
	h := NewBootHandler(bs)

	for _, path := range []string{"/packages", "/storage/package-volumes", "/accounts", "/anything-else"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, rec.Code)
		}
	}
}

func TestBootHandler_ForbidsNonGetOnPingPath(t *testing.T) {
	// POST /status/ping should 403 because the mux only registers the
	// GET verb — this proves the method-scoped registration works.
	bs := NewBootStatus()
	h := NewBootHandler(bs)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/status/ping", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /status/ping status = %d, want 403", rec.Code)
	}
}

func TestBootHandler_BootStatusStreamsEvents(t *testing.T) {
	bs := NewBootStatus()
	// Seed one event before subscribing so the history replay fires.
	bs.Step("first")

	srv := httptest.NewServer(NewBootHandler(bs))
	defer srv.Close()

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
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response body: %v", err)
		}
	}()

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", got)
	}

	events := make(chan progressEvent, 16)
	go func() {
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
			select {
			case events <- evt:
			default:
				return
			}
		}
	}()

	// Expect "first" from history replay, then "second" from live push,
	// then Done terminates the stream.
	got := []progressEvent{}
	expect := []progressEvent{{Step: "first"}, {Step: "second"}, {Done: true}}

	// Push the live event after a short delay so the server has already
	// written the history frame.
	go func() {
		time.Sleep(50 * time.Millisecond)
		bs.Step("second")
		time.Sleep(50 * time.Millisecond)
		bs.Done()
	}()

	for len(got) < len(expect) {
		select {
		case evt := <-events:
			got = append(got, evt)
		case <-ctx.Done():
			t.Fatalf("timed out; got %+v, want %+v", got, expect)
		}
	}
	for i, evt := range expect {
		if got[i] != evt {
			t.Errorf("event %d = %+v, want %+v", i, got[i], evt)
		}
	}
}
