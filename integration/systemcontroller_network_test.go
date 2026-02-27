package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// --- Command and NetworkMode integration tests ---

func TestSystemControllerInstallRedisCommandInUnit(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "")

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

	// Verify -p port mapping is present (default bridge mode).
	if !strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatalf("service unit missing '-p 6379:6379' in bridge mode, got:\n%s", serviceContent)
	}

	// Verify --net host is NOT present.
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host in bridge mode")
	}
}

func TestSystemControllerInstallWithHostNetworkMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

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

	calls := sd.GetCalls()

	// Find the service unit content.
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

	// Verify --net host is present.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("service unit missing '--net host' in host mode, got:\n%s", serviceContent)
	}

	// Verify -p is NOT present.
	if strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatal("service unit should not have -p mappings in host mode")
	}

	// Verify command args still appear.
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("service unit missing command arg 'redis-server' in host mode, got:\n%s", serviceContent)
	}
	if !strings.Contains(serviceContent, "--bind") {
		t.Fatalf("service unit missing command arg '--bind' in host mode, got:\n%s", serviceContent)
	}

	// Redis 6379→6379 (same port, internal only): no network controller should be installed.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed for internal-only port: %s", name)
			}
		}
	}
}

func TestSystemControllerInstallNginxBridgeMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "")

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Find the service unit content.
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

	// Nginx has no command field, so command args should not appear after the image.
	// The image line should end the ExecStart (no extra args).
	if !strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatalf("service unit missing '-p 8080:80', got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "--net host") {
		t.Fatal("service unit should not have --net host in bridge mode")
	}
}

func TestSystemControllerInstallNginxHostMode(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

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

	// Find the service unit content.
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

	// Verify --net host is present and -p is not.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("service unit missing '--net host' in host mode, got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 8080:80") {
		t.Fatal("service unit should not have -p mappings in host mode")
	}

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
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "After=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing After, got:\n%s", ncContent)
	}
}

func TestSystemControllerInstallNginxHostModeNetworkController(t *testing.T) {
	c, sd := initSystemControllerInstallSystemdTestWithNetworkMode(t, "host")

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

	// Verify network controller unit was installed.
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
		t.Fatal("expected InstallUnit call for town-os-package--core-nginx-1.0-network.service")
	}
	if !strings.Contains(ncContent, "Description=Town OS Network Controller: core/nginx@1.0") {
		t.Fatalf("network controller missing description, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo, got:\n%s", ncContent)
	}
	if !strings.Contains(ncContent, "After=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing After, got:\n%s", ncContent)
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

func TestReconcileWithNetworkMode(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

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

	// Clear mock systemd calls (simulate restart).
	sd.Calls = nil

	err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		NetworkMode:    "host",
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
		t.Fatal("expected InstallUnit call for town-os-package--core-redis-7.0.service after reconcile")
	}

	// Verify --net host and command args after reconcile.
	if !strings.Contains(serviceContent, "--net host") {
		t.Fatalf("reconciled unit missing '--net host', got:\n%s", serviceContent)
	}
	if strings.Contains(serviceContent, "-p 6379:6379") {
		t.Fatal("reconciled unit should not have -p in host mode")
	}
	if !strings.Contains(serviceContent, "redis-server") {
		t.Fatalf("reconciled unit missing command arg 'redis-server', got:\n%s", serviceContent)
	}

	// Redis 6379→6379 (same port, internal only): no network controller should be installed after reconcile.
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed after reconcile for internal-only port: %s", name)
			}
		}
	}
}

func TestReconcileWithNetworkModeNginxNetworkController(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

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
		NetworkMode:    "host",
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	calls := sd.GetCalls()

	// Verify network controller unit was installed during reconcile.
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
		t.Fatal("expected InstallUnit call for network controller unit after reconcile")
	}
	if !strings.Contains(ncContent, "BindsTo=town-os-package--core-nginx-1.0.service") {
		t.Fatalf("network controller missing BindsTo after reconcile, got:\n%s", ncContent)
	}

	// Verify network controller was enabled.
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

// --- Internal port forwarding integration tests ---

func TestSystemControllerInstallNginxHostModeNetworkState(t *testing.T) {
	c, _, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t, "host")

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

	if state.NetworkMode != "host" {
		t.Fatalf("expected network_mode host, got %s", state.NetworkMode)
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
		t.Fatal("expected Forward=true for external port with host!=container in host mode")
	}
}

func TestSystemControllerInstallRedisHostModeNoNCForSamePort(t *testing.T) {
	c, sd, netStateDir := initSystemControllerInstallSystemdTestWithNetworkState(t, "host")

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

	// Redis 6379→6379 (same port, internal only): no network controller should be installed.
	calls := sd.GetCalls()
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed for same-port internal mapping: %s", name)
			}
		}
	}

	// No network state file should be written (no external ports, internal same-port).
	statePath := filepath.Join(netStateDir, "core-redis-7.0.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("expected no network state file for same-port internal mapping, but file exists")
	}
}

func TestSystemControllerInstallNginxBridgeModeNoInternalPortForwarding(t *testing.T) {
	c, sd, _ := initSystemControllerInstallSystemdTestWithNetworkState(t, "")

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

	// Bridge mode: no network controller for internal-only ports.
	calls := sd.GetCalls()
	for _, call := range calls {
		if call.Method == "InstallUnit" {
			name, ok := call.Args[0].(string)
			if !ok {
				t.Fatal("type assertion failed")
			}
			if strings.Contains(name, "network") {
				t.Fatalf("unexpected network controller unit installed in bridge mode: %s", name)
			}
		}
	}
}

func TestReconcileNginxHostModeNetworkState(t *testing.T) {
	c, rr, inst, sd, mock := initReconcileTestWithNetworkMode(t, "host")

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
		Installer:                inst,
		RepositoryRoot:           rr,
		Storage:                  mock,
		Systemd:                  sd,
		NetworkMode:              "host",
		NetworkControllerBinPath: "/town-os-networkcontroller",
		NetworkStatePath:         netStateDir,
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

	if state.NetworkMode != "host" {
		t.Fatalf("expected network_mode host, got %s", state.NetworkMode)
	}
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port after reconcile, got %d", len(state.Ports))
	}
	if state.Ports[0].ExternalPort != 8080 || state.Ports[0].InternalPort != 80 {
		t.Fatalf("expected 8080->80, got %d->%d", state.Ports[0].ExternalPort, state.Ports[0].InternalPort)
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
