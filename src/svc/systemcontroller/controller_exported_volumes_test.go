package systemcontroller

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/systemd"
)

// stubExportedResolver stands in for the InstallManager: it answers from maps
// so the exported-volume machinery can be exercised with no btrfs, no install
// records on disk, and no root.
type stubExportedResolver struct {
	installed []string
	// versions maps "<repo>/<pkg>" to the installed version. A missing key
	// means "not installed".
	versions map[string]string
	// responses maps "<repo>/<pkg>@<version>" to saved responses.
	responses map[string]packages.Responses
	listErr   error
}

func (s *stubExportedResolver) ListInstalled() ([]string, error) {
	return s.installed, s.listErr
}

func (s *stubExportedResolver) GetInstalledVersion(repo, name string) (string, bool, error) {
	v, ok := s.versions[repo+"/"+name]
	return v, ok, nil
}

func (s *stubExportedResolver) GetResponses(repo, name, version string) (packages.Responses, error) {
	if r, ok := s.responses[repo+"/"+name+"@"+version]; ok {
		return r, nil
	}
	return packages.Responses{}, nil
}

// stubLoader builds a compiledPackageLoader from a map of "<repo>/<pkg>@<ver>"
// to the volumes that package declares.
func stubLoader(vols map[string]map[string]packages.PackageVolume) compiledPackageLoader {
	return func(repo, name, version string, _ packages.Responses) (*packages.Package, error) {
		v, ok := vols[repo+"/"+name+"@"+version]
		if !ok {
			return nil, errors.New("not found")
		}
		return &packages.Package{Volumes: v}, nil
	}
}

func TestListExportedVolumesReturnsOnlyExported(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{
		installed: []string{"default/jellyfin@1.0", "default/radarr@2.0"},
	}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/jellyfin@1.0": {
			"media":  {Mountpoint: "/media", Quota: 500, Exported: true},
			"config": {Mountpoint: "/config"},
		},
		"default/radarr@2.0": {
			// Shareable is the dependency-tree flag and must NOT put a volume
			// in the box-wide picker.
			"movies": {Mountpoint: "/movies", Shareable: true},
		},
	})

	got, err := listExportedVolumes(res, load)
	if err != nil {
		t.Fatalf("listExportedVolumes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("exported volumes = %#v, want exactly the jellyfin media volume", got)
	}
	if got[0].Reference != "default/jellyfin/media" {
		t.Fatalf("reference = %q", got[0].Reference)
	}
	if got[0].Mountpoint != "/media" || got[0].Quota != 500 {
		t.Fatalf("volume detail lost: %+v", got[0])
	}
}

// TestListExportedVolumesSkipsDependencies: a dependency's storage is nested
// under its parent and it is internal to that parent by design. Offering one
// in the picker would hand every package a handle on somebody's sub-package.
func TestListExportedVolumesSkipsDependencies(t *testing.T) {
	t.Parallel()

	depName := "media" + packages.DependencySeparator + "radarr"
	res := &stubExportedResolver{installed: []string{"default/" + depName + "@1.0"}}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/" + depName + "@1.0": {"movies": {Mountpoint: "/movies", Exported: true}},
	})

	got, err := listExportedVolumes(res, load)
	if err != nil {
		t.Fatalf("listExportedVolumes: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a dependency's exported volume reached the picker: %#v", got)
	}
}

// TestListExportedVolumesSurvivesOneBadPackage: one package whose YAML has
// since vanished from its repository must not empty the picker for every
// other package on the box.
func TestListExportedVolumesSurvivesOneBadPackage(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{installed: []string{"default/broken@1.0", "default/jellyfin@1.0"}}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/jellyfin@1.0": {"media": {Mountpoint: "/media", Exported: true}},
	})

	got, err := listExportedVolumes(res, load)
	if err != nil {
		t.Fatalf("listExportedVolumes: %v", err)
	}
	if len(got) != 1 || got[0].Reference != "default/jellyfin/media" {
		t.Fatalf("a broken sibling took out the listing: %#v", got)
	}
}

func TestListExportedVolumesIsSorted(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{installed: []string{"default/zeta@1.0", "default/alpha@1.0"}}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/zeta@1.0":  {"vol": {Mountpoint: "/z", Exported: true}},
		"default/alpha@1.0": {"vol": {Mountpoint: "/a", Exported: true}},
	})

	got, err := listExportedVolumes(res, load)
	if err != nil {
		t.Fatalf("listExportedVolumes: %v", err)
	}
	if len(got) != 2 || got[0].Reference != "default/alpha/vol" {
		t.Fatalf("listing is not sorted: %#v", got)
	}
}

func TestListExportedVolumesNilInputs(t *testing.T) {
	t.Parallel()

	if got, err := listExportedVolumes(nil, nil); err != nil || got != nil {
		t.Fatalf("listExportedVolumes(nil, nil) = (%#v, %v)", got, err)
	}
}

