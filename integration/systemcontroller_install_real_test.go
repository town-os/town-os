// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// realRedisMu serializes tests that install core/redis@7.0 into real
// btrfs (/town-os) with real systemd. Multiple tests use the same
// package identity and cannot share that state concurrently, but they
// still participate in the parallel pool — holding this mutex only
// blocks other redis-using tests, not the rest of the parallel queue.
var realRedisMu sync.Mutex

// --- Install + Real Systemd integration tests ---

func initSystemControllerInstallRealSystemdTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.NewManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{Storage: mock, RepositoryRoot: rr, Installer: inst, Systemd: sd})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

func TestSystemControllerInstallWithRealSystemd(t *testing.T) {
	t.Parallel()
	c := initSystemControllerInstallRealSystemdTest(t)

	unitName := systemd.UnitName("core", "nginx", "1.0")
	unitPath := "/etc/systemd/system/" + unitName

	// Cleanup: unconditionally stop/disable/remove all units to prevent leaks.
	t.Cleanup(func() {
		cleanup := systemd.NewManager()
		ctx := context.Background()
		allUnits := systemd.PackageUnitNames("core", "nginx", "1.0", packages.PortMap{8080: 80}, packages.PortMap{})
		for _, name := range allUnits {
			if err := cleanup.SetStatus(ctx, name, systemd.Stop); err != nil {
				t.Logf("cleanup: SetStatus stop %s: %v", name, err)
			}
			if err := cleanup.SetStatus(ctx, name, systemd.Disable); err != nil {
				t.Logf("cleanup: SetStatus disable %s: %v", name, err)
			}
			if err := cleanup.UninstallUnit(ctx, name); err != nil {
				t.Logf("cleanup: UninstallUnit %s: %v", name, err)
			}
		}
	})

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install nginx@1.0.
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{"hostname": "example", "port": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// Verify listed as installed.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after install: %v", err)
	}
	if len(pkgs.Entries) != 1 {
		t.Fatalf("expected 1 installed, got %d", len(pkgs.Entries))
	}
	if pkgs.Entries[0] != "core/nginx@1.0" {
		t.Fatalf("expected core/nginx@1.0, got %s", pkgs.Entries[0])
	}

	// Verify the unit is active via ListUnits.
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: "town-os-package--core-nginx"})
	if err != nil {
		t.Fatalf("ListUnits after install: %v", err)
	}

	var found bool
	for _, u := range units.Entries {
		if u.Name == unitName {
			found = true
			// The unit may be active, deactivating, or failed depending on
			// timing (no real container image exists to keep it running).
			// Any of these states confirms the unit was installed and started.
			switch u.ActiveState {
			case "active", "activating", "deactivating", "failed":
				// OK — activating is a transient state while the container starts.
			default:
				t.Fatalf("expected unit %q ActiveState active/activating/deactivating/failed, got %q", unitName, u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected unit %q in ListUnits output", unitName)
	}

	// Uninstall nginx@1.0.
	if err := c.UninstallPackage(context.TODO(), "core", "nginx", "1.0", false); err != nil {
		t.Fatalf("UninstallPackage nginx@1.0: %v", err)
	}

	// Verify no longer installed.
	pkgs, err = c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled after uninstall: %v", err)
	}
	if len(pkgs.Entries) != 0 {
		t.Fatalf("expected 0 installed after uninstall, got %d", len(pkgs.Entries))
	}

	// Verify unit file no longer exists on disk.
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %q to be removed, got err: %v", unitPath, err)
	}
}

// --- Real container integration tests ---

// initSystemControllerRealContainerTest sets up a test server with real BtrFS
// storage, real systemd, and proper paths so that podman containers can
// actually be launched.
func initSystemControllerRealContainerTest(t *testing.T) *systemcontroller.SystemdClient {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repository list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}

	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("failed to load repository root: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	btr := storage.InitBtrFS("/town-os")
	sd := systemd.NewManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                  btr,
		RepositoryRoot:           rr,
		Installer:                inst,
		Systemd:                  sd,
		BtrfsBasePath:            "/town-os",
		NetworkControllerImage: "localhost/town-os-networkcontroller:local",
		NetworkStatePath:       "/run/town-os",
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c
}

// cleanupContainerUnits unconditionally stops, disables, and removes all
// systemd units for a package plus the podman container itself.
func cleanupContainerUnits(t *testing.T, repo, pkgName, version string, external, internal packages.PortMap) {
	t.Helper()
	cleanup := systemd.NewManager()
	ctx := context.Background()
	allUnits := systemd.PackageUnitNames(repo, pkgName, version, external, internal)
	for _, name := range allUnits {
		if err := cleanup.SetStatus(ctx, name, systemd.Stop); err != nil {
			t.Logf("cleanup: SetStatus stop %s: %v", name, err)
		}
		if err := cleanup.SetStatus(ctx, name, systemd.Disable); err != nil {
			t.Logf("cleanup: SetStatus disable %s: %v", name, err)
		}
		if err := cleanup.UninstallUnit(ctx, name); err != nil {
			t.Logf("cleanup: UninstallUnit %s: %v", name, err)
		}
	}
	containerName := systemd.ContainerName(repo, pkgName, version)
	if err := exec.CommandContext(context.TODO(), "podman","stop", "-t", "10", containerName).Run(); err != nil {
		t.Logf("cleanup: podman stop %s: %v", containerName, err)
	}
	if err := exec.CommandContext(context.TODO(), "podman","rm", "-f", containerName).Run(); err != nil {
		t.Logf("cleanup: podman rm %s: %v", containerName, err)
	}
}

