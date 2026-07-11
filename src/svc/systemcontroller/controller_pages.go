package systemcontroller

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/i18n"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"github.com/labstack/echo/v5"
)

// --- Pages handlers ---

type CreatePageRequest struct {
	Name           string `json:"name"`
	RepoURL        string `json:"repo_url"`
	Branch         string `json:"branch"`
	Domain         string `json:"domain"`
	SourceType     string `json:"source_type"`
	Image          string `json:"image"`
	ImageDirectory string `json:"image_directory"`
	// Network is the network the page is published on (default "home"), exactly
	// like InstallRequest.Network for a package. It selects the TLD the page's
	// hostname, cert, DNS records and ingress vhost are named under, and which
	// WireGuard peers can resolve it.
	Network string `json:"network,omitempty"`
}

type UpdatePageRequest struct {
	Name   string                 `json:"name"`
	Fields account.PageSiteUpdate `json:"fields"`
}

type PageNameRequest struct {
	Name string `json:"name"`
}

// refreshPages reprograms the shared :443 ingress (pages + HTTP packages) over
// gRPC so a created/updated/removed package or page is served immediately. The
// push is synchronous — programming is a single fast SetRoutes gRPC call (no
// container restart, unlike the legacy file-mounted ingress), and the mutex
// serializes concurrent CRUD so two reconciles cannot interleave a stale set.
// Errors are logged inside reprogramIngress, not surfaced: the periodic
// reconcile is the backstop.
func (s *SystemControllerHandlers) refreshPages(ctx context.Context) {
	s.ingressRestartMu.Lock()
	defer s.ingressRestartMu.Unlock()
	s.reprogramIngress(ctx)
}

// setPageDNS adds or removes the rolodex records for an internal page's hostname
// so it resolves immediately on create/remove. It is the page-side twin of
// registerPackageDNSForNetwork:
//
//   - a DEFAULT-network page gets a single global A record in the home zone;
//   - a NON-default-network page is DUAL-HOMED under its network's TLD — a
//     SCOPED record at the box's overlay address (served to that network's
//     WireGuard peers, and hidden from every other network's peers) plus a
//     GLOBAL record at the box's LAN address (served to loopback/LAN clients,
//     which have no route to the overlay).
//
// Public-FQDN pages are resolved by the user's own DNS and are skipped. The
// periodic DNS reconcile keeps records converged regardless, so errors here are
// only logged.
func (s *SystemControllerHandlers) setPageDNS(ctx context.Context, domain, network string, add bool) {
	cl := s.Controller.GetRolodexClient()
	if cl == nil {
		return
	}
	nm := s.Controller.GetNetworkManager()
	globalTLD := reconcileDNSTLD(s.Controller.GetSettingsManager())
	tld := pageNetworkTLD(nm, network, globalTLD)
	if pageIsPublic(domain, tld) {
		return
	}
	host := pageHostname(domain, tld)
	if host == "" {
		return
	}

	// Overlay-facing scoped record for a non-default-network page. Owning the
	// TLD via the scope is also what partitions it: rolodex hides a scope's TLD
	// from peers joined to any other scope.
	overlay := networkOverlayIPValue(nm, network)
	scoped := !pageOnDefaultNetworkName(network) && overlay != ""

	if add {
		if scoped {
			if n, err := nm.Get(network); err == nil {
				if err := rolodex.EnsureNetworkScope(ctx, cl, n.Name, n.TLD+"."); err != nil {
					logNonFatal("ensure scope for page dns", err)
				} else if err := cl.AddScopedRecord(ctx, n.Name, &upstream.DnsRecord{
					Name: host + ".", RecordType: upstream.RecordTypeA, Value: overlay, Ttl: 300,
				}); err != nil {
					logNonFatal("add scoped page record "+host, err)
				}
			}
		}
		// LAN-facing global record (both the default and the non-default case).
		// A bare global A resolves on the LAN with no authoritative zone —
		// rolodex's LAN->owning-scope fallback makes the network TLD
		// authoritative for LAN sources.
		ip := s.Controller.GetInternalIP()
		if ip == "" {
			return
		}
		if err := cl.AddRecord(ctx, &upstream.DnsRecord{
			Name:       host + ".",
			RecordType: upstream.RecordTypeA,
			Value:      ip,
			Ttl:        300,
		}); err != nil {
			slog.Debug(fmt.Sprintf("page DNS add %s: %v", host, err))
		}
		return
	}

	if scoped {
		if n, err := nm.Get(network); err == nil {
			if _, err := cl.RemoveScopedRecord(ctx, n.Name, host+".", nil); err != nil {
				slog.Debug(fmt.Sprintf("page scoped DNS remove %s: %v", host, err))
			}
		}
	}
	aType := upstream.RecordTypeA
	if _, err := cl.RemoveRecord(ctx, host+".", &upstream.RemoveRecordOptions{RecordType: &aType}); err != nil {
		slog.Debug(fmt.Sprintf("page DNS remove %s: %v", host, err))
	}
}

