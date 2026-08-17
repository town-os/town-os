// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// Everything else that exercises post_install replaces the exec function with
// a recorder, which is the right call for asserting order, merging and marker
// resolution — but it means the process boundary itself is the one part of the
// path nothing runs. PodmanContainerExec is where a wrong `sh -c`, a swallowed
// stderr, or an exec into a container that is not up would live, and a recorder
// cannot fail any of those.
//
// So these run against a real container. The alpine image is loaded into the
// test container by the harness (make/test.sh), and the name each test uses is
// unique per run — IRON RULE: two `make test-full` runs in different checkouts
// share this host's podman, and a fixed name would have them stealing each
// other's container through `--replace`.

// postInstallToken returns a token unique to this moment, used to keep container
// and package names from colliding with a concurrent run. Named for this file
// rather than generically: every test in package integration_test shares one
// namespace, and several sessions add to it.
func postInstallToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}

// startPostInstallProbeContainer runs an idle alpine container under the given name and
// removes it when the test ends.
func startPostInstallProbeContainer(t *testing.T, name string) {
	t.Helper()

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "podman", "run", "-d", "--replace",
		"--name", name, "docker.io/library/alpine:latest", "sleep", "600").CombinedOutput()
	if err != nil {
		t.Skipf("cannot start the probe container (%v):\n%s", err, out)
	}
	t.Cleanup(func() {
		// Cleanup is exempt from the never-swallow-exit-codes rule.
		_ = exec.Command("podman", "rm", "-f", name).Run() //nolint:noctx // best-effort cleanup
	})
}

// The production exec path, against a container that exists: the command goes
// through a shell, so an operator's `&&`, redirection and pipes mean what they
// mean in the YAML, and the output comes back for the journal.
func TestPodmanContainerExecRunsThroughAShell(t *testing.T) {
	t.Parallel()

	name := "town-os-postinstall-shell-" + postInstallToken()
	startPostInstallProbeContainer(t, name)

	out, err := systemcontroller.PodmanContainerExec(t.Context(), name, "echo one > /tmp/marker && cat /tmp/marker")
	if err != nil {
		t.Fatalf("PodmanContainerExec: %v", err)
	}
	if strings.TrimSpace(out) != "one" {
		t.Fatalf("output = %q, want %q", strings.TrimSpace(out), "one")
	}
}

// A failing command has to carry its own output back. runPostInstall logs the
// error and moves on, so this string is the entire record an operator gets of
// why a hook did not do what they wrote it to do — and stderr is where a shell
// puts that.
func TestPodmanContainerExecCarriesTheFailureOutput(t *testing.T) {
	t.Parallel()

	name := "town-os-postinstall-fail-" + postInstallToken()
	startPostInstallProbeContainer(t, name)

	out, err := systemcontroller.PodmanContainerExec(t.Context(), name, "echo boom >&2; exit 3")
	if err == nil {
		t.Fatalf("a command exiting 3 reported success; output %q", out)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error does not carry the command's output: %v", err)
	}
}

// The failure the readiness wait polls on. systemd's start job reports done
// once `podman run` has been launched, which is strictly before the container
// can serve an exec — so "container is not there yet" has to be an error rather
// than an empty success, or the wait would return immediately and every hook
// would race the container start.
func TestPodmanContainerExecFailsWhenTheContainerIsNotRunning(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available")
	}

	name := "town-os-postinstall-absent-" + postInstallToken()
	if _, err := systemcontroller.PodmanContainerExec(t.Context(), name, "true"); err == nil {
		t.Fatal("exec into a container that does not exist reported success")
	}
}

// setupRealExecPostInstallRepo builds a file:// repository holding one package
// and a systemcontroller whose exec function is the PRODUCTION one. Deliberately
// separate from the recorder-based setup the other post-install tests share:
// the point here is that nothing between the install handler and podman is
// substituted.
func setupRealExecPostInstallRepo(t *testing.T, pkgName, body string) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, pkgName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll %s: %v", pkgName, err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(body), 0600); err != nil {
		t.Fatalf("WriteFile %s: %v", pkgName, err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      packages.NewInstallManager(dir),
		Systemd:        systemd.InitMockManager(),
		BtrfsBasePath:  t.TempDir(),
		// The production exec function, not a stand-in.
		ContainerExecFunc: systemcontroller.PodmanContainerExec,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c
}

// The whole path with nothing faked at the far end: the install handler
// compiles the package, derives the container name, and PodmanContainerExec
// runs the operator's commands inside a container that is actually running.
//
// The container is created here rather than by the install, because systemd is
// mocked and starts nothing — but it is created under exactly the name
// systemd.ContainerName derives, which is the part being proven. A hook that
// addressed the container by any other spelling would find nothing there, and
// (by design) would say so only in the journal.
func TestPostInstallReachesARealContainerThroughTheInstallPath(t *testing.T) {
	t.Parallel()

	// Unique per run: the container name is derived from the package name, and
	// this host's podman is shared with every other checkout's test run.
	pkgName := "postinstall" + postInstallToken()
	container := systemd.ContainerName("local", pkgName, "1.0")
	startPostInstallProbeContainer(t, container)

	c := setupRealExecPostInstallRepo(t, pkgName, `image: alpine:3.20
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
post_install:
  - "echo first >> /tmp/town-os-hook"
  - "echo second >> /tmp/town-os-hook"
`)

	if err := c.InstallPackage(t.Context(), pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	out, err := systemcontroller.PodmanContainerExec(t.Context(), container, "cat /tmp/town-os-hook")
	if err != nil {
		t.Fatalf("the hook left nothing behind in %s: %v", container, err)
	}
	if got := strings.Fields(out); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("hook output = %q, want the two commands in declaration order", out)
	}
}

// A hook that fails must not fail the install. By the time it runs the package
// is fully installed — volumes provisioned, record saved, units written and
// started — so reporting a failure would report one for an install that
// happened, and there would be nothing to roll back to make the report true.
func TestPostInstallFailureAgainstARealContainerDoesNotFailTheInstall(t *testing.T) {
	t.Parallel()

	pkgName := "postinstallfail" + postInstallToken()
	container := systemd.ContainerName("local", pkgName, "1.0")
	startPostInstallProbeContainer(t, container)

	c := setupRealExecPostInstallRepo(t, pkgName, `image: alpine:3.20
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
post_install:
  - "exit 42"
  - "echo ran-anyway > /tmp/town-os-hook-after-failure"
`)

	if err := c.InstallPackage(t.Context(), pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("a failing post_install failed the install: %v", err)
	}

	// And the command after the failure still ran: the commands are
	// independent wiring steps, not a script that aborts on the first error.
	out, err := systemcontroller.PodmanContainerExec(t.Context(), container, "cat /tmp/town-os-hook-after-failure")
	if err != nil {
		t.Fatalf("the command after the failing one never ran: %v", err)
	}
	if strings.TrimSpace(out) != "ran-anyway" {
		t.Fatalf("output = %q, want %q", strings.TrimSpace(out), "ran-anyway")
	}
}
