// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// initDNSMockTest creates a test server with a mock rolodex client and
// settings manager, suitable for testing DNS API endpoints without a real
// rolodex container.
func initDNSMockTest(t *testing.T) (*systemcontroller.SystemdClient, *rolodex.MockClient) {
	t.Helper()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	dataDir := rolodexTempDir(t, "dns-mock-*")
	rolMgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "dns-test.sock"),
	}))

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: rc,
		SettingsMgr:   settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	return c, rc
}

// --- Mock tests ---

func TestDNSStatusDisabled(t *testing.T) {
	t.Parallel()
	mock := storage.InitBtrFSMock()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage: mock,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	status, err := c.DNSStatus(context.TODO())
	if err != nil {
		t.Fatalf("DNSStatus: %v", err)
	}

	if status.Enabled {
		t.Fatal("expected DNS disabled when no rolodex manager")
	}
}

func TestDNSStatusEnabled(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)

	status, err := c.DNSStatus(context.TODO())
	if err != nil {
		t.Fatalf("DNSStatus: %v", err)
	}

	if !status.Enabled {
		t.Fatal("expected DNS enabled")
	}
	if status.TLD != "home" {
		t.Fatalf("expected TLD %q, got %q", "home", status.TLD)
	}
}

func TestDNSTLDDefault(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)

	tld, err := c.GetDNSTLD(context.TODO())
	if err != nil {
		t.Fatalf("GetDNSTLD: %v", err)
	}

	if tld != "home" {
		t.Fatalf("expected default TLD %q, got %q", "home", tld)
	}
}

func TestDNSTLDGetSet(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)

	if err := c.SetDNSTLD(context.TODO(), "local"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	tld, err := c.GetDNSTLD(context.TODO())
	if err != nil {
		t.Fatalf("GetDNSTLD: %v", err)
	}

	if tld != "local" {
		t.Fatalf("expected TLD %q, got %q", "local", tld)
	}
}

func TestDNSSetupCreatesZone(t *testing.T) {
	t.Parallel()
	c, rc := initDNSMockTest(t)

	if err := c.SetupDNS(context.TODO()); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}

	// Verify authoritative zone was created.
	zones := rc.AuthZones
	if len(zones) != 1 {
		t.Fatalf("expected 1 authoritative zone, got %d", len(zones))
	}
	if zones[0] != "home." {
		t.Fatalf("expected zone %q, got %q", "home.", zones[0])
	}

	// Verify SOA, NS, and A records were created.
	records := rc.Records
	var hasSOA, hasNS, hasA bool
	for _, r := range records {
		switch {
		case r.RecordType == upstream.RecordTypeSOA && r.Name == "home.":
			hasSOA = true
		case r.RecordType == upstream.RecordTypeNS && r.Name == "home.":
			hasNS = true
		case r.RecordType == upstream.RecordTypeA && r.Name == "ns1.home.":
			hasA = true
		}
	}

	if !hasSOA {
		t.Fatal("expected SOA record for home.")
	}
	if !hasNS {
		t.Fatal("expected NS record for home.")
	}
	if !hasA {
		t.Fatal("expected A record for ns1.home.")
	}
}

