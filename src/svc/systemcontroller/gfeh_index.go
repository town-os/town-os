// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/gfeh"
)

// The partition index, on the Town OS side: where its bytes live, how the
// static server reaches them, and how it becomes a name.
//
// # Why the pages container serves it
//
// The index is static HTML that has to appear on :443 under a local-CA leaf,
// which is exactly what the pages service already does for every page on the
// box — a Caddy container on the ingress network rooting /srv/{host}, fronted by
// an ingress vhost. So the index needs no server of its own. The alternatives
// were worse in specific ways: serving it from gfehd would mean asking gfeh to
// grow a route that is Town OS's opinion about presentation; serving it from the
// system controller would mean an ingress backend on the host network, which the
// ingress reaches by container name and cannot; and emitting it inline as a
// Caddy `respond` body would put generated HTML inside the config file, where an
// escaping mistake does not break one page but makes Caddy reject the whole
// config and takes every vhost on the box down.
//
// # Why it is not under the pages tree
//
// Content lives under its own gfeh-index/ root rather than in pages/, for the
// same reason gfeh-control is a sibling of gfeh/ rather than a directory inside
// it: everything under pages/ is a *page*, owned by a row in the pages table and
// swept by the pages reconcile. A generated directory sitting in that namespace
// would be a page that pages does not know about, which is the shape that ends
// with one of the two deleting the other's content.
//
// The webroot is the one thing they must share, because it is what the container
// mounts — so the index appears there as a symlink beside the page symlinks, and
// pruneStalePageSymlinks is told about it (see gfehIndexHostnames).

const (
	// GfehIndexDirName is the host root the generated index pages live under,
	// a sibling of the object-storage root and of gfeh-control.
	GfehIndexDirName = "gfeh-index"

	// GfehIndexContainerDir is where that root is mounted in the pages
	// container. The webroot symlinks point here, so it is the target half of
	// every link and must match the mount in StartPagesService.
	GfehIndexContainerDir = "/data/" + GfehIndexDirName

	// gfehIndexFileName is the file Caddy's file_server resolves a directory
	// request to.
	gfehIndexFileName = "index.html"
)

// gfehIndexRoot is the host directory holding every partition's index.
func gfehIndexRoot(btrfsBase string) string {
	return filepath.Join(btrfsBase, GfehIndexDirName)
}

// gfehIndexDir resolves one partition's index directory, refusing a name that
// would leave the index root.
//
// The name is an FQDN composed from a label gfehd reported, and gfehFQDN already
// refuses a label that is not a run of DNS labels — so this cannot currently
// fail. It is here anyway because the two checks answer to different owners: the
// one in gfehFQDN protects the vhost and the zone and could be relaxed for a
// reason that has nothing to do with paths, and this one is what makes a
// function that creates and removes directories as root safe on its own terms.
func gfehIndexDir(btrfsBase, fqdn string) (string, error) {
	// safeSubvolumePath alone is not quite the check this wants. It guarantees
	// the result stays inside the base, which is the security property and is
	// genuinely enough to stop `../../etc` — but it gets there through
	// filepath.Join, which *normalizes*: "/absolute" joins to <root>/absolute,
	// lands inside the root, and is accepted. That is not an escape, so nothing
	// is unsafe about it; it is a name being silently reinterpreted as a
	// different one.
	//
	// Which matters here specifically, because this name is not decoration. The
	// pages Caddyfile roots on /srv/{host}, so the directory name must be the
	// FQDN exactly. A name that quietly becomes a different name yields a
	// directory nothing ever resolves to, and the symptom is the one this whole
	// feature exists to remove: a name that resolves, presents a valid
	// certificate, and 404s.
	//
	// Checked with filepath.Clean rather than gfeh.ValidateLabel, deliberately:
	// this is a question about paths, and borrowing the DNS rule would tie the
	// directory layer to a predicate that may be relaxed for naming reasons —
	// and would reject a TLD that is unusual but working, taking a live index
	// down to enforce a rule about hostnames.
	if err := validGfehIndexName(fqdn); err != nil {
		return "", err
	}
	return safeSubvolumePath(gfehIndexRoot(btrfsBase), fqdn)
}

