// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

func rolodexTestImage() string {
	if img := os.Getenv("ROLODEX_IMAGE"); img != "" {
		return img
	}
	return "quay.io/town/rolodex:rc.latest"
}

// rolodexTempDir creates a temporary directory with a dash-case name and
// registers cleanup with t.Cleanup.
func rolodexTempDir(t *testing.T, pattern string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", pattern) //nolint:usetesting // dash-case dir names
	if err != nil {
		t.Fatalf("MkdirTemp(%s): %v", pattern, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func initSystemControllerRolodexTest(t *testing.T) (*systemcontroller.SystemdClient, *systemd.MockManager) {
	t.Helper()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	dataDir := rolodexTempDir(t, "rolodex-mock-*")
	rolMgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage: mock,
		Systemd: sd,
		Rolodex: rolMgr,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, sd
}

// rolodexTestKey returns a unique service key for test isolation so the test
// unit/container does not collide with the production rolodex service.
func rolodexTestKey() string {
	return "rolodex-test-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatUint(rand.Uint64(), 36)
}

// initRolodexRealTest creates a rolodex container via a systemd unit, writes
// config, and returns a connected client and the DNS port. The Manager does
// not manage units, so this helper installs the unit directly.
func initRolodexRealTest(t *testing.T) (rolodex.Client, string) {
	t.Helper()

	dataDir := rolodexTempDir(t, "rolodex-data-*")
	sd := systemd.NewManager()
	socketPath := filepath.Join(dataDir, "rolodex.sock")
	dnsPort := findFreePort(t)
	key := rolodexTestKey()

	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: socketPath,
		DNSPort:        dnsPort,
		Key:            key,
	})

	if _, err := mgr.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	// Install and start the unit directly via systemd.
	unitName := systemd.SystemServiceUnitName(key)
	_ = sd.SetStatus(ctx, unitName, systemd.Stop)
	_ = sd.UninstallUnit(ctx, unitName)

	cfg := systemd.SystemServiceUnitConfig{
		Key:         key,
		Description: "Rolodex DNS (test)",
		Image:       rolodexTestImage(),
		Args: []string{
			"--net", "host",
			"-v", dataDir + ":/data",
		},
		Command:    []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
		VolumeDirs: []string{dataDir},
	}
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if err := sd.SetStatus(cleanupCtx, unitName, systemd.Stop); err != nil {
			t.Logf("cleanup SetStatus(stop): %v", err)
		}
		if err := sd.UninstallUnit(cleanupCtx, unitName); err != nil {
			t.Logf("cleanup UninstallUnit: %v", err)
		}
	})

	// Verify the unit actually started before waiting for the socket.
	time.Sleep(2 * time.Second)
	status := mgr.Status(ctx)
	if !status.Running {
		dumpRolodexDiagnostics(ctx, t, dataDir, key)
		t.Fatal("rolodex not running 2s after start")
	}

	client := waitForRolodexClient(t, ctx, socketPath, dataDir, key)
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return client, dnsPort
}

func TestRolodexSystemServiceListed(t *testing.T) {
	t.Parallel()
	c, _ := initSystemControllerRolodexTest(t)

	entries, err := c.ListSystemServices(context.TODO())
	if err != nil {
		t.Fatalf("ListSystemServices: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 system service, got %d", len(entries))
	}

	if entries[0].Key != "rolodex" {
		t.Fatalf("expected key %q, got %q", "rolodex", entries[0].Key)
	}
}

func TestRolodexStartStopRestart(t *testing.T) {
	t.Parallel()
	c, sd := initSystemControllerRolodexTest(t)

	for _, action := range []systemd.StatusAction{systemd.Start, systemd.Stop, systemd.Restart} {
		if err := c.SetSystemServiceStatus(context.TODO(), "rolodex", action); err != nil {
			t.Fatalf("SetSystemServiceStatus(%s): %v", action, err)
		}
	}

	calls := sd.GetCalls()
	statusCalls := 0
	for _, call := range calls {
		if call.Method == "SetStatus" {
			statusCalls++
		}
	}

	if statusCalls != 3 {
		t.Fatalf("expected 3 SetStatus calls, got %d", statusCalls)
	}
}

