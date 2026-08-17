package packages

import (
	"errors"
	"strings"
	"testing"

	yaml "go.yaml.in/yaml/v4"
)

func TestParseExportedVolumeRefRoundTrip(t *testing.T) {
	t.Parallel()

	ref, err := ParseExportedVolumeRef("default/jellyfin/media")
	if err != nil {
		t.Fatalf("ParseExportedVolumeRef: %v", err)
	}
	if ref.Repo != "default" || ref.Package != "jellyfin" || ref.Volume != "media" {
		t.Fatalf("parsed = %+v", ref)
	}
	if got := ref.String(); got != "default/jellyfin/media" {
		t.Fatalf("String() = %q", got)
	}
}

// TestParseExportedVolumeRefRejectsTraversal is the security case. The three
// segments are joined onto the btrfs base to build a bind-mount source, and
// the value arrives from any authenticated account's install request — so a
// segment that climbs, anchors, or empties has to be refused by the grammar
// rather than caught downstream.
func TestParseExportedVolumeRefRejectsTraversal(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{
		"",
		"default",
		"default/jellyfin",
		"default/jellyfin/media/extra",
		"../gfeh/home",
		"default/../../etc/passwd",
		"default/jellyfin/..",
		"default/jellyfin/.",
		"/default/jellyfin/media",
		"default//media",
		"default/jellyfin/",
		"default/jellyfin/med ia",
		"default/jellyfin/media\x00",
	} {
		if _, err := ParseExportedVolumeRef(bad); err == nil {
			t.Errorf("ParseExportedVolumeRef(%q) accepted a malformed reference", bad)
		}
	}
}

// TestParseExportedVolumeRefRejectsDependencyName: a dependency's storage is
// nested under its parent, and a dep is internal to that parent by design.
// Letting one be referenced would hand every package on the box a handle on
// somebody else's private sub-package.
func TestParseExportedVolumeRefRejectsDependencyName(t *testing.T) {
	t.Parallel()

	_, err := ParseExportedVolumeRef("default/media" + DependencySeparator + "radarr/movies")
	if err == nil {
		t.Fatal("a dependency effective name was accepted as a reference")
	}
	if !errors.Is(err, ErrInvalidExportedVolumeRef) {
		t.Fatalf("err = %v, want ErrInvalidExportedVolumeRef", err)
	}
}

func TestVolumeExportedYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	src := `image: jellyfin/jellyfin
volumes:
  media:
    mountpoint: /media
    exported: true
  config:
    mountpoint: /config
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !ip.Volumes["media"].Exported {
		t.Fatal("media.exported did not survive unmarshal")
	}
	if ip.Volumes["config"].Exported {
		t.Fatal("config.exported defaulted true")
	}

	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !compiled.Volumes["media"].Exported {
		t.Fatal("media.exported lost during compile")
	}
	if compiled.Volumes["config"].Exported {
		t.Fatal("config.exported flipped during compile")
	}
}

// TestExportedIsIndependentOfShareable holds the two flags apart. Shareable
// grants one named parent in the same dependency tree; exported grants every
// package on the box, including ones not installed yet. Collapsing them would
// turn every existing `shareable: true` volume into a box-wide offer.
func TestExportedIsIndependentOfShareable(t *testing.T) {
	t.Parallel()

	src := `image: app:latest
volumes:
  a:
    mountpoint: /a
    shareable: true
  b:
    mountpoint: /b
    exported: true
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if compiled.Volumes["a"].Exported {
		t.Error("shareable leaked into exported")
	}
	if compiled.Volumes["b"].Shareable {
		t.Error("exported leaked into shareable")
	}
}

func TestAttachYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	src := `image: lscr.io/linuxserver/radarr
questions:
  library:
    query: "Media library"
    type: shared_volume
attach:
  library:
    volume: "@library@"
    subpath: movies
    path: /library/movies
    uid: 1000
    gid: 1000
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	att, ok := ip.Attach["library"]
	if !ok {
		t.Fatal("attach entry missing")
	}
	if att.Subpath != "movies" || att.Path != "/library/movies" {
		t.Fatalf("attach = %+v", att)
	}
	// Writable is the default, the opposite of expose:. The whole point of an
	// attachment is filing content into somebody else's library.
	if att.AttachReadOnly() {
		t.Error("attach readonly defaulted true")
	}
	if att.UID == nil || *att.UID != 1000 || att.GID == nil || *att.GID != 1000 {
		t.Fatalf("uid/gid did not survive unmarshal: %+v", att)
	}

	compiled, err := ip.Compile(Responses{"library": "default/jellyfin/media"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, ok := compiled.Attach["library"]
	if !ok {
		t.Fatal("attach entry lost during compile")
	}
	if got.Volume != "default/jellyfin/media" {
		t.Fatalf("attach.volume = %q, want the substituted reference", got.Volume)
	}
}

// TestAttachUnselectedIsDropped is what makes an optional attachment
// expressible: an optional shared_volume question left blank compiles to the
// empty string, and an attach with nothing to attach to must vanish rather
// than resolve to a mount whose source is the btrfs root.
func TestAttachUnselectedIsDropped(t *testing.T) {
	t.Parallel()

	src := `image: app:latest
