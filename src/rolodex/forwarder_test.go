package rolodex

import (
	"strings"
	"testing"
)

// These specs are the wire contract with rolodex's own parser
// (`src/forwarder.rs` in ../rolodex-dns). Nothing checks the two agree at build
// time — they are separate repositories with no shared generated code — so the
// cases here are deliberately the same ones that file pins, and a change to
// either side that is not made to both shows up as a forwarder Town OS accepts
// and rolodex refuses.

func TestParseForwarderTreatsABareAddressAsPlaintextUDP(t *testing.T) {
	t.Parallel()

	got, err := ParseForwarder("8.8.8.8:53")
	if err != nil {
		t.Fatalf("ParseForwarder: %v", err)
	}
	if got.Transport != TransportDo53UDP {
		t.Fatalf("Transport = %q, want %q", got.Transport, TransportDo53UDP)
	}
	if got.Encrypted() {
		t.Fatal("a bare address must not be reported as encrypted")
	}
	if got.ServerName != "" {
		t.Fatalf("ServerName = %q, want empty for plaintext", got.ServerName)
	}
}

func TestParseForwarderRecognisesEveryTransport(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec      string
		transport string
		encrypted bool
	}{
		{"8.8.8.8:53", TransportDo53UDP, false},
		{"udp://8.8.8.8:53", TransportDo53UDP, false},
		{"tcp://8.8.8.8:53", TransportDo53TCP, false},
		{"tls://dns.google@8.8.8.8:853", TransportDoT, true},
		{"dot://dns.google@8.8.8.8:853", TransportDoT, true},
		{"https://cloudflare-dns.com@1.1.1.1/dns-query", TransportDoH, true},
		{"doh://cloudflare-dns.com@1.1.1.1/dns-query", TransportDoH, true},
		{"quic://dns.adguard.com@94.140.14.14:853", TransportDoQ, true},
		{"doq://dns.adguard.com@94.140.14.14:853", TransportDoQ, true},
	}

	for _, tc := range cases {
		got, err := ParseForwarder(tc.spec)
		if err != nil {
			t.Fatalf("ParseForwarder(%q): %v", tc.spec, err)
		}
		if got.Transport != tc.transport {
			t.Errorf("%q: Transport = %q, want %q", tc.spec, got.Transport, tc.transport)
		}
		if got.Encrypted() != tc.encrypted {
			t.Errorf("%q: Encrypted() = %v, want %v", tc.spec, got.Encrypted(), tc.encrypted)
		}
	}
}

// The default port is a property of the transport, so a spec that names none is
// still fully resolved. Getting this wrong sends DoT to :53, where nothing is
// listening for it.
func TestParseForwarderSuppliesTheTransportsDefaultPort(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"udp://8.8.8.8":                       "8.8.8.8:53",
		"tcp://8.8.8.8":                       "8.8.8.8:53",
		"tls://dns.google@8.8.8.8":            "8.8.8.8:853",
		"https://cloudflare-dns.com@1.1.1.1":  "1.1.1.1:443",
		"quic://dns.adguard.com@94.140.14.14": "94.140.14.14:853",
	}
	for spec, want := range cases {
		got, err := ParseForwarder(spec)
		if err != nil {
			t.Fatalf("ParseForwarder(%q): %v", spec, err)
		}
		if got.Addr != want {
			t.Errorf("%q: Addr = %q, want %q", spec, got.Addr, want)
		}
	}
}

// The name and the address are separate halves of an encrypted upstream: the
// address is dialed, the name is validated. Collapsing them would mean either
// dialing a hostname (which needs the resolver this forwarder is FOR) or
// validating against an address that is not in the certificate.
func TestParseForwarderKeepsTheNameAndTheAddressApart(t *testing.T) {
	t.Parallel()

	got, err := ParseForwarder("tls://cloudflare-dns.com@1.1.1.1:853")
	if err != nil {
		t.Fatalf("ParseForwarder: %v", err)
	}
	if got.Addr != "1.1.1.1:853" {
		t.Errorf("Addr = %q, want 1.1.1.1:853", got.Addr)
	}
	if got.ServerName != "cloudflare-dns.com" {
		t.Errorf("ServerName = %q, want cloudflare-dns.com", got.ServerName)
	}
}