// validGfehIndexName rejects anything that is not a plain relative name.
//
// Shared by the two functions that turn an FQDN into a path — the content
// directory and the webroot symlink — because they must agree on what a legal
// name is. If only one checked, the other would compose a path for a name the
// first refused, and the pair would disagree about where a partition's index
// lives; that is the mismatch class this feature is most exposed to.
func validGfehIndexName(fqdn string) error {
	if fqdn == "" || strings.HasPrefix(fqdn, "/") || filepath.Clean(fqdn) != fqdn {
		return fmt.Errorf("%w: gfeh index name %q is not a plain relative name", ErrSubvolumeTraversal, fqdn)
	}
	return nil
}

// gfehIndexSite is the site a partition's index is served as.
//
// It is a GfehSite rather than a thing of its own so that the index gets its A
// record, its AAAA, its scoped overlay record, its DANE pin, its place in the
// leaf's SAN set and its ingress route from the code that already derives all
// six for the views — one collector, one FQDN, one certificate. A parallel path
// for "the index name" is how the vhost and the SAN come to disagree.
//
// HTTP is set explicitly rather than through gfeh.IsHTTPView, which answers only
// for views gfehd reports; the backend is the pages container, not gfehd's.
// Port is zero for the same reason: there is no gfeh listener behind this name,
// and the ingress answers it on :443 like every other HTTP view.
func gfehIndexSite(network, tld string) (GfehSite, bool) {
	fqdn := gfehFQDN(gfeh.IndexLabel, tld)
	if fqdn == "" {
		return GfehSite{}, false
	}
	return GfehSite{
		Network: network,
		View:    gfeh.ViewIndex,
		FQDN:    fqdn,
		Backend: pagesBackend(),
		HTTP:    true,
	}, true
}

// gfehIndexHostnames is every index FQDN the box could be serving, derived from
// the network set alone.
//
// Deliberately without asking a single daemon. Its one caller is the pages
// reconcile, which uses it to keep pruneStalePageSymlinks from deleting the
// index symlinks — and a prune that depended on gfehd answering would delete
// them all every time a partition was slow to start, taking the index off the
// air until the next pass rewrote it. What may be pruned has to be decidable
// from state Town OS owns.
func gfehIndexHostnames(ctx context.Context, reg GfehRegistry, nm account.NetworkManager, globalTLD string) map[string]struct{} {
	out := map[string]struct{}{}
	if reg == nil {
		return out
	}
	for network := range reg.Clients() {
		tld := gfehNetworkTLD(ctx, nm, network, globalTLD)
		// Both generated names: the partition index, and the http view's root.
		// The second belongs here for exactly the reason the first does — the
		// pages prune deletes any webroot entry it cannot account for, and the
		// published-files index owns its entry just as much even though the name
		// is also an ingress route to gfehd for /f/<token>.
		for _, label := range []string{gfeh.IndexLabel, gfeh.ViewLabel(gfeh.ViewHTTP)} {
			if fqdn := gfehFQDN(label, tld); fqdn != "" {
				out[fqdn] = struct{}{}
			}
		}
	}
	return out
}

// writeGfehIndexContent renders one partition's index to disk and links it into
// the pages webroot.
//
// Returns whether the file changed, which the caller logs rather than acts on:
// the pages container serves from the filesystem and re-reads per request, so
// unlike the Caddyfile there is nothing to reload.
func writeGfehIndexContent(btrfsBase, fqdn string, page gfeh.IndexPage) (bool, error) {
	return writeGfehIndexFile(btrfsBase, fqdn, gfeh.RenderIndex(page))
}

