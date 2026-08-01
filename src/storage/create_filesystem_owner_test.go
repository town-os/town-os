// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package storage

import (
	"errors"
	"path/filepath"
	"testing"
)

func ownerTestMock(t *testing.T) (*BtrFS, *MockBtrFSController) {
	t.Helper()
	mock := InitBtrFSMock()
	controller, ok := mock.Controller.(*MockBtrFSController)
	if !ok {
		t.Fatal("expected *MockBtrFSController")
	}
	return mock, controller
}

func chownCalls(controller *MockBtrFSController) []Call {
	var out []Call
	for _, c := range controller.GetLog() {
		if c.Operation == "Chown" {
			out = append(out, c)
		}
	}
	return out
}

// TestCreateFilesystemAppliesDeclaredOwnership is the behavior gfeh depends on:
// it runs as a fixed non-root uid and must write into a subvolume the
// systemcontroller created as root. A bind mount passes host ownership straight
// through, so without this the daemon starts cleanly and fails on its first
// write.
func TestCreateFilesystemAppliesDeclaredOwnership(t *testing.T) {
	mock, controller := ownerTestMock(t)

	uid, gid := uint32(2000), uint32(2000)
	if err := mock.CreateFilesystem(Filesystem{Name: "gfeh/photos", UID: &uid, GID: &gid}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	calls := chownCalls(controller)
	if len(calls) != 1 {
		t.Fatalf("got %d Chown calls, want exactly 1 (the subvolume root, not its parents)", len(calls))
	}
	got, ok := controller.GetOwners()[filepath.Join(mock.BasePath, "gfeh/photos")]
	if !ok {
		t.Fatalf("no ownership recorded; owners = %v", controller.GetOwners())
	}
	if got.UID != uid || got.GID != gid {
		t.Errorf("owner = %d:%d, want %d:%d", got.UID, got.GID, uid, gid)
	}
}

// TestCreateFilesystemDoesNotChownIntermediates pins the blast radius. Only the
// partition is handed over; the gfeh/ root stays root-owned so a daemon cannot
// create sibling partitions next to its own.
func TestCreateFilesystemDoesNotChownIntermediates(t *testing.T) {
	mock, controller := ownerTestMock(t)

	uid, gid := uint32(2000), uint32(2000)
	if err := mock.CreateFilesystem(Filesystem{Name: "gfeh/photos", UID: &uid, GID: &gid}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if _, ok := controller.GetOwners()[filepath.Join(mock.BasePath, "gfeh")]; ok {
		t.Error("the object-storage root was chowned; only the partition should be")
	}
}

// TestCreateFilesystemLeavesOwnershipAloneWhenUnset guards every existing
// caller. UID/GID are pointers precisely so "not specified" is distinct from
// "root", and a nil pair must produce no chown at all.
func TestCreateFilesystemLeavesOwnershipAloneWhenUnset(t *testing.T) {
	mock, controller := ownerTestMock(t)

	if err := mock.CreateFilesystem(Filesystem{Name: "user/notes"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	if calls := chownCalls(controller); len(calls) != 0 {
		t.Errorf("got %d Chown calls for a filesystem with no declared owner, want 0", len(calls))
	}
}

// TestCreateFilesystemIgnoresAHalfSpecifiedOwner pins the both-or-neither rule.
// A uid with no gid is a caller mistake, and quietly handing the subvolume to
// group 0 is worse than doing nothing.
func TestCreateFilesystemIgnoresAHalfSpecifiedOwner(t *testing.T) {
	for _, tc := range []struct {
		name string
		fs   Filesystem
	}{
		{"uid only", Filesystem{Name: "user/half", UID: ptrU32(2000)}},
		{"gid only", Filesystem{Name: "user/other", GID: ptrU32(2000)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock, controller := ownerTestMock(t)
			if err := mock.CreateFilesystem(tc.fs); err != nil {
				t.Fatalf("CreateFilesystem: %v", err)
			}
			if calls := chownCalls(controller); len(calls) != 0 {
				t.Errorf("got %d Chown calls, want 0", len(calls))
			}
		})
	}
}

// TestCreateFilesystemChownsRootZero is the reason UID/GID are pointers rather
// than values: uid 0 is a legitimate owner, and a value type could not tell it
// apart from "unset".
func TestCreateFilesystemChownsRootZero(t *testing.T) {
	mock, controller := ownerTestMock(t)

	zero := uint32(0)
	if err := mock.CreateFilesystem(Filesystem{Name: "user/rooted", UID: &zero, GID: &zero}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	if calls := chownCalls(controller); len(calls) != 1 {
		t.Fatalf("got %d Chown calls for an explicit 0:0, want 1", len(calls))
	}
}

// TestCreateFilesystemFailsWhenOwnershipCannotBeApplied: a subvolume whose
// owner could not be set is one the service cannot write to, and reporting
// success would defer the failure to the first write.
func TestCreateFilesystemFailsWhenOwnershipCannotBeApplied(t *testing.T) {
	mock, controller := ownerTestMock(t)
	controller.ChownErr = errors.New("operation not permitted")

	uid, gid := uint32(2000), uint32(2000)
	err := mock.CreateFilesystem(Filesystem{Name: "gfeh/photos", UID: &uid, GID: &gid})
	if err == nil {
		t.Fatal("CreateFilesystem succeeded despite a failed chown")
	}
}

// TestCreateFilesystemOwnershipSurvivesAQuota covers the ordering: the chown
// runs after QuotaEnable/QGroupLimit, so a quota'd partition still ends up
// owned by the service that has to fill it.
func TestCreateFilesystemOwnershipSurvivesAQuota(t *testing.T) {
	mock, controller := ownerTestMock(t)

	uid, gid := uint32(2000), uint32(2000)
	if err := mock.CreateFilesystem(Filesystem{Name: "gfeh/limited", Quota: 4096, UID: &uid, GID: &gid}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	got, ok := controller.GetOwners()[filepath.Join(mock.BasePath, "gfeh/limited")]
	if !ok {
		t.Fatal("no ownership recorded for a quota'd subvolume")
	}
	if got.UID != uid || got.GID != gid {
		t.Errorf("owner = %d:%d, want %d:%d", got.UID, got.GID, uid, gid)
	}
	if controller.Quotas[filepath.Join(mock.BasePath, "gfeh/limited")] != 4096 {
		t.Errorf("quota = %d, want 4096", controller.Quotas[filepath.Join(mock.BasePath, "gfeh/limited")])
	}
}

// TestModifyFilesystemRepairsDriftedOwnership: Modify is the idempotent path
// reconcile takes for a subvolume that already exists, so it is the only place
// an owner set wrong (or set before the uid was declared) can ever be fixed —
// CreateFilesystem never runs again for that partition.
func TestModifyFilesystemRepairsDriftedOwnership(t *testing.T) {
	mock, controller := ownerTestMock(t)

	if err := mock.CreateFilesystem(Filesystem{Name: "gfeh/photos"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}
	uid, gid := uint32(2000), uint32(2000)
	if err := mock.ModifyFilesystem("gfeh/photos", Filesystem{Name: "gfeh/photos", UID: &uid, GID: &gid}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	got, ok := controller.GetOwners()[filepath.Join(mock.BasePath, "gfeh/photos")]
	if !ok {
		t.Fatal("ModifyFilesystem did not re-assert ownership")
	}
	if got.UID != uid || got.GID != gid {
		t.Errorf("owner = %d:%d, want %d:%d", got.UID, got.GID, uid, gid)
	}
}

func ptrU32(v uint32) *uint32 { return &v }
