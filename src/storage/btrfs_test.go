package storage

import (
	"strings"
	"testing"
)

func TestParseSubvolShow(t *testing.T) {
	output := `/mnt/data/mysubvol
	Name: 			mysubvol
	UUID: 			5e076a14-4e42-254d-ac8e-55bebea982d1
	Parent UUID: 		-
	Received UUID: 		-
	Creation time: 		2024-01-15 10:30:00 +0000
	Subvolume ID: 		259
	Generation: 		42
	Gen at creation: 	42
	Parent ID: 		5
	Top level ID: 		5
	Flags: 			-
	Send transid: 		0
	Send time: 		2024-01-15 10:30:00 +0000
	Receive transid: 	0
	Receive time: 		-
`
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}

	if info.Name != "mysubvol" {
		t.Fatalf("expected Name %q, got %q", "mysubvol", info.Name)
	}

	if info.ID != 259 {
		t.Fatalf("expected ID 259, got %d", info.ID)
	}
}

func TestParseSubvolShowMinimal(t *testing.T) {
	output := `	Name: 			vol
	Subvolume ID: 		5
`
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}

	if info.Name != "vol" {
		t.Fatalf("expected Name %q, got %q", "vol", info.Name)
	}

	if info.ID != 5 {
		t.Fatalf("expected ID 5, got %d", info.ID)
	}
}

func TestParseSubvolShowBadID(t *testing.T) {
	output := `	Name: 			vol
	Subvolume ID: 		notanumber
`
	_, err := parseSubvolShow(output)
	if err == nil {
		t.Fatal("expected error for bad subvolume ID")
	}
}

func TestParseSubvolList(t *testing.T) {
	output := `ID 256 gen 34 top level 5 path home
ID 257 gen 37 top level 5 path root
ID 258 gen 25 top level 257 path root/var/lib/machines
`
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(infos))
	}

	expected := []struct {
		name string
		id   uint64
	}{
		{"home", 256},
		{"root", 257},
		{"root/var/lib/machines", 258},
	}

	for i, e := range expected {
		if infos[i].Name != e.name {
			t.Fatalf("entry %d: expected Name %q, got %q", i, e.name, infos[i].Name)
		}
		if infos[i].ID != e.id {
			t.Fatalf("entry %d: expected ID %d, got %d", i, e.id, infos[i].ID)
		}
	}
}

func TestParseSubvolListWithPrefix(t *testing.T) {
	output := `ID 256 gen 34 top level 5 path home
ID 257 gen 37 top level 5 path root
ID 258 gen 25 top level 257 path root/var/lib/machines
`
	infos, err := parseSubvolList(output, "root")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(infos))
	}

	if infos[0].Name != "root" {
		t.Fatalf("expected first entry %q, got %q", "root", infos[0].Name)
	}

	if infos[1].Name != "root/var/lib/machines" {
		t.Fatalf("expected second entry %q, got %q", "root/var/lib/machines", infos[1].Name)
	}
}

func TestParseSubvolListDeduplicates(t *testing.T) {
	output := `ID 256 gen 34 top level 5 path home
ID 256 gen 34 top level 5 path home
`
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d", len(infos))
	}
}

func TestParseSubvolListEmpty(t *testing.T) {
	infos, err := parseSubvolList("", "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(infos))
	}
}

func TestParseSubvolListSkipsMalformed(t *testing.T) {
	output := `not a valid line
ID 256 gen 34 top level 5 path home
also bad
`
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(infos))
	}

	if infos[0].Name != "home" {
		t.Fatalf("expected Name %q, got %q", "home", infos[0].Name)
	}
}

func TestParseSubvolListPathWithSpaces(t *testing.T) {
	output := "ID 256 gen 34 top level 5 path my subvol name\n"
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(infos))
	}

	if infos[0].Name != "my subvol name" {
		t.Fatalf("expected Name %q, got %q", "my subvol name", infos[0].Name)
	}
}

