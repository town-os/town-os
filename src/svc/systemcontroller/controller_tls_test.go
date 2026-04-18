package systemcontroller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gitea.com/town-os/town-os/src/networkcontroller"
	"gitea.com/town-os/town-os/src/packages"
	townostls "gitea.com/town-os/town-os/src/tls"
)

func TestTLSSubvolumeReserved(t *testing.T) {
	if !isReservedFilesystem(TLSSubvolume) {
		t.Error("tls subvolume should be reserved")
	}
	if !isReservedFilesystem("tls/leaves/default/nginx/1.0") {
		t.Error("nested tls paths should be reserved")
	}
}

func TestHostTLSLeafDir(t *testing.T) {
	got := hostTLSLeafDir("/town-os", "default", "nginx", "1.0")
	want := "/town-os/tls/leaves/default/nginx/1.0"
	if got != want {
		t.Errorf("hostTLSLeafDir = %q, want %q", got, want)
	}
}

func TestContainerTLSLeafDir(t *testing.T) {
	got := containerTLSLeafDir("default", "nginx", "1.0")
	want := "/etc/town-os/tls/leaves/default/nginx/1.0"
	if got != want {
		t.Errorf("containerTLSLeafDir = %q, want %q", got, want)
	}
}

func TestSuppliesHTTP(t *testing.T) {
	if !suppliesHTTP([]string{"http"}) {
		t.Error("http supplies not detected")
	}
	if !suppliesHTTP([]string{"other", "http"}) {
		t.Error("http supplies mixed with others not detected")
	}
	if suppliesHTTP([]string{"https"}) {
		t.Error("https should not match http supplies")
	}
	if suppliesHTTP(nil) {
		t.Error("nil supplies should not match")
	}
}

func TestCollectTLSSans(t *testing.T) {
	sans := collectTLSSans("nginx.default.home", []string{"nginx.alt.home"})
	want := []string{"nginx.default.home", "nginx.alt.home", "localhost", "127.0.0.1"}
	if len(sans) != len(want) {
		t.Fatalf("len=%d, want %d", len(sans), len(want))
	}
	for i, w := range want {
		if sans[i] != w {
			t.Errorf("sans[%d]=%q, want %q", i, sans[i], w)
		}
	}
}

func TestIssueLeafForPackageNoOpWithoutCA(t *testing.T) {
	path, err := issueLeafForPackage(nil, "/town-os", "default", "nginx", "1.0", &packages.Package{}, "nginx.default.home")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if path != "" {
		t.Errorf("expected empty cert path when CA is nil, got %q", path)
	}
}

