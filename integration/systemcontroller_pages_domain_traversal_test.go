// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// A page's `domain` is validated only for non-emptiness --
// SQLitePagesManager.Create checks strings.TrimSpace(domain) != "" and Update
// checks the same -- yet it is not a label. It becomes a filesystem path:
//
//	pageDirName(domain, network) -> pageHostname(domain, tld)   // verbatim for a public FQDN
//	pagesSubvolumePath(dir)      -> filepath.Join(base, "pages", dir)
//	EnsurePageSymlink(base, dir) -> filepath.Join(base, "pages-webroot", dir)
//	RemovePageSymlink(base, dir) -> os.Remove(that path)
//
// None of those go through safeSubvolumePath, which is the helper the storage
// archive endpoints use for exactly this. filepath.Join collapses "..", so a
// domain of "../x.example.com" addresses a sibling of the pages tree under the
// btrfs base -- where tls/ (the local CA's private key), installed/, gfeh/ and
// data/db/system.db live.
//
// POST /pages/create is incidentally covered on a real box: CreateFilesystem
// runs storage.ValidateFilesystemName, fails, and the handler rolls back before
// reaching the symlink code. POST /pages/update is NOT. migratePageDir logs the
// RenameFilesystem failure and carries on to RemovePageSymlink /
// EnsurePageSymlink regardless, and uploadPageArchive hands the escaped path
// straight to `tar -xf - -C`.
//
// Both routes are admin-only, so this is defence in depth rather than a
// privilege boundary being crossed -- but "the admin already has root" is not a
// reason for the control plane to unlink a path of the caller's choosing, and
// the ingress consequence below is a denial of service that needs no traversal
// at all.
//
// These tests assert the SECURE behaviour and fail against the current code.

// pagesWebroot is the directory whose entries the pages container serves.
// Nothing a page names may address anything outside it.
func pagesWebroot(base string) string {
	return filepath.Join(base, systemcontroller.PagesWebrootDir)
}

// tarGz builds a one-file gzipped tar in memory, so an upload test needs no
// fixture on disk.
func tarGz(t *testing.T, name, content string) *bytes.Reader {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		t.Fatalf("tar WriteHeader: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("tar Write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}
	return bytes.NewReader(buf.Bytes())
}

