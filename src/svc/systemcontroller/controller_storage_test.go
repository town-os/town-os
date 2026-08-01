// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

func TestParseInstalledPathCases(t *testing.T) {
	tests := []struct {
		label         string
		input         string
		ok            bool
		state         string
		repo          string
		effectiveName string
		prettyName    string
		version       string
		volName       string
	}{
		{
			label:         "standalone parent with volume",
			input:         "installed/core/nginx/1.0/data",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "nginx",
			prettyName:    "nginx",
			version:       "1.0",
			volName:       "data",
		},
		{
			label:         "standalone parent no volume",
			input:         "installed/core/nginx/1.0",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "nginx",
			prettyName:    "nginx",
			version:       "1.0",
			volName:       "",
		},
		{
			label:         "one-level dep",
			input:         "installed/core/gitea/subpackages/postgres/15.0/pgdata",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "gitea--dep--postgres",
			prettyName:    "gitea/postgres",
			version:       "15.0",
			volName:       "pgdata",
		},
		{
			label:         "two-level dep",
			input:         "installed/core/jitsi/subpackages/prosody/subpackages/metrics/0.3/data",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "jitsi--dep--prosody--dep--metrics",
			prettyName:    "jitsi/prosody/metrics",
			version:       "0.3",
			volName:       "data",
		},
		{
			label:         "uninstalled mirror",
			input:         "uninstalled/core/gitea/subpackages/postgres/15.0/pgdata",
			ok:            true,
			state:         "uninstalled",
			repo:          "core",
			effectiveName: "gitea--dep--postgres",
			prettyName:    "gitea/postgres",
			version:       "15.0",
			volName:       "pgdata",
		},
		{
			label:         "volName has nested slashes (legacy deep path)",
			input:         "installed/myrepo/app/2.5/logs/sub",
			ok:            true,
			state:         "installed",
			repo:          "myrepo",
			effectiveName: "app",
			prettyName:    "app",
			version:       "2.5",
			volName:       "logs/sub",
		},
		{
			// Bare package dir surfaces with empty version/volName so
			// the filesystem lister still shows the directory; the
			// listPackageVolumes handler gates it out separately.
			label:         "bare package dir",
			input:         "installed/core/nginx",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "nginx",
			prettyName:    "nginx",
			version:       "",
			volName:       "",
		},
		{
			// Same treatment for a nested dep key dir — the walker is
			// done with the name chain and just has no version yet.
			label:         "bare dep key dir (no version yet)",
			input:         "installed/core/gitea/subpackages/postgres",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "gitea--dep--postgres",
			prettyName:    "gitea/postgres",
			version:       "",
			volName:       "",
		},
		{
			// Bare version dir (package name plus version, no volume)
			// stays accepted so serviceNameFromVolumePath can still
			// derive the unit name.
			label:         "bare version dir",
			input:         "installed/core/nginx/1.0",
			ok:            true,
			state:         "installed",
			repo:          "core",
			effectiveName: "nginx",
			prettyName:    "nginx",
			version:       "1.0",
			volName:       "",
		},
		{label: "bare prefix", input: "installed/", ok: false},
		{label: "bare repo", input: "installed/core", ok: false},
		{label: "bare subpackages container", input: "installed/core/gitea/subpackages", ok: false},
		{label: "no matching prefix", input: "pages/my-site", ok: false},
		{label: "user subvol", input: "user/foo", ok: false},
		{label: "malformed subpackages at repo root", input: "installed/core/subpackages/key/1.0/data", ok: false},
		{label: "empty", input: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got, ok := parseInstalledPath(tt.input)
			if ok != tt.ok {
				t.Fatalf("parseInstalledPath(%q) ok = %v, want %v", tt.input, ok, tt.ok)
			}
			if !ok {
				return
			}
			if got.state != tt.state || got.repo != tt.repo || got.effectiveName != tt.effectiveName || got.prettyName != tt.prettyName || got.version != tt.version || got.volName != tt.volName {
				t.Errorf("parseInstalledPath(%q) = %+v, want state=%s repo=%s effective=%s pretty=%s version=%s vol=%s",
					tt.input, got, tt.state, tt.repo, tt.effectiveName, tt.prettyName, tt.version, tt.volName)
			}
		})
	}
}

func TestClassifyFilesystemDepPretty(t *testing.T) {
	state, display := classifyFilesystem("installed/core/gitea/subpackages/postgres/15.0/pgdata")
	if state != "installed" {
		t.Errorf("state = %q, want installed", state)
	}
	if display != "gitea/postgres/15.0/pgdata" {
		t.Errorf("display = %q, want gitea/postgres/15.0/pgdata", display)
	}

	// Bare package and dep-key dirs surface with just the pretty name
	// so users can browse the tree; the `subpackages` container itself
	// is infra and must still be skipped entirely.
	bareCases := map[string]struct {
		state, display string
	}{
		"installed/core/gitea":                     {"installed", "gitea"},
		"installed/core/gitea/subpackages/postgres": {"installed", "gitea/postgres"},
	}
	for input, want := range bareCases {
		gotState, gotDisplay := classifyFilesystem(input)
		if gotState != want.state || gotDisplay != want.display {
			t.Errorf("classifyFilesystem(%q) = (%q, %q), want (%q, %q)",
				input, gotState, gotDisplay, want.state, want.display)
		}
	}
	if state, _ := classifyFilesystem("installed/core/gitea/subpackages"); state != "" {
		t.Errorf("classifyFilesystem bare subpackages container: state = %q, want empty", state)
	}
}