// mediaStackResolver is the fixture the attach tests share: Jellyfin installed
// at 1.0 with an exported media volume.
func mediaStackResolver() (*stubExportedResolver, compiledPackageLoader) {
	res := &stubExportedResolver{
		installed: []string{"default/jellyfin@1.0"},
		versions:  map[string]string{"default/jellyfin": "1.0"},
	}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/jellyfin@1.0": {
			"media":  {Mountpoint: "/media", Exported: true},
			"config": {Mountpoint: "/config"},
		},
	})
	return res, load
}

func TestResolveAttachMountsBuildsBindMount(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	uid := uint32(1000)
	attach := map[string]packages.InputPackageAttach{
		"library": {Volume: "default/jellyfin/media", Subpath: "movies", Path: "/library/movies", UID: &uid, GID: &uid},
	}

	mounts, err := resolveAttachMounts("/btrfs", attach, res, load, true)
	if err != nil {
		t.Fatalf("resolveAttachMounts: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v, want 1", mounts)
	}
	want := filepath.Join("/btrfs", "installed", "default", "jellyfin", "1.0", "media", "movies")
	if mounts[0].HostPath != want {
		t.Fatalf("HostPath = %q, want %q", mounts[0].HostPath, want)
	}
	if mounts[0].ContainerPath != "/library/movies" {
		t.Fatalf("ContainerPath = %q", mounts[0].ContainerPath)
	}
	// Writable is the default: an attachment exists so the consumer can file
	// content into somebody else's library.
	if mounts[0].Options != "rw,z" {
		t.Fatalf("Options = %q, want rw,z", mounts[0].Options)
	}
	if mounts[0].UID == nil || *mounts[0].UID != 1000 {
		t.Fatalf("UID not carried onto the mount: %+v", mounts[0])
	}
}

func TestResolveAttachMountsReadOnly(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	ro := true
	mounts, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"library": {Volume: "default/jellyfin/media", Path: "/library", ReadOnly: &ro},
	}, res, load, true)
	if err != nil {
		t.Fatalf("resolveAttachMounts: %v", err)
	}
	if mounts[0].Options != "ro" {
		t.Fatalf("Options = %q, want ro", mounts[0].Options)
	}
}

// TestResolveAttachMountsOmitsSubpathWhenEmpty: no subpath means the volume
// root, and the path must not pick up a trailing separator that would change
// what podman binds.
func TestResolveAttachMountsOmitsSubpathWhenEmpty(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	mounts, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"library": {Volume: "default/jellyfin/media", Path: "/library"},
	}, res, load, true)
	if err != nil {
		t.Fatalf("resolveAttachMounts: %v", err)
	}
	want := filepath.Join("/btrfs", "installed", "default", "jellyfin", "1.0", "media")
	if mounts[0].HostPath != want {
		t.Fatalf("HostPath = %q, want %q", mounts[0].HostPath, want)
	}
}

// TestResolveAttachMountsIsSortedByName keeps the generated unit stable. Map
// iteration order is random, and an unstable -v ordering would rewrite the
// unit text on every reconcile and restart the service for no reason.
func TestResolveAttachMountsIsSortedByName(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	attach := map[string]packages.InputPackageAttach{
		"zeta":  {Volume: "default/jellyfin/media", Subpath: "z", Path: "/z"},
		"alpha": {Volume: "default/jellyfin/media", Subpath: "a", Path: "/a"},
		"mid":   {Volume: "default/jellyfin/media", Subpath: "m", Path: "/m"},
	}
	for range 8 {
		mounts, err := resolveAttachMounts("/btrfs", attach, res, load, true)
		if err != nil {
			t.Fatalf("resolveAttachMounts: %v", err)
		}
		got := []string{mounts[0].ContainerPath, mounts[1].ContainerPath, mounts[2].ContainerPath}
		if got[0] != "/a" || got[1] != "/m" || got[2] != "/z" {
			t.Fatalf("mount order = %v, want sorted by attach name", got)
		}
	}
}

// TestResolveAttachMountsFollowsProducerVersion is why the reference carries
// no version: the consumer attaches to "the library Jellyfin is serving", and
// an upgrade moves that library to a new subvolume path. A pinned version
// would leave the consumer bind-mounted at a path that no longer exists.
func TestResolveAttachMountsFollowsProducerVersion(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{versions: map[string]string{"default/jellyfin": "2.0"}}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/jellyfin@2.0": {"media": {Mountpoint: "/media", Exported: true}},
	})

	mounts, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"library": {Volume: "default/jellyfin/media", Path: "/library"},
	}, res, load, true)
	if err != nil {
		t.Fatalf("resolveAttachMounts: %v", err)
	}
	if !strings.Contains(mounts[0].HostPath, filepath.Join("jellyfin", "2.0", "media")) {
		t.Fatalf("HostPath did not follow the producer to 2.0: %q", mounts[0].HostPath)
	}
}