// TestPagesUpdateDomainCannotUnlinkOutsideWebroot is the decisive one: it needs
// no directory to be pre-created and it reproduces on a real btrfs, because
// migratePageDir treats the rejected rename as non-fatal and proceeds to the
// symlink work with the traversing name.
//
// EnsurePageSymlink removes whatever is at the target before creating the
// link ("remove any stale entry"), so an existing file at that path is
// destroyed by a page edit.
func TestPagesUpdateDomainCannotUnlinkOutsideWebroot(t *testing.T) {
	t.Parallel()
	env := initSystemControllerPagesEnv(t)

	// A file standing in for anything the box keeps beside the pages tree. On a
	// real box the siblings of pages-webroot/ under the btrfs base include tls/,
	// which holds the local CA's private key.
	sentinelName := "sentinel.example.com"
	sentinelPath := filepath.Join(env.BtrfsBase, sentinelName)
	const sentinelBody = "do-not-delete"
	if err := os.WriteFile(sentinelPath, []byte(sentinelBody), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	if _, err := env.Client.CreatePage(context.TODO(), "victim-site", "", "", "victim.example.com",
		account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// One level up from pages-webroot/ lands on the sentinel. The name keeps a
	// dot so pageHostname treats it as a public FQDN and passes it through
	// without appending the TLD.
	traversing := "../" + sentinelName
	_, err := env.Client.UpdatePage(context.TODO(), "victim-site", account.PageSiteUpdate{Domain: &traversing})

	data, readErr := os.ReadFile(sentinelPath)
	if readErr != nil {
		t.Fatalf("a page edit unlinked %s, a file outside the pages webroot: %v", sentinelPath, readErr)
	}
	if string(data) != sentinelBody {
		t.Fatalf("a page edit replaced %s (now %q); EnsurePageSymlink planted a symlink outside the webroot",
			sentinelPath, string(data))
	}

	if err == nil {
		t.Error("UpdatePage accepted a domain that is not a hostname; it becomes a filesystem path")
	}
}

// The same traversal aimed at the unpack target. `tar -xf - -C <dir>` needs the
// directory to exist, so the test creates it -- which is the realistic case: on
// a real box every sibling of pages/ under the btrfs base (tls/, installed/,
// gfeh/, archives/, user/) already exists, and reconcile creates them at boot.
func TestPagesUploadCannotUnpackOutsidePagesTree(t *testing.T) {
	t.Parallel()
	env := initSystemControllerPagesEnv(t)

	// Stand-in for an existing sibling of the pages subvolume.
	escapeName := "escape.example.com"
	escapeDir := filepath.Join(env.BtrfsBase, escapeName)
	if err := os.MkdirAll(escapeDir, 0o750); err != nil {
		t.Fatalf("MkdirAll escape dir: %v", err)
	}
	// The real target directory has to exist too, or the fixture proves nothing
	// about which of the two the unpack chose.
	if err := os.MkdirAll(filepath.Join(env.BtrfsBase, "pages", "upload.example.com"), 0o750); err != nil {
		t.Fatalf("MkdirAll pages dir: %v", err)
	}

	if _, err := env.Client.CreatePage(context.TODO(), "upload-site", "", "", "upload.example.com",
		account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	traversing := "../" + escapeName
	if _, err := env.Client.UpdatePage(context.TODO(), "upload-site", account.PageSiteUpdate{Domain: &traversing}); err != nil {
		// A fix that rejects the domain here is the right one, and it makes the
		// rest of this test moot.
		t.Skipf("UpdatePage rejected the traversing domain (%v); nothing left to escape with", err)
	}

	const marker = "town-os-escaped-marker"
	if _, err := env.Client.UploadPageArchive(context.TODO(), "upload-site", tarGz(t, marker, "x"), "site.tar.gz"); err != nil {
		t.Logf("UploadPageArchive returned %v (the escape is asserted below regardless)", err)
	}

	if _, err := os.Stat(filepath.Join(escapeDir, marker)); err == nil {
		t.Errorf("page upload unpacked into %s, outside the pages tree; "+
			"uploadPageArchive passes pagesSubvolumePath's result to `tar -C` without safeSubvolumePath", escapeDir)
	}
}

// A domain that is not a hostname should be refused where it enters, on both
// routes, rather than being caught downstream by whichever consumer happens to
// validate. This is the fix the two tests above are really asking for.
func TestPagesRejectDomainsThatAreNotHostnames(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		domain string
	}{
		{"parent traversal", "../escape.example.com"},
		{"deep traversal", "../../../../etc/cron.d"},
		{"absolute path", "/etc/cron.d"},
		{"embedded slash", "site.example.com/../../etc"},
		{"newline", "site.example.com\nblog.example.com"},
		{"null byte", "site.example.com\x00"},
		// Whitespace is not only untidy: caddy reads space-separated site
		// addresses as several addresses for one block, so this claims a
		// hostname belonging to another service.
		{"space", "site.example.com other.example.com"},
		// Not a traversal and not an injection — just not a hostname. A page
		// named with an underscore cannot be covered by a certificate SAN, so
		// it would resolve and then fail every TLS handshake.
		{"underscore", "my_site.example.com"},
		{"leading underscore label", "_acme-challenge.example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := initSystemControllerPagesEnv(t)

			if _, err := env.Client.CreatePage(context.TODO(), "created-"+strings.ReplaceAll(tc.name, " ", "-"),
				"", "", tc.domain, account.PageSourceArchive, "", "", ""); err == nil {
				t.Errorf("CreatePage accepted domain %q", tc.domain)
			}

			if _, err := env.Client.CreatePage(context.TODO(), "edited-"+strings.ReplaceAll(tc.name, " ", "-"),
				"", "", "ok.example.com", account.PageSourceArchive, "", "", ""); err != nil {
				t.Fatalf("CreatePage with a valid domain: %v", err)
			}
			domain := tc.domain
			if _, err := env.Client.UpdatePage(context.TODO(), "edited-"+strings.ReplaceAll(tc.name, " ", "-"),
				account.PageSiteUpdate{Domain: &domain}); err == nil {
				t.Errorf("UpdatePage accepted domain %q", tc.domain)
			}
		})
	}
}

// The counterpart to the rejection table: the domains real pages actually use
// must still work, over the full HTTP stack, on both create and update. Without
// this, "reject everything that is not [a-z]" would satisfy the table above.
func TestPagesAcceptOrdinaryDomains(t *testing.T) {
	t.Parallel()

	for i, domain := range []string{
		"blog",
		"site.example.com",
		"my-site.example.com",
		"9front.example.com",
		"a.b.c.example.com",
	} {
		name := fmt.Sprintf("page-%d", i)
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			env := initSystemControllerPagesEnv(t)

			if _, err := env.Client.CreatePage(context.TODO(), name, "", "", domain,
				account.PageSourceArchive, "", "", ""); err != nil {
				t.Fatalf("CreatePage with domain %q: %v", domain, err)
			}

			updated := "renamed-" + domain
			if _, err := env.Client.UpdatePage(context.TODO(), name,
				account.PageSiteUpdate{Domain: &updated}); err != nil {
				t.Fatalf("UpdatePage to domain %q: %v", updated, err)
			}
		})
	}
}

// Nothing a page names may end up outside pages-webroot/. Asserted over the
// directory rather than over one known path, so a traversal shape nobody
// thought of still trips it.
func TestPagesWebrootHoldsOnlyItsOwnEntries(t *testing.T) {
	t.Parallel()
	env := initSystemControllerPagesEnv(t)

	if _, err := env.Client.CreatePage(context.TODO(), "site", "", "", "site.example.com",
		account.PageSourceArchive, "", "", ""); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	before := siblingNames(t, env.BtrfsBase)

	traversing := "../planted.example.com"
	if _, err := env.Client.UpdatePage(context.TODO(), "site", account.PageSiteUpdate{Domain: &traversing}); err != nil {
		t.Skipf("UpdatePage rejected the traversing domain (%v); nothing left to escape with", err)
	}

	for name := range siblingNames(t, env.BtrfsBase) {
		if !before[name] {
			t.Errorf("a page edit created %q beside the pages tree, outside %s",
				filepath.Join(env.BtrfsBase, name), pagesWebroot(env.BtrfsBase))
		}
	}
}

// siblingNames is the set of entries directly under the btrfs base.
func siblingNames(t *testing.T, base string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", base, err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}
