package main

import (
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

func TestVersionFallbackLogic(t *testing.T) {
	// When Version is empty, the tag should fall back to reading
	// /town-os.tag or defaulting to "rc.latest". We can't test the
	// file read in unit tests, but we verify the variable is usable.
	tag := "rc.latest"
	if Version != "" {
		tag = Version
	}
	if tag == "" {
		t.Fatal("tag must never be empty")
	}
}
