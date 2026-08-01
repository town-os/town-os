package systemcontroller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

const (
	PackagesVolumePrefix    = "installed"
	UninstalledVolumePrefix = "uninstalled"
	PagesVolumePrefix       = "pages"
	UserVolumePrefix        = "user"
	// GfehVolumePrefix is the object-storage root: every gfeh partition is a
	// subvolume beneath it. Reserved like the others, so nobody can create a
	// user volume that shadows a partition — and specifically NOT reachable
	// through /storage/create, which rewrites every submitted name to
	// user/<name>. That rewrite is exactly why partitions need their own
	// /gfeh/partitions/* handlers (see TOWNOS_CONTRACT.md in the gfeh repo).
	GfehVolumePrefix = "gfeh"
)

// isReservedFilesystem returns true if the given name is one of the
// system-managed volume prefixes that users must not create, modify, or delete.
func isReservedFilesystem(name string) bool {
	if name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix || name == VMImagesSubvolume || name == UserVolumePrefix || name == TLSSubvolume || name == packages.SubpackagesDir || name == GfehVolumePrefix {
		return true
	}
	if strings.HasPrefix(name, PackagesVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, UninstalledVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, ArchivesSubvolume+"/") {
		return true
	}
	if strings.HasPrefix(name, PagesVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, VMImagesSubvolume+"/") {
		return true
	}
	if strings.HasPrefix(name, UserVolumePrefix+"/") {
		return true
	}
	if strings.HasPrefix(name, TLSSubvolume+"/") {
		return true
	}
	if strings.HasPrefix(name, packages.SubpackagesDir+"/") {
		return true
	}
	if strings.HasPrefix(name, GfehVolumePrefix+"/") {
		return true
	}
	return false
}

// isPackageVolume returns true when name is an installed or uninstalled package
// volume path (has an installed/ or uninstalled/ prefix followed by content).
func isPackageVolume(name string) bool {
	return strings.HasPrefix(name, PackagesVolumePrefix+"/") ||
		strings.HasPrefix(name, UninstalledVolumePrefix+"/")
}

// parsedInstalledPath is the structured result of parseInstalledPath.
// When ok=true the entry is a legitimate package path; version and
// volName may still be empty when the path is a bare package dir (the
// classifier still wants to surface those in the storage list) or a
// bare version dir with no volume leaf yet. Callers that specifically
// want volume leaves (e.g. listPackageVolumes) should gate on
// volName != "".
type parsedInstalledPath struct {
	state         string // "installed" or "uninstalled"
	repo          string
	effectiveName string // flat --dep-- form
	prettyName    string // slash-separated pretty form
	version       string // may be empty for bare name / dep-key dirs
	volName       string // may be empty for bare version dirs
}

// parseInstalledPath splits a btrfs internal name such as
// "installed/<repo>/<parent>/subpackages/<key>/<version>/<vol>" into its
// structured pieces. The first segment after the repo becomes the first
// piece of the effective name; any following `subpackages/<key>` pair
// appends another piece; the next segment is the version and the
// remainder is the volume name.
//
// ok=false is returned for:
//   - paths whose prefix isn't installed/ or uninstalled/
//   - paths too short to carry a package (missing repo or package name)
//   - infra paths that terminate on a `subpackages` container without a
//     following key (e.g. "installed/core/gitea/subpackages") or that
//     place `subpackages` directly under the repo
//
// Ambiguity note: an infra path like `installed/<repo>/<parent>/subpackages`
// looks just like a malformed flat dir `installed/<repo>/<parent>/<version>`
// where version == "subpackages". The reservation enforced by
// packages.SubpackagesDir on dep keys and package names guarantees this
// collision cannot happen for legitimate installs, so the parser safely
// treats any literal `subpackages` segment as the encapsulator marker.
func parseInstalledPath(internalName string) (parsedInstalledPath, bool) {
	var p parsedInstalledPath

	var rest string
	switch {
	case strings.HasPrefix(internalName, PackagesVolumePrefix+"/"):
		p.state = "installed"
		rest = strings.TrimPrefix(internalName, PackagesVolumePrefix+"/")
	case strings.HasPrefix(internalName, UninstalledVolumePrefix+"/"):
		p.state = "uninstalled"
		rest = strings.TrimPrefix(internalName, UninstalledVolumePrefix+"/")
	default:
		return p, false
	}

	if rest == "" {
		return p, false
	}
	segs := strings.Split(rest, "/")
	if len(segs) < 2 {
		// Bare repo dir (e.g. "installed/core") — skip.
		return p, false
	}
	p.repo = segs[0]
	segs = segs[1:]

	// First segment after repo must be a package name piece — a bare
	// `subpackages` here is malformed.
	if segs[0] == packages.SubpackagesDir {
		return p, false
	}
	nameParts := []string{segs[0]}
	i := 1

	// Alternate (subpackages, key) pairs.
	for i+1 < len(segs) && segs[i] == packages.SubpackagesDir {
		nameParts = append(nameParts, segs[i+1])
		i += 2
	}

	p.effectiveName = strings.Join(nameParts, packages.DependencySeparator)
	p.prettyName = strings.Join(nameParts, "/")

	if i >= len(segs) {
		// Bare package or dep-key directory (nothing after the name
		// chain). ok=true with empty version/volName so the filesystem
		// lister still surfaces the dir with its package pretty name.
		return p, true
	}
	// A trailing `subpackages` without a key (e.g. the container subvol
	// itself) is infra and should be skipped entirely.
	if segs[i] == packages.SubpackagesDir {
		return parsedInstalledPath{}, false
	}
	p.version = segs[i]
	i++
	if i < len(segs) {
		p.volName = strings.Join(segs[i:], "/")
	}
	return p, true
}

