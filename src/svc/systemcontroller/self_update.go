package systemcontroller

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitea.com/town-os/town-os/src/systemd"
)

// SelfUpdateMarkerFilename records the image id the last self-update restarted
// FOR, next to `town-os-version` under the btrfs base. It exists to bound the
// one failure mode a self-restart has: if the restart does not actually change
// the running image — a unit that pins a digest, a `podman run` that resolves
// the tag differently than we did — the box would otherwise decide to restart
// again on every boot, forever, and never finish one.
const SelfUpdateMarkerFilename = "town-os-self-update"

// selfRestartSettleTimeout bounds how long the boot waits after asking systemd
// to restart it. There is nothing to wait FOR — the point is to not race ahead
// with a boot that is about to be thrown away — so this only has to be longer
// than a SIGTERM takes to arrive and short enough that a restart request systemd
// silently dropped costs a boot half a minute rather than hanging it.
const selfRestartSettleTimeout = 30 * time.Second

// runningContainerID reads this process's own container id from
// /proc/1/cgroup, or "" when the process is not running under podman.
var runningContainerID = func() string {
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return ""
	}
	var id string
	for line := range strings.SplitSeq(string(data), "\n") {
		// cgroup v2: "0::/machine.slice/libpod-<id>.scope"
		if _, after, ok := strings.Cut(line, "libpod-"); ok {
			if dotIdx := strings.Index(after, "."); dotIdx > 0 {
				id = after[:dotIdx]
			}
		}
	}
	return id
}

// inspectSelf returns one `podman inspect` field of this process's own
// container, or "" when the lookup fails.
var inspectSelf = func(ctx context.Context, format string) string {
	id := runningContainerID()
	if id == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "podman", "inspect", "--format", format, id).Output() //nolint:gosec // G204 -- id from /proc, format from callers in this file
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// RunningImageID is the image id this process is running, or "" when that
// cannot be determined (not in a container, no podman, detection failed).
func RunningImageID(ctx context.Context) string { return inspectSelf(ctx, "{{.Image}}") }

// RunningImageRef is the image REFERENCE this process was started from —
// `quay.io/town/town:rc.latest-x86_64` rather than a sha.
//
// Asked of podman rather than derived from TOWN_OS_TAG on purpose. The install
// build system substitutes whatever CONTROLLER_IMAGE it was given into the unit,
// which may be another registry entirely, and a self-update that refreshed a
// reference the unit does not actually run would compare two unrelated images
// and restart the box on every boot.
func RunningImageRef(ctx context.Context) string { return inspectSelf(ctx, "{{.ImageName}}") }

// localImageID resolves a reference to the image id it currently names in the
// local store, or "" when it names nothing.
var localImageID = func(ctx context.Context, ref string) string {
	out, err := exec.CommandContext(ctx, "podman", "image", "inspect", "--format", "{{.Id}}", ref).Output() //nolint:gosec // G204 -- ref from podman inspect of our own container
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// SelfUpdate refreshes the systemcontroller's own image and asks systemd to
// restart the unit when the tag it runs under has come to name a different
// image. It reports whether a restart was requested.
//
// This is the other half of EnsureImage. That one keeps every service a box
// runs current, but it cannot do anything about the process executing it: the
// controller's unit is `--pull=missing` (deliberately — a crash-restart must not
// depend on the registry), so the controller stayed on the image the box was
// installed with until an operator pressed "refresh system services". A box that
// updates everything except the thing doing the updating is the one component
// that can never ship a fix for itself.
//
// Everything here is best-effort and silent-on-failure by design: no detection,
// no pull, and no restart request is worth failing a boot over. A box that
// cannot self-update still boots and still serves — it just stays on the version
// it has.
func SelfUpdate(ctx context.Context, sd systemd.Manager, btrfsBase string) bool {
	ref := RunningImageRef(ctx)
	if ref == "" {
		return false // not under podman, or detection failed
	}
	// A pinned reference is the operator pinning the box on purpose (or a
	// localhost image in dev/test); refreshing it could not change anything
	// and restarting for it would be restarting for nothing.
	if !FloatingImageRef(ref) {
		return false
	}
	runningID := RunningImageID(ctx)
	if runningID == "" {
		return false
	}

	if err := EnsureImage(ctx, ref); err != nil {
		fmt.Fprintf(os.Stderr, "self-update: refresh %s: %v\n", ref, err)
		return false
	}
	newID := localImageID(ctx, ref)
	if newID == "" || newID == runningID {
		return false // nothing moved
	}

	markerPath := ""
	if btrfsBase != "" {
		markerPath = filepath.Join(btrfsBase, SelfUpdateMarkerFilename)
		if prev, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(prev)) == newID { //nolint:gosec // G304 -- markerPath from the controlled btrfs base
			// The last boot already restarted for exactly this image and
			// came back running something else. Restarting again would
			// produce the same result and nothing but a boot loop.
			fmt.Fprintf(os.Stderr,
				"self-update: already restarted for %s but still running %s; not restarting again\n", newID, runningID)
			return false
		}
		// Written BEFORE the restart is requested: the process is about to be
		// killed, and a marker written afterwards is a marker that may never
		// be written at all — which is exactly the case the loop guard is for.
		if err := os.WriteFile(markerPath, []byte(newID+"\n"), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "self-update: write marker: %v\n", err)
			return false
		}
	}

	fmt.Fprintf(os.Stderr, "self-update: %s now names %s (running %s); restarting %s\n",
		ref, newID, runningID, systemd.SystemControllerUnitName)
	if err := sd.SetStatus(ctx, systemd.SystemControllerUnitName, systemd.Restart); err != nil {
		fmt.Fprintf(os.Stderr, "self-update: restart %s: %v\n", systemd.SystemControllerUnitName, err)
		// A restart systemd refused is not a restart that happened, so the
		// marker must not survive to tell the next boot that this image was
		// already tried. The guard is for restarts that did not take, not for
		// restarts that never ran.
		if markerPath != "" {
			if rmErr := os.Remove(markerPath); rmErr != nil {
				fmt.Fprintf(os.Stderr, "self-update: clear marker: %v\n", rmErr)
			}
		}
		return false
	}
	return true
}

// AwaitSelfRestart blocks until this process is killed by the restart it asked
// for, the context ends, or the settle timeout passes. The boot calls it right
// after SelfUpdate so the stages that follow do not run — and reconcile does not
// half-write a box's state — in a process systemd is already tearing down.
func AwaitSelfRestart(ctx context.Context) {
	timer := time.NewTimer(selfRestartSettleTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
		fmt.Fprintf(os.Stderr,
			"self-update: still running %v after asking for a restart; continuing this boot\n", selfRestartSettleTimeout)
	}
}
