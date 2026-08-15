// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// archTag maps Go's runtime.GOARCH (amd64/arm64) to the per-arch image tag
// suffix used by the registry (x86_64/aarch64, the uname -m form). The tag
// suffix deliberately differs from Go's GOARCH spelling.
func archTag() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return runtime.GOARCH
	}
}

func rolodexTestImage() string {
	img := os.Getenv("ROLODEX_IMAGE")
	if img == "" {
		// Internal Town OS pulls default to the host's per-arch rc tag
		// (rc.latest-x86_64 / rc.latest-aarch64). The plain rc.latest
		// (no arch suffix) is a multi-arch manifest and must never be
		// used. archTag() maps the Go runtime arch to the tag suffix.
		img = "quay.io/town/rolodex:rc.latest-" + archTag()
	}
	ensureImagePulled(img)
	return img
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

// writeRolodexBootstrapConfig writes the config file the INSTALL IMAGE writes
// on a real box (scripts/rolodex-config.sh in ../install), which is the only
// config rolodex ever reads from disk: the DNS binds, the metrics listener, and
// the two boot defaults (mode and forwarders) that give the window before the
// controller's first push a defined state. The ports are parameterised only
// because tests relocate them; on a real box every value here is a constant.
//
// Town OS deliberately writes no config file — forwarders, resolution mode and
// both blocklists are programmed into the running server over gRPC — so a test
// that starts a real rolodex has to stand in for the install image here. It is
// spelled out rather than generated so it keeps matching what that script
// actually emits; if the two drift, a test rolodex stops resembling a real one.
//
// One stanza is deliberately NOT in the install script: `address_family: off`.
// rolodex defaults that to `auto`, which TCP-connects to 1.1.1.1:443 and
// [2606:4700:4700::1111]:443 to decide whether the host can route each family,
// and then filters A or AAAA answers out of every response for the family it
// could not reach. On a real box that is exactly right. In the harness it makes
// the answer depend on the build machine's internet — a container with no v6
// route serves NODATA for every AAAA this suite publishes, so a test asserting
// the record it just wrote fails on the network rather than on the code — and it
// puts a recurring outbound probe to a hardcoded public address inside every
// test rolodex, on captive networks included. `off` answers both families and
// probes nothing.
func writeRolodexBootstrapConfig(t *testing.T, dataDir, dnsPort, metricsPort string) {
	t.Helper()

	if metricsPort == "" {
		metricsPort = rolodex.DefaultMetricsPort
	}
	config := fmt.Sprintf(`database_path: /data/rolodex.db
dns:
  bind:
    - udp: "%s:%s"
    - tcp: "%s:%s"
grpc:
  tcp_bind: ""
  unix_socket: /data/rolodex.sock
  shared_secret: ""
forwarders:
  - "8.8.8.8:53"
  - "8.8.4.4:53"
resolution:
  mode: auto
address_family:
  mode: "off"
metrics:
  bind: "%s:%s"
`,
		rolodex.DNSLoopback, dnsPort, rolodex.DNSLoopback, dnsPort,
		rolodex.DNSLoopback, metricsPort,
	)
	if err := os.WriteFile(filepath.Join(dataDir, "rolodex.yml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write bootstrap rolodex.yml: %v", err)
	}
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
	return initRolodexRealTestForwarders(t, nil)
}

// initRolodexRealTestForwarders is initRolodexRealTest with custom upstream
// DNS forwarders written into rolodex.yml. Forwarding tests point this at a
// local stub DNS server so they work without internet access.
func initRolodexRealTestForwarders(t *testing.T, forwarders []string) (rolodex.Client, string) {
	t.Helper()
	return initRolodexRealTestWith(t, forwarders, rolodex.Blocklist{})
}

// initRolodexRealTestWith is initRolodexRealTestForwarders with blocklist
// provider lists rendered into rolodex.yml, so a test can prove what a rolodex
// starting from that file alone comes up holding.
func initRolodexRealTestWith(t *testing.T, forwarders []string, dnsbl rolodex.Blocklist) (rolodex.Client, string) {
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
		Forwarders:     forwarders,
		// This helper exists specifically to exercise forwarding to a stub
		// upstream; rolodex's default is recursive-from-roots, so opt into
		// forward mode here.
		ResolutionMode: rolodex.ResolutionModeForward,
		DNSBL:          dnsbl,
	})

	writeRolodexBootstrapConfig(t, dataDir, dnsPort, "")

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
		logCleanupf(t, sd.SetStatus(cleanupCtx, unitName, systemd.Stop), "SetStatus(stop)")
		logCleanupf(t, sd.UninstallUnit(cleanupCtx, unitName), "UninstallUnit")
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

	// Program the settings that do not live in the config file, exactly as a
	// real box does once rolodex is up. Without this a test rolodex holds
	// rolodex's defaults — public forwarders, auto mode, no blocklists — no
	// matter what the caller put in the manager.
	if err := systemcontroller.ProgramRolodex(ctx, client, mgr, nil); err != nil {
		t.Fatalf("ProgramRolodex: %v", err)
	}

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

	writeRolodexBootstrapConfig(t, dataDir, dnsPort, "")

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
		logCleanupf(t, sd.SetStatus(ctx, unitName, systemd.Stop), "SetStatus(stop)")
		logCleanupf(t, sd.UninstallUnit(ctx, unitName), "UninstallUnit")
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
	t.Parallel()
	client, _ := initRolodexRealTest(t)
	ctx := context.Background()

	// Connect: a fresh rolodex instance has no records.
	records, err := client.ListRecords(ctx, nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records on fresh instance, got %d", len(records))
	}

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
	records, err = client.ListRecords(ctx, nil)
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
}

