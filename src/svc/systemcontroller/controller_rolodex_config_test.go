// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/rolodex"
)

// newProgramTestManager builds a manager whose socket path points into a temp
// dir, so a test can create and recreate that socket to stand in for rolodex
// restarting.
func newProgramTestManager(t *testing.T, cfg rolodex.Config) (*rolodex.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "rolodex.sock")
	cfg.DataDir = dir
	cfg.UnixSocketPath = socket
	return rolodex.NewManager(cfg), socket
}

// bindSocket creates a real unix socket at path, replacing any existing one,
// and returns after it exists. A real socket rather than a plain file because
// Generation is documented against what rolodex actually binds.
func bindSocket(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove old socket: %v", err)
	}
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("listen on %s: %v", path, err)
	}
	t.Cleanup(func() { _ = l.Close() })
}

// TestProgramRolodexPushesEverythingTownOSOwns is the base case: the settings
// that used to be rendered into rolodex.yml now have to arrive over the API,
// and all of them have to arrive — a rolodex missing any one of them is a box
// resolving or filtering differently than the operator asked.
func TestProgramRolodexPushesEverythingTownOSOwns(t *testing.T) {
	t.Parallel()

	mgr, _ := newProgramTestManager(t, rolodex.Config{
		Forwarders:     []string{"10.0.0.1:53"},
		ResolutionMode: rolodex.ResolutionModeForward,
	})
	client := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}

	if err := ProgramRolodex(context.Background(), client, mgr, nil); err != nil {
		t.Fatalf("ProgramRolodex: %v", err)
	}

	if want := []string{"10.0.0.1:53"}; !slices.Equal(client.Forwarders, want) {
		t.Errorf("forwarders = %v, want %v", client.Forwarders, want)
	}
	if client.ResolutionMode != rolodex.ResolutionModeForward {
		t.Errorf("resolution mode = %q, want forward", client.ResolutionMode)
	}
}

// TestProgramRolodexSkipsAnUnchangedMode guards the one push with a cost:
// switching into auto restarts rolodex's tier discovery, so re-asserting a mode
// the server already holds would throw away a settled tier on a box that is
// exactly where it should be. Forwarders have no such cost and are pushed
// unconditionally — there is no getter for them.
func TestProgramRolodexSkipsAnUnchangedMode(t *testing.T) {
	t.Parallel()

	mgr, _ := newProgramTestManager(t, rolodex.Config{ResolutionMode: rolodex.ResolutionModeAuto})
	client := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}

	if err := ProgramRolodex(context.Background(), client, mgr, nil); err != nil {
		t.Fatalf("ProgramRolodex: %v", err)
	}

	if client.Called("SetResolutionMode") {
		t.Error("SetResolutionMode called for a mode the server already had")
	}
	if !client.Called("SetForwarders") {
		t.Error("SetForwarders not called; the forwarder push is unconditional")
	}
}

// TestProgramRolodexReportsEveryFailure asserts the pushes are independent. A
// rolodex that refuses one setting is no reason to leave the others unset, and
// a caller that only saw the first error would log one problem and hide two.
func TestProgramRolodexReportsEveryFailure(t *testing.T) {
	t.Parallel()

	mgr, _ := newProgramTestManager(t, rolodex.Config{ResolutionMode: rolodex.ResolutionModeForward})
	client := &rolodex.MockClient{
		ResolutionMode:       rolodex.ResolutionModeAuto,
		SetForwardersErr:     errors.New("forwarders boom"),
		SetResolutionModeErr: errors.New("mode boom"),
	}

	err := ProgramRolodex(context.Background(), client, mgr, nil)
	if err == nil {
		t.Fatal("ProgramRolodex returned nil with two failing pushes")
	}
	for _, want := range []string{"forwarders boom", "mode boom"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// The mode push was still attempted despite the forwarder failure.
	if !client.Called("SetResolutionMode") {
		t.Error("SetResolutionMode was skipped after the forwarder push failed")
	}
}

// TestReconcileProgrammingRunsOncePerRolodexRun is the tick's whole contract:
// program a run once, then stay silent until rolodex restarts. Without the
// second half this would re-push every 15 seconds forever.
func TestReconcileProgrammingRunsOncePerRolodexRun(t *testing.T) {
	t.Parallel()

	mgr, socket := newProgramTestManager(t, rolodex.Config{})
	client := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}
	s := &serverBase{}
	s.Rolodex = mgr
	s.RolodexClient = client

	bindSocket(t, socket)
	s.reconcileRolodexProgramming(context.Background())
	first := client.CallCount("SetForwarders")
	if first != 1 {
		t.Fatalf("SetForwarders called %d times on the first pass, want 1", first)
	}

	s.reconcileRolodexProgramming(context.Background())
	s.reconcileRolodexProgramming(context.Background())
	if got := client.CallCount("SetForwarders"); got != first {
		t.Fatalf("SetForwarders called %d times over three passes, want %d — the generation check is not holding", got, first)
	}
}

