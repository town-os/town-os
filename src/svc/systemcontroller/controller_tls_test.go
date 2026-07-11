package systemcontroller

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	sans := collectTLSSans("nginx.default.home", []string{"nginx.alt.home"}, "", "", "")
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

// TestCollectTLSSansIncludesInternalIP guards the "LAN IP SAN" behaviour:
// a browser on the home network that types the raw IP instead of the
// .home name needs the cert to list that IP or it gets a name-mismatch
// warning. Empty internalIP means "skip" so boots that can't discover
// the LAN IP don't churn the cert SAN set on every reconcile.
func TestCollectTLSSansIncludesInternalIP(t *testing.T) {
	sans := collectTLSSans("nginx.default.home", nil, "192.168.1.88", "", "")
	want := []string{"nginx.default.home", "localhost", "127.0.0.1", "192.168.1.88"}
	if len(sans) != len(want) {
		t.Fatalf("len=%d, want %d", len(sans), len(want))
	}
	for i, w := range want {
		if sans[i] != w {
			t.Errorf("sans[%d]=%q, want %q", i, sans[i], w)
		}
	}
}

// TestCollectTLSSansIncludesInternalIPv6 guards the AAAA-parity SAN: when the
// host has a global IPv6, a direct https://[v6-literal] dial must match the
// cert. A v4-only host (empty IPv6) gets the v4-only SAN set unchanged.
func TestCollectTLSSansIncludesInternalIPv6(t *testing.T) {
	sans := collectTLSSans("nginx.default.home", nil, "192.168.1.88", "2001:db8::1", "")
	want := []string{"nginx.default.home", "localhost", "127.0.0.1", "192.168.1.88", "2001:db8::1"}
	if len(sans) != len(want) {
		t.Fatalf("len=%d, want %d (%v)", len(sans), len(want), sans)
	}
	for i, w := range want {
		if sans[i] != w {
			t.Errorf("sans[%d]=%q, want %q", i, sans[i], w)
		}
	}
}

