//go:build proton

// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/systemd"
)

func initProtonInstallTestClient(t *testing.T) (*SystemdClient, *packages.MockInstallManager, *systemd.MockManager) {
	t.Helper()
	mock := storage.InitBtrFSMock()
	rr := emptyRepoRoot(t)
	u, err := url.Parse("https://example.com/repo-a.git")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}
	rr.Items = []packages.Repository{
		{Name: "repo-a", URL: *u},
	}
	protonPkg := `image:
  type: oci
proton:
  app_image: mycompany/windows-app:1.0
  app_directory: /app
  volume: app
  exe: /app/myapp.exe
volumes:
  app:
    mountpoint: /app
  compatdata:
    mountpoint: /proton-data
environment:
  STEAM_COMPAT_DATA_PATH: /proton-data
`
	writeTestPackage(t, rr.BaseDir, "repo-a", "winapp", "1.0", protonPkg)

	inst := packages.InitMockInstallManager()
	sd := systemd.InitMockManager()

	settingsMgr := &mockSettingsManager{
		values: map[string]string{
			"proton_image": "ghcr.io/town-os/proton-runner:latest",
		},
	}

	ts := InitTestServer(ServerConfig{
		Storage:        mock,
		RepositoryRoot: rr,
		Installer:      inst,
		Systemd:        sd,
		SettingsMgr:    settingsMgr,
	})
	t.Cleanup(ts.Close)

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("ts.Client: %v", err)
	}

	return c, inst, sd
}
