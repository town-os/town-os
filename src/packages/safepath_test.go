// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package packages

import (
	"testing"
)

func TestSafePathJoinsCorrectly(t *testing.T) {
	result, err := SafePath("/base/dir", "sub", "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/base/dir/sub/file.txt" {
		t.Fatalf("expected /base/dir/sub/file.txt, got %s", result)
	}
}

func TestSafePathRejectsTraversal(t *testing.T) {
	_, err := SafePath("/base/dir", "..", "etc", "passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestSafePathRejectsDoubleTraversal(t *testing.T) {
	_, err := SafePath("/base/dir", "sub", "..", "..", "etc")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestSafePathAllowsBaseExact(t *testing.T) {
	result, err := SafePath("/base/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/base/dir" {
		t.Fatalf("expected /base/dir, got %s", result)
	}
}

func TestSafePathCleansRedundantSeparators(t *testing.T) {
	result, err := SafePath("/base/dir", "sub//nested")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/base/dir/sub/nested" {
		t.Fatalf("expected /base/dir/sub/nested, got %s", result)
	}
}

func TestSafePathRejectsDotDotInSinglePart(t *testing.T) {
	_, err := SafePath("/base", "../outside")
	if err == nil {
		t.Fatal("expected error for path traversal in single part")
	}
}

func TestSafePathAllowsDotSegment(t *testing.T) {
	result, err := SafePath("/base/dir", ".", "file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/base/dir/file.txt" {
		t.Fatalf("expected /base/dir/file.txt, got %s", result)
	}
}
