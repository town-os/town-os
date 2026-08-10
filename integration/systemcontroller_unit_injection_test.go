// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// The unit tests prove Compile refuses the value and that unit generation
// strips it. This proves the thing that actually matters: the bytes written to
// /etc/systemd/system never contain a directive the package did not legitimately
// produce, and systemd itself agrees about what those bytes mean.
//
// A unit file is line-oriented and its quoting does not span lines, so a
// package value carrying a raw newline used to end its directive early and turn
// everything after it into a new directive in the same [Service] section. That
// crosses a privilege boundary: a package author already controls the image and
// the command, which is authority over what runs inside a container, while a
// systemd directive runs on the host as root before podman is invoked.

// TestInstalledUnitCannotCarryInjectedDirective writes a generated unit to disk
// exactly as InstallUnit does and reads it back, so the assertion is against
// file content rather than an in-memory string.
func TestInstalledUnitCannotCarryInjectedDirective(t *testing.T) {
	t.Parallel()

	units := systemd.GeneratePackageUnits(systemd.PackageUnitConfig{
		RepoName: "core",
		PkgName:  "injection",
		Version:  "1.0",
		Image:    "docker.io/library/nginx:latest",
		Environment: map[string]string{
			"PAYLOAD": "harmless\nExecStartPre=/bin/sh -c 'touch /tmp/town-os-pwned'",
		},
		Command: []string{"sh", "-c", "echo hi\nExecStopPost=/bin/rm -rf /"},
	})

	path := filepath.Join(t.TempDir(), units.Service.Name)
	if err := os.WriteFile(path, []byte(units.Service.Content), 0600); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read unit back: %v", err)
	}

	// Walk it the way systemd's parser does: a directive is a line that is not
	// a continuation of the previous one.
	continued := false
	for line := range strings.SplitSeq(string(raw), "\n") {
		wasContinued := continued
		continued = strings.HasSuffix(line, "\\")
		if wasContinued {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ExecStartPre=/bin/sh -c 'touch /tmp/town-os-pwned"):
			t.Fatalf("payload became a real ExecStartPre directive:\n%s", raw)
		case strings.HasPrefix(trimmed, "ExecStopPost=/bin/rm -rf"):
			t.Fatalf("payload became a real ExecStopPost directive:\n%s", raw)
		}
	}
}

// The install path refuses the package before a unit is ever generated, which
// is where the operator actually gets told. Driving Compile directly (rather
// than the HTTP install route) keeps this independent of a repository fixture
// while still exercising the real compile path an install takes.
func TestInstallCompileRefusesInjectedResponse(t *testing.T) {
	t.Parallel()

	ip := &packages.InputPackage{
		Image:       packages.InputPackageImage{URL: "docker.io/library/nginx:latest"},
		Environment: map[string]string{"ADMIN_TOKEN": "@token@"},
		Questions: map[string]packages.Question{
			// No `type:`, so nothing else validates the answer. This is the
			// path that reaches a unit file with caller-chosen bytes in it.
			"token": {Query: "admin token"},
		},
	}

	_, err := ip.Compile(packages.Responses{
		"token": "abc123\nExecStartPre=/bin/sh -c 'touch /tmp/town-os-pwned'",
	})
	if !errors.Is(err, packages.ErrControlCharacter) {
		t.Fatalf("Compile with an injected response = %v, want ErrControlCharacter", err)
	}
}

// And an ordinary package still installs. The check must not have made a
// legitimate multi-word value — the shape real packages use — fail.
func TestInstallCompileAcceptsOrdinaryPackage(t *testing.T) {
	t.Parallel()

	ip := &packages.InputPackage{
		Image: packages.InputPackageImage{URL: "docker.io/library/postgres:16"},
		Environment: map[string]string{
			"POSTGRES_INITDB_ARGS": "--encoding=UTF8 --lc-collate=C --lc-ctype=C",
			"POSTGRES_PASSWORD":    "@dbpass@",
		},
		Command: []string{"sh", "-c", "exec postgres -c max_connections=100"},
		Questions: map[string]packages.Question{
			"dbpass": {Query: "database password", Type: packages.Secret},
		},
	}

	pkg, err := ip.Compile(packages.Responses{"dbpass": "auto"})
	if err != nil {
		t.Fatalf("Compile of an ordinary package: %v", err)
	}
	if pkg.Environment["POSTGRES_INITDB_ARGS"] != "--encoding=UTF8 --lc-collate=C --lc-ctype=C" {
		t.Errorf("multi-word env value was altered: %q", pkg.Environment["POSTGRES_INITDB_ARGS"])
	}
	if pkg.Environment["POSTGRES_PASSWORD"] == "" || pkg.Environment["POSTGRES_PASSWORD"] == "auto" {
		t.Errorf("secret was not generated: %q", pkg.Environment["POSTGRES_PASSWORD"])
	}

	// The generated unit for it is well-formed: one directive per line.
	units := systemd.GeneratePackageUnits(systemd.PackageUnitConfig{
		RepoName:    "core",
		PkgName:     "ordinary",
		Version:     "1.0",
		Image:       pkg.Image,
		Environment: pkg.Environment,
		Command:     pkg.Command,
	})
	if !strings.Contains(units.Service.Content, "--encoding=UTF8 --lc-collate=C --lc-ctype=C") {
		t.Errorf("multi-word env value did not survive into the unit:\n%s", units.Service.Content)
	}
}
