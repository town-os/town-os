package systemcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

// recordingReporter captures Step names in order so tests can assert the
// event sequence without dragging in the real BootStatus.
type recordingReporter struct {
	mu    sync.Mutex
	steps []string
}

func (r *recordingReporter) Step(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, s)
}

func (r *recordingReporter) Steps() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.steps)
}

// stubLister returns a canned list + optional error, matching the
// FreshnessLister interface exactly.
type stubLister struct {
	items []string
	err   error
}

func (s *stubLister) ListInstalled() ([]string, error) { return s.items, s.err }

// recordingRestarter records (unit, action) tuples and optionally
// injects an error for specific unit names.
type recordingRestarter struct {
	mu    sync.Mutex
	calls []string
	errFn func(unit string) error
}

func (r *recordingRestarter) SetStatus(_ context.Context, unit string, _ systemd.StatusAction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, unit)
	if r.errFn != nil {
		return r.errFn(unit)
	}
	return nil
}

func (r *recordingRestarter) Calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

func TestWriteRestartPendingMarker_EmptyBaseIsNoOp(t *testing.T) {
	if err := WriteRestartPendingMarker(""); err != nil {
		t.Errorf("empty baseDir should no-op, got err=%v", err)
	}
}

func TestWriteRestartPendingMarker_CreatesFile(t *testing.T) {
	base := t.TempDir()
	if err := WriteRestartPendingMarker(base); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	path := filepath.Join(base, RestartPendingMarkerFilename)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("marker mode = %o, want 0600", mode)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("marker body is empty")
	}
}

func TestRunFreshnessStage_NoMarkerIsNoOp(t *testing.T) {
	base := t.TempDir() // deliberately no marker
	r := &recordingReporter{}
	lister := &stubLister{items: []string{"core/gitea@1.0"}}
	restarter := &recordingRestarter{}

	failed, err := RunFreshnessStage(t.Context(), r, lister, restarter, base)
	if err != nil {
		t.Fatalf("no-marker call should not error, got %v", err)
	}
	if len(failed) != 0 {
		t.Errorf("failed = %+v, want empty", failed)
	}
	if len(r.Steps()) != 0 {
		t.Errorf("Step events emitted without marker: %+v", r.Steps())
	}
	if len(restarter.Calls()) != 0 {
		t.Errorf("unit restarted without marker: %+v", restarter.Calls())
	}
}

func TestRunFreshnessStage_EmptyBaseIsNoOp(t *testing.T) {
	r := &recordingReporter{}
	lister := &stubLister{items: []string{"core/gitea@1.0"}}
	restarter := &recordingRestarter{}

	failed, err := RunFreshnessStage(t.Context(), r, lister, restarter, "")
	if err != nil {
		t.Fatalf("empty baseDir should not error, got %v", err)
	}
	if len(failed) != 0 || len(r.Steps()) != 0 || len(restarter.Calls()) != 0 {
		t.Errorf("unexpected side effects on empty baseDir")
	}
}

func TestRunFreshnessStage_RestartsEachPackageAndRemovesMarker(t *testing.T) {
	base := t.TempDir()
	if err := WriteRestartPendingMarker(base); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	r := &recordingReporter{}
	lister := &stubLister{items: []string{
		"core/gitea@1.0",
		"core/gitea--dep--postgres@15.0",
		"extras/matrix@2.0",
	}}
	restarter := &recordingRestarter{}

	failed, err := RunFreshnessStage(t.Context(), r, lister, restarter, base)
	if err != nil {
		t.Fatalf("freshness stage: %v", err)
	}
	if len(failed) != 0 {
		t.Errorf("failed = %+v, want empty", failed)
	}

	// Expect a top-level "refresh_packages" step followed by one event
	// per installed identifier. Order matches ListInstalled order.
	wantSteps := []string{
		"refresh_packages",
		"refreshing_core/gitea",
		"refreshing_core/gitea--dep--postgres",
		"refreshing_extras/matrix",
	}
	if got := r.Steps(); !slices.Equal(got, wantSteps) {
		t.Errorf("steps = %+v, want %+v", got, wantSteps)
	}

	// Each installed package should have produced one unit restart.
	wantCalls := []string{
		systemd.UnitName("core", "gitea", "1.0"),
		systemd.UnitName("core", "gitea--dep--postgres", "15.0"),
		systemd.UnitName("extras", "matrix", "2.0"),
	}
	if got := restarter.Calls(); !slices.Equal(got, wantCalls) {
		t.Errorf("restart calls = %+v, want %+v", got, wantCalls)
	}

	// Marker must be gone so the next boot does not repeat.
	if _, err := os.Stat(filepath.Join(base, RestartPendingMarkerFilename)); !os.IsNotExist(err) {
		t.Errorf("marker still present after successful stage; stat err=%v", err)
	}
}

func TestRunFreshnessStage_SinglePackageFailureDoesNotAbort(t *testing.T) {
	base := t.TempDir()
	if err := WriteRestartPendingMarker(base); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	r := &recordingReporter{}
	lister := &stubLister{items: []string{
		"core/a@1.0",
		"core/b@1.0",
		"core/c@1.0",
	}}
	failing := systemd.UnitName("core", "b", "1.0")
	restarter := &recordingRestarter{
		errFn: func(unit string) error {
			if unit == failing {
				return errors.New("systemd say no")
			}
			return nil
		},
	}

	failed, err := RunFreshnessStage(t.Context(), r, lister, restarter, base)
	if err != nil {
		t.Fatalf("freshness stage should not return an I/O error on restart failure, got %v", err)
	}
	if want := []string{"core/b@1.0"}; !slices.Equal(failed, want) {
		t.Errorf("failed = %+v, want %+v", failed, want)
	}

	// All three packages should have been attempted — b's failure did
	// not abort the loop.
	if got := restarter.Calls(); len(got) != 3 {
		t.Errorf("restart calls = %+v, want 3 entries", got)
	}
}

func TestRunFreshnessStage_UnparseableIdentSkipped(t *testing.T) {
	base := t.TempDir()
	if err := WriteRestartPendingMarker(base); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	r := &recordingReporter{}
	lister := &stubLister{items: []string{
		"bogus-no-slash",
		"core/no-at-sign",
		"core/valid@1.0",
	}}
	restarter := &recordingRestarter{}

	_, err := RunFreshnessStage(t.Context(), r, lister, restarter, base)
	if err != nil {
		t.Fatalf("stage: %v", err)
	}

	// Only the well-formed identity triggered a restart.
	if got := restarter.Calls(); !slices.Equal(got, []string{systemd.UnitName("core", "valid", "1.0")}) {
		t.Errorf("restart calls = %+v, want exactly one valid restart", got)
	}
	// Two step events: the top-level "refresh_packages" + one
	// "refreshing_core/valid". The malformed entries produced no events.
	if got := r.Steps(); len(got) != 2 {
		t.Errorf("steps = %+v, want 2", got)
	}
}

func TestRunFreshnessStage_ListerErrorSurfaces(t *testing.T) {
	base := t.TempDir()
	if err := WriteRestartPendingMarker(base); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	r := &recordingReporter{}
	lister := &stubLister{err: errors.New("disk on fire")}
	restarter := &recordingRestarter{}

	_, err := RunFreshnessStage(t.Context(), r, lister, restarter, base)
	if err == nil {
		t.Fatalf("expected error from failing lister")
	}
	// Marker must still exist — we did not complete the stage, so next
	// boot should retry.
	if _, statErr := os.Stat(filepath.Join(base, RestartPendingMarkerFilename)); statErr != nil {
		t.Errorf("marker removed despite lister error: %v", statErr)
	}
}