func TestServiceNameFromNestedVolumePath(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"flat":          {"installed/core/nginx/1.0/data", "town-os-package--core-nginx-1.0.service"},
		"one-level dep": {"installed/core/gitea/subpackages/postgres/15.0/pgdata", "town-os-package--core-gitea--dep--postgres-15.0.service"},
		"two-level dep": {"installed/core/jitsi/subpackages/prosody/subpackages/metrics/0.3/data", "town-os-package--core-jitsi--dep--prosody--dep--metrics-0.3.service"},
		"uninstalled":   {"uninstalled/core/gitea/subpackages/postgres/15.0/pgdata", "town-os-package--core-gitea--dep--postgres-15.0.service"},
	}
	for label, tt := range tests {
		t.Run(label, func(t *testing.T) {
			got := serviceNameFromVolumePath(tt.input)
			if got != tt.want {
				t.Fatalf("serviceNameFromVolumePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPackageVolumePathUsesStoragePath(t *testing.T) {
	// Standalone packages produce the legacy flat form (regression guard).
	if got := packageVolumePath("core", "nginx", "1.0", "data"); got != "installed/core/nginx/1.0/data" {
		t.Errorf("packageVolumePath standalone = %q", got)
	}
	// Dep effective names translate through StoragePath so on-disk paths
	// end up nested under their parent.
	if got := packageVolumePath("core", "gitea--dep--postgres", "15.0", "pgdata"); got != "installed/core/gitea/subpackages/postgres/15.0/pgdata" {
		t.Errorf("packageVolumePath dep = %q", got)
	}
	// Nested deps produce multiple subpackages segments.
	if got := packageVolumePath("core", "jitsi--dep--prosody--dep--metrics", "0.3", "data"); got != "installed/core/jitsi/subpackages/prosody/subpackages/metrics/0.3/data" {
		t.Errorf("packageVolumePath nested dep = %q", got)
	}
}

func TestIsReservedFilesystemIncludesSubpackages(t *testing.T) {
	if !isReservedFilesystem(packages.SubpackagesDir) {
		t.Error("expected subpackages to be reserved at top level")
	}
	if !isReservedFilesystem("subpackages/anything") {
		t.Error("expected subpackages prefix to be reserved")
	}
}

// TestIsReservedFilesystemIncludesGfeh pins the reserved prefix gfeh's own
// `make check-townos-sync` reads out of isReservedFilesystem. A user volume
// that shadowed a partition would put unmanaged files inside the object-storage
// root, so both the bare name and anything beneath it must be refused.
func TestIsReservedFilesystemIncludesGfeh(t *testing.T) {
	if !isReservedFilesystem(GfehVolumePrefix) {
		t.Error("expected gfeh to be reserved at top level")
	}
	if !isReservedFilesystem("gfeh/photos") {
		t.Error("expected gfeh prefix to be reserved")
	}
	// Not a false positive on a name that merely starts with the same letters.
	if isReservedFilesystem("gfeh-notes") {
		t.Error("gfeh-notes is an ordinary user volume, not a reserved prefix")
	}
}

// TestGfehSubvolumesAreRefusedByTheArchiveEndpoints guards the IRON RULE
// boundary: the archive routes are a tar transport for volume seeding, and
// unpacking into a partition would create files gfeh's index has never seen —
// no owner, no ACL, no change sequence. resolveArchiveSubvolume deliberately
// omits the gfeh prefix, and isGfehSubvolume is what turns that omission into a
// refusal instead of a silent rewrite to user/gfeh/<...>.
func TestGfehSubvolumesAreRefusedByTheArchiveEndpoints(t *testing.T) {
	for _, name := range []string{"gfeh", "gfeh/photos", "gfeh/photos/nested"} {
		if !isGfehSubvolume(name) {
			t.Errorf("isGfehSubvolume(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "user/photos", "gfeh-notes", "installed/core/nginx"} {
		if isGfehSubvolume(name) {
			t.Errorf("isGfehSubvolume(%q) = true, want false", name)
		}
	}
	// And the resolver must never turn a gfeh name into a partition path.
	if got := resolveArchiveSubvolume("gfeh/photos"); got == "gfeh/photos" {
		t.Error("resolveArchiveSubvolume passed a gfeh partition through; the archive routes could then write into it")
	}
}