// pageOnDefaultNetworkName is pageOnDefaultNetwork for a bare network string.
func pageOnDefaultNetworkName(network string) bool {
	return network == "" || network == account.DefaultNetworkName
}

func (s *SystemControllerHandlers) createPage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return fmt.Errorf("%s", i18n.T(s.getLocale(), i18n.MsgPagesNotConfigured))
	}

	de := json.NewDecoder(c.Request().Body)
	req := CreatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	// Default domain to subdomain based on name if not provided.
	if req.Domain == "" {
		req.Domain = req.Name
	}

	// Default source type to archive.
	if req.SourceType == "" {
		req.SourceType = account.PageSourceArchive
	}

	// Validate the requested network the same way a package install does: an
	// unknown network is a 400, and "" normalizes to the default network.
	network, nerr := s.resolveInstallNetwork(req.Network)
	if nerr != nil {
		return nerr
	}

	page, err := mgr.Create(req.Name, req.RepoURL, req.Branch, req.Domain, req.SourceType, req.Image, req.ImageDirectory, network)
	if err != nil {
		return err
	}

	// All page storage (content subvolume, webroot symlink, the host the static
	// server matches) is keyed by the page's served FQDN — which is named under
	// the page's NETWORK TLD, not its short name and not the global dns_tld.
	dir := s.pageDirName(req.Domain, network)
	if dir == "" {
		if rerr := mgr.Remove(req.Name); rerr != nil {
			slog.Debug(fmt.Sprintf("pages rollback remove %s: %v", req.Name, rerr))
		}
		return fmt.Errorf("pages: could not derive directory for domain %q", req.Domain)
	}

	// Create the btrfs subvolume for this page's content.
	st := s.Controller.GetStorage()
	if st != nil {
		fsName := PagesVolumePrefix + "/" + dir
		if err := st.CreateFilesystem(storage.Filesystem{Name: fsName}); err != nil {
			// Rollback: remove the page from the DB.
			if rerr := mgr.Remove(req.Name); rerr != nil {
				slog.Debug(fmt.Sprintf("pages rollback remove %s: %v", req.Name, rerr))
			}
			return fmt.Errorf("create pages subvolume: %w", err)
		}
	}

	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase != "" {
		if err := EnsurePageSymlink(btrfsBase, dir); err != nil {
			slog.Debug(fmt.Sprintf("pages symlink %s: %v", dir, err))
		}
	}

	subvolPath := s.pagesSubvolumePath(dir)

	switch req.SourceType {
	case account.PageSourceGit:
		// Clone the git repository asynchronously in the background, through the
		// shared git->storage primitive — the same clone code package and
		// reconcile seeding use, just pointed at this page's subvolume.
		gitClient := s.Controller.GetGitClient()
		if gitClient != nil && subvolPath != "" {
			go func() {
				cloneErr := gitCloneIntoPath(s.ctx, gitClient, req.RepoURL, subvolPath, page.Branch)

				status := "active"
				if cloneErr != nil {
					slog.Debug(fmt.Sprintf("pages clone %s: %v", dir, cloneErr))
					// A failed clone can leave a partial/broken tree; reset the
					// content so a later rebuild starts clean. The reset leaves an
					// empty subvolume, which the reconcile git-seed then retries.
					s.resetPageContent(dir)
					status = "error"
				}

				if _, err := mgr.Update(req.Name, account.PageSiteUpdate{Status: &status}); err != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", req.Name, err))
				}
			}()
		}
	case account.PageSourceContainerImage:
		// Extract from container image asynchronously.
		if subvolPath != "" {
			extractFn := s.Controller.GetImageExtractFunc()
			go func() {
				extractErr := extractFn(s.ctx, req.Image, req.ImageDirectory, subvolPath)

				status := "active"
				if extractErr != nil {
					slog.Debug(fmt.Sprintf("pages extract image %s: %v", dir, extractErr))
					s.resetPageContent(dir)
					status = "error"
				}

				if _, err := mgr.Update(req.Name, account.PageSiteUpdate{Status: &status}); err != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", req.Name, err))
				}
			}()
		}
	case account.PageSourceArchive:
		// Archive pages remain pending until the user uploads an archive
		// via POST /pages/upload.
	}

	// Issue the leaf, render the vhost, and publish DNS so the page is reachable
	// over HTTPS immediately (content for git/image sources streams in async,
	// but the hostname, cert, and A record only depend on the page metadata).
	ctx := c.Request().Context()
	s.refreshPages(ctx)
	s.setPageDNS(ctx, req.Domain, network, true)

	return c.JSON(200, page)
}

