// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
)

// errGfehIndexTestDown stands in for a daemon that is not answering.
var errGfehIndexTestDown = errors.New("connection refused")

// homeRegistry is one partition on the default network, serving every view.
func homeRegistry() stubGfehRegistry {
	return stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}}
}

// homeSites is the site set that partition produces.
func homeSites(t *testing.T) []GfehSite {
	t.Helper()
	return collectGfehSites(gfehTLSCtx(t), homeRegistry(), nil, "home", "")
}

// The index content, the webroot symlink and the mount in the pages unit have
// to agree on one path, or the static server roots on a directory that is not
// there and every index name 404s.
func TestGfehIndexPathsAgree(t *testing.T) {
	base := t.TempDir()

	if _, err := writeGfehIndexContent(base, "gfeh.home", gfeh.IndexPage{Network: "home", TLD: "home"}); err != nil {
		t.Fatalf("writeGfehIndexContent: %v", err)
	}

	content := filepath.Join(base, GfehIndexDirName, "gfeh.home", "index.html")
	if _, err := os.Stat(content); err != nil {
		t.Fatalf("index content not written where expected: %v", err)
	}

	link := filepath.Join(base, PagesWebrootDir, "gfeh.home")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("webroot symlink: %v", err)
	}
	// The target is container-absolute: it is resolved inside the pages
	// container, where the index root is mounted at GfehIndexContainerDir.
	if want := GfehIndexContainerDir + "/gfeh.home"; target != want {
		t.Errorf("symlink target = %q, want %q", target, want)
	}
	if !strings.HasPrefix(target, GfehIndexContainerDir+"/") {
		t.Errorf("symlink target %q is outside the mounted index root", target)
	}
}

// The reconcile writes on every ingress rebuild, which is hourly plus every
// package and page CRUD. Rewriting an unchanged file each time is pointless
// churn on a box whose storage is the product.
func TestWriteGfehIndexContentOnlyWritesWhenChanged(t *testing.T) {
	base := t.TempDir()
	page := gfeh.IndexPage{
		Network: "home",
		TLD:     "home",
		Views:   []gfeh.IndexView{{View: gfeh.ViewS3, FQDN: "s3.gfeh.home", URL: "https://s3.gfeh.home"}},
	}

	changed, err := writeGfehIndexContent(base, "gfeh.home", page)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}
	if !changed {
		t.Error("the first write did not report a change")
	}

	changed, err = writeGfehIndexContent(base, "gfeh.home", page)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if changed {
		t.Error("an unchanged index was rewritten")
	}

	page.Views = append(page.Views, gfeh.IndexView{View: gfeh.ViewHTTP, FQDN: "http.gfeh.home", URL: "https://http.gfeh.home"})
	changed, err = writeGfehIndexContent(base, "gfeh.home", page)
	if err != nil {
		t.Fatalf("third write: %v", err)
	}
	if !changed {
		t.Error("a changed index was not written")
	}
}

// The FQDN becomes a directory name, and this function creates and removes
// directories as root. gfehFQDN already refuses a traversing label, but this is
// the check that makes the function safe on its own terms.
func TestGfehIndexRefusesATraversingName(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"../escaped", "../../etc", "/absolute"} {
		if _, err := writeGfehIndexContent(base, name, gfeh.IndexPage{Network: "home"}); err == nil {
			t.Errorf("writeGfehIndexContent(%q) was allowed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(base), "escaped")); err == nil {
		t.Error("a traversing name wrote outside the index root")
	}
}

// reconcileGfehIndexes renders from the site set the ingress is programmed with
// on the same pass, so the page cannot advertise a name the route set omits.
func TestReconcileGfehIndexesRendersTheServedViews(t *testing.T) {
	base := t.TempDir()
	reconcileGfehIndexes(context.Background(), homeRegistry(), base, homeSites(t))

	raw, err := os.ReadFile(filepath.Join(base, GfehIndexDirName, "gfeh.home", "index.html"))
	if err != nil {
		t.Fatalf("read rendered index: %v", err)
	}
	out := string(raw)

	for _, fqdn := range []string{"s3.gfeh.home", "http.gfeh.home", "drive.gfeh.home", "ipfs.gfeh.home"} {
		if !strings.Contains(out, fqdn) {
			t.Errorf("the index does not list %s", fqdn)
		}
	}
	// SMB is not served and has no ingress route; sending a reader at it would
	// be sending them at a dead address.
	if strings.Contains(out, "smb.gfeh.home") {
		t.Error("the index lists the SMB view, which nothing serves")
	}
	// The container-side backend ports must never reach a reader.
	for _, port := range []string{":9000", ":9001", ":9002", ":9003"} {
		if strings.Contains(out, port) {
			t.Errorf("the index carries the container-side port %s", port)
		}
	}
}

// A partition that goes away leaves a page describing storage that is not there
// and a webroot entry pointing at nothing.
func TestReconcileGfehIndexesPrunesWhatIsNoLongerServed(t *testing.T) {
	base := t.TempDir()
	reconcileGfehIndexes(context.Background(), homeRegistry(), base, homeSites(t))

	stale := filepath.Join(base, GfehIndexDirName, "gfeh.office")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatalf("seed stale index: %v", err)
	}
	if err := ensureGfehIndexSymlink(base, "gfeh.office"); err != nil {
		t.Fatalf("seed stale symlink: %v", err)
	}

	reconcileGfehIndexes(context.Background(), homeRegistry(), base, homeSites(t))

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("a stale index directory survived the reconcile")
	}
	if _, err := os.Lstat(filepath.Join(base, PagesWebrootDir, "gfeh.office")); !os.IsNotExist(err) {
		t.Error("a stale index symlink survived the reconcile")
	}
	// And the live one is untouched.
	if _, err := os.Stat(filepath.Join(base, GfehIndexDirName, "gfeh.home", "index.html")); err != nil {
		t.Errorf("the live index was pruned: %v", err)
	}
}

