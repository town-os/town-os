package systemcontroller

import (
	"context"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
)

// The designation is what makes the DoH vhost findable. Without it a client has
// to be told about `dns.<tld>` by hand, which is the state encrypted DNS was in
// before: built, reachable, and discovered by nobody.
func TestDDRDesignationNamesEveryTransport(t *testing.T) {
	t.Parallel()

	got := ddrDesignations("dns.fart")
	if len(got) != 3 {
		t.Fatalf("designations = %v, want one per transport", got)
	}

	// Preference order is load-bearing: a client walks the list and stops at the
	// first endpoint it can reach, and :443 survives the DPI that filters DoT's
	// :853 — the same reason rolodex's own upstream chain prefers DoH.
	for i, want := range []string{
		"1 dns.fart. alpn=h2 port=443 dohpath=/dns-query{?dns}",
		"2 dns.fart. alpn=dot port=853",
		"3 dns.fart. alpn=doq port=853",
	} {
		if got[i] != want {
			t.Errorf("designation %d = %q, want %q", i, got[i], want)
		}
	}
}

// The published DoH port is the INGRESS's 443, not rolodex's own 4443. A
// designation naming 4443 would send every client at a loopback-only listener
// holding a self-signed certificate — reachable from nowhere, and untrusted if
// it were.
func TestDDRDesignationPublishesTheIngressPortNotRolodexes(t *testing.T) {
	t.Parallel()

	got := ddrDesignations("dns.fart")[0]
	if !strings.Contains(got, "port=443") {
		t.Errorf("DoH designation = %q, want the ingress port 443", got)
	}
	if strings.Contains(got, "4443") {
		t.Errorf("DoH designation = %q must not name rolodex's internal port", got)
	}
	if !strings.Contains(RolodexDohBackend, "4443") {
		t.Fatal("this test is anchored to RolodexDohBackend being the 4443 one")
	}
}

// A box with no TLD publishes nothing, rather than a designation for a bare
// "dns." — the same rule dohIngressRoute and publishDohRecord follow.
func TestDDRDesignationNeedsATLD(t *testing.T) {
	t.Parallel()

	if got := ddrDesignations(""); got != nil {
		t.Errorf("designations = %v with no hostname, want none", got)
	}

	mock := &rolodex.MockClient{}
	publishDDRDesignation(context.Background(), mock, "")
	if mock.Called("AddRecord") {
		t.Error("published a designation with no TLD to name it after")
	}
	if mock.Called("RemoveRecord") {
		t.Error("cleared the designation before deciding there was nothing to publish")
	}
}

