// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// dohTestCA builds a CA in a temp btrfs base, the way boot does.
func dohTestCA(t *testing.T) (*townostls.CA, string) {
	t.Helper()
	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	return ca, btrfsBase
}

// TestDohIngressRouteFrontsRolodexOverTLS is the contract that makes DoH usable.
//
// rolodex terminates its own TLS with a self-signed certificate, which every
// validating DoH client refuses. The route exists so the ingress presents a leaf
// from the box's CA and proxies the internal hop with verification skipped — so
// the certificate a client checks is the ingress's, and rolodex's is a detail of
// a hop that never leaves the box. A route without BackendTls would send
// plaintext at that TLS socket and every query would 502.
func TestDohIngressRouteFrontsRolodexOverTLS(t *testing.T) {
	t.Parallel()

	ca, btrfsBase := dohTestCA(t)

	route := dohIngressRoute(ca, btrfsBase, "home", "192.168.1.10")
	if route == nil {
		t.Fatal("dohIngressRoute returned nil with a CA and a TLD")
	}
	if got, want := route.GetHostname(), "dns.home"; got != want {
		t.Errorf("hostname = %q, want %q", got, want)
	}
	if got, want := route.GetBackend(), RolodexDohBackend; got != want {
		t.Errorf("backend = %q, want %q", got, want)
	}
	if !route.GetBackendTls() {
		t.Error("backend_tls is false; the ingress would send plaintext at rolodex's TLS listener")
	}
	if route.GetCertDir() == "" {
		t.Error("cert dir is empty; caddy rejects the whole config over one such route")
	}
}

// The port is written in two repos that cannot read each other — here, and
// `doh.bind` in ../install's scripts/rolodex-config.sh. If they disagree the
// ingress proxies to a closed port and DoH 502s with nothing to say why, so the
// value is pinned rather than merely referenced.
func TestRolodexDohBackendIsTheLoopbackPortInstallWrites(t *testing.T) {
	t.Parallel()

	if RolodexDohBackend != "127.0.0.2:4443" {
		t.Errorf("RolodexDohBackend = %q; ../install writes doh.bind: \"127.0.0.2:4443\"", RolodexDohBackend)
	}
}

// installRolodexConfigScript locates ../install's rolodex-config.sh, the script
// that writes the other half of this contract.
//
// The two repositories sit side by side in a normal checkout, so a walk up from
// the test's working directory finds it; TOWN_OS_INSTALL_DIR overrides that for
// a layout where they do not. Inside the integration container neither exists —
// only this repo is copied in — so the test skips rather than failing, which is
// the honest outcome: it can check the agreement wherever both halves are
// present, and nowhere else.
func installRolodexConfigScript(t *testing.T) string {
	t.Helper()

	if dir := os.Getenv("TOWN_OS_INSTALL_DIR"); dir != "" {
		return filepath.Join(dir, "scripts", "rolodex-config.sh")
	}

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		candidate := filepath.Join(dir, "..", "install", "scripts", "rolodex-config.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("../install is not checked out beside this repo; set TOWN_OS_INSTALL_DIR to check the DoH backend contract")
	return ""
}

// dohBindFromInstallScript pulls the `bind:` value out of the script's `doh:`
// section — the first bind after the section header, which is how the heredoc
// is laid out (doh, then dot, then doq, each with its own bind).
func dohBindFromInstallScript(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var inDoh bool
	for line := range strings.SplitSeq(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "doh:":
			inDoh = true
		case inDoh && strings.HasPrefix(trimmed, "bind:"):
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "bind:")), `"'`)
		case inDoh && (trimmed == "dot:" || trimmed == "doq:"):
			t.Fatalf("%s: the doh: section has no bind:", path)
		}
	}
	t.Fatalf("%s: no doh: section found; the install image no longer opens a DoH listener, and this repo still programs a route to one", path)
	return ""
}