// classifyFilesystem determines the state of a filesystem based on its name
// prefix. Returns the state ("user", "installed", "uninstalled") and the
// display name with internal prefixes and repository component stripped.
// Root subvolumes (installed, uninstalled, empty name) and infra
// intermediate subvolumes (the `subpackages` encapsulator, bare package
// dirs, bare version dirs) return empty state to signal they should be
// skipped.
func classifyFilesystem(name string) (state, displayName string) {
	if name == "" || name == PackagesVolumePrefix || name == UninstalledVolumePrefix || name == ArchivesSubvolume || name == PagesVolumePrefix || name == VMImagesSubvolume || name == UserVolumePrefix {
		return "", name
	}

	archivesPrefix := ArchivesSubvolume + "/"
	if strings.HasPrefix(name, archivesPrefix) {
		return "", name
	}

	pagesPrefix := PagesVolumePrefix + "/"
	if strings.HasPrefix(name, pagesPrefix) {
		return "", name
	}

	vmImagesPrefix := VMImagesSubvolume + "/"
	if strings.HasPrefix(name, vmImagesPrefix) {
		return "", name
	}

	if p, ok := parseInstalledPath(name); ok {
		// Display form uses the pretty name so dep volumes render as
		// "parent/key/version/vol" instead of the on-disk
		// "parent/subpackages/key/version/vol". Bare package / version
		// directories (empty version / volName) still surface with
		// whatever parts are present so users can browse the tree.
		display := p.prettyName
		if p.version != "" {
			display += "/" + p.version
		}
		if p.volName != "" {
			display += "/" + p.volName
		}
		return p.state, display
	}

	// Install/uninstall prefix matched but parseInstalledPath said ok=false —
	// infra intermediate subvolume, skip.
	if strings.HasPrefix(name, PackagesVolumePrefix+"/") || strings.HasPrefix(name, UninstalledVolumePrefix+"/") {
		return "", name
	}

	userPrefix := UserVolumePrefix + "/"
	if after, ok := strings.CutPrefix(name, userPrefix); ok {
		return "user", after
	}

	return "", name
}

// serviceNameFromVolumePath derives the systemd service unit name from a
// volume internal name like
// "installed/<repo>/<parent>/subpackages/<key>/<version>/<vol>". Returns
// empty string if the path does not have enough components to identify a
// package — specifically, bare repo / package / dep-key / subpackages
// container paths have no version, and systemd unit names require one.
func serviceNameFromVolumePath(internalName string) string {
	p, ok := parseInstalledPath(internalName)
	if !ok || p.version == "" {
		return ""
	}
	return systemd.UnitName(p.repo, p.effectiveName, p.version)
}

// packageVolumePath returns the canonical on-disk btrfs volume path for
// a package volume. Dependency effective names are translated to the
// nested form via packages.StoragePath, so every caller stays oblivious
// to flat-vs-nested layout details.
func packageVolumePath(repo, name, version, volName string) string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", PackagesVolumePrefix, repo, packages.StoragePath(name), version, volName)
}