func (s *SystemControllerHandlers) updatePage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return fmt.Errorf("%s", i18n.T(s.getLocale(), i18n.MsgPagesNotConfigured))
	}

	de := json.NewDecoder(c.Request().Body)
	req := UpdatePageRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	old, getErr := mgr.Get(req.Name)
	if getErr != nil {
		// Best effort: without the prior record we just skip retiring the old
		// hostname's DNS below; the update itself still proceeds.
		slog.Debug(fmt.Sprintf("pages get %s before update: %v", req.Name, getErr))
	}

	page, err := mgr.Update(req.Name, req.Fields)
	if err != nil {
		return err
	}

	// A domain change moves the page to a new hostname: retire the old A record,
	// publish the new one, and re-render so the vhost (and its leaf) follow.
	ctx := c.Request().Context()
	// A domain OR network change moves the page to a new hostname (the network
	// selects the TLD), so retire the old name under its OLD network and publish
	// the new one under the new network. Migrate the on-disk subvolume/symlink
	// too — they are keyed by the FQDN, so the page would otherwise serve 404
	// from a directory nothing points at.
	if old != nil && (pageDomain(*old) != pageDomain(*page) || old.Network != page.Network) {
		s.setPageDNS(ctx, pageDomain(*old), old.Network, false)
		s.migratePageDir(*old, *page)
	}
	s.setPageDNS(ctx, pageDomain(*page), page.Network, true)
	s.refreshPages(ctx)

	return c.JSON(200, page)
}

func (s *SystemControllerHandlers) removePage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return fmt.Errorf("%s", i18n.T(s.getLocale(), i18n.MsgPagesNotConfigured))
	}

	de := json.NewDecoder(c.Request().Body)
	req := PageNameRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	// Capture the domain before removal so we can retire its DNS record.
	removed, getErr := mgr.Get(req.Name)
	if getErr != nil {
		slog.Debug(fmt.Sprintf("pages get %s before remove: %v", req.Name, getErr))
	}

	if err := mgr.Remove(req.Name); err != nil {
		return err
	}

	// Resolve the FQDN directory from the captured record (falling back to the
	// name when the record could not be read).
	dir := s.pageDirName(req.Name, "")
	if removed != nil {
		dir = s.pageDirName(pageDomain(*removed), removed.Network)
	}

	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase != "" {
		if err := RemovePageSymlink(btrfsBase, dir); err != nil {
			slog.Debug(fmt.Sprintf("pages remove symlink %s: %v", dir, err))
		}
	}

	st := s.Controller.GetStorage()
	if st != nil {
		fsName := PagesVolumePrefix + "/" + dir
		if err := st.RemoveFilesystem(fsName); err != nil {
			slog.Debug(fmt.Sprintf("pages remove subvolume %s: %v", dir, err))
		}
	}

	// Drop the page's vhost from the Caddyfile and its A record from DNS.
	ctx := c.Request().Context()
	if removed != nil {
		s.setPageDNS(ctx, pageDomain(*removed), removed.Network, false)
	}
	s.refreshPages(ctx)

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listPages(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return fmt.Errorf("%s", i18n.T(s.getLocale(), i18n.MsgPagesNotConfigured))
	}

	pages, err := mgr.List()
	if err != nil {
		return err
	}

	p := readListParams(c)
	pages = filterSearch(pages, p.Search)
	sortSlice(pages, p.SortBy, p.SortOrder)

	return c.JSON(200, paginate(pages, p.Limit, p.Offset))
}

