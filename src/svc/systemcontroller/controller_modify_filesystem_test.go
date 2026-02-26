package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestModifyFilesystemQuota(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "test-vol", storage.Filesystem{Name: "test-vol", Quota: 1024}); err != nil {
		t.Fatalf("ModifyFilesystem quota: %v", err)
	}

	if controller.Quotas["test-vol"] != 1024 {
		t.Fatalf("expected quota 1024, got %d", controller.Quotas["test-vol"])
	}
}

func TestModifyFilesystemRename(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "old-vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "old-vol", storage.Filesystem{Name: "new-vol", Quota: 0}); err != nil {
		t.Fatalf("ModifyFilesystem rename: %v", err)
	}

	fsResult, err := c.ListFilesystems(context.TODO(), "", "", ListParams{})
	if err != nil {
		t.Fatalf("ListFilesystems: %v", err)
	}

	found := false
	for _, f := range fsResult.Entries {
		if f.Name == "new-vol" {
			found = true
		}
		if f.Name == "old-vol" {
			t.Fatal("old name should not exist after rename")
		}
	}
	if !found {
		t.Fatal("renamed filesystem not found")
	}
}

func TestModifyFilesystemRenameAndQuota(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	if err := c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "renamed", Quota: 2048}); err != nil {
		t.Fatalf("ModifyFilesystem: %v", err)
	}

	if controller.Quotas["renamed"] != 2048 {
		t.Fatalf("expected quota 2048 on renamed, got %d", controller.Quotas["renamed"])
	}
}

func TestModifyFilesystemInvalidName(t *testing.T) {
	c, _ := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "vol"}); err != nil {
		t.Fatalf("CreateFilesystem: %v", err)
	}

	err := c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "/bad"})
	if err == nil {
		t.Fatal("expected error for leading slash")
	}

	err = c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: ".."})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}

	err = c.ModifyFilesystem(context.TODO(), "vol", storage.Filesystem{Name: "has space"})
	if err == nil {
		t.Fatal("expected error for space in name")
	}
}

func TestModifyFilesystemRejectsRoot(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.ModifyFilesystem(context.TODO(), "", storage.Filesystem{Name: "test"})
	if err == nil {
		t.Fatal("expected error when modifying root filesystem")
	}
}

func TestModifyFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "storage/modify"), bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.Server.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("resp.Body.Close: %v", err)
		}
	}()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected non-200 status for bad JSON")
	}
}

func TestModifyInstalledFilesystemQuota(t *testing.T) {
	c, controller := initTestClient(t)

	injectSubvol(t, controller, "installed/repo/pkg/1.0/data", 0)

	if err := c.ModifyFilesystem(context.TODO(), "installed/repo/pkg/1.0/data", storage.Filesystem{
		Name:  "installed/repo/pkg/1.0/data",
		Quota: 4096,
	}); err != nil {
		t.Fatalf("ModifyFilesystem quota on installed volume: %v", err)
	}

	if controller.Quotas["installed/repo/pkg/1.0/data"] != 4096 {
		t.Fatalf("expected quota 4096, got %d", controller.Quotas["installed/repo/pkg/1.0/data"])
	}
}

func TestModifyInstalledFilesystemRename(t *testing.T) {
	c, controller := initTestClient(t)

	injectSubvol(t, controller, "installed/repo/pkg/1.0/data", 0)

	if err := c.ModifyFilesystem(context.TODO(), "installed/repo/pkg/1.0/data", storage.Filesystem{
		Name: "installed/repo/pkg/1.0/renamed",
	}); err != nil {
		t.Fatalf("ModifyFilesystem rename on installed volume: %v", err)
	}
}
