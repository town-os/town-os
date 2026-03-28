// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Reconciliation tests ---

func TestReconcileAfterInstall(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	// Add a repository and install a package via the API.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify the install created systemd units.
	// nginx 1.0 has 1 external port (8080->80) and 1 volume:
	//   InstallUnit(service) + InstallUnit(socket) + InstallUnit(networkcontroller) +
	//   Enable(socket) + Enable(networkcontroller) + Start(service) = 6
	installCalls := sd.GetCalls()
	if len(installCalls) != 6 {
		t.Fatalf("expected 6 systemd calls from install, got %d", len(installCalls))
	}

	// Simulate a container restart: clear the mock systemd state.
	// The installed state (symlinks + responses) persists but units are gone.
	sd.Calls = nil

	// Run reconciliation.
	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify reconciliation re-created all units (same 6 calls).
	calls := sd.GetCalls()
	if len(calls) != 6 {
		t.Fatalf("expected 6 systemd calls from reconcile, got %d: %v", len(calls), calls)
	}

	if calls[0].Method != "InstallUnit" {
		t.Fatalf("call 0: expected InstallUnit, got %q", calls[0].Method)
	}
	unitName, ok := calls[0].Args[0].(string)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if unitName != "town-os-package--core-nginx-1.0.service" {
		t.Fatalf("expected unit town-os-package--core-nginx-1.0.service, got %s", unitName)
	}

	// Last call should be Start for the service.
	lastCall := calls[len(calls)-1]
	lastAction, ok := lastCall.Args[1].(systemd.StatusAction)
	if !ok {
		t.Fatal("type assertion failed")
	}
	if lastCall.Method != "SetStatus" || lastAction != systemd.Start {
		t.Fatalf("last call: expected SetStatus Start, got %s %v", lastCall.Method, lastCall.Args)
	}

	// Verify installed state is intact.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 1 || pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected [core/nginx@1.0], got %v", pkgs.Entries)
	}
}

func TestReconcileMultiplePackagesAfterInstall(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{"port": "6379", "password": "secret", "maxmemory": "100mb"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()
	// nginx (1 ext port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// redis (1 int port): 3 InstallUnit + 2 Enable + 1 Start = 6
	// Total = 12
	if len(calls) != 12 {
		t.Fatalf("expected 12 systemd calls, got %d", len(calls))
	}
}

func TestReconcilePreservesResponses(t *testing.T) {
	t.Parallel()
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	responses := packages.Responses{"hostname": "myhost", "port": "9090"}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", responses, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify responses are still accessible via the API.
	resp, err := c.GetResponses(context.TODO(), "core", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetResponses: %v", err)
	}
	if resp["hostname"] != "myhost" {
		t.Fatalf("expected hostname myhost, got %s", resp["hostname"])
	}
	if resp["port"] != "9090" {
		t.Fatalf("expected port 9090, got %s", resp["port"])
	}
}