// TestRolodexDohBackendMatchesTheInstallScript is the check the constant's own
// comment asks for. RolodexDohBackend and `doh.bind` in ../install's
// rolodex-config.sh are the same number written in two repositories that cannot
// read each other, and neither side fails when they drift: the install image
// opens a listener nothing proxies to, the ingress proxies to a closed port, and
// every DoH query 502s with nothing anywhere saying why.
//
// TestRolodexDohBackendIsTheLoopbackPortInstallWrites pins this side against a
// literal, which catches an edit here. This catches an edit there — on any box
// where both repositories are checked out, which is every box where somebody
// could make that edit.
func TestRolodexDohBackendMatchesTheInstallScript(t *testing.T) {
	t.Parallel()

	path := installRolodexConfigScript(t)
	if got := dohBindFromInstallScript(t, path); got != RolodexDohBackend {
		t.Errorf("%s writes doh.bind: %q, RolodexDohBackend is %q — the ingress would proxy DoH to a closed port", path, got, RolodexDohBackend)
	}
}

// A leaf really is issued, and under its own repo so reissuing it cannot
// disturb the pages or gfeh leaves.
func TestDohIngressRouteIssuesItsOwnLeaf(t *testing.T) {
	t.Parallel()

	ca, btrfsBase := dohTestCA(t)

	if route := dohIngressRoute(ca, btrfsBase, "home", "192.168.1.10"); route == nil {
		t.Fatal("dohIngressRoute returned nil")
	}

	leafDir := hostTLSLeafDir(btrfsBase, DohLeafRepo, DohLeafName, DohLeafVersion)
	for _, name := range []string{townostls.LeafCertFileName, townostls.LeafKeyFileName} {
		if _, err := os.Stat(filepath.Join(leafDir, name)); err != nil {
			t.Errorf("leaf %s not issued into %s: %v", name, leafDir, err)
		}
	}
}

// No CA, no btrfs base, or no TLD means no route — not a route with an empty
// cert dir. Caddy rejects a config containing one of those outright, which
// takes every other vhost on the box down to fix a service that is merely
// absent. The pages routes follow the same rule.
func TestDohIngressRouteSkipsRatherThanRenderingAnEmptyCertDir(t *testing.T) {
	t.Parallel()

	ca, btrfsBase := dohTestCA(t)

	if route := dohIngressRoute(nil, btrfsBase, "home", "192.168.1.10"); route != nil {
		t.Errorf("route built with no CA: %+v", route)
	}
	if route := dohIngressRoute(ca, "", "home", "192.168.1.10"); route != nil {
		t.Errorf("route built with no btrfs base: %+v", route)
	}
	if route := dohIngressRoute(ca, btrfsBase, "", "192.168.1.10"); route != nil {
		t.Errorf("route built with no TLD: %+v", route)
	}
}

// dohDNSFixture is a DNS reconcile config with one installed package, which is
// all these tests need: they are about the one name no package accounts for.
func dohDNSFixture(t *testing.T) (*rolodex.MockClient, ReconcileDNSConfig) {
	t.Helper()

	rr, inst := setupReconcileRepo(t, map[string]string{"nginx/1.0": "image: nginx:1.0\n"})
	if err := inst.Install("repo-a", "nginx", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("pre-install nginx: %v", err)
	}
	mock := &rolodex.MockClient{}
	return mock, ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}},
		InternalIP:     "192.168.1.100",
		InternalIPv6:   "2001:db8::50",
	}
}

// recordValues returns the values every record for name carries, by type.
func recordValues(records []*upstream.DnsRecord, name string, rtype upstream.RecordType) []string {
	var out []string
	for _, r := range records {
		if r.Name == name && r.RecordType == rtype {
			out = append(out, r.Value)
		}
	}
	return out
}

