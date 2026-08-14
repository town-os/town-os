// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/monitoring"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// The harness mounts this at container start (make/test.sh); its mirror stanza
// is the only registry these tests are allowed to reach. Everything below
// pushes into that mirror under a name unique to this run, so no test can
// collide with a parallel one and nothing here needs Docker Hub.
const localRegistryConf = "/etc/containers/registries.conf.d/local-registry.conf"

var mirrorLocationRE = regexp.MustCompile(`location\s*=\s*"([^"]+)"`)

// mirrorAddress returns the host:port of the local registry the harness
// configured as docker.io's mirror, or "" when there is none.
func mirrorAddress(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(localRegistryConf)
	if err != nil {
		return ""
	}
	// The file has two locations: docker.io itself, then the mirror. The
	// mirror is the one under [[registry.mirror]], so take the last match.
	matches := mirrorLocationRE.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return ""
	}
	last := matches[len(matches)-1]
	if len(last) < 2 || last[1] == "docker.io" {
		return ""
	}
	return last[1]
}

func podmanRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	return podmanIn(ctx, args...)
}

// untagRef removes exactly one name from an image, and is always called with
// both arguments: `podman untag REF` with no name removes EVERY tag the image
// carries, which for a fixture built by tagging a shared image would strip the
// name a parallel test is using.
//
// It runs on its own timeout context rather than t.Context(): cleanups run
// after that context is cancelled, so a cleanup that used it would never issue
// the command it exists to issue.
func untagRef(t *testing.T, ref string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if out, err := podmanIn(ctx, "untag", ref, ref); err != nil {
		t.Logf("cleanup untag %s: %v: %s", ref, err, out)
	}
}

