// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// bootFrame is the SSE payload the boot stub emits on /boot-status.
type bootFrame struct {
	Step  string `json:"step,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

// pingBody is the subset of both /status/ping shapes (boot stub and full
// router) that the refresh UI reads. Both must carry boot_id — that is the
// only thing distinguishing the outgoing controller from its successor.
type pingBody struct {
	Status  string `json:"status"`
	Booting bool   `json:"booting"`
	Step    string `json:"step"`
	Done    bool   `json:"done"`
	BootID  string `json:"boot_id"`
}

func getPing(t *testing.T, url string) (int, pingBody) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/status/ping", nil)
	if err != nil {
		t.Fatalf("new ping request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close ping body: %v", err)
		}
	}()

	var body pingBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode ping body: %v", err)
	}
	return resp.StatusCode, body
}

// readBootStatus opens /boot-status and reads frames until it has `want` of
// them, a done frame arrives, or the stream closes. It returns the HTTP
// status (so a 404 from the full router can be asserted) and the frames.
//
// The bound matters: the stub replays history to a late subscriber and then
// HOLDS THE STREAM OPEN for live events — it only closes on Done. A reader
// that waits for close while the controller is still mid-boot blocks until
// its context expires. Every call here therefore says how many frames the
// generation under test has emitted so far (0 when no stream is expected).
func readBootStatus(t *testing.T, url string, want int) (int, []bootFrame) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/boot-status", nil)
	if err != nil {
		t.Fatalf("new boot-status request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("boot-status: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close boot-status body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}

	frames := make([]bootFrame, 0, want)
	scanner := bufio.NewScanner(resp.Body)
	for len(frames) < want && scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var f bootFrame
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f); err != nil {
			t.Fatalf("unmarshal frame %q: %v", line, err)
		}
		frames = append(frames, f)
		if f.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan boot-status: %v", err)
	}
	return resp.StatusCode, frames
}

// TestBootStatusRefreshGenerationHandoff drives the exact HTTP sequence the
// Refresh Core Services dialog sees, against a real listener, a real btrfs
// storage backend, and the real Echo router — through the RootHandler swap
// that a booting systemcontroller performs.
//
// The bug it pins: the full router does NOT serve /boot-status, so a booted
// controller 404s it. During a refresh the OLD process stays up for about a
// second after accepting the request, 404ing that route exactly like a
// finished new process would. A client that reads 404 as "boot complete"
// therefore declares the refresh done against the process that is about to
// die, and never renders a single stage of the restart it was opened to show.
//
// boot_id is what makes the two generations distinguishable, so this test
// asserts the ping carries it on BOTH sides of the swap, that it is stable
// within one incarnation, and that it changes when the process restarts.
func TestBootStatusRefreshGenerationHandoff(t *testing.T) {
	btr := storage.InitBtrFS("/town-os")

	// --- Generation 1: a controller that is still booting. ---
	bs1 := systemcontroller.NewBootStatus()
	root := systemcontroller.NewRootHandler(systemcontroller.NewBootHandler(bs1))
	srv := httptest.NewServer(root)
	t.Cleanup(srv.Close)

	bs1.Step("open_db")
	bs1.Step("reconcile")

	// While booting, ping must be 503 (so readiness probes don't call a
	// half-booted controller ready) and must carry the generation's id.
	code, body := getPing(t, srv.URL)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("booting ping status = %d, want 503", code)
	}
	if !body.Booting {
		t.Error("booting ping: booting = false, want true")
	}
	if body.Step != "reconcile" {
		t.Errorf("booting ping: step = %q, want %q", body.Step, "reconcile")
	}
	gen1 := body.BootID
	if gen1 == "" {
		t.Fatal("booting ping: boot_id is empty; the refresh UI cannot tell one process from another without it")
	}
	if gen1 != bs1.BootID() {
		t.Errorf("booting ping: boot_id = %q, want %q", gen1, bs1.BootID())
	}

	// The stub serves the stream and replays history to a late subscriber.
	streamCode, frames := readBootStatus(t, srv.URL, 2)
	if streamCode != http.StatusOK {
		t.Fatalf("boot-status status = %d, want 200", streamCode)
	}
	if len(frames) != 2 || frames[0].Step != "open_db" || frames[1].Step != "reconcile" {
		t.Fatalf("boot-status frames = %+v, want the emitted steps in order", frames)
	}

	// --- Generation 1 finishes booting: swap in the full Echo router. ---
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	root.Swap(systemcontroller.NewHandler(ctx, systemcontroller.ServerConfig{
		Storage: btr,
		BootID:  bs1.BootID(),
	}))
	bs1.Done()

	// This is the state the refresh dialog opens against: a fully booted
	// controller that 404s /boot-status. Same generation, so 404 here does
	// NOT mean a restart completed.
	if streamCode, _ := readBootStatus(t, srv.URL, 0); streamCode != http.StatusNotFound {
		t.Fatalf("booted /boot-status status = %d, want 404 (the full router has no such route)", streamCode)
	}
	code, body = getPing(t, srv.URL)
	if code != http.StatusOK {
		t.Fatalf("booted ping status = %d, want 200", code)
	}
	if body.Status != "ok" {
		t.Errorf("booted ping: status = %q, want %q", body.Status, "ok")
	}
	if body.BootID != gen1 {
		t.Fatalf("booted ping: boot_id = %q, want %q — the id must survive the stub → full-router swap, "+
			"or a refresh client cannot recognize the process it asked to restart", body.BootID, gen1)
	}

	// --- Generation 2: the process restarts. ---
	bs2 := systemcontroller.NewBootStatus()
	if bs2.BootID() == gen1 {
		t.Fatal("restarted controller reused the previous boot_id; generations must be distinguishable")
	}
	root.Swap(systemcontroller.NewBootHandler(bs2))
	bs2.Step("open_db")

	code, body = getPing(t, srv.URL)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("restarted ping status = %d, want 503 (booting again)", code)
	}
	if body.BootID == gen1 {
		t.Fatal("restarted ping still reports the old boot_id; the refresh UI would keep waiting forever")
	}
	if body.BootID != bs2.BootID() {
		t.Errorf("restarted ping: boot_id = %q, want %q", body.BootID, bs2.BootID())
	}

	// The successor streams its own boot, which is what the stepper renders.
	streamCode, frames = readBootStatus(t, srv.URL, 1)
	if streamCode != http.StatusOK {
		t.Fatalf("restarted /boot-status status = %d, want 200", streamCode)
	}
	if len(frames) != 1 || frames[0].Step != "open_db" {
		t.Fatalf("restarted boot-status frames = %+v, want only the new generation's steps "+
			"(history from the previous generation must not leak into the new stream)", frames)
	}
}

// TestBootStatusPingBootIDStableWithinGeneration pins the other half of the
// contract: the id identifies a PROCESS, not a request or a boot stage, so
// repeated pings across the boot must all report the same value. A client
// polling for "has it changed yet?" would otherwise see spurious restarts.
func TestBootStatusPingBootIDStableWithinGeneration(t *testing.T) {
	bs := systemcontroller.NewBootStatus()
	srv := httptest.NewServer(systemcontroller.NewBootHandler(bs))
	t.Cleanup(srv.Close)

	_, first := getPing(t, srv.URL)
	if first.BootID == "" {
		t.Fatal("ping boot_id is empty")
	}

	for _, step := range []string{"create_dirs", "pull_images", "reconcile"} {
		bs.Step(step)
		_, body := getPing(t, srv.URL)
		if body.BootID != first.BootID {
			t.Fatalf("boot_id changed mid-boot at step %q: %q != %q", step, body.BootID, first.BootID)
		}
		if body.Step != step {
			t.Errorf("ping step = %q, want %q", body.Step, step)
		}
	}
}
