package systemcontroller

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// The systemcontroller emits boot progress via bs.Step("...") literals in
// cmd/systemcontroller/main.go. The UI's BootStatusStepper renders one row
// per stage from the ordered list in ui/src/components/system/boot-steps.js
// and classifies each stage by its INDEX in that list. A step the backend
// emits that is missing from the frontend list resolves to indexOf === -1,
// which the stepper treats as "nothing done yet" and blanks out every
// already-completed row — the Refresh Core Services panel then flickers all
// stages back to unchecked whenever such a step is current.
//
// These tests fail if the two lists drift apart, so adding a bs.Step() to
// main.go without adding it to boot-steps.js (or reordering one) is caught
// in CI instead of on a live box.

var (
	backendStepRe = regexp.MustCompile(`bs\.Step\("([a-z0-9_]+)"\)`)
	jsTokenRe     = regexp.MustCompile(`['"]([a-z0-9_]+)['"]`)
)

// repoFile resolves a path relative to the repository root from this test
// file's location (src/svc/systemcontroller/), so the test works regardless
// of the process working directory.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = <root>/src/svc/systemcontroller/boot_steps_sync_test.go
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	return filepath.Join(root, filepath.FromSlash(rel))
}

// backendBootSteps extracts the ordered list of static bs.Step("...")
// literals emitted by main.go.
func backendBootSteps(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(repoFile(t, "src/svc/systemcontroller/cmd/systemcontroller/main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	matches := backendStepRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("no bs.Step(\"...\") literals found in main.go — regex or file layout changed")
	}
	steps := make([]string, 0, len(matches))
	for _, m := range matches {
		steps = append(steps, m[1])
	}
	return steps
}

// frontendBootSteps extracts the ordered bootSteps array from boot-steps.js,
// reading only the array literal (not the surrounding prose comment, which
// mentions step names in sentences).
func frontendBootSteps(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(repoFile(t, "ui/src/components/system/boot-steps.js"))
	if err != nil {
		t.Fatalf("read boot-steps.js: %v", err)
	}
	src := string(data)
	openIdx := regexp.MustCompile(`bootSteps\s*=\s*\[`).FindStringIndex(src)
	if openIdx == nil {
		t.Fatal("could not locate `bootSteps = [` in boot-steps.js")
	}
	rest := src[openIdx[1]:]
	closeIdx := regexp.MustCompile(`\]`).FindStringIndex(rest)
	if closeIdx == nil {
		t.Fatal("could not locate closing `]` for bootSteps array")
	}
	body := rest[:closeIdx[0]]
	matches := jsTokenRe.FindAllStringSubmatch(body, -1)
	steps := make([]string, 0, len(matches))
	for _, m := range matches {
		steps = append(steps, m[1])
	}
	return steps
}

func TestBootStepsFrontendCoversEveryBackendStep(t *testing.T) {
	backend := backendBootSteps(t)
	frontend := frontendBootSteps(t)

	frontendSet := make(map[string]bool, len(frontend))
	for _, s := range frontend {
		frontendSet[s] = true
	}

	for _, s := range backend {
		if !frontendSet[s] {
			t.Errorf("backend emits step %q but ui/src/components/system/boot-steps.js does not list it — "+
				"the Refresh Core Services stepper will blank all completed rows when this step is current", s)
		}
	}
}

func TestBootStepsFrontendInSyncWithBackend(t *testing.T) {
	backend := backendBootSteps(t)
	frontend := frontendBootSteps(t)

	// The frontend list may contain steps main.go does not emit directly
	// (refresh_packages comes from freshness.go). Filter the frontend down
	// to just the backend steps, preserving order, then require the two
	// ordered sequences to match exactly. This catches both a missing step
	// and a reordered one.
	backendSet := make(map[string]bool, len(backend))
	for _, s := range backend {
		backendSet[s] = true
	}
	var filtered []string
	for _, s := range frontend {
		if backendSet[s] {
			filtered = append(filtered, s)
		}
	}

	if len(filtered) != len(backend) {
		t.Fatalf("frontend covers %d of %d backend steps; backend=%v frontend(filtered)=%v",
			len(filtered), len(backend), backend, filtered)
	}
	for i := range backend {
		if filtered[i] != backend[i] {
			t.Fatalf("boot step order drift at index %d: backend=%q frontend=%q\nbackend=%v\nfrontend(filtered)=%v",
				i, backend[i], filtered[i], backend, filtered)
		}
	}
}
