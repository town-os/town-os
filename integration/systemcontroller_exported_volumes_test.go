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

// exportedVolumeHarness is a systemcontroller over a file:// repository, used
// to install a producer and then a consumer that attaches to what it exported.
type exportedVolumeHarness struct {
	client    *systemcontroller.SystemdClient
	sd        *systemd.MockManager
	btrfsBase string
}

// setupExportedVolumeRepo writes the supplied package YAMLs into a file://
// repository and returns a systemcontroller wired to it.
func setupExportedVolumeRepo(t *testing.T, pkgs map[string]string) *exportedVolumeHarness {
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

	for name, body := range pkgs {
		pkgDir := filepath.Join(dir, "local", packages.PackagesDir, name)
		if err := os.MkdirAll(pkgDir, 0750); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(body), 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	sd := systemd.InitMockManager()
	btrfsBase := t.TempDir()
	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:        storage.InitBtrFSMock(),
		RepositoryRoot: rr,
		Installer:      packages.NewInstallManager(dir),
		Systemd:        sd,
		BtrfsBasePath:  btrfsBase,
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}
	return &exportedVolumeHarness{client: c, sd: sd, btrfsBase: btrfsBase}
}

// unitContent scans the mock systemd manager for the most recent InstallUnit
// content for unitName, or "" when the unit was never installed.
func (h *exportedVolumeHarness) unitContent(t *testing.T, unitName string) string {
	t.Helper()
	var content string
	for _, call := range h.sd.GetCalls() {
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

// The producer: a media server that offers its library to the whole box.
const exportedProducerYAML = `image: alpine:3.20
description: "media server"
network:
  internal:
    http: "8096"
volumes:
  media:
    mountpoint: /media
    uid: 1000
    gid: 1000
    exported: true
  config:
    mountpoint: /config
questions: {}
`

// The consumer: an importer that files content into a library it does not own.
const exportedConsumerYAML = `image: alpine:3.20
description: "importer"
network:
  internal:
    http: "7878"
volumes:
  config:
    mountpoint: /config
questions:
  library:
    query: "Media library"
    type: shared_volume
    optional: true
attach:
  library:
    volume: "@library@"
    subpath: movies
    path: /library/movies
    uid: 1000
    gid: 1000
`

// TestIntegrationExportedVolumeAttachWiresConsumerMount is the end-to-end
// check for the whole feature: the producer exports, the consumer is installed
// picking that export, and the consumer's unit carries a -v flag at the
// producer's btrfs subvolume. Both are standalone packages -- neither is a
// dependency of the other -- which is the whole point.
func TestIntegrationExportedVolumeAttachWiresConsumerMount(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":   exportedProducerYAML,
		"ev-importer": exportedConsumerYAML,
	})

	if err := h.client.InstallPackage(context.TODO(), "ev-server", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("install producer: %v", err)
	}

	// The picker must now offer exactly the exported volume.
	exported, err := h.client.ListExportedVolumes(context.TODO())
	if err != nil {
		t.Fatalf("ListExportedVolumes: %v", err)
	}
	if len(exported) != 1 || exported[0].Reference != "local/ev-server/media" {
		t.Fatalf("exported volumes = %#v, want local/ev-server/media", exported)
	}
	if exported[0].Mountpoint != "/media" {
		t.Fatalf("mountpoint = %q, want /media", exported[0].Mountpoint)
	}

	if err := h.client.InstallPackage(context.TODO(), "ev-importer", "1.0",
		packages.Responses{"library": exported[0].Reference}, false, "", false); err != nil {
		t.Fatalf("install consumer: %v", err)
	}

	body := h.unitContent(t, systemd.UnitName("local", "ev-importer", "1.0"))
	if body == "" {
		t.Fatal("consumer unit was not installed")
	}
	wantHost := filepath.Join(h.btrfsBase, "installed", "local", "ev-server", "1.0", "media", "movies")
	wantFlag := "-v " + wantHost + ":/library/movies:rw,z"
	if !strings.Contains(body, wantFlag) {
		t.Fatalf("consumer unit missing attach mount %q; body:\n%s", wantFlag, body)
	}
	// The subpath directory belongs to nobody until something creates it, and
	// the consumer runs as 1000 while the producer's container may not.
	if !strings.Contains(body, "mkdir -p "+wantHost) {
		t.Fatalf("consumer unit does not create the attach subpath; body:\n%s", body)
	}
	if !strings.Contains(body, "chown 1000:1000 "+wantHost) {
		t.Fatalf("consumer unit does not chown the attach subpath; body:\n%s", body)
	}
	// The producer's non-exported volume must never be reachable this way.
	if strings.Contains(body, "ev-server/1.0/config") {
		t.Fatalf("consumer unit reached a non-exported producer volume; body:\n%s", body)
	}
}