func (s *SystemControllerHandlers) rebuildPage(c *echo.Context) error {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return fmt.Errorf("%s", i18n.T(s.getLocale(), i18n.MsgPagesNotConfigured))
	}

	de := json.NewDecoder(c.Request().Body)
	req := PageNameRequest{}
	if err := de.Decode(&req); err != nil {
		return err
	}

	page, err := mgr.Get(req.Name)
	if err != nil {
		return err
	}

	locale := s.getLocale()
	btrfsBase := s.Controller.GetBtrfsBasePath()
	if btrfsBase == "" {
		return errors.New(i18n.T(locale, i18n.MsgPagesDirNotConfigured))
	}

	dir := s.pageDirName(pageDomain(*page), page.Network)
	targetDir := s.pagesSubvolumePath(dir)
	pagesDir := filepath.Join(btrfsBase, PagesVolumePrefix)

	switch page.SourceType {
	case account.PageSourceGit, "":
		gitClient := s.Controller.GetGitClient()
		if gitClient == nil {
			return errors.New(i18n.T(locale, i18n.MsgPagesGitNotConfigured))
		}

		// Check if the directory exists and has a .git directory.
		gitDir := targetDir + "/.git"
		if _, err := os.Stat(gitDir); err != nil {
			// Not cloned yet; do a fresh clone. A failed clone can leave a
			// partial tree, so reset the content before reporting the error.
			if err := gitClient.CloneBranch(c.Request().Context(), pagesDir, page.RepoURL, dir, page.Branch); err != nil {
				s.resetPageContent(dir)
				status := "error"
				if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
				}
				return fmt.Errorf("pages clone %s: %w", page.Name, err)
			}
		} else {
			// A failed pull leaves the previously-working clone intact; do not
			// reset it.
			if err := gitClient.Pull(c.Request().Context(), targetDir); err != nil {
				status := "error"
				if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
					slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
				}
				return fmt.Errorf("pages pull %s: %w", page.Name, err)
			}
		}

	case account.PageSourceContainerImage:
		if err := s.Controller.GetImageExtractFunc()(c.Request().Context(), page.Image, page.ImageDirectory, targetDir); err != nil {
			s.resetPageContent(dir)
			status := "error"
			if _, uerr := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status}); uerr != nil {
				slog.Debug(fmt.Sprintf("pages update status %s: %v", page.Name, uerr))
			}
			return fmt.Errorf("pages extract image %s: %w", page.Name, err)
		}

	case account.PageSourceArchive:
		// Archive pages must be rebuilt by uploading a new archive via
		// POST /pages/upload. This endpoint only refreshes the status.
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgPagesArchiveRebuildRequired))
	}

	status := "active"
	updated, err := mgr.Update(page.Name, account.PageSiteUpdate{Status: &status})
	if err != nil {
		return err
	}

	return c.JSON(200, updated)
}

