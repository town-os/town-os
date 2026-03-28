// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// findInstalledUnitContent finds the content of a specific unit from mock
// systemd InstallUnit calls.
func findInstalledUnitContent(t *testing.T, sd *systemd.MockManager, unitName string) string {
	t.Helper()
	for _, call := range sd.GetCalls() {
		if call.Method != "InstallUnit" {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed on InstallUnit arg 0")
		}
		if name == unitName {
			content, ok := call.Args[1].(string)
			if !ok {
				t.Fatal("type assertion failed on InstallUnit arg 1")
			}
			return content
		}
	}
	return ""
}

// --- Dependency install lifecycle tests ---

func TestInstallWithDependencyLifecycle(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install app-with-cache which depends on redis.
	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	// Verify both parent and dep are listed as installed.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(pkgs.Entries) != 2 {
		t.Fatalf("expected 2 installed (parent + dep), got %d: %v", len(pkgs.Entries), pkgs.Entries)
	}

	// Verify dep unit was installed with correct name.
	depEffName := packages.DependencyName("app-with-cache", "cache")
	depUnitName := systemd.UnitName("core", depEffName, "7.0")
	depContent := findInstalledUnitContent(t, sd, depUnitName)
	if depContent == "" {
		t.Fatalf("expected dep unit %s to be installed", depUnitName)
	}

	// Verify parent unit was installed.
	parentUnitName := systemd.UnitName("core", "app-with-cache", "1.0")
	parentContent := findInstalledUnitContent(t, sd, parentUnitName)
	if parentContent == "" {
		t.Fatalf("expected parent unit %s to be installed", parentUnitName)
	}

	// Dep must have PartOf and Before parent.
	if !strings.Contains(depContent, "PartOf="+parentUnitName) {
		t.Fatalf("dep missing PartOf parent, got:\n%s", depContent)
	}
	if !strings.Contains(depContent, "Before="+parentUnitName) {
		t.Fatalf("dep missing Before parent, got:\n%s", depContent)
	}

	// Parent must have Wants and After for dep.
	if !strings.Contains(parentContent, "Wants="+depUnitName) {
		t.Fatalf("parent missing Wants for dep, got:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "After="+depUnitName) {
		t.Fatalf("parent missing After for dep, got:\n%s", parentContent)
	}

	// Parent must have TOWNOS_DEP_CACHE_HOST env var.
	depContainerName := systemd.ContainerName("core", depEffName, "7.0")
	if !strings.Contains(parentContent, "TOWNOS_DEP_CACHE_HOST="+depContainerName) {
		t.Fatalf("parent missing TOWNOS_DEP_CACHE_HOST, got:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "TOWNOS_DEP_CACHE_PORT_6379=6379") {
		t.Fatalf("parent missing TOWNOS_DEP_CACHE_PORT_6379, got:\n%s", parentContent)
	}

	// Uninstall should remove both parent and dep.
	if err := c.UninstallPackage(context.TODO(), "core", "app-with-cache", "1.0", true); err != nil {
		t.Fatalf("UninstallPackage: %v", err)
	}

	pkgs2, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs2.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d: %v", len(pkgs2.Entries), pkgs2.Entries)
	}
}

// --- NC setup tests ---

