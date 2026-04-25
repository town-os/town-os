package packages

import (
	"errors"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v4"
)

// TestVolumeShareableYAMLRoundTrip pins down that the `shareable: true`
// flag survives unmarshal → compile and lands on PackageVolume. Without
// this, the parent's Expose / sibling's Consume validators have no flag
// to check against and silently allow non-opt-in volumes through.
func TestVolumeShareableYAMLRoundTrip(t *testing.T) {
	src := `image: myapp:latest
volumes:
  movies:
    mountpoint: /movies
    shareable: true
  config:
    mountpoint: /config
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ip.Volumes["movies"].Shareable {
		t.Fatal("movies.shareable did not survive unmarshal")
	}
	if ip.Volumes["config"].Shareable {
		t.Fatal("config.shareable defaulted true (should be false)")
	}

	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.Volumes["movies"].Shareable {
		t.Fatal("movies.shareable lost during compile")
	}
	if compiled.Volumes["config"].Shareable {
		t.Fatal("config.shareable flipped during compile")
	}
}

// TestDependencyExposeConsumeYAMLRoundTrip pins down that expose: and
// consume: blocks survive unmarshal and compile, including the readonly
// pointer-vs-bool behaviour (absent → ExposeReadOnly default true,
// ConsumeReadOnly default false).
func TestDependencyExposeConsumeYAMLRoundTrip(t *testing.T) {
	src := `image: parent:latest
dependencies:
  qbittorrent:
    package: qbittorrent
  radarr:
    package: radarr
    expose:
      movies:
        path: /data/movies
        readonly: true
    consume:
      - from: qbittorrent
        volume: downloads
        path: /downloads
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	radarr, ok := ip.Dependencies["radarr"]
	if !ok {
		t.Fatal("radarr dep missing")
	}
	mov, ok := radarr.Expose["movies"]
	if !ok {
		t.Fatal("expose.movies missing")
	}
	if mov.Path != "/data/movies" {
		t.Errorf("expose.movies.path = %q, want /data/movies", mov.Path)
	}
	if !mov.ExposeReadOnly() {
		t.Error("expose.movies.readonly should be true (explicit)")
	}

	if len(radarr.Consume) != 1 {
		t.Fatalf("consume entries = %d, want 1", len(radarr.Consume))
	}
	c := radarr.Consume[0]
	if c.From != "qbittorrent" || c.Volume != "downloads" || c.Path != "/downloads" {
		t.Errorf("consume[0] = %+v, want qbittorrent/downloads → /downloads", c)
	}
	// Default readonly: true for expose, false for consume — inverted by design.
	if c.ConsumeReadOnly() {
		t.Error("consume.readonly should default false")
	}

	// Compile preserves both blocks.
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, ok := compiled.Dependencies["radarr"].Expose["movies"]; !ok {
		t.Error("expose lost during compile")
	}
	if len(compiled.Dependencies["radarr"].Consume) != 1 {
		t.Error("consume lost during compile")
	}
}

// TestExposeReadOnlyDefaultsTrue covers the pointer-defaults wiring
// directly: a YAML expose entry without a readonly key must yield
// ExposeReadOnly() == true. This is the parent-friendly default
// (Plex reads Radarr's /movies, never writes).
func TestExposeReadOnlyDefaultsTrue(t *testing.T) {
	src := `image: parent:latest
dependencies:
  dep:
    package: dep
    expose:
      v:
        path: /v
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	exp := ip.Dependencies["dep"].Expose["v"]
	if exp.ReadOnly != nil {
		t.Errorf("readonly should be nil when omitted, got %v", *exp.ReadOnly)
	}
	if !exp.ExposeReadOnly() {
		t.Error("ExposeReadOnly() should default to true")
	}
}

// TestSharedMountValidationRejects checks every validation gate at
// compile time. If any of these slip through, the install path would
// fail later with a less-actionable error or — worse — silently emit
// a bogus -v flag.
func TestSharedMountValidationRejects(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		errSub  string
		wantErr error
	}{
		{
			name: "expose path must be absolute",
			yaml: `image: parent:latest
dependencies:
  d:
    package: d
    expose:
      v:
        path: relative/path
`,
			errSub:  "must start with /",
			wantErr: ErrInvalidSharedMount,
		},
		{
			name: "expose path traversal rejected",
			yaml: `image: parent:latest
dependencies:
  d:
    package: d
    expose:
      v:
        path: /a/../b
`,
			errSub:  "directory traversal",
			wantErr: ErrInvalidSharedMount,
		},
		{
			name: "consume from unknown sibling rejected",
			yaml: `image: parent:latest
dependencies:
  d:
    package: d
    consume:
      - from: ghost
        volume: data
        path: /data
`,
			errSub:  "not a sibling dep key",
			wantErr: ErrInvalidSharedMount,
		},
		{
			name: "consume self-reference rejected",
			yaml: `image: parent:latest
dependencies:
  d:
    package: d
    consume:
      - from: d
        volume: data
        path: /data
`,
			errSub:  "cannot consume from itself",
			wantErr: ErrInvalidSharedMount,
		},
		{
			name: "duplicate consume path rejected",
			yaml: `image: parent:latest
dependencies:
  a:
    package: a
  d:
    package: d
    consume:
      - from: a
        volume: x
        path: /shared
      - from: a
        volume: y
        path: /shared
`,
			errSub:  "duplicate path",
			wantErr: ErrInvalidSharedMount,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ip InputPackage
			if err := yaml.Unmarshal([]byte(tc.yaml), &ip); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := ip.Validate()
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error chain missing %v, got %v", tc.wantErr, err)
			}
			if tc.errSub != "" && !strings.Contains(err.Error(), tc.errSub) {
				t.Errorf("error %q missing substring %q", err.Error(), tc.errSub)
			}
		})
	}
}

// TestExposePathTemplateSubstitution proves a parent's expose.path
// participates in @question@ substitution exactly like a regular volume
// mountpoint. Without this, packages with parameterized paths
// (`path: /data/@library@`) would render literal `@library@` into the
// systemd unit and the bind-mount target would not exist.
func TestExposePathTemplateSubstitution(t *testing.T) {
	src := `image: parent:latest
dependencies:
  d:
    package: d
    expose:
      v:
        path: /data/@library@
questions:
  library:
    query: lib name
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{"library": "movies"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if got := compiled.Dependencies["d"].Expose["v"].Path; got != "/data/movies" {
		t.Errorf("expose path = %q, want /data/movies", got)
	}
}