// uploadPageArchive handles multipart form upload of an archive file and
// unpacks it into the pages directory for the named page. Only valid for
// pages with source_type "archive".
func (s *SystemControllerHandlers) uploadPageArchive(c *echo.Context) error {
	locale := s.getLocale()
	mgr := s.Controller.GetPagesManager()
	if mgr == nil {
		return errors.New(i18n.T(locale, i18n.MsgPagesNotConfigured))
	}

	name := c.FormValue("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgPagesNameRequired))
	}

	page, err := mgr.Get(name)
	if err != nil {
		return err
	}

	if page.SourceType != account.PageSourceArchive {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgPagesUploadArchiveOnly))
	}

	// Check Content-Length against max size.
	if c.Request().ContentLength > 0 {
		maxSize := s.maxArchiveSize()
		if c.Request().ContentLength > maxSize {
			return echo.NewHTTPError(http.StatusForbidden, ErrArchiveTooLarge.Error())
		}
	}

	file, header, err := c.Request().FormFile("archive")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, i18n.T(locale, i18n.MsgArchiveFileRequired, err))
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Debug(fmt.Sprintf("close upload file: %v", err))
		}
	}()

	dir := s.pageDirName(pageDomain(*page), page.Network)
	targetDir := s.pagesSubvolumePath(dir)
	if targetDir == "" {
		return errors.New(i18n.T(locale, i18n.MsgPagesDirNotConfigured))
	}

	ctx := c.Request().Context()
	br := bufio.NewReader(file)

	// Detect archive format.
	format, _, fmtErr := detectArchiveFormat(br)
	if fmtErr != nil {
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		if errors.Is(fmtErr, ErrUnsupportedArchive) {
			return echo.NewHTTPError(http.StatusBadRequest, fmtErr.Error())
		}
		return fmtErr
	}

	if _, extErr := archiveFormat(header.Filename); extErr != nil {
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		return echo.NewHTTPError(http.StatusBadRequest, extErr.Error())
	}

	// Enforce size limit.
	maxSize := s.maxArchiveSize()
	cr := &countingReader{r: io.LimitReader(br, maxSize+1)}

	timeout := s.archiveUnpackTimeout()
	unpackCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	decompCmd := decompressCommand(unpackCtx, format)

	var unpackErr error
	if decompCmd != nil {
		unpackErr = s.unpackWithValidation(unpackCtx, cr, decompCmd, targetDir, maxSize)
	} else {
		unpackErr = s.unpackPlainTar(unpackCtx, cr, targetDir, maxSize)
	}

	if unpackErr != nil {
		// A failed unpack can leave a partial tree; reset the content.
		s.resetPageContent(dir)
		status := "error"
		if _, uerr := mgr.Update(name, account.PageSiteUpdate{Status: &status}); uerr != nil {
			slog.Debug(fmt.Sprintf("pages update status %s: %v", name, uerr))
		}
		if errors.Is(unpackErr, ErrArchiveTooLarge) {
			return echo.NewHTTPError(http.StatusForbidden, unpackErr.Error())
		}
		if errors.Is(unpackErr, ErrUnsupportedArchive) || errors.Is(unpackErr, ErrInvalidTar) {
			return echo.NewHTTPError(http.StatusBadRequest, unpackErr.Error())
		}
		return unpackErr
	}

	status := "active"
	updated, err := mgr.Update(name, account.PageSiteUpdate{Status: &status})
	if err != nil {
		return err
	}

	return c.JSON(200, updated)
}

// pagesSubvolumePath returns the filesystem path for a page's btrfs subvolume,
// given its directory name (the served FQDN — see pageDirName).
func (s *SystemControllerHandlers) pagesSubvolumePath(dir string) string {
	base := s.Controller.GetBtrfsBasePath()
	if base == "" {
		return ""
	}
	return filepath.Join(base, PagesVolumePrefix, dir)
}

// pageDirName returns the on-disk directory name for a page: its served FQDN at
// the current TLD (e.g. "blog.home" for an internal page, or the public FQDN
// verbatim). The content subvolume, webroot symlink, and the host the static
// pages server matches are all keyed by this name, so two pages whose short
// labels collide (e.g. blog.a.com and blog.b.com) never share a directory.
func (s *SystemControllerHandlers) pageDirName(domain, network string) string {
	return pageHostname(domain, s.pageTLDFor(network))
}

// pageTLDFor resolves the TLD a page on the given network is named under: the
// network's own TLD, or the global dns_tld for the default network.
func (s *SystemControllerHandlers) pageTLDFor(network string) string {
	return pageNetworkTLD(s.Controller.GetNetworkManager(), network,
		reconcileDNSTLD(s.Controller.GetSettingsManager()))
}