// TestRebuildDNSPublishesTheDoHEndpointsName is the other half of the DoH
// vhost. dohIngressRoute names a Caddy site and issues a leaf for dns.<tld>,
// and nothing else on the box ever names it: it has no package, no page and no
// object-storage partition behind it. Without a record here the endpoint fails
// at resolution — before TLS, before the ingress is reached — and reads to a
// client exactly like a feature that was never built.
func TestRebuildDNSPublishesTheDoHEndpointsName(t *testing.T) {
	mock, cfg := dohDNSFixture(t)

	if err := RebuildDNS(context.Background(), cfg); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}

	if got := recordValues(mock.Records, "dns.lan.", upstream.RecordTypeA); len(got) != 1 || got[0] != "192.168.1.100" {
		t.Errorf("A records for dns.lan. = %v, want [192.168.1.100]", got)
	}
	// The leaf carries both host addresses as SANs, so a client arriving over
	// v6 has to be able to find the box over v6 as well.
	if got := recordValues(mock.Records, "dns.lan.", upstream.RecordTypeAAAA); len(got) != 1 || got[0] != "2001:db8::50" {
		t.Errorf("AAAA records for dns.lan. = %v, want [2001:db8::50]", got)
	}
	// The control for both assertions above. `dns.` is published under the
	// configured TLD and nowhere else, so a helper that ignored the name it was
	// given — or a RebuildDNS that sprayed the record across every zone it knew
	// — would satisfy the two checks above and fail only here.
	if got := recordValues(mock.Records, "dns.example.", upstream.RecordTypeA); len(got) != 0 {
		t.Errorf("A records for dns.example. = %v, want none", got)
	}
}

// TestReconcileDNSKeepsTheDoHEndpointsName pins the failure that is easiest to
// miss: the hourly reconcile deletes every A/AAAA in the zone it cannot account
// for, so a name published only by RebuildDNS survives the boot and disappears
// an hour later, leaving a vhost and a leaf for a name that no longer resolves.
// The first pass must publish it and the second must leave it alone.
func TestReconcileDNSKeepsTheDoHEndpointsName(t *testing.T) {
	mock, cfg := dohDNSFixture(t)

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}
	if got := recordValues(mock.Records, "dns.lan.", upstream.RecordTypeA); len(got) != 1 {
		t.Fatalf("A records for dns.lan. after first pass = %v, want exactly one", got)
	}

	mock.Calls = nil
	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS second pass: %v", err)
	}
	for _, c := range mock.GetCalls() {
		if c.Method != "RemoveRecord" {
			continue
		}
		name, ok := c.Args[0].(string)
		if !ok {
			t.Fatal("RemoveRecord arg is not a string")
		}
		if name == "dns.lan." {
			t.Error("the second pass removed dns.lan. as an orphan; the DoH name is missing from the desired set")
		}
	}
	if got := recordValues(mock.Records, "dns.lan.", upstream.RecordTypeA); len(got) != 1 {
		t.Errorf("A records for dns.lan. after second pass = %v, want exactly one", got)
	}
}

// A box with no TLD publishes no DoH record rather than a record for a bare
// "dns." — the same rule that makes dohIngressRoute return nil.
func TestDohRecordNameNeedsATLD(t *testing.T) {
	t.Parallel()

	if got, want := dohRecordName("fart"), "dns.fart."; got != want {
		t.Errorf("dohRecordName = %q, want %q", got, want)
	}
	if got := dohRecordName(""); got != "" {
		t.Errorf("dohRecordName = %q with no TLD, want empty", got)
	}

	mock := &rolodex.MockClient{}
	publishDohRecord(context.Background(), mock, "", "192.168.1.100", "")
	if mock.Called("AddRecord") {
		t.Error("published a DoH record with no TLD to name it after")
	}
}

// The vhost is named under the box's TLD, so a box on a different TLD serves
// DoH under that one rather than a hardcoded .home.
func TestDohIngressHostnameFollowsTheTLD(t *testing.T) {
	t.Parallel()

	if got, want := dohIngressHostname("fart"), "dns.fart"; got != want {
		t.Errorf("hostname = %q, want %q", got, want)
	}
	if got := dohIngressHostname(""); got != "" {
		t.Errorf("hostname = %q with no TLD, want empty", got)
	}
}
