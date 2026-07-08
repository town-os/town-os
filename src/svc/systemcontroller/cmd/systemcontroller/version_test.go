package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestDefaultVersionTagIsPerArch(t *testing.T) {
	// rc tags are partitioned per architecture; the default must carry the host
	// arch suffix so a binary with no TOWN_OS_TAG override still pulls per-arch
	// sibling images instead of a single-arch rc.latest. The suffix is the
	// uname -m form (x86_64/aarch64), not Go's GOARCH (amd64/arm64).
	tag := defaultVersionTag()
	want := "rc.latest-" + archTag()
	if tag != want {
		t.Fatalf("defaultVersionTag() = %q, want %q", tag, want)
	}
	if !strings.HasPrefix(tag, "rc.latest-") {
		t.Fatalf("defaultVersionTag() %q must keep the rc.latest- prefix", tag)
	}
	switch runtime.GOARCH {
	case "amd64":
		if tag != "rc.latest-x86_64" {
			t.Fatalf("on amd64, defaultVersionTag() = %q, want rc.latest-x86_64", tag)
		}
	case "arm64":
		if tag != "rc.latest-aarch64" {
			t.Fatalf("on arm64, defaultVersionTag() = %q, want rc.latest-aarch64", tag)
		}
	}
}

// resolveImageTag defaults to rc.latest-<arch> so a system update always pulls
// the newest images. The former compile-time Version pin and /town-os.tag file
// were removed; only the TOWN_OS_TAG env var (set by the install build system)
// overrides the default now.
func TestResolveImageTagDefaultsToRCLatest(t *testing.T) {
	t.Setenv("TOWN_OS_TAG", "")
	if got := resolveImageTag(); got != defaultVersionTag() {
		t.Fatalf("resolveImageTag() with no override = %q, want %q", got, defaultVersionTag())
	}
}

func TestResolveImageTagHonorsEnvOverride(t *testing.T) {
	// The install image build system pins a specific tag via TOWN_OS_TAG.
	t.Setenv("TOWN_OS_TAG", "rc.20260707-aarch64")
	if got := resolveImageTag(); got != "rc.20260707-aarch64" {
		t.Fatalf("resolveImageTag() = %q, want rc.20260707-aarch64", got)
	}
}

func TestResolveImageTagTrimsWhitespace(t *testing.T) {
	// A stray newline/space from the unit's Environment= must not leak into the
	// image reference.
	t.Setenv("TOWN_OS_TAG", "  release.20260708-x86_64\n")
	if got := resolveImageTag(); got != "release.20260708-x86_64" {
		t.Fatalf("resolveImageTag() = %q, want trimmed release.20260708-x86_64", got)
	}
}

func TestResolveImageTagNeverEmpty(t *testing.T) {
	// A blank override must fall back to the default, never an empty tag.
	t.Setenv("TOWN_OS_TAG", "   ")
	if got := resolveImageTag(); got == "" {
		t.Fatal("resolveImageTag() must never return an empty tag")
	}
}
