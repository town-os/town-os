package systemcontroller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// labelHandler returns a handler that writes `label` in its response so
// tests can identify which handler served a given request.
func labelHandler(label string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, label); err != nil {
			panic(err)
		}
	})
}

// newReq is a test helper that builds a request carrying the test's
// context, satisfying the noctx lint rule without cluttering call sites.
func newReq(ctx context.Context) *http.Request {
	return httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
}

func TestRootHandler_DelegatesToInitial(t *testing.T) {
	r := NewRootHandler(labelHandler("boot"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newReq(t.Context()))
	if got := rec.Body.String(); got != "boot" {
		t.Errorf("initial handler body = %q, want boot", got)
	}
}

func TestRootHandler_SwapChangesDelegate(t *testing.T) {
	r := NewRootHandler(labelHandler("boot"))
	r.Swap(labelHandler("full"))

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newReq(t.Context()))
	if got := rec.Body.String(); got != "full" {
		t.Errorf("post-swap body = %q, want full", got)
	}
}

func TestRootHandler_InFlightRequestSurvivesSwap(t *testing.T) {
	// The old handler blocks until the test releases it. Between its
	// dispatch and its return, we call Swap. Verify the old handler
	// still completes normally.
	release := make(chan struct{})
	started := make(chan struct{})
	old := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		if _, err := fmt.Fprint(w, "old-completed"); err != nil {
			panic(err)
		}
	})

	r := NewRootHandler(old)

	var wg sync.WaitGroup
	var body string
	wg.Go(func() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, newReq(t.Context()))
		body = rec.Body.String()
	})

	<-started
	r.Swap(labelHandler("new"))
	close(release)
	wg.Wait()

	if body != "old-completed" {
		t.Errorf("in-flight request body = %q, want old-completed", body)
	}

	// Subsequent requests see the new handler.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newReq(t.Context()))
	if got := rec.Body.String(); got != "new" {
		t.Errorf("post-swap body = %q, want new", got)
	}
}

func TestRootHandler_ConcurrentServeAndSwap(t *testing.T) {
	// Race-detector pressure: 100 goroutines serve while another keeps
	// flipping the inner handler. No panic, no corruption.
	r := NewRootHandler(labelHandler("0"))

	var served atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for range 100 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, newReq(t.Context()))
				served.Add(1)
			}
		})
	}

	// Swapper: alternate between "a" and "b" as fast as possible.
	var swapWG sync.WaitGroup
	swapWG.Go(func() {
		for range 1000 {
			select {
			case <-stop:
				return
			default:
			}
			r.Swap(labelHandler("a"))
			r.Swap(labelHandler("b"))
		}
	})
	swapWG.Wait()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	if served.Load() == 0 {
		t.Fatalf("no requests served under concurrent swap")
	}
}
