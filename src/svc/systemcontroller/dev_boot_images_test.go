// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"strings"
	"testing"
)

// Every image the boot sequence starts a unit from has to be in the harness
// container's inner podman store BEFORE the systemcontroller boots. Not as an
// optimization: the generated units run `podman run` without --pull=never, so a
// missing image is not an error that fails fast, it is a registry pull happening
// inside `podman run` while require_controller_ready's 120s budget and the
// service's own 30s readiness wait both count down. On a slow or captive link
// that is a harness failure reported as "the systemcontroller never came up".
//
// The ingress and caddy were both missing from `make dev` for exactly that
// reason, and caddy from the integration harness besides. These tests are
// written against the LOAD CALLS rather than a list of image names, so an image
// added to the boot set has to be loaded somewhere to pass.

// harnessLoads reports whether the script loads the given image reference --
// either a shell variable expansion (${INGRESS_IMAGE}) or a literal ref -- into
// some container.
func harnessLoads(script, ref string) bool {
	for line := range strings.SplitSeq(script, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "load_images_into_container") {
			continue
		}
		if strings.Contains(line, ref) {
			return true
		}
	}
	return false
}

// The ingress is started during boot and the pages service runs on caddy, so
// both are needed before the controller is asked to be ready. The ingress is
// built locally by the `ingress-image` prerequisite; caddy is a base image that
// pull-images has always cached and that nothing ever copied in.
func TestDevHarnessLoadsEveryImageBootStarts(t *testing.T) {
	t.Parallel()
	dev := readRepoFile(t, "make/dev.sh")

	for _, ref := range []string{
		"${ROLODEX_IMAGE}",
		"${NC_IMAGE}",
		"${INGRESS_IMAGE}",
		DefaultCaddyImage,
	} {
		if !harnessLoads(dev, ref) {
			t.Errorf("make/dev.sh never loads %s into the dev container; boot will pull it from a "+
				"registry inside require_controller_ready's budget", ref)
		}
	}
}

// The same omission, in the harness where a network pull is least acceptable:
// the integration suite is the thing that must run concurrently and offline.
// The ingress was already loaded here; caddy was not.
func TestIntegrationHarnessLoadsThePagesImage(t *testing.T) {
	t.Parallel()
	test := readRepoFile(t, "make/test.sh")

	if !harnessLoads(test, DefaultCaddyImage) {
		t.Errorf("make/test.sh never loads %s; StartPagesService pulls it from Docker Hub during boot",
			DefaultCaddyImage)
	}
	// Both harness containers, not just the first: the UI integration run boots
	// its own controller through the same path.
	if got := strings.Count(test, "load_images_into_container \"${PODMAN_UI_BACKEND}\""); got == 0 {
		t.Error("make/test.sh loads nothing into the UI integration container")
	}
}

// The ingress has to be BUILT before it can be loaded, and gfeh must not be:
// it is the one local image whose content comes from outside the repo, so it
// carries a day-granularity cache-bust and recompiles the whole gfehd
// dependency tree once per calendar day. make/dev.sh already treats a missing
// gfeh tar as the documented off switch, which is only true if nothing forces
// the build.
func TestDevTargetBuildsTheIngressAndNotGfeh(t *testing.T) {
	t.Parallel()
	line := makefileTargetLine(t, "dev")

	if !strings.Contains(line, "ingress-image") {
		t.Errorf("the `dev` target does not depend on ingress-image, so make/dev.sh has no locally "+
			"built ingress to load; got:\n%s", line)
	}
	if strings.Contains(line, "gfeh-image") {
		t.Errorf("the `dev` target depends on gfeh-image, which recompiles gfehd once per calendar "+
			"day for every developer whether or not object storage is used; got:\n%s", line)
	}
}

// The doomed first boot. systemd starts an enabled unit the instant `podman run
// -d` returns -- before a single image has been loaded -- so an enabled
// systemcontroller boots against an empty store, sits out WaitForDNSReady's
// full 30s, tries to pull what is not there, and leaves failed units behind,
// all of it discarded by the start make/dev.sh issues once loading is done.
//
// rolodex is the deliberate exception: it must be up before the controller
// boots, and its unit carries Restart=always/RestartSec=5 precisely to survive
// the window where its image has not arrived yet.
func TestDevImageDoesNotAutostartTheSystemcontroller(t *testing.T) {
	t.Parallel()
	containerfile := readRepoFile(t, "integration/testdata/Containerfile.dev")

	for line := range strings.SplitSeq(containerfile, "\n") {
		if !strings.Contains(line, "systemctl enable") {
			continue
		}
		if strings.Contains(line, "town-os-systemcontroller.service") {
			t.Errorf("Containerfile.dev enables the systemcontroller, so it boots against an empty "+
				"podman store before make/dev.sh loads anything: %s", strings.TrimSpace(line))
		}
		if !strings.Contains(line, "town-os-system--rolodex.service") {
			t.Errorf("Containerfile.dev no longer enables rolodex, which must be serving before the "+
				"controller boots: %s", strings.TrimSpace(line))
		}
	}
}

// Loading through a bind mount instead of `podman cp` removes one full write of
// every tar -- the image set runs to hundreds of megabytes -- but only for a
// container that actually has the mount. The fallback is what keeps that safe,
// so it is asserted rather than assumed: a harness that forgets the mount must
// keep working, not silently load nothing.
func TestImageLoaderPrefersTheMountAndStillFallsBack(t *testing.T) {
	t.Parallel()
	lib := readRepoFile(t, "make/lib.sh")
	body := shellFuncBody(t, lib, "load_images_into_container")

	if !strings.Contains(body, "${IMAGE_CACHE_MOUNT}") {
		t.Error("load_images_into_container never reads IMAGE_CACHE_MOUNT, so every tar is still copied in")
	}
	if !strings.Contains(body, "podman cp") {
		t.Error("load_images_into_container lost its podman cp fallback; a container without the " +
			"cache mount would silently load nothing")
	}
}

// The mount itself, on every harness container that loads images. Derived from
// the podman run invocations rather than counted, so a new harness container
// cannot quietly omit it.
func TestHarnessContainersMountTheImageCache(t *testing.T) {
	t.Parallel()
	for _, script := range []string{"make/dev.sh", "make/test.sh"} {
		body := readRepoFile(t, script)
		loaders := strings.Count(body, "load_images_into_container \"${PODMAN_")
		if loaders == 0 {
			t.Fatalf("%s: no load_images_into_container calls found — test needs updating", script)
		}
		if !strings.Contains(body, "${IMAGE_CACHE}:${IMAGE_CACHE_MOUNT}:ro,z") {
			t.Errorf("%s loads images but never mounts the image cache, so every tar is copied in "+
				"through podman cp", script)
		}
	}
}

// makefileTargetLine returns the single-line rule declaring target, with its
// prerequisites. Named targets in this Makefile are declared on one line.
func makefileTargetLine(t *testing.T, target string) string {
	t.Helper()
	for line := range strings.SplitSeq(readRepoFile(t, "Makefile"), "\n") {
		if strings.HasPrefix(line, target+":") {
			return line
		}
	}
	t.Fatalf("target %q not found in Makefile", target)
	return ""
}
