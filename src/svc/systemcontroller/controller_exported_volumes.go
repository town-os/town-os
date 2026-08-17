package systemcontroller

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// errExportedVolumeUnavailable reports that an attach reference names
// something this box cannot currently offer — the producer is not installed,
// no longer declares the volume, or has stopped exporting it.
var errExportedVolumeUnavailable = errors.New("exported volume unavailable")

// exportedVolumeResolver is the slice of the installer and repository root the
// exported-volume machinery needs. Narrowing it to these three calls is what
// lets both the picker and the mount resolver be tested against a stub instead
// of a populated btrfs tree.
type exportedVolumeResolver interface {
	ListInstalled() ([]string, error)
	GetInstalledVersion(repoName, pkgName string) (string, bool, error)
	GetResponses(repoName, pkgName, version string) (packages.Responses, error)
}

// compiledPackageLoader resolves a repo/name/version to a compiled package.
// The producer's volume list is only accurate after compilation: a quota or a
// mountpoint is routinely a `@marker@` in the YAML, and `exported` is read off
// the compiled volume so a future conditional export would be honored here too.
type compiledPackageLoader func(repo, name, version string, responses packages.Responses) (*packages.Package, error)

// newCompiledPackageLoader builds the loader used by both the picker and the
// mount resolver, on the install path and on the reconcile path alike.
//
// It tries the repository copy first and falls back to the installed hard
// link, mirroring reconcile's own loading. The repository file is the live
// definition, but a package outlives the repository it came from — a repo
// removed, a version withdrawn upstream — and the installed copy is what that
// container is actually running. A producer reachable only as an installed
// hard link must stay attachable, or removing a repository would silently
// detach every consumer on the box at the next boot.
//
// Returns nil when there is no repository root, which the callers treat as
// "cannot resolve" rather than dereferencing.
func newCompiledPackageLoader(rr *packages.RepositoryRoot) compiledPackageLoader {
	if rr == nil {
		return nil
	}
	return func(repo, name, version string, responses packages.Responses) (*packages.Package, error) {
		ip, err := rr.LoadPackage(repo, name, version)
		if err != nil {
			ip, err = rr.LoadInstalledPackage(repo, name, version)
			if err != nil {
				return nil, fmt.Errorf("load %s/%s@%s: %w", repo, name, version, err)
			}
		}
		compiled, cerr := ip.Compile(responses)
		if cerr != nil {
			return nil, fmt.Errorf("compile %s/%s@%s: %w", repo, name, version, cerr)
		}
		return compiled, nil
	}
}

// reconcileAttachResolver adapts a packages.Installer to the narrow interface
// the attach resolver needs, returning nil for a nil installer so the caller's
// nil check is not defeated by a non-nil interface holding a nil pointer.
func reconcileAttachResolver(inst packages.Installer) exportedVolumeResolver {
	if inst == nil {
		return nil
	}
	return inst
}

