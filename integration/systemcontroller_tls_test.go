// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// initSystemControllerTLSTest stands up a test server with a btrfs base
// path, a network state dir, and a freshly-generated TLS CA so the
// install pipeline can issue per-package leaf certs end-to-end. The
// shared test-packages-core/nginx fixture declares supplies: ["http"]
// so installing it activates the TLS wiring without any bespoke package.
func initSystemControllerTLSTest(t *testing.T) (
	c *systemcontroller.SystemdClient,
	btrfsBase string,
	netStateDir string,
	ca *townostls.CA,
) {
	t.Helper()
	dir := t.TempDir()

	reposFile := filepath.Join(dir, packages.RepositoriesFile)
	if err := os.WriteFile(reposFile, []byte("[]"), 0o600); err != nil {
		t.Fatalf("seed repositories file: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}

	btrfsBase = filepath.Join(dir, "btrfs")
	netStateDir = filepath.Join(dir, "network-state")
	for _, d := range []string{btrfsBase, netStateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	ca, err = townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	sd := systemd.InitMockManager()

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:                mock,
		RepositoryRoot:         rr,
		Installer:              inst,
		Systemd:                sd,
		NetworkControllerImage: ncTestImage(),
		NetworkStatePath:       netStateDir,
		BtrfsBasePath:          btrfsBase,
		TLSCA:                  ca,
	})
	t.Cleanup(func() { ts.Server.Close() })

	client, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return client, btrfsBase, netStateDir, ca
}

// TestInstallNginxEmitsTLSStateAndLeaf verifies that installing an
// HTTP-supplying package produces:
//
//   1. A network state file whose port(s) are marked TLS=true with a
//      CertPath that points at the NC-internal mount path.
//   2. A leaf cert on disk under <btrfs>/tls/leaves/... that chains to
//      the local CA and includes the expected SANs (PACKAGE_DNS, extra
//      domains, localhost, 127.0.0.1).
//
// This is the primary smoke test for the NC-TLS pipeline — individual
// pieces have unit tests; this one proves they compose correctly during a
// real package install through the HTTP API.
func TestInstallNginxEmitsTLSStateAndLeaf(t *testing.T) {
	t.Parallel()
	c, btrfsBase, netStateDir, ca := initSystemControllerTLSTest(t)

	if err := addRepoWithCreds(c, "core", testCoreURLString()); err != nil {
		t.Fatalf("AddRepository core: %v", err)
	}
	if err := c.InstallPackage(context.TODO(), "nginx", "1.0", packages.Responses{
		"hostname": "example",
		"port":     "8080",
	}, false, "", false); err != nil {
		t.Fatalf("InstallPackage nginx@1.0: %v", err)
	}

	// --- Step 1: state file has TLS=true + container cert path ---
	stateFile := filepath.Join(netStateDir, "core-nginx-1.0.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var state networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(state.Ports))
	}
	port := state.Ports[0]
	if !port.TLS {
		t.Fatal("nginx port should be TLS=true")
	}
	wantCert := "/etc/town-os/tls/leaves/core/nginx/1.0"
	if port.CertPath != wantCert {
		t.Errorf("cert path = %q, want %q", port.CertPath, wantCert)
	}

	// --- Step 2: leaf files on disk, signed by the CA, correct SANs ---
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
	if _, err := os.Stat(filepath.Join(leafDir, "key.pem")); err != nil {
		t.Errorf("leaf key missing: %v", err)
	}

	// Chain verifies against the CA.
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "nginx.core.home",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("leaf chain verify: %v", err)
	}

	// SANs cover PACKAGE_DNS + localhost + 127.0.0.1.
	dnsSet := map[string]bool{}
	for _, n := range leaf.DNSNames {
		dnsSet[n] = true
	}
	if !dnsSet["nginx.core.home"] {
		t.Errorf("missing PACKAGE_DNS in SAN list: %v", leaf.DNSNames)
	}
	if !dnsSet["localhost"] {
		t.Errorf("missing localhost in SAN list: %v", leaf.DNSNames)
	}
	ipFound := false
	for _, ip := range leaf.IPAddresses {
		if ip.String() == "127.0.0.1" {
			ipFound = true
			break
		}
	}
	if !ipFound {
		t.Errorf("missing 127.0.0.1 in IP SANs: %v", leaf.IPAddresses)
	}
}
