// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/storage"
)

// These tests pin the /gfeh/partitions/* CONTRACT. gfeh's Rust client parses
// these exact shapes and its emulator replicates them, so a drift here produces
// a green gfeh test suite and a 422 in production — which is the specific
// failure mode TOWNOS_CONTRACT.md exists to prevent. Treat a change that breaks
// one of these as a contract change, not a test to update.

func initGfehTestClient(t *testing.T) (*SystemdClient, *storage.BtrFS) {
	t.Helper()

	st := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: st})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, st
}

func gfehTestCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// TestGfehCreatePartitionAppliesThePrefix: the request carries a bare name and
// the response carries the volume name, because the prefix is Town OS's
// namespace artifact rather than part of the partition's identity. gfeh's
// Partition::from_volume strips it on the way back.
func TestGfehCreatePartitionAppliesThePrefix(t *testing.T) {
	c, _ := initGfehTestClient(t)

	got, err := c.CreateGfehPartition(gfehTestCtx(t), "photos", 1<<30)
	if err != nil {
		t.Fatalf("CreateGfehPartition: %v", err)
	}
	if got.Name != "gfeh/photos" {
		t.Errorf("name = %q, want gfeh/photos", got.Name)
	}
	if got.Quota != 1<<30 {
		t.Errorf("quota = %d, want %d", got.Quota, uint64(1)<<30)
	}
}

// TestGfehCreatePartitionSetsTheOwner is the reason storage.Filesystem grew
// UID/GID: a bind mount passes host ownership straight through, so a partition
// owned by root is one the unprivileged daemon cannot write to.
func TestGfehCreatePartitionSetsTheOwner(t *testing.T) {
	c, st := initGfehTestClient(t)

	if _, err := c.CreateGfehPartition(gfehTestCtx(t), "photos", 0); err != nil {
		t.Fatalf("CreateGfehPartition: %v", err)
	}

	controller, ok := st.Controller.(*storage.MockBtrFSController)
	if !ok {
		t.Fatal("expected *storage.MockBtrFSController")
	}
	owner, ok := controller.GetOwners()[filepath.Join(st.BasePath, "gfeh/photos")]
	if !ok {
		t.Fatalf("partition was not chowned; owners = %v", controller.GetOwners())
	}
	if owner.UID != gfeh.UID || owner.GID != gfeh.GID {
		t.Errorf("owner = %d:%d, want %d:%d", owner.UID, owner.GID, gfeh.UID, gfeh.GID)
	}
}

// TestGfehCreatePartitionConflicts: gfehd's provisioning is a create-or-resize,
// and it distinguishes the two by this status. A daemon whose own partition
// already existed on every start but the first would otherwise only ever be
// startable once.
func TestGfehCreatePartitionConflicts(t *testing.T) {
	c, _ := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	if _, err := c.CreateGfehPartition(ctx, "photos", 0); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := c.CreateGfehPartition(ctx, "photos", 0)
	if err == nil {
		t.Fatal("second create succeeded; want a conflict")
	}
	assertProblemStatus(t, err, http.StatusConflict)
}

// TestGfehModifyPartitionRequiresAnExistingOne and its remove twin: gfeh's
// client maps 404 onto Error::NotFound and branches on it.
func TestGfehModifyPartitionRequiresAnExistingOne(t *testing.T) {
	c, _ := initGfehTestClient(t)

	_, err := c.ModifyGfehPartition(gfehTestCtx(t), "absent", 1<<20)
	if err == nil {
		t.Fatal("modified a partition that does not exist")
	}
	assertProblemStatus(t, err, http.StatusNotFound)
}

func TestGfehRemovePartitionRequiresAnExistingOne(t *testing.T) {
	c, _ := initGfehTestClient(t)

	err := c.RemoveGfehPartition(gfehTestCtx(t), "absent")
	if err == nil {
		t.Fatal("removed a partition that does not exist")
	}
	assertProblemStatus(t, err, http.StatusNotFound)
}

func TestGfehModifyAndRemoveRoundTrip(t *testing.T) {
	c, _ := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	if _, err := c.CreateGfehPartition(ctx, "photos", 1<<30); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := c.ModifyGfehPartition(ctx, "photos", 2<<30)
	if err != nil {
		t.Fatalf("modify: %v", err)
	}
	if got.Name != "gfeh/photos" || got.Quota != 2<<30 {
		t.Errorf("modify returned %+v", got)
	}
	if err := c.RemoveGfehPartition(ctx, "photos"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	list, err := c.ListGfehPartitions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("partitions after remove = %v, want none", list)
	}
}

