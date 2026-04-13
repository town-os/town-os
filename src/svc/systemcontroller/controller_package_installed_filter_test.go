package systemcontroller

import (
	"slices"
	"testing"
)

func TestFilterDependencyInstalls(t *testing.T) {
	input := []string{
		"repo-a/parent@1.0",
		"repo-a/parent--dep--up@1.0",
		"repo-a/parent--dep--down@1.0",
		"repo-a/parent--dep--up--dep--nested@1.0",
		"repo-b/nginx@1.0",
	}
	want := []string{
		"repo-a/parent@1.0",
		"repo-b/nginx@1.0",
	}
	got := filterDependencyInstalls(input)
	if !slices.Equal(got, want) {
		t.Errorf("filterDependencyInstalls = %v, want %v", got, want)
	}
}

func TestFilterDependencyInstallsKeepsMalformedEntries(t *testing.T) {
	// An entry that fails to parse should pass through unchanged — the
	// caller will handle the parse error on its own. We do not want to
	// accidentally hide a malformed entry the user needs to see.
	input := []string{
		"repo-a/nginx@1.0",
		"garbage",
	}
	got := filterDependencyInstalls(input)
	if len(got) != 2 {
		t.Errorf("want 2 entries, got %v", got)
	}
}