func TestParseQGroupShowNone(t *testing.T) {
	output := `qgroupid         rfer         excl     max_rfer     max_excl
--------         ----         ----     --------     --------
0/256         16384        16384         none         none
`
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for none, got %d", val)
	}
}

func TestParseQGroupShowWithLimit(t *testing.T) {
	output := `qgroupid         rfer         excl     max_rfer     max_excl
--------         ----         ----     --------     --------
0/256         16384        16384      1073741824         none
`
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 1073741824 {
		t.Fatalf("expected 1073741824, got %d", val)
	}
}

func TestParseQGroupShowEmpty(t *testing.T) {
	val, err := parseQGroupShow("", 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for empty, got %d", val)
	}
}

func TestParseQGroupShowBadNumber(t *testing.T) {
	output := `qgroupid         rfer         excl     max_rfer     max_excl
--------         ----         ----     --------     --------
0/256         16384        16384      notanumber         none
`
	_, err := parseQGroupShow(output, 256)
	if err == nil {
		t.Fatal("expected error for bad number")
	}
}

func TestParseQGroupShowMultipleQGroups(t *testing.T) {
	//nolint:dupword // test data intentionally contains repeated "none" values
	output := `qgroupid         rfer         excl     max_rfer     max_excl
--------         ----         ----     --------     --------
0/256         16384        16384         none         none
0/257         32768        32768      2147483648         none
0/258         65536        65536      1073741824         none
`
	// Target subvol 257 — should find its quota, not the first line
	val, err := parseQGroupShow(output, 257)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 2147483648 {
		t.Fatalf("expected 2147483648, got %d", val)
	}

	// Target subvol 256 — has no quota
	val, err = parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for none, got %d", val)
	}

	// Target subvol 999 — not in output
	val, err = parseQGroupShow(output, 999)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for missing, got %d", val)
	}
}

func TestFindMountPointBtrfs(t *testing.T) {
	// This tests the parsing logic only, not actual mounts
	// The function reads /proc/self/mounts which requires a real FS
	// so we just verify the error case for a definitely-not-mounted path
	_, err := findMountPoint("/definitely/not/a/real/btrfs/mount/path")
	if err == nil {
		t.Fatal("expected error for non-existent mount")
	}
	if !strings.Contains(err.Error(), "mount point not found") {
		t.Fatalf("expected mount not found error, got: %v", err)
	}
}

// --- Additional btrfs output parsing edge-case tests ---

func TestParseSubvolShowEmpty(t *testing.T) {
	info, err := parseSubvolShow("")
	if err != nil {
		t.Fatalf("parseSubvolShow empty: %v", err)
	}
	if info.Name != "" {
		t.Fatalf("expected empty Name, got %q", info.Name)
	}
	if info.ID != 0 {
		t.Fatalf("expected ID 0, got %d", info.ID)
	}
}

func TestParseSubvolShowOnlyName(t *testing.T) {
	output := "\tName: \t\t\tjust-a-name\n"
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}
	if info.Name != "just-a-name" {
		t.Fatalf("expected Name %q, got %q", "just-a-name", info.Name)
	}
	if info.ID != 0 {
		t.Fatalf("expected ID 0 when not present, got %d", info.ID)
	}
}

func TestParseSubvolShowOnlyID(t *testing.T) {
	output := "\tSubvolume ID: \t\t42\n"
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}
	if info.Name != "" {
		t.Fatalf("expected empty Name, got %q", info.Name)
	}
	if info.ID != 42 {
		t.Fatalf("expected ID 42, got %d", info.ID)
	}
}

func TestParseSubvolShowExtraWhitespace(t *testing.T) {
	output := "  \tName:   \t  spacey-vol  \n  \tSubvolume ID:   \t  100  \n"
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}
	if info.Name != "spacey-vol" {
		t.Fatalf("expected Name %q, got %q", "spacey-vol", info.Name)
	}
	if info.ID != 100 {
		t.Fatalf("expected ID 100, got %d", info.ID)
	}
}

