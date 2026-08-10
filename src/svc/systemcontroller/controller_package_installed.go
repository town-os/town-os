package systemcontroller

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
	"github.com/labstack/echo/v5"
)

func (s *SystemControllerHandlers) listInstalled(c *echo.Context) error {
	inst := s.Controller.GetInstaller()

	pkgs, err := inst.ListInstalled()
	if err != nil {
		return err
	}

	// Filter out sub-packages installed as dependencies of other packages:
	// they belong to their parent's lifecycle and the user does not manage
	// them directly.
	pkgs = filterDependencyInstalls(pkgs)

	p := readListParams(c)
	pkgs = filterSearch(pkgs, p.Search)
	sort.Strings(pkgs)
	if strings.EqualFold(p.SortOrder, "desc") {
		sort.Sort(sort.Reverse(sort.StringSlice(pkgs)))
	}

	return c.JSON(200, paginate(pkgs, p.Limit, p.Offset))
}

// filterDependencyInstalls drops any install record whose effective name is
// a dependency child (contains "--dep--"). Used by every user-facing
// installed-packages view so dependency sub-packages stay hidden while
// reconcile, uninstall, and the systemd unit list still see them via the
// underlying ListInstalled() call.
func filterDependencyInstalls(pkgs []string) []string {
	out := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		pi, err := packages.ParsePackageIdentity(pkg)
		if err != nil {
			out = append(out, pkg)
			continue
		}
		if packages.IsDependency(pi.Name) {
			continue
		}
		out = append(out, pkg)
	}
	return out
}

func (s *SystemControllerHandlers) getResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := GetResponsesRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		if errors.Is(err, packages.ErrNotInstalled) {
			return c.JSON(200, packages.Responses{})
		}
		return err
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) getLastResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	resp, err := inst.LoadLastResponses(req.Repo, req.Name)
	if err != nil {
		return c.JSON(200, packages.Responses{})
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) clearLastResponses(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	if err := inst.ClearLastResponses(req.Repo, req.Name); err != nil {
		return err
	}

	return c.JSON(200, struct{}{})
}

func (s *SystemControllerHandlers) getInstalledInfo(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageIdentityRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	responses, err := inst.GetResponses(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	rr := s.Controller.GetRepositoryRoot()

	ip, err := rr.LoadPackage(req.Repo, req.Name, req.Version)
	if err != nil {
		return err
	}

	// A package installed into a non-default network resolves under that
	// network's TLD (e.g. gitea.default.fart), not the global home zone, so the
	// notes/URL pages compiled here must use the network TLD rather than the
	// global dns_tld. An unknown/default network falls back to dns_tld via
	// networkTLD.
	network, _ := inst.LoadNetwork(req.Repo, req.Name)
	tld := s.networkTLD(c.Request().Context(), network)

	ctx := packages.CompileContext{
		ExternalHost: s.Controller.GetExternalIP(),
		InternalHost: s.Controller.GetInternalIP(),
		PackageDNS:   req.Name + "." + req.Repo + "." + tld,
	}
	notes, err := ip.CompileNotesWithContext(responses, ctx)
	if err != nil {
		return err
	}

	var noteTypes map[string]packages.NoteType
	for k, note := range ip.Notes {
		if note.Type != "" {
			if noteTypes == nil {
				noteTypes = make(map[string]packages.NoteType)
			}
			noteTypes[k] = note.Type
		}
	}

	// This route is requireAuth, because the dashboard renders every installed
	// service's notes for every account — that is what the notes are for. The
	// answers behind them are a different matter: a `type: secret` question is
	// answered with a generated credential and a `type: oauth` one with a
	// vendor token, so returning the full response map to any account with a
	// login would hand it every package's credentials.
	//
	// A non-admin therefore gets the notes and nothing else. The questions go
	// too: a question's `query` is harmless, but pairing it with a redacted
	// response map only advertises what is being withheld, and the only screen
	// that renders questions is the install dialog, which is admin-only.
	if !s.callerIsAdmin(c) {
		return c.JSON(200, InstalledInfoResponse{
			Notes:     redactSecretsInNotes(notes, ip.Questions, responses),
			NoteTypes: noteTypes,
		})
	}

	return c.JSON(200, InstalledInfoResponse{
		Questions: ip.Questions,
		Responses: responses,
		Notes:     notes,
		NoteTypes: noteTypes,
	})
}

// redactSecretsInNotes replaces any secret answer that a compiled note embedded
// with a mask.
//
// Dropping the response map is not enough on its own: a note is compiled from
// those same answers, so a package whose note reads
// `postgres://user:@dbpass@@host/db` publishes the credential in its text. The
// mask is applied by value, for exactly the question types whose answers are
// credentials — matching on the answer rather than on the note means a note
// that never quotes one is left completely intact, which is the common case and
// the one the dashboard depends on.
//
// Short answers are left alone: a two-character secret is not a credential
// anybody chose, and masking every occurrence of it would shred unrelated note
// text.
func redactSecretsInNotes(notes map[string]string, questions map[string]packages.Question, responses packages.Responses) map[string]string {
	const minMaskable = 6

	secrets := make([]string, 0, len(questions))
	for name, q := range questions {
		switch q.Type {
		case packages.Secret, packages.Oauth:
		default:
			continue
		}
		if v, ok := responses[name]; ok && len(v) >= minMaskable {
			secrets = append(secrets, v)
		}
	}
	if len(secrets) == 0 {
		return notes
	}

	out := make(map[string]string, len(notes))
	for k, v := range notes {
		for _, secret := range secrets {
			v = strings.ReplaceAll(v, secret, "********")
		}
		out[k] = v
	}
	return out
}

func (s *SystemControllerHandlers) listChildren(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := ListChildrenRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	inst := s.Controller.GetInstaller()
	children, err := inst.LoadChildren(req.Repo, req.Name)
	if err != nil {
		return err
	}
	if children == nil {
		children = []string{}
	}

	return c.JSON(200, children)
}

func (s *SystemControllerHandlers) listUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	resp := UninstalledVolumesResponse{}

	st := s.Controller.GetStorage()
	if st != nil {
		// Check uninstalled/<repo>/<name>/ for existing volume trees.
		uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, req.Name)
		filesystems, err := st.ListFilesystems(uninstPrefix)
		if err != nil {
			return err
		}

		if len(filesystems) > 0 {
			resp.HasUninstalledVolumes = true
			// Extract unique versions from uninstalled/<repo>/<name>/<version>/...
			versionSet := map[string]bool{}
			for _, fs := range filesystems {
				rest := strings.TrimPrefix(fs.Name, uninstPrefix)
				parts := strings.SplitN(rest, "/", 2)
				if len(parts) > 0 && parts[0] != "" {
					versionSet[parts[0]] = true
				}
			}
			for v := range versionSet {
				resp.UninstalledVersions = append(resp.UninstalledVersions, v)
			}
			sort.Strings(resp.UninstalledVersions)
		}

		// Extract installed versions from installed/<repo>/<name>/<version>/...
		instPrefix := packagePrefix(req.Repo, req.Name)
		instFS, err := st.ListFilesystems(instPrefix)
		if err != nil {
			return err
		}

		instVersionSet := map[string]bool{}
		for _, fs := range instFS {
			rest := strings.TrimPrefix(fs.Name, instPrefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) > 0 && parts[0] != "" {
				instVersionSet[parts[0]] = true
			}
		}
		for v := range instVersionSet {
			resp.InstalledVersions = append(resp.InstalledVersions, v)
		}
		sort.Strings(resp.InstalledVersions)
	}

	return c.JSON(200, resp)
}