// waitForContainer polls podman inspect until the container reaches "running"
// state or the timeout expires.
func waitForContainer(t *testing.T, repo, pkgName, version string, timeout time.Duration) {
	t.Helper()
	containerName := systemd.ContainerName(repo, pkgName, version)
	unitName := systemd.UnitName(repo, pkgName, version)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.CommandContext(context.TODO(), "podman","inspect", "--format", "{{.State.Status}}", containerName).Output()
		if err == nil && strings.TrimSpace(string(out)) == "running" {
			return
		}
		time.Sleep(2 * time.Second)
	}
	// Log the systemd journal for debugging before failing.
	journal, journalErr := exec.CommandContext(context.TODO(), "journalctl", "-u", unitName, "--no-pager", "-n", "50").Output()
	if journalErr != nil {
		t.Logf("journalctl for %s: %v", unitName, journalErr)
	}
	// Also log the NC unit journal if it exists.
	ncUnitName := systemd.NetworkControllerUnitName(repo, pkgName, version)
	ncJournal, ncErr := exec.CommandContext(context.TODO(), "journalctl", "-u", ncUnitName, "--no-pager", "-n", "50").Output()
	if ncErr != nil {
		t.Logf("journalctl for %s: %v", ncUnitName, ncErr)
	}
	t.Fatalf("container %q did not reach running state within %v\njournal (%s):\n%s\njournal (%s):\n%s",
		containerName, timeout, unitName, string(journal), ncUnitName, string(ncJournal))
}