// TestReconcileProgrammingReprogramsAfterARestart is the failure this whole
// path exists for: rolodex keeps these settings in memory only, so a restart it
// performed on its own leaves the box resolving with rolodex's defaults and
// filtering nothing until something notices.
func TestReconcileProgrammingReprogramsAfterARestart(t *testing.T) {
	t.Parallel()

	mgr, socket := newProgramTestManager(t, rolodex.Config{})
	client := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}
	s := &serverBase{}
	s.Rolodex = mgr
	s.RolodexClient = client

	bindSocket(t, socket)
	s.reconcileRolodexProgramming(context.Background())
	before := client.CallCount("SetForwarders")

	// A new run: rolodex unlinks and rebinds its socket at startup.
	bindSocket(t, socket)
	s.reconcileRolodexProgramming(context.Background())

	if got := client.CallCount("SetForwarders"); got != before+1 {
		t.Fatalf("SetForwarders called %d times after a restart, want %d", got, before+1)
	}
}

// TestReconcileProgrammingIgnoresAnAbsentSocket covers rolodex not running yet,
// which is every tick between the controller starting and rolodex binding. It
// must not record a generation for a server it never programmed, or the real
// first run would look already-done.
func TestReconcileProgrammingIgnoresAnAbsentSocket(t *testing.T) {
	t.Parallel()

	mgr, socket := newProgramTestManager(t, rolodex.Config{})
	client := &rolodex.MockClient{ResolutionMode: rolodex.ResolutionModeAuto}
	s := &serverBase{}
	s.Rolodex = mgr
	s.RolodexClient = client

	s.reconcileRolodexProgramming(context.Background())
	if client.Called("SetForwarders") {
		t.Fatal("programmed a rolodex whose socket does not exist")
	}

	bindSocket(t, socket)
	s.reconcileRolodexProgramming(context.Background())
	if !client.Called("SetForwarders") {
		t.Fatal("did not program rolodex once its socket appeared")
	}
}

// TestReconcileProgrammingRetriesAfterAFailedPass asserts a partial failure is
// not recorded as success. Recording it would leave a box with, say, its
// blocklists off until the next restart.
func TestReconcileProgrammingRetriesAfterAFailedPass(t *testing.T) {
	t.Parallel()

	mgr, socket := newProgramTestManager(t, rolodex.Config{})
	client := &rolodex.MockClient{
		ResolutionMode:   rolodex.ResolutionModeAuto,
		SetForwardersErr: errors.New("rolodex busy"),
	}
	s := &serverBase{}
	s.Rolodex = mgr
	s.RolodexClient = client

	bindSocket(t, socket)
	s.reconcileRolodexProgramming(context.Background())
	failed := client.CallCount("SetForwarders")

	client.SetForwardersErr = nil
	s.reconcileRolodexProgramming(context.Background())
	if got := client.CallCount("SetForwarders"); got != failed+1 {
		t.Fatalf("SetForwarders called %d times, want %d — a failed pass must not be recorded as programmed", got, failed+1)
	}
}

// TestReconcileProgrammingNilRolodexIsNoOp: the tick runs on every controller,
// including ones built without a rolodex manager in tests.
func TestReconcileProgrammingNilRolodexIsNoOp(t *testing.T) {
	t.Parallel()

	s := &serverBase{}
	s.reconcileRolodexProgramming(context.Background())
}
