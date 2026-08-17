package packages

import (
	"errors"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v4"
)

// TestPostInstallYAMLRoundTrip pins that `post_install:` survives unmarshal →
// compile at both levels it is declared: the package's own top-level list and
// the list a parent injects into one of its dependencies. Without both halves
// arriving on the compiled Package, the install path has nothing to exec.
func TestPostInstallYAMLRoundTrip(t *testing.T) {
	src := `image: parent:latest
post_install:
  - "echo parent-one"
  - "echo parent-two"
dependencies:
  radarr:
    package: radarr
    post_install:
      - "echo dep-one"
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ip.PostInstall) != 2 {
		t.Fatalf("top-level post_install = %d entries, want 2", len(ip.PostInstall))
	}
	radarr, ok := ip.Dependencies["radarr"]
	if !ok {
		t.Fatal("radarr dep missing")
	}
	if len(radarr.PostInstall) != 1 || radarr.PostInstall[0] != "echo dep-one" {
		t.Fatalf("dep post_install = %#v, want [echo dep-one]", radarr.PostInstall)
	}

	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(compiled.PostInstall) != 2 || compiled.PostInstall[0] != "echo parent-one" {
		t.Fatalf("compiled post_install = %#v", compiled.PostInstall)
	}
	if got := compiled.Dependencies["radarr"].PostInstall; len(got) != 1 || got[0] != "echo dep-one" {
		t.Fatalf("compiled dep post_install = %#v", got)
	}
}

// TestPostInstallAbsentStaysNil keeps the zero value honest: a package with no
// post_install must compile to a nil slice, because the install path branches
// on len() to decide whether to wait for the container at all. A non-nil empty
// slice would still be len 0, but a stray default entry would make every
// install pay the readiness wait.
func TestPostInstallAbsentStaysNil(t *testing.T) {
	var ip InputPackage
	if err := yaml.Unmarshal([]byte("image: app:latest\n"), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.PostInstall != nil {
		t.Fatalf("PostInstall = %#v, want nil", compiled.PostInstall)
	}
}

// TestPostInstallSubstitutesQuestionResponses covers the compile-time half of
// substitution at both levels. An API key or hostname the operator answered is
// exactly what a wiring command needs, so `@marker@` has to resolve here.
func TestPostInstallSubstitutesQuestionResponses(t *testing.T) {
	src := `image: parent:latest
post_install:
  - "curl -H 'X-Api-Key: @apikey@' http://localhost/api"
dependencies:
  radarr:
    package: radarr
    post_install:
      - "curl --data 'name=@libname@' http://localhost:7878/api/v3/notification"
questions:
  apikey:
    query: "API key?"
  libname:
    query: "Library name?"
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{"apikey": "s3cr3t", "libname": "Movies"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(compiled.PostInstall[0], "X-Api-Key: s3cr3t") {
		t.Fatalf("top-level substitution failed: %q", compiled.PostInstall[0])
	}
	depCmd := compiled.Dependencies["radarr"].PostInstall[0]
	if !strings.Contains(depCmd, "name=Movies") {
		t.Fatalf("dep substitution failed: %q", depCmd)
	}
}

// TestPostInstallPreservesDepMarkers is the counterpart: @dep_KEY_host@ is NOT
// a question response and must survive compile untouched, because the
// systemcontroller resolves it at install time against the deps it just
// brought up. A compile-time collapse here would hand `sh -c` a hostname that
// does not exist yet, or none at all.
func TestPostInstallPreservesDepMarkers(t *testing.T) {
	src := `image: parent:latest
post_install:
  - "curl http://@dep_radarr_host@:@dep_radarr_port_http@/api/v3/system/status"
dependencies:
  radarr:
    package: radarr
    post_install:
      - "curl http://@dep_qbit_host@:8080/"
  qbit:
    package: qbittorrent
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(compiled.PostInstall[0], "@dep_radarr_host@") {
		t.Fatalf("top-level dep marker lost: %q", compiled.PostInstall[0])
	}
	if !strings.Contains(compiled.PostInstall[0], "@dep_radarr_port_http@") {
		t.Fatalf("top-level dep port marker lost: %q", compiled.PostInstall[0])
	}
	depCmd := compiled.Dependencies["radarr"].PostInstall[0]
	if !strings.Contains(depCmd, "@dep_qbit_host@") {
		t.Fatalf("dep-level dep marker lost: %q", depCmd)
	}
}

// TestPostInstallPreservesEscapedAt holds the `@@` escape open through
// compile. post_install has a runtime ApplyTemplates pass ahead of it
// (applyDepTemplatesSlice), and that pass is what collapses `@@` → `@`.
// Collapsing here as well would eat the literal `@` that an adjacent
// @dep_*@ marker needs to resolve next to — the same failure the Environment
// comment in Compile describes.
func TestPostInstallPreservesEscapedAt(t *testing.T) {
	src := `image: parent:latest
post_install:
  - "login user@@example.com"
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(compiled.PostInstall[0], "@@") {
		t.Fatalf("@@ collapsed at compile time: %q", compiled.PostInstall[0])
	}
}

// TestPostInstallTrimsWhitespace mirrors post_update: a YAML block scalar
// picks up trailing newlines that would otherwise reach `sh -c`.
func TestPostInstallTrimsWhitespace(t *testing.T) {
	ip := InputPackage{
		Image:       InputPackageImage{Type: ImageTypeOCI, URL: "app:latest"},
		PostInstall: []string{"  echo hi  "},
		Dependencies: map[string]InputPackageDependency{
			"d": {Package: "dep", PostInstall: []string{"\techo dep\t"}},
		},
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.PostInstall[0] != "echo hi" {
		t.Fatalf("top-level not trimmed: %q", compiled.PostInstall[0])
	}
	if got := compiled.Dependencies["d"].PostInstall[0]; got != "echo dep" {
		t.Fatalf("dep-level not trimmed: %q", got)
	}
}

// TestPostInstallRejectedForVMPackages: post_install execs into a container,
// and a VM package has none. Rejecting at compile is what keeps the install
// path's runtime guard a restatement rather than the only line of defense.
func TestPostInstallRejectedForVMPackages(t *testing.T) {
	src := `vm:
  image: https://example.com/disk.qcow2
  memory: 1gb
post_install:
  - "echo hi"
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	_, err := ip.Compile(Responses{})
	if !errors.Is(err, ErrPostInstallVMNotSupported) {
		t.Fatalf("compile error = %v, want ErrPostInstallVMNotSupported", err)
	}
}

// TestPostInstallRejectsEmptyCommand covers both levels. An empty entry is
// always an authoring mistake (a stray `- ""`, or a marker that resolved to
// nothing), and running it would silently succeed and hide the mistake.
func TestPostInstallRejectsEmptyCommand(t *testing.T) {
	t.Run("top level", func(t *testing.T) {
		ip := InputPackage{
			Image:       InputPackageImage{Type: ImageTypeOCI, URL: "app:latest"},
			PostInstall: []string{"echo ok", "   "},
		}
		_, err := ip.Compile(Responses{})
		if !errors.Is(err, ErrEmptyPostInstallCommand) {
			t.Fatalf("compile error = %v, want ErrEmptyPostInstallCommand", err)
		}
	})

	t.Run("dependency level", func(t *testing.T) {
		ip := InputPackage{
			Image: InputPackageImage{Type: ImageTypeOCI, URL: "app:latest"},
			Dependencies: map[string]InputPackageDependency{
				"d": {Package: "dep", PostInstall: []string{""}},
			},
		}
		_, err := ip.Compile(Responses{})
		if !errors.Is(err, ErrEmptyPostInstallCommand) {
			t.Fatalf("compile error = %v, want ErrEmptyPostInstallCommand", err)
		}
		if !strings.Contains(err.Error(), `dependency "d"`) {
			t.Fatalf("error does not name the dependency: %v", err)
		}
	})
}

// TestPostInstallRejectsControlCharsFromResponses is the one that matters.
// The YAML holds a bare `@marker@`, which carries no control character of its
// own and so passes the input-side check; the newline arrives with the
// operator's response and would otherwise reach `podman exec ... sh -c` free
// to append a second command. The sweep over the COMPILED package is what
// catches it, at both levels.
func TestPostInstallRejectsControlCharsFromResponses(t *testing.T) {
	t.Run("top level", func(t *testing.T) {
		src := `image: app:latest
post_install:
  - "echo @value@"
questions:
  value:
    query: "Value?"
`
		var ip InputPackage
		if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		_, err := ip.Compile(Responses{"value": "ok\nrm -rf /"})
		if err == nil {
			t.Fatal("compile accepted a newline injected through a response")
		}
		if !strings.Contains(err.Error(), "post_install[0]") {
			t.Fatalf("error does not name the field: %v", err)
		}
	})

	t.Run("dependency level", func(t *testing.T) {
		src := `image: app:latest
dependencies:
  d:
    package: dep
    post_install:
      - "echo @value@"
questions:
  value:
    query: "Value?"
`
		var ip InputPackage
		if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		_, err := ip.Compile(Responses{"value": "ok\nrm -rf /"})
		if err == nil {
			t.Fatal("compile accepted a newline injected through a dep response")
		}
		if !strings.Contains(err.Error(), "post_install[0]") {
			t.Fatalf("error does not name the field: %v", err)
		}
	})
}

// TestPostInstallCompileDoesNotAliasInput guards the copy in compiledDeps:
// mutating the compiled dep's command list must not reach back into the
// InputPackage the RepositoryRoot cache hands out, or one install would
// rewrite the commands every later install of the same package sees.
func TestPostInstallCompileDoesNotAliasInput(t *testing.T) {
	ip := InputPackage{
		Image: InputPackageImage{Type: ImageTypeOCI, URL: "app:latest"},
		Dependencies: map[string]InputPackageDependency{
			"d": {Package: "dep", PostInstall: []string{"echo original"}},
		},
		PostInstall: []string{"echo top"},
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	compiled.Dependencies["d"].PostInstall[0] = "echo mutated"
	compiled.PostInstall[0] = "echo mutated"

	if got := ip.Dependencies["d"].PostInstall[0]; got != "echo original" {
		t.Fatalf("dep input aliased by compile: %q", got)
	}
	if got := ip.PostInstall[0]; got != "echo top" {
		t.Fatalf("top-level input aliased by compile: %q", got)
	}
}