func TestIssueLeafForPackageWritesAndReturnsContainerPath(t *testing.T) {
	dir := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(dir, "tls"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	compiled := &packages.Package{
		Network: packages.PackageNetwork{Domains: []string{"custom.example"}},
	}
	path, err := issueLeafForPackage(ca, dir, "default", "nginx", "1.0", compiled, "nginx.default.home")
	if err != nil {
		t.Fatalf("issueLeafForPackage: %v", err)
	}
	if path != "/etc/town-os/tls/leaves/default/nginx/1.0" {
		t.Errorf("cert path = %q", path)
	}
	leafCert := filepath.Join(dir, "tls", "leaves", "default", "nginx", "1.0", "cert.pem")
	if _, err := os.Stat(leafCert); err != nil {
		t.Errorf("leaf cert not written: %v", err)
	}
}

func TestApplyTLSToPortsWrapsHTTPOnly(t *testing.T) {
	// Mimics gitea: http on container port 3000, ssh on 22. Only 3000
	// should get TLS-wrapped; SSH must stay plaintext.
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 3000, InternalPort: 3000, Forward: true},
			{ExternalPort: 2222, InternalPort: 22, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/gitea/1.0")

	var httpPort, sshPort *networkcontroller.PortConfig
	for i := range state.Ports {
		switch state.Ports[i].InternalPort {
		case 3000:
			httpPort = &state.Ports[i]
		case 22:
			sshPort = &state.Ports[i]
		}
	}
	if httpPort == nil || !httpPort.TLS || httpPort.CertPath == "" {
		t.Errorf("http port should be TLS-wrapped: %+v", httpPort)
	}
	if sshPort == nil || sshPort.TLS || sshPort.CertPath != "" {
		t.Errorf("ssh port must NOT be TLS-wrapped: %+v", sshPort)
	}
}

// TestApplyTLSToPortsWrapsImmichPort is the regression for the
// "TLS termination issue on immich @ town-os.local" report: immich's
// container-side HTTP port is 2283 (not one of the canonical 80/3000/
// 8008/8065/32400), and when 2283 wasn't in httpContainerPorts the NC
// never applied TLS — the socat forwarder passed ClientHellos through
// as plain TCP and every HTTPS probe ended in "Connection reset by peer".
func TestApplyTLSToPortsWrapsImmichPort(t *testing.T) {
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 56510, InternalPort: 2283, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/immich/1.0")
	if !state.Ports[0].TLS || state.Ports[0].CertPath == "" {
		t.Fatalf("immich port 2283 must be TLS-wrapped, got %+v", state.Ports[0])
	}
}

func TestHasHTTPPort(t *testing.T) {
	nonHTTP := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{InternalPort: 22},
			{InternalPort: 5432},
		},
	}
	if hasHTTPPort(&nonHTTP) {
		t.Error("ssh/postgres-only package should not have HTTP ports")
	}
	http := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{InternalPort: 22},
			{InternalPort: 3000},
		},
	}
	if !hasHTTPPort(&http) {
		t.Error("package with port 3000 should have HTTP port")
	}
}

func TestReconcileWriteNetworkStateIncludesTLSWhenSuppliesHTTP(t *testing.T) {
	dir := t.TempDir()
	btrfs := filepath.Join(dir, "btrfs")
	stateDir := filepath.Join(dir, "state")
	for _, d := range []string{btrfs, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	ca, err := townostls.EnsureCA(filepath.Join(btrfs, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	cfg := ReconcileConfig{
		BtrfsBasePath:    btrfs,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
	}
	compiled := &packages.Package{
		Network: packages.PackageNetwork{
			External: packages.PortMap{8080: 80},
		},
	}
	if err := reconcileWriteNetworkState(cfg, "default", "nginx", "1.0", compiled, []string{"http"}); err != nil {
		t.Fatalf("reconcileWriteNetworkState: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(stateDir, "default-nginx-1.0.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if len(st.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(st.Ports))
	}
	if !st.Ports[0].TLS {
		t.Error("expected TLS=true")
	}
	if st.Ports[0].CertPath != "/etc/town-os/tls/leaves/default/nginx/1.0" {
		t.Errorf("unexpected cert path: %q", st.Ports[0].CertPath)
	}
	if _, err := os.Stat(filepath.Join(btrfs, "tls", "leaves", "default", "nginx", "1.0", "cert.pem")); err != nil {
		t.Errorf("leaf cert not issued: %v", err)
	}
}

func TestReconcileWriteNetworkStateSkipsTLSWithoutSupplies(t *testing.T) {
	dir := t.TempDir()
	btrfs := filepath.Join(dir, "btrfs")
	stateDir := filepath.Join(dir, "state")
	for _, d := range []string{btrfs, stateDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	ca, err := townostls.EnsureCA(filepath.Join(btrfs, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	cfg := ReconcileConfig{
		BtrfsBasePath:    btrfs,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
	}
	compiled := &packages.Package{Network: packages.PackageNetwork{External: packages.PortMap{9090: 9090}}}
	if err := reconcileWriteNetworkState(cfg, "default", "prometheus", "1.0", compiled, nil); err != nil {
		t.Fatalf("reconcileWriteNetworkState: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "default-prometheus-1.0.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if st.Ports[0].TLS {
		t.Error("unexpected TLS=true on non-http package")
	}
	if _, err := os.Stat(filepath.Join(btrfs, "tls", "leaves", "default", "prometheus", "1.0")); !os.IsNotExist(err) {
		t.Errorf("leaf cert should not exist for non-http package, err=%v", err)
	}
}
