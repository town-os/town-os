package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// FloatingTagSuffix is the tag component that marks a moving target. Every tag
// the make pipeline publishes as "newest" ends in it: "latest" itself, and the
// "rc.latest" / "release.latest" families.
const FloatingTagSuffix = "latest"

// imagePullTimeout bounds a pull that runs outside a request. Generous because
// the images it covers are large (Grafana is ~771MB) and a slow home uplink is
// the normal case, but finite: the point is that a registry that accepts the
// connection and then stalls cannot hold a background goroutine — and the state
// flag it owns — for the life of the process.
const imagePullTimeout = 30 * time.Minute

// archTagSuffixes are the per-architecture suffixes every published tag carries
// (the raw `uname -m` form — see the image-tag rules in CLAUDE.md). They are
// stripped before a tag is classified, so "rc.latest-x86_64" floats for the
// same reason "rc.latest" does, while "rc.2026-08-13-x86_64" stays pinned.
var archTagSuffixes = []string{"-x86_64", "-aarch64"}

// EnvImageRefresh turns the floating-tag refresh off when set to a false value
// ("0", "false", "no", "off"). Anything else, including unset, leaves it on —
// a box must default to acquiring the images its tags name.
//
// The harness sets it: `make dev` and the test containers load every image they
// need from the local image cache on purpose (captive networks break registry
// DNS, and the local mirror only covers docker.io), so a refresh there would
// reach for quay.io on every start to replace an image that was placed
// deliberately. An operator on a metered link can set it for the same reason.
const EnvImageRefresh = "TOWN_OS_IMAGE_REFRESH"

// imageRefreshDisabled reports whether EnvImageRefresh switches the refresh off.
func imageRefreshDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvImageRefresh))) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

// imageExistsLocally reports whether the image is already in the local podman
// store. A package-level variable for the same reason pullImage is one: tests
// drive the classification without a live podman.
var imageExistsLocally = func(ctx context.Context, image string) bool {
	return exec.CommandContext(ctx, "podman", "image", "exists", image).Run() == nil //nolint:gosec // G204 -- image from caller
}

// imageTag returns the tag portion of a container image reference, or "" when
// the reference carries no tag at all.
//
// The last colon is only a tag separator when nothing after it contains a "/":
// "registry:5000/town/ui" is a host:port and a repository, not a tag.
func imageTag(ref string) string {
	colon := strings.LastIndex(ref, ":")
	if colon < 0 {
		return ""
	}
	if strings.Contains(ref[colon+1:], "/") {
		return ""
	}
	return ref[colon+1:]
}

// FloatingImageRef reports whether ref names a tag whose contents change under
// it — the "latest" family, in any of its arch-suffixed spellings, plus a
// reference with no tag at all (which podman resolves to :latest).
//
// This is the distinction that decides whether an image already in the local
// store is up to date. A pinned tag (rc.2026-08-13-x86_64, release.*, a
// per-instance test tag, a digest) means what it said the day it was pulled, so
// having it locally IS having the right bits. A floating tag means "whatever is
// newest", and a box that only ever checks whether it has *something* under
// that name stays on the image it happened to pull first — which is how a
// re-installed VM kept serving a UI from the day its disk was created.
//
// A localhost/ reference never floats regardless of its tag: nothing can
// republish it, and an attempt to pull one only produces a registry error for
// an image that is already exactly what it will ever be.
func FloatingImageRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if strings.HasPrefix(ref, "localhost/") {
		return false
	}
	if strings.Contains(ref, "@") {
		// Digest reference: immutable by construction. Any tag in front of
		// the digest says nothing about what will be pulled.
		return false
	}
	tag := imageTag(ref)
	if tag == "" {
		// No tag at all: podman resolves this to :latest, which floats.
		return true
	}
	for _, suffix := range archTagSuffixes {
		tag = strings.TrimSuffix(tag, suffix)
	}
	return tag == FloatingTagSuffix || strings.HasSuffix(tag, "."+FloatingTagSuffix)
}

// EnsureImage makes image available locally before a unit that runs it starts.
//
// A pinned reference is fetched only when it is missing. A floating one is
// re-pulled every time, because "present" and "current" are different questions
// for a tag that moves: the boot pull set is the point at which a box picks up
// the images it was told to run, and skipping the pull for anything already in
// the store froze every system service at the version installed on the box's
// first boot. `podman pull` on an unchanged tag is a manifest check, so the
// cost when nothing moved is a round trip, not a download.
//
// A failed refresh of an image that IS present locally is not an error. The
// alternative is a box that cannot reboot without the registry — the exact
// reason the systemcontroller's own unit runs --pull=missing rather than
// --pull=always — so the local copy is used and the failure is logged.
func EnsureImage(ctx context.Context, image string) error {
	if (!FloatingImageRef(image) || imageRefreshDisabled()) && imageExistsLocally(ctx, image) {
		return nil
	}
	if err := pullImage(ctx, image); err != nil {
		// The existence probe is here rather than up front so the common
		// path — a floating tag that refreshes fine — costs one podman
		// invocation instead of two.
		if imageExistsLocally(ctx, image) {
			slog.Warn("refresh of image tag failed; continuing with the local copy",
				"image", image, "error", err)
			return nil
		}
		return fmt.Errorf("pull %s: %w", image, err)
	}
	return nil
}