func podmanIn(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "podman", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("podman %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	return string(out), nil
}

// imageID returns the local image ID a reference currently resolves to, or ""
// when the reference is not in the local store.
func imageID(t *testing.T, ref string) string {
	t.Helper()
	out, err := podmanRun(t, "image", "inspect", "--format", "{{.Id}}", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// imageRefreshFixture stages the situation every Town OS box is in on its
// second boot: a reference that is already in the local store, whose registry
// content has since moved on.
//
// The mirror gets image A under a unique repository; the local store gets image
// B under the same reference. What EnsureImage does next is then visible as an
// image ID: A means it refreshed, B means it decided the local copy was
// sufficient.
type imageRefreshFixture struct {
	repo     string // docker.io/library/<unique>
	localID  string // image B: what the reference resolves to before the call
	remoteID string // image A: what the mirror serves
}

func stageImageRefreshFixture(t *testing.T, tags ...string) imageRefreshFixture {
	t.Helper()

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}
	mirror := mirrorAddress(t)
	if mirror == "" {
		t.Skip("no local registry mirror configured; run via make test-integration")
	}

	const remoteSource = "docker.io/library/alpine:latest"
	remoteID := imageID(t, remoteSource)
	localID := imageID(t, monitoring.NodeExporterImage)
	if remoteID == "" || localID == "" {
		t.Skipf("fixture images not loaded in this container (%s=%q, %s=%q)",
			remoteSource, remoteID, monitoring.NodeExporterImage, localID)
	}
	if remoteID == localID {
		t.Fatal("fixture images must differ; the whole test is an image ID comparison")
	}

	// Unique per run AND per test: two tests staging the same repository name
	// would each be asserting on the other's pushes.
	repo := fmt.Sprintf("docker.io/library/town-os-refresh-%d", time.Now().UnixNano())

	for _, tag := range tags {
		ref := repo + ":" + tag
		// Push image A to the mirror under the docker.io-mapped name. The
		// mirror is plain HTTP (insecure = true in the harness config), which
		// is why the push needs the flag and the pull does not.
		mirrorRef := strings.Replace(ref, "docker.io", mirror, 1)
		if _, err := podmanRun(t, "tag", remoteSource, mirrorRef); err != nil {
			t.Fatalf("tag for push: %v", err)
		}
		if _, err := podmanRun(t, "push", "--tls-verify=false", mirrorRef); err != nil {
			t.Skipf("cannot push to the local mirror at %s: %v", mirror, err)
		}
		if _, err := podmanRun(t, "untag", remoteSource, mirrorRef); err != nil {
			t.Fatalf("untag after push: %v", err)
		}
		// Now point the local reference at image B, so "already present" is
		// true and "up to date" is false.
		if _, err := podmanRun(t, "tag", monitoring.NodeExporterImage, ref); err != nil {
			t.Fatalf("tag local: %v", err)
		}
		t.Cleanup(func() { untagRef(t, ref) })
	}

	return imageRefreshFixture{repo: repo, localID: localID, remoteID: remoteID}
}

// A floating tag that is already in the local store must still be fetched: this
// is the boot path, and skipping the pull here is what pinned a re-booted box
// to the images it downloaded the day its disk was created.
func TestEnsureImageRefreshesFloatingTagAgainstTheRegistry(t *testing.T) {
	// Not parallel: these set TOWN_OS_IMAGE_REFRESH so the production
	// behaviour is what is under test, and the harness sets it to 0 for the
	// container as a whole (make/test.sh). t.Setenv and t.Parallel are
	// mutually exclusive, and env is process-wide either way.
	t.Setenv(systemcontroller.EnvImageRefresh, "1")

	f := stageImageRefreshFixture(t, "rc.latest-x86_64")
	ref := f.repo + ":rc.latest-x86_64"

	if got := imageID(t, ref); got != f.localID {
		t.Fatalf("fixture: %s should start as the local image %s, got %s", ref, f.localID, got)
	}
	if err := systemcontroller.EnsureImage(t.Context(), ref); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if got := imageID(t, ref); got != f.remoteID {
		t.Fatalf("floating tag was not refreshed: %s resolves to %s, want the registry's %s", ref, got, f.remoteID)
	}
}

// A pinned tag means what it said the day it was pulled, so having it locally
// IS having the right bits — no registry round trip on every boot.
func TestEnsureImageLeavesPinnedTagAlone(t *testing.T) {
	t.Setenv(systemcontroller.EnvImageRefresh, "1")

	f := stageImageRefreshFixture(t, "rc.2026-01-01-x86_64")
	ref := f.repo + ":rc.2026-01-01-x86_64"

	if err := systemcontroller.EnsureImage(t.Context(), ref); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if got := imageID(t, ref); got != f.localID {
		t.Fatalf("pinned tag was re-pulled: %s resolves to %s, want the untouched local %s", ref, got, f.localID)
	}
}

// A box has to reboot when the registry does not answer. The refresh is
// attempted, fails, and the local copy carries the boot — the same reason the
// systemcontroller's own unit runs --pull=missing rather than --pull=always.
func TestEnsureImageFallsBackToLocalCopyWhenTheRegistryIsUnreachable(t *testing.T) {
	t.Setenv(systemcontroller.EnvImageRefresh, "1")

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}
	localID := imageID(t, monitoring.NodeExporterImage)
	if localID == "" {
		t.Skipf("%s not loaded in this container", monitoring.NodeExporterImage)
	}

	// Port 1 on the loopback: nothing listens there, and the connection is
	// refused immediately rather than hanging the test on a timeout.
	ref := fmt.Sprintf("127.0.0.1:1/town-os-offline-%d:rc.latest-x86_64", time.Now().UnixNano())
	if _, err := podmanRun(t, "tag", monitoring.NodeExporterImage, ref); err != nil {
		t.Fatalf("tag: %v", err)
	}
	t.Cleanup(func() { untagRef(t, ref) })

	if err := systemcontroller.EnsureImage(t.Context(), ref); err != nil {
		t.Fatalf("a failed refresh with the image already local must not fail the boot: %v", err)
	}
	if got := imageID(t, ref); got != localID {
		t.Fatalf("local copy should be untouched: %s resolves to %s, want %s", ref, got, localID)
	}
}

// The self-update decision, end to end against a real registry and a real
// podman: the box is running the image its tag USED to name, the refresh pulls
// what the tag names now, and the controller asks systemd to restart it into
// the new one.
//
// Only "which container am I" is stubbed, and it has to be: this is a test
// binary, not the systemcontroller container, and the test container's own
// podman does not know the id in its /proc/1/cgroup (that id belongs to the
// host's podman, which the real controller reaches through CONTAINER_HOST).
// Everything downstream of that answer — the pull, the tag-to-id comparison,
// the marker, the restart request — is the production path.
func TestSelfUpdateRestartsIntoTheImageTheTagNowNames(t *testing.T) {
	t.Setenv(systemcontroller.EnvImageRefresh, "1")

	f := stageImageRefreshFixture(t, "rc.latest-x86_64")
	ref := f.repo + ":rc.latest-x86_64"
	btrfsBase := t.TempDir()

	// Running the stale image, which is exactly what the reference resolves
	// to locally right now.
	restore := systemcontroller.TestSetSelfImage(ref, f.localID)
	defer restore()

	sd := systemd.InitMockManager()
	if !systemcontroller.SelfUpdate(t.Context(), sd, btrfsBase) {
		t.Fatal("expected a restart to be requested once the tag moved")
	}
	if got := imageID(t, ref); got != f.remoteID {
		t.Fatalf("self-update did not refresh the reference: %s resolves to %s, want %s", ref, got, f.remoteID)
	}
	if !findMockRestart(sd, systemd.SystemControllerUnitName) {
		t.Fatalf("expected Restart on %q, got calls: %v", systemd.SystemControllerUnitName, sd.GetCalls())
	}
	marker, err := os.ReadFile(filepath.Join(btrfsBase, systemcontroller.SelfUpdateMarkerFilename))
	if err != nil {
		t.Fatalf("read self-update marker: %v", err)
	}
	if got := strings.TrimSpace(string(marker)); got != f.remoteID {
		t.Fatalf("marker = %q, want the image the restart was for (%q)", got, f.remoteID)
	}

	// The boot that comes back up runs the image the tag names, so it must
	// not restart again — that would be a loop, once per boot, forever.
	restore()
	defer systemcontroller.TestSetSelfImage(ref, f.remoteID)()
	sd.ClearCalls()
	if systemcontroller.SelfUpdate(t.Context(), sd, btrfsBase) {
		t.Fatal("a box already running what its tag names must not restart")
	}
	if findMockRestart(sd, systemd.SystemControllerUnitName) {
		t.Fatal("no restart may be requested when nothing moved")
	}
}

// findMockRestart reports whether the mock recorded a restart of the unit.
func findMockRestart(sd *systemd.MockManager, unit string) bool {
	for _, call := range sd.GetCalls() {
		if call.Method != "SetStatus" || len(call.Args) < 2 {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok || name != unit {
			continue
		}
		if action, ok := call.Args[1].(systemd.StatusAction); ok && action == systemd.Restart {
			return true
		}
	}
	return false
}

// The same unreachable registry, with nothing local to fall back to, IS an
// error: the caller has no image at all.
func TestEnsureImageFailsWhenUnreachableAndAbsent(t *testing.T) {
	t.Setenv(systemcontroller.EnvImageRefresh, "1")

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}

	ref := fmt.Sprintf("127.0.0.1:1/town-os-missing-%d:rc.latest-x86_64", time.Now().UnixNano())
	if err := systemcontroller.EnsureImage(t.Context(), ref); err == nil {
		t.Fatal("expected an error for an image that is neither pullable nor local")
	}
}
