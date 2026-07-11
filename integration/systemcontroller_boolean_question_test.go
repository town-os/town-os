// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initBooleanQuestionTest provisions a local repository holding one package
// with two boolean questions: one with no default (off unless answered) and one
// defaulting to true. Both are substituted into the environment and into a file
// template, so a single install exercises every path a boolean answer travels.
func initBooleanQuestionTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, string) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repo list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := `image: alpine:3.20
description: "boolean question test"
environment:
  REGISTRATION_OPEN: "@open@"
  METRICS_ENABLED: "@metrics@"
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
questions:
  open:
    query: "Allow open registration?"
    type: boolean
  metrics:
    query: "Enable metrics?"
    type: boolean
    default: "true"
templates:
  conf:
    volume: config
    path: app.conf
    content: "open={{.Responses.open}} metrics={{.Responses.metrics}}"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "boolpkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile package: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, sd, btrfsBase
}

// A checked box submits "true" and an unchecked box submits "false"; both must
// reach the container as an environment variable and a rendered template file.
func TestIntegrationBooleanQuestionInstall(t *testing.T) {
	t.Parallel()
	c, sd, btrfsBase := initBooleanQuestionTest(t)

	responses := packages.Responses{"open": "true", "metrics": "false"}
	if err := c.InstallPackage(context.TODO(), "boolpkg", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	unitName := systemd.UnitName("local", "boolpkg", "1.0")
	content := findInstalledUnitContent(t, sd, unitName)
	if content == "" {
		t.Fatalf("expected unit %s to be installed", unitName)
	}
	if !strings.Contains(content, "REGISTRATION_OPEN=true") {
		t.Fatalf("missing REGISTRATION_OPEN=true; unit:\n%s", content)
	}
	// An unchecked box must reach the container as an explicit "false", not as
	// an empty value and not silently overridden by the package's true default.
	if !strings.Contains(content, "METRICS_ENABLED=false") {
		t.Fatalf("missing METRICS_ENABLED=false; unit:\n%s", content)
	}

	conf := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "boolpkg", "1.0", "config", "app.conf")
	rendered, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("expected app.conf to be written: %v", err)
	}
	if string(rendered) != "open=true metrics=false" {
		t.Fatalf("app.conf = %q, want %q", string(rendered), "open=true metrics=false")
	}
}

// An unanswered boolean is the normal case for a dependency and for a checkbox
// the user never touched: it must fall back to the package default (or to
// false) rather than failing the install with an empty-response error.
func TestIntegrationBooleanQuestionUnansweredUsesDefault(t *testing.T) {
	t.Parallel()
	c, sd, btrfsBase := initBooleanQuestionTest(t)

	// Neither question is answered at all.
	if err := c.InstallPackage(context.TODO(), "boolpkg", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage with no responses: %v", err)
	}

	unitName := systemd.UnitName("local", "boolpkg", "1.0")
	content := findInstalledUnitContent(t, sd, unitName)
	if content == "" {
		t.Fatalf("expected unit %s to be installed", unitName)
	}
	// No default declared → false. Default of "true" → true.
	if !strings.Contains(content, "REGISTRATION_OPEN=false") {
		t.Fatalf("missing REGISTRATION_OPEN=false; unit:\n%s", content)
	}
	if !strings.Contains(content, "METRICS_ENABLED=true") {
		t.Fatalf("missing METRICS_ENABLED=true; unit:\n%s", content)
	}

	conf := filepath.Join(btrfsBase, systemcontroller.PackagesVolumePrefix, "local", "boolpkg", "1.0", "config", "app.conf")
	rendered, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("expected app.conf to be written: %v", err)
	}
	if string(rendered) != "open=false metrics=true" {
		t.Fatalf("app.conf = %q, want %q", string(rendered), "open=false metrics=true")
	}
}

// A non-boolean answer is rejected rather than reaching the container as a
// bogus string.
func TestIntegrationBooleanQuestionRejectsNonBoolean(t *testing.T) {
	t.Parallel()
	c, _, _ := initBooleanQuestionTest(t)

	responses := packages.Responses{"open": "maybe", "metrics": "true"}
	if err := c.InstallPackage(context.TODO(), "boolpkg", "1.0", responses, false, "", false); err == nil {
		t.Fatal("expected install to fail on a non-boolean response")
	}
}
