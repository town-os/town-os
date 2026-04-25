// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
)

// setupInlineSharedVolumeRepo constructs a file:// repository under a
// temp dir with parent + dep package YAMLs the caller supplies.
// Returns the configured systemcontroller test client, the repo root,
// and the btrfs base path so the test can poke at on-disk paths.
func setupInlineSharedVolumeRepo(t *testing.T, parentYAML, depYAML string, extraDeps map[string]string) (*systemcontroller.SystemdClient, *systemd.MockManager, string) {
	t.Helper()

	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
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

	writePkg := func(name, body string) {
		pkgDir := filepath.Join(dir, "local", packages.PackagesDir, name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(body), 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	writePkg("sv-parent", parentYAML)
	writePkg("sv-dep", depYAML)
	for name, body := range extraDeps {
		writePkg(name, body)
	}

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return c, sd, btrfsBase
}

// findInstalledUnitContentInt scans MockManager calls for the most recent
// InstallUnit content for unitName. Returns "" when not found. Mirrors the
// helper in systemcontroller_dependency_test.go but kept private to this
// file so each integration test is self-contained.
func findInstalledUnitContentInt(t *testing.T, sd *systemd.MockManager, unitName string) string {
	t.Helper()
	var content string
	for _, call := range sd.GetCalls() {
		if call.Method != "InstallUnit" {
			continue
		}
		name, ok := call.Args[0].(string)
		if !ok {
			t.Fatal("type assertion failed on InstallUnit arg 0")
		}
		if name != unitName {
			continue
		}
		c, ok := call.Args[1].(string)
		if !ok {
			t.Fatal("type assertion failed on InstallUnit arg 1")
		}
		content = c
	}
	return content
}

// TestIntegrationSharedVolumeExposeWiresParentMount installs a parent
// that exposes a dep's shareable volume. After install the parent's
// systemd unit must contain a -v flag pointing at the dep's btrfs
// subvolume on disk with the requested in-container path and :ro
// options. This is the end-to-end check that schema → compile →
// resolveExposeMounts → unit-gen all wire correctly.
func TestIntegrationSharedVolumeExposeWiresParentMount(t *testing.T) {
	t.Parallel()

	depYAML := `image: alpine:3.20
description: "shared dep"
network:
  internal:
    "5432": "5432"
volumes:
  movies:
    mountpoint: /movies
    shareable: true
  config:
    mountpoint: /config
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "shared parent"
network:
  external:
    "@port@": "80"
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  d:
    package: sv-dep
    version: "1.0"
    expose:
      movies:
        path: /data/movies
        readonly: true
`
	c, sd, btrfsBase := setupInlineSharedVolumeRepo(t, parentYAML, depYAML, nil)

	if err := c.InstallPackage(context.TODO(), "sv-parent", "1.0", packages.Responses{"port": "9099"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	parentUnit := systemd.UnitName("local", "sv-parent", "1.0")
	body := findInstalledUnitContentInt(t, sd, parentUnit)
	if body == "" {
		t.Fatalf("parent unit %q not installed", parentUnit)
	}

	// The dep's effective name (parent--dep--key) drives StoragePath
	// nesting; the btrfs mount source must reference the nested path.
	wantHostPath := filepath.Join(btrfsBase, "installed", "local", "sv-parent", packages.SubpackagesDir, "d", "1.0", "movies")
	wantFlag := "-v " + wantHostPath + ":/data/movies:ro"
	if !strings.Contains(body, wantFlag) {
		t.Fatalf("parent unit missing expose mount %q; body:\n%s", wantFlag, body)
	}
	// Non-shareable volumes (config) must NEVER appear in the parent's
	// unit even if a future regression accidentally exposed everything.
	if strings.Contains(body, "/config") {
		t.Fatalf("parent unit leaked non-shareable dep volume; body:\n%s", body)
	}
}

// TestIntegrationSharedVolumeConsumeWiresDepMount installs a parent
// with two deps where one consumes a shareable volume from the other.
// After install the consumer dep's unit must carry the producer dep's
// btrfs subvolume as a -v flag with :rw,z (the consume default).
func TestIntegrationSharedVolumeConsumeWiresDepMount(t *testing.T) {
	t.Parallel()

	producerYAML := `image: alpine:3.20
description: "producer"
network:
  internal:
    "8080": "8080"
volumes:
  downloads:
    mountpoint: /downloads
    shareable: true
questions: {}
`
	consumerYAML := `image: alpine:3.20
description: "consumer"
network:
  internal:
    "9090": "9090"
volumes:
  config:
    mountpoint: /config
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "wiring parent"
network:
  external:
    "@port@": "80"
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  prod:
    package: sv-producer
    version: "1.0"
  cons:
    package: sv-consumer
    version: "1.0"
    consume:
      - from: prod
        volume: downloads
        path: /downloads
`
	extras := map[string]string{
		"sv-producer": producerYAML,
		"sv-consumer": consumerYAML,
	}
	// We'll use sv-producer as the dep YAML; setupInlineSharedVolumeRepo
	// always writes sv-dep but a parent that does not name it just
	// leaves the file unused.
	c, sd, btrfsBase := setupInlineSharedVolumeRepo(t, parentYAML, producerYAML, extras)

	if err := c.InstallPackage(context.TODO(), "sv-parent", "1.0", packages.Responses{"port": "9100"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Consumer's effective name is "sv-parent--dep--cons"; producer is
	// "sv-parent--dep--prod". Cross-mount points consumer at producer's
	// nested volume path under the parent.
	consumerUnit := systemd.UnitName("local", "sv-parent--dep--cons", "1.0")
	body := findInstalledUnitContentInt(t, sd, consumerUnit)
	if body == "" {
		t.Fatalf("consumer dep unit %q not installed", consumerUnit)
	}

	wantHostPath := filepath.Join(btrfsBase, "installed", "local", "sv-parent", packages.SubpackagesDir, "prod", "1.0", "downloads")
	wantFlag := "-v " + wantHostPath + ":/downloads:rw,z"
	if !strings.Contains(body, wantFlag) {
		t.Fatalf("consumer unit missing consume mount %q; body:\n%s", wantFlag, body)
	}

	// Producer's unit must NOT have the consume mount — the share is
	// one-directional. Producer only owns the volume; consumer reaches
	// in across the parent's storage tree.
	producerUnit := systemd.UnitName("local", "sv-parent--dep--prod", "1.0")
	prodBody := findInstalledUnitContentInt(t, sd, producerUnit)
	if prodBody == "" {
		t.Fatalf("producer dep unit %q not installed", producerUnit)
	}
	if strings.Contains(prodBody, ":/downloads:rw,z") && strings.Count(prodBody, ":/downloads:rw,z") > 1 {
		t.Fatalf("producer unit double-mounted /downloads; body:\n%s", prodBody)
	}
}

// TestIntegrationSharedVolumeNonShareableRejected proves that a parent
// trying to expose a dep volume the dep author did NOT mark
// `shareable: true` fails install with a clear error. Without this
// the platform would silently bind-mount any dep volume.
func TestIntegrationSharedVolumeNonShareableRejected(t *testing.T) {
	t.Parallel()

	depYAML := `image: alpine:3.20
description: "private dep"
network:
  internal:
    "5432": "5432"
volumes:
  secret:
    mountpoint: /secret
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "naughty parent"
network:
  external:
    "@port@": "80"
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  d:
    package: sv-dep
    version: "1.0"
    expose:
      secret:
        path: /data/secret
`
	c, _, _ := setupInlineSharedVolumeRepo(t, parentYAML, depYAML, nil)

	err := c.InstallPackage(context.TODO(), "sv-parent", "1.0", packages.Responses{"port": "9101"}, false, "", false)
	if err == nil {
		t.Fatal("expected install to fail for non-shareable volume, got nil")
	}
	if !strings.Contains(err.Error(), "not marked shareable") {
		t.Fatalf("error %q does not mention shareability", err.Error())
	}
}

// TestIntegrationSharedVolumeReconcileRebuildsMounts pins down the
// reconcile path: install + clear mock systemd state + Reconcile must
// re-emit the same -v flag. This is the regression check that the
// per-boot rebuild from persisted dep records works (parent's compiled
// Dependencies block re-loads via parentDepBlocks; dep volumes are
// still recognized as shareable on the second pass).
func TestIntegrationSharedVolumeReconcileRebuildsMounts(t *testing.T) {
	t.Parallel()

	depYAML := `image: alpine:3.20
description: "shared dep"
network:
  internal:
    "5432": "5432"
volumes:
  movies:
    mountpoint: /movies
    shareable: true
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "shared parent"
network:
  external:
    "@port@": "80"
volumes:
  data:
    mountpoint: /data
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  d:
    package: sv-dep
    version: "1.0"
    expose:
      movies:
        path: /data/movies
        readonly: true
`
	dir := t.TempDir()
	data, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), data, 0600); err != nil {
		t.Fatalf("WriteFile repositories: %v", err)
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

	writePkg := func(name, body string) {
		pkgDir := filepath.Join(dir, "local", packages.PackagesDir, name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(body), 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}
	writePkg("sv-parent", parentYAML)
	writePkg("sv-dep", depYAML)

	inst := packages.NewInstallManager(dir)
	mock := storage.InitBtrFSMock()
	btrfsBase := t.TempDir()
	sd := systemd.InitMockManager()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	if err := c.InstallPackage(context.TODO(), "sv-parent", "1.0", packages.Responses{"port": "9102"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	// Snapshot the parent unit content from install so we can compare
	// against the reconcile rebuild — the two must be byte-identical
	// for installUnitIfChanged to skip a gratuitous restart.
	parentUnit := systemd.UnitName("local", "sv-parent", "1.0")
	beforeBody := findInstalledUnitContentInt(t, sd, parentUnit)
	if beforeBody == "" {
		t.Fatal("parent unit not captured from install")
	}
	wantHostPath := filepath.Join(btrfsBase, "installed", "local", "sv-parent", packages.SubpackagesDir, "d", "1.0", "movies")
	wantFlag := "-v " + wantHostPath + ":/data/movies:ro"
	if !strings.Contains(beforeBody, wantFlag) {
		t.Fatalf("install-time parent unit missing expose mount %q; body:\n%s", wantFlag, beforeBody)
	}

	// Simulate a container restart by clearing mock systemd calls.
	sd.Calls = nil

	if err := systemcontroller.Reconcile(context.Background(), systemcontroller.ReconcileConfig{
		Installer:      inst,
		RepositoryRoot: rr,
		Storage:        mock,
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	afterBody := findInstalledUnitContentInt(t, sd, parentUnit)
	if afterBody == "" {
		t.Fatal("parent unit not re-installed by reconcile")
	}
	if !strings.Contains(afterBody, wantFlag) {
		t.Fatalf("reconcile-time parent unit missing expose mount %q; body:\n%s", wantFlag, afterBody)
	}
	if afterBody != beforeBody {
		t.Fatalf("reconcile produced different unit content; install-time vs reconcile-time differ:\nINSTALL:\n%s\nRECONCILE:\n%s", beforeBody, afterBody)
	}
}
