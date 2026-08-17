package packages

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidExportedVolumeRef is returned when a `shared_volume` answer or
	// an `attach.volume` value is not a well-formed `<repo>/<package>/<volume>`
	// reference.
	ErrInvalidExportedVolumeRef = errors.New("invalid exported volume reference")

	// ErrInvalidAttach is returned for a malformed `attach:` entry.
	ErrInvalidAttach = errors.New("invalid attach entry")
)

// ExportedVolumeRefSeparator joins the three segments of a reference.
const ExportedVolumeRefSeparator = "/"

// ExportedVolumeRef names one exported volume belonging to one installed
// package.
//
// The reference deliberately carries NO version. A consumer attaches to "the
// library Jellyfin is serving", not to "the library the 1.0 install of Jellyfin
// was serving": package volumes live at installed/<repo>/<name>/<version>/<vol>,
// so pinning the version here would silently detach the consumer the next time
// the producer upgraded, leaving a container bind-mounted at a path that no
// longer exists. The version is resolved from the install record every time the
// mount is built, and reconcile rebuilds every unit on boot, so an upgrade
// re-points the consumer without anyone re-answering anything.
type ExportedVolumeRef struct {
	Repo    string
	Package string
	Volume  string
}

// String renders the reference in its canonical wire form.
func (r ExportedVolumeRef) String() string {
	return strings.Join([]string{r.Repo, r.Package, r.Volume}, ExportedVolumeRefSeparator)
}

// ParseExportedVolumeRef parses `<repo>/<package>/<volume>`.
//
// Every segment is validated rather than merely split on. The parsed value is
// joined onto the btrfs base to build a bind-mount source, so a segment
// carrying `..`, a slash, or an empty string would address a subvolume outside
// the installed tree — and the operator picking the value is any authenticated
// account, not necessarily an admin. `volumeNameRegexp` admits no separator and
// no dot-only component, which is what closes that.
func ParseExportedVolumeRef(s string) (ExportedVolumeRef, error) {
	parts := strings.Split(s, ExportedVolumeRefSeparator)
	if len(parts) != 3 {
		return ExportedVolumeRef{}, fmt.Errorf("%w: %q (want <repo>/<package>/<volume>)", ErrInvalidExportedVolumeRef, s)
	}
	for idx, p := range parts {
		if err := ValidateVolumeName(p); err != nil {
			return ExportedVolumeRef{}, fmt.Errorf("%w: %q: segment %d: %w", ErrInvalidExportedVolumeRef, s, idx, err)
		}
	}
	// A dependency's effective name carries the DependencySeparator, and its
	// storage is nested under its parent rather than sitting at
	// installed/<repo>/<name>/. Deps are internal to their parent by design;
	// exporting one would hand the whole box a handle on somebody's private
	// sub-package. Rejecting the name here keeps that out of the reference
	// grammar itself rather than relying on every resolver to remember.
	if err := ValidatePackageName(parts[1]); err != nil {
		return ExportedVolumeRef{}, fmt.Errorf("%w: %q: %w", ErrInvalidExportedVolumeRef, s, err)
	}
	return ExportedVolumeRef{Repo: parts[0], Package: parts[1], Volume: parts[2]}, nil
}

