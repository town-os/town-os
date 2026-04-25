// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"errors"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
)

// TestResolveExposeMountsNoMountsEmits regression-tests that a parent
// with deps but no expose: blocks produces zero HostVolumeMounts —
// existing dep-using packages (mattermost, immich, jitsi, matrix) must
// keep generating byte-identical units after this feature lands.
func TestResolveExposeMountsNoEntries(t *testing.T) {
	parentDeps := map[string]packages.InputPackageDependency{
		"db": {Package: "postgres"}, // no expose: at all
	}
	depRecs := map[string]packages.DependencyRecord{
		"db": {EffectiveName: "app--dep--db", Package: "postgres", Repo: "default", Version: "15.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		t.Fatal("loader should not be called when no expose entries exist")
		return nil, errors.New("unreachable")
	}
	mounts, err := resolveExposeMounts("/btrfs", parentDeps, depRecs, loader)
	if err != nil {
		t.Fatalf("resolveExposeMounts: %v", err)
	}
	if len(mounts) != 0 {
		t.Errorf("mounts = %v, want empty", mounts)
	}
}

func TestSharedMountOptions(t *testing.T) {
	if got := sharedMountOptions(true); got != "ro" {
		t.Errorf("readonly options = %q, want ro", got)
	}
	if got := sharedMountOptions(false); got != "rw,z" {
		t.Errorf("read-write options = %q, want rw,z", got)
	}
}

func TestResolveExposeMountsHappyPath(t *testing.T) {
	parentDeps := map[string]packages.InputPackageDependency{
		"radarr": {
			Package: "radarr",
			Expose: map[string]packages.InputDepExpose{
				"movies": {Path: "/data/movies"}, // readonly nil → defaults true
			},
		},
	}
	depRecs := map[string]packages.DependencyRecord{
		"radarr": {EffectiveName: "plex--dep--radarr", Package: "radarr", Repo: "default", Version: "1.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		return &packages.Package{
			Volumes: map[string]packages.PackageVolume{
				"movies": {Mountpoint: "/movies", Shareable: true},
			},
		}, nil
	}

	mounts, err := resolveExposeMounts("/btrfs", parentDeps, depRecs, loader)
	if err != nil {
		t.Fatalf("resolveExposeMounts: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts = %d, want 1", len(mounts))
	}
	want := "/btrfs/installed/default/plex/subpackages/radarr/1.0/movies"
	if mounts[0].HostPath != want {
		t.Errorf("HostPath = %q, want %q", mounts[0].HostPath, want)
	}
	if mounts[0].ContainerPath != "/data/movies" {
		t.Errorf("ContainerPath = %q, want /data/movies", mounts[0].ContainerPath)
	}
	if mounts[0].Options != "ro" {
		t.Errorf("Options = %q, want ro", mounts[0].Options)
	}
}

func TestResolveExposeMountsRejectsNonShareable(t *testing.T) {
	parentDeps := map[string]packages.InputPackageDependency{
		"radarr": {
			Package: "radarr",
			Expose: map[string]packages.InputDepExpose{
				"config": {Path: "/data/config"},
			},
		},
	}
	depRecs := map[string]packages.DependencyRecord{
		"radarr": {EffectiveName: "plex--dep--radarr", Package: "radarr", Repo: "default", Version: "1.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		return &packages.Package{
			Volumes: map[string]packages.PackageVolume{
				// config is NOT marked shareable — must be rejected.
				"config": {Mountpoint: "/config"},
			},
		}, nil
	}

	_, err := resolveExposeMounts("/btrfs", parentDeps, depRecs, loader)
	if err == nil {
		t.Fatal("expected error for non-shareable volume, got nil")
	}
	if !strings.Contains(err.Error(), "not marked shareable") {
		t.Errorf("error %q does not mention shareability", err.Error())
	}
}

func TestResolveExposeMountsRejectsUnknownVolume(t *testing.T) {
	parentDeps := map[string]packages.InputPackageDependency{
		"radarr": {
			Package: "radarr",
			Expose: map[string]packages.InputDepExpose{
				"ghost": {Path: "/data/ghost"},
			},
		},
	}
	depRecs := map[string]packages.DependencyRecord{
		"radarr": {EffectiveName: "plex--dep--radarr", Package: "radarr", Repo: "default", Version: "1.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		return &packages.Package{
			Volumes: map[string]packages.PackageVolume{
				"movies": {Mountpoint: "/movies", Shareable: true},
			},
		}, nil
	}

	_, err := resolveExposeMounts("/btrfs", parentDeps, depRecs, loader)
	if err == nil {
		t.Fatal("expected error for unknown volume, got nil")
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Errorf("error %q does not mention undeclared volume", err.Error())
	}
}

func TestResolveExposeMountsMissingRecord(t *testing.T) {
	parentDeps := map[string]packages.InputPackageDependency{
		"radarr": {
			Package: "radarr",
			Expose: map[string]packages.InputDepExpose{
				"movies": {Path: "/data/movies"},
			},
		},
	}
	// depRecs is empty — the parent declared expose for a dep that
	// does not have an install record. Should surface a clear error
	// rather than silently producing an empty-host-path mount that
	// would crash podman.
	depRecs := map[string]packages.DependencyRecord{}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		t.Fatal("loader should not be called when no record exists")
		return nil, errors.New("unreachable")
	}

	_, err := resolveExposeMounts("/btrfs", parentDeps, depRecs, loader)
	if err == nil {
		t.Fatal("expected error for missing dep record, got nil")
	}
	if !strings.Contains(err.Error(), "no install record") {
		t.Errorf("error %q does not mention missing record", err.Error())
	}
}

