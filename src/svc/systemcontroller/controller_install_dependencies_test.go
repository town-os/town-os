// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func TestExtractDepKeyRefs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"no refs", "static-value-123", nil},
		{"single host", "@dep_db_host@", []string{"db"}},
		{"single port", "@dep_db_port_5432@", []string{"db"}},
		{"url pattern", "postgres://user:pass@@@dep_db_host@:@dep_db_port_5432@/mydb", []string{"db", "db"}},
		{"multiple keys", "@dep_db_host@ @dep_cache_host@", []string{"db", "cache"}},
		{"underscore key", "@dep_my_service_host@", []string{"my_service"}},
		{"underscore key with port", "@dep_my_service_port_8080@", []string{"my_service"}},
		{"numeric key", "@dep_svc1_host@", []string{"svc1"}},
		{"incomplete", "@dep_db@", nil},
		{"non-dep template", "@PACKAGE_DNS@", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDepKeyRefs(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractDepKeyRefs(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestOrderDependencies(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		order, err := orderDependencies(map[string]packages.InputPackageDependency{})
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if len(order) != 0 {
			t.Fatalf("expected empty order, got %v", order)
		}
	})

	t.Run("no refs sorted alphabetically", func(t *testing.T) {
		deps := map[string]packages.InputPackageDependency{
			"zeta":  {Package: "zeta"},
			"alpha": {Package: "alpha"},
			"mu":    {Package: "mu"},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		want := []string{"alpha", "mu", "zeta"}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
	})

	t.Run("simple reference orders predecessor first", func(t *testing.T) {
		// b references a via @dep_a_host@; a must come first.
		deps := map[string]packages.InputPackageDependency{
			"b": {
				Package: "svcb",
				Responses: map[string]string{
					"upstream": "@dep_a_host@",
				},
			},
			"a": {Package: "svca"},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"a", "b"}) {
			t.Fatalf("order = %v, want [a b]", order)
		}
	})

	t.Run("jitsi pattern", func(t *testing.T) {
		// jicofo and jvb both reference prosody; prosody must come first.
		// Among unconstrained siblings (jicofo and jvb), order is alphabetical.
		deps := map[string]packages.InputPackageDependency{
			"prosody": {Package: "prosody"},
			"jicofo": {
				Package: "jicofo",
				Responses: map[string]string{
					"xmpphost": "@dep_prosody_host@",
					"xmppport": "@dep_prosody_port_5222@",
				},
			},
			"jvb": {
				Package: "jvb",
				Responses: map[string]string{
					"xmpphost": "@dep_prosody_host@",
					"xmppport": "@dep_prosody_port_5222@",
				},
			},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		// prosody is first; jicofo and jvb follow in alphabetical order.
		want := []string{"prosody", "jicofo", "jvb"}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("order = %v, want %v", order, want)
		}
	})

	t.Run("chain", func(t *testing.T) {
		// a -> b -> c: c must install first, then b, then a.
		deps := map[string]packages.InputPackageDependency{
			"a": {
				Package:   "pa",
				Responses: map[string]string{"x": "@dep_b_host@"},
			},
			"b": {
				Package:   "pb",
				Responses: map[string]string{"x": "@dep_c_host@"},
			},
			"c": {Package: "pc"},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"c", "b", "a"}) {
			t.Fatalf("order = %v, want [c b a]", order)
		}
	})

	t.Run("diamond", func(t *testing.T) {
		// d references both b and c; b and c each reference a.
		// Expected: a first, then b/c (alphabetical), then d.
		deps := map[string]packages.InputPackageDependency{
			"a": {Package: "pa"},
			"b": {
				Package:   "pb",
				Responses: map[string]string{"x": "@dep_a_host@"},
			},
			"c": {
				Package:   "pc",
				Responses: map[string]string{"x": "@dep_a_host@"},
			},
			"d": {
				Package: "pd",
				Responses: map[string]string{
					"x": "@dep_b_host@",
					"y": "@dep_c_host@",
				},
			},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		// Validate constraints rather than exact order for the middle.
		pos := map[string]int{}
		for i, k := range order {
			pos[k] = i
		}
		if pos["a"] > pos["b"] || pos["a"] > pos["c"] {
			t.Fatalf("a must precede b and c, got %v", order)
		}
		if pos["b"] > pos["d"] || pos["c"] > pos["d"] {
			t.Fatalf("b and c must precede d, got %v", order)
		}
		if len(order) != 4 {
			t.Fatalf("expected 4 keys, got %v", order)
		}
	})

	t.Run("cycle is rejected", func(t *testing.T) {
		deps := map[string]packages.InputPackageDependency{
			"a": {
				Package:   "pa",
				Responses: map[string]string{"x": "@dep_b_host@"},
			},
			"b": {
				Package:   "pb",
				Responses: map[string]string{"x": "@dep_a_host@"},
			},
		}
		_, err := orderDependencies(deps)
		if err == nil {
			t.Fatalf("expected cycle error, got nil")
		}
		if !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("expected cycle error, got %v", err)
		}
	})

	t.Run("self-reference is ignored", func(t *testing.T) {
		// A dep referencing itself via its own key must not create a
		// false cycle. It is harmless and ignored.
		deps := map[string]packages.InputPackageDependency{
			"a": {
				Package:   "pa",
				Responses: map[string]string{"x": "@dep_a_host@"},
			},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"a"}) {
			t.Fatalf("order = %v, want [a]", order)
		}
	})

	t.Run("unknown ref is ignored", func(t *testing.T) {
		// References to names that are not sibling deps are treated as
		// external template vars and ignored for ordering.
		deps := map[string]packages.InputPackageDependency{
			"a": {
				Package:   "pa",
				Responses: map[string]string{"x": "@dep_external_host@"},
			},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"a"}) {
			t.Fatalf("order = %v, want [a]", order)
		}
	})

	t.Run("jitsi-like with typed ports does not error", func(t *testing.T) {
		// Regression check: before orderDependencies existed, a sibling dep
		// referencing another via @dep_KEY_port_N@ in a port-typed Response
		// would fail compile with ErrInvalidResponseType because the literal
		// placeholder is not a valid port. orderDependencies plus the
		// pre-compile template substitution in installDependencies fixes
		// this; here we only check that the topo sort itself accepts the
		// shape without error.
		deps := map[string]packages.InputPackageDependency{
			"prosody": {Package: "prosody"},
			"jvb": {
				Package: "jvb",
				Responses: map[string]string{
					"xmpphost": "@dep_prosody_host@",
					"xmppport": "@dep_prosody_port_5222@",
				},
			},
		}
		order, err := orderDependencies(deps)
		if err != nil {
			t.Fatalf("orderDependencies: %v", err)
		}
		if !reflect.DeepEqual(order, []string{"prosody", "jvb"}) {
			t.Fatalf("order = %v, want [prosody jvb]", order)
		}
	})

	t.Run("stable across runs", func(t *testing.T) {
		// Map iteration order in Go is randomized; the topological sort
		// must still produce a deterministic result on repeated calls.
		deps := map[string]packages.InputPackageDependency{
			"one":   {Package: "p1"},
			"two":   {Package: "p2"},
			"three": {Package: "p3"},
			"four":  {Package: "p4"},
		}
		var prev []string
		for range 20 {
			order, err := orderDependencies(deps)
			if err != nil {
				t.Fatalf("orderDependencies: %v", err)
			}
			if !slices.IsSorted(order) {
				t.Fatalf("expected deterministic sorted order, got %v", order)
			}
			if prev != nil && !reflect.DeepEqual(order, prev) {
				t.Fatalf("unstable order: %v vs %v", prev, order)
			}
			prev = order
		}
	})
}

