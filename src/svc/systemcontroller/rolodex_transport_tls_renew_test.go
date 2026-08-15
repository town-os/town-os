// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// transportRenewalFixture is an hourly-reconcile config with a real CA on a
// temp btrfs base, so the leaf under test is genuinely issued, genuinely
// readable, and genuinely pinned.
func transportRenewalFixture(t *testing.T) (*rolodex.MockClient, ReconcileDNSConfig, string) {
	t.Helper()

	rr, inst := setupReconcileRepo(t, map[string]string{})
	btrfsBase := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	mock := &rolodex.MockClient{}
	return mock, ReconcileDNSConfig{
		Client:         mock,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    &mockSettingsManager{values: map[string]string{"dns_tld": "lan"}},
		InternalIP:     "192.168.1.100",
		InternalIPv6:   "2001:db8::50",
		TLSCA:          ca,
		BtrfsBasePath:  btrfsBase,
	}, btrfsBase
}

// transportLeafDir is where both the issuer and rolodex look for the DoT/DoQ
// leaf, spelled once so a test cannot drift from the code it is checking.
func transportLeafDir(btrfsBase string) string {
	return filepath.Join(btrfsBase, RolodexDataSubdir, RolodexTLSSubdir)
}

// plantLeaf writes a leaf signed by the same CA, with the SANs the issuer would
// choose and an expiry the caller picks. It is how a test reaches the state a
// running box reaches by staying up: the certificate is correct in every way
// except how much life it has left.
//
// The SANs are computed with collectTLSSans rather than written out, because a
// planted certificate whose SANs merely LOOK right would be reissued for the
// mismatch and the test would pass without the expiry rule ever being consulted.
func plantLeaf(t *testing.T, cfg ReconcileDNSConfig, tld string, validFor time.Duration) string {
	t.Helper()

	sans := collectTLSSans(dohIngressHostname(tld), nil, cfg.InternalIP, cfg.InternalIPv6, "")
	var dnsNames []string
	var ips []net.IP
	for _, s := range sans {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
			continue
		}
		dnsNames = append(dnsNames, s)
	}
	slices.Sort(dnsNames)
	slices.SortFunc(ips, func(a, b net.IP) int { return slices.Compare([]byte(a.String()), []byte(b.String())) })

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate planted key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generate planted serial: %v", err)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: dnsNames[0], Organization: []string{"Town OS"}},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(validFor),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, cfg.TLSCA.Cert, &key.PublicKey, cfg.TLSCA.Key)
	if err != nil {
		t.Fatalf("sign planted leaf: %v", err)
	}

	dir := transportLeafDir(cfg.BtrfsBasePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create planted leaf dir: %v", err)
	}
	chain := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cfg.TLSCA.CertPEM...)
	// 0o644 matches the mode the issuer writes, so the planted leaf is
	// indistinguishable from a real one. (No //nolint needed: gosec's G306 is
	// not applied to test files.)
	if err := os.WriteFile(filepath.Join(dir, townostls.LeafCertFileName), chain, 0o644); err != nil {
		t.Fatalf("write planted leaf: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal planted key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(filepath.Join(dir, townostls.LeafKeyFileName), keyPEM, 0o600); err != nil {
		t.Fatalf("write planted key: %v", err)
	}

	value, err := tlsaValue(filepath.Join(dir, townostls.LeafCertFileName))
	if err != nil {
		t.Fatalf("pin the planted leaf: %v", err)
	}
	return value
}

// publishPins puts an association at both encrypted-DNS endpoints, standing in
// for the boot-time publish these tests start after.
func publishPins(t *testing.T, mock *rolodex.MockClient, tld, value string) {
	t.Helper()
	name := dohRecordName(tld)
	if err := rolodex.RegisterPackageTLSA(context.Background(), mock, []rolodex.TLSAEntry{
		{Name: name, Port: DoTPort, Proto: "tcp", Value: value},
		{Name: name, Port: DoQPort, Proto: "udp", Value: value},
	}); err != nil {
		t.Fatalf("seed pins: %v", err)
	}
}

// pinsAt returns the TLSA associations published at both encrypted-DNS
// endpoints, keyed by owner.
func pinsAt(records []*upstream.DnsRecord) map[string][]string {
	out := map[string][]string{}
	for _, owner := range []string{"_853._tcp.dns.lan.", "_853._udp.dns.lan."} {
		out[owner] = recordValues(records, owner, upstream.RecordTypeTLSA)
	}
	return out
}

