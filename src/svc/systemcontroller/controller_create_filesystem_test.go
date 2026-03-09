// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestCreateFilesystem(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := controller.GetFilesystems()
	// "user" root subvolume + "user/test-vol" = 2
	if len(fs) != 2 {
		t.Fatalf("expected 2 filesystems, got %d", len(fs))
	}

	found := false
	for _, f := range fs {
		if f.Name == "user/test-vol" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected filesystem %q to be present", "user/test-vol")
	}
}

func TestCreateFilesystemMultiple(t *testing.T) {
	c, controller := initTestClient(t)

	names := []string{"vol-a", "vol-b", "vol-c"}
	for _, name := range names {
		if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name}); err != nil {
			t.Fatalf("unexpected error creating %q: %v", name, err)
		}
	}

	fs := controller.GetFilesystems()
	// 3 user volumes + "user" root subvolume = 4
	if len(fs) != 4 {
		t.Fatalf("expected 4 filesystems, got %d", len(fs))
	}
}

func TestCreateFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "storage/create"), bytes.NewBufferString("{bad"))
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

func TestCreateFilesystemRejectsReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"installed",
		"installed/nginx",
		"installed/nginx/1.0/data",
		"uninstalled",
		"uninstalled/nginx",
		"uninstalled/nginx/1.0/data",
		"user",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name})
			if err == nil {
				t.Fatalf("expected error when creating reserved filesystem %q", name)
			}
		})
	}
}

func TestCreateFilesystemAllowsNonReserved(t *testing.T) {
	c, _ := initTestClient(t)

	for _, name := range []string{
		"my-installed",
		"installedbackup",
		"data",
		"user-vol",
	} {
		t.Run(name, func(t *testing.T) {
			err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: name})
			if err != nil {
				t.Fatalf("unexpected error creating %q: %v", name, err)
			}
		})
	}
}
