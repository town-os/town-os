// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// errFailingHook is the canned failure the non-fatal-hook test injects.
var errFailingHook = errors.New("exit status 127")

// postInstallRecorder captures every (container, command) pair the install
// path execs, in order. It stands in for podman so the whole install handler
// — compile, dependency ordering, unit install, hook dispatch — runs for real
// while the only thing faked is the process boundary at the very end.
type postInstallRecorder struct {
	mu    sync.Mutex
	calls []postInstallCall
}

type postInstallCall struct {
	container string
	command   string
}

func (r *postInstallRecorder) exec(_ context.Context, container, command string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, postInstallCall{container: container, command: command})
	return "", nil
}

// commandsFor returns the operator commands recorded against one container,
// with the readiness probe (`true`) filtered out.
func (r *postInstallRecorder) commandsFor(container string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.calls {
		if c.container != container || c.command == "true" {
			continue
		}
		out = append(out, c.command)
	}
	return out
}

// setupInlinePostInstallRepo builds a file:// repository holding the supplied
// package YAMLs and a systemcontroller wired to a recording exec function.
// Mirrors setupInlineSharedVolumeRepo, but keyed by an arbitrary package map
// because post-install topologies need more than a parent and one dep.
func setupInlinePostInstallRepo(t *testing.T, pkgs map[string]string) (*systemcontroller.SystemdClient, *postInstallRecorder) {
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

	for name, body := range pkgs {
		pkgDir := filepath.Join(dir, "local", packages.PackagesDir, name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(body), 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	rec := &postInstallRecorder{}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:           storage.InitBtrFSMock(),
		RepositoryRoot:    rr,
		Installer:         packages.NewInstallManager(dir),
		Systemd:           systemd.InitMockManager(),
		BtrfsBasePath:     t.TempDir(),
		ContainerExecFunc: rec.exec,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, rec
}

// TestIntegrationPostInstallRunsInParentContainer is the end-to-end check for
// the simple case: a standalone package's own post_install commands run, in
// order, inside its own container, with question responses substituted.
func TestIntegrationPostInstallRunsInParentContainer(t *testing.T) {
	t.Parallel()

	parentYAML := `image: alpine:3.20
description: "post-install parent"
network:
  external:
    "@port@": "80"
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
  apikey:
    query: "API key?"
post_install:
  - "echo first @apikey@"
  - "echo second"
`
	c, rec := setupInlinePostInstallRepo(t, map[string]string{"pi-parent": parentYAML})

	if err := c.InstallPackage(context.TODO(), "pi-parent", "1.0", packages.Responses{"port": "9110", "apikey": "s3cr3t"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	container := systemd.ContainerName("local", "pi-parent", "1.0")
	got := rec.commandsFor(container)
	want := []string{"echo first s3cr3t", "echo second"}
	if len(got) != len(want) {
		t.Fatalf("post_install commands = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("command[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestIntegrationPostInstallProbesBeforeCommands pins the readiness probe to
// the front of the sequence. systemd reports a start job "done" before the
// container can serve an exec, so a first command that ran without the probe
// would race the container into existence.
func TestIntegrationPostInstallProbesBeforeCommands(t *testing.T) {
	t.Parallel()

	parentYAML := `image: alpine:3.20
description: "probe order"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
post_install:
  - "echo only"
`
	c, rec := setupInlinePostInstallRepo(t, map[string]string{"pi-parent": parentYAML})

	if err := c.InstallPackage(context.TODO(), "pi-parent", "1.0", packages.Responses{"port": "9111"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	rec.mu.Lock()
	calls := append([]postInstallCall(nil), rec.calls...)
	rec.mu.Unlock()

	if len(calls) < 2 {
		t.Fatalf("expected a probe followed by the command, got %#v", calls)
	}
	if calls[0].command != "true" {
		t.Fatalf("first exec = %q, want the readiness probe %q", calls[0].command, "true")
	}
	if calls[1].command != "echo only" {
		t.Fatalf("second exec = %q, want %q", calls[1].command, "echo only")
	}
}

// TestIntegrationPostInstallWiresDependency covers the case the feature exists
// for: a parent injecting commands into a dep's container to wire it to a
// sibling, with the sibling's container name arriving through @dep_KEY_host@.
// The dep's own post_install must run first, then the parent's injections.
func TestIntegrationPostInstallWiresDependency(t *testing.T) {
	t.Parallel()

	serverYAML := `image: alpine:3.20
description: "media server dep"
network:
  internal:
    http: "8096"
volumes:
  media:
    mountpoint: /media
    shareable: true
questions: {}
`
	arrYAML := `image: alpine:3.20
description: "arr dep"
network:
  internal:
    http: "7878"
volumes:
  movies:
    mountpoint: /movies
    shareable: true
questions: {}
post_install:
  - "echo own-setup"
`
	parentYAML := `image: alpine:3.20
description: "media stack"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  server:
    package: pi-server
    version: "1.0"
  arr:
    package: pi-arr
    version: "1.0"
    post_install:
      - "notify http://@dep_server_host@:@dep_server_port_http@/refresh"
`
	c, rec := setupInlinePostInstallRepo(t, map[string]string{
		"pi-parent": parentYAML,
		"pi-server": serverYAML,
		"pi-arr":    arrYAML,
	})

	if err := c.InstallPackage(context.TODO(), "pi-parent", "1.0", packages.Responses{"port": "9112"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	arrContainer := systemd.ContainerName("local", "pi-parent--dep--arr", "1.0")
	serverContainer := systemd.ContainerName("local", "pi-parent--dep--server", "1.0")

	got := rec.commandsFor(arrContainer)
	if len(got) != 2 {
		t.Fatalf("arr commands = %#v, want 2 (own then injected)", got)
	}
	if got[0] != "echo own-setup" {
		t.Fatalf("arr command[0] = %q, want the dep's own %q", got[0], "echo own-setup")
	}
	// The sibling's container name and named port must both have resolved:
	// an unresolved marker reaching `sh -c` is the failure this substitution
	// exists to prevent, and it would look like a working install.
	wantInjected := "notify http://" + serverContainer + ":8096/refresh"
	if got[1] != wantInjected {
		t.Fatalf("arr command[1] = %q, want %q", got[1], wantInjected)
	}
	if strings.Contains(got[1], "@dep_") {
		t.Fatalf("dep marker survived substitution: %q", got[1])
	}
}

// TestIntegrationPostInstallOrdersAfterReferencedSibling holds the topological
// edge that @dep_KEY_host@ in a post_install command adds. The referenced
// sibling sorts alphabetically AFTER the referencing one, so without the edge
// the marker would be substituted against an env map that does not hold it
// yet and would reach the container verbatim.
func TestIntegrationPostInstallOrdersAfterReferencedSibling(t *testing.T) {
	t.Parallel()

	plainYAML := `image: alpine:3.20
description: "plain dep"
network:
  internal:
    http: "8096"
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "ordering parent"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  aaa:
    package: pi-plain
    version: "1.0"
    post_install:
      - "notify @dep_zzz_host@"
  zzz:
    package: pi-plain
    version: "1.0"
`
	c, rec := setupInlinePostInstallRepo(t, map[string]string{
		"pi-parent": parentYAML,
		"pi-plain":  plainYAML,
	})

	if err := c.InstallPackage(context.TODO(), "pi-parent", "1.0", packages.Responses{"port": "9113"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	aaaContainer := systemd.ContainerName("local", "pi-parent--dep--aaa", "1.0")
	zzzContainer := systemd.ContainerName("local", "pi-parent--dep--zzz", "1.0")

	got := rec.commandsFor(aaaContainer)
	if len(got) != 1 {
		t.Fatalf("aaa commands = %#v, want 1", got)
	}
	want := "notify " + zzzContainer
	if got[0] != want {
		t.Fatalf("aaa command = %q, want %q — the zzz edge did not order the install", got[0], want)
	}
}

// TestIntegrationPostInstallFailureDoesNotFailInstall: by the time the hook
// runs, volumes are provisioned, the install record is saved and the units are
// started. There is nothing to roll back, so a failing command must be logged
// and left behind rather than reported as a failed install.
func TestIntegrationPostInstallFailureDoesNotFailInstall(t *testing.T) {
	t.Parallel()

	parentYAML := `image: alpine:3.20
description: "failing hook"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
post_install:
  - "definitely-not-a-command"
`
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
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "pi-parent")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(parentYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        systemd.InitMockManager(),
		BtrfsBasePath:  t.TempDir(),
		// The probe succeeds so the hook is actually attempted; the command
		// itself is what fails.
		ContainerExecFunc: func(_ context.Context, _, command string) (string, error) {
			if command == "true" {
				return "", nil
			}
			return "sh: definitely-not-a-command: not found", errFailingHook
		},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "pi-parent", "1.0", packages.Responses{"port": "9114"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage reported failure for a failed post_install hook: %v", err)
	}

	// The install must actually be recorded, not merely reported as fine.
	version, found, err := inst.GetInstalledVersion("local", "pi-parent")
	if err != nil {
		t.Fatalf("GetInstalledVersion: %v", err)
	}
	if !found || version != "1.0" {
		t.Fatalf("install record = (%q, %v), want (\"1.0\", true)", version, found)
	}
}