func TestParseSubvolListBadIDSkipped(t *testing.T) {
	output := "ID abc gen 34 top level 5 path home\nID 256 gen 34 top level 5 path data\n"
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 entry (bad ID skipped), got %d", len(infos))
	}
	if infos[0].Name != "data" {
		t.Fatalf("expected Name %q, got %q", "data", infos[0].Name)
	}
}

func TestParseSubvolListTooFewFields(t *testing.T) {
	output := "ID 256 gen 34\n"
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("expected 0 entries for short line, got %d", len(infos))
	}
}

func TestParseSubvolListPrefixNoMatch(t *testing.T) {
	output := "ID 256 gen 34 top level 5 path home\nID 257 gen 37 top level 5 path data\n"
	infos, err := parseSubvolList(output, "nonexistent")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("expected 0 entries for unmatched prefix, got %d", len(infos))
	}
}

func TestParseQGroupShowOnlyHeaders(t *testing.T) {
	output := "qgroupid         rfer         excl     max_rfer     max_excl\n--------         ----         ----     --------     --------\n"
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for headers-only, got %d", val)
	}
}

func TestParseQGroupShowShortFields(t *testing.T) {
	output := "qgroupid         rfer         excl     max_rfer     max_excl\n--------         ----         ----     --------     --------\n0/256\n"
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for short line, got %d", val)
	}
}

func TestParseQGroupShowLargeQuota(t *testing.T) {
	// 10 TB in bytes
	output := "qgroupid         rfer         excl     max_rfer     max_excl\n--------         ----         ----     --------     --------\n0/256         16384        16384      10995116277760         none\n"
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 10995116277760 {
		t.Fatalf("expected 10995116277760, got %d", val)
	}
}

func TestParseQGroupShowOneHeaderLine(t *testing.T) {
	// Only one header line instead of two — data should still be parsed
	output := "qgroupid rfer excl max_rfer max_excl\n0/256 16384 16384 1048576 none\n"
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	// The second line is treated as the dashes header, and there's nothing after
	if val != 0 {
		t.Fatalf("expected 0 (data line consumed as header), got %d", val)
	}
}

// --- DiskUsage struct tests ---

func TestDiskUsageFields(t *testing.T) {
	du := DiskUsage{Total: 1000, Used: 600, Available: 400}
	if du.Total != 1000 {
		t.Fatalf("expected Total 1000, got %d", du.Total)
	}
	if du.Used != 600 {
		t.Fatalf("expected Used 600, got %d", du.Used)
	}
	if du.Available != 400 {
		t.Fatalf("expected Available 400, got %d", du.Available)
	}
}

func TestDiskUsageZero(t *testing.T) {
	du := DiskUsage{}
	if du.Total != 0 || du.Used != 0 || du.Available != 0 {
		t.Fatalf("expected all zeros, got %v", du)
	}
}

func TestDiskUsageOverride(t *testing.T) {
	override := &DiskUsage{Total: 5000, Used: 3000, Available: 2000}
	b := &BtrFS{DiskUsageOverride: override}
	du, err := b.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if du.Total != 5000 {
		t.Fatalf("expected Total 5000, got %d", du.Total)
	}
	if du.Used != 3000 {
		t.Fatalf("expected Used 3000, got %d", du.Used)
	}
	if du.Available != 2000 {
		t.Fatalf("expected Available 2000, got %d", du.Available)
	}
}

func TestDiskUsageOverrideZero(t *testing.T) {
	override := &DiskUsage{}
	b := &BtrFS{DiskUsageOverride: override}
	du, err := b.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if du.Total != 0 || du.Used != 0 || du.Available != 0 {
		t.Fatalf("expected all zeros, got %v", du)
	}
}

// --- Additional tests for parsing edge cases and mock workflows ---