func TestNCOwnsNetworkLifecycleForStandalonePackage(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	ncUnitName := systemd.NetworkControllerUnitName("core", "nginx", "1.0")
	ncContent := findInstalledUnitContent(t, sd, ncUnitName)
	if ncContent == "" {
		t.Fatalf("expected NC unit %s to be installed", ncUnitName)
	}

	networkName := systemd.NetworkName("core", "nginx", "1.0")
	svcName := systemd.UnitName("core", "nginx", "1.0")
	containerName := systemd.ContainerName("core", "nginx", "1.0")

	// NC must create the network.
	if !strings.Contains(ncContent, "podman network create "+networkName) {
		t.Fatalf("NC missing network create, got:\n%s", ncContent)
	}

	// NC must remove the network on stop.
	if !strings.Contains(ncContent, "podman network rm -f "+networkName) {
		t.Fatalf("NC missing network rm, got:\n%s", ncContent)
	}

	// NC must have PartOf (not BindsTo) and Before (not After) the service.
	if !strings.Contains(ncContent, "PartOf="+svcName) {
		t.Fatalf("NC missing PartOf, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "Before="+svcName) {
		t.Fatalf("NC missing Before, got:\n%s", ncContent)
	}
	if strings.Contains(ncContent, "BindsTo=") {
		t.Fatalf("NC must not have BindsTo (use PartOf), got:\n%s", ncContent)
	}
	if strings.Contains(ncContent, "After=") {
		t.Fatalf("NC must not have After (use Before), got:\n%s", ncContent)
	}

	// NC must join the podman network (not --net host).
	if !strings.Contains(ncContent, "--net "+networkName) {
		t.Fatalf("NC missing --net on podman network, got:\n%s", ncContent)
	}
	if strings.Contains(ncContent, "--net host") {
		t.Fatalf("NC must not use --net host, got:\n%s", ncContent)
	}

	// NC must have -p port mappings.
	if !strings.Contains(ncContent, "-p 8080:8080") {
		t.Fatalf("NC missing -p 8080:8080, got:\n%s", ncContent)
	}

	// NC must use --target-container (not --target-host).
	if !strings.Contains(ncContent, "--target-container "+containerName) {
		t.Fatalf("NC missing --target-container, got:\n%s", ncContent)
	}
	if strings.Contains(ncContent, "--target-host") {
		t.Fatalf("NC must not use --target-host, got:\n%s", ncContent)
	}

	// NC must NOT have podman inspect (no IP discovery).
	if strings.Contains(ncContent, "podman inspect") {
		t.Fatalf("NC must not use podman inspect, got:\n%s", ncContent)
	}

	// Service must NOT create or remove the network.
	svcContent := findInstalledUnitContent(t, sd, svcName)
	if svcContent == "" {
		t.Fatalf("expected service unit %s to be installed", svcName)
	}
	if strings.Contains(svcContent, "podman network create") {
		t.Fatalf("service must not create network, got:\n%s", svcContent)
	}
	if strings.Contains(svcContent, "podman network rm") {
		t.Fatalf("service must not remove network, got:\n%s", svcContent)
	}

	// Service must have After= for the NC.
	if !strings.Contains(svcContent, "After="+ncUnitName) {
		t.Fatalf("service missing After for NC, got:\n%s", svcContent)
	}
}

func TestNCBeforeDependenciesInChain(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage app-with-cache@1.0: %v", err)
	}

	parentUnitName := systemd.UnitName("core", "app-with-cache", "1.0")
	ncUnitName := systemd.NetworkControllerUnitName("core", "app-with-cache", "1.0")
	depEffName := packages.DependencyName("app-with-cache", "cache")
	depUnitName := systemd.UnitName("core", depEffName, "7.0")

	ncContent := findInstalledUnitContent(t, sd, ncUnitName)
	if ncContent == "" {
		t.Fatalf("expected NC unit %s to be installed", ncUnitName)
	}

	// NC must have Before= for both parent service and dependency.
	if !strings.Contains(ncContent, "Before="+parentUnitName) {
		t.Fatalf("NC missing Before for parent, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, depUnitName) {
		t.Fatalf("NC missing Before for dep %s, got:\n%s", depUnitName, ncContent)
	}

	// Dep must have After= for the NC.
	depContent := findInstalledUnitContent(t, sd, depUnitName)
	if depContent == "" {
		t.Fatalf("expected dep unit %s to be installed", depUnitName)
	}
	if !strings.Contains(depContent, ncUnitName) {
		t.Fatalf("dep missing NC %s in After, got:\n%s", ncUnitName, depContent)
	}

	// Dep must NOT create or remove the network (NC owns it).
	if strings.Contains(depContent, "podman network create") {
		t.Fatalf("dep must not create network, got:\n%s", depContent)
	}
	if strings.Contains(depContent, "podman network rm") {
		t.Fatalf("dep must not remove network, got:\n%s", depContent)
	}
}

func TestNCMultiplePortMappings(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// nginx has one external port. Install it.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	ncUnitName := systemd.NetworkControllerUnitName("core", "nginx", "1.0")
	ncContent := findInstalledUnitContent(t, sd, ncUnitName)
	if ncContent == "" {
		t.Fatalf("expected NC unit to be installed")
	}

	// Verify port mapping appears in NC unit.
	if !strings.Contains(ncContent, "-p 8080:8080") {
		t.Fatalf("NC missing -p 8080:8080, got:\n%s", ncContent)
	}
}

func TestNCNetworkStateContainsContainerName(t *testing.T) {
	t.Parallel()
	c, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Read the network state file and verify container_name is set.
	stateFile := netStateDir + "/core-nginx-1.0.json"
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	expectedContainerName := systemd.ContainerName("core", "nginx", "1.0")
	if !strings.Contains(string(data), expectedContainerName) {
		t.Fatalf("state file missing container_name %q, got:\n%s", expectedContainerName, string(data))
	}
}

func TestDependencyWithPortsGetsNoNC(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// app-with-cache has a redis dependency with ports.
	if err := c.InstallPackage(context.TODO(), "app-with-cache", "1.0", packages.Responses{
		"port":      "9090",
		"cachepass": "secret123",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Parent should have an NC unit.
	parentNCUnit := systemd.NetworkControllerUnitName("core", "app-with-cache", "1.0")
	if _, ok := sd.InstalledUnits[parentNCUnit]; !ok {
		t.Fatalf("parent should have NC unit %s", parentNCUnit)
	}

	// Dependency should NOT have its own NC unit.
	depEffName := packages.DependencyName("app-with-cache", "cache")
	depNCUnit := systemd.NetworkControllerUnitName("core", depEffName, "7.0")
	if _, ok := sd.InstalledUnits[depNCUnit]; ok {
		t.Fatalf("dependency must NOT have its own NC unit %s", depNCUnit)
	}

	// Dependency service should be installed though.
	depSvcUnit := systemd.UnitName("core", depEffName, "7.0")
	if _, ok := sd.InstalledUnits[depSvcUnit]; !ok {
		t.Fatalf("dependency service unit %s should be installed", depSvcUnit)
	}
}
