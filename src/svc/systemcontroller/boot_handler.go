package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// bootPingResponse is the minimal shape returned by the early /status/ping
// stub. It is deliberately a strict subset of the full ping response so the
// UI's existing poll keeps working during the pre-swap window — it just
// sees Booting=true and a Step field instead of the full system state.
type bootPingResponse struct {
	Booting bool   `json:"booting"`
	Step    string `json:"step,omitempty"`
	Done    bool   `json:"done,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewBootHandler returns the early HTTP handler that backs :5309 until
// the full Echo router is swapped in. Surface:
//
//   - GET /status/ping    → JSON {booting:true, step, ...}
//   - GET /boot-status    → SSE stream of progressEvent frames
//   - *                   → 403 Forbidden
//
// A bare http.ServeMux is used intentionally: the stub must never
// accidentally mount a real API route (e.g. if a future developer
// extends the Echo router and forgets to thread the boot handler
// separately), so we keep this handler hermetic.
func NewBootHandler(bs *BootStatus) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status/ping", func(w http.ResponseWriter, _ *http.Request) {
		step, done, errStr := bs.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		// 503 while still booting so external readiness probes (the
		// test container's wait_for_url, container orchestrators'
		// health checks, etc.) do not treat the boot stub's ping as
		// "service ready" and start hammering a half-booted
		// controller. Only after RunBoot + RootHandler.Swap complete
		// does the full /status/ping take over and return 200. The
		// JSON body still carries booting/step/done so the UI refresh
		// poller can distinguish "controller coming up" from
		// "controller fully down" and render accurate progress copy.
		status := http.StatusServiceUnavailable
		if done {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		body := bootPingResponse{Booting: !done, Step: step, Done: done, Error: errStr}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			return
		}
	})
	mux.HandleFunc("GET /boot-status", func(w http.ResponseWriter, r *http.Request) {
		streamBootStatus(bs, w, r)
	})
	// Anything else: 403. The whole point of the early handler is to
	// lock out privileged routes during boot; 403 is the correct
	// answer (not 404 — we know the route exists in the full handler;
	// it's just unavailable until swap).
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "booting", http.StatusForbidden)
	})
	return mux
}

// streamBootStatus upgrades the response to SSE and forwards every event
// from a fresh Subscription. Exits when the client disconnects (context
// cancels) or the BootStatus closes the subscriber channel (Done or
// overflow).
//
// Pre-existing subscribers survive a RootHandler swap because their
// writer/flusher are held by the already-dispatched goroutine; the swap
// only affects *new* requests.
func streamBootStatus(bs *BootStatus, w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := bs.Subscribe()
	// The cancel needs to run once even if we exit via either path
	// (client disconnect or channel close). sync.Once guards against
	// the close-then-cancel race where the channel closed because of
	// overflow and the defer still fires.
	var cancelOnce sync.Once
	defer cancelOnce.Do(cancel)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, open := <-ch:
			if !open {
				return
			}
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
			if evt.Done {
				return
			}
		}
	}
}
