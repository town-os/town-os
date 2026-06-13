package main

import (
	"runtime"
	"strings"
	"testing"
)

func TestVersionVariableExists(t *testing.T) {
	// Version is set via ldflags at build time; at test time it is the
	// zero value (empty string). We verify the variable exists and that
	// the fallback logic handles the empty case.
	if Version != "" {
		t.Logf("Version is set to %q (via ldflags)", Version)
	} else {
		t.Log("Version is empty (expected during testing without ldflags)")
	}
}

func TestDefaultVersionTagIsPerArch(t *testing.T) {
	// rc tags are partitioned per architecture; the fallback must carry
	// the host arch suffix so a tag-less binary still pulls per-arch
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

func TestVersionFallbackLogic(t *testing.T) {
	// When Version is empty, the tag should fall back to reading
	// /town-os.tag or defaulting to defaultVersionTag(). We can't test
	// the file read in unit tests, but we verify the variable is usable.
	tag := defaultVersionTag()
	if Version != "" {
		tag = Version
	}
	if tag == "" {
		t.Fatal("tag must never be empty")
	}
}