func TestSystemControllerRealContainerLifecycle(t *testing.T) {
	t.Parallel()
	// Serialize against other redis-installing tests since they share the
	// same btrfs subvolume path and systemd unit name. Registered first so
	// LIFO cleanup holds the lock until after all teardown completes.
	realRedisMu.Lock()
	t.Cleanup(realRedisMu.Unlock)

	c := initSystemControllerRealContainerTest(t)

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0 (port is auto-assigned by FindAvailablePort).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	// Discover the dynamically assigned port.
	responses, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	assignedPort := responses["port"]
	if assignedPort == "" {
		t.Fatal("expected non-empty port in responses after install")
	}
	portNum, err := strconv.ParseUint(assignedPort, 10, 16)
	if err != nil {
		t.Fatalf("parse assigned port %q: %v", assignedPort, err)
	}

	t.Cleanup(func() {
		cleanupContainerUnits(t, "core", "redis", "7.0", packages.PortMap{}, packages.PortMap{uint16(portNum): 6379})
	})

	// Wait for the container to start (includes image pull).
	waitForContainer(t, "core", "redis", "7.0", 3 * time.Minute)

	// Verify the systemd unit is active.
	containerName := systemd.ContainerName("core", "redis", "7.0")
	units, err := c.ListUnits(context.TODO(), systemcontroller.ListParams{Search: containerName})
	if err != nil {
		t.Fatalf("ListUnits: %v", err)
	}

	unitName := systemd.UnitName("core", "redis", "7.0")
	var found bool
	for _, u := range units.Entries {
		if u.Name == unitName {
			found = true
			if u.ActiveState != "active" {
				t.Fatalf("expected unit %q ActiveState active, got %q", unitName, u.ActiveState)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected unit %q in ListUnits output", unitName)
	}

	// Verify the assigned port is accessible via TCP from the host.
	// Retry with short dials because nested podman port forwarding can be slow.
	dialAddr := "127.0.0.1:" + assignedPort
	var conn net.Conn
	dialDeadline := time.Now().Add(time.Minute)
	for time.Now().Before(dialDeadline) {
		conn, err = (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		logs, logsErr := exec.CommandContext(context.TODO(), "podman","logs", "--tail", "20", containerName).CombinedOutput()
		if logsErr != nil {
			t.Logf("podman logs %s: %v", containerName, logsErr)
		}
		t.Fatalf("TCP connect to redis on port %s failed: %v\ncontainer logs:\n%s", assignedPort, err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Verify podman container is listed.
	out, err := exec.CommandContext(context.TODO(), "podman","ps", "--filter", "name="+containerName, "--format", "{{.Names}}").Output()
	if err != nil {
		t.Fatalf("podman ps: %v", err)
	}
	if !strings.Contains(string(out), containerName) {
		t.Fatalf("expected %s in podman ps output, got: %s", containerName, string(out))
	}

	// Uninstall redis@7.0.
	if err := c.UninstallPackage(context.TODO(), "core", "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}

	// Wait for the container to stop.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		inspectOut, inspectErr := exec.CommandContext(context.TODO(), "podman","inspect", "--format", "{{.State.Status}}", containerName).Output()
		if inspectErr != nil || strings.TrimSpace(string(inspectOut)) != "running" {
			break
		}
		time.Sleep(time.Second)
	}

	// Verify the unit file is gone.
	unitPath := "/etc/systemd/system/" + unitName
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("expected unit file %q removed after uninstall, got err: %v", unitPath, err)
	}

	// Verify the container is no longer running.
	out, err = exec.CommandContext(context.TODO(), "podman","ps", "--filter", "name="+containerName, "--format", "{{.Names}}").Output()
	if err == nil && strings.Contains(string(out), containerName) {
		t.Fatalf("expected %s not in podman ps after uninstall", containerName)
	}

	// Verify the assigned port is no longer accessible.
	postConn, postErr := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
	if postErr == nil {
		if err := postConn.Close(); err != nil {
			t.Errorf("postConn.Close: %v", err)
		}
		t.Fatalf("expected port %s to be unreachable after uninstall", assignedPort)
	}
}

func TestSystemControllerRealContainerReinstall(t *testing.T) {
	t.Parallel()
	// Serialize against other redis-installing tests — see the comment
	// in TestSystemControllerRealContainerLifecycle for why.
	realRedisMu.Lock()
	t.Cleanup(realRedisMu.Unlock)

	c := initSystemControllerRealContainerTest(t)

	// Add core repo.
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}

	// Install redis@7.0 (port is auto-assigned by FindAvailablePort).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "testpass",
		"maxmemory": "100mb",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage redis@7.0: %v", err)
	}

	waitForContainer(t, "core", "redis", "7.0", 3 * time.Minute)

	// Discover the dynamically assigned port.
	responses, err := c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses redis@7.0: %v", err)
	}
	assignedPort := responses["port"]
	if assignedPort == "" {
		t.Fatal("expected non-empty port in responses after install")
	}
	portNum, err := strconv.ParseUint(assignedPort, 10, 16)
	if err != nil {
		t.Fatalf("parse assigned port %q: %v", assignedPort, err)
	}

	t.Cleanup(func() {
		cleanupContainerUnits(t, "core", "redis", "7.0", packages.PortMap{}, packages.PortMap{uint16(portNum): 6379})
	})

	// Reinstall with the same version (same-version reinstall path).
	if err := c.InstallPackage(context.TODO(), "redis", "7.0", packages.Responses{
		"password":  "newpass",
		"maxmemory": "200mb",
	}, false, "", false); err != nil {
		t.Fatalf("Reinstall redis@7.0: %v", err)
	}

	// Wait for the new container to come up.
	waitForContainer(t, "core", "redis", "7.0", 3 * time.Minute)

	// Discover the port after reinstall (may have changed).
	responses, err = c.GetResponses(context.TODO(), "core", "redis", "7.0")
	if err != nil {
		t.Fatalf("GetResponses after reinstall: %v", err)
	}
	reinstallPort := responses["port"]
	if reinstallPort == "" {
		t.Fatal("expected non-empty port in responses after reinstall")
	}

	// Verify installed list still shows redis@7.0.
	pkgs, err := c.ListInstalled(context.TODO(), systemcontroller.ListParams{})
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	var foundRedis bool
	for _, entry := range pkgs.Entries {
		if entry == "core/redis@7.0" {
			foundRedis = true
		}
	}
	if !foundRedis {
		t.Fatalf("expected core/redis@7.0 in installed list, got: %v", pkgs.Entries)
	}

	// Verify the assigned port is accessible via TCP after reinstall.
	// Retry with short dials because nested podman port forwarding can be slow.
	dialAddr := "127.0.0.1:" + reinstallPort
	var conn net.Conn
	tcpDeadline := time.Now().Add(time.Minute)
	for time.Now().Before(tcpDeadline) {
		conn, err = (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.TODO(), "tcp", dialAddr)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		logs, logsErr := exec.CommandContext(context.TODO(), "podman","logs", "--tail", "20", systemd.ContainerName("core", "redis", "7.0")).CombinedOutput()
		if logsErr != nil {
			t.Logf("podman logs %s: %v", systemd.ContainerName("core", "redis", "7.0"), logsErr)
		}
		t.Fatalf("TCP connect to redis on port %s after reinstall failed: %v\ncontainer logs:\n%s", reinstallPort, err, string(logs))
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close redis connection: %v", err)
	}

	// Uninstall.
	if err := c.UninstallPackage(context.TODO(), "core", "redis", "7.0", false); err != nil {
		t.Fatalf("UninstallPackage redis@7.0: %v", err)
	}
}
