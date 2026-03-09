// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"testing"
)

func TestDiskUsageRealFS(t *testing.T) {
	// Use a real directory to exercise the syscall.Statfs path.
	b := &BtrFS{
		BasePath: t.TempDir(),
	}

	du, err := b.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}

	// On a real filesystem, total should be > 0 and available <= total.
	if du.Total == 0 {
		t.Fatal("expected non-zero Total on a real filesystem")
	}
	if du.Available > du.Total {
		t.Fatalf("Available (%d) should not exceed Total (%d)", du.Available, du.Total)
	}
	if du.Used != du.Total-du.Available {
		t.Fatalf("Used (%d) should equal Total-Available (%d)", du.Used, du.Total-du.Available)
	}
}

func TestDiskUsageBadPath(t *testing.T) {
	b := &BtrFS{
		BasePath: "/definitely/not/a/real/path/for/disk/usage",
	}

	_, err := b.DiskUsage()
	if err == nil {
		t.Fatal("expected error for non-existent path")
	}
}
