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
