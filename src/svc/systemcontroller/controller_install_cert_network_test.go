// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// certNetworkHandler wires a handler with a local CA, btrfs/state dirs, a
// network manager, and (optionally) a rolodex mock, for exercising the
// package certificate + DANE provisioning paths.
func certNetworkHandler(t *testing.T, nm account.NetworkManager, rc rolodex.Client) (*SystemControllerHandlers, string) {
	t.Helper()
	dir := t.TempDir()
	btrfs := filepath.Join(dir, "btrfs")
	stateDir := filepath.Join(dir, "state")
	for _, d := range []string{btrfs, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	ca, err := townostls.EnsureCA(filepath.Join(btrfs, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	sb := &serverBase{ServerConfig: ServerConfig{
		NetworkMgr:       nm,
		RolodexClient:    rc,
		TLSCA:            ca,
		BtrfsBasePath:    btrfs,
		NetworkStatePath: stateDir,
	}}
	sb.internalIP.Store("192.168.1.10")
	return &SystemControllerHandlers{Controller: sb, ctx: context.Background()}, btrfs
}

func httpPackage() *packages.Package {
	return &packages.Package{
		Network: packages.PackageNetwork{External: packages.PortMap{8080: 80}},
	}
}

// The issued leaf certificate's SAN must carry the install network's TLD, so a
// package on the "fart" network gets a cert for gitea.<repo>.fart — not the
// global home zone.
func TestPackageLeafSANFollowsNetworkTLD(t *testing.T) {
	nm := seedNetwork(t)
	s, btrfs := certNetworkHandler(t, nm, nil)

	if err := s.writePackageNetworkState("repo-a", "gitea", "2.0", "fart", httpPackage(), []string{"http"}); err != nil {
		t.Fatalf("writePackageNetworkState: %v", err)
	}

	dnsNames := leafDNSNames(t, filepath.Join(btrfs, "tls", "leaves", "repo-a", "gitea", "2.0", "cert.pem"))
	if !slices.Contains(dnsNames, "gitea.repo-a.fart") {
		t.Fatalf("leaf SAN must follow the network TLD; want gitea.repo-a.fart in %v", dnsNames)
	}
	if slices.Contains(dnsNames, "gitea.repo-a.home") {
		t.Fatalf("leaf SAN must not use the home zone for a fart-network package: %v", dnsNames)
	}
}

// The default network keeps issuing certs in the global home zone.
func TestPackageLeafSANDefaultNetworkUsesHome(t *testing.T) {
	nm := account.InitMockNetworkManager()
	s, btrfs := certNetworkHandler(t, nm, nil)

	if err := s.writePackageNetworkState("repo-a", "nginx", "1.0", account.DefaultNetworkName, httpPackage(), []string{"http"}); err != nil {
		t.Fatalf("writePackageNetworkState: %v", err)
	}

	dnsNames := leafDNSNames(t, filepath.Join(btrfs, "tls", "leaves", "repo-a", "nginx", "1.0", "cert.pem"))
	if !slices.Contains(dnsNames, "nginx.repo-a.home") {
		t.Fatalf("default-network leaf SAN should be nginx.repo-a.home, got %v", dnsNames)
	}
}

// The DANE TLSA record for a non-default-network package must be published
// scoped to that network, under the network TLD — never as a global home-zone
// record (which would be hidden by the owned-TLD partition).
func TestPublishPackageTLSAScopedUnderNetworkTLD(t *testing.T) {
	nm := seedNetwork(t)
	mc := &rolodex.MockClient{}
	s, _ := certNetworkHandler(t, nm, mc)

	// Provision the state file + leaf so buildTLSAEntries has something to pin.
	if err := s.writePackageNetworkState("repo-a", "gitea", "2.0", "fart", httpPackage(), []string{"http"}); err != nil {
		t.Fatalf("writePackageNetworkState: %v", err)
	}

	s.publishPackageTLSA(context.Background(), "repo-a", "gitea", "2.0", "fart", nil)

	scoped := mc.ScopedRecords["fart"]
	if !hasTLSAUnder(scoped, "gitea.repo-a.fart.") {
		t.Fatalf("expected a scoped TLSA under gitea.repo-a.fart. in scope fart, got %v", recordNames(scoped))
	}
	if len(mc.Records) != 0 {
		t.Fatalf("non-default network must not publish a global TLSA, got %v", globalNames(mc))
	}
}

// The default network keeps publishing TLSA globally in the home zone.
func TestPublishPackageTLSADefaultNetworkGlobalHome(t *testing.T) {
	nm := account.InitMockNetworkManager()
	mc := &rolodex.MockClient{}
	s, _ := certNetworkHandler(t, nm, mc)

	if err := s.writePackageNetworkState("repo-a", "nginx", "1.0", account.DefaultNetworkName, httpPackage(), []string{"http"}); err != nil {
		t.Fatalf("writePackageNetworkState: %v", err)
	}

	s.publishPackageTLSA(context.Background(), "repo-a", "nginx", "1.0", account.DefaultNetworkName, nil)

	if !hasTLSAUnder(mc.Records, "nginx.repo-a.home.") {
		t.Fatalf("expected a global TLSA under nginx.repo-a.home., got %v", globalNames(mc))
	}
	if len(mc.ScopedRecords) != 0 {
		t.Fatalf("default network must not publish scoped TLSA, got %+v", mc.ScopedRecords)
	}
}

// networkTLD maps a package's install network to the TLD its cert/DANE records
// must use.
func TestNetworkTLDResolvesInstallNetwork(t *testing.T) {
	nm := seedNetwork(t)
	s := dnsNetworkHandler(nm, nil)

	if got := s.networkTLD("fart"); got != "fart" {
		t.Errorf("networkTLD(fart) = %q, want fart", got)
	}
	if got := s.networkTLD(account.DefaultNetworkName); got != "home" {
		t.Errorf("networkTLD(home) = %q, want home", got)
	}
	if got := s.networkTLD(""); got != "home" {
		t.Errorf("networkTLD(\"\") = %q, want home", got)
	}
	// Unknown networks fall back to the global default rather than inventing a TLD.
	if got := s.networkTLD("nope"); got != "home" {
		t.Errorf("networkTLD(nope) = %q, want home", got)
	}
}

func leafDNSNames(t *testing.T, certPath string) []string {
	t.Helper()
	data, err := os.ReadFile(certPath) //nolint:gosec // G304 -- test temp path
	if err != nil {
		t.Fatalf("read leaf cert: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("leaf cert PEM decode failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	return cert.DNSNames
}

func hasTLSAUnder(recs []*upstream.DnsRecord, baseSuffix string) bool {
	for _, r := range recs {
		if r.RecordType == upstream.RecordTypeTLSA &&
			strings.Contains(r.Name, "._tcp.") &&
			strings.HasSuffix(r.Name, baseSuffix) {
			return true
		}
	}
	return false
}
