// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package integration_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/account"
	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
)

// TestInstalledInfoNotesFollowNetworkTLD drives the /packages/installed/info
// endpoint end-to-end through the real HTTP client, a disk-backed InstallManager
// (which persists and reads network.json), a real RepositoryRoot, and the sqlite
// network manager. It confirms the @PACKAGE_DNS@ substitution in a package's
// notes/URL pages follows the install network's TLD: a package on the "fart"
// network renders gitea.default.fart, while a default-network package stays on
// the global dns_tld (home). This is the recompile path the UI info dialog hits,
// which previously always used dns_tld and showed gitea.default.home for a
// fart-network package.
func TestInstalledInfoNotesFollowNetworkTLD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Real sqlite network manager holding the non-default "fart" network. The
	// controller layer derives keys/subnet; the manager stores fields verbatim,
	// so name + TLD are all networkTLD needs.
	nm := initNetworkDB(t)
	if _, err := nm.Create(t.Context(), &account.Network{Name: "fart", TLD: "fart", Enabled: true}); err != nil {
		t.Fatalf("create fart network: %v", err)
	}

	// A repository root with a package whose sole note is a URL built from
	// @PACKAGE_DNS@.
	repoDir := t.TempDir()
	rr := &packages.RepositoryRoot{BaseDir: repoDir, Git: &git.GoGitClient{Home: t.TempDir()}}
	u, err := url.Parse("https://example.com/default.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{{Name: "default", URL: *u}}

	pkgYAML := `image: nginx:1.0
notes:
  URL:
    value: "https://@PACKAGE_DNS@/"
    type: url
`
	writeIntegrationPackage(t, repoDir, "default", "gitea", "2.0", pkgYAML)
	writeIntegrationPackage(t, repoDir, "default", "nginx", "1.0", pkgYAML)

	// Disk-backed install manager: gitea is assigned to fart (network.json on
	// disk), nginx stays on the default network. Both need persisted responses so
	// GetResponses succeeds.
	inst := packages.NewInstallManager(t.TempDir())
	if err := inst.SaveResponses("default", "gitea", "2.0", packages.Responses{}); err != nil {
		t.Fatalf("SaveResponses gitea: %v", err)
	}
	if err := inst.SaveResponses("default", "nginx", "1.0", packages.Responses{}); err != nil {
		t.Fatalf("SaveResponses nginx: %v", err)
	}
	if err := inst.SaveNetwork("default", "gitea", "fart"); err != nil {
		t.Fatalf("SaveNetwork gitea: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		NetworkMgr:     nm,
		Installer:      inst,
		RepositoryRoot: rr,
		SettingsMgr:    &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	})
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	// gitea on the fart network resolves under the network TLD.
	giteaInfo, err := c.GetInstalledInfo(ctx, "default", "gitea", "2.0")
	if err != nil {
		t.Fatalf("GetInstalledInfo gitea: %v", err)
	}
	if got := giteaInfo.Notes["URL"]; got != "https://gitea.default.fart/" {
		t.Errorf("gitea URL note = %q, want https://gitea.default.fart/", got)
	}

	// nginx on the default network stays on the global dns_tld (home).
	nginxInfo, err := c.GetInstalledInfo(ctx, "default", "nginx", "1.0")
	if err != nil {
		t.Fatalf("GetInstalledInfo nginx: %v", err)
	}
	if got := nginxInfo.Notes["URL"]; got != "https://nginx.default.home/" {
		t.Errorf("nginx URL note = %q, want https://nginx.default.home/", got)
	}
}

// writeIntegrationPackage writes a package YAML into a repository root's on-disk
// layout (<baseDir>/<repo>/packages/<pkg>/<version>.yaml).
func writeIntegrationPackage(t *testing.T, baseDir, repo, pkg, version, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, repo, packages.PackagesDir, pkg)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, version+".yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s/%s.yaml: %v", dir, version, err)
	}
}