func (s *SystemControllerHandlers) purgeUninstalledVolumes(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageNameRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	st := s.Controller.GetStorage()
	if st == nil {
		c.Response().WriteHeader(200)
		return nil
	}

	storagePath := packages.StoragePath(req.Name)
	uninstPrefix := fmt.Sprintf("%s/%s/%s/", UninstalledVolumePrefix, req.Repo, storagePath)
	if err := s.purgeVolumePrefix(st, uninstPrefix); err != nil {
		return err
	}

	uninstParent := fmt.Sprintf("%s/%s/%s", UninstalledVolumePrefix, req.Repo, storagePath)
	if err := st.RemoveFilesystem(uninstParent); err != nil {
		slog.Debug(fmt.Sprintf("purge uninstalled parent %s: %v", uninstParent, err))
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) disablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Repo, req.Name, true); err != nil {
		return err
	}

	// Look up the installed version directly instead of scanning the full list.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		version, ok, err := inst.GetInstalledVersion(req.Repo, req.Name)
		if err != nil {
			return err
		}
		if ok {
			unitName := systemd.UnitName(req.Repo, req.Name, version)
			if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Stop); err != nil {
				return err
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}

func (s *SystemControllerHandlers) enablePackage(c *echo.Context) error {
	de := json.NewDecoder(c.Request().Body)
	req := PackageToggleRequest{}

	if err := de.Decode(&req); err != nil {
		return err
	}

	unlock := s.lockPackage(req.Repo, req.Name)
	defer unlock()

	inst := s.Controller.GetInstaller()
	if err := inst.SetDisabled(req.Repo, req.Name, false); err != nil {
		return err
	}

	// Look up the installed version directly instead of scanning the full list.
	if sd := s.Controller.GetSystemdManager(); sd != nil {
		version, ok, err := inst.GetInstalledVersion(req.Repo, req.Name)
		if err != nil {
			return err
		}
		if ok {
			unitName := systemd.UnitName(req.Repo, req.Name, version)
			if err := sd.SetStatus(c.Request().Context(), unitName, systemd.Start); err != nil {
				return err
			}
		}
	}

	c.Response().WriteHeader(200)
	return nil
}