func TestRolodexRealContainerStart(t *testing.T) {
	t.Parallel()
	dataDir := rolodexTempDir(t, "rolodex-start-*")
	sd := systemd.NewManager()
	key := rolodexTestKey()
	dnsPort := findFreePort(t)

	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
		DNSPort:        dnsPort,
		Key:            key,
	})

	if _, err := mgr.WriteConfig(); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	ctx := context.Background()

	// Clean up any leftover unit with this key (unlikely but safe).
	unitName := systemd.SystemServiceUnitName(key)
	_ = sd.SetStatus(ctx, unitName, systemd.Stop)
	_ = sd.UninstallUnit(ctx, unitName)

	cfg := systemd.SystemServiceUnitConfig{
		Key:         key,
		Description: "Rolodex DNS (test)",
		Image:       rolodexTestImage(),
		Args: []string{
			"--net", "host",
			"-v", dataDir + ":/data",
		},
		Command:    []string{"/usr/local/bin/rolodex-dns", "--config", "/data/rolodex.yml"},
		VolumeDirs: []string{dataDir},
	}
	uf := systemd.GenerateSystemServiceUnit(cfg)

	if err := sd.InstallUnit(ctx, uf.Name, uf.Content); err != nil {
		t.Fatalf("InstallUnit: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Enable); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := sd.SetStatus(ctx, uf.Name, systemd.Restart); err != nil {
		t.Fatalf("Restart: %v", err)
	}

	t.Cleanup(func() {
		if err := sd.SetStatus(ctx, unitName, systemd.Stop); err != nil {
			t.Logf("cleanup SetStatus(stop): %v", err)
		}
		if err := sd.UninstallUnit(ctx, unitName); err != nil {
			t.Logf("cleanup UninstallUnit: %v", err)
		}
	})

	// Wait for systemd to bring the container up.
	var status rolodex.Status
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		status = mgr.Status(ctx)
		if status.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !status.Running {
		t.Fatal("expected rolodex running after unit start")
	}
}

// TestRolodexClientOperations shares a single rolodex container across client
// subtests. Separate containers cause unreliable port bindings on 127.0.0.2
// after teardown (see d789b23).
func TestRolodexClientOperations(t *testing.T) {
	client, _ := initRolodexRealTest(t)
	ctx := context.Background()

	t.Run("Connect", func(t *testing.T) {
		// Verify the connection works by listing records.
		records, err := client.ListRecords(ctx, nil)
		if err != nil {
			t.Fatalf("ListRecords: %v", err)
		}
		// New server should have no records.
		if len(records) != 0 {
			t.Fatalf("expected 0 records, got %d", len(records))
		}
	})

	t.Run("AddRemoveRecords", func(t *testing.T) {
		// Add an A record.
		if err := client.AddRecord(ctx, &upstream.DnsRecord{
			Name:       "test.local.",
			RecordType: upstream.RecordTypeA,
			Value:      "10.0.0.1",
			Ttl:        300,
		}); err != nil {
			t.Fatalf("AddRecord A: %v", err)
		}

		// Add an AAAA record.
		if err := client.AddRecord(ctx, &upstream.DnsRecord{
			Name:       "test.local.",
			RecordType: upstream.RecordTypeAAAA,
			Value:      "::1",
			Ttl:        300,
		}); err != nil {
			t.Fatalf("AddRecord AAAA: %v", err)
		}

		// List and verify.
		records, err := client.ListRecords(ctx, nil)
		if err != nil {
			t.Fatalf("ListRecords: %v", err)
		}
		if len(records) < 2 {
			t.Fatalf("expected at least 2 records, got %d", len(records))
		}

		// Remove records for test.local. by type.
		aType := upstream.RecordTypeA
		removed, err := client.RemoveRecord(ctx, "test.local.", &upstream.RemoveRecordOptions{RecordType: &aType})
		if err != nil {
			t.Fatalf("RemoveRecord A: %v", err)
		}
		if removed == 0 {
			t.Fatal("expected at least 1 A record removed")
		}
		aaaaType := upstream.RecordTypeAAAA
		removed, err = client.RemoveRecord(ctx, "test.local.", &upstream.RemoveRecordOptions{RecordType: &aaaaType})
		if err != nil {
			t.Fatalf("RemoveRecord AAAA: %v", err)
		}
		if removed == 0 {
			t.Fatal("expected at least 1 AAAA record removed")
		}

		// Verify empty.
		records, err = client.ListRecords(ctx, nil)
		if err != nil {
			t.Fatalf("ListRecords after remove: %v", err)
		}
		if len(records) != 0 {
			t.Fatalf("expected 0 records after remove, got %d", len(records))
		}
	})
}

func TestRolodexDNSQueryForwarding(t *testing.T) {
	t.Parallel()
	_, dnsPort := initRolodexRealTest(t)

	// Use a short timeout — these tests query external domains via DNS
	// forwarding (8.8.8.8), which requires outbound internet from the
	// nested container. Fail fast rather than hanging for the full
	// test timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Rolodex is running on a random high port (local mode) and handles its
	// own forwarding. Use a custom resolver pointing at that port.
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", rolodex.DNSLoopback+":"+dnsPort)
		},
	}

	for _, domain := range []string{"example.org.", "example.com."} {
		var addrs []string
		var resolveErr error
		for ctx.Err() == nil {
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			addrs, resolveErr = resolver.LookupHost(lookupCtx, domain)
			cancel()
			if resolveErr == nil && len(addrs) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if resolveErr != nil {
			t.Fatalf("LookupHost(%s): %v (DNS forwarding to 8.8.8.8 may be unreachable from nested container)", domain, resolveErr)
		}
		if len(addrs) == 0 {
			t.Fatalf("expected at least 1 address for %s", domain)
		}
	}
}

