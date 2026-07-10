// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"gitea.com/town-os/town-os/src/git"
	"gitea.com/town-os/town-os/src/packages"
	"github.com/labstack/echo/v5"
)

// A package installed into a non-default network must render its notes/URL info
// pages under that network's TLD (gitea.default.fart), not the global home zone.
// The info dialog recompiles @PACKAGE_DNS@ on demand, so the old always-dns_tld
// behavior showed gitea.default.home for a package on the fart network even
// though its address records resolve under .fart. A default-network package
// stays under dns_tld (home).
func TestGetInstalledInfoUsesNetworkTLD(t *testing.T) {
	pkgYAML := `image: nginx:1.0
notes:
  URL:
    value: "https://@PACKAGE_DNS@/"
    type: url
`
	tests := []struct {
		name     string
		pkg      string
		network  string // "" leaves the package on the default network
		wantNote string
	}{
		{name: "fart network follows network TLD", pkg: "gitea", network: "fart", wantNote: "https://gitea.default.fart/"},
		{name: "default network stays on home", pkg: "nginx", network: "", wantNote: "https://nginx.default.home/"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nm := seedNetwork(t) // fart network, TLD "fart"

			rr := &packages.RepositoryRoot{
				BaseDir: t.TempDir(),
				Git:     &git.GoGitClient{Home: t.TempDir()},
			}
			u, err := url.Parse("https://example.com/default.git")
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			rr.Items = []packages.Repository{{Name: "default", URL: *u}}
			writeTestPackage(t, rr.BaseDir, "default", tc.pkg, "1.0", pkgYAML)

			inst := packages.InitMockInstallManager()
			inst.StoredResponses = map[string]packages.Responses{
				"default/" + tc.pkg + "@1.0": {},
			}
			if tc.network != "" {
				if err := inst.SaveNetwork("default", tc.pkg, tc.network); err != nil {
					t.Fatalf("SaveNetwork: %v", err)
				}
			}

			sb := &serverBase{ServerConfig: ServerConfig{NetworkMgr: nm, Installer: inst, RepositoryRoot: rr}}
			s := &SystemControllerHandlers{Controller: sb, ctx: context.Background()}

			info := getInstalledInfoView(t, s, tc.pkg)
			if got := info.Notes["URL"]; got != tc.wantNote {
				t.Errorf("URL note = %q, want %q", got, tc.wantNote)
			}
		})
	}
}

// getInstalledInfoView invokes the POST /packages/installed/info handler for a
// default-repo package at version 1.0 and decodes the JSON response.
func getInstalledInfoView(t *testing.T, s *SystemControllerHandlers, name string) InstalledInfoResponse {
	t.Helper()
	body, err := json.Marshal(PackageIdentityRequest{Repo: "default", Name: name, Version: "1.0"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	e := echo.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/packages/installed/info", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if err := s.getInstalledInfo(e.NewContext(req, rec)); err != nil {
		t.Fatalf("getInstalledInfo: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var info InstalledInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v (body: %s)", err, rec.Body.String())
	}
	return info
}
