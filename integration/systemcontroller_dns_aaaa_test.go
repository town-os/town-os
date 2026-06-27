// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// aaaaHarness carries the moving parts an AAAA-publishing test needs. Unlike
// dnsLifecycleHarness it also exposes the *TestServer so the test can pin the
// host's internal IPv4/IPv6 (SetInternalIP / SetInternalIPv6) before driving
// the install/SetupDNS API path that reads GetInternalIPv6().
type aaaaHarness struct {
	ts     *systemcontroller.TestServer
	client *systemcontroller.SystemdClient
	rolo   *rolodex.MockClient
}

// initAAAATest wires a full install-capable systemcontroller with a file://
// repository and a MockClient rolodex, mirroring initDNSLifecycleTest but
// returning the TestServer so internal IPs can be pinned.
func initAAAATest(t *testing.T) *aaaaHarness {
	t.Helper()
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	pkgYAML := `image: nginx:1.0
description: "test pkg"
network:
  external:
    "@port@": "80"
volumes: {}
questions:
  port:
    query: "port?"
    type: port
    default: "8080"
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, "dnspkg")
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0600); err != nil {
		t.Fatalf("WriteFile pkg: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()
	rc := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		RolodexClient:  rc,
		SettingsMgr:    settings,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	return &aaaaHarness{ts: ts, client: c, rolo: rc}
}

// countRecordsFor returns how many records of the given type exist for fqdn.
func countRecordsFor(t *testing.T, rc *rolodex.MockClient, fqdn string, rt upstream.RecordType) int {
	t.Helper()
	records, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	n := 0
	for _, r := range records {
		if r.Name == fqdn && r.RecordType == rt {
			n++
		}
	}
	return n
}

// aaaaValueFor returns the value of the single AAAA record for fqdn, failing
// the test if there is not exactly one.
func aaaaValueFor(t *testing.T, rc *rolodex.MockClient, fqdn string) string {
	t.Helper()
	records, err := rc.ListRecords(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	var vals []string
	for _, r := range records {
		if r.Name == fqdn && r.RecordType == upstream.RecordTypeAAAA {
			vals = append(vals, r.Value)
		}
	}
	if len(vals) != 1 {
		t.Fatalf("expected exactly 1 AAAA record for %s, got %d (%v)", fqdn, len(vals), vals)
	}
	return vals[0]
}

// TestIntegrationInstallRegistersAAAA pins the per-install AAAA contract:
// when the host has a global IPv6, installing a package through the API
// registers exactly one AAAA record (alongside the A record) pointing at the
// host's pinned IPv6. This exercises the full HTTP install path
// (registerPackageDNS -> GetInternalIPv6), not just the standalone ReconcileDNS
// helper that the unit tests cover.
func TestIntegrationInstallRegistersAAAA(t *testing.T) {
	t.Parallel()
	h := initAAAATest(t)
	h.ts.SetInternalIP("10.0.0.5")
	h.ts.SetInternalIPv6("2001:db8::50")

	pkgFQDN := "dnspkg.local.home."
	if err := h.client.InstallPackage(context.TODO(), "dnspkg", "1.0", packages.Responses{"port": "9191"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if got := countRecordsFor(t, h.rolo, pkgFQDN, upstream.RecordTypeA); got != 1 {
		t.Fatalf("after install: %d A records for %s, want 1", got, pkgFQDN)
	}
	if v := aaaaValueFor(t, h.rolo, pkgFQDN); v != "2001:db8::50" {
		t.Fatalf("AAAA value = %q, want 2001:db8::50", v)
	}
}

// TestIntegrationInstallNoAAAAWhenV4Only pins the back-compat contract: a
// v4-only host (GetInternalIPv6 == "") registers the A record but NO AAAA
// record, so existing IPv4 installs are unchanged by the feature. Pinning the
// IPv6 cache to "" deterministically models a host with no global IPv6,
// independent of the test machine's actual interfaces.
func TestIntegrationInstallNoAAAAWhenV4Only(t *testing.T) {
	t.Parallel()
	h := initAAAATest(t)
	h.ts.SetInternalIP("10.0.0.6")
	h.ts.SetInternalIPv6("")

	pkgFQDN := "dnspkg.local.home."
	if err := h.client.InstallPackage(context.TODO(), "dnspkg", "1.0", packages.Responses{"port": "9192"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	if got := countRecordsFor(t, h.rolo, pkgFQDN, upstream.RecordTypeA); got != 1 {
		t.Fatalf("v4-only install: %d A records for %s, want 1", got, pkgFQDN)
	}
	if got := countRecordsFor(t, h.rolo, pkgFQDN, upstream.RecordTypeAAAA); got != 0 {
		t.Fatalf("v4-only install: %d AAAA records for %s, want 0", got, pkgFQDN)
	}
}

// TestIntegrationSetupDNSPublishesAAAA verifies the zone-bootstrap API
// (SetupDNS) publishes an AAAA record for the nameserver (ns1) when the host
// has a global IPv6, in addition to the existing A record.
func TestIntegrationSetupDNSPublishesAAAA(t *testing.T) {
	t.Parallel()
	h := initAAAATest(t)
	h.ts.SetInternalIP("10.0.0.7")
	h.ts.SetInternalIPv6("2001:db8::7")

	if err := h.client.SetupDNS(context.TODO()); err != nil {
		t.Fatalf("SetupDNS: %v", err)
	}

	if got := countRecordsFor(t, h.rolo, "ns1.home.", upstream.RecordTypeA); got != 1 {
		t.Fatalf("SetupDNS: %d A records for ns1.home., want 1", got)
	}
	if v := aaaaValueFor(t, h.rolo, "ns1.home."); v != "2001:db8::7" {
		t.Fatalf("ns1 AAAA value = %q, want 2001:db8::7", v)
	}
}

// TestIntegrationLeafCertIncludesGlobalIPv6SAN is the end-to-end TLS check for
// the AAAA-parity SAN: installing an HTTP-supplying package must put the host's
// global IPv6 into the leaf cert's IP SANs so a direct https://[v6-literal]
// dial matches. The IPv6 SAN is discovered from the live host interfaces
// (issueLeafForPackage calls InternalInterfaceIPs directly and is not
// injectable), so this skips on v4-only hosts/containers; the SAN-assembly
// logic itself is pinned by the TestCollectTLSSansIncludesInternalIPv6 unit
// test, which always runs.
func TestIntegrationLeafCertIncludesGlobalIPv6SAN(t *testing.T) {
	t.Parallel()

	_, hostV6 := systemcontroller.InternalInterfaceIPs()
	if hostV6 == "" {
		t.Skip("host has no global IPv6; AAAA-parity SAN is unobservable here")
	}

	c, btrfsBase, _, ca := initSystemControllerTLSTest(t)
	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	leafDir := filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, "leaves", "core", "nginx", "1.0")
	certPEM, err := os.ReadFile(filepath.Join(leafDir, "cert.pem"))
	if err != nil {
		t.Fatalf("read leaf cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatalf("leaf cert is not a PEM block")
		return
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	// Chain still verifies against the CA (sanity: the v6 SAN didn't break it).
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "nginx.core.home",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("leaf chain verify: %v", err)
	}

	want := net.ParseIP(hostV6)
	found := false
	for _, ip := range leaf.IPAddresses {
		if ip.Equal(want) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("leaf IP SANs %v missing host global IPv6 %s", leaf.IPAddresses, hostV6)
	}
}
