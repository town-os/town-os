// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemd

import (
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestQuoteCommandArg(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty string becomes empty single-quote pair", "", "''"},
		{"simple token unchanged", "redis-server", "redis-server"},
		{"flag with equals unchanged", "--bind=0.0.0.0", "--bind=0.0.0.0"},
		{"arg with space is single-quoted", "hello world", "'hello world'"},
		{"arg with tab is single-quoted", "a\tb", "'a\tb'"},
		{"shell metachar && forces quoting", "a && b", "'a && b'"},
		{"dollar sign forces quoting", "echo $HOME", "'echo $HOME'"},
		{"pipe forces quoting", "foo | bar", "'foo | bar'"},
		{"semicolon forces quoting", "foo; bar", "'foo; bar'"},
		{"embedded single quote escaped POSIX-style", "it's", `'it'\''s'`},
		{"double quotes inside single-quoted arg preserved", `say "hi"`, `'say "hi"'`},
		{"chained command typical of sh -c", "python a && exec python b", "'python a && exec python b'"},
		{"json-array-like string forces quoting (brackets)", `["sh","-c"]`, `'["sh","-c"]'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := quoteCommandArg(tc.in)
			if got != tc.want {
				t.Fatalf("quoteCommandArg(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestGeneratePackageUnitsCommandWithSpaces exercises the regression that
// brought the whole .Dep / entrypoint / quoting cascade to light: a
// sh -c chained command was being written to the unit file as three raw
// whitespace-separated tokens, and systemd was tokenizing the third one
// into further argv pieces — so podman only got the first word of the
// actual shell command and exited 0 without ever running synapse.
func TestGeneratePackageUnitsCommandWithSpaces(t *testing.T) {
	t.Parallel()
	cmd := "python -m synapse.app.homeserver --generate-keys && exec python -m synapse.app.homeserver"
	cfg := PackageUnitConfig{
		RepoName: "test-repo",
		PkgName:  "matrix",
		Version:  "1.0",
		Image:    "docker.io/matrixdotorg/synapse:latest",
		Command:  []string{"sh", "-c", cmd},
	}

	svc := GeneratePackageUnits(cfg).Service.Content

	// Multi-word arg must be wrapped in single quotes in the unit file so
	// systemd's ExecStart tokenizer forwards it as one argv element.
	if !strings.Contains(svc, "'"+cmd+"'") {
		t.Fatalf("expected quoted chained command in unit, got:\n%s", svc)
	}
	// Simple tokens stay unquoted so existing units remain byte-stable.
	if strings.Contains(svc, "'sh'") || strings.Contains(svc, `'-c'`) {
		t.Fatalf("simple tokens must not be single-quoted, got:\n%s", svc)
	}
}

func TestGeneratePackageUnitsEntrypointOverride(t *testing.T) {
	t.Parallel()
	cfg := PackageUnitConfig{
		RepoName:   "test-repo",
		PkgName:    "matrix",
		Version:    "1.0",
		Image:      "docker.io/matrixdotorg/synapse:latest",
		Entrypoint: []string{"sh", "-c"},
		Command:    []string{"echo hi"},
	}

	svc := GeneratePackageUnits(cfg).Service.Content

	// --entrypoint must appear as a single argv element (JSON-encoded and
	// single-quoted) placed before the image name.
	const want = `--entrypoint='["sh","-c"]'`
	if !strings.Contains(svc, want) {
		t.Fatalf("expected %q in unit, got:\n%s", want, svc)
	}
	epIdx := strings.Index(svc, "--entrypoint=")
	imageIdx := strings.Index(svc, "docker.io/matrixdotorg/synapse:latest")
	if epIdx < 0 || imageIdx < 0 || epIdx > imageIdx {
		t.Fatalf("--entrypoint must appear before image name; unit:\n%s", svc)
	}
}

// TestGeneratePackageUnitsEntrypointOmittedWhenUnset guards against a
// regression where an empty Entrypoint would still emit --entrypoint=[]
// and effectively blank out the image's own ENTRYPOINT.
func TestGeneratePackageUnitsEntrypointOmittedWhenUnset(t *testing.T) {
	t.Parallel()
	cfg := PackageUnitConfig{
		RepoName: "test-repo",
		PkgName:  "nginx",
		Version:  "1.0",
		Image:    "docker.io/library/nginx:latest",
	}
	svc := GeneratePackageUnits(cfg).Service.Content
	if strings.Contains(svc, "--entrypoint") {
		t.Fatalf("--entrypoint must not be emitted when Entrypoint is nil:\n%s", svc)
	}
}

// TestGeneratePackageUnitsEnvWithSpaces is the regression for the
// "unknown flag: --lc-collate" failure mode: setting
// POSTGRES_INITDB_ARGS="--encoding=UTF8 --lc-collate=C --lc-ctype=C"
// in a package's environment used to render as
// `-e POSTGRES_INITDB_ARGS=--encoding=UTF8 --lc-collate=C --lc-ctype=C`,
// which systemd splits on whitespace. podman then treated `--lc-collate=C`
// as an unknown podman-run flag and the container never started.
func TestGeneratePackageUnitsEnvWithSpaces(t *testing.T) {
	t.Parallel()
	cfg := PackageUnitConfig{
		RepoName: "test-repo",
		PkgName:  "postgres",
		Version:  "1.0",
		Image:    "docker.io/library/postgres:latest",
		Environment: map[string]string{
			"POSTGRES_INITDB_ARGS": "--encoding=UTF8 --lc-collate=C --lc-ctype=C",
			"PG_PASSWORD":          "simple",
		},
	}
	svc := GeneratePackageUnits(cfg).Service.Content

	const wantQuoted = `-e 'POSTGRES_INITDB_ARGS=--encoding=UTF8 --lc-collate=C --lc-ctype=C'`
	if !strings.Contains(svc, wantQuoted) {
		t.Fatalf("expected %q in unit, got:\n%s", wantQuoted, svc)
	}
	// Simple values without whitespace must not be quoted, so existing
	// units stay byte-stable.
	if !strings.Contains(svc, "-e PG_PASSWORD=simple") {
		t.Fatalf("simple value must not be quoted; got:\n%s", svc)
	}
}

func TestGeneratePackageUnitsEntrypointSingleArg(t *testing.T) {
	t.Parallel()
	cfg := PackageUnitConfig{
		RepoName:   "test-repo",
		PkgName:    "busybox",
		Version:    "1.0",
		Image:      "docker.io/library/busybox:latest",
		Entrypoint: []string{"/bin/sh"},
		Volumes:    map[string]packages.PackageVolume{},
	}
	svc := GeneratePackageUnits(cfg).Service.Content
	if !strings.Contains(svc, `--entrypoint='["/bin/sh"]'`) {
		t.Fatalf("expected single-element JSON array for entrypoint, got:\n%s", svc)
	}
}