// stubDNSAnswer builds a DNS response for a raw query packet, answering A
// queries with 192.0.2.55 (TEST-NET-1) and AAAA queries with 2001:db8::55.
// Other query types get an empty NOERROR response. Returns nil for packets
// too short to be DNS queries.
func stubDNSAnswer(query []byte) []byte {
	if len(query) < 12 {
		return nil
	}
	// Walk the QNAME labels to find the end of the question section.
	i := 12
	for i < len(query) && query[i] != 0 {
		i += int(query[i]) + 1
	}
	qend := i + 5 // null byte + qtype(2) + qclass(2)
	if qend > len(query) {
		return nil
	}
	qtype := uint16(query[i+1])<<8 | uint16(query[i+2])

	var rdata []byte
	switch qtype {
	case 1: // A
		rdata = []byte{192, 0, 2, 55}
	case 28: // AAAA
		rdata = net.ParseIP("2001:db8::55").To16()
	}

	resp := make([]byte, 0, qend+16+len(rdata))
	// Header: same ID; QR=1 RD=1 RA=1; QDCOUNT=1.
	resp = append(resp, query[0], query[1], 0x81, 0x80, 0x00, 0x01)
	if rdata == nil {
		resp = append(resp, 0x00, 0x00) // ANCOUNT=0
	} else {
		resp = append(resp, 0x00, 0x01) // ANCOUNT=1
	}
	resp = append(resp, 0x00, 0x00, 0x00, 0x00) // NSCOUNT, ARCOUNT
	resp = append(resp, query[12:qend]...)      // question section verbatim
	if rdata != nil {
		resp = append(resp, 0xC0, 0x0C)                              // name: pointer to QNAME
		resp = append(resp, byte(qtype>>8), byte(qtype), 0x00, 0x01) // type, class IN
		resp = append(resp, 0x00, 0x00, 0x00, 0x3C)                  // TTL 60
		resp = append(resp, 0x00, byte(len(rdata)))                  // RDLENGTH
		resp = append(resp, rdata...)
	}
	return resp
}

