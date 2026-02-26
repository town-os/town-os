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
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "test-vol" {
		t.Fatalf("expected filesystem name %q, got %q", "test-vol", fs[0].Name)
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
	if len(fs) != 3 {
		t.Fatalf("expected 3 filesystems, got %d", len(fs))
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
