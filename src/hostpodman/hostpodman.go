// Package hostpodman provides a podman CLI wrapper that routes every
// command through the host's podman socket.
//
// The systemcontroller runs inside its own podman container. If it invokes
// /usr/bin/podman directly, operations land in the container's isolated
// storage instead of the host's — built images never become visible to
// the systemd units on the host that reference them, pulled images vanish
// at container restart, and container lifecycle events are disconnected
// from everything else running on the machine.
//
// To avoid that, all runtime podman operations in the systemcontroller
// must go through this package. The systemcontroller container bind-mounts
// /run/podman/podman.sock from the host and every Command call prepends
// --url unix:///run/podman/podman.sock so the CLI talks to host podman
// over that socket instead of using its own local storage.
package hostpodman

import (
	"context"
	"os/exec"
)

// SocketURL is the unix socket URL that Command routes every podman
// invocation through. The systemcontroller systemd unit on the host
// must bind-mount /run/podman/podman.sock from the host into the
// container at this path for podman to reach it.
const SocketURL = "unix:///run/podman/podman.sock"

// Command returns an exec.Cmd that runs podman against the host socket.
// Callers pass the subcommand and its arguments; the --url flag is
// prepended automatically. Do not pass --url in args — podman will
// reject duplicate global flags.
//
//nolint:gosec // G204 -- callers are trusted systemcontroller code
func Command(ctx context.Context, args ...string) *exec.Cmd {
	full := make([]string, 0, len(args)+2)
	full = append(full, "--url", SocketURL)
	full = append(full, args...)
	return exec.CommandContext(ctx, "podman", full...)
}
