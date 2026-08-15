package systemcontroller

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"testing"

	townostls "gitea.com/town-os/town-os/src/tls"
)

// readLeaf parses the certificate the issuer wrote, so the assertions are about
// what a client would actually be presented rather than about a file existing.
func readLeaf(t *testing.T, dir string) *x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, townostls.LeafCertFileName))
	if err != nil {
		t.Fatalf("read leaf: %v", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("leaf is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return cert
}

// The leaf must land where rolodex can actually read it: inside the data
// directory, which is the ONE path its container mounts. Anywhere else and the
// certificate is issued, correct, and invisible — which is indistinguishable
// from not issuing it.
func TestRolodexTransportLeafLandsInTheMountedDataDir(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	dataDir := filepath.Join(base, "rolodex")

	issueRolodexTransportLeaf(ca, dataDir, "fart", "192.168.1.100", "")

	want := filepath.Join(dataDir, RolodexTLSSubdir)
	for _, name := range []string{townostls.LeafCertFileName, townostls.LeafKeyFileName} {
		if _, statErr := os.Stat(filepath.Join(want, name)); statErr != nil {
			t.Errorf("%s not written to %s: %v", name, want, statErr)
		}
	}

	// The container path ../install writes into rolodex.yml is `/data/` plus
	// this subdirectory. If the constant moves, that file has to move with it.
	if RolodexTLSSubdir != "tls/dot" {
		t.Errorf("RolodexTLSSubdir = %q; ../install's scripts/rolodex-config.sh names /data/tls/dot", RolodexTLSSubdir)
	}
}

// The certificate has to name what clients dial. A DoT client checks the
// identity it dialled, so a leaf that names only the hostname fails for a client
// configured by address, and vice versa.
func TestRolodexTransportLeafNamesTheHostAndItsAddresses(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	dataDir := filepath.Join(base, "rolodex")

	issueRolodexTransportLeaf(ca, dataDir, "fart", "192.168.1.100", "fd00::5")
	cert := readLeaf(t, filepath.Join(dataDir, RolodexTLSSubdir))

	if !slices.Contains(cert.DNSNames, "dns.fart") {
		t.Errorf("DNS SANs = %v, want dns.fart", cert.DNSNames)
	}
	var ips []string
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	if !slices.Contains(ips, "192.168.1.100") {
		t.Errorf("IP SANs = %v, want the box's v4 address", ips)
	}
	if !slices.Contains(ips, "fd00::5") {
		t.Errorf("IP SANs = %v, want the box's v6 address", ips)
	}
}

// Issued by the box's CA, not self-signed — that is the whole point. A
// self-signed certificate cannot be verified by a DoT client short of pinning
// it; one from this CA is verifiable by every device the household enrolled.
func TestRolodexTransportLeafIsSignedByTheBoxCA(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	dataDir := filepath.Join(base, "rolodex")

	issueRolodexTransportLeaf(ca, dataDir, "fart", "192.168.1.100", "")
	cert := readLeaf(t, filepath.Join(dataDir, RolodexTLSSubdir))

	if cert.Issuer.String() == cert.Subject.String() {
		t.Errorf("leaf is self-signed (issuer == subject == %q)", cert.Subject)
	}
}

// Reissued on every rebuild, so the SANs follow the box. An address change that
// left the old certificate in place would fail the name check for every client
// dialling the new address.
func TestRolodexTransportLeafFollowsAnAddressChange(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	dataDir := filepath.Join(base, "rolodex")

	issueRolodexTransportLeaf(ca, dataDir, "fart", "192.168.1.100", "")
	issueRolodexTransportLeaf(ca, dataDir, "fart", "10.0.0.5", "")
	cert := readLeaf(t, filepath.Join(dataDir, RolodexTLSSubdir))

	var ips []string
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}
	if !slices.Contains(ips, "10.0.0.5") {
		t.Errorf("IP SANs = %v, want the new address", ips)
	}
	if slices.Contains(ips, "192.168.1.100") {
		t.Errorf("IP SANs = %v still carry the old address", ips)
	}
}

// Nothing to issue for, nothing written. A box with no CA keeps the generated
// certificate rolodex starts with — encrypted DNS still works, it is just
// unverifiable — and a box with no TLD has no name to put in a certificate.
func TestRolodexTransportLeafSkipsWhenThereIsNothingToIssueFor(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	for _, tc := range []struct {
		name    string
		ca      *townostls.CA
		dataDir string
		tld     string
	}{
		{"no CA", nil, filepath.Join(base, "a"), "fart"},
		{"no TLD", ca, filepath.Join(base, "b"), ""},
		{"no data dir", ca, "", "fart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			issueRolodexTransportLeaf(tc.ca, tc.dataDir, tc.tld, "192.168.1.100", "")
			if tc.dataDir == "" {
				return
			}
			if _, statErr := os.Stat(filepath.Join(tc.dataDir, RolodexTLSSubdir, townostls.LeafCertFileName)); statErr == nil {
				t.Error("issued a leaf with nothing to issue it for")
			}
		})
	}
}

// The leaf is only half the job. Issuing it makes the certificate verifiable by
// something that already trusts this box's CA; the TLSA pin is what makes it
// verifiable by something that does not, which is the case the whole feature
// exists for.
//
// Two records for one certificate, because a TLSA record is owned by a service
// endpoint: DoT is 853/tcp and DoQ is 853/udp. The count and both protocols are
// asserted, since publishing one of the two is the silent half-failure — a
// DANE-aware client that finds no record for the transport it picked fails
// closed rather than falling back.
func TestRolodexTransportTLSAPinsBothEncryptedDNSEndpoints(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(base, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	dataDir := filepath.Join(base, "rolodex")
	issueRolodexTransportLeaf(ca, dataDir, "fart", "192.168.1.100", "")

	entries := collectRolodexTransportTLSA(dataDir, "fart")
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2 (DoT and DoQ)", len(entries))
	}

	// The value has to be the pin for the leaf actually on disk. Asserting only
	// that it is non-empty would pass with a constant, which would make every
	// client refuse the connection.
	want, err := tlsaValue(filepath.Join(dataDir, RolodexTLSSubdir, townostls.LeafCertFileName))
	if err != nil {
		t.Fatalf("tlsaValue: %v", err)
	}

	protos := map[string]bool{}
	for _, e := range entries {
		protos[e.Proto] = true
		if e.Name != "dns.fart." {
			t.Errorf("Name = %q, want dns.fart. (the name the DoH vhost publishes)", e.Name)
		}
		if e.Port != 853 {
			t.Errorf("Port = %d, want 853", e.Port)
		}
		if e.Value != want {
			t.Errorf("Value = %q, want the issued leaf's pin %q", e.Value, want)
		}
	}
	for _, p := range []string{"tcp", "udp"} {
		if !protos[p] {
			t.Errorf("no entry for %s; both transports share the certificate and both need a pin", p)
		}
	}
}

// The control for the test above: a pin published for a certificate that has
// not been issued is worse than none, because a DANE client will refuse the
// connection once the real one appears and does not match.
func TestRolodexTransportTLSAPublishesNothingWithoutALeaf(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	for _, tc := range []struct {
		name    string
		dataDir string
		tld     string
	}{
		{"leaf never issued", filepath.Join(base, "empty"), "fart"},
		{"no TLD", base, ""},
		{"no data dir", "", "fart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := collectRolodexTransportTLSA(tc.dataDir, tc.tld); got != nil {
				t.Errorf("entries = %v, want none", got)
			}
		})
	}
}
