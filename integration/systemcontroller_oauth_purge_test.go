// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initOAuthPurgeTest builds a package shaped like plex: one oauth question whose
// answer is a vendor credential, and one ordinary question that is an operator
// preference. It hands back the InstallManager so the test can read what is
// actually left on disk after an uninstall -- the client API cannot, because the
// package is gone by then.
//
// The flow's URLs are never fetched here. Compile only runs ValidateOAuthSpec,
// which checks the shape of a flow rather than whether it can be called, so the
// install path needs no provider standing by.
func initOAuthPurgeTest(t *testing.T) (*systemcontroller.SystemdClient, *packages.InstallManager) {
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

	const pkgYAML = `image: alpine:3.20
description: "oauth purge test"
environment:
  PLEX_TOKEN: "@plextoken@"
  MEDIA_SIZE: "@mediasize@"
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
questions:
  plextoken:
    query: "Plex account"
    type: oauth
    oauth:
      start:
        method: POST
        url: "https://plex.tv/api/v2/pins?strong=true"
      extract:
        id: id
        code: code
      approve: "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}"
      poll:
        url: "https://plex.tv/api/v2/pins/{{id}}"
      token: authToken
      interval: 2s
      timeout: 10m
  mediasize:
    query: "How much storage?"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "oauthpurge")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile package: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        systemd.InitMockManager(),
		BtrfsBasePath:  t.TempDir(),
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, inst
}

// Purging a package must forget the credential it was holding.
//
// A purge destroys the volumes, and with them the server identity the vendor
// credential was bound to. The answers, though, deliberately outlive an
// uninstall: the version file stays and the uninstall additionally copies it to
// responses/last so a reinstall does not re-interrogate the operator. For plex
// that combination is the trap -- the next install silently re-uses a token
// minted for a machine identity that no longer exists, comes up "not
// authorized", and the install dialog pre-fills the very answer the operator
// wanted to replace, so there is no way to ask for the flow again.
func TestIntegrationPurgeForgetsOAuthAnswer(t *testing.T) {
	t.Parallel()
	c, inst := initOAuthPurgeTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	responses := packages.Responses{"plextoken": "durable-auth-token", "mediasize": "10G"}
	if err := c.InstallPackage(ctx, "oauthpurge", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(ctx, "local", "oauthpurge", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}

	// responses/last is the store a reuse reinstall merges from, and the one the
	// uninstall just wrote.
	last, err := inst.LoadLastResponses("local", "oauthpurge")
	if err != nil {
		t.Fatalf("LoadLastResponses: %v", err)
	}
	if tok, ok := last["plextoken"]; ok {
		t.Fatalf("purge left the oauth answer in responses/last: %q", tok)
	}
	// The operator's own answers are not credentials and must survive, or every
	// purge becomes a full re-interrogation.
	if last["mediasize"] != "10G" {
		t.Fatalf("purge dropped a non-credential answer: %v", last)
	}
}

// The non-purging uninstall is the contrast: the volumes are kept, so the
// identity the credential belongs to is still there and the answer must be kept
// with it. Otherwise every ordinary uninstall/reinstall would demand the flow
// again for no reason.
func TestIntegrationUninstallWithoutPurgeKeepsOAuthAnswer(t *testing.T) {
	t.Parallel()
	c, inst := initOAuthPurgeTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	responses := packages.Responses{"plextoken": "durable-auth-token", "mediasize": "10G"}
	if err := c.InstallPackage(ctx, "oauthpurge", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if err := c.UninstallPackage(ctx, "local", "oauthpurge", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage without purge: %v", err)
	}

	last, err := inst.LoadLastResponses("local", "oauthpurge")
	if err != nil {
		t.Fatalf("LoadLastResponses: %v", err)
	}
	if last["plextoken"] != "durable-auth-token" {
		t.Fatalf("a non-purging uninstall lost the oauth answer: %v", last)
	}
}
