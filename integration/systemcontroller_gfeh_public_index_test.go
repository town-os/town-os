// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/gfeh"
	"gitea.com/town-os/town-os/src/ingress"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// The published-files index, end to end.
//
// http.gfeh.<tld> belongs to gfehd, which answers /f/<token> and 404s its own
// root — so the one name on the box whose purpose is handing files to people
// tells anyone who types it that there is nothing there. The fix is a page Town
// OS renders and the pages container serves, at that name's root, while every
// other path still reaches gfehd.
//
// That makes it the only name on the box with two backends, and the pieces are
// derived by different code in different files: the route and its path split
// (ingress_program.go), the rendered bytes (src/gfeh/public_index.go), the
// directory the static server roots on and its webroot entry (gfeh_index.go),
// and the mount that makes the entry resolve (pages_service.go). A disagreement
// in any one of them produces the same symptom the feature exists to remove — a
// name that resolves, presents a valid certificate, and 404s — so this asserts
// them together.
func TestIntegrationGfehPublishedIndexIsRoutedAndServed(t *testing.T) {
	t.Parallel()

	// Short by necessity: the fake daemon binds <btrfsBase>/gfeh-control/home/
	// run/admin.sock, and t.TempDir() spends this test's whole name on the path
	// before that suffix is added.
	dir := gfehTempDir(t)
	btrfsBase := filepath.Join(dir, "btrfs")
	if err := os.MkdirAll(btrfsBase, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	socket := gfeh.SocketPath(btrfsBase, "home")
	fake := startFakeGfehd(t, socket, browsableNames("home", ""))
	quarterly := "q3-report.pdf"
	photo := "holiday.jpg"
	fake.Publish(
		gfeh.Exposure{Token: "abc123", Path: "/reports/q3.pdf", Filename: &quarterly, Enabled: true},
		gfeh.Exposure{Token: "def456", Path: "/photos/holiday.jpg", Filename: &photo, Enabled: true},
		// Withdrawn: gfehd does not serve it, so a row would be a link that 404s.
		gfeh.Exposure{Token: "gone789", Path: "/secret.txt", Enabled: false},
	)

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

	// --- 1. One name, two backends ---
	var route *ingresspb.Route
	for _, r := range ingMock.Routes {
		if r.GetHostname() == "http.gfeh.home" {
			route = r
			break
		}
	}
	if route == nil {
		t.Fatalf("no ingress route for http.gfeh.home")
	}
	// The route's own backend stays gfehd's: every path but the root is a
	// published link, and taking those would be the failure this page fixes,
	// inverted.
	if !strings.HasPrefix(route.GetBackend(), "town-os-system--gfeh-") {
		t.Errorf("route backend = %q, want the gfeh container", route.GetBackend())
	}
	pbs := route.GetPathBackends()
	if len(pbs) != 1 {
		t.Fatalf("got %d path backends, want 1: %+v", len(pbs), pbs)
	}
	if pbs[0].GetPath() != "/" {
		t.Errorf("path backend matches %q, want the root exactly", pbs[0].GetPath())
	}
	if !strings.HasPrefix(pbs[0].GetBackend(), "town-os-system--pages:") {
		t.Errorf("path backend = %q, want the pages container", pbs[0].GetBackend())
	}

	// --- 2. The certificate ---
	// The name was already in the partition's leaf as an HTTP view; serving a
	// second thing at its root must not have changed that.
	leafDir := filepath.Join(btrfsBase, systemcontroller.TLSSubvolume, "leaves", "gfeh", "home", "current")
	certPEM, err := os.ReadFile(filepath.Join(leafDir, "cert.pem"))
	if err != nil {
		t.Fatalf("no leaf issued for the partition: %v", err)
	}
	if !certCoversName(t, certPEM, "http.gfeh.home") {
		t.Error("the partition's leaf does not cover http.gfeh.home")
	}

	// --- 3. The content ---
	contentDir := filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName, "http.gfeh.home")
	raw, err := os.ReadFile(filepath.Join(contentDir, "index.html"))
	if err != nil {
		t.Fatalf("the published-files index was never rendered: %v", err)
	}
	page := string(raw)

	for _, want := range []string{quarterly, photo, `href="/f/abc123"`, `href="/f/def456"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the index is missing %q:\n%s", want, page)
		}
	}
	// A withdrawn link resolves to nothing, so listing it would advertise a 404.
	for _, gone := range []string{"gone789", "secret.txt"} {
		if strings.Contains(page, gone) {
			t.Errorf("the index lists the withdrawn %q:\n%s", gone, page)
		}
	}
	// The container-side backend port is reachable from nowhere a reader sits.
	if strings.Contains(page, ":9001") {
		t.Errorf("the index publishes the container-side port:\n%s", page)
	}

	// --- 4. The path the static server resolves, and the mount behind it ---
	link := filepath.Join(btrfsBase, systemcontroller.PagesWebrootDir, "http.gfeh.home")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("no webroot entry for the published-files index: %v", err)
	}
	if want := systemcontroller.GfehIndexContainerDir + "/http.gfeh.home"; target != want {
		t.Errorf("webroot target = %q, want %q", target, want)
	}

	sd := systemd.InitMockManager()
	if err := systemcontroller.StartPagesService(ctx, sd, btrfsBase, "docker.io/library/caddy:latest"); err != nil {
		t.Fatalf("StartPagesService: %v", err)
	}
	unit := pagesUnitContent(t, sd)
	mount := filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName) + ":" + systemcontroller.GfehIndexContainerDir
	if !strings.Contains(unit, mount) {
		t.Errorf("the pages unit does not mount the index root (%s):\n%s", mount, unit)
	}

	// --- 5. The partition index is still the partition index ---
	// Two generated pages now live under one root and share a webroot with
	// pages. The prune that keeps that root tidy must not treat either as the
	// other's leftovers.
	if _, err := os.Stat(filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName, "gfeh.home", "index.html")); err != nil {
		t.Errorf("the partition index was pruned by the published-files pass: %v", err)
	}
	partition, err := os.ReadFile(filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName, "gfeh.home", "index.html"))
	if err != nil {
		t.Fatalf("read partition index: %v", err)
	}
	// The partition index is a directory of endpoints and stays one: it is the
	// page an operator hands round to explain what object storage is, and a
	// /f/<token> on it is a bearer credential nobody asked to publish there.
	for _, token := range []string{"abc123", "def456"} {
		if strings.Contains(string(partition), token) {
			t.Errorf("the partition index lists the published link %q", token)
		}
	}
}

// A second reconcile with nothing changed must not rewrite either page: the
// ingress supervisor no-ops a reload whose content is identical, and the same
// reasoning applies to the storage the box exists to look after.
func TestIntegrationGfehPublishedIndexIsStableAcrossReconciles(t *testing.T) {
	t.Parallel()

	// Short by necessity: the fake daemon binds <btrfsBase>/gfeh-control/home/
	// run/admin.sock, and t.TempDir() spends this test's whole name on the path
	// before that suffix is added.
	dir := gfehTempDir(t)
	btrfsBase := filepath.Join(dir, "btrfs")
	if err := os.MkdirAll(btrfsBase, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	socket := gfeh.SocketPath(btrfsBase, "home")
	fake := startFakeGfehd(t, socket, browsableNames("home", ""))
	name := "q3.pdf"
	fake.Publish(gfeh.Exposure{Token: "abc123", Path: "/q3.pdf", Filename: &name, Enabled: true})

	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	reg := gfehTestRegistry{clients: map[string]gfeh.Client{"home": gfeh.NewClient(socket)}}
	ctx := context.Background()
	stateDir := filepath.Join(dir, "state")

	rebuild := func() {
		t.Helper()
		if err := systemcontroller.RebuildIngress(ctx, &ingress.MockClient{}, nil, nil, nil, reg, ca,
			btrfsBase, stateDir, "home", "192.168.10.50"); err != nil {
			t.Fatalf("RebuildIngress: %v", err)
		}
	}

	rebuild()
	path := filepath.Join(btrfsBase, systemcontroller.GfehIndexDirName, "http.gfeh.home", "index.html")
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}

	rebuild()
	second, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat index after second pass: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read index after second pass: %v", err)
	}

	if string(before) != string(after) {
		t.Errorf("an unchanged partition rendered a different page:\n%s\n---\n%s", string(before), string(after))
	}
	if !first.ModTime().Equal(second.ModTime()) {
		t.Error("an unchanged page was rewritten, so every reconcile touches the storage")
	}
}
