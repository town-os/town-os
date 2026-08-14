// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"context"
	"log/slog"

	"gitea.com/town-os/town-os/src/gfeh"
	ingresspb "gitea.com/town-os/town-os/src/ingress/proto"
)

// The published-files index, on the Town OS side: how a page written by this box
// comes to be served at the root of a name whose every other path belongs to
// gfehd.
//
// # One name, two backends
//
// http.gfeh.<tld> is gfehd's: it answers /f/<token> and nothing else, and its
// root 404s (see the header of src/gfeh/public_index.go for what that costs).
// The index cannot be served by gfehd — Town OS does not get to add routes to
// somebody else's daemon — and it cannot take a name of its own without
// inventing a second thing for an operator to learn, one that would also have to
// be in DNS, in the leaf's SAN set, and in the route table.
//
// So the name stays gfehd's and the ingress splits it by path: "/" is handled by
// the pages container, everything else falls through to gfehd. That is what
// PathBackend on an ingress Route exists for, and this is its only caller.
//
// # Why "/" and not "/index" or a catch-all
//
// The matcher is exactly the root. A prefix would shadow paths gfehd may grow
// later, and Town OS would have taken them silently — the whole failure this
// page exists to fix, inverted. The root is the one path gfehd has said it does
// not serve, by 404ing it, and the page rendered there is self-contained (inline
// CSS, no script, no assets) precisely so one path is all it needs.

// gfehPublicIndexPath is the caddy matcher the published-files index is served
// at: the root, exactly, and no other path.
const gfehPublicIndexPath = "/"

// gfehPublicIndexSites picks the http-view site out of a collected set.
//
// Keyed by FQDN because that is what the route, the directory, and the webroot
// symlink are all named by; the network comes along for the page's own heading.
func gfehPublicIndexSites(sites []GfehSite) map[string]GfehSite {
	out := map[string]GfehSite{}
	for _, s := range sites {
		if s.View == gfeh.ViewHTTP && s.HTTP && s.FQDN != "" {
			out[s.FQDN] = s
		}
	}
	return out
}

// reconcileGfehPublicIndexes writes the published-files index for every http
// view in the site set, and returns the names it actually wrote.
//
// The return value is the contract with the route builder: a path backend is
// programmed only for a name whose bytes are on disk. Getting that order wrong
// is not cosmetic — routing "/" to the pages container for a name it has no
// webroot entry for replaces gfehd's 404 with Caddy's, which is the same failure
// with a different logo.
//
// A partition whose exposures cannot be read contributes nothing and keeps its
// old page, if it had one, rather than being rewritten to "nothing published
// yet": the daemon being briefly unreachable is not evidence that somebody
// withdrew every file, and a reconcile that acted on it that way would empty the
// index on every restart and refill it a pass later.
func reconcileGfehPublicIndexes(ctx context.Context, reg GfehRegistry, btrfsBase string, sites []GfehSite) map[string]struct{} {
	if reg == nil || btrfsBase == "" {
		return nil
	}

	targets := gfehPublicIndexSites(sites)
	if len(targets) == 0 {
		return nil
	}

	clients := reg.Clients()
	written := make(map[string]struct{}, len(targets))
	for fqdn, site := range targets {
		client, ok := clients[site.Network]
		if !ok || client == nil {
			// The site came from this same registry a moment ago, so this is a
			// partition that was removed mid-reconcile rather than a lookup that
			// was ever expected to miss.
			slog.Debug("gfeh published index: no client for network", "network", site.Network, "fqdn", fqdn)
			continue
		}

		exposures, err := client.ListExposures(ctx)
		if err != nil {
			slog.Debug("gfeh published index: list exposures", "network", site.Network, "fqdn", fqdn, "error", err)
			continue
		}

		page := gfeh.PublicIndexPage{
			Network: site.Network,
			FQDN:    fqdn,
			Files:   gfeh.PublicFilesFromExposures(exposures),
		}
		if _, werr := writeGfehIndexFile(btrfsBase, fqdn, gfeh.RenderPublicIndex(page)); werr != nil {
			slog.Debug("gfeh published index: write", "network", site.Network, "fqdn", fqdn, "error", werr)
			continue
		}
		written[fqdn] = struct{}{}
	}
	return written
}

// gfehPublicIndexBackends is the path-scoped backend list for one gfeh route:
// the pages container at the root for an http view whose index exists, and
// nothing at all for every other name.
//
// Returning nil rather than an empty slice matters to the rendered bytes: a
// route with no path backends renders as a plain reverse_proxy, which is what
// every other vhost on the box is and what the ingress supervisor has already
// decided not to reload.
func gfehPublicIndexBackends(site GfehSite, published map[string]struct{}) []*ingresspb.PathBackend {
	if site.View != gfeh.ViewHTTP {
		return nil
	}
	if _, ok := published[site.FQDN]; !ok {
		return nil
	}
	return []*ingresspb.PathBackend{{
		Path:    gfehPublicIndexPath,
		Backend: pagesBackend(),
	}}
}