// packagePrefix returns the installed/<repo>/<storagePath>/ trailing-slash
// prefix used for recursive listing and purging. Dependency names route
// through packages.StoragePath just like packageVolumePath.
func packagePrefix(repo, name string) string {
	return fmt.Sprintf("%s/%s/%s/", PackagesVolumePrefix, repo, packages.StoragePath(name))
}

type FilesystemName struct {
	Name      string `json:"name"`
	SortBy    string `json:"sort_by"`
	SortOrder string `json:"sort_order"`
	State     string `json:"state,omitempty"`
	Limit     int    `json:"limit"`
	Offset    int    `json:"offset"`
	Search    string `json:"search"`
}

type ModifyFilesystemRequest struct {
	Name       string             `json:"name"`
	Filesystem storage.Filesystem `json:"filesystem"`
}

// isGfehSubvolume reports whether a name addresses the object-storage root or
// a partition beneath it.
//
// The archive endpoints refuse these. GfehVolumePrefix is deliberately absent
// from resolveArchiveSubvolume's passthrough list below, and this is the check
// that makes that absence a clear refusal rather than a silent rewrite to
// user/gfeh/<...>. Unpacking a tar straight into a partition would put files on
// disk that gfeh's index has never seen — no owner, no ACL, no change sequence —
// which is precisely the "tar transport grows into an object API" that CLAUDE.md
// forbids. Seeding a partition is gfeh's job, through one of its own views.
func isGfehSubvolume(name string) bool {
	return name == GfehVolumePrefix || strings.HasPrefix(name, GfehVolumePrefix+"/")
}

// resolveArchiveSubvolume applies the user/ prefix to subvolume names that
// don't already carry an internal prefix (installed/, uninstalled/, etc.).
func resolveArchiveSubvolume(name string) string {
	for _, prefix := range []string{
		PackagesVolumePrefix + "/",
		UninstalledVolumePrefix + "/",
		ArchivesSubvolume + "/",
		PagesVolumePrefix + "/",
		VMImagesSubvolume + "/",
		UserVolumePrefix + "/",
	} {
		if strings.HasPrefix(name, prefix) {
			return name
		}
	}
	return fmt.Sprintf("%s/%s", UserVolumePrefix, name)
}

// --- Storage handlers ---

func (s *SystemControllerHandlers) createFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := storage.Filesystem{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if fs.State == "installed" || fs.State == "uninstalled" {
		return storage.ErrReservedFilesystem
	}

	if isReservedFilesystem(fs.Name) {
		return storage.ErrReservedFilesystem
	}

	fs.State = ""
	fs.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, fs.Name)

	if err := s.Controller.GetStorage().CreateFilesystem(fs); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) removeFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	if fs.State == "installed" || fs.State == "uninstalled" {
		return storage.ErrReservedFilesystem
	}

	if fs.Name == "" {
		return storage.ErrRootFilesystem
	}

	if isReservedFilesystem(fs.Name) {
		return storage.ErrReservedFilesystem
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(fmt.Sprintf("%s/%s", UserVolumePrefix, fs.Name)); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) modifyFilesystem(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ModifyFilesystemRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if req.Name == "" {
		return storage.ErrRootFilesystem
	}

	if isPackageVolume(req.Name) && req.Filesystem.Name != req.Name {
		return storage.ErrPackageVolumeRename
	}

	req.Filesystem.State = ""

	if !isPackageVolume(req.Name) {
		req.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, req.Name)
		req.Filesystem.Name = fmt.Sprintf("%s/%s", UserVolumePrefix, req.Filesystem.Name)
	}

	if err := s.Controller.GetStorage().ModifyFilesystem(req.Name, req.Filesystem); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

// --- Package volume types ---

type PackageVolume struct {
	Name         string `json:"name"`          // volume sub-path e.g. "1.0/data"
	InternalName string `json:"internal_name"` // full path e.g. "installed/repo-a/nginx/1.0/data"
	Repo         string `json:"repo"`          // repository name
	Quota        uint64 `json:"quota"`
	State        string `json:"state"` // "installed" or "uninstalled"
}

type PackageVolumeGroup struct {
	Package string `json:"package"` // display name, e.g. "nginx" or "repo-a/nginx" on collision
	// EffectiveName is the flat --dep-- form (e.g. "jitsi--dep--prosody")
	// the cascade-delete endpoint expects. Emitted so the UI can round-trip
	// the same identity it renders without re-encoding prettyName.
	EffectiveName string          `json:"effective_name"`
	Repo          string          `json:"repo"` // always present for API calls
	Volumes       []PackageVolume `json:"volumes"`
}