func TestResolveConsumeMountsHappyPath(t *testing.T) {
	rwFalse := false
	consume := []packages.InputDepConsume{
		{From: "qbittorrent", Volume: "downloads", Path: "/downloads", ReadOnly: &rwFalse},
	}
	siblings := map[string]packages.DependencyRecord{
		"qbittorrent": {EffectiveName: "media--dep--qbittorrent", Package: "qbittorrent", Repo: "default", Version: "2.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		return &packages.Package{
			Volumes: map[string]packages.PackageVolume{
				"downloads": {Mountpoint: "/downloads", Shareable: true},
			},
		}, nil
	}

	mounts, err := resolveConsumeMounts("/btrfs", "radarr", consume, siblings, loader)
	if err != nil {
		t.Fatalf("resolveConsumeMounts: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts = %d, want 1", len(mounts))
	}
	want := "/btrfs/installed/default/media/subpackages/qbittorrent/2.0/downloads"
	if mounts[0].HostPath != want {
		t.Errorf("HostPath = %q, want %q", mounts[0].HostPath, want)
	}
	if mounts[0].Options != "rw,z" {
		t.Errorf("Options = %q, want rw,z (consume default)", mounts[0].Options)
	}
}

func TestResolveConsumeMountsRejectsSelf(t *testing.T) {
	consume := []packages.InputDepConsume{
		{From: "radarr", Volume: "movies", Path: "/movies"},
	}
	siblings := map[string]packages.DependencyRecord{
		"radarr": {EffectiveName: "plex--dep--radarr", Package: "radarr", Repo: "default", Version: "1.0"},
	}
	loader := func(rec packages.DependencyRecord) (*packages.Package, error) {
		t.Fatal("loader should not be called for self-reference")
		return nil, errors.New("unreachable")
	}

	_, err := resolveConsumeMounts("/btrfs", "radarr", consume, siblings, loader)
	if err == nil {
		t.Fatal("expected self-reference error, got nil")
	}
	if !errors.Is(err, err) || !strings.Contains(err.Error(), "self-reference") {
		t.Errorf("error %q does not mention self-reference", err.Error())
	}
}

// TestOrderDependenciesAddsConsumeEdges proves the topological sort sees
// consume.From as an install-time ordering dependency. Without this,
// dep B that consumes from sibling A might install before A and find
// A's btrfs subvolume missing — the bind mount would still resolve at
// systemd-unit time (paths exist on disk after CreateFilesystem) but
// the install code would fail to verify Shareable on a not-yet-loaded
// sibling.
func TestOrderDependenciesAddsConsumeEdges(t *testing.T) {
	deps := map[string]packages.InputPackageDependency{
		"a": {Package: "a"},
		"b": {Package: "b", Consume: []packages.InputDepConsume{
			{From: "a", Volume: "downloads", Path: "/downloads"},
		}},
	}
	order, err := orderDependencies(deps)
	if err != nil {
		t.Fatalf("orderDependencies: %v", err)
	}
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("order = %v, want [a b]", order)
	}
}

// TestOrderDependenciesRejectsConsumeCycle pins down that a consume
// cycle (A consumes from B; B consumes from A) is rejected, mirroring
// the existing cycle handling for response references.
func TestOrderDependenciesRejectsConsumeCycle(t *testing.T) {
	deps := map[string]packages.InputPackageDependency{
		"a": {Package: "a", Consume: []packages.InputDepConsume{
			{From: "b", Volume: "x", Path: "/x"},
		}},
		"b": {Package: "b", Consume: []packages.InputDepConsume{
			{From: "a", Volume: "y", Path: "/y"},
		}},
	}
	if _, err := orderDependencies(deps); err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}