// writeGfehIndexFile puts already-rendered bytes at a name's index path and
// links that name into the pages webroot.
//
// Split from writeGfehIndexContent so the published-files index
// (gfeh_public_index.go) lands through exactly the same directory check,
// write-if-changed, and symlink as the partition index. Two writers would be two
// opinions about where an index lives and which names the prune may remove.
func writeGfehIndexFile(btrfsBase, fqdn string, content []byte) (bool, error) {
	dir, err := gfehIndexDir(btrfsBase, fqdn)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301 -- served read-only by the pages container
		return false, fmt.Errorf("create gfeh index dir: %w", err)
	}

	path := filepath.Join(dir, gfehIndexFileName)
	prev, readErr := os.ReadFile(path) //nolint:gosec // G304 -- path under the trusted btrfs base, traversal-checked above
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, fmt.Errorf("read existing gfeh index: %w", readErr)
	}
	changed := string(prev) != string(content)
	if changed {
		if err := os.WriteFile(path, content, 0o644); err != nil { //nolint:gosec // G306 -- readable by the pages container
			return false, fmt.Errorf("write gfeh index: %w", err)
		}
	}

	if err := ensureGfehIndexSymlink(btrfsBase, fqdn); err != nil {
		return changed, err
	}
	return changed, nil
}

// ensureGfehIndexSymlink points the pages webroot entry for an index FQDN at the
// container-absolute path its content is mounted under.
//
// Idempotent, and it replaces whatever is in the way. A page whose served FQDN
// collides with an index name would otherwise leave the two fighting over one
// webroot entry on alternating reconciles; the index wins, matching
// dedupeIngressRoutes, which appends object storage ahead of pages and so hands
// the vhost to the index as well. Losing both consistently beats losing each on
// alternate passes.
func ensureGfehIndexSymlink(btrfsBase, fqdn string) error {
	// The webroot is created here rather than assumed, because the alternative
	// is a function that only works when something else ran first. Its one
	// production caller does create it — but on a box with object storage and no
	// pages, nothing else on the boot path has any reason to, so "the pages
	// reconcile made the directory" is a dependency on a subsystem that may have
	// nothing to do. Idempotent, so paying for it per index costs a stat.
	if err := validGfehIndexName(fqdn); err != nil {
		return err
	}
	if err := EnsurePagesWebroot(btrfsBase); err != nil {
		return fmt.Errorf("gfeh index symlink %q: ensure pages webroot: %w", fqdn, err)
	}

	linkPath, err := pageSymlinkPath(btrfsBase, fqdn)
	if err != nil {
		return fmt.Errorf("gfeh index symlink %q: %w", fqdn, err)
	}
	target := GfehIndexContainerDir + "/" + fqdn

	if existing, rerr := os.Readlink(linkPath); rerr == nil && existing == target {
		return nil
	}
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		slog.Debug("remove stale gfeh index webroot entry", "path", linkPath, "error", err)
	}
	return os.Symlink(target, linkPath)
}

// pruneGfehIndexes removes index directories, and their webroot symlinks, for
// names that are no longer served.
//
// Best-effort and logged, like every other prune: a directory left behind serves
// a page describing a partition that is gone, which is untidy, while failing the
// reconcile over it would be worse.
func pruneGfehIndexes(btrfsBase string, valid map[string]struct{}) {
	root := gfehIndexRoot(btrfsBase)
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("prune gfeh indexes: read index root", "error", err)
		}
		return
	}
	for _, e := range entries {
		name := e.Name()
		if _, ok := valid[name]; ok {
			continue
		}
		dir, derr := gfehIndexDir(btrfsBase, name)
		if derr != nil {
			slog.Debug("prune gfeh indexes: unsafe name", "name", name, "error", derr)
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			slog.Debug("prune gfeh index", "name", name, "error", err)
		}
		removeGfehIndexSymlink(btrfsBase, name)
	}
}

