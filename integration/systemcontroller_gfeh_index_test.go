// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

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
	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/ingress"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// The partition index, end to end.
//
// Every view gfeh serves is a protocol endpoint and none of them is browsable:
// the HTTP view has exactly one route (/f/{token}), so its root is a 404, and
// S3, Drive and IPFS answer their own APIs to anything else. An operator who
// opens a partition's name in a browser is therefore told the thing is broken.
//
// The index is the page that fixes that, and it only works if four separate
// things agree on one string: the ingress vhost, the leaf's SAN set, the DNS
// record, and the directory the static server roots on. Each is derived by
// different code, and a mismatch in any one of them produces the same symptom —
// a name that resolves and then 404s — so this test asserts all four together.

// browsableNames is a partition serving the four HTTP views plus SMB, which is
// what a real gfehd renders from the config gfehctl writes.
func browsableNames(partition, network string) gfeh.NameList {
	list := gfeh.NameList{
		Partition: partition,
		Names: []gfeh.Name{
			{Hostname: "s3.gfeh", View: gfeh.ViewS3, Port: gfeh.PortS3},
			{Hostname: "http.gfeh", View: gfeh.ViewHTTP, Port: gfeh.PortHTTP},
			{Hostname: "drive.gfeh", View: gfeh.ViewDrive, Port: gfeh.PortDrive},
			{Hostname: "ipfs.gfeh", View: gfeh.ViewIPFS, Port: gfeh.PortIPFS},
			{Hostname: "smb.gfeh", View: gfeh.ViewSMB, Port: 4450},
		},
	}
	if network != "" {
		n := network
		list.Network = &n
	}
	return list
}