// TestHTTPInstallSiblingDepTypedPortRef is the end-to-end regression for the
// jitsi install failure. A parent package declares two dependencies; the
// second's Responses feed a *port-typed* question via @dep_FIRST_port_N@.
// Without orderDependencies + applyDepTemplates, the second dep's Compile
// runs Output validation on a literal "@dep_up_port_9000@" and fails with
// ErrInvalidResponseType, leaving the parent half-installed and the
// install handler silently surfacing the error only over SSE.
func TestHTTPInstallSiblingDepTypedPortRef(t *testing.T) {
	mockStorage := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "repo-a", URL: *u}}

	// Leaf dep — publishes container port 9000.
	upYAML := `image: example/up:1.0
environment: {}
network:
  external:
    "@port@": "9000"
  internal: {}
volumes: {}
questions:
  port:
    query: "external port"
    type: port
`
	// Referencing dep — has a port-typed question whose response in the
	// parent YAML is @dep_up_port_9000@. Without the fix, Compile runs
	// Output validation on that literal and fails.
	downYAML := `image: example/down:1.0
environment:
  UP_HOST: "@uphost@"
  UP_PORT: "@upport@"
network:
  external:
    "@port@": "5432"
  internal: {}
volumes: {}
questions:
  port:
    query: "external port"
    type: port
  upport:
    query: "upstream port"
    type: port
  uphost:
    query: "upstream host"
`
	// Parent — declares the two deps. The "down" dep Responses reference
	// the "up" dep's host and container port. Map ordering in Go is
	// random; the fix must still route the install correctly.
	parentYAML := `image: example/parent:1.0
environment: {}
network:
  external:
    "@port@": "80"
  internal: {}
volumes: {}
questions:
  port:
    query: "external port"
    type: port
dependencies:
  up:
    package: up
  down:
    package: down
    responses:
      uphost: "@dep_up_host@"
      upport: "@dep_up_port_9000@"
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "up", "1.0", upYAML)
	writeTestPackage(t, rr.BaseDir, "repo-a", "down", "1.0", downYAML)
	writeTestPackage(t, rr.BaseDir, "repo-a", "parent", "1.0", parentYAML)

	inst := packages.InitMockInstallManager()
	ts := InitTestServer(ServerConfig{Storage: mockStorage, RepositoryRoot: rr, Installer: inst})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	// Install parent — "port" for parent auto-generates.
	if err := c.InstallPackage(context.TODO(), "parent", "1.0", packages.Responses{"port": "auto"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage parent: %v", err)
	}

	// The parent must show up in the user-facing installed list; its
	// dependency sub-packages are intentionally hidden there (they are
	// managed via the parent's lifecycle) but they still have to exist
	// in the underlying installer, which the raw inst.ListInstalled()
	// call verifies below.
	pkgs, err := c.ListInstalled(context.TODO(), ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	gotSet := map[string]bool{}
	for _, e := range pkgs.Entries {
		gotSet[e] = true
	}
	if !gotSet["repo-a/parent@1.0"] {
		t.Fatalf("missing parent entry in %v", pkgs.Entries)
	}
	for _, hidden := range []string{"repo-a/parent--dep--up@1.0", "repo-a/parent--dep--down@1.0"} {
		if gotSet[hidden] {
			t.Fatalf("dependency sub-package %q should be hidden from ListInstalled, got %v", hidden, pkgs.Entries)
		}
	}

	rawInstalled, err := inst.ListInstalled()
	if err != nil {
		t.Fatalf("inst.ListInstalled: %v", err)
	}
	rawSet := map[string]bool{}
	for _, e := range rawInstalled {
		rawSet[e] = true
	}
	for _, want := range []string{"repo-a/parent@1.0", "repo-a/parent--dep--up@1.0", "repo-a/parent--dep--down@1.0"} {
		if !rawSet[want] {
			t.Fatalf("missing raw install record %q in %v", want, rawInstalled)
		}
	}

	// The referencing dep must have been installed with RESOLVED values in
	// its persisted Responses, not the literal placeholders.
	downResp, err := inst.GetResponses("repo-a", "parent--dep--down", "1.0")
	if err != nil {
		t.Fatalf("GetResponses down: %v", err)
	}
	if downResp["upport"] != "9000" {
		t.Fatalf("down.upport = %q, want %q (should have been resolved from @dep_up_port_9000@)", downResp["upport"], "9000")
	}
	wantHost := systemd.ContainerName("repo-a", "parent--dep--up", "1.0")
	if downResp["uphost"] != wantHost {
		t.Fatalf("down.uphost = %q, want %q", downResp["uphost"], wantHost)
	}

	// The install order must have been: up before down. The mock install
	// manager records every Install call in sequence; scan for the two
	// dep install calls and confirm up precedes down.
	upIdx, downIdx := -1, -1
	for i, call := range inst.GetCalls() {
		if call.Method != "Install" {
			continue
		}
		name, ok := call.Args[1].(string)
		if !ok {
			t.Fatalf("type assertion on Install arg 1 failed")
		}
		if name == "parent--dep--up" && upIdx < 0 {
			upIdx = i
		}
		if name == "parent--dep--down" && downIdx < 0 {
			downIdx = i
		}
	}
	if upIdx < 0 || downIdx < 0 {
		t.Fatalf("missing dep Install calls: up=%d down=%d", upIdx, downIdx)
	}
	if upIdx > downIdx {
		t.Fatalf("expected 'up' installed before 'down', got up=%d down=%d", upIdx, downIdx)
	}
}
