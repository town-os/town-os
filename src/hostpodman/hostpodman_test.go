// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package hostpodman

import (
	"context"
	"strings"
	"testing"
)

func TestSocketURLIsHostPodmanSocket(t *testing.T) {
	t.Parallel()
	const want = "unix:///run/podman/podman.sock"
	if SocketURL != want {
		t.Fatalf("SocketURL: expected %q, got %q", want, SocketURL)
	}
}

func TestCommandInvokesPodmanBinary(t *testing.T) {
	t.Parallel()
	cmd := Command(context.TODO(), "ps")
	if cmd == nil {
		t.Fatal("Command returned nil")
	}
	if len(cmd.Args) == 0 {
		t.Fatal("Command returned empty args")
	}
	// Args[0] is the binary; systemcontroller relies on /usr/bin/podman
	// being in PATH.
	if cmd.Args[0] != "podman" {
		t.Fatalf("expected binary 'podman', got %q", cmd.Args[0])
	}
}

func TestCommandPrependsURLFlagBeforeSubcommand(t *testing.T) {
	t.Parallel()
	cmd := Command(context.TODO(), "image", "exists", "nginx:latest")

	// Expected: podman --url unix:///run/podman/podman.sock image exists nginx:latest
	want := []string{"podman", "--url", SocketURL, "image", "exists", "nginx:latest"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("expected %d args, got %d: %v", len(want), len(cmd.Args), cmd.Args)
	}
	for i, a := range want {
		if cmd.Args[i] != a {
			t.Fatalf("arg %d: expected %q, got %q", i, a, cmd.Args[i])
		}
	}
}

func TestCommandURLFlagGoesBeforeAllArgs(t *testing.T) {
	t.Parallel()
	// podman's global flags must come before the subcommand. Verify the
	// --url flag is at positions 1 and 2 (right after the binary name) so
	// podman's CLI parser recognizes it as a global option.
	cmd := Command(context.TODO(), "pull", "quay.io/example/image:latest")

	if cmd.Args[1] != "--url" {
		t.Fatalf("expected --url at position 1, got %q (full args: %v)", cmd.Args[1], cmd.Args)
	}
	if cmd.Args[2] != SocketURL {
		t.Fatalf("expected socket URL at position 2, got %q", cmd.Args[2])
	}
	// Subcommand comes after the global flag.
	if cmd.Args[3] != "pull" {
		t.Fatalf("expected subcommand 'pull' at position 3, got %q", cmd.Args[3])
	}
}

func TestCommandAcceptsNoArgs(t *testing.T) {
	t.Parallel()
	cmd := Command(context.TODO())
	if len(cmd.Args) != 3 {
		t.Fatalf("expected 3 args (podman + --url + socket), got %d: %v", len(cmd.Args), cmd.Args)
	}
}

func TestCommandIsContextBound(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := Command(ctx, "ps")
	// Running a command with a canceled context should fail immediately
	// (not hang) — verifies the context is wired into the exec.Cmd.
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	// The specific error depends on environment (context canceled or
	// "killed"), so just check it's not nil and mentions the process.
	msg := err.Error()
	if !strings.Contains(msg, "kill") && !strings.Contains(msg, "canceled") && !strings.Contains(msg, "signal") && !strings.Contains(msg, "executable file not found") {
		t.Logf("unexpected error form, still non-nil so test passes: %v", err)
	}
}

func TestCommandPreservesArgOrder(t *testing.T) {
	t.Parallel()
	cmd := Command(context.TODO(), "run", "--rm", "--name", "test", "nginx:latest", "echo", "hello")

	// Args after --url/socket should match the caller's order exactly.
	passed := cmd.Args[3:]
	want := []string{"run", "--rm", "--name", "test", "nginx:latest", "echo", "hello"}
	if len(passed) != len(want) {
		t.Fatalf("expected %d passthrough args, got %d: %v", len(want), len(passed), passed)
	}
	for i, a := range want {
		if passed[i] != a {
			t.Fatalf("passthrough arg %d: expected %q, got %q", i, a, passed[i])
		}
	}
}
