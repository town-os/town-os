package storage

import (
	"errors"
	"sort"
	"strings"
	"testing"
)

func TestMockBtrFSStorage(t *testing.T) {
	mock := InitBtrFSMock()
	err := mock.CreateFilesystem(Filesystem{Name: "test"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("Len should be 1 and was not: %d", len(fs))
	}

	err = mock.RemoveFilesystem("test")
	if err != nil {
		t.Fatalf("RemoveFilesystem: %v", err)
	}

	fs, err = mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems after remove: %v", err)
	}

	if len(fs) != 0 {
		t.Fatalf("Len should be 0 and was not: %d", len(fs))
	}
}

func TestMockBtrFSBasic(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	err = mock.SubvolCreate("test/sub")
	if err != nil {
		t.Fatalf("SubvolCreate test/sub: %v", err)
	}

	info := mock.GetFilesystems()

	if len(info) != 2 {
		t.Fatal("Not all filesystems were recorded")
	}

	table := []struct {
		Name string
		ID   uint64
	}{
		{
			Name: "test",
			ID:   1,
		},
		{
			Name: "test/sub",
			ID:   2,
		},
	}

	for x, item := range table {
		if info[x].Name != item.Name {
			t.Fatalf("Filesystems were not created in order (name check %d): %v", x, info[x].Name)
		}

		if info[x].ID != item.ID {
			t.Fatalf("Filesystems were not created in order (id check %d): %v", x, info[x].ID)
		}

		id, err := mock.SubvolID(item.Name)
		if err != nil {
			t.Fatalf("Received error getting ID for %q subvolume (%d): %v", item.Name, x, err)
		}

		if id != item.ID {
			t.Fatalf("Invalid ID for %q subvolume (%d): expected: %d, actual: %d", item.Name, x, item.ID, id)
		}
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Invalid number of results listing test subvolumes")
	}

	if info[0].Name != "test" {
		t.Fatal("test volume does not exist in list call")
	}

	if info[0].ID != 1 {
		t.Fatal("test volume does not exist in list call (id check)")
	}

	if info[1].Name != "test/sub" {
		t.Fatal("test/sub volume does not exist in list call")
	}

	if info[1].ID != 2 {
		t.Fatal("test/sub volume does not exist in list call (id check)")
	}

	err = mock.SubvolDelete("test/sub")
	if err != nil {
		t.Fatalf("SubvolDelete test/sub: %v", err)
	}

	info, err = mock.SubvolList("test/sub")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test/sub` was not deleted after delete call")
	}

	err = mock.SubvolDelete("test")
	if err != nil {
		t.Fatalf("SubvolDelete test: %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}

	err = mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test (re-create): %v", err)
	}

	err = mock.SubvolCreate("test/sub")
	if err != nil {
		t.Fatalf("SubvolCreate test/sub (re-create): %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 2 {
		t.Fatal("Volumes were not re-created")
	}

	// Deleting a parent with children must fail (matches real btrfs behavior).
	err = mock.SubvolDelete("test")
	if err == nil {
		t.Fatal("SubvolDelete test should fail when child subvolumes exist")
	}

	// Delete child first, then parent.
	err = mock.SubvolDelete("test/sub")
	if err != nil {
		t.Fatalf("SubvolDelete test/sub (final child): %v", err)
	}

	err = mock.SubvolDelete("test")
	if err != nil {
		t.Fatalf("SubvolDelete test (final): %v", err)
	}

	info, err = mock.SubvolList("test")
	if err != nil {
		t.Fatalf("Unable to list test subvolumes: %v", err)
	}

	if len(info) != 0 {
		t.Fatal("Subvol `test` was not deleted after delete call")
	}
}

func TestMockIsSubvolume(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.IsSubvolume("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	err = mock.SubvolCreate("test")
	if err != nil {
		t.Fatalf("SubvolCreate test: %v", err)
	}

	err = mock.IsSubvolume("test")
	if err != nil {
		t.Fatalf("expected nil error for existing subvolume, got %v", err)
	}

	err = mock.IsSubvolume("test/sub")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem for non-existing sub, got %v", err)
	}
}

func TestMockSubvolSnapshot(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("src")
	if err != nil {
		t.Fatalf("SubvolCreate src: %v", err)
	}

	err = mock.SubvolSnapshot("dst", "src", true)
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}

	log := mock.GetLog()
	found := false
	for _, c := range log {
		if c.Operation == "SubvolSnapshot" {
			found = true
			if len(c.Arguments) != 3 {
				t.Fatalf("expected 3 arguments, got %d", len(c.Arguments))
			}
			if c.Arguments[0] != "dst" || c.Arguments[1] != "src" || c.Arguments[2] != true {
				t.Fatalf("unexpected snapshot arguments: %v", c.Arguments)
			}
		}
	}
	if !found {
		t.Fatal("SubvolSnapshot not recorded in call log")
	}
}

func TestMockCallLog(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol1")
	if err != nil {
		t.Fatalf("SubvolCreate vol1: %v", err)
	}

	_, err = mock.SubvolInfo("vol1")
	if err != nil {
		t.Fatalf("SubvolInfo vol1: %v", err)
	}

	_, err = mock.SubvolList("vol1")
	if err != nil {
		t.Fatalf("SubvolList vol1: %v", err)
	}

	log := mock.GetLog()
	if len(log) != 3 {
		t.Fatalf("expected 3 call log entries, got %d", len(log))
	}

	expected := []string{"SubvolCreate", "SubvolInfo", "SubvolList"}
	for i, op := range expected {
		if log[i].Operation != op {
			t.Fatalf("log entry %d: expected %q, got %q", i, op, log[i].Operation)
		}
	}
}

func TestMockSubvolInfoNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	_, err := mock.SubvolInfo("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}
}

func TestMockSubvolIDNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	id, err := mock.SubvolID("nonexistent")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got %v", err)
	}

	if id != 0 {
		t.Fatalf("expected id 0 for nonexistent subvol, got %d", id)
	}
}

// --- Validation tests ---

func TestValidateFilesystemName(t *testing.T) {
	valid := []string{
		"test",
		"my-volume",
		"data_dir",
		"vol.1",
		"abc/def",
		"A-Z.0-9_test",
	}

	for _, name := range valid {
		err := ValidateFilesystemName(name)
		if err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateFilesystemNameRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		desc string
	}{
		{"", "empty name"},
		{"/test", "leading slash"},
		{"test/", "trailing slash (empty component)"},
		{"test//sub", "double slash"},
		{"..", "dotdot traversal"},
		{"test/..", "dotdot in path"},
		{"test/./sub", "dot in path"},
		{"hello world", "space in name"},
		{"test\x00vol", "null byte"},
		{"my@vol", "at sign"},
		{"my:vol", "colon"},
		{"vol*name", "asterisk"},
	}

	for _, tc := range cases {
		err := ValidateFilesystemName(tc.name)
		if err == nil {
			t.Errorf("expected %q (%s) to be invalid, got nil", tc.name, tc.desc)
		}
	}
}

// --- Rename tests ---

func TestMockSubvolRename(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("old")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.SubvolRename("old", "new")
	if err != nil {
		t.Fatalf("SubvolRename: %v", err)
	}

	err = mock.IsSubvolume("new")
	if err != nil {
		t.Fatalf("expected renamed volume to exist: %v", err)
	}

	err = mock.IsSubvolume("old")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected old name to not exist, got: %v", err)
	}
}

func TestMockSubvolRenameNotFound(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolRename("nonexistent", "new")
	if !errors.Is(err, ErrNoFilesystem) {
		t.Fatalf("expected ErrNoFilesystem, got: %v", err)
	}
}

func TestMockSubvolRenamePreservesQuota(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 1024)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	err = mock.SubvolRename("vol", "renamed")
	if err != nil {
		t.Fatalf("SubvolRename: %v", err)
	}

	if mock.Quotas["renamed"] != 1024 {
		t.Fatalf("expected quota 1024 on renamed vol, got %d", mock.Quotas["renamed"])
	}

	if _, ok := mock.Quotas["vol"]; ok {
		t.Fatal("old quota entry should be removed")
	}
}

// --- QGroupLimit tests ---

func TestMockQGroupLimit(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	if mock.Quotas["vol"] != 4096 {
		t.Fatalf("expected quota 4096, got %d", mock.Quotas["vol"])
	}
}

func TestMockQGroupLimitZeroRemoves(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit set: %v", err)
	}

	err = mock.QGroupLimit("vol", 0)
	if err != nil {
		t.Fatalf("QGroupLimit clear: %v", err)
	}

	if _, ok := mock.Quotas["vol"]; ok {
		t.Fatal("expected quota to be removed for 0")
	}
}

// --- ModifyFilesystem (BtrFS level) tests ---

func TestModifyFilesystemRename(t *testing.T) {
	mock := InitBtrFSMock()
	err := mock.CreateFilesystem(Filesystem{Name: "old"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("old", Filesystem{Name: "new", Quota: 0})
	if err != nil {
		t.Fatalf("ModifyFilesystem rename: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "new" {
		t.Fatalf("expected name %q, got %q", "new", fs[0].Name)
	}
}

func TestModifyFilesystemQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "vol", Quota: 2048})
	if err != nil {
		t.Fatalf("ModifyFilesystem quota: %v", err)
	}

	if controller.Quotas["vol"] != 2048 {
		t.Fatalf("expected quota 2048, got %d", controller.Quotas["vol"])
	}
}

func TestModifyFilesystemRenameAndQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "old"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("old", Filesystem{Name: "new", Quota: 4096})
	if err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 || fs[0].Name != "new" {
		t.Fatalf("expected filesystem named %q, got %v", "new", fs)
	}

	if controller.Quotas["new"] != 4096 {
		t.Fatalf("expected quota 4096 on new name, got %d", controller.Quotas["new"])
	}
}

func TestModifyFilesystemInvalidName(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "vol"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "/bad", Quota: 0})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "..", Quota: 0})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "", Quota: 0})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestModifyFilesystemClearQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 1024})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	callsBefore := len(controller.GetLog())

	err = mock.ModifyFilesystem("vol", Filesystem{Name: "vol", Quota: 0})
	if err != nil {
		t.Fatalf("ModifyFilesystem clear quota: %v", err)
	}

	calls := controller.GetLog()[callsBefore:]
	foundQGroupShow := false
	foundQGroupLimit := false
	for _, c := range calls {
		if c.Operation == "QGroupShow" {
			foundQGroupShow = true
		}
		if c.Operation == "QGroupLimit" {
			foundQGroupLimit = true
			if c.Arguments[1] != uint64(0) {
				t.Fatalf("expected QGroupLimit called with 0, got %v", c.Arguments[1])
			}
		}
	}
	if !foundQGroupShow {
		t.Fatal("expected QGroupShow call when clearing quota")
	}
	if !foundQGroupLimit {
		t.Fatal("expected QGroupLimit call with 0 when clearing quota")
	}
}

func TestCreateFilesystemValidatesName(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "/bad"})
	if err == nil {
		t.Fatal("expected error for leading slash")
	}

	err = mock.CreateFilesystem(Filesystem{Name: ".."})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = mock.CreateFilesystem(Filesystem{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateFilesystemWithQuota(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 8192})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if controller.Quotas["vol"] != 8192 {
		t.Fatalf("expected quota 8192, got %d", controller.Quotas["vol"])
	}
}

func TestMockQGroupShow(t *testing.T) {
	mock := InitBtrFSMockController()

	err := mock.SubvolCreate("vol")
	if err != nil {
		t.Fatalf("SubvolCreate: %v", err)
	}

	err = mock.QGroupLimit("vol", 4096)
	if err != nil {
		t.Fatalf("QGroupLimit: %v", err)
	}

	val, err := mock.QGroupShow("vol")
	if err != nil {
		t.Fatalf("QGroupShow: %v", err)
	}
	if val != 4096 {
		t.Fatalf("expected 4096, got %d", val)
	}

	val, err = mock.QGroupShow("nonexistent")
	if err != nil {
		t.Fatalf("QGroupShow nonexistent: %v", err)
	}
	if val != 0 {
		t.Fatalf("expected 0 for missing, got %d", val)
	}
}

func TestListFilesystemsReturnsQuota(t *testing.T) {
	mock := InitBtrFSMock()

	err := mock.CreateFilesystem(Filesystem{Name: "vol", Quota: 2048})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	fs, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Quota != 2048 {
		t.Fatalf("expected quota 2048, got %d", fs[0].Quota)
	}
}

func TestCreateFilesystemNestedPath(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "a/b/c"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// All 3 subvolumes should exist
	err = controller.IsSubvolume("a")
	if err != nil {
		t.Fatalf("expected intermediate 'a' to exist: %v", err)
	}
	err = controller.IsSubvolume("a/b")
	if err != nil {
		t.Fatalf("expected intermediate 'a/b' to exist: %v", err)
	}
	err = controller.IsSubvolume("a/b/c")
	if err != nil {
		t.Fatalf("expected leaf 'a/b/c' to exist: %v", err)
	}
}

func TestCreateFilesystemNestedPartialExists(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	// Pre-create 'a'
	err := controller.SubvolCreate("a")
	if err != nil {
		t.Fatalf("SubvolCreate a: %v", err)
	}

	err = mock.CreateFilesystem(Filesystem{Name: "a/b/c"})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// Count SubvolCreate calls — 'a' was pre-created, so only 'a/b' and 'a/b/c' should be new
	createCount := 0
	for _, c := range controller.GetLog() {
		if c.Operation == "SubvolCreate" {
			createCount++
		}
	}
	// 1 for pre-create of 'a', 1 for 'a/b', 1 for 'a/b/c' = 3
	if createCount != 3 {
		t.Fatalf("expected 3 SubvolCreate calls (1 pre-create + 2 new), got %d", createCount)
	}
}

func TestCreateFilesystemNestedQuotaOnlyOnLeaf(t *testing.T) {
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}

	err := mock.CreateFilesystem(Filesystem{Name: "a/b/c", Quota: 1024})
	if err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	// Quota should only be on the leaf
	if controller.Quotas["a/b/c"] != 1024 {
		t.Fatalf("expected quota 1024 on leaf, got %d", controller.Quotas["a/b/c"])
	}

	if _, ok := controller.Quotas["a"]; ok {
		t.Fatal("intermediate 'a' should not have quota")
	}

	if _, ok := controller.Quotas["a/b"]; ok {
		t.Fatal("intermediate 'a/b' should not have quota")
	}
}

// --- Purge simulation tests ---

func TestListFilesystemsPrefixFilter(t *testing.T) {
	mock := InitBtrFSMock()

	// Create volumes for two packages with similar-prefix names.
	for _, name := range []string{"nginx/html", "nginx/logs", "nginx2/data"} {
		err := mock.CreateFilesystem(Filesystem{Name: name})
		if err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// List with trailing-slash prefix: should return only nginx children, not
	// the parent "nginx" itself and not "nginx2" or its children.
	fs, err := mock.ListFilesystems("nginx/")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	names := map[string]bool{}
	for _, f := range fs {
		names[f.Name] = true
	}

	if !names["nginx/html"] {
		t.Fatal("expected nginx/html in prefix-filtered list")
	}
	if !names["nginx/logs"] {
		t.Fatal("expected nginx/logs in prefix-filtered list")
	}
	if names["nginx"] {
		t.Fatal("parent nginx should not appear when filtering by nginx/")
	}
	if names["nginx2"] || names["nginx2/data"] {
		t.Fatal("nginx2 volumes should not appear when filtering by nginx/")
	}
}

func TestPurgeSimulation(t *testing.T) {
	mock := InitBtrFSMock()

	// Create a deep hierarchy mimicking a package with nested volumes.
	for _, name := range []string{"pkg/a", "pkg/a/b", "pkg/a/b/c"} {
		err := mock.CreateFilesystem(Filesystem{Name: name})
		if err != nil {
			t.Fatalf("CreateFilesystem %q: %v", name, err)
		}
	}

	// Also create a sibling package to verify it survives.
	err := mock.CreateFilesystem(Filesystem{Name: "other/data"})
	if err != nil {
		t.Fatalf("CreateFilesystem other/data: %v", err)
	}

	// Simulate the purge algorithm: list children, sort deepest-first, remove.
	prefix := "pkg/"
	filesystems, err := mock.ListFilesystems(prefix)
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	sort.Slice(filesystems, func(i, j int) bool {
		return strings.Count(filesystems[i].Name, "/") > strings.Count(filesystems[j].Name, "/")
	})

	// Verify deepest-first ordering.
	if len(filesystems) < 2 {
		t.Fatalf("expected at least 2 filesystems, got %d", len(filesystems))
	}
	for i := 1; i < len(filesystems); i++ {
		depthPrev := strings.Count(filesystems[i-1].Name, "/")
		depthCurr := strings.Count(filesystems[i].Name, "/")
		if depthPrev < depthCurr {
			t.Fatalf("sort order wrong: %q (depth %d) before %q (depth %d)",
				filesystems[i-1].Name, depthPrev, filesystems[i].Name, depthCurr)
		}
	}

	// Remove children deepest-first.
	for _, fs := range filesystems {
		err := mock.RemoveFilesystem(fs.Name)
		if err != nil {
			t.Fatalf("RemoveFilesystem %q: %v", fs.Name, err)
		}
	}

	// Remove the parent package volume itself.
	err = mock.RemoveFilesystem("pkg")
	if err != nil {
		t.Fatalf("RemoveFilesystem pkg: %v", err)
	}

	// Verify pkg is completely gone.
	remaining, err := mock.ListFilesystems("")
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	for _, fs := range remaining {
		if fs.Name == "pkg" || strings.HasPrefix(fs.Name, "pkg/") {
			t.Fatalf("expected all pkg volumes purged, found %q", fs.Name)
		}
	}

	// Verify other package survived.
	found := map[string]bool{}
	for _, fs := range remaining {
		found[fs.Name] = true
	}
	if !found["other"] {
		t.Fatal("expected other parent volume to survive purge")
	}
	if !found["other/data"] {
		t.Fatal("expected other/data volume to survive purge")
	}
}