// migratePageDir renames a page's content subvolume and webroot symlink when its
// served FQDN changes (a domain edit, or a network move that changes the TLD).
// Best-effort: failures are logged and the periodic reconcile is the backstop.
func (s *SystemControllerHandlers) migratePageDir(old, updated account.PageSite) {
	oldDir := s.pageDirName(pageDomain(old), old.Network)
	newDir := s.pageDirName(pageDomain(updated), updated.Network)
	if oldDir == "" || newDir == "" || oldDir == newDir {
		return
	}
	if st := s.Controller.GetStorage(); st != nil {
		if err := st.RenameFilesystem(PagesVolumePrefix+"/"+oldDir, PagesVolumePrefix+"/"+newDir); err != nil {
			slog.Debug(fmt.Sprintf("pages rename %s -> %s: %v", oldDir, newDir, err))
		}
	}
	if base := s.Controller.GetBtrfsBasePath(); base != "" {
		if err := RemovePageSymlink(base, oldDir); err != nil {
			slog.Debug(fmt.Sprintf("pages remove old symlink %s: %v", oldDir, err))
		}
		if err := EnsurePageSymlink(base, newDir); err != nil {
			slog.Debug(fmt.Sprintf("pages symlink %s: %v", newDir, err))
		}
	}
}

// migratePageDirsForTLD renames each internal page's content subvolume and
// webroot symlink from its old-TLD FQDN to the new-TLD FQDN when the DNS TLD
// changes, so served content follows the new hostname. Public-FQDN pages are
// unaffected (their directory name does not include the TLD, so oldDir ==
// newDir and they are skipped). Best-effort: failures are logged and the
// periodic reconcile (with pruneStalePageSymlinks) is the backstop.
func (s *SystemControllerHandlers) migratePageDirsForTLD(oldTLD, newTLD string) {
	mgr := s.Controller.GetPagesManager()
	if mgr == nil || oldTLD == newTLD {
		return
	}
	pages, err := mgr.List()
	if err != nil {
		slog.Debug(fmt.Sprintf("pages TLD migrate list: %v", err))
		return
	}
	st := s.Controller.GetStorage()
	base := s.Controller.GetBtrfsBasePath()
	for _, p := range pages {
		// Only default-network pages are named under the global dns_tld. A page on
		// a WireGuard network is named under THAT network's TLD, so a dns_tld
		// change must not touch its directory — renaming it would move the content
		// out from under the FQDN the ingress still serves.
		if !pageOnDefaultNetwork(p) {
			continue
		}
		domain := pageDomain(p)
		oldDir := pageHostname(domain, oldTLD)
		newDir := pageHostname(domain, newTLD)
		if oldDir == "" || newDir == "" || oldDir == newDir {
			continue
		}
		if st != nil {
			if err := st.RenameFilesystem(PagesVolumePrefix+"/"+oldDir, PagesVolumePrefix+"/"+newDir); err != nil {
				slog.Debug(fmt.Sprintf("pages TLD rename %s -> %s: %v", oldDir, newDir, err))
			}
		}
		if base != "" {
			if err := RemovePageSymlink(base, oldDir); err != nil {
				slog.Debug(fmt.Sprintf("pages TLD remove old symlink %s: %v", oldDir, err))
			}
			if err := EnsurePageSymlink(base, newDir); err != nil {
				slog.Debug(fmt.Sprintf("pages TLD symlink %s: %v", newDir, err))
			}
		}
	}
}

// resetPageContent empties a page's content subvolume so a failed clone,
// image extract, or archive unpack never leaves a partial/broken tree (a half-
// written .git, truncated files) that would corrupt a later rebuild. Best
// effort: remove and recreate the subvolume so the directory exists but is
// clean.
func (s *SystemControllerHandlers) resetPageContent(dir string) {
	st := s.Controller.GetStorage()
	if st == nil || dir == "" {
		return
	}
	fsName := PagesVolumePrefix + "/" + dir
	if err := st.RemoveFilesystem(fsName); err != nil {
		slog.Debug(fmt.Sprintf("pages reset remove %s: %v", dir, err))
	}
	if err := st.CreateFilesystem(storage.Filesystem{Name: fsName}); err != nil {
		slog.Debug(fmt.Sprintf("pages reset create %s: %v", dir, err))
	}
}
