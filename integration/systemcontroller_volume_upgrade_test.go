// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// upgradeBtrfsBase is the btrfs mount the integration container provides, the
// same root initSystemControllerTestWithStorage uses.
const upgradeBtrfsBase = "/town-os"

// initVolumeUpgradeTest provisions a local repository holding one package at two
// versions, both declaring the same `config` volume, backed by the REAL btrfs
// mount rather than the storage mock -- the whole point of the test is that the
// subvolume rename an upgrade performs actually happens on disk.
//
// The package name is unique per run: the volume tree under /town-os is shared
// by every test in the container, so a fixed name would collide with a parallel
// test.
func initVolumeUpgradeTest(t *testing.T) (*systemcontroller.SystemdClient, string) {
	t.Helper()

	pkgName := fmt.Sprintf("upgvol%d", time.Now().UnixNano())

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal empty repo list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories file: %v", err)
	}
	rr, err := packages.RepositoryRootFromBase(dir)
	if err != nil {
		t.Fatalf("RepositoryRootFromBase: %v", err)
	}
	u, err := url.Parse("file://" + dir)
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "local", URL: *u}}

	const pkgYAML = `image: alpine:3.20
description: "volume upgrade test"
network:
  external: {}
  internal: {}
volumes:
  config:
    mountpoint: /config
`
	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, pkgName)
	if err := os.MkdirAll(pkgDir, 0750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	for _, version := range []string{"1.0", "2.0"} {
		if err := os.WriteFile(filepath.Join(pkgDir, version+".yaml"), []byte(pkgYAML), 0600); err != nil {
			t.Fatalf("WriteFile package %s: %v", version, err)
		}
	}

	btr := storage.InitBtrFS(upgradeBtrfsBase)
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        btr,
		RepositoryRoot: rr,
		Installer:      packages.NewInstallManager(dir),
		Systemd:        systemd.InitMockManager(),
		BtrfsBasePath:  upgradeBtrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Both trees: a non-purging uninstall parks the volumes under
		// uninstalled/, so a test that ends there would otherwise leak them.
		for _, prefix := range []string{systemcontroller.PackagesVolumePrefix, systemcontroller.UninstalledVolumePrefix} {
			for _, version := range []string{"1.0", "2.0"} {
				name := fmt.Sprintf("%s/local/%s/%s/config", prefix, pkgName, version)
				if err := c.RemoveFilesystem(ctx, name); err != nil {
					t.Logf("cleanup RemoveFilesystem %s: %v", name, err)
				}
			}
		}
	})

	return c, pkgName
}

// Upgrading a package must carry its volumes -- and the data in them -- across
// to the new version's subvolume.
//
// This regressed silently and completely: the upgrade renames
// installed/<repo>/<pkg>/<old>/<vol> to the path under <new>, but
// RenameFilesystem is os.Rename, which cannot create the destination's parent,
// and provisionVolumes only builds installed/<repo>/<pkg>/<new>/ AFTER the
// rename has already been attempted. Every rename failed ENOENT into a
// slog.Debug, then a fresh empty volume was created in its place. For plex that
// meant losing PlexOnlineToken out of Preferences.xml -- the server comes back
// "not authorized" and re-runs the whole claim -- plus the library database.
func TestIntegrationUpgradeKeepsVolumeData(t *testing.T) {
	t.Parallel()
	c, pkgName := initVolumeUpgradeTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := c.InstallPackage(ctx, pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	// Stands in for Preferences.xml: state the package itself wrote after
	// install, which only the volume move can preserve.
	volDir := filepath.Join(upgradeBtrfsBase, systemcontroller.PackagesVolumePrefix, "local", pkgName, "1.0", "config")
	marker := filepath.Join(volDir, "state.txt")
	if err := os.WriteFile(marker, []byte("PlexOnlineToken=kept"), 0600); err != nil {
		t.Fatalf("write marker into the 1.0 volume: %v", err)
	}

	if err := c.InstallPackage(ctx, pkgName, "2.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 2.0 (upgrade): %v", err)
	}

	moved := filepath.Join(upgradeBtrfsBase, systemcontroller.PackagesVolumePrefix, "local", pkgName, "2.0", "config", "state.txt")
	got, err := os.ReadFile(moved)
	if err != nil {
		t.Fatalf("upgrade did not carry the config volume to 2.0 (%s): %v", moved, err)
	}
	if string(got) != "PlexOnlineToken=kept" {
		t.Fatalf("migrated state.txt = %q, want %q", string(got), "PlexOnlineToken=kept")
	}
}

