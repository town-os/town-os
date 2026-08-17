// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

// controller_post_install_test.go covers the post-install machinery in
// isolation — the merge, the readiness wait, the runner. This file covers the
// half that only the install handler can answer: whether anything calls it,
// with which container, and with the markers resolved.
//
// Every failure in that half is silent by construction. runPostInstall logs and
// swallows, so a hook wired to the wrong container, or handed
// "@dep_db_host@" verbatim, produces an install that reports success and a
// stack that is not connected. Nothing else in the suite would notice.

// execCall is one command the install path sent into one container.
type execCall struct {
	container string
	command   string
}

// wiringExecRecorder is a ContainerExecFunc that records the container as well
// as the command, which is the whole point here: the unit-level recorder in
// controller_post_install_test.go only keeps commands, and "which container"
// is exactly what the install path decides.
type wiringExecRecorder struct {
	mu    sync.Mutex
	calls []execCall
}

func (r *wiringExecRecorder) exec(_ context.Context, container, command string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, execCall{container: container, command: command})
	return "", nil
}

func (r *wiringExecRecorder) recorded() []execCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]execCall(nil), r.calls...)
}

// commandsFor returns, in order, the commands the recorder saw for one
// container, with the readiness probe dropped — it is asserted separately
// where it matters and is noise everywhere else.
func (r *wiringExecRecorder) commandsFor(container string) []string {
	var cmds []string
	for _, call := range r.recorded() {
		if call.container == container && call.command != "true" {
			cmds = append(cmds, call.command)
		}
	}
	return cmds
}

// initPostInstallServer builds an install-capable test server with a recording
// exec function. Systemd is a mock and non-nil deliberately: the post_install
// call sites live inside the `sd != nil` block, because the container they exec
// into does not exist until the unit that runs it has been started.
func initPostInstallServer(t *testing.T) (*SystemdClient, *packages.RepositoryRoot, *wiringExecRecorder) {
	t.Helper()

	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "repo-a", URL: *u}}

	rec := &wiringExecRecorder{}
	ts := InitTestServer(ServerConfig{
		Storage:           storage.InitBtrFSMock(),
		RepositoryRoot:    rr,
		Installer:         packages.InitMockInstallManager(),
		Systemd:           systemd.InitMockManager(),
		ContainerExecFunc: rec.exec,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, rr, rec
}

// wantCommands compares one container's command list against an expectation.
func wantCommands(t *testing.T, rec *wiringExecRecorder, container string, want []string) {
	t.Helper()
	got := rec.commandsFor(container)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands for %s = %#v, want %#v (all calls: %#v)", container, got, want, rec.recorded())
	}
}