func TestIssueLeafForPackageNoOpWithoutCA(t *testing.T) {
	path, err := issueLeafForPackage(nil, "/town-os", "default", "nginx", "1.0", &packages.Package{}, "nginx.default.home", "", "")
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
	path, err := issueLeafForPackage(ca, dir, "default", "nginx", "1.0", compiled, "nginx.default.home", "192.168.1.88", "")
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

	// Walk the issued cert and verify the internal IP landed in SANs —
	// the whole point of threading internalIP through the issuance path
	// is that LAN-IP browser access matches the cert.
	data, err := os.ReadFile(leafCert) //nolint:gosec // G304 -- test temp path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("cert PEM decode failed")
		return
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	foundIP := false
	for _, ip := range parsed.IPAddresses {
		if ip.String() == "192.168.1.88" {
			foundIP = true
			break
		}
	}
	if !foundIP {
		t.Fatalf("internal IP 192.168.1.88 not in cert IP SANs: %v", parsed.IPAddresses)
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
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/gitea/1.0", nil, "home")

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
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/immich/1.0", nil, "home")
	if !state.Ports[0].TLS || state.Ports[0].CertPath == "" {
		t.Fatalf("immich port 2283 must be TLS-wrapped, got %+v", state.Ports[0])
	}
}

// TestApplyTLSToPortsWrapsByPortName is the regression for the gitea
// "browser shows SSL error" report. Gitea's yaml declares
// `network.internal: { http: "@httpport@" }` with a user-settable
// @httpport@ that auto-generates into the 10000–60000 range — any
// numeric allowlist inevitably misses it. Using the `http` port-name
// hint lets the NC TLS-wrap the right port on every install.
func TestApplyTLSToPortsWrapsByPortName(t *testing.T) {
	pkg := &packages.Package{
		Network: packages.PackageNetwork{
			Internal:      packages.PortMap{38895: 38895, 38883: 38883},
			InternalNames: packages.PortNameMap{38895: "http", 38883: "ssh"},
		},
	}
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 38895, InternalPort: 38895, Forward: true},
			{ExternalPort: 38883, InternalPort: 38883, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/gitea/1.0", pkg, "home")

	var httpPort, sshPort *networkcontroller.PortConfig
	for i := range state.Ports {
		switch state.Ports[i].InternalPort {
		case 38895:
			httpPort = &state.Ports[i]
		case 38883:
			sshPort = &state.Ports[i]
		}
	}
	if httpPort == nil || !httpPort.TLS || httpPort.CertPath == "" {
		t.Errorf("named http port 38895 must be TLS-wrapped: %+v", httpPort)
	}
	if sshPort == nil || sshPort.TLS || sshPort.CertPath != "" {
		t.Errorf("named ssh port 38883 must stay plaintext: %+v", sshPort)
	}
}

// TestApplyTLSToPortsExternalNamesAlsoWrap pins that the name check
// looks at both ExternalNames and InternalNames — a package that puts
// the http name under `network.external` (the more common pattern)
// must still get TLS.
func TestApplyTLSToPortsExternalNamesAlsoWrap(t *testing.T) {
	pkg := &packages.Package{
		Network: packages.PackageNetwork{
			External:      packages.PortMap{56510: 2283},
			ExternalNames: packages.PortNameMap{2283: "http"},
		},
	}
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 56510, InternalPort: 2283, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/x/1.0", pkg, "home")
	if !state.Ports[0].TLS {
		t.Fatalf("external-named http port must be TLS-wrapped: %+v", state.Ports[0])
	}
}

func TestHasHTTPPort(t *testing.T) {
	nonHTTP := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{InternalPort: 22},
			{InternalPort: 5432},
		},
	}
	if hasHTTPPort(&nonHTTP, nil) {
		t.Error("ssh/postgres-only package should not have HTTP ports")
	}
	http := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{InternalPort: 22},
			{InternalPort: 3000},
		},
	}
	if !hasHTTPPort(&http, nil) {
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
	if err := reconcileWriteNetworkState(cfg, "default", "nginx", "1.0", "home", "", compiled, []string{"http"}); err != nil {
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

// leafSANs parses the issued leaf and returns its DNS names and IP SANs.
func leafSANs(t *testing.T, btrfs, repo, pkg, version string) ([]string, []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(btrfs, "tls", "leaves", repo, pkg, version, "cert.pem")) //nolint:gosec // G304 -- test temp path
	if err != nil {
		t.Fatalf("read leaf: %v", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("cert PEM decode failed")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	ips := make([]string, 0, len(parsed.IPAddresses))
	for _, ip := range parsed.IPAddresses {
		ips = append(ips, ip.String())
	}
	return parsed.DNSNames, ips
}

// Bug A regression. Reconcile runs on every boot; it used to recompute the TLD
// from the global dns_tld setting, so a package installed into the "fart"
// network had its leaf re-issued with SAN gitea.default.home — silently
// clobbering the correct gitea.default.fart SAN written at install and leaving
// the ingress serving a name the cert was not valid for. The tld must come from
// the CALLER (which resolved it from the install network), and the global
// setting must not be consulted here at all.
func TestReconcileWriteNetworkStateUsesCallerTLDNotGlobalSetting(t *testing.T) {
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

	// The global dns_tld is "home" — reconcile must ignore it for this package.
	cfg := ReconcileConfig{
		BtrfsBasePath:    btrfs,
		NetworkStatePath: stateDir,
		TLSCA:            ca,
		SettingsMgr:      &mockSettingsManager{values: map[string]string{"dns_tld": "home"}},
	}
	compiled := &packages.Package{
		Network: packages.PackageNetwork{External: packages.PortMap{8080: 80}},
	}

	// Caller passes the install-network TLD and the box's overlay address on it.
	if err := reconcileWriteNetworkState(cfg, "default", "gitea", "1.0", "fart", "10.65.0.1", compiled, []string{"http"}); err != nil {
		t.Fatalf("reconcileWriteNetworkState: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(stateDir, "default-gitea-1.0.json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	if st.FQDN != "gitea.default.fart" {
		t.Fatalf("state FQDN = %q, want gitea.default.fart", st.FQDN)
	}

	dnsNames, ips := leafSANs(t, btrfs, "default", "gitea", "1.0")
	var hasFart, hasHome bool
	for _, n := range dnsNames {
		switch n {
		case "gitea.default.fart":
			hasFart = true
		case "gitea.default.home":
			hasHome = true
		}
	}
	if !hasFart {
		t.Errorf("leaf SANs %v missing gitea.default.fart", dnsNames)
	}
	if hasHome {
		t.Errorf("leaf SANs %v must not carry the global-TLD name gitea.default.home", dnsNames)
	}

	// The state file's FQDN and the cert SAN must be the SAME string — that
	// invariant is what keeps the ingress vhost valid for the cert it serves.
	if !slices.Contains(dnsNames, st.FQDN) {
		t.Errorf("state FQDN %q is not a SAN of the issued leaf %v", st.FQDN, dnsNames)
	}

	// The overlay address is a SAN too, so a WireGuard peer can hit the box by
	// raw overlay IP and not just by name.
	if !slices.Contains(ips, "10.65.0.1") {
		t.Errorf("leaf IP SANs %v missing the overlay address 10.65.0.1", ips)
	}
}

// A default-network package has no overlay address and keeps exactly the SAN set
// it had before this change — no cert churn on every reconcile.
func TestReconcileWriteNetworkStateDefaultNetworkHasNoOverlaySAN(t *testing.T) {
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
	cfg := ReconcileConfig{BtrfsBasePath: btrfs, NetworkStatePath: stateDir, TLSCA: ca}
	compiled := &packages.Package{
		Network: packages.PackageNetwork{External: packages.PortMap{8080: 80}},
	}
	if err := reconcileWriteNetworkState(cfg, "default", "nginx", "1.0", "home", "", compiled, []string{"http"}); err != nil {
		t.Fatalf("reconcileWriteNetworkState: %v", err)
	}

	dnsNames, ips := leafSANs(t, btrfs, "default", "nginx", "1.0")
	if !slices.Contains(dnsNames, "nginx.default.home") {
		t.Errorf("leaf SANs %v missing nginx.default.home", dnsNames)
	}
	for _, ip := range ips {
		if strings.HasPrefix(ip, "10.6") {
			t.Errorf("default-network leaf must carry no overlay SAN, got %v", ips)
		}
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
	if err := reconcileWriteNetworkState(cfg, "default", "prometheus", "1.0", "home", "", compiled, nil); err != nil {
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

func TestIsPublicFQDN(t *testing.T) {
	cases := []struct {
		name string
		tld  string
		want bool
	}{
		{"app.example.com", "home", true},
		{"git", "home", false},               // bare label → internal subdomain
		{"gitea.default.home", "home", false}, // under internal TLD
		{"home", "home", false},
		{"localhost", "home", false},
		{"192.168.1.10", "home", false},
		{"", "home", false},
		{"app.example.com.", "home", true}, // trailing dot tolerated
	}
	for _, tc := range cases {
		if got := isPublicFQDN(tc.name, tc.tld); got != tc.want {
			t.Errorf("isPublicFQDN(%q, %q) = %v, want %v", tc.name, tc.tld, got, tc.want)
		}
	}
}

func TestApplyTLSToPortsPassthrough(t *testing.T) {
	pkg := &packages.Package{
		Network: packages.PackageNetwork{
			Internal:      packages.PortMap{8443: 8443},
			InternalNames: packages.PortNameMap{8443: "http"},
			TLSModes:      map[uint16]packages.TLSMode{8443: packages.TLSModePassthrough},
		},
	}
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 8443, InternalPort: 8443, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/app/1.0", pkg, "home")
	p := state.Ports[0]
	if p.TLS {
		t.Errorf("passthrough port must not be TLS-terminated: %+v", p)
	}
	if !p.Passthrough {
		t.Errorf("passthrough port must be marked Passthrough: %+v", p)
	}
	if p.CertPath != "" {
		t.Errorf("passthrough port must carry no cert path: %+v", p)
	}
}

func TestApplyTLSToPortsPublicDomain(t *testing.T) {
	pkg := &packages.Package{
		Network: packages.PackageNetwork{
			External: packages.PortMap{443: 8080},
			Domains:  []string{"git", "app.example.com"}, // one internal label, one public FQDN
		},
	}
	state := networkcontroller.PackageNetworkState{
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 443, InternalPort: 8080, Forward: true},
		},
	}
	applyTLSToPorts(&state, "/etc/town-os/tls/leaves/default/app/1.0", pkg, "home")
	p := state.Ports[0]
	if !p.TLS || p.CertPath == "" {
		t.Errorf("public-domain port must still terminate with the local leaf (DANE fallback): %+v", p)
	}
	if !p.PublicDomain {
		t.Errorf("port with a public FQDN must be marked PublicDomain: %+v", p)
	}
	if len(p.SNINames) != 1 || p.SNINames[0] != "app.example.com" {
		t.Errorf("SNINames should contain only the public FQDN, got %v", p.SNINames)
	}
}

func TestBuildPackageNetworkStateSkipsDirectPorts(t *testing.T) {
	compiled := &packages.Package{
		Network: packages.PackageNetwork{
			External:    packages.PortMap{2222: 22, 8080: 80},
			DirectPorts: map[uint16]bool{2222: true},
		},
	}
	state := buildPackageNetworkState("default", "app", "1.0", compiled)
	if len(state.Ports) != 1 {
		t.Fatalf("expected 1 proxied port (direct skipped), got %d: %+v", len(state.Ports), state.Ports)
	}
	if state.Ports[0].ExternalPort != 8080 {
		t.Fatalf("expected proxied port 8080, got %d", state.Ports[0].ExternalPort)
	}
}

func TestTLSAValue(t *testing.T) {
	dir := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(dir, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := hostTLSLeafDir(dir, "default", "gitea", "1.0")
	if err := ca.IssueLeaf(leafDir, []string{"gitea.default.home"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}
	val, err := tlsaValue(filepath.Join(leafDir, "cert.pem"))
	if err != nil {
		t.Fatalf("tlsaValue: %v", err)
	}
	// DANE-EE / SPKI / SHA-256 prefix and a 64-hex-char digest.
	const prefix = "3 1 1 "
	if len(val) != len(prefix)+64 || val[:len(prefix)] != prefix {
		t.Fatalf("tlsaValue = %q, want %q + 64 hex chars", val, prefix)
	}
}

func TestBuildTLSAEntriesFromStateFile(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ca, err := townostls.EnsureCA(filepath.Join(dir, TLSSubvolume))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := hostTLSLeafDir(dir, "default", "gitea", "1.0")
	if err := ca.IssueLeaf(leafDir, []string{"gitea.default.home"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}

	// State with one terminated port and one passthrough port.
	st := networkcontroller.PackageNetworkState{
		Repo: "default", Package: "gitea", Version: "1.0",
		ContainerName: "town-os-default-gitea-1.0",
		Ports: []networkcontroller.PortConfig{
			{ExternalPort: 443, InternalPort: 3000, Forward: true, TLS: true, CertPath: "/etc/town-os/tls/leaves/default/gitea/1.0"},
			{ExternalPort: 8443, InternalPort: 8443, Forward: true, Passthrough: true},
		},
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "default-gitea-1.0.json"), data, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	entries, err := buildTLSAEntries(stateDir, dir, "default", "gitea", "1.0", "home", []string{"git", "app.example.com"})
	if err != nil {
		t.Fatalf("buildTLSAEntries: %v", err)
	}
	// Terminated port 443 × internal FQDNs (gitea.default.home + git subdomain).
	// The passthrough port (8443) and the public FQDN (app.example.com) are excluded.
	if len(entries) != 2 {
		t.Fatalf("expected 2 TLSA entries, got %d: %+v", len(entries), entries)
	}
	names := map[string]bool{}
	for _, e := range entries {
		if e.Port != 443 {
			t.Errorf("unexpected port %d (passthrough must be excluded)", e.Port)
		}
		names[e.Name] = true
	}
	if !names["gitea.default.home"] || !names["git.gitea.default.home"] {
		t.Fatalf("missing expected internal FQDNs: %v", names)
	}
	if names["app.example.com"] {
		t.Fatal("public FQDN must not get a DANE TLSA entry")
	}
}

func TestReconcileWriteNetworkStateOmitsDirectPort(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := ReconcileConfig{NetworkStatePath: stateDir}
	compiled := &packages.Package{
		Network: packages.PackageNetwork{
			External:    packages.PortMap{2222: 22, 9000: 9000},
			DirectPorts: map[uint16]bool{2222: true},
		},
	}
	if err := reconcileWriteNetworkState(cfg, "default", "app", "1.0", "home", "", compiled, nil); err != nil {
		t.Fatalf("reconcileWriteNetworkState: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "default-app-1.0.json"))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var st networkcontroller.PackageNetworkState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(st.Ports) != 1 || st.Ports[0].ExternalPort != 9000 {
		t.Fatalf("direct port must be absent from state, got %+v", st.Ports)
	}
}