// dumpRolodexDiagnostics logs unit status, journal entries, directory
// contents, and the unit file to help debug socket-not-appearing failures.
func dumpRolodexDiagnostics(ctx context.Context, t *testing.T, dataDir, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	unitName := systemd.SystemServiceUnitName(key)

	// Unit status via systemctl.
	if out, err := exec.CommandContext(ctx, "systemctl", "status", unitName).CombinedOutput(); err != nil {
		t.Logf("systemctl status %s (exit %v):\n%s", unitName, err, out)
	} else {
		t.Logf("systemctl status %s:\n%s", unitName, out)
	}

	// Recent journal entries.
	if out, err := exec.CommandContext(ctx, "journalctl", "-u", unitName, "-n", "40", "--no-pager").CombinedOutput(); err != nil {
		t.Logf("journalctl (exit %v):\n%s", err, out)
	} else {
		t.Logf("journalctl -u %s:\n%s", unitName, out)
	}

	// Unit file content.
	unitPath := "/etc/systemd/system/" + unitName
	if content, err := os.ReadFile(unitPath); err != nil {
		t.Logf("unit file %s: %v", unitPath, err)
	} else {
		t.Logf("unit file %s:\n%s", unitPath, content)
	}

	// Data directory listing.
	if entries, err := os.ReadDir(dataDir); err != nil {
		t.Logf("ReadDir(%s): %v", dataDir, err)
	} else {
		t.Logf("dataDir %s contents:", dataDir)
		for _, e := range entries {
			info, _ := e.Info()
			if info != nil {
				t.Logf("  %s (mode=%s size=%d)", e.Name(), info.Mode(), info.Size())
			} else {
				t.Logf("  %s", e.Name())
			}
		}
	}

	// Container status.
	if out, err := exec.CommandContext(ctx, "podman", "ps", "-a", "--filter", "name="+systemd.SystemServiceContainerName(key)).CombinedOutput(); err != nil {
		t.Logf("podman ps (exit %v):\n%s", err, out)
	} else {
		t.Logf("podman ps:\n%s", out)
	}
}

// waitForRolodexClient waits for the rolodex Unix socket to become available
// and returns a connected client. The wait is bounded by ctx's deadline
// (set from -test.timeout via t.Deadline).
func waitForRolodexClient(t *testing.T, ctx context.Context, socketPath, dataDir, key string) rolodex.Client {
	t.Helper()

	poll := 100 * time.Millisecond

	var lastDialErr, lastRPCErr error
	for ctx.Err() == nil {
		attemptCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		client, err := rolodex.Dial(attemptCtx, socketPath)
		if err != nil {
			lastDialErr = err
			cancel()
			time.Sleep(poll)
			continue
		}
		// Verify the connection is actually live.
		_, err = client.ListRecords(attemptCtx, nil)
		cancel()
		if err != nil {
			lastRPCErr = err
			if closeErr := client.Close(); closeErr != nil {
				t.Logf("waitForRolodexClient: close after failed ListRecords: %v", closeErr)
			}
			time.Sleep(poll)
			continue
		}
		return client
	}
	if lastDialErr != nil {
		t.Logf("waitForRolodexClient: last Dial error: %v", lastDialErr)
	}
	if lastRPCErr != nil {
		t.Logf("waitForRolodexClient: last ListRecords error: %v", lastRPCErr)
	}
	dumpRolodexDiagnostics(ctx, t, dataDir, key)
	t.Fatalf("waitForRolodexClient: timed out waiting for %s", socketPath)
	return nil
}

func TestRolodexWriteConfigIdempotent(t *testing.T) {
	t.Parallel()
	dataDir := rolodexTempDir(t, "rolodex-idempotent-*")
	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})

	// First call should write.
	written, err := mgr.WriteConfig()
	if err != nil {
		t.Fatalf("first WriteConfig: %v", err)
	}
	if !written {
		t.Fatal("expected first WriteConfig to write")
	}

	// Record mtime.
	configPath := filepath.Join(dataDir, "rolodex.yml")
	fi1, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}

	// Second call should skip (content unchanged, file older than binary).
	written, err = mgr.WriteConfig()
	if err != nil {
		t.Fatalf("second WriteConfig: %v", err)
	}
	if written {
		t.Fatal("expected second WriteConfig to skip (content unchanged)")
	}

	// File should not have been rewritten.
	fi2, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat after second write: %v", err)
	}
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatal("file mtime changed even though content was identical")
	}
}

func TestRolodexWriteConfigSkipsNewerFile(t *testing.T) {
	t.Parallel()
	dataDir := rolodexTempDir(t, "rolodex-newer-*")
	configPath := filepath.Join(dataDir, "rolodex.yml")

	// Write custom content with a future mtime.
	customContent := "# user-modified config\n"
	if err := os.WriteFile(configPath, []byte(customContent), 0644); err != nil {
		t.Fatalf("pre-write: %v", err)
	}
	future := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	mgr := rolodex.NewManager(rolodex.Config{
		Systemd:        systemd.InitMockManager(),
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "rolodex.sock"),
	})

	written, err := mgr.WriteConfig()
	if err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	if written {
		t.Fatal("expected WriteConfig to skip file newer than binary")
	}

	// Verify content was preserved.
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != customContent {
		t.Fatalf("expected preserved content, got:\n%s", data)
	}
}
