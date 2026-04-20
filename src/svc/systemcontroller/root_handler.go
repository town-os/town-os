package systemcontroller

import (
	"net/http"
	"sync/atomic"
)

// RootHandler is an http.Handler whose inner delegate can be swapped
// atomically at runtime. It exists so main.go can bind :5309 with a
// lightweight boot-status handler the instant the process starts, then
// atomically replace it with the full Echo handler once startup finishes
// — without closing the listener (which would drop in-flight SSE
// subscribers during the blackout window the handler is designed to
// cover).
//
// The swap is one `atomic.Pointer.Store` and imposes zero contention on
// the request-serving hot path: each request is one atomic load. A
// request already dispatched to the previous handler completes normally
// even after Swap returns; Swap only affects *subsequent* requests.
type RootHandler struct {
	p atomic.Pointer[http.Handler]
}

// NewRootHandler wraps the given initial handler. `initial` must be non-nil.
func NewRootHandler(initial http.Handler) *RootHandler {
	r := &RootHandler{}
	r.p.Store(&initial)
	return r
}

// ServeHTTP delegates to whichever handler is currently installed.
func (r *RootHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	(*r.p.Load()).ServeHTTP(w, req)
}

// Swap replaces the active handler. Returns immediately; requests
// already dispatched to the old handler keep running against it.
func (r *RootHandler) Swap(next http.Handler) {
	r.p.Store(&next)
}