questions:
  library:
    query: "Media library"
    type: shared_volume
    optional: true
attach:
  library:
    volume: "@library@"
    path: /library
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	compiled, err := ip.Compile(Responses{"library": ""})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(compiled.Attach) != 0 {
		t.Fatalf("unselected attach survived compile: %#v", compiled.Attach)
	}
}

func TestAttachRejectedForVMPackages(t *testing.T) {
	t.Parallel()

	src := `vm:
  image: https://example.com/disk.qcow2
  memory: 1gb
attach:
  lib:
    volume: default/jellyfin/media
    path: /library
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := ip.Compile(Responses{}); !errors.Is(err, ErrAttachVMNotSupported) {
		t.Fatalf("compile error = %v, want ErrAttachVMNotSupported", err)
	}
}

func TestValidateAttachRejectsBadEntries(t *testing.T) {
	t.Parallel()

	cases := map[string]InputPackageAttach{
		"empty volume":       {Volume: "", Path: "/library"},
		"malformed ref":      {Volume: "not-a-reference", Path: "/library"},
		"relative path":      {Volume: "default/jellyfin/media", Path: "library"},
		"traversing path":    {Volume: "default/jellyfin/media", Path: "/library/../etc"},
		"empty path":         {Volume: "default/jellyfin/media", Path: ""},
		"absolute subpath":   {Volume: "default/jellyfin/media", Path: "/library", Subpath: "/etc"},
		"traversing subpath": {Volume: "default/jellyfin/media", Path: "/library", Subpath: "movies/../../.."},
	}
	for name, att := range cases {
		if err := ValidateAttach("lib", att); err == nil {
			t.Errorf("%s: ValidateAttach accepted %+v", name, att)
		}
	}
}

// TestValidateAttachAllowsTemplatedVolume: the common form is a bare
// `@question@` that holds no reference until an operator answers, so the shape
// check has to stand down for it and let the resolver do the real work.
func TestValidateAttachAllowsTemplatedVolume(t *testing.T) {
	t.Parallel()

	if err := ValidateAttach("lib", InputPackageAttach{Volume: "@library@", Path: "/library"}); err != nil {
		t.Fatalf("ValidateAttach rejected a templated reference: %v", err)
	}
}

// TestAttachRejectsControlCharsFromResponses: the attach path and subpath both
// reach a podman -v flag in a systemd ExecStart line, and both can carry a
// response. The YAML holds a bare marker that passes the input-side check; the
// newline arrives with the answer.
func TestAttachRejectsControlCharsFromResponses(t *testing.T) {
	t.Parallel()

	src := `image: app:latest
questions:
  dir:
    query: "Directory?"
attach:
  lib:
    volume: default/jellyfin/media
    path: /library/@dir@
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	_, err := ip.Compile(Responses{"dir": "movies\nrm -rf /"})
	if err == nil {
		t.Fatal("compile accepted a newline injected through an attach path")
	}
	if !strings.Contains(err.Error(), "attach") {
		t.Fatalf("error does not name the field: %v", err)
	}
}

func TestSharedVolumeQuestionOutput(t *testing.T) {
	t.Parallel()

	got, err := SharedVolume.Output("default/jellyfin/media")
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	if got != "default/jellyfin/media" {
		t.Fatalf("Output = %q", got)
	}
	if _, err := SharedVolume.Output("garbage"); err == nil {
		t.Fatal("Output accepted a malformed reference")
	}
	if _, err := SharedVolume.Output("../gfeh/home"); err == nil {
		t.Fatal("Output accepted a traversing reference")
	}
}

// TestSharedVolumeIsNeverAutoGenerated documents the deliberate omission: the
// auto-generate switch covers port, hostname, secret, and boolean. There is no
// sensible default for "whose library should this write into", and guessing
// one would file somebody's downloads into the wrong place silently. A blank
// required answer must fail instead.
func TestSharedVolumeRequiredBlankFails(t *testing.T) {
	t.Parallel()

	src := `image: app:latest
questions:
  library:
    query: "Media library"
    type: shared_volume
attach:
  lib:
    volume: "@library@"
    path: /library
`
	var ip InputPackage
	if err := yaml.Unmarshal([]byte(src), &ip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, err := ip.Compile(Responses{"library": ""}); err == nil {
		t.Fatal("compile accepted a blank answer to a required shared_volume question")
	}
}