// InputPackageAttach is one entry in a package's top-level `attach:` map: a
// request to bind-mount an exported volume belonging to some other installed
// package into this one's container.
//
// It is deliberately NOT a `volumes:` entry. A volumes entry provisions a
// btrfs subvolume and is followed everywhere by quota accounting, archive
// seeding, uninstall renames, and purge; an attach owns none of that. It
// resolves to a podman `-v` flag and nothing else, so the producer stays the
// only package that creates, sizes, or deletes the storage.
type InputPackageAttach struct {
	// Volume is an `<repo>/<package>/<volume>` reference, in practice written
	// as a `@question@` marker backed by a `shared_volume` question so the
	// operator picks from what is actually installed. An empty value after
	// substitution means "not selected" and the whole entry is skipped, which
	// is what makes an optional attachment expressible.
	Volume string `yaml:"volume" json:"volume"`

	// Subpath is an optional relative directory INSIDE the exported volume.
	// A media server exports one library; the packages filing into it want
	// their own corner of it (`movies`, `tv`) rather than its root. Created on
	// demand by the unit's ExecStartPre mkdir.
	Subpath string `yaml:"subpath,omitempty" json:"subpath,omitempty"`

	// Path is the absolute in-container mountpoint.
	Path string `yaml:"path" json:"path"`

	// ReadOnly defaults to FALSE, the opposite of `expose:`. An attachment
	// exists so the consumer can file content into somebody else's library;
	// a read-only default would make the common case the one you have to
	// remember to turn off.
	ReadOnly *bool `yaml:"readonly,omitempty" json:"readonly,omitempty"`

	// UID/GID chown the mounted directory (non-recursive) before the container
	// starts, exactly as on a regular volume. Bind mounts pass host ownership
	// straight through, so a consumer running as a different uid than the
	// producer gets EACCES without this.
	UID *uint32 `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID *uint32 `yaml:"gid,omitempty" json:"gid,omitempty"`
}

// AttachReadOnly returns the effective readonly flag, defaulting to false.
func (a InputPackageAttach) AttachReadOnly() bool {
	if a.ReadOnly == nil {
		return false
	}
	return *a.ReadOnly
}

// Selected reports whether the entry names a volume at all. An attach backed by
// an optional `shared_volume` question that the operator left blank compiles to
// an empty Volume and is skipped rather than failing the install.
func (a InputPackageAttach) Selected() bool {
	return strings.TrimSpace(a.Volume) != ""
}

// ExportedVolume describes one exported volume for the picker behind a
// `shared_volume` question. Mountpoint and Quota are the producer's, resolved
// against its saved responses, so the operator sees what the volume actually
// is rather than the uncompiled `@marker@` its YAML holds.
type ExportedVolume struct {
	Reference  string `json:"reference"`
	Repo       string `json:"repo"`
	Package    string `json:"package"`
	Version    string `json:"version"`
	Volume     string `json:"volume"`
	Mountpoint string `json:"mountpoint"`
	Quota      uint64 `json:"quota,omitempty"`
}

// ValidateAttach checks one `attach:` entry from package YAML.
//
// The Volume field is checked for shape only when it carries no template
// marker: the common form is a bare `@question@` that has no reference in it
// until an operator answers, and the real check happens when the mount is
// resolved against what is installed.
func ValidateAttach(name string, a InputPackageAttach) error {
	if err := ValidateVolumeName(name); err != nil {
		return fmt.Errorf("%w: name %q: %w", ErrInvalidAttach, name, err)
	}
	if a.Volume == "" {
		return fmt.Errorf("%w: %q: volume must not be empty", ErrInvalidAttach, name)
	}
	if !strings.Contains(a.Volume, "@") {
		if _, err := ParseExportedVolumeRef(a.Volume); err != nil {
			return fmt.Errorf("%w: %q: %w", ErrInvalidAttach, name, err)
		}
	}
	if err := validateSharedMountPath(a.Path); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrInvalidAttach, name, err)
	}
	if err := ValidateAttachSubpath(a.Subpath); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrInvalidAttach, name, err)
	}
	return nil
}

// ValidateAttachSubpath checks the optional in-volume directory. Empty is
// valid and means the volume root. Anything else must be a relative path with
// no traversal and no absolute anchor, because it is joined onto the
// producer's subvolume path to form the bind-mount source — an absolute or
// climbing value would mount something else entirely.
func ValidateAttachSubpath(subpath string) error {
	if subpath == "" {
		return nil
	}
	if strings.HasPrefix(subpath, "/") {
		return fmt.Errorf("%w: subpath %q must be relative", ErrInvalidAttach, subpath)
	}
	for seg := range strings.SplitSeq(subpath, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: subpath %q has an empty or traversing component", ErrInvalidAttach, subpath)
		}
	}
	return nil
}