// Republishing must not stack copies. `RebuildDNS` tears the TLD's zone down
// first, which is what makes every other publisher idempotent — but this name
// is in `arpa.`, which is in no zone this box owns, so the teardown never
// reaches it.
func TestDDRDesignationIsIdempotentAcrossRebuilds(t *testing.T) {
	t.Parallel()

	mock := &rolodex.MockClient{}
	ctx := context.Background()
	publishDDRDesignation(ctx, mock, "fart")
	publishDDRDesignation(ctx, mock, "fart")

	records, err := mock.ListRecords(ctx, nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	var svcb int
	for _, r := range records {
		if r.Name == DDRDesignationName {
			svcb++
			if r.RecordType != upstream.RecordTypeSVCB {
				t.Errorf("designation record type = %v, want SVCB", r.RecordType)
			}
		}
	}
	if svcb != 3 {
		t.Errorf("after two publishes there are %d designation records, want 3", svcb)
	}
}

// The clearing call has to NAME the type it is clearing. RemoveRecordOptions
// documents nil as "every type at this name", but `record_type` is a plain
// proto3 enum — unset and A are the same byte — so rolodex reads a nil as "A"
// and removes nothing from a name that holds only SVCB. It answers success with
// a count of zero, so the only evidence is designations piling up one rebuild at
// a time. This asserts the options that actually go over the wire, because the
// idempotence test above can only see the outcome the mock produces.
func TestDDRDesignationClearsBySVCBType(t *testing.T) {
	t.Parallel()

	mock := &rolodex.MockClient{}
	publishDDRDesignation(context.Background(), mock, "fart")

	var found bool
	for _, call := range mock.GetCalls() {
		if call.Method != "RemoveRecord" {
			continue
		}
		found = true
		if len(call.Args) != 2 {
			t.Fatalf("RemoveRecord recorded %d args, want name and options", len(call.Args))
		}
		opts, ok := call.Args[1].(*upstream.RemoveRecordOptions)
		if !ok || opts == nil {
			t.Fatalf("RemoveRecord options = %#v, want a *RemoveRecordOptions naming SVCB", call.Args[1])
		}
		if opts.RecordType == nil || *opts.RecordType != upstream.RecordTypeSVCB {
			t.Errorf("RemoveRecord record type = %v, want SVCB", opts.RecordType)
		}
	}
	if !found {
		t.Error("the designation was published without clearing the old one first")
	}
}

// Changing the TLD must move the designation, not add to it. A client that gets
// both would try the old TLD's endpoints first — a name that no longer resolves
// and no certificate covers.
func TestDDRDesignationFollowsATLDChange(t *testing.T) {
	t.Parallel()

	mock := &rolodex.MockClient{}
	ctx := context.Background()
	publishDDRDesignation(ctx, mock, "fart")
	publishDDRDesignation(ctx, mock, "lan")

	records, err := mock.ListRecords(ctx, nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	for _, r := range records {
		if r.Name != DDRDesignationName {
			continue
		}
		if strings.Contains(r.Value, "dns.fart") {
			t.Errorf("designation still advertises the old TLD: %q", r.Value)
		}
		if !strings.Contains(r.Value, "dns.lan") {
			t.Errorf("designation = %q, want the new TLD", r.Value)
		}
	}
}

// Every value published has to be one rolodex will actually accept: it parses
// SVCB on the way in and refuses what it cannot serve, so a malformed
// designation would be rejected at the API rather than stored and skipped.
// These are the exact strings that go over the wire.
func TestDDRDesignationValuesAreWellFormed(t *testing.T) {
	t.Parallel()

	for _, value := range ddrDesignations("dns.fart") {
		fields := strings.Fields(value)
		if len(fields) < 3 {
			t.Fatalf("designation %q has too few fields", value)
		}
		// "<priority> <target> [key=value ...]" — the target must be a FQDN, or
		// a client resolves it relative to something and misses.
		if !strings.HasSuffix(fields[1], ".") {
			t.Errorf("designation %q target %q is not fully qualified", value, fields[1])
		}
		for _, param := range fields[2:] {
			if !strings.Contains(param, "=") {
				t.Errorf("designation %q has a valueless parameter %q", value, param)
			}
		}
	}
}

// The status endpoint publishes the endpoints a person can read; the DDR
// designation publishes the ones a client can find. They must agree, or the
// screen tells the operator one thing while clients are handed another.
func TestEncryptedDNSViewAgreesWithTheDesignation(t *testing.T) {
	t.Parallel()

	view := encryptedDNSView("fart")
	if view == nil {
		t.Fatal("a box with a TLD must publish its encrypted endpoints")
	}
	if want := "https://dns.fart/dns-query"; view.DohURL != want {
		t.Errorf("DohURL = %q, want %q", view.DohURL, want)
	}
	if view.DotName != "dns.fart" || view.DoqName != "dns.fart" {
		t.Errorf("DoT/DoQ names = %q/%q, want dns.fart", view.DotName, view.DoqName)
	}
	if view.DotPort != DoTPort || view.DoqPort != DoQPort {
		t.Errorf("ports = %d/%d, want %d/%d", view.DotPort, view.DoqPort, DoTPort, DoQPort)
	}
	if view.Discovery != DDRDesignationName {
		t.Errorf("Discovery = %q, want %q", view.Discovery, DDRDesignationName)
	}

	// The designation must name the same host and ports the screen shows.
	designations := ddrDesignations("dns.fart")
	if !strings.Contains(designations[0], "port=443") {
		t.Errorf("designation %q disagrees with the published DoH URL", designations[0])
	}
	for _, d := range designations {
		if !strings.Contains(d, view.DotName+".") {
			t.Errorf("designation %q names a different host than the status view %q", d, view.DotName)
		}
	}
}

// No TLD, nothing to publish — the same rule the vhost and the designation
// follow, so the screen does not offer an endpoint that resolves to nothing.
func TestEncryptedDNSViewNeedsATLD(t *testing.T) {
	t.Parallel()

	if got := encryptedDNSView(""); got != nil {
		t.Errorf("encryptedDNSView = %+v with no TLD, want nil", got)
	}
}

// The designation is published as SVCB, which is the one thing about it a
// client cannot recover from being wrong: DDR is defined over SVCB records
// (RFC 9462 §4), so a designation stored under any other type is a record no
// client will ever ask for.
//
// This replaced a bridge constant. `ingress_ddr.go` spelled the enum value out
// as `upstream.RecordType(22)` while go.mod pinned a rolodex-dns that predated
// SVCB — a wire number written in a second repo with no way to read across.
// The pin now carries the generated enum, so the number is upstream's to
// choose and this asserts only what this repo decides: which type the
// designation goes out as.
func TestDDRDesignationIsPublishedAsSVCB(t *testing.T) {
	t.Parallel()

	mock := &rolodex.MockClient{}
	publishDDRDesignation(context.Background(), mock, "fart")

	if len(mock.Records) == 0 {
		t.Fatal("no designation was published")
	}
	for _, r := range mock.Records {
		if r.RecordType != upstream.RecordTypeSVCB {
			t.Errorf("designation %q is a %s record, want SVCB — no DDR client queries any other type",
				r.Value, r.RecordType)
		}
	}
}