// TestHourlyReconcileRenewsTheExpiringTransportLeaf is the regression test for
// encrypted DNS going down on a box that changed nothing.
//
// `IssueLeaf` reissues a certificate inside 30 days of expiry, but only when
// something calls it — and until this the only callers were boot and a confirmed
// internal-IP change. Renewal was a side effect of rebooting. This asserts the
// hourly drift pass is now a caller, which is the only one that runs while the
// box is merely up.
func TestHourlyReconcileRenewsTheExpiringTransportLeaf(t *testing.T) {
	t.Parallel()

	mock, cfg, btrfsBase := transportRenewalFixture(t)
	oldPin := plantLeaf(t, cfg, "lan", 10*24*time.Hour)
	publishPins(t, mock, "lan", oldPin)

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	// The certificate is new. Ten days of remaining life is inside the reissue
	// window, so a pass that renewed nothing leaves this at ten days.
	leaf := readLeaf(t, transportLeafDir(btrfsBase))
	if remaining := time.Until(leaf.NotAfter); remaining < 365*24*time.Hour {
		t.Errorf("leaf expires in %s; the expiring certificate was not reissued", remaining)
	}

	// And the zone pins the certificate that is now on disk, at BOTH transports.
	// A renewal that forgets the zone is worse than no renewal: a DANE client
	// that finds a record and no match refuses the connection outright.
	newPin, err := tlsaValue(filepath.Join(transportLeafDir(btrfsBase), townostls.LeafCertFileName))
	if err != nil {
		t.Fatalf("pin the renewed leaf: %v", err)
	}
	if newPin == oldPin {
		t.Fatal("the renewed leaf carries the old key; the pin comparison below proves nothing")
	}
	for owner, values := range pinsAt(mock.Records) {
		if !slices.Contains(values, newPin) {
			t.Errorf("%s pins %v, want the renewed certificate", owner, values)
		}
		if slices.Contains(values, oldPin) {
			t.Errorf("%s still pins the retired certificate: %v", owner, values)
		}
	}
}

// The steady state has to be free, or an hourly pass that rewrites a certificate
// every hour hands every DoT client a different one twice a day — and re-pins it
// in the zone each time, which is the churn DANE is least able to absorb.
func TestHourlyReconcileLeavesACurrentTransportLeafAlone(t *testing.T) {
	t.Parallel()

	mock, cfg, btrfsBase := transportRenewalFixture(t)
	pin := plantLeaf(t, cfg, "lan", 5*365*24*time.Hour)
	publishPins(t, mock, "lan", pin)

	before := readLeaf(t, transportLeafDir(btrfsBase))
	mock.Calls = nil
	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if after := readLeaf(t, transportLeafDir(btrfsBase)); after.SerialNumber.Cmp(before.SerialNumber) != 0 {
		t.Error("a certificate with five years left was reissued; the pass is not idempotent")
	}
	for _, c := range mock.GetCalls() {
		if c.Method != "AddRecord" && c.Method != "RemoveRecord" {
			continue
		}
		name, ok := c.Args[0].(string)
		if !ok {
			continue
		}
		if _, ours := pinsAt(nil)[name]; ours {
			t.Errorf("%s touched %s with nothing to renew", c.Method, name)
		}
	}
	for owner, values := range pinsAt(mock.Records) {
		if len(values) != 1 || values[0] != pin {
			t.Errorf("%s pins %v, want exactly the one unchanged association", owner, values)
		}
	}
}

// A pin the zone lost — rolodex re-initialized, a teardown that took the whole
// zone with it — is republished without waiting for the certificate to need
// renewing. The endpoint is serving a certificate the zone does not vouch for,
// which for a DANE-checking client is the same outage as a wrong pin.
func TestHourlyReconcileRepublishesAMissingTransportPin(t *testing.T) {
	t.Parallel()

	mock, cfg, btrfsBase := transportRenewalFixture(t)
	pin := plantLeaf(t, cfg, "lan", 5*365*24*time.Hour)

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS: %v", err)
	}

	if after := readLeaf(t, transportLeafDir(btrfsBase)); after == nil {
		t.Fatal("no leaf after the pass")
	}
	for owner, values := range pinsAt(mock.Records) {
		if !slices.Contains(values, pin) {
			t.Errorf("%s pins %v, want the certificate the endpoint is serving", owner, values)
		}
	}
}

// Without a CA or a btrfs base there is nothing to renew and nothing to pin, and
// the pass must still reconcile every other record rather than failing. This is
// the shape every caller that does not carry TLS state takes.
func TestTransportRenewalIsSkippedWithoutACA(t *testing.T) {
	t.Parallel()

	mock, cfg, _ := transportRenewalFixture(t)
	cfg.TLSCA = nil

	if err := ReconcileDNS(context.Background(), cfg); err != nil {
		t.Fatalf("ReconcileDNS without a CA: %v", err)
	}
	for owner, values := range pinsAt(mock.Records) {
		if len(values) != 0 {
			t.Errorf("%s pins %v with no CA to issue a certificate", owner, values)
		}
	}
}