func TestIntegrationGfehIndexIsRoutedAndServed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	if err := os.MkdirAll(btrfsBase, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	socket := gfeh.SocketPath(btrfsBase, "home")
	startFakeGfehd(t, socket, browsableNames("home", ""))

	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	reg := gfehTestRegistry{clients: map[string]gfeh.Client{"home": gfeh.NewClient(socket)}}
	ingMock := &ingress.MockClient{}
	ctx := context.Background()

	if err := systemcontroller.RebuildIngress(ctx, ingMock, nil, nil, nil, reg, ca,
		btrfsBase, filepath.Join(dir, "state"), "home", "192.168.10.50"); err != nil {
		t.Fatalf("RebuildIngress: %v", err)
	}

	// --- 1. The vhost ---
	routes := map[string]string{}
	for _, r := range ingMock.Routes {
		routes[r.GetHostname()] = r.GetBackend()
	}
	backend, ok := routes["gfeh.home"]
	if !ok {
		t.Fatalf("no ingress route for the index name gfeh.home; have %v", routes)
	}
	// The index is static HTML Town OS generates, not something gfehd serves.
	// A vhost pointing at the gfeh container would be a route to a daemon with
	// no handler for it — a 404 behind a valid certificate.
	if !strings.HasPrefix(backend, "town-os-system--pages:") {
		t.Errorf("index backend = %q, want the pages container", backend)
	}

	// --- 2. The certificate ---
	// Served over HTTPS from the same :443 listener under the partition's one
	// leaf, so a SAN set that omitted it would make the single browsable name
	// the only one a browser refuses.
	leafDir := filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, "leaves", "gfeh", "home", "current")
	certPEM, err := os.ReadFile(filepath.Join(leafDir, "cert.pem"))
	if err != nil {
		t.Fatalf("no leaf issued for the partition: %v", err)
	}
	if !certCoversName(t, certPEM, "gfeh.home") {
		t.Error("the partition's leaf does not cover the index name gfeh.home")
	}

	// --- 3. The content, and the path the static server resolves ---
	contentDir := filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName, "gfeh.home")
	raw, err := os.ReadFile(filepath.Join(contentDir, "index.html"))
	if err != nil {
		t.Fatalf("the index page was never rendered: %v", err)
	}
	page := string(raw)
	for _, fqdn := range []string{"s3.gfeh.home", "http.gfeh.home", "drive.gfeh.home", "ipfs.gfeh.home"} {
		if !strings.Contains(page, fqdn) {
			t.Errorf("the index does not list %s", fqdn)
		}
	}
	// SMB is not served and gets no route; pointing a reader at it would send
	// them at an address that accepts a connection and then does nothing.
	if strings.Contains(page, "smb.gfeh.home") {
		t.Error("the index lists the SMB view, which nothing serves")
	}
	// The ports gfeh reports for the HTTP views are container-side backend
	// ports the ingress proxies to. A URL carrying one is refused by every
	// client that tries it.
	for _, port := range []string{":9000", ":9001", ":9002", ":9003"} {
		if strings.Contains(page, port) {
			t.Errorf("the index publishes the container-side port %s", port)
		}
	}

	// The pages Caddyfile is `root * /srv/{host}`, so the webroot entry named
	// for the FQDN is what turns a request into a directory.
	link := filepath.Join(btrfsBase, systemcontroller.PagesWebrootDir, "gfeh.home")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("no webroot entry for the index name: %v", err)
	}

	// --- 4. The mount, which is what makes that target resolve ---
	//
	// The symlink target is container-absolute, so it means nothing unless the
	// pages unit actually binds the index root there. These are written by
	// different functions in different files; when they disagree the name
	// resolves, the certificate validates, and every request 404s.
	sd := systemd.InitMockManager()
	if err := systemcontroller.StartPagesService(ctx, sd, btrfsBase, "docker.io/library/caddy:latest"); err != nil {
		t.Fatalf("StartPagesService: %v", err)
	}
	unit := pagesUnitContent(t, sd)
	mount := filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName) + ":" + systemcontroller.GfehIndexContainerDir
	if !strings.Contains(unit, mount) {
		t.Errorf("the pages unit does not mount the index root (%s):\n%s", mount, unit)
	}
	if !strings.HasPrefix(target, systemcontroller.GfehIndexContainerDir+"/") {
		t.Errorf("webroot target %q is outside the mounted index root %q", target, systemcontroller.GfehIndexContainerDir)
	}
	// And the host side of that mount is where the content was actually written.
	if _, err := os.Stat(contentDir); err != nil {
		t.Errorf("the mounted index root does not contain the rendered page: %v", err)
	}

	// --- 5. DNS ---
	// A name with a route, a certificate and content still resolves to nothing
	// without a record.
	rolMock := &rolodex.MockClient{}
	settings := &mockSettingsManager{values: map[string]string{"dns_tld": "home"}}
	if err := systemcontroller.RebuildDNS(ctx, systemcontroller.ReconcileDNSConfig{
		Client:        rolMock,
		SettingsMgr:   settings,
		InternalIP:    "192.168.10.50",
		BtrfsBasePath: btrfsBase,
		Gfeh:          reg,
	}); err != nil {
		t.Fatalf("RebuildDNS: %v", err)
	}
	if !hasRolodexRecord(rolMock, upstream.RecordTypeA, "gfeh.home.") {
		t.Errorf("no A record for the index name; have %v", rolodexRecordNames(rolMock, upstream.RecordTypeA))
	}
}

// certCoversName reports whether a PEM-encoded leaf carries a name in its SANs.
//
// Parsed rather than string-matched: a DNS name appearing anywhere in the DER
// is not the same as it being a SAN a TLS client will accept, and the whole
// point of the assertion is what the client does.
func certCoversName(t *testing.T, pemBytes []byte, name string) bool {
	t.Helper()

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("leaf is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	return slices.Contains(cert.DNSNames, name)
}

// pagesUnitContent pulls the pages system-service unit out of the mock.
func pagesUnitContent(t *testing.T, sd *systemd.MockManager) string {
	t.Helper()
	for name, content := range sd.InstalledUnits {
		if strings.Contains(name, systemcontroller.PagesServiceKey) {
			return content
		}
	}
	t.Fatalf("no pages unit was installed; have %v", sd.InstalledUnits)
	return ""
}
