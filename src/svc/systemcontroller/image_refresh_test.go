// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFloatingImageRef(t *testing.T) {
	cases := []struct {
		ref  string
		want bool
		why  string
	}{
		// The tags every box actually runs. These are the whole reason this
		// function exists: they are what the install unit bakes in and what
		// resolveImageTag defaults to.
		{"quay.io/town/ui:rc.latest-x86_64", true, "the default tag on every installed box"},
		{"quay.io/town/ui:rc.latest-aarch64", true, "same, on the other architecture"},
		{"quay.io/town/town:rc.latest", true, "the multi-arch manifest form"},
		{"quay.io/town/ui:latest-x86_64", true, "the release channel's floating tag"},
		{"quay.io/town/ui:release.latest", true, "release channel, manifest form"},
		{"quay.io/prometheus/prometheus:latest", true, "third-party floating tag"},
		{"docker.io/grafana/grafana:latest", true, "the grafana image, verbatim"},

		// Dated and per-run tags are pinned: having them IS having the right
		// bits, so a boot must not spend a registry round trip on them.
		{"quay.io/town/ui:rc.2026-08-13-x86_64", false, "dated rc tag"},
		{"quay.io/town/ui:release.2026-08-13-x86_64", false, "dated release tag"},
		{"quay.io/town/ui:testtag", false, "the neutral tag mocked unit tests use"},
		{"quay.io/town/ui@sha256:0000000000000000000000000000000000000000000000000000000000000000", false, "digest reference"},

		// localhost images have no registry to refresh from at all.
		{"localhost/town-os-ui:abc123", false, "per-instance test image"},
		{"localhost/town-os-networkcontroller:local", false, "the socat fallback image"},

		// A registry with a port is not a tag. Getting this wrong would
		// classify "registry:5000/town/ui" by the string "5000/town/ui".
		{"registry:5000/town/ui:rc.latest-x86_64", true, "host:port plus floating tag"},
		{"registry:5000/town/ui:rc.2026-08-13-x86_64", false, "host:port plus pinned tag"},
		{"registry:5000/town/ui", true, "host:port, no tag — podman resolves :latest"},

		{"alpine", true, "bare name resolves to :latest"},
		{"", false, "empty reference pulls nothing"},
	}

	for _, tc := range cases {
		if got := FloatingImageRef(tc.ref); got != tc.want {
			t.Errorf("FloatingImageRef(%q) = %v, want %v (%s)", tc.ref, got, tc.want, tc.why)
		}
	}
}

// stubImageSeams points both podman seams at recorders and returns the pull
// log. Restores are registered on t, so a failing test cannot leak a stub into
// the next one.
func stubImageSeams(t *testing.T, exists bool, pullErr error) *[]string {
	t.Helper()
	var pulled []string
	t.Cleanup(TestSetImageExistsLocally(func(_ context.Context, _ string) bool { return exists }))
	t.Cleanup(TestSetPullImage(func(_ context.Context, image string) error {
		pulled = append(pulled, image)
		return pullErr
	}))
	return &pulled
}

// The regression this whole file exists for: a box that already has SOMETHING
// under a floating tag must still fetch what the tag points at now. Skipping
// this pull is what left a re-booted VM serving the UI image its disk was
// created with, monitoring tabs and all.
func TestEnsureImageRefreshesFloatingTagThatIsAlreadyPresent(t *testing.T) {
	pulled := stubImageSeams(t, true, nil)

	if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(*pulled) != 1 {
		t.Fatalf("expected the floating tag to be re-pulled, pulls = %v", *pulled)
	}
}

func TestEnsureImageSkipsPullForPinnedTagThatIsAlreadyPresent(t *testing.T) {
	pulled := stubImageSeams(t, true, nil)

	if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.2026-08-13-x86_64"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(*pulled) != 0 {
		t.Fatalf("pinned tag already present must not be pulled, pulls = %v", *pulled)
	}
}