type PackageVolumesRequest struct {
	IncludeUninstalled bool `json:"include_uninstalled"`
}

type RemovePackageVolumeRequest struct {
	InternalName string `json:"internal_name"`
}

func (s *SystemControllerHandlers) listPackageVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageVolumesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	list, err := s.Controller.GetStorage().ListFilesystems("")
	if err != nil {
		return err
	}

	// Group volumes by (repo, pretty name). Pretty name is used as both
	// the grouping key and the display name so dep volumes surface as
	// "parent/key" instead of the ugly flat effective form. Different
	// packages at different effective positions cannot share a pretty
	// name within the same repo because StoragePath/PrettyName are
	// deterministic functions of the effective name.
	type groupKey struct {
		repo string
		name string // pretty display form
	}
	groups := map[groupKey][]PackageVolume{}
	// effectiveFor preserves the flat `--dep--` form alongside the pretty
	// display form so the cascade-delete API can be addressed without
	// re-encoding `/` → `--dep--` on the client.
	effectiveFor := map[groupKey]string{}
	nameToRepos := map[string]map[string]bool{} // pretty name → set of repos

	for _, f := range list {
		p, ok := parseInstalledPath(f.Name)
		if !ok {
			continue
		}
		// Skip any intermediate (bare package, dep-key, or version-only)
		// subvolume — listPackageVolumes only surfaces actual volume
		// leaves, which require both a version and a volume name.
		if p.version == "" || p.volName == "" {
			continue
		}
		if !req.IncludeUninstalled && p.state == "uninstalled" {
			continue
		}

		subPath := p.version + "/" + p.volName

		key := groupKey{repo: p.repo, name: p.prettyName}
		groups[key] = append(groups[key], PackageVolume{
			Name:         subPath,
			InternalName: f.Name,
			Repo:         p.repo,
			Quota:        f.Quota,
			State:        p.state,
		})
		effectiveFor[key] = p.effectiveName

		if nameToRepos[p.prettyName] == nil {
			nameToRepos[p.prettyName] = map[string]bool{}
		}
		nameToRepos[p.prettyName][p.repo] = true
	}

	// Build result, detecting cross-repo name collisions and
	// disambiguating by prefixing with the repo name when two repos
	// expose a package with the same pretty form.
	result := make([]PackageVolumeGroup, 0, len(groups))
	for key, vols := range groups {
		displayName := key.name
		if len(nameToRepos[key.name]) > 1 {
			displayName = key.repo + "/" + key.name
		}

		sort.Slice(vols, func(i, j int) bool {
			return vols[i].Name < vols[j].Name
		})

		result = append(result, PackageVolumeGroup{
			Package:       displayName,
			EffectiveName: effectiveFor[key],
			Repo:          key.repo,
			Volumes:       vols,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Package < result[j].Package
	})

	return c.JSON(200, result)
}

func (s *SystemControllerHandlers) removePackageVolume(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := RemovePackageVolumeRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	if !strings.HasPrefix(req.InternalName, PackagesVolumePrefix+"/") &&
		!strings.HasPrefix(req.InternalName, UninstalledVolumePrefix+"/") {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"error": "internal_name must start with installed/ or uninstalled/",
		})
	}

	if err := s.Controller.GetStorage().RemoveFilesystem(req.InternalName); err != nil {
		return err
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) listFilesystems(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	fs := FilesystemName{}

	if err := de.Decode(&fs); err != nil {
		return err
	}

	prefix := fs.Name
	if prefix != "" {
		prefix = fmt.Sprintf("%s/%s", UserVolumePrefix, prefix)
	}

	list, err := s.Controller.GetStorage().ListFilesystems(prefix)
	if err != nil {
		return err
	}

	filtered := make([]storage.Filesystem, 0, len(list))
	for _, f := range list {
		state, displayName := classifyFilesystem(f.Name)
		if state == "" {
			continue
		}
		if fs.State != "" && state != fs.State {
			continue
		}
		if state == "installed" || state == "uninstalled" {
			f.InternalName = f.Name
		}
		f.Name = displayName
		f.State = state
		filtered = append(filtered, f)
	}

	filtered = filterSearch(filtered, fs.Search)
	sortSlice(filtered, fs.SortBy, fs.SortOrder)

	return c.JSON(200, paginate(filtered, fs.Limit, fs.Offset))
}