// TestGfehRejectsANameWithASeparator: gfehd refuses a partition name
// containing a separator, so Town OS must refuse it at the same boundary or the
// two disagree about what a legal partition is — and a name like
// "../user/something" would address a volume outside the object-storage root.
func TestGfehRejectsANameWithASeparator(t *testing.T) {
	c, _ := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	for _, name := range []string{"a/b", "../escape", "photos/"} {
		if _, err := c.CreateGfehPartition(ctx, name, 0); err == nil {
			t.Errorf("created a partition named %q", name)
		} else {
			assertProblemStatus(t, err, http.StatusBadRequest)
		}
	}
}

func TestGfehRejectsAnEmptyName(t *testing.T) {
	c, _ := initGfehTestClient(t)

	if _, err := c.CreateGfehPartition(gfehTestCtx(t), "   ", 0); err == nil {
		t.Fatal("created a partition with a blank name")
	}
}

// TestGfehListReturnsAPlainArray is the shape assertion that matters most.
// gfeh's list_partitions deserializes Vec<Filesystem> directly, so a paginated
// PageResult — which every other Town OS list endpoint returns — would fail to
// decode on the Rust side.
func TestGfehListReturnsAPlainArray(t *testing.T) {
	c, _ := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	if _, err := c.CreateGfehPartition(ctx, "photos", 1<<30); err != nil {
		t.Fatalf("create: %v", err)
	}

	body := rawPost(t, c, "gfeh/partitions")
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("response is not a JSON array; got %s", trimmed)
	}

	var out []storage.Filesystem
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		t.Fatalf("decode as an array: %v", err)
	}
	if len(out) != 1 || out[0].Name != "gfeh/photos" {
		t.Errorf("partitions = %+v, want one named gfeh/photos", out)
	}
}

// TestGfehListIsAnArrayWhenEmpty: an empty body of `null` is a decode error on
// the Rust side, where the field is Vec<Filesystem>.
func TestGfehListIsAnArrayWhenEmpty(t *testing.T) {
	c, _ := initGfehTestClient(t)

	if got := strings.TrimSpace(rawPost(t, c, "gfeh/partitions")); got != "[]" {
		t.Errorf("empty listing = %s, want []", got)
	}
}

// TestGfehListExcludesTheRootAndNestedPaths: a partition is exactly gfeh/<name>.
// The root itself is not one, and neither is anything deeper.
func TestGfehListExcludesTheRootAndNestedPaths(t *testing.T) {
	c, st := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	if err := st.CreateFilesystem(storage.Filesystem{Name: GfehVolumePrefix}); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := st.CreateFilesystem(storage.Filesystem{Name: "gfeh/photos"}); err != nil {
		t.Fatalf("create partition: %v", err)
	}
	if err := st.CreateFilesystem(storage.Filesystem{Name: "gfeh/photos/inner"}); err != nil {
		t.Fatalf("create nested: %v", err)
	}

	list, err := c.ListGfehPartitions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].Name != "gfeh/photos" {
		t.Errorf("partitions = %+v, want only gfeh/photos", list)
	}
}

// TestGfehPartitionsAreNotReachableThroughStorageCreate is the whole reason
// these routes exist: /storage/create rewrites every submitted name to
// user/<name>, so it cannot produce a partition, and the reserved prefix stops
// it from trying.
func TestGfehPartitionsAreNotReachableThroughStorageCreate(t *testing.T) {
	c, _ := initGfehTestClient(t)
	ctx := gfehTestCtx(t)

	for _, name := range []string{"gfeh", "gfeh/photos"} {
		if err := c.CreateFilesystem(ctx, storage.Filesystem{Name: name}); err == nil {
			t.Errorf("/storage/create accepted the reserved name %q", name)
		}
	}
}

// rawPost issues a POST with an empty JSON body and returns the raw response
// text, so a test can assert on the wire shape rather than on what a Go
// decoder was willing to accept.
func rawPost(t *testing.T, c *SystemdClient, path string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(gfehTestCtx(t), http.MethodPost, c.route(path), strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d: %s", path, resp.StatusCode, body)
	}
	return string(body)
}

// assertProblemStatus checks the HTTP status carried by a ProblemError, which
// is what gfeh's client branches on.
func assertProblemStatus(t *testing.T, err error, want int) {
	t.Helper()

	var pe *ProblemError
	if !errors.As(err, &pe) {
		t.Fatalf("err is not a *ProblemError: %v", err)
	}
	if pe.StatusCode() != want {
		t.Errorf("status = %d, want %d (err: %v)", pe.StatusCode(), want, err)
	}
}