func TestParseSubvolListMultiplePaths(t *testing.T) {
	// Paths sharing a common prefix: "nginx", "nginx-backup", "nginx/data".
	// With prefix "nginx/" only "nginx/data" should match.
	output := `ID 300 gen 10 top level 5 path nginx
ID 301 gen 11 top level 5 path nginx-backup
ID 302 gen 12 top level 5 path nginx/data
`
	infos, err := parseSubvolList(output, "nginx/")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("expected 1 entry with prefix %q, got %d", "nginx/", len(infos))
	}

	if infos[0].Name != "nginx/data" {
		t.Fatalf("expected Name %q, got %q", "nginx/data", infos[0].Name)
	}

	if infos[0].ID != 302 {
		t.Fatalf("expected ID 302, got %d", infos[0].ID)
	}
}

func TestParseQGroupShowMaxExcl(t *testing.T) {
	// max_rfer (column 4) is "none" but max_excl (column 5) has a value.
	// parseQGroupShow should only read max_rfer and return 0.
	output := `qgroupid         rfer         excl     max_rfer     max_excl
--------         ----         ----     --------     --------
0/256         16384        16384         none      5368709120
`
	val, err := parseQGroupShow(output, 256)
	if err != nil {
		t.Fatalf("parseQGroupShow: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 because max_rfer is none (max_excl should be ignored), got %d", val)
	}
}

func TestParseSubvolShowMixedContent(t *testing.T) {
	// Output has extra unrecognized fields mixed in between Name and
	// Subvolume ID. The parser should still extract both correctly.
	output := `/mnt/data/mixed
	Name: 			mixed-vol
	UUID: 			aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
	Parent UUID: 		-
	Received UUID: 		-
	Creation time: 		2025-06-01 08:00:00 +0000
	Custom Field: 		some-value
	Subvolume ID: 		512
	Generation: 		99
	Another Field: 		extra-data
	Parent ID: 		5
	Top level ID: 		5
	Flags: 			-
`
	info, err := parseSubvolShow(output)
	if err != nil {
		t.Fatalf("parseSubvolShow: %v", err)
	}

	if info.Name != "mixed-vol" {
		t.Fatalf("expected Name %q, got %q", "mixed-vol", info.Name)
	}

	if info.ID != 512 {
		t.Fatalf("expected ID 512, got %d", info.ID)
	}
}

func TestParseSubvolListLargeID(t *testing.T) {
	// Use the maximum uint64 value to verify large ID handling.
	output := "ID 18446744073709551615 gen 1 top level 5 path bigvol\n"
	infos, err := parseSubvolList(output, "")
	if err != nil {
		t.Fatalf("parseSubvolList: %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(infos))
	}

	if infos[0].ID != 18446744073709551615 {
		t.Fatalf("expected ID 18446744073709551615, got %d", infos[0].ID)
	}

	if infos[0].Name != "bigvol" {
		t.Fatalf("expected Name %q, got %q", "bigvol", infos[0].Name)
	}
}

func TestBtrFSMockCreateAndList(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "webapp"})
	if err != nil {
		t.Fatalf("CreateFilesystem webapp: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "webapp" {
		t.Fatalf("expected filesystem name %q, got %q", "webapp", fs[0].Name)
	}
}

func TestBtrFSMockDeleteNonExistent(t *testing.T) {
	mock := InitBtrFSMockController()

	// The subvolume "ghost" was never created. Verify it does not exist.
	err := mock.IsSubvolume("ghost")
	if err == nil {
		t.Fatal("expected error from IsSubvolume for non-existent subvolume")
	}

	// SubvolDelete on the mock silently succeeds even when the subvolume
	// does not exist (it simply filters the empty list). Verify no panic
	// and that the filesystem list remains empty.
	err = mock.SubvolDelete("ghost")
	if err != nil {
		t.Fatalf("SubvolDelete ghost: unexpected error: %v", err)
	}

	info := mock.GetFilesystems()
	if len(info) != 0 {
		t.Fatalf("expected 0 filesystems after deleting non-existent, got %d", len(info))
	}
}
