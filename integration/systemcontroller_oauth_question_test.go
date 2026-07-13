// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initOAuthQuestionTest stands up a provider that behaves like plex.tv -- a pin
// with a numeric id, and a null token until approval -- and a package whose
// oauth question names that provider's URLs. Nothing about the provider is known
// to the system controller; the package describes the whole flow.
func initOAuthQuestionTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager, *atomic.Bool) {
	t.Helper()

	approved := &atomic.Bool{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/pins", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"code":"wxyz"}`))
	})
	mux.HandleFunc("/api/v2/pins/42", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !approved.Load() {
			_, _ = w.Write([]byte(`{"authToken":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"authToken":"durable-auth-token"}`))
	})
	provider := httptest.NewServer(mux)
	t.Cleanup(provider.Close)

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
description: "oauth question test"
environment:
  PLEX_TOKEN: "@plextoken@"
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
        url: "` + provider.URL + `/api/v2/pins?strong=true"
      extract:
        id: id
        code: code
      approve: "https://app.plex.tv/auth#?clientID={{client_id}}&code={{code}}"
      poll:
        url: "` + provider.URL + `/api/v2/pins/{{id}}"
      token: authToken
      interval: 1s
      timeout: 5m
templates:
  conf:
    volume: config
    path: app.conf
    content: "token={{.Responses.plextoken}}"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "oauthpkg")
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
		// The provider is an httptest server on loopback. In production this stays
		// false and packages.CheckOAuthAddr refuses to let a package aim the
		// controller at the host's own network.
		OAuthAllowPrivate: true,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, sd, approved
}

// The whole point of the feature, end to end: the operator runs a device flow
// from the install dialog instead of running a script by hand, and the token it
// returns is what the container is installed with.
func TestIntegrationOAuthQuestionInstall(t *testing.T) {
	t.Parallel()
	c, sd, approved := initOAuthQuestionTest(t)
	ctx := context.TODO()

	start, err := c.StartOAuth(ctx, "local", "oauthpkg", "1.0", "plextoken")
	if err != nil {
		t.Fatalf("StartOAuth: %v", err)
	}
	if start.FlowID == "" {
		t.Fatal("no flow id")
	}
	// The approval link the UI opens is built from the start response.
	if !strings.Contains(start.ApproveURL, "code=wxyz") {
		t.Fatalf("approve URL %q carries no code", start.ApproveURL)
	}

	// Nothing has been approved yet, which is not an error -- it is the state the
	// dialog sits in while the operator is over at the provider.
	pending, err := c.PollOAuth(ctx, start.FlowID)
	if err != nil {
		t.Fatalf("PollOAuth: %v", err)
	}
	if pending.Status != "pending" {
		t.Fatalf("status = %q before approval, want pending", pending.Status)
	}

	approved.Store(true)

	done, err := c.PollOAuth(ctx, start.FlowID)
	if err != nil {
		t.Fatalf("PollOAuth: %v", err)
	}
	if done.Status != "approved" || done.Token != "durable-auth-token" {
		t.Fatalf("poll after approval = %+v, want the provider's token", done)
	}

	// The browser hands the token back as the question's answer, exactly as if it
	// had been typed in.
	responses := packages.Responses{"plextoken": done.Token}
	if err := c.InstallPackage(ctx, "oauthpkg", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	unitName := systemd.UnitName("local", "oauthpkg", "1.0")
	content := findInstalledUnitContent(t, sd, unitName)
	if !strings.Contains(content, "PLEX_TOKEN=durable-auth-token") {
		t.Fatalf("the token from the flow never reached the container; unit:\n%s", content)
	}
}

// An install whose oauth question was never answered has to fail rather than
// install a service with an empty credential.
func TestIntegrationOAuthQuestionUnansweredFails(t *testing.T) {
	t.Parallel()
	c, _, _ := initOAuthQuestionTest(t)

	err := c.InstallPackage(context.TODO(), "oauthpkg", "1.0", packages.Responses{}, false, "", false)
	if err == nil {
		t.Fatal("an unanswered oauth question installed anyway")
	}
}