// TestIntegrationExportedVolumeOptionalAttachSkipped: an optional
// shared_volume question left blank compiles to nothing, and the consumer
// installs with no attach mount rather than one whose source is the btrfs
// root.
func TestIntegrationExportedVolumeOptionalAttachSkipped(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":   exportedProducerYAML,
		"ev-importer": exportedConsumerYAML,
	})

	if err := h.client.InstallPackage(context.TODO(), "ev-importer", "1.0",
		packages.Responses{"library": ""}, false, "", false); err != nil {
		t.Fatalf("install consumer: %v", err)
	}

	body := h.unitContent(t, systemd.UnitName("local", "ev-importer", "1.0"))
	if body == "" {
		t.Fatal("consumer unit was not installed")
	}
	if strings.Contains(body, "/library/movies") {
		t.Fatalf("an unanswered optional attach still produced a mount; body:\n%s", body)
	}
}

// TestIntegrationExportedVolumeNonExportedRejected proves the flag is load
// bearing: naming a producer volume that is not exported fails the install
// rather than quietly bind-mounting somebody's config directory.
func TestIntegrationExportedVolumeNonExportedRejected(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":   exportedProducerYAML,
		"ev-importer": exportedConsumerYAML,
	})

	if err := h.client.InstallPackage(context.TODO(), "ev-server", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("install producer: %v", err)
	}

	err := h.client.InstallPackage(context.TODO(), "ev-importer", "1.0",
		packages.Responses{"library": "local/ev-server/config"}, false, "", false)
	if err == nil {
		t.Fatal("install accepted an attach to a non-exported volume")
	}
	if !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("error does not say why: %v", err)
	}
}

// TestIntegrationExportedVolumeMissingProducerRejected: the operator picked it
// moments ago, so a reference to something not installed is a failure they
// should see rather than a container that comes up without its library.
func TestIntegrationExportedVolumeMissingProducerRejected(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":   exportedProducerYAML,
		"ev-importer": exportedConsumerYAML,
	})

	err := h.client.InstallPackage(context.TODO(), "ev-importer", "1.0",
		packages.Responses{"library": "local/ev-server/media"}, false, "", false)
	if err == nil {
		t.Fatal("install accepted an attach to a package that is not installed")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error does not say why: %v", err)
	}
}

// TestIntegrationExportedVolumeRejectsTraversingReference is the security case
// end to end. /storage/exported-volumes and the install route are requireAuth,
// not requireAdmin, so any ordinary account can submit this.
func TestIntegrationExportedVolumeRejectsTraversingReference(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":   exportedProducerYAML,
		"ev-importer": exportedConsumerYAML,
	})
	if err := h.client.InstallPackage(context.TODO(), "ev-server", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("install producer: %v", err)
	}

	for _, bad := range []string{"../../etc", "local/../gfeh/home", "local/ev-server/.."} {
		err := h.client.InstallPackage(context.TODO(), "ev-importer", "1.0",
			packages.Responses{"library": bad}, false, "", false)
		if err == nil {
			t.Errorf("install accepted a traversing reference %q", bad)
		}
	}
}