func TestEnsureImagePullsPinnedTagThatIsMissing(t *testing.T) {
	pulled := stubImageSeams(t, false, nil)

	if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.2026-08-13-x86_64"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(*pulled) != 1 {
		t.Fatalf("missing pinned tag must be pulled, pulls = %v", *pulled)
	}
}

// A box must reboot with no registry, which is the same reason its own unit
// runs --pull=missing rather than --pull=always.
func TestEnsureImageFallsBackToTheLocalCopyWhenTheRefreshFails(t *testing.T) {
	pulled := stubImageSeams(t, true, errors.New("dial quay.io: network unreachable"))

	if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64"); err != nil {
		t.Fatalf("a failed refresh of an image that IS local must not be an error: %v", err)
	}
	if len(*pulled) != 1 {
		t.Fatalf("expected one attempted pull, pulls = %v", *pulled)
	}
}

func TestEnsureImageFailsWhenThePullFailsAndNothingIsLocal(t *testing.T) {
	stubImageSeams(t, false, errors.New("dial quay.io: network unreachable"))

	err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64")
	if err == nil {
		t.Fatal("expected an error when the image is neither pullable nor local")
	}
}

func TestEnsureImageRefreshCanBeTurnedOff(t *testing.T) {
	for _, off := range []string{"0", "false", "no", "off", "OFF"} {
		t.Setenv(EnvImageRefresh, off)
		pulled := stubImageSeams(t, true, nil)
		if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64"); err != nil {
			t.Fatalf("EnsureImage with %s=%q: %v", EnvImageRefresh, off, err)
		}
		if len(*pulled) != 0 {
			t.Fatalf("%s=%q must suppress the refresh, pulls = %v", EnvImageRefresh, off, *pulled)
		}
	}
}

// Anything that is not one of the recognised false spellings leaves the refresh
// on. A box that stops acquiring its own services because of a typo in an env
// var is a worse failure than one extra pull.
func TestEnsureImageRefreshStaysOnForUnrecognisedValues(t *testing.T) {
	for _, on := range []string{"", "1", "true", "yes", "please"} {
		t.Setenv(EnvImageRefresh, on)
		pulled := stubImageSeams(t, true, nil)
		if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64"); err != nil {
			t.Fatalf("EnsureImage with %s=%q: %v", EnvImageRefresh, on, err)
		}
		if len(*pulled) != 1 {
			t.Fatalf("%s=%q must leave the refresh on, pulls = %v", EnvImageRefresh, on, *pulled)
		}
	}
}

// Turning the refresh off must never turn a MISSING image into a silent
// no-op: the flag is about not re-fetching, not about not fetching.
func TestEnsureImageRefreshOffStillPullsWhatIsMissing(t *testing.T) {
	t.Setenv(EnvImageRefresh, "0")
	pulled := stubImageSeams(t, false, nil)

	if err := EnsureImage(t.Context(), "quay.io/town/ui:rc.latest-x86_64"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(*pulled) != 1 {
		t.Fatalf("a missing image must still be pulled, pulls = %v", *pulled)
	}
}

// The harness loads every image its containers run from the local image cache
// (captive networks break registry DNS, and the docker.io mirror does not cover
// quay.io), so the boxes it starts must not go back to the registry to replace
// an image that was placed there on purpose.
func TestHarnessContainersDisableTheImageRefresh(t *testing.T) {
	for _, script := range []string{"make/test.sh", "make/dev.sh"} {
		body := readRepoFile(t, script)
		runs := strings.Count(body, "--systemd=true")
		offs := strings.Count(body, EnvImageRefresh+"=0")
		if offs < runs {
			t.Errorf("%s starts %d systemd container(s) but sets %s=0 on only %d of them",
				script, runs, EnvImageRefresh, offs)
		}
	}
}

func TestEnsureImageNeverPullsLocalhostImages(t *testing.T) {
	pulled := stubImageSeams(t, true, errors.New("localhost is not a registry"))

	if err := EnsureImage(t.Context(), "localhost/town-os-ui:abc123"); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	if len(*pulled) != 0 {
		t.Fatalf("localhost images have no registry to pull from, pulls = %v", *pulled)
	}
}
