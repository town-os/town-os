// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/systemd"
)

func TestStartPagesService(t *testing.T) {
	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()

	if err := StartPagesService(context.Background(), sd, btrfsBase, ""); err != nil {
		t.Fatalf("StartPagesService: %v", err)
	}

	// The static Caddyfile serves /srv/<full-request-host> so each page's
	// FQDN-named directory is matched directly (no leftmost-label collision).
	cf := filepath.Join(btrfsBase, PagesServeCaddyDir, "Caddyfile")
	data, err := os.ReadFile(cf) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read Caddyfile: %v", err)
	}
	for _, want := range []string{"root * /srv/{host}", "file_server"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("pages Caddyfile missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "{page}") {
		t.Errorf("pages Caddyfile should no longer use the leftmost-label map:\n%s", data)
	}

	// The unit is installed, enabled, and started, joins the ingress network,
	// and mounts the webroot read-only at /srv.
	unitName := systemd.SystemServiceUnitName(PagesServiceKey)
	var install, enable, start bool
	var content string
	for _, c := range sd.GetCalls() {
		name, _ := c.Args[0].(string)
		if name != unitName {
			continue
		}
		switch c.Method {
		case "InstallUnit":
			install = true
			content, _ = c.Args[1].(string)
		case "SetStatus":
			switch c.Args[1] {
			case systemd.Enable:
				enable = true
			case systemd.Start:
				start = true
			}
		}
	}
	if !install || !enable || !start {
		t.Fatalf("expected install+enable+start for %s (install=%v enable=%v start=%v)", unitName, install, enable, start)
	}
	for _, want := range []string{"--net", systemd.IngressNetworkName, ":/srv:ro"} {
		if !strings.Contains(content, want) {
			t.Errorf("pages unit missing %q:\n%s", want, content)
		}
	}
}