// TestResolveAttachMountsRejectsNonExported: the export flag is re-read from
// the producer as installed NOW. A version that withdrew the export must stop
// being mountable, or the flag is only ever checked on the first install.
func TestResolveAttachMountsRejectsNonExported(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{versions: map[string]string{"default/jellyfin": "1.0"}}
	load := stubLoader(map[string]map[string]packages.PackageVolume{
		"default/jellyfin@1.0": {"config": {Mountpoint: "/config"}},
	})

	_, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"cfg": {Volume: "default/jellyfin/config", Path: "/library"},
	}, res, load, true)
	if !errors.Is(err, errExportedVolumeUnavailable) {
		t.Fatalf("err = %v, want errExportedVolumeUnavailable", err)
	}
}

func TestResolveAttachMountsRejectsUnknownVolume(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	_, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"lib": {Volume: "default/jellyfin/nosuch", Path: "/library"},
	}, res, load, true)
	if !errors.Is(err, errExportedVolumeUnavailable) {
		t.Fatalf("err = %v, want errExportedVolumeUnavailable", err)
	}
}

// TestResolveAttachMountsStrictFailsOnMissingProducer covers the install path:
// the operator picked this moments ago, so an unresolvable reference is a
// failure they should see rather than a container that comes up without the
// library it was installed to fill.
func TestResolveAttachMountsStrictFailsOnMissingProducer(t *testing.T) {
	t.Parallel()

	res := &stubExportedResolver{versions: map[string]string{}}
	load := stubLoader(nil)

	_, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"lib": {Volume: "default/jellyfin/media", Path: "/library"},
	}, res, load, true)
	if !errors.Is(err, errExportedVolumeUnavailable) {
		t.Fatalf("err = %v, want errExportedVolumeUnavailable", err)
	}
}

// TestResolveAttachMountsLenientSkipsMissingProducer covers the reconcile
// path: a producer uninstalled months ago must not stop this package from
// coming back up after a reboot.
func TestResolveAttachMountsLenientSkipsMissingProducer(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	mounts, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"good": {Volume: "default/jellyfin/media", Path: "/good"},
		"gone": {Volume: "default/plex/data", Path: "/gone"},
	}, res, load, false)
	if err != nil {
		t.Fatalf("lenient resolve returned an error: %v", err)
	}
	if len(mounts) != 1 || mounts[0].ContainerPath != "/good" {
		t.Fatalf("mounts = %#v, want only the resolvable one", mounts)
	}
}

// TestResolveAttachMountsRejectsTraversingReference is the security case at
// the resolver: even if a malformed reference got past the question type, the
// mount source must never escape the installed tree.
func TestResolveAttachMountsRejectsTraversingReference(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	for _, bad := range []string{"../../etc/passwd", "default/../gfeh/home", "default/jellyfin/../../.."} {
		_, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
			"lib": {Volume: bad, Path: "/library"},
		}, res, load, true)
		if err == nil {
			t.Errorf("resolveAttachMounts accepted reference %q", bad)
		}
	}
}

// TestResolveAttachMountsRejectsTraversingSubpath: the subpath is joined onto
// the producer's subvolume path, so a climbing value would mount a sibling
// package's storage instead.
func TestResolveAttachMountsRejectsTraversingSubpath(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	_, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"lib": {Volume: "default/jellyfin/media", Subpath: "../../../gfeh", Path: "/library"},
	}, res, load, true)
	if err == nil {
		t.Fatal("resolveAttachMounts accepted a traversing subpath")
	}
}

// TestResolveAttachMountsSkipsUnselected guards the hand-built-map case:
// compileAttach already drops unselected entries, but nothing should be able
// to produce a mount whose source is the btrfs root.
func TestResolveAttachMountsSkipsUnselected(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	mounts, err := resolveAttachMounts("/btrfs", map[string]packages.InputPackageAttach{
		"lib": {Volume: "   ", Path: "/library"},
	}, res, load, true)
	if err != nil {
		t.Fatalf("resolveAttachMounts: %v", err)
	}
	if len(mounts) != 0 {
		t.Fatalf("an unselected attach produced a mount: %#v", mounts)
	}
}

func TestResolveAttachMountsEmptyInput(t *testing.T) {
	t.Parallel()

	res, load := mediaStackResolver()
	if mounts, err := resolveAttachMounts("/btrfs", nil, res, load, true); err != nil || mounts != nil {
		t.Fatalf("resolveAttachMounts(nil) = (%#v, %v)", mounts, err)
	}
}

