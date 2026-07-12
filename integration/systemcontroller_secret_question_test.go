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

// initSecretQuestionTest provisions a local repository holding one package with
// an auto-generated secret question that is substituted into BOTH an environment
// variable and a note. That pairing is the point: the info panel copies the
// stored response, while the note it displays is compiled from the same
// response, and the container runs on the compiled environment. A user who
// copies the secret out of the panel must end up with the string the container
// is actually running with -- so all three have to be the same value.
func initSecretQuestionTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, *packages.InstallManager, *packages.RepositoryRoot) {
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
description: "secret question test"
environment:
  ADMIN_TOKEN: "@admintoken@"
questions:
  admintoken:
    query: "Admin token"
    type: secret
    default: auto
notes:
  Admin token:
    value: "@admintoken@"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "secretpkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile package: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, sd, inst, rr
}

// An auto-generated secret is minted once and must land, unchanged, in all three
// places the user can meet it: the container's environment, the responses saved
// on disk (which the info panel masks and copies), and the compiled note the
// info panel prints. If these ever drift, a user copies one token and the server
// is running another.
func TestIntegrationSecretQuestionIsOneValueEverywhere(t *testing.T) {
	t.Parallel()
	c, sd, inst, rr := initSecretQuestionTest(t)

	// An empty response is what the install form sends for an untouched
	// auto-generating secret; the server mints the value.
	if err := c.InstallPackage(context.TODO(), "secretpkg", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	stored, err := inst.GetResponses("local", "secretpkg", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	token := stored["admintoken"]
	if token == "" {
		t.Fatal("no secret was generated and stored")
	}
	if token == "auto" {
		t.Fatal(`stored response is the literal "auto": the secret was never generated`)
	}

	// What the container runs with.
	unitName := systemd.UnitName("local", "secretpkg", "1.0")
	content := findInstalledUnitContent(t, sd, unitName)
	if content == "" {
		t.Fatalf("expected unit %s to be installed", unitName)
	}
	if !strings.Contains(content, "ADMIN_TOKEN="+token) {
		t.Fatalf("unit does not carry the stored secret as ADMIN_TOKEN; unit:\n%s", content)
	}

	// What the info panel prints, compiled from the same stored responses the
	// panel's masked field copies.
	ip, err := rr.LoadPackage("local", "secretpkg", "1.0")
	if err != nil {
		t.Fatalf("LoadPackage: %v", err)
	}
	notes, err := ip.CompileNotes(stored)
	if err != nil {
		t.Fatalf("CompileNotes: %v", err)
	}
	if notes["Admin token"] != token {
		t.Fatalf("note = %q, want the stored secret %q", notes["Admin token"], token)
	}
}

// A secret the user supplies must be kept verbatim -- never regenerated behind
// their back, which would leave them holding a token the server does not accept.
func TestIntegrationSecretQuestionKeepsSuppliedValue(t *testing.T) {
	t.Parallel()
	c, sd, inst, _ := initSecretQuestionTest(t)

	const supplied = "qw7m3T2jDytu7je3SMf0aBn9DYHC3sVc"
	if err := c.InstallPackage(context.TODO(), "secretpkg", "1.0",
		packages.Responses{"admintoken": supplied}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	stored, err := inst.GetResponses("local", "secretpkg", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if stored["admintoken"] != supplied {
		t.Fatalf("stored secret = %q, want %q", stored["admintoken"], supplied)
	}

	content := findInstalledUnitContent(t, sd, systemd.UnitName("local", "secretpkg", "1.0"))
	if !strings.Contains(content, "ADMIN_TOKEN="+supplied) {
		t.Fatalf("unit does not carry the supplied secret; unit:\n%s", content)
	}
}
