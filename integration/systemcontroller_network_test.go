// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Private network integration tests ---

func TestSystemControllerInstallRedisCommandInUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Find the InstallUnit call for the service unit and inspect its content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-redis-7.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-redis-7.0.service")
	}

	// Verify command args appear after the image name.
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("service unit missing command arg 'redis-server', got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "--bind") {
		t.Fatalf("service unit missing command arg '--bind', got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "0.0.0.0") {
		t.Fatalf("service unit missing command arg '0.0.0.0', got:\n%s", serviceContent)
	}

	// Verify private network (no -p, no --net host).
	if !strings.Contains(serviceContent, "--net town-os-net--") {
		t.Fatalf("service unit missing --net private network, got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatal("service unit should not have -p mappings (private network)")
	}
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host")
	}
}

func TestSystemControllerInstallNginxPrivateNetwork(t *testing.T) {
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

	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}

	// Private network mode: no -p, no --net host.
	if !strings.Contains(serviceContent, "--net town-os-net--") {
		t.Fatalf("service unit missing --net private network, got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatal("service unit should not have -p mappings")
	}
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host")
	}
}

func TestSystemControllerInstallNginxNetworkController(t *testing.T) {
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

	calls := sd.GetCalls()

	// Verify networkcontroller unit was installed.
	var ncContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				nc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				ncContent = nc
				break
			}
		}
	}
	if ncContent == "" {
		t.Fatal("expected InstallUnit call for network controller unit town-os-package--core-nginx-1.0-network.service")
	}
	if !strings.Contains(ncContent, "PartOf=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing PartOf, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "Before=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing Before, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "Description=Town OS Network Controller: core/nginx@1.0") {
		t.Fatalf("network controller missing description, got:\n%s", ncContent)
	}

	// Verify the network controller was enabled.
	ncEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" && action == systemd.Enable {
				ncEnabled = true
				break
			}
		}
	}
	if !ncEnabled {
		t.Fatal("expected network controller unit to be enabled")
	}

	// Verify the service unit has Wants= for the network controller.
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}
	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}
	if !strings.Contains(serviceContent, "Wants=town-os-package--core-nginx-1.0-network.service") {
		t.Fatalf("service unit missing Wants for network controller, got:\n%s", serviceContent)
	}
}

func TestSystemControllerInstallRedisNCForInternalPorts(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Redis has internal-only ports: NC should still be installed (all ports go through NC).
	calls := sd.GetCalls()
	ncInstalled := false
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				ncInstalled = true
				break
			}
		}
	}
	if !ncInstalled {
		t.Fatal("expected network controller unit installed for internal-only ports")
	}
}

func TestReconcileNetworkController(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
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

	// Find the reconciled service unit content.
	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service after reconcile")
	}

	// Verify private network after reconcile.
	if !strings.Contains(serviceContent, "--net town-os-net--") {
		t.Fatalf("reconciled unit missing '--net private network', got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatal("reconciled unit should not have -p mappings")
	}

	// Verify NC unit was installed during reconcile.
	var ncInstalled bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				ncInstalled = true
				break
			}
		}
	}
	if !ncInstalled {
		t.Fatal("expected network controller unit to be installed during reconcile")
	}

	// Verify NC was enabled.
	ncEnabled := false
	for _, call := range calls {
		if call.Method == "SetStatus" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			action, ok := call.Args[1].(systemd.StatusAction)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" && action == systemd.Enable {
				ncEnabled = true
				break
			}
		}
	}
	if !ncEnabled {
		t.Fatal("expected network controller unit to be enabled after reconcile")
	}
}