// TestIntegrationExportedVolumesExcludesDependencies: a dep's storage is
// nested under its parent and it is internal by design, so it must never
// appear in the box-wide picker even when its own YAML exports a volume.
func TestIntegrationExportedVolumesExcludesDependencies(t *testing.T) {
	t.Parallel()

	depYAML := `image: alpine:3.20
description: "exporting dep"
network:
  internal:
    "9090": "9090"
volumes:
  shared:
    mountpoint: /shared
    exported: true
questions: {}
`
	parentYAML := `image: alpine:3.20
description: "parent"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  child:
    package: ev-dep
    version: "1.0"
`
	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-parent": parentYAML,
		"ev-dep":    depYAML,
	})

	if err := h.client.InstallPackage(context.TODO(), "ev-parent", "1.0",
		packages.Responses{"port": "9130"}, false, "", false); err != nil {
		t.Fatalf("install parent: %v", err)
	}

	exported, err := h.client.ListExportedVolumes(context.TODO())
	if err != nil {
		t.Fatalf("ListExportedVolumes: %v", err)
	}
	for _, v := range exported {
		if packages.IsDependency(v.Package) {
			t.Fatalf("a dependency's exported volume reached the picker: %#v", v)
		}
	}
}

// TestIntegrationExportedVolumesEmptyWithoutExports: the picker returns an
// empty list rather than an error when nothing on the box exports anything,
// which is what lets the install dialog say so instead of failing to open.
func TestIntegrationExportedVolumesEmptyWithoutExports(t *testing.T) {
	t.Parallel()

	h := setupExportedVolumeRepo(t, map[string]string{"ev-importer": exportedConsumerYAML})

	exported, err := h.client.ListExportedVolumes(context.TODO())
	if err != nil {
		t.Fatalf("ListExportedVolumes: %v", err)
	}
	if len(exported) != 0 {
		t.Fatalf("exported volumes = %#v, want empty", exported)
	}
}

// TestIntegrationExportedVolumeDependencyAttach covers the second install site.
// reconcile resolves `attach:` with no IsDependency guard, so a dependency
// whose attach were skipped at install would sprout the mount on the next boot
// and restart under the operator — which reads as a spontaneous failure rather
// than as a step install forgot. Both sites have to agree.
func TestIntegrationExportedVolumeDependencyAttach(t *testing.T) {
	t.Parallel()

	// A dep cannot be handed a picker, so the parent supplies the reference
	// through responses: exactly as it supplies any other dep answer.
	depYAML := `image: alpine:3.20
description: "attaching dep"
network:
  internal:
    "9091": "9091"
questions:
  library:
    query: "Media library"
    type: shared_volume
attach:
  library:
    volume: "@library@"
    subpath: movies
    path: /movies
`
	parentYAML := `image: alpine:3.20
description: "parent of an attaching dep"
network:
  external:
    "@port@": "80"
questions:
  port:
    query: "External port?"
    type: port
dependencies:
  child:
    package: ev-attaching-dep
    version: "1.0"
    responses:
      library: "local/ev-server/media"
`
	h := setupExportedVolumeRepo(t, map[string]string{
		"ev-server":        exportedProducerYAML,
		"ev-parent":        parentYAML,
		"ev-attaching-dep": depYAML,
	})

	if err := h.client.InstallPackage(context.TODO(), "ev-server", "1.0", packages.Responses{}, false, "", false); err != nil {
		t.Fatalf("install producer: %v", err)
	}
	if err := h.client.InstallPackage(context.TODO(), "ev-parent", "1.0",
		packages.Responses{"port": "9131"}, false, "", false); err != nil {
		t.Fatalf("install parent: %v", err)
	}

	depUnit := systemd.UnitName("local", "ev-parent--dep--child", "1.0")
	body := h.unitContent(t, depUnit)
	if body == "" {
		t.Fatalf("dep unit %q was not installed", depUnit)
	}
	wantHost := filepath.Join(h.btrfsBase, "installed", "local", "ev-server", "1.0", "media", "movies")
	if !strings.Contains(body, "-v "+wantHost+":/movies:rw,z") {
		t.Fatalf("dep unit missing the attach mount; body:\n%s", body)
	}
}