// listExportedVolumes returns every volume that an installed, non-dependency
// package has marked `exported: true`, sorted for a stable picker.
//
// Failures for a single package are skipped rather than fatal: one package
// whose YAML has since disappeared from its repository must not empty the
// picker for every other package on the box.
func listExportedVolumes(res exportedVolumeResolver, load compiledPackageLoader) ([]packages.ExportedVolume, error) {
	if res == nil || load == nil {
		return nil, nil
	}
	installed, err := res.ListInstalled()
	if err != nil {
		return nil, fmt.Errorf("list installed: %w", err)
	}

	out := []packages.ExportedVolume{}
	for _, entry := range installed {
		pi, perr := packages.ParsePackageIdentity(entry)
		if perr != nil {
			continue
		}
		// A dependency belongs to exactly one parent and its storage is nested
		// under it. Offering one to the whole box would hand every package a
		// handle on somebody else's private sub-package.
		if packages.IsDependency(pi.Name) {
			continue
		}
		responses, rerr := res.GetResponses(pi.Repo, pi.Name, pi.Version)
		if rerr != nil {
			slog.Debug("exported volumes: responses", "package", entry, "error", rerr)
			continue
		}
		compiled, cerr := load(pi.Repo, pi.Name, pi.Version, responses)
		if cerr != nil {
			slog.Debug("exported volumes: compile", "package", entry, "error", cerr)
			continue
		}
		for volName, vol := range compiled.Volumes {
			if !vol.Exported {
				continue
			}
			ref := packages.ExportedVolumeRef{Repo: pi.Repo, Package: pi.Name, Volume: volName}
			out = append(out, packages.ExportedVolume{
				Reference:  ref.String(),
				Repo:       pi.Repo,
				Package:    pi.Name,
				Version:    pi.Version,
				Volume:     volName,
				Mountpoint: vol.Mountpoint,
				Quota:      vol.Quota,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Reference < out[j].Reference })
	return out, nil
}

// resolveAttachMounts turns a compiled package's `attach:` map into host bind
// mounts. Entries are sorted by attach name so the generated unit is stable
// across runs — an unordered map would churn the unit text on every reconcile
// and restart the service for no reason.
//
// The version is resolved here, from the install record, rather than being
// carried in the reference: see ExportedVolumeRef on why an attachment follows
// its producer across upgrades instead of pinning to the version that was
// current when somebody answered the question.
//
// strict decides what an unresolvable reference means. At install time it is
// an error the operator should see immediately — they just picked the thing,
// and a silent skip would produce a container running without the library it
// was installed to fill. At reconcile it is logged and skipped, because the
// alternative is refusing to bring a package back up after a reboot over a
// producer that was uninstalled months ago.
func resolveAttachMounts(
	btrfsBase string,
	attach map[string]packages.InputPackageAttach,
	res exportedVolumeResolver,
	load compiledPackageLoader,
	strict bool,
) ([]systemd.HostVolumeMount, error) {
	if len(attach) == 0 {
		return nil, nil
	}
	if res == nil || load == nil {
		if strict {
			return nil, fmt.Errorf("%w: no installer available to resolve attachments", errExportedVolumeUnavailable)
		}
		return nil, nil
	}

	names := make([]string, 0, len(attach))
	for name := range attach {
		names = append(names, name)
	}
	sort.Strings(names)

	mounts := make([]systemd.HostVolumeMount, 0, len(names))
	for _, name := range names {
		att := attach[name]
		// compileAttach already drops unselected entries, but a caller that
		// hands us a hand-built map should not be able to produce a mount
		// whose source is the btrfs root.
		if !att.Selected() {
			continue
		}
		mount, err := resolveOneAttach(btrfsBase, name, att, res, load)
		if err != nil {
			if strict {
				return nil, err
			}
			slog.Error("attach skipped", "attach", name, "volume", att.Volume, "error", err)
			continue
		}
		mounts = append(mounts, mount)
	}
	if len(mounts) == 0 {
		return nil, nil
	}
	return mounts, nil
}

// applyAttachMounts adds resolved attachments to a unit config.
//
// Every attach host path is ALSO registered as an mkdir target, which the
// mounts built by expose:/consume: do not need. Those name a producer volume's
// root, which its own install created as a btrfs subvolume; an attach may name
// a `subpath:` inside one, and nothing has ever created that directory. It
// matters more than a missing directory usually would, because the generator
// emits the mkdir lines before the host-mount chowns and the chown is NOT
// prefixed with `-`: an absent directory does not merely skip the mount, it
// fails ExecStartPre and the service never starts.
func applyAttachMounts(cfg *systemd.PackageUnitConfig, mounts []systemd.HostVolumeMount) {
	if len(mounts) == 0 {
		return
	}
	cfg.HostVolumeMounts = append(cfg.HostVolumeMounts, mounts...)
	for _, m := range mounts {
		cfg.MkdirPaths = append(cfg.MkdirPaths, m.HostPath)
	}
}

// resolveOneAttach resolves a single attach entry against what is installed.
func resolveOneAttach(
	btrfsBase, name string,
	att packages.InputPackageAttach,
	res exportedVolumeResolver,
	load compiledPackageLoader,
) (systemd.HostVolumeMount, error) {
	ref, err := packages.ParseExportedVolumeRef(att.Volume)
	if err != nil {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: %w", name, err)
	}
	if err := packages.ValidateAttachSubpath(att.Subpath); err != nil {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: %w", name, err)
	}

	version, found, err := res.GetInstalledVersion(ref.Repo, ref.Package)
	if err != nil {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: installed version of %s: %w", name, ref, err)
	}
	if !found {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: %w: %s is not installed", name, errExportedVolumeUnavailable, ref)
	}

	responses, err := res.GetResponses(ref.Repo, ref.Package, version)
	if err != nil {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: responses for %s: %w", name, ref, err)
	}
	producer, err := load(ref.Repo, ref.Package, version, responses)
	if err != nil {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: load %s: %w", name, ref, err)
	}

	vol, ok := producer.Volumes[ref.Volume]
	if !ok {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: %w: %s declares no volume %q", name, errExportedVolumeUnavailable, ref, ref.Volume)
	}
	// The export flag is re-read from the producer as it is installed NOW, not
	// as it was when the operator picked it. A package version that withdrew
	// the export must stop being mountable, or the flag would only ever be a
	// check on the first install.
	if !vol.Exported {
		return systemd.HostVolumeMount{}, fmt.Errorf("attach %q: %w: volume %q on %s is not exported", name, errExportedVolumeUnavailable, ref.Volume, ref)
	}

	hostPath := filepath.Join(btrfsBase, packageVolumePath(ref.Repo, ref.Package, version, ref.Volume))
	if att.Subpath != "" {
		hostPath = filepath.Join(hostPath, att.Subpath)
	}

	return systemd.HostVolumeMount{
		HostPath:      hostPath,
		ContainerPath: att.Path,
		Options:       sharedMountOptions(att.AttachReadOnly()),
		UID:           att.UID,
		GID:           att.GID,
	}, nil
}