// The base case, and the one that decides whether the feature exists at all:
// a package's own post_install runs inside that package's own container, in
// declaration order, once its unit has been started.
func TestInstallRunsPostInstallInThePackagesOwnContainer(t *testing.T) {
	c, rr, rec := initPostInstallServer(t)

	writeTestPackage(t, rr.BaseDir, "repo-a", "app", "1.0", `image: example/app:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
post_install:
  - "app-first"
  - "app-second"
`)

	if err := c.InstallPackage(context.TODO(), "app", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	container := systemd.ContainerName("repo-a", "app", "1.0")
	wantCommands(t, rec, container, []string{"app-first", "app-second"})

	// The readiness probe has to come first, and it has to go to the same
	// container: a probe against anything else proves nothing about the
	// container the commands are about to run in.
	calls := rec.recorded()
	if len(calls) == 0 || calls[0].command != "true" || calls[0].container != container {
		t.Fatalf("first call = %#v, want the readiness probe on %s", calls, container)
	}
}

// A package with no post_install must not exec at all — not even the readiness
// probe. The probe waits up to postInstallReadyTimeout, so running it for
// packages that have nothing to do would put that wait on every install on the
// box.
func TestInstallWithoutPostInstallNeverExecs(t *testing.T) {
	c, rr, rec := initPostInstallServer(t)

	writeTestPackage(t, rr.BaseDir, "repo-a", "app", "1.0", `image: example/app:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
`)

	if err := c.InstallPackage(context.TODO(), "app", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}
	if got := rec.recorded(); len(got) != 0 {
		t.Fatalf("a package with no post_install still ran %#v", got)
	}
}

// The parent's own post_install resolves @dep_KEY_host@ against the deps that
// were just installed, exactly as its environment does. Compile deliberately
// leaves those markers alone — there is no container name to substitute at
// compile time — so if the install path skipped this pass the marker would
// reach `sh -c` verbatim and the command would address a host called
// "@dep_db_host@".
func TestParentPostInstallResolvesDependencyMarkers(t *testing.T) {
	c, rr, rec := initPostInstallServer(t)

	writeTestPackage(t, rr.BaseDir, "repo-a", "db", "1.0", `image: example/db:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
`)
	writeTestPackage(t, rr.BaseDir, "repo-a", "app", "1.0", `image: example/app:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
dependencies:
  db:
    package: db
post_install:
  - "wire @dep_db_host@"
`)

	if err := c.InstallPackage(context.TODO(), "app", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	depContainer := systemd.ContainerName("repo-a", packages.DependencyName("app", "db"), "1.0")
	wantCommands(t, rec, systemd.ContainerName("repo-a", "app", "1.0"), []string{"wire " + depContainer})
}

// A dependency runs its own post_install first and the parent's injection
// second. The order is the contract: the dep's own commands configure the
// package as its author intended, and the parent's wire that configured
// package to its siblings — an injection that ran first would be configuring
// something not yet configured.
func TestDependencyRunsOwnPostInstallThenTheParentsInjection(t *testing.T) {
	c, rr, rec := initPostInstallServer(t)

	writeTestPackage(t, rr.BaseDir, "repo-a", "db", "1.0", `image: example/db:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
post_install:
  - "db-own"
`)
	writeTestPackage(t, rr.BaseDir, "repo-a", "app", "1.0", `image: example/app:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
dependencies:
  db:
    package: db
    post_install:
      - "db-injected"
`)

	if err := c.InstallPackage(context.TODO(), "app", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	depContainer := systemd.ContainerName("repo-a", packages.DependencyName("app", "db"), "1.0")
	wantCommands(t, rec, depContainer, []string{"db-own", "db-injected"})

	// And the parent, which declared no post_install of its own, ran nothing.
	if got := rec.commandsFor(systemd.ContainerName("repo-a", "app", "1.0")); len(got) != 0 {
		t.Fatalf("parent ran %#v with no post_install of its own", got)
	}
}

// An injected command may name a *sibling* dep, which is the case the feature
// exists for: one dep is handed the coordinates of another. Two things have to
// hold together for it — the referenced sibling installs first (the ordering
// edge), and its address is substituted in (the template pass). Either one
// missing produces the same visible result: a command containing a literal
// marker, run against a container that may not be up.
func TestInjectedDependencyPostInstallResolvesSiblingMarkers(t *testing.T) {
	c, rr, rec := initPostInstallServer(t)

	writeTestPackage(t, rr.BaseDir, "repo-a", "alpha", "1.0", `image: example/alpha:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
`)
	writeTestPackage(t, rr.BaseDir, "repo-a", "beta", "1.0", `image: example/beta:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
`)
	// "b" sorts before "a" alphabetically only if nothing orders them, so the
	// keys are chosen to make the ordering edge do real work: without it,
	// alphabetical order would install "b" first and envVars would hold no
	// entry for "a" when b's injection is substituted.
	writeTestPackage(t, rr.BaseDir, "repo-a", "parent", "1.0", `image: example/parent:1.0
environment: {}
network:
  external: {}
  internal: {}
volumes: {}
dependencies:
  z:
    package: alpha
  b:
    package: beta
    post_install:
      - "point-at @dep_z_host@"
`)

	if err := c.InstallPackage(context.TODO(), "parent", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	zContainer := systemd.ContainerName("repo-a", packages.DependencyName("parent", "z"), "1.0")
	bContainer := systemd.ContainerName("repo-a", packages.DependencyName("parent", "b"), "1.0")
	wantCommands(t, rec, bContainer, []string{"point-at " + zContainer})

	for _, call := range rec.recorded() {
		if strings.Contains(call.command, "@dep_") {
			t.Fatalf("unresolved marker reached the container: %#v", call)
		}
	}
}

// The ordering edge on its own, at the level it is implemented: an injected
// command naming a sibling has to create the same graph edge a response naming
// one does.
func TestOrderDependenciesFollowsInjectedPostInstall(t *testing.T) {
	t.Parallel()

	t.Run("injected command orders its referent first", func(t *testing.T) {
		t.Parallel()

		// Alphabetically "b" precedes "z"; only the post_install reference can
		// reverse that.
		deps := map[string]packages.InputPackageDependency{
			"b": {Package: "beta", PostInstall: []string{"point-at @dep_z_host@"}},
			"z": {Package: "zeta"},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"z", "b"}) {
			t.Fatalf("order = %v, want [z b]", order)
		}
	})

	t.Run("a cycle through post_install is refused", func(t *testing.T) {
		t.Parallel()

		deps := map[string]packages.InputPackageDependency{
			"a": {Package: "alpha", PostInstall: []string{"@dep_b_host@"}},
			"b": {Package: "beta", PostInstall: []string{"@dep_a_host@"}},
		}
		if _, err := orderDependencies(deps); err == nil {
			t.Fatal("a dependency cycle through post_install was accepted; the install would substitute one of the markers against an empty env")
		}
	})
}

// GetContainerExecFunc must never hand runPostInstall a nil: the runner
// tolerates one, but a production box that silently stopped running post_install
// would look exactly like a box whose commands all succeeded.
func TestGetContainerExecFuncDefaultsToPodman(t *testing.T) {
	t.Parallel()

	s := &serverBase{}
	got := s.GetContainerExecFunc()
	if got == nil {
		t.Fatal("GetContainerExecFunc returned nil")
	}
	if reflect.ValueOf(got).Pointer() != reflect.ValueOf(PodmanContainerExec).Pointer() {
		t.Fatal("the default is not PodmanContainerExec")
	}
}

func TestGetContainerExecFuncHonorsTheOverride(t *testing.T) {
	t.Parallel()

	rec := &wiringExecRecorder{}
	s := &serverBase{ServerConfig: ServerConfig{ContainerExecFunc: rec.exec}}
	if _, err := s.GetContainerExecFunc()(context.Background(), "c", "cmd"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	got := rec.recorded()
	if len(got) != 1 || got[0].container != "c" || got[0].command != "cmd" {
		t.Fatalf("override was not used: %#v", got)
	}
}