func TestSystemControllerInstallNginxNetworkLifecycle(t *testing.T) {
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

	calls := sd.GetCalls()
	var serviceContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0.service" {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				serviceContent = sc
				break
			}
		}
	}

	if serviceContent == "" {
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0.service")
	}

	networkName := systemd.NetworkName("core", "nginx", "1.0")

	// Service unit must NOT create or remove the network (NC owns it).
	if strings.Contains(serviceContent, "podman network create") {
		t.Fatalf("service unit must not create network (NC owns it), got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "podman network rm") {
		t.Fatalf("service unit must not remove network (NC owns it), got:\n%s", serviceContent)
	}

	// Service must have After= for the NC unit.
	ncUnitName := systemd.NetworkControllerUnitName("core", "nginx", "1.0")
	if !strings.Contains(serviceContent, "After="+ncUnitName) {
		t.Fatalf("service unit missing After for NC, got:\n%s", serviceContent)
	}

	// Find the NC unit and verify it owns network lifecycle.
	var ncContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == ncUnitName {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				ncContent = sc
				break
			}
		}
	}
	if ncContent == "" {
		t.Fatalf("expected InstallUnit call for %s", ncUnitName)
	}
	if !strings.Contains(ncContent, "podman network create "+networkName) {
		t.Fatalf("NC unit missing network create, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "podman network rm -f "+networkName) {
		t.Fatalf("NC unit missing network rm -f, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "--target-container town-os-package--core-nginx-1.0") {
		t.Fatalf("NC unit missing --target-container, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "-p 8080:8080") {
		t.Fatalf("NC unit missing -p port mapping, got:\n%s", ncContent)
	}
}

// --- Network state file integration tests ---

func TestSystemControllerInstallNginxNetworkState(t *testing.T) {
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

	// Verify the network state file was written.
	statePath := filepath.Join(netStateDir, "core-nginx-1.0.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read network state file: %v", err)
	}

	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}

	// Nginx has external 8080→80: should have Forward=true, UPnP=true.
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port in state, got %d", len(state.Ports))
	}
	if state.Ports[0].ExternalPort != 8080 {
		t.Fatalf("expected external_port 8080, got %d", state.Ports[0].ExternalPort)
	}
	if state.Ports[0].InternalPort != 80 {
		t.Fatalf("expected internal_port 80, got %d", state.Ports[0].InternalPort)
	}
	if !state.Ports[0].UPnP {
		t.Fatal("expected UPnP=true for external port")
	}
	if !state.Ports[0].Forward {
		t.Fatal("expected Forward=true for all ports")
	}
}

func TestSystemControllerInstallRedisNetworkState(t *testing.T) {
	c, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Redis has internal-only ports: network state file should be written
	// with Forward=true (all ports go through NC now).
	statePath := filepath.Join(netStateDir, "core-redis-7.0.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read network state file: %v (expected state file for internal ports)", err)
	}

	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}

	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port in state, got %d", len(state.Ports))
	}
	if !state.Ports[0].Forward {
		t.Fatal("expected Forward=true for all ports")
	}
}

func TestReconcileNginxNetworkState(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	netStateDir := t.TempDir()

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:              inst,
		RepositoryRoot:         rr,
		Storage:                mock,
		Systemd:                sd,
		NetworkControllerImage: "town-os-networkcontroller:local",
		NetworkStatePath:       netStateDir,
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Verify network state file was written during reconcile.
	statePath := filepath.Join(netStateDir, "core-nginx-1.0.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read network state file after reconcile: %v", err)
	}

	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal network state: %v", err)
	}

	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port after reconcile, got %d", len(state.Ports))
	}
	if state.Ports[0].ExternalPort != 8080 || state.Ports[0].InternalPort != 80 {
		t.Fatalf("expected 8080->80, got %d->%d", state.Ports[0].ExternalPort, state.Ports[0].InternalPort)
	}
	if !state.Ports[0].Forward {
		t.Fatal("expected Forward=true for all ports after reconcile")
	}

	// Verify NC unit was installed during reconcile.
	calls := sd.GetCalls()
	var ncInstalled bool
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == "town-os-package--core-nginx-1.0-network.service" {
				ncInstalled = true
				break
			}
		}
	}
	if !ncInstalled {
		t.Fatal("expected network controller unit to be installed during reconcile")
	}
}

