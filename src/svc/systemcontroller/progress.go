package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"
)

// ProgressWriter streams SSE progress events during long-running operations
// like install, uninstall, and repository refresh. Each event is a JSON
// object prefixed with "data: " per the SSE spec.
type ProgressWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

type progressEvent struct {
	Step  string `json:"step,omitempty"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
}

// NewProgressWriter initializes SSE headers and returns a writer that streams
// progress events. Returns nil if the response does not support flushing.
func NewProgressWriter(c *echo.Context) *ProgressWriter {
	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}

	return &ProgressWriter{w: w, flusher: flusher}
}

// Step emits a progress event with the current step name.
func (pw *ProgressWriter) Step(step string) {
	pw.emit(progressEvent{Step: step})
}

// Done emits a completion event.
func (pw *ProgressWriter) Done() {
	pw.emit(progressEvent{Done: true})
}

// Err emits an error event. The operation is considered failed.
func (pw *ProgressWriter) Err(err error) {
	pw.emit(progressEvent{Error: err.Error()})
}

func (pw *ProgressWriter) emit(evt progressEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	if _, err := fmt.Fprintf(pw.w, "data: %s\n\n", data); err != nil {
		return
	}
	pw.flusher.Flush()
}