// Omitting the name validates against the address's own IP SANs. It has to
// produce a name rather than an empty one, or the caller cannot tell "validate
// against the IP" from "validate against nothing".
func TestParseForwarderFallsBackToTheAddressAsTheName(t *testing.T) {
	t.Parallel()

	got, err := ParseForwarder("https://1.1.1.1/dns-query")
	if err != nil {
		t.Fatalf("ParseForwarder: %v", err)
	}
	if got.ServerName != "1.1.1.1" {
		t.Errorf("ServerName = %q, want 1.1.1.1", got.ServerName)
	}
}

func TestParseForwarderDefaultsTheDoHPath(t *testing.T) {
	t.Parallel()

	got, err := ParseForwarder("https://cloudflare-dns.com@1.1.1.1")
	if err != nil {
		t.Fatalf("ParseForwarder: %v", err)
	}
	if got.Path != DefaultDoHPath {
		t.Errorf("Path = %q, want %q", got.Path, DefaultDoHPath)
	}
}

func TestParseForwarderHandlesIPv6BothWays(t *testing.T) {
	t.Parallel()

	for spec, want := range map[string]string{
		"udp://[2001:4860:4860::8888]:53":         "[2001:4860:4860::8888]:53",
		"tls://dns.google@[2001:4860:4860::8888]": "[2001:4860:4860::8888]:853",
		"udp://2001:4860:4860::8888":              "[2001:4860:4860::8888]:53",
	} {
		got, err := ParseForwarder(spec)
		if err != nil {
			t.Fatalf("ParseForwarder(%q): %v", spec, err)
		}
		if got.Addr != want {
			t.Errorf("%q: Addr = %q, want %q", spec, got.Addr, want)
		}
	}
}

func TestParseForwarderRejectsNonsense(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{
		"",
		"   ",
		"gopher://8.8.8.8:53",
		"udp://",
		"udp://not-an-ip",
		// A hostname with no address: unusable, because resolving it needs the
		// resolver this forwarder exists to provide.
		"tls://dns.google",
		// A name is meaningless without a certificate to check it against.
		"udp://name@8.8.8.8:53",
		// A path is meaningless without HTTP.
		"tls://dns.google@8.8.8.8:853/dns-query",
		"tls://@8.8.8.8:853",
	} {
		if _, err := ParseForwarder(spec); err == nil {
			t.Errorf("ParseForwarder(%q) = nil error, want a rejection", spec)
		}
	}
}

// An error an operator reads has to name the thing they typed, or they cannot
// tell which entry of a list was the bad one.
func TestParseForwarderErrorsNameTheOffendingSpec(t *testing.T) {
	t.Parallel()

	_, err := ParseForwarder("gopher://8.8.8.8:53")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "gopher") {
		t.Fatalf("error does not name the bad spec: %v", err)
	}
}

// Validation is all-or-nothing: rolodex applies a forwarder list as a
// replacement, so accepting a list with one entry silently dropped configures
// the resolver with something the operator did not ask for.
func TestValidateForwardersRejectsTheWholeListForOneBadEntry(t *testing.T) {
	t.Parallel()

	specs := []string{
		"8.8.8.8:53",
		"https://cloudflare-dns.com@1.1.1.1/dns-query",
		"not-a-forwarder",
	}
	if err := ValidateForwarders(specs); err == nil {
		t.Fatal("a list containing an unparseable entry was accepted")
	}
}

// The control for the test above: a list of good entries must pass, including
// the encrypted transports. A validator that rejected everything would satisfy
// the rejection test on its own.
func TestValidateForwardersAcceptsEveryTransport(t *testing.T) {
	t.Parallel()

	specs := []string{
		"8.8.8.8:53",
		"tcp://8.8.8.8:53",
		"tls://dns.google@8.8.8.8:853",
		"https://cloudflare-dns.com@1.1.1.1/dns-query",
		"quic://dns.adguard.com@94.140.14.14:853",
	}
	if err := ValidateForwarders(specs); err != nil {
		t.Fatalf("ValidateForwarders: %v", err)
	}
}

func TestSplitForwarderSpecs(t *testing.T) {
	t.Parallel()

	got := SplitForwarderSpecs(" 8.8.8.8:53 , ,tls://dns.google@8.8.8.8:853,")
	want := []string{"8.8.8.8:53", "tls://dns.google@8.8.8.8:853"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("SplitForwarderSpecs = %v, want %v", got, want)
	}

	if got := SplitForwarderSpecs("   "); len(got) != 0 {
		t.Fatalf("SplitForwarderSpecs(blank) = %v, want empty", got)
	}
}
