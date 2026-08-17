package systemcontroller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// execRecorder is a ContainerExecFunc that records every command it is handed
// and replies from a scripted table. It exists so the post-install machinery
// can be driven with no podman, no container, and no root.
type execRecorder struct {
	mu       sync.Mutex
	commands []string
	// failUntil makes the first N calls fail, modelling a container that is
	// not yet accepting execs.
	failUntil int
	// failCommands maps a specific command string to the error it returns.
	failCommands map[string]error
	// output is returned for every successful call.
	output string
}

func (r *execRecorder) exec(_ context.Context, _ string, command string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, command)
	if r.failUntil > 0 {
		r.failUntil--
		return "", errors.New("container is not running")
	}
	if err, ok := r.failCommands[command]; ok {
		return "", err
	}
	return r.output, nil
}

func (r *execRecorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func TestPostInstallCommandsMergeOrder(t *testing.T) {
	t.Parallel()

	own := []string{"own-1", "own-2"}
	injected := []string{"inj-1"}

	got := postInstallCommands(own, injected)
	want := []string{"own-1", "own-2", "inj-1"}
	if len(got) != len(want) {
		t.Fatalf("merged = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("merged[%d] = %q, want %q (full: %#v)", i, got[i], want[i], got)
		}
	}
}

// TestPostInstallCommandsNeverAliases is the guard that keeps
// applyDepTemplatesSlice from rewriting the compiled package in place. The
// merge result is handed straight to that function, which mutates entries, so
// an aliased return would leave a resolved hostname baked into the cached
// InputPackage for every later install.
func TestPostInstallCommandsNeverAliases(t *testing.T) {
	t.Parallel()

	t.Run("only own", func(t *testing.T) {
		t.Parallel()

		own := []string{"cmd"}
		got := postInstallCommands(own, nil)
		got[0] = "mutated"
		if own[0] != "cmd" {
			t.Fatalf("own aliased: %q", own[0])
		}
	})

	t.Run("only injected", func(t *testing.T) {
		t.Parallel()

		injected := []string{"cmd"}
		got := postInstallCommands(nil, injected)
		got[0] = "mutated"
		if injected[0] != "cmd" {
			t.Fatalf("injected aliased: %q", injected[0])
		}
	})

	t.Run("neither", func(t *testing.T) {
		t.Parallel()

		if got := postInstallCommands(nil, nil); got != nil {
			t.Fatalf("merged = %#v, want nil", got)
		}
	})
}

func TestWaitContainerReadySucceedsImmediately(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{}
	if err := waitContainerReady(context.Background(), rec.exec, "c", time.Second, time.Millisecond); err != nil {
		t.Fatalf("waitContainerReady: %v", err)
	}
	got := rec.recorded()
	if len(got) != 1 || got[0] != "true" {
		t.Fatalf("probe commands = %#v, want [true]", got)
	}
}

// TestWaitContainerReadyRetries is the reason the wait exists at all:
// systemd's start job reports "done" once podman run has been launched, which
// is strictly before the container can serve an exec.
func TestWaitContainerReadyRetries(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{failUntil: 3}
	if err := waitContainerReady(context.Background(), rec.exec, "c", 10*time.Second, time.Millisecond); err != nil {
		t.Fatalf("waitContainerReady: %v", err)
	}
	if got := len(rec.recorded()); got != 4 {
		t.Fatalf("probe attempts = %d, want 4", got)
	}
}

func TestWaitContainerReadyTimesOut(t *testing.T) {
	t.Parallel()

	// A zero timeout still probes once — the deadline check happens after the
	// attempt — then gives up without sleeping.
	rec := &execRecorder{failUntil: 100}
	err := waitContainerReady(context.Background(), rec.exec, "c", 0, time.Millisecond)
	if !errors.Is(err, errContainerNeverReady) {
		t.Fatalf("err = %v, want errContainerNeverReady", err)
	}
	if got := len(rec.recorded()); got != 1 {
		t.Fatalf("probe attempts = %d, want 1", got)
	}
	// The last underlying failure has to survive into the message, or the
	// journal says only "not ready" and never why.
	if err != nil && !strings.Contains(err.Error(), "container is not running") {
		t.Fatalf("err does not carry the underlying failure: %v", err)
	}
}

func TestWaitContainerReadyHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	rec := &execRecorder{failUntil: 100}
	err := waitContainerReady(ctx, rec.exec, "c", time.Minute, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestWaitContainerReadyRejectsNilExec(t *testing.T) {
	t.Parallel()

	if err := waitContainerReady(context.Background(), nil, "c", time.Second, time.Millisecond); err == nil {
		t.Fatal("waitContainerReady accepted a nil exec function")
	}
}

func TestRunPostInstallRunsCommandsInOrder(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{}
	runPostInstall(context.Background(), rec.exec, "c", []string{"first", "second", "third"}, time.Second, time.Millisecond)

	got := rec.recorded()
	want := []string{"true", "first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("commands[%d] = %q, want %q (full: %#v)", i, got[i], want[i], got)
		}
	}
}

// TestRunPostInstallContinuesAfterFailure: one failed wiring call must not
// strand the ones after it. The stack is configured by several independent
// commands, and stopping at the first failure would leave the rest of a
// working install unwired for a reason unrelated to them.
func TestRunPostInstallContinuesAfterFailure(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{failCommands: map[string]error{"second": errors.New("boom")}}
	runPostInstall(context.Background(), rec.exec, "c", []string{"first", "second", "third"}, time.Second, time.Millisecond)

	got := rec.recorded()
	if len(got) != 4 {
		t.Fatalf("commands = %#v, want 4 entries (probe + 3)", got)
	}
	if got[3] != "third" {
		t.Fatalf("command after the failure did not run; got %#v", got)
	}
}

// TestRunPostInstallSkipsWhenNeverReady holds the ordering guarantee: if the
// container never accepts an exec, no operator command is attempted at all.
// Running them against a container that is not up would report failures that
// say nothing about the commands themselves.
func TestRunPostInstallSkipsWhenNeverReady(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{failUntil: 1 << 20}
	runPostInstall(context.Background(), rec.exec, "c", []string{"first"}, 0, time.Millisecond)

	for _, cmd := range rec.recorded() {
		if cmd != "true" {
			t.Fatalf("ran %q against a container that never became ready", cmd)
		}
	}
}

func TestRunPostInstallNoCommandsIsANoop(t *testing.T) {
	t.Parallel()

	rec := &execRecorder{}
	runPostInstall(context.Background(), rec.exec, "c", nil, time.Second, time.Millisecond)
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("empty command list still probed: %#v", got)
	}
}

// TestRunPostInstallNilExecIsSafe: GetContainerExecFunc never returns nil in
// production, but a test backend can, and a panic in the install handler is a
// worse outcome than a skipped hook.
func TestRunPostInstallNilExecIsSafe(t *testing.T) {
	t.Parallel()

	runPostInstall(context.Background(), nil, "c", []string{"first"}, time.Second, time.Millisecond)
}

// TestApplyDepTemplatesSliceResolvesDepMarkers covers the runtime half of
// substitution: the compile pass deliberately leaves @dep_KEY_*@ alone, and
// this is where the just-installed sibling's container name lands.
func TestApplyDepTemplatesSliceResolvesDepMarkers(t *testing.T) {
	t.Parallel()

	cmds := []string{"curl http://@dep_radarr_host@:@dep_radarr_port_http@/api"}
	applyDepTemplatesSlice(cmds, map[string]string{
		"TOWNOS_DEP_RADARR_HOST":      "town-os-local-radarr-1.0",
		"TOWNOS_DEP_RADARR_PORT_HTTP": "7878",
	})

	want := "curl http://town-os-local-radarr-1.0:7878/api"
	if cmds[0] != want {
		t.Fatalf("resolved = %q, want %q", cmds[0], want)
	}
}

// TestApplyDepTemplatesSliceCollapsesEscapeWithNoDeps is why the call is made
// unconditionally rather than only when a package has dependencies: this pass
// is also the one that turns `@@` into a literal `@`, and skipping it would
// ship the escape through to `sh -c`.
func TestApplyDepTemplatesSliceCollapsesEscapeWithNoDeps(t *testing.T) {
	t.Parallel()

	cmds := []string{"login user@@example.com"}
	applyDepTemplatesSlice(cmds, nil)

	want := "login user@example.com"
	if cmds[0] != want {
		t.Fatalf("resolved = %q, want %q", cmds[0], want)
	}
}