// configVolumePath is where a version's `config` volume lives on the pool.
func configVolumePath(pkgName, version string) string {
	return filepath.Join(upgradeBtrfsBase, systemcontroller.PackagesVolumePrefix, "local", pkgName, version, "config")
}

// A non-purging uninstall followed by a reinstall with ReuseVolumes must give
// the package its data back.
//
// This is the cycle a package actually has to survive: uninstall parks
// installed/<repo>/<pkg> under uninstalled/<repo>/<pkg>, and the reinstall
// renames it back. That second rename is os.Rename, so installed/<repo>/ has to
// exist -- and on a box where no other package from that repo is installed, it
// does not. The failure was swallowed at Debug and provisionVolumes then created
// an empty volume, which for plex means the claim token in Preferences.xml and
// the library database are gone and the server comes back "not authorized".
func TestIntegrationUninstallReinstallKeepsVolumeData(t *testing.T) {
	t.Parallel()
	c, pkgName := initVolumeUpgradeTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := c.InstallPackage(ctx, pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	marker := filepath.Join(configVolumePath(pkgName, "1.0"), "state.txt")
	if err := os.WriteFile(marker, []byte("PlexOnlineToken=kept"), 0600); err != nil {
		t.Fatalf("seed config volume: %v", err)
	}

	// purgeVolumes=false: the operator asked to keep the data.
	if err := c.UninstallPackage(ctx, "local", pkgName, "1.0", false); err != nil {
		t.Fatalf("UninstallPackage without purge: %v", err)
	}

	// reuseVolumes=true: and asked for it back.
	if err := c.InstallPackage(ctx, pkgName, "1.0", packages.Responses{}, true, "", false); err != nil {
		t.Fatalf("reinstall with ReuseVolumes: %v", err)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("reinstall did not restore the config volume (%s): %v", marker, err)
	}
	if string(got) != "PlexOnlineToken=kept" {
		t.Fatalf("restored state.txt = %q, want %q", string(got), "PlexOnlineToken=kept")
	}
}

// The purging uninstall is the other half of the same contract, and the one that
// actually ran on the box where plex lost its claim: it must destroy the data,
// so the reinstall genuinely starts clean rather than half-inheriting the old
// volume. Asserted so that "purge" cannot quietly decay into "keep" either.
func TestIntegrationUninstallWithPurgeDropsVolumeData(t *testing.T) {
	t.Parallel()
	c, pkgName := initVolumeUpgradeTest(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := c.InstallPackage(ctx, pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("InstallPackage 1.0: %v", err)
	}

	marker := filepath.Join(configVolumePath(pkgName, "1.0"), "state.txt")
	if err := os.WriteFile(marker, []byte("PlexOnlineToken=kept"), 0600); err != nil {
		t.Fatalf("seed config volume: %v", err)
	}

	if err := c.UninstallPackage(ctx, "local", pkgName, "1.0", true); err != nil {
		t.Fatalf("UninstallPackage with purge: %v", err)
	}

	if err := c.InstallPackage(ctx, pkgName, "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("reinstall after purge: %v", err)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("purging uninstall left %s behind", marker)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s after purge: %v", marker, err)
	}

	// The volume itself must still be there and writable -- purge means empty,
	// not missing.
	if err := os.WriteFile(marker, []byte("fresh"), 0600); err != nil {
		t.Fatalf("config volume not usable after purge+reinstall: %v", err)
	}
}