// startStubDNS starts a minimal UDP DNS server on a random loopback port and
// returns its address. It lets forwarding tests run hermetically: captive
// networks block direct queries to public resolvers, and the tests verify
// rolodex's forwarding mechanics, not internet reachability.
func startStubDNS(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp for stub DNS: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Logf("close stub DNS: %v", err)
		}
	})
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // listener closed by cleanup
			}
			resp := stubDNSAnswer(buf[:n])
			if resp == nil {
				continue
			}
			if _, err := conn.WriteToUDP(resp, addr); err != nil {
				return
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestRolodexDNSQueryForwarding(t *testing.T) {
	t.Parallel()

	// Forward to a local stub upstream instead of public resolvers so the
	// test is hermetic and works without internet access.
	upstreamAddr := startStubDNS(t)
	_, dnsPort := initRolodexRealTestForwarders(t, []string{upstreamAddr})

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
			t.Fatalf("LookupHost(%s): %v (rolodex did not forward to the stub upstream)", domain, resolveErr)
		}
		if !slices.Contains(addrs, "192.0.2.55") {
			t.Fatalf("expected stub answer 192.0.2.55 for %s, got %v", domain, addrs)
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

// rolodexClientWaitTimeout is the hard upper bound on how long
// waitForRolodexClient blocks waiting for the gRPC unix socket to come up.
// A healthy rolodex container is ready in under a second; anything past 30s
// means the unit is failing to start (config parse error, image pull,
// crash loop, etc.) and waiting longer just hides the failure.
const rolodexClientWaitTimeout = 30 * time.Second

// rolodexUnitFailureSubstrings are journal phrases that indicate rolodex-dns
// has crashed at startup and is not going to recover on its own. Hitting any
// of these short-circuits the poll loop with a specific error.
var rolodexUnitFailureSubstrings = []string{
	"failed to parse config file",
	"missing field",
	"Address already in use",
	"panicked at",
	"Error: failed to",
}

// waitForRolodexClient waits for the rolodex Unix socket to become available
// and returns a connected client. The wait is hard-capped at
// rolodexClientWaitTimeout (independent of the parent ctx, which is derived
// from -test.timeout and would otherwise let a wedged container hang the
// suite for ~60 minutes). On every poll iteration it also inspects the
// systemd unit and recent journal entries; a failed/crash-looping unit or a
// known-fatal error message in the journal aborts the wait immediately
// instead of polling until the deadline.
func waitForRolodexClient(t *testing.T, ctx context.Context, socketPath, dataDir, key string) rolodex.Client {
	t.Helper()

	deadline := time.Now().Add(rolodexClientWaitTimeout)
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	unitName := systemd.SystemServiceUnitName(key)
	poll := 200 * time.Millisecond

	var lastDialErr, lastRPCErr error
	for waitCtx.Err() == nil {
		// Cheap fail-fast check: if the unit has crashed or the journal
		// already shows a known-fatal message, stop polling.
		if reason := rolodexUnitFatal(waitCtx, unitName); reason != "" {
			dumpRolodexDiagnostics(ctx, t, dataDir, key)
			t.Fatalf("waitForRolodexClient: %s", reason)
		}

		attemptCtx, attemptCancel := context.WithTimeout(waitCtx, 2*time.Second)
		client, err := rolodex.Dial(attemptCtx, socketPath)
		if err != nil {
			lastDialErr = err
			attemptCancel()
			time.Sleep(poll)
			continue
		}
		// Verify the connection is actually live.
		_, err = client.ListRecords(attemptCtx, nil)
		attemptCancel()
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
	t.Fatalf("waitForRolodexClient: timed out after %s waiting for %s", rolodexClientWaitTimeout, socketPath)
	return nil
}

// rolodexUnitFatal returns a non-empty reason string when the rolodex unit
// is in a state that means waiting longer is pointless. It checks both the
// systemd ActiveState (failed unit) and the recent journal for known-fatal
// startup errors. All shell-outs use a tight timeout so a slow systemctl
// call cannot itself wedge the test.
func rolodexUnitFatal(ctx context.Context, unitName string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Read ActiveState/NRestarts/Result via `systemctl show`. A non-active
	// Result (exit-code/signal) means rolodex-dns crashed at startup, even
	// if Restart=always will eventually retry.
	if out, err := exec.CommandContext(probeCtx, "systemctl", "show", unitName,
		"--property=ActiveState", "--property=NRestarts", "--property=Result").Output(); err == nil {
		props := string(out)
		if strings.Contains(props, "Result=exit-code") || strings.Contains(props, "Result=signal") {
			return "rolodex unit reported fatal Result: " + strings.TrimSpace(props)
		}
		// NRestarts climbing means Restart=always is masking a crash loop.
		// Three restarts in the wait window is enough to call it.
		for line := range strings.SplitSeq(props, "\n") {
			if rest, ok := strings.CutPrefix(line, "NRestarts="); ok {
				if n, convErr := strconv.Atoi(strings.TrimSpace(rest)); convErr == nil && n >= 3 {
					return "rolodex unit is crash-looping (NRestarts=" + rest + ")"
				}
			}
		}
	}

	// Journal scan: look for any of the known-fatal phrases. journalctl
	// with -n 50 is bounded and fast.
	if out, err := exec.CommandContext(probeCtx, "journalctl", "-u", unitName,
		"-n", "50", "--no-pager", "-o", "cat").Output(); err == nil {
		log := string(out)
		for _, sub := range rolodexUnitFailureSubstrings {
			if strings.Contains(log, sub) {
				return "rolodex unit journal shows fatal error (" + sub + ")"
			}
		}
	}

	return ""
}