// TestReconcileSharedNetworkForDependency verifies that during reconcile,
// a dependency package joins its parent's podman network and gets
// PartOf/Before systemd ordering.
func TestReconcileSharedNetworkForDependency(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx as the parent and redis as a fake dependency.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Install redis under the dependency effective name.
	depEffName := packages.DependencyName("nginx", "cache")
	if err := inst.Install("core", depEffName, "redis", "7.0", packages.Responses{
		"port":      "6379",
		"password":  "testpass",
		"maxmemory": "100mb",
	}); err != nil {
		t.Fatalf("Install dep: %v", err)
	}

	// Save dependency records for nginx.
	if err := inst.SaveDependencies("core", "nginx", map[string]packages.DependencyRecord{
		"cache": {
			EffectiveName: depEffName,
			Package:       "redis",
			Repo:          "core",
			Version:       "7.0",
		},
	}); err != nil {
		t.Fatalf("SaveDependencies: %v", err)
	}

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()

	// Find the dependency's service unit content.
	depUnitName := systemd.UnitName("core", depEffName, "7.0")
	var depContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == depUnitName {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				depContent = sc
				break
			}
		}
	}
	if depContent == "" {
		t.Fatalf("expected InstallUnit call for %s", depUnitName)
	}

	parentNetwork := systemd.NetworkName("core", "nginx", "1.0")
	parentUnit := systemd.UnitName("core", "nginx", "1.0")

	// Dep must join parent's network.
	if !strings.Contains(depContent, "--net "+parentNetwork) {
		t.Fatalf("dependency unit missing parent network, got:\n%s", depContent)
	}

	// Dep must have PartOf and Before parent.
	if !strings.Contains(depContent, "PartOf="+parentUnit) {
		t.Fatalf("dependency unit missing PartOf, got:\n%s", depContent)
	}
	if !strings.Contains(depContent, "Before="+parentUnit) {
		t.Fatalf("dependency unit missing Before, got:\n%s", depContent)
	}

	// Dep must NOT create or rm the network (NC owns it).
	if strings.Contains(depContent, "podman network rm") {
		t.Fatalf("dependency must not remove the shared network, got:\n%s", depContent)
	}
	if strings.Contains(depContent, "podman network create") {
		t.Fatalf("dependency must not create the network (NC owns it), got:\n%s", depContent)
	}

	// Dep must wait for the parent's NC (which creates the network).
	parentNCUnit := systemd.NetworkControllerUnitName("core", "nginx", "1.0")
	if !strings.Contains(depContent, parentNCUnit) {
		t.Fatalf("dependency missing parent NC %s in After, got:\n%s", parentNCUnit, depContent)
	}

	// Find the parent's service unit content.
	var parentContent string
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if name == parentUnit {
				sc, ok := call.Args[1].(string)
				if !ok {
					t.Fatal("type assertion failed")
				}
				parentContent = sc
				break
			}
		}
	}
	if parentContent == "" {
		t.Fatalf("expected InstallUnit call for %s", parentUnit)
	}

	// Parent must have Wants and After for the dependency.
	if !strings.Contains(parentContent, "Wants="+depUnitName) {
		t.Fatalf("parent missing Wants for dep, got:\n%s", parentContent)
	}
	if !strings.Contains(parentContent, "After="+depUnitName) {
		t.Fatalf("parent missing After for dep, got:\n%s", parentContent)
	}
}

// TestNCImageBuildProducesValidImage verifies that the NC container image can
// be built from the binary baked into the systemcontroller image. The test
// container has /town-os-networkcontroller and alpine:latest pre-loaded.
func TestNCImageBuildProducesValidImage(t *testing.T) {
	const imageName = "town-os-networkcontroller:test-build"
	const ncBinaryPath = "/town-os-networkcontroller"

	// Verify the NC binary exists in the test container.
	if _, err := os.Stat(ncBinaryPath); err != nil {
		t.Skipf("NC binary not available at %s (not running in integration container): %v", ncBinaryPath, err)
	}

	ctx := context.Background()

	// Build in a temp directory — same steps as buildNetworkControllerImage.
	buildDir := t.TempDir()

	containerfile := "FROM docker.io/library/alpine:latest\nRUN apk add --no-cache socat\nCOPY town-os-networkcontroller /town-os-networkcontroller\nENTRYPOINT [\"/town-os-networkcontroller\"]\n"
	if err := os.WriteFile(filepath.Join(buildDir, "Containerfile"), []byte(containerfile), 0600); err != nil {
		t.Fatalf("write Containerfile: %v", err)
	}

	// Copy the NC binary into the build context.
	src, err := os.ReadFile(ncBinaryPath)
	if err != nil {
		t.Fatalf("read NC binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "town-os-networkcontroller"), src, 0755); err != nil { //nolint:gosec // G306 -- binary must be executable
		t.Fatalf("write NC binary: %v", err)
	}

	// Build the image.
	out, err := exec.CommandContext(ctx, "podman", "build", "--pull=never", "-t", imageName, "-f", "Containerfile", buildDir).CombinedOutput()
	if err != nil {
		t.Fatalf("podman build failed: %v\n%s", err, string(out))
	}
	t.Cleanup(func() {
		if rmErr := exec.CommandContext(ctx, "podman", "rmi", "-f", imageName).Run(); rmErr != nil { //nolint:gosec // G204 -- imageName is a constant
			t.Logf("cleanup image: %v", rmErr)
		}
	})

	// Verify the image exists.
	if err := exec.CommandContext(ctx, "podman", "image", "exists", imageName).Run(); err != nil {
		t.Fatalf("image %s should exist after build: %v", imageName, err)
	}

	// Verify the binary is inside the image.
	out, err = exec.CommandContext(ctx, "podman", "run", "--rm", imageName, "ls", "/town-os-networkcontroller").CombinedOutput()
	if err != nil {
		t.Fatalf("NC binary not found in image: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "/town-os-networkcontroller") {
		t.Fatalf("expected /town-os-networkcontroller in output, got: %s", string(out))
	}
}
