package systemcontroller

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"gitea.com/town-os/town-os/src/storage"
)

func TestRemoveFilesystem(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "test-vol"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "test-vol", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "test-vol"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 0 {
		t.Fatalf("expected 0 filesystems after removal, got %d", len(fs))
	}
}

func TestRemoveFilesystemPreservesOthers(t *testing.T) {
	c, controller := initTestClient(t)

	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "keep"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "keep", err)
	}
	if err := c.CreateFilesystem(context.TODO(), storage.Filesystem{Name: "remove"}); err != nil {
		t.Fatalf("CreateFilesystem %q: %v", "remove", err)
	}

	if err := c.RemoveFilesystem(context.TODO(), "remove"); err != nil {
		t.Fatalf("RemoveFilesystem %q: %v", "remove", err)
	}

	fs := controller.GetFilesystems()
	if len(fs) != 1 {
		t.Fatalf("expected 1 filesystem, got %d", len(fs))
	}

	if fs[0].Name != "keep" {
		t.Fatalf("expected remaining filesystem %q, got %q", "keep", fs[0].Name)
	}
}

func TestRemoveFilesystemBadJSON(t *testing.T) {
	mock := storage.InitBtrFSMock()
	ts := InitTestServer(ServerConfig{Storage: mock})
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.TODO(), http.MethodPost, testRoute(t, ts.Server.URL, "storage/remove"), bytes.NewBufferString("{bad"))
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

func TestRemoveFilesystemRejectsRoot(t *testing.T) {
	c, _ := initTestClient(t)

	err := c.RemoveFilesystem(context.TODO(), "")
	if err == nil {
		t.Fatal("expected error when removing root filesystem, got nil")
	}
}

func TestRemoveFilesystemRejectsReserved(t *testing.T) {
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
			err := c.RemoveFilesystem(context.TODO(), name)
			if err == nil {
				t.Fatalf("expected error when removing reserved filesystem %q", name)
			}
		})
	}
}