// The index symlinks live in the pages webroot, which the pages reconcile
// prunes. Without this the first reconcile deletes every one of them and the
// index names 404 until the ingress is next rebuilt.
func TestGfehIndexHostnamesCoverEveryPartition(t *testing.T) {
	nm := account.InitMockNetworkManager()
	if _, err := nm.Create(t.Context(), &account.Network{Name: "office", TLD: "office", Enabled: true}); err != nil {
		t.Fatalf("create network: %v", err)
	}
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{
		"home":   allViews("home", ""),
		"office": allViews("office", "office"),
	}}

	hosts := gfehIndexHostnames(t.Context(), reg, nm, "home")
	for _, want := range []string{"gfeh.home", "gfeh.office"} {
		if _, ok := hosts[want]; !ok {
			t.Errorf("%s is not protected from the pages prune: %v", want, hosts)
		}
	}
}

// Derived from the network set alone. A prune that depended on gfehd answering
// would delete every index symlink whenever a daemon was slow to start.
func TestGfehIndexHostnamesDoNotDependOnTheDaemon(t *testing.T) {
	dead := gfeh.NewMockClient("home", "")
	dead.Errors["Names"] = errGfehIndexTestDown
	reg := stubGfehRegistry{clients: map[string]gfeh.Client{"home": dead}}

	hosts := gfehIndexHostnames(t.Context(), reg, nil, "home")
	if _, ok := hosts["gfeh.home"]; !ok {
		t.Error("a partition whose daemon is down lost its index symlink protection")
	}
}

// The webroot is shared with pages, so a page whose served FQDN collides with an
// index name would otherwise leave the two fighting over one entry on
// alternating reconciles. The index wins, matching dedupeIngressRoutes, which
// appends object storage ahead of pages and hands it the vhost too.
func TestEnsureGfehIndexSymlinkReplacesWhatIsInTheWay(t *testing.T) {
	base := t.TempDir()
	if err := EnsurePagesWebroot(base); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}
	if err := EnsurePageSymlink(base, "gfeh.home"); err != nil {
		t.Fatalf("EnsurePageSymlink: %v", err)
	}

	if err := ensureGfehIndexSymlink(base, "gfeh.home"); err != nil {
		t.Fatalf("ensureGfehIndexSymlink: %v", err)
	}
	target, err := os.Readlink(filepath.Join(base, PagesWebrootDir, "gfeh.home"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if want := GfehIndexContainerDir + "/gfeh.home"; target != want {
		t.Errorf("target = %q, want %q", target, want)
	}

	// Idempotent: a second pass must not churn the link.
	if err := ensureGfehIndexSymlink(base, "gfeh.home"); err != nil {
		t.Fatalf("second ensureGfehIndexSymlink: %v", err)
	}
}

// The prune interaction itself, through the real reconcilePages.
//
// The index webroot entries sit in the pages webroot, which this function
// sweeps of everything that is not a current page. A box with object storage and
// no pages hits the most aggressive case of that on every single pass, so the
// symptom of getting this wrong is not rare — it is permanent: the entries are
// deleted, and the index names 404 until the ingress is next rebuilt.
func TestReconcilePagesKeepsTheGfehIndexEntries(t *testing.T) {
	base := t.TempDir()
	reg := homeRegistry()

	reconcileGfehIndexes(context.Background(), reg, base, homeSites(t))
	link := filepath.Join(base, PagesWebrootDir, "gfeh.home")
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("no webroot entry to begin with: %v", err)
	}

	// No pages at all, which is the aggressive case.
	cfg := ReconcileConfig{
		BtrfsBasePath: base,
		PagesManager:  account.InitMockPagesManager(),
		Gfeh:          reg,
	}
	if err := reconcilePages(context.Background(), cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}

	if _, err := os.Readlink(link); err != nil {
		t.Errorf("the pages prune deleted the index webroot entry: %v", err)
	}
}

// And a webroot entry for a partition that is gone is still pruned, so the
// protection above is a rule about the current network set rather than a
// blanket exemption for anything gfeh-shaped.
func TestReconcilePagesPrunesAnIndexEntryForAVanishedPartition(t *testing.T) {
	base := t.TempDir()

	if err := EnsurePagesWebroot(base); err != nil {
		t.Fatalf("EnsurePagesWebroot: %v", err)
	}
	if err := ensureGfehIndexSymlink(base, "gfeh.office"); err != nil {
		t.Fatalf("seed stale entry: %v", err)
	}

	// A registry holding only home: the office partition is gone.
	cfg := ReconcileConfig{
		BtrfsBasePath: base,
		PagesManager:  account.InitMockPagesManager(),
		Gfeh:          stubGfehRegistry{clients: map[string]gfeh.Client{"home": allViews("home", "")}},
	}
	if err := reconcilePages(context.Background(), cfg); err != nil {
		t.Fatalf("reconcilePages: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(base, PagesWebrootDir, "gfeh.office")); !os.IsNotExist(err) {
		t.Error("a webroot entry for a partition that no longer exists survived the prune")
	}
}

func TestGfehTLDOfFQDN(t *testing.T) {
	for in, want := range map[string]string{
		"s3.gfeh.home":   "home",
		"gfeh.home":      "home",
		"s3.gfeh.office": "office",
		"nonsense":       "",
	} {
		if got := gfehTLDOfFQDN(in); got != want {
			t.Errorf("gfehTLDOfFQDN(%q) = %q, want %q", in, got, want)
		}
	}
}
