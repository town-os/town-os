package storage

import (
	"testing"
)

func TestDiskUsageOverride(t *testing.T) {
	override := DiskUsage{Total: 1000, Used: 400, Available: 600}
	b := &BtrFS{
		DiskUsageOverride: &override,
	}

	du, err := b.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}

	if du.Total != 1000 {
		t.Fatalf("expected Total 1000, got %d", du.Total)
	}
	if du.Used != 400 {
		t.Fatalf("expected Used 400, got %d", du.Used)
	}
	if du.Available != 600 {
		t.Fatalf("expected Available 600, got %d", du.Available)
	}
}

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