// removeGfehIndexSymlink drops a webroot entry, but only if it is still ours.
//
// The check is not paranoia about our own writes: the webroot is shared with
// pages, and a page created after an index of the same name went away owns that
// entry now. Deleting it because a directory with a matching name is being
// pruned would take a live page off the air on a pass that had nothing to do
// with it — and the pages reconcile would only put it back on the next one.
func removeGfehIndexSymlink(btrfsBase, name string) {
	linkPath, err := pageSymlinkPath(btrfsBase, name)
	if err != nil {
		slog.Debug("prune gfeh index symlink: unsafe name", "name", name, "error", err)
		return
	}
	target, err := os.Readlink(linkPath)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Debug("prune gfeh index symlink: read", "name", name, "error", err)
		}
		return
	}
	if !strings.HasPrefix(target, GfehIndexContainerDir+"/") {
		// Somebody else's entry now.
		return
	}
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		slog.Debug("prune gfeh index symlink", "name", name, "error", err)
	}
}

// reconcileGfehIndexes writes an index for every partition represented in sites,
// and prunes the ones that are not.
//
// It takes the already-collected sites rather than collecting its own so the
// page describes exactly the view set the ingress is being programmed with on
// this same pass. Two collections would be two answers from a daemon that is
// free to have restarted between them, and the index would then advertise a name
// the route set does not carry.
// It returns the http-view names that now have a published-files index on disk,
// so the caller can path-route only the ones whose bytes exist.
func reconcileGfehIndexes(ctx context.Context, reg GfehRegistry, btrfsBase string, sites []GfehSite) map[string]struct{} {
	if btrfsBase == "" {
		return nil
	}
	if err := EnsurePagesWebroot(btrfsBase); err != nil {
		slog.Debug("gfeh index: ensure pages webroot", "error", err)
		return nil
	}

	pages := map[string]*gfeh.IndexPage{}
	fqdns := map[string]string{}
	for _, s := range sites {
		if s.View == gfeh.ViewIndex {
			fqdns[s.Network] = s.FQDN
			continue
		}
		if !s.HTTP {
			// SMB contributes a DNS record and no route, and an index row for a
			// view nothing serves would send the reader at a dead address.
			continue
		}
		page, ok := pages[s.Network]
		if !ok {
			page = &gfeh.IndexPage{Network: s.Network, TLD: gfehTLDOfFQDN(s.FQDN)}
			pages[s.Network] = page
		}
		page.Views = append(page.Views, gfeh.IndexView{
			View: s.View,
			FQDN: s.FQDN,
			URL:  "https://" + s.FQDN,
		})
	}

	valid := make(map[string]struct{}, len(fqdns))
	for network, fqdn := range fqdns {
		page, ok := pages[network]
		if !ok {
			// The index site exists but the partition served no view. Rendered
			// anyway, saying so: a name in DNS that answers nothing is the
			// failure this page exists to replace.
			page = &gfeh.IndexPage{Network: network, TLD: gfehTLDOfFQDN(fqdn)}
		}
		valid[fqdn] = struct{}{}
		if _, err := writeGfehIndexContent(btrfsBase, fqdn, *page); err != nil {
			slog.Debug("write gfeh index", "network", network, "fqdn", fqdn, "error", err)
		}
	}

	// The http view's own root, from the same site set.
	published := reconcileGfehPublicIndexes(ctx, reg, btrfsBase, sites)

	// What the prune may remove is decided by the names the box *has*, not by
	// the ones this pass managed to render — the same rule gfehIndexHostnames
	// states for the webroot, and it matters here for the same reason. These
	// pages share one root with the partition indexes, so a valid set built from
	// `published` would delete the page of every partition whose daemon happened
	// to be unreachable, which is precisely the case reconcileGfehPublicIndexes
	// declines to rewrite because a brief outage is not a withdrawal.
	for fqdn := range gfehPublicIndexSites(sites) {
		valid[fqdn] = struct{}{}
	}

	pruneGfehIndexes(btrfsBase, valid)
	return published
}

// gfehTLDOfFQDN recovers the zone a gfeh name was qualified under.
//
// Every gfeh label is "<view>.gfeh" or "gfeh", so whatever follows the first
// "gfeh." — or the whole remainder after the leading label — is the TLD. It is
// used for display only; nothing routes on the result.
func gfehTLDOfFQDN(fqdn string) string {
	if _, rest, ok := strings.Cut(fqdn, gfeh.IndexLabel+"."); ok {
		return rest
	}
	return ""
}
