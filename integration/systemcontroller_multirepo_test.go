// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// --- Multi-repo integration tests ---

func TestSystemControllerMultiRepoListInstalled(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Install nginx from core and mosquitto from extras.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "mosquitto", "2.0", packages.Responses{"port": "1883"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage mosquitto@2.0: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed, got %d", len(pkgs.Entries))
	}

	found := map[string]bool{}
	for _, p := range pkgs.Entries {
		found[p] = true
	}
	if !found["core/nginx@1.0"] {
		t.Fatalf("expected core/nginx@1.0 in installed list, got %v", pkgs.Entries)
	}
	if !found["extras/mosquitto@2.0"] {
		t.Fatalf("expected extras/mosquitto@2.0 in installed list, got %v", pkgs.Entries)
	}
}

func TestSystemControllerListPackagesByRepoIntegration(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	groups, err := c.ListPackagesByRepo(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListPackagesByRepo: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Highest-precedence repo (extras, last added) comes first.
	if groups[0].Repo != "extras" {
		t.Fatalf("expected first group to be extras, got %s", groups[0].Repo)
	}
	if groups[1].Repo != "core" {
		t.Fatalf("expected second group to be core, got %s", groups[1].Repo)
	}

	// Extras has mosquitto and postgres.
	if len(groups[0].Packages) != 2 {
		t.Fatalf("expected 2 packages in extras, got %d", len(groups[0].Packages))
	}
	// Core has demo-nginx, nginx, redis.
	if len(groups[1].Packages) != 3 {
		t.Fatalf("expected 3 packages in core, got %d", len(groups[1].Packages))
	}
}

func TestSystemControllerMultiRepoUninstallIsolation(t *testing.T) {
	c, _ := initSystemControllerInstallTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := addRepoWithCreds(c, "extras", testExtrasURLString()); err != nil {
		t.Fatalf("AddRepository extras: %v", err)
	}

	// Install from both repos.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "alpha", "port": "80"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "mosquitto", "2.0", packages.Responses{"port": "1883"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage mosquitto@2.0: %v", err)
	}

	// Uninstall only core/nginx.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage core/nginx: %v", err)
	}

	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed after uninstall, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "extras/mosquitto@2.0" {
		t.Fatalf("expected extras/mosquitto@2.0, got %s", pkgs.Entries[0])
	}
}
