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

	upstream "gitea.com/town-os/rolodex-dns/go"
	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	"gitea.com/town-os/town-os/src/rolodex"
	"gitea.com/town-os/town-os/src/storage"
	"gitea.com/town-os/town-os/src/svc/systemcontroller"
	"gitea.com/town-os/town-os/src/systemd"
	townostls "gitea.com/town-os/town-os/src/tls"
)

// proxyTestServer wires a mock-backed systemcontroller (no real podman/systemd
// needed) and returns the client plus the dirs/mocks the assertions read.
type proxyTestEnv struct {
	client    *systemcontroller.SystemdClient
	stateDir  string
	btrfsBase string
	rol       *rolodex.MockClient
}

func newProxyTestEnv(t *testing.T, pkgName, pkgYAML string) *proxyTestEnv {
	t.Helper()
	dir := t.TempDir()
	btrfsBase := filepath.Join(dir, "btrfs")
	stateDir := filepath.Join(dir, "network-state")
	for _, d := range []string{btrfsBase, stateDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}

	repoData, err := json.Marshal([]packages.Repository{})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, packages.RepositoriesFile), repoData, 0o600); err != nil {
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

	pkgDir := filepath.Join(dir, "local", packages.PackagesDir, pkgName)
	if err := os.MkdirAll(pkgDir, 0o750); err != nil {
		t.Fatalf("MkdirAll pkgDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "1.0.yaml"), []byte(pkgYAML), 0o600); err != nil {
		t.Fatalf("WriteFile yaml: %v", err)
	}

	inst := packages.NewInstallManager(dir)
	rol := &rolodex.MockClient{}
	ca, err := townostls.EnsureCA(filepath.Join(btrfsBase, systemcontroller.TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	ts := systemcontroller.InitTestServer(systemcontroller.ServerConfig{
		Storage:          storage.InitBtrFSMock(),
		RepositoryRoot:   rr,
		Installer:        inst,
		Systemd:          systemd.InitMockManager(),
		RolodexClient:    rol,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
		BtrfsBasePath:    btrfsBase,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
	})
	ts.SetInternalIP("192.168.10.42")
	t.Cleanup(func() { ts.Server.Close() })

	c, err := ts.Client()
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	return &proxyTestEnv{client: c, stateDir: stateDir, btrfsBase: btrfsBase, rol: rol}
}

func (e *proxyTestEnv) readState(t *testing.T, pkgName string) networkcontroller.PackageNetworkState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.stateDir, "local-"+pkgName+"-1.0.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	return st
}

func (e *proxyTestEnv) records(rt upstream.RecordType) []*upstream.DnsRecord {
	var out []*upstream.DnsRecord
	for _, r := range e.rol.Records {
		if r.RecordType == rt {
			out = append(out, r)
		}
	}
	return out
}

// TestIntegrationDirectPortBypassesProxy installs a package with one proxied
// HTTP port and one `direct` port. The direct port must be absent from the NC
// state file (the service container publishes it itself); the proxied port must
// be present and TLS-terminated, with a matching A record and DANE TLSA record
// in rolodex.
func TestIntegrationDirectPortBypassesProxy(t *testing.T) {
	t.Parallel()
	const yaml = `image: nginx:1.0
description: "proxy/direct test"
supplies: ["http"]
network:
  external:
    http: "@httpport@"
    "2222": 22
  direct:
    - "2222"
volumes: {}
questions:
  httpport:
    query: "port?"
    type: port
    default: "8080"
`
	env := newProxyTestEnv(t, "directpkg", yaml)
	if err := env.client.InstallPackage(context.TODO(), "directpkg", "1.0", packages.Responses{"httpport": "8080"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	st := env.readState(t, "directpkg")
	if len(st.Ports) != 1 {
		t.Fatalf("expected only the proxied port in state (direct omitted), got %+v", st.Ports)
	}
	p := st.Ports[0]
	if p.ExternalPort != 8080 {
		t.Fatalf("expected proxied http port 8080, got %d", p.ExternalPort)
	}
	if !p.TLS || p.CertPath == "" {
		t.Fatalf("proxied http port must be TLS-terminated: %+v", p)
	}
	for _, sp := range st.Ports {
		if sp.ExternalPort == 2222 {
			t.Fatalf("direct port 2222 must not appear in NC state: %+v", st.Ports)
		}
	}

	// rolodex A record at the package's internal name.
	foundA := false
	for _, r := range env.records(upstream.RecordTypeA) {
		if r.Name == "directpkg.local.home." {
			foundA = true
		}
	}
	if !foundA {
		t.Fatalf("missing A record directpkg.local.home.: %+v", env.records(upstream.RecordTypeA))
	}

	// DANE TLSA record at _443._tcp.directpkg.local.home.: the named `http` port
	// is fronted by the shared :443 ingress, so the leaf is pinned on _443.
	tlsa := env.records(upstream.RecordTypeTLSA)
	if len(tlsa) != 1 || tlsa[0].Name != "_443._tcp.directpkg.local.home." {
		t.Fatalf("expected one TLSA record at _443._tcp.directpkg.local.home., got %+v", tlsa)
	}
	if !strings.HasPrefix(tlsa[0].Value, "3 1 1 ") {
		t.Fatalf("TLSA value must be DANE-EE/SPKI/SHA-256, got %q", tlsa[0].Value)
	}
}

// TestIntegrationPassthroughPortNotTerminated installs a package whose HTTP
// port opts into tls_mode: passthrough. The NC must raw-forward it (TLS=false,
// Passthrough=true), render no Caddy site, and publish no TLSA record.
func TestIntegrationPassthroughPortNotTerminated(t *testing.T) {
	t.Parallel()
	const yaml = `image: nginx:1.0
description: "passthrough test"
supplies: ["http"]
network:
  internal:
    http: "@httpport@"
  tls_mode:
    http: passthrough
volumes: {}
questions:
  httpport:
    query: "port?"
    type: port
    default: "8443"
`
	env := newProxyTestEnv(t, "passpkg", yaml)
	if err := env.client.InstallPackage(context.TODO(), "passpkg", "1.0", packages.Responses{"httpport": "8443"}, false, "", false); err != nil {
		t.Fatalf("InstallPackage: %v", err)
	}

	st := env.readState(t, "passpkg")
	if len(st.Ports) != 1 {
		t.Fatalf("expected 1 port, got %+v", st.Ports)
	}
	p := st.Ports[0]
	if p.TLS {
		t.Fatalf("passthrough port must not be TLS-terminated: %+v", p)
	}
	if !p.Passthrough {
		t.Fatalf("passthrough port must be marked Passthrough: %+v", p)
	}

	// No Caddy site for a passthrough port.
	sites := networkcontroller.CollectCaddySites([]*networkcontroller.PackageNetworkState{&st})
	if len(sites) != 0 {
		t.Fatalf("passthrough port must produce no Caddy site, got %+v", sites)
	}

	// No DANE TLSA — the backend owns the cert end to end.
	if tlsa := env.records(upstream.RecordTypeTLSA); len(tlsa) != 0 {
		t.Fatalf("passthrough port must publish no TLSA record, got %+v", tlsa)
	}
}