func TestDNSRecordAddRemove(t *testing.T) {
	t.Parallel()
	c, _ := initDNSMockTest(t)

	ctx := context.TODO()

	// Add a record via the API.
	if err := c.AddDNSRecord(ctx, &upstream.DnsRecord{
		Name:       "test.home.",
		RecordType: upstream.RecordTypeA,
		Value:      "10.0.0.5",
		Ttl:        300,
	}); err != nil {
		t.Fatalf("AddDNSRecord: %v", err)
	}

	// List records.
	records, err := c.ListDNSRecords(ctx, "")
	if err != nil {
		t.Fatalf("ListDNSRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "test.home." {
		t.Fatalf("expected record name %q, got %q", "test.home.", records[0].Name)
	}

	// Remove the record.
	aType := upstream.RecordTypeA
	removed, err := c.RemoveDNSRecord(ctx, "test.home.", &aType)
	if err != nil {
		t.Fatalf("RemoveDNSRecord: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed, got %d", removed)
	}

	// Verify empty.
	records, err = c.ListDNSRecords(ctx, "")
	if err != nil {
		t.Fatalf("ListDNSRecords after remove: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(records))
	}
}

func TestDNSTLDChangeReprovisionsRecords(t *testing.T) {
	t.Parallel()
	c, rc := initDNSMockTest(t)
	ctx := context.TODO()

	// Setup initial TLD.
	if err := c.SetupDNS(ctx); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}

	// Verify we have records under "home.".
	initialRecords := len(rc.Records)
	if initialRecords == 0 {
		t.Fatal("expected records after setup")
	}

	// Change TLD.
	if err := c.SetDNSTLD(ctx, "lan"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	// Verify old zone removed and new zone created.
	hasOldZone := false
	hasNewZone := false
	for _, z := range rc.AuthZones {
		if z == "home." {
			hasOldZone = true
		}
		if z == "lan." {
			hasNewZone = true
		}
	}
	if hasOldZone {
		t.Fatal("expected old zone home. to be removed")
	}
	if !hasNewZone {
		t.Fatal("expected new zone lan. to be created")
	}

	// Verify records are now under "lan.".
	for _, r := range rc.Records {
		if r.RecordType == upstream.RecordTypeSOA || r.RecordType == upstream.RecordTypeNS {
			if r.Name != "lan." {
				t.Fatalf("expected record under lan., got %q", r.Name)
			}
		}
		if r.RecordType == upstream.RecordTypeA && r.Name == "ns1.home." {
			t.Fatal("found stale A record for ns1.home.")
		}
	}
}

// --- Real container tests ---

// TestDNSRealQueries shares a single rolodex container across DNS query
// subtests. Separate containers cause unreliable port bindings on 127.0.0.2
// after teardown (see d789b23).
func TestDNSRealQueries(t *testing.T) {
	t.Parallel()
	client, dnsPort := initRolodexRealTest(t)
	ctx := context.Background()
	if dl, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, dl.Add(-15*time.Second))
		t.Cleanup(cancel)
	}

	// Setup TLD using the real client (shared across subtests).
	if err := rolodex.SetupTLD(ctx, client, "home", rolodex.DNSLoopback, ""); err != nil {
		t.Fatalf("SetupTLD: %v", err)
	}

	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", rolodex.DNSLoopback+":"+dnsPort)
		},
	}

	t.Run("SetupAndQuery", func(t *testing.T) {
		t.Parallel()
		// Verify ns1.home. resolves via DNS.
		var addrs []string
		var resolveErr error
		for ctx.Err() == nil {
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			addrs, resolveErr = resolver.LookupHost(lookupCtx, "ns1.home.")
			cancel()
			if resolveErr == nil && len(addrs) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if resolveErr != nil {
			t.Fatalf("LookupHost(ns1.home.): %v", resolveErr)
		}
		if len(addrs) == 0 {
			t.Fatal("expected at least 1 address for ns1.home.")
		}

		if !slices.Contains(addrs, rolodex.DNSLoopback) {
			t.Fatalf("expected ns1.home. to resolve to %s, got %v", rolodex.DNSLoopback, addrs)
		}
	})

	t.Run("PackageRecord", func(t *testing.T) {
		t.Parallel()
		// Register a package.
		if err := rolodex.RegisterPackageDNS(ctx, client, "core", "nginx", "home", rolodex.DNSLoopback, "", nil); err != nil {
			t.Fatalf("RegisterPackageDNS: %v", err)
		}

		// Verify nginx.core.home. resolves via DNS.
		var addrs []string
		var resolveErr error
		for ctx.Err() == nil {
			lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			addrs, resolveErr = resolver.LookupHost(lookupCtx, "nginx.core.home.")
			cancel()
			if resolveErr == nil && len(addrs) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if resolveErr != nil {
			t.Fatalf("LookupHost(nginx.core.home.): %v", resolveErr)
		}
		if len(addrs) == 0 {
			t.Fatal("expected at least 1 address for nginx.core.home.")
		}

		if !slices.Contains(addrs, rolodex.DNSLoopback) {
			t.Fatalf("expected nginx.core.home. to resolve to %s, got %v", rolodex.DNSLoopback, addrs)
		}
	})
}

// --- Resolved routing tests ---
//
// Two tests that used to live here — one asserting ConfigureResolvedRouting is
// non-fatal, one asserting the drop-in it writes has the right content — called
// the real function, and so wrote the real /etc/systemd/resolved.conf.d/town-os.conf
// and SIGHUPed the real systemd-resolved. Inside this container that is its own
// /etc, but it is still a test run creating the one file the harness treats as
// proof that a run leaked onto the host's resolver, and it broke
// TestRolodexDNSPortOverrideReachesTheBootPath, whose whole assertion is that no
// such file exists on a box where rolodex was relocated off :53.
//
// Their coverage moved to src/rolodex/resolved_test.go, which exercises the
// write through writeResolvedDropIn against a temp directory: same content
// assertion, same error path, no system file and no signal. What is left here is
// the half that is genuinely about the controller — whether the configurator is
// called at all — and every one of those uses a recorder in place of the real
// function.

func TestResolvedRoutingCalledOnTLDChange(t *testing.T) {
	t.Parallel()

	var calledTLD, calledAddr string
	recorder := func(_ context.Context, tld, addr string) {
		calledTLD = tld
		calledAddr = addr
	}

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	dataDir := rolodexTempDir(t, "resolved-tld-*")
	rolMgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "resolved-test.sock"),
	}))

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:              mock,
		Systemd:              sd,
		Rolodex:              rolMgr,
		RolodexClient:        rc,
		SettingsMgr:          settings,
		ResolvedConfigurator: recorder,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	if err := c.SetDNSTLD(context.TODO(), "local"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}

	if calledTLD != "local" {
		t.Fatalf("expected ResolvedConfigurator called with TLD %q, got %q", "local", calledTLD)
	}
	if calledAddr != rolodex.DNSLoopback {
		t.Fatalf("expected ResolvedConfigurator called with addr %q, got %q", rolodex.DNSLoopback, calledAddr)
	}
}

func TestResolvedRoutingNotCalledWhenNil(t *testing.T) {
	t.Parallel()

	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}

	dataDir := rolodexTempDir(t, "resolved-nil-*")
	rolMgr := rolodex.NewManager(pinRolodexDiscovery(t, rolodex.Config{
		Systemd:        sd,
		DataDir:        dataDir,
		Image:          rolodexTestImage(),
		UnixSocketPath: filepath.Join(dataDir, "resolved-nil.sock"),
	}))

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:       mock,
		Systemd:       sd,
		Rolodex:       rolMgr,
		RolodexClient: rc,
		SettingsMgr:   settings,
		// ResolvedConfigurator intentionally nil.
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("could not create client: %v", err)
	}

	// Should not panic when ResolvedConfigurator is nil.
	if err := c.SetDNSTLD(context.TODO(), "local"); err != nil {
		t.Fatalf("SetDNSTLD: %v", err)
	}
}