// TestResolveAttachMountsNilResolver: strict says so loudly, lenient stays
// quiet. A nil interface here means the controller has no installer at all,
// which is a test-shaped configuration rather than a running box.
func TestResolveAttachMountsNilResolver(t *testing.T) {
	t.Parallel()

	attach := map[string]packages.InputPackageAttach{
		"lib": {Volume: "default/jellyfin/media", Path: "/library"},
	}
	if _, err := resolveAttachMounts("/btrfs", attach, nil, nil, true); err == nil {
		t.Error("strict resolve accepted a nil resolver")
	}
	if mounts, err := resolveAttachMounts("/btrfs", attach, nil, nil, false); err != nil || mounts != nil {
		t.Errorf("lenient resolve with nil resolver = (%#v, %v)", mounts, err)
	}
}

// TestApplyAttachMountsRegistersMkdir is the guard on a failure that is worse
// than a missing mount. An attach may name a subpath inside the producer's
// volume, and nothing has ever created that directory; the generator emits
// mkdir lines before the host-mount chowns, and the chown carries no `-`
// prefix, so an absent directory fails ExecStartPre and the service never
// starts at all.
func TestApplyAttachMountsRegistersMkdir(t *testing.T) {
	t.Parallel()

	cfg := systemd.PackageUnitConfig{}
	uid := uint32(1000)
	applyAttachMounts(&cfg, []systemd.HostVolumeMount{
		{HostPath: "/btrfs/installed/local/jf/1.0/media/movies", ContainerPath: "/library/movies", Options: "rw,z", UID: &uid, GID: &uid},
	})

	if len(cfg.HostVolumeMounts) != 1 {
		t.Fatalf("HostVolumeMounts = %#v", cfg.HostVolumeMounts)
	}
	if len(cfg.MkdirPaths) != 1 || cfg.MkdirPaths[0] != "/btrfs/installed/local/jf/1.0/media/movies" {
		t.Fatalf("MkdirPaths = %#v, want the attach host path", cfg.MkdirPaths)
	}
}

// TestApplyAttachMountsGeneratesMkdirBeforeChown pins the ordering in the
// generated unit itself, not just the config: a chown that runs before its
// mkdir fails just as hard as one with no mkdir at all.
func TestApplyAttachMountsGeneratesMkdirBeforeChown(t *testing.T) {
	t.Parallel()

	uid := uint32(1000)
	hostPath := "/btrfs/installed/local/jf/1.0/media/movies"
	cfg := systemd.PackageUnitConfig{
		RepoName: "local", PkgName: "radarr", Version: "1.0",
		Image: "alpine:3.20", BtrfsBase: "/btrfs",
		Runtime: packages.RuntimeContainer,
	}
	applyAttachMounts(&cfg, []systemd.HostVolumeMount{
		{HostPath: hostPath, ContainerPath: "/library/movies", Options: "rw,z", UID: &uid, GID: &uid},
	})

	body := systemd.GeneratePackageUnits(cfg).Service.Content
	mkdirAt := strings.Index(body, "ExecStartPre=/bin/mkdir -p "+hostPath)
	chownAt := strings.Index(body, "ExecStartPre=/bin/chown 1000:1000 "+hostPath)
	if mkdirAt < 0 {
		t.Fatalf("unit has no mkdir for the attach path; body:\n%s", body)
	}
	if chownAt < 0 {
		t.Fatalf("unit has no chown for the attach path; body:\n%s", body)
	}
	if mkdirAt > chownAt {
		t.Fatalf("chown is emitted before its mkdir; body:\n%s", body)
	}
	if !strings.Contains(body, "-v "+hostPath+":/library/movies:rw,z") {
		t.Fatalf("unit has no -v flag for the attach; body:\n%s", body)
	}
}

func TestApplyAttachMountsEmptyIsNoop(t *testing.T) {
	t.Parallel()

	cfg := systemd.PackageUnitConfig{}
	applyAttachMounts(&cfg, nil)
	if cfg.HostVolumeMounts != nil || cfg.MkdirPaths != nil {
		t.Fatalf("applyAttachMounts(nil) touched the config: %+v", cfg)
	}
}

func TestReconcileAttachResolverNilInstaller(t *testing.T) {
	t.Parallel()

	// A nil *InstallManager wrapped in an interface is not nil, and every
	// caller's nil check would pass while the first method call panicked.
	if got := reconcileAttachResolver(nil); got != nil {
		t.Fatalf("reconcileAttachResolver(nil) = %#v, want nil", got)
	}
}

func TestNewCompiledPackageLoaderNilRoot(t *testing.T) {
	t.Parallel()

	if got := newCompiledPackageLoader(nil); got != nil {
		t.Fatal("newCompiledPackageLoader(nil) returned a non-nil loader")
	}
}
