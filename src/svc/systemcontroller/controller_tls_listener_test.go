// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package systemcontroller

import (
	cryptotls "crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"slices"
	"testing"

	townostls "gitea.com/town-os/town-os/src/tls"
)

// The control plane carries the login password and every bearer token after it,
// on a LAN-facing port, in the clear. These cover the opt-in that terminates it
// with the box's own CA — the same root the ingress already uses for packages,
// fetchable by any client through the public GET /tls/ca.crt.

func TestControllerTLSRequested(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		want  bool
	}{
		{"unset", nil, false},
		{"empty", map[string]string{EnvTLS: ""}, false},
		{"zero", map[string]string{EnvTLS: "0"}, false},
		{"off", map[string]string{EnvTLS: "off"}, false},
		{"one", map[string]string{EnvTLS: "1"}, true},
		{"true", map[string]string{EnvTLS: "true"}, true},
		{"TRUE", map[string]string{EnvTLS: "TRUE"}, true},
		{"padded yes", map[string]string{EnvTLS: " yes "}, true},
		{"explicit pair", map[string]string{EnvTLSCert: "/c.pem", EnvTLSKey: "/k.pem"}, true},
		{"cert without key", map[string]string{EnvTLSCert: "/c.pem"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvTLS, "")
			t.Setenv(EnvTLSCert, "")
			t.Setenv(EnvTLSKey, "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := ControllerTLSRequested(); got != tc.want {
				t.Fatalf("ControllerTLSRequested() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Loopback must be in the SAN set: localhost is a first-class caller here (the
// localhostOrAuth routes exist for it), and a leaf that omits 127.0.0.1 turns
// every one of them into a handshake failure.
func TestControllerTLSHostnamesCoverLoopback(t *testing.T) {
	t.Setenv(EnvTLSSANs, "")

	names := ControllerTLSHostnames(nil)
	for _, want := range []string{"localhost", "127.0.0.1", "::1"} {
		if !slices.Contains(names, want) {
			t.Errorf("SAN set %v is missing %q", names, want)
		}
	}
}

func TestControllerTLSHostnamesIncludesExtrasAndEnv(t *testing.T) {
	t.Setenv(EnvTLSSANs, "box.example.com, 10.9.9.9 ,")

	names := ControllerTLSHostnames([]string{"extra.example"})
	for _, want := range []string{"box.example.com", "10.9.9.9", "extra.example"} {
		if !slices.Contains(names, want) {
			t.Errorf("SAN set %v is missing %q", names, want)
		}
	}
	if slices.Contains(names, "") {
		t.Errorf("SAN set %v contains an empty entry", names)
	}
}

// The set must be stable across boots, or IssueLeaf's "already covers exactly
// this set" check stops no-opping and the cert churns on every restart.
func TestControllerTLSHostnamesIsDeduplicatedAndStable(t *testing.T) {
	t.Setenv(EnvTLSSANs, "dup.example,dup.example")

	first := ControllerTLSHostnames([]string{"dup.example"})
	second := ControllerTLSHostnames([]string{"dup.example"})

	if !slices.Equal(first, second) {
		t.Fatalf("SAN set is not stable: %v then %v", first, second)
	}
	count := 0
	for _, n := range first {
		if n == "dup.example" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dup.example appears %d times in %v, want once", count, first)
	}
}

func TestControllerTLSConfigDisabledByDefault(t *testing.T) {
	t.Setenv(EnvTLS, "")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")

	cfg, err := ControllerTLSConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatal("TLS config produced without TOWN_OS_TLS set")
	}
}

// The leaf is issued from the local CA and must actually verify against it —
// the whole point being that a client which fetched /tls/ca.crt can trust it.
func TestControllerTLSConfigIssuesVerifiableLeaf(t *testing.T) {
	t.Setenv(EnvTLS, "1")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")
	t.Setenv(EnvTLSSANs, "")

	base := t.TempDir()
	cfg, err := ControllerTLSConfig(base, []string{"10.7.7.7"})
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("no TLS config with TOWN_OS_TLS=1")
	}
	if cfg.MinVersion != cryptotls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("got %d certificates, want 1", len(cfg.Certificates))
	}

	caPEM, err := os.ReadFile(filepath.Join(base, TLSSubvolume, "ca.crt"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("CA PEM did not parse")
	}

	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	for _, host := range []string{"localhost", "127.0.0.1", "10.7.7.7"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: hostForVerify(host)}); err != nil {
			t.Errorf("leaf does not verify for %s: %v", host, err)
		}
	}
}

// hostForVerify returns the name to pass to x509 verification. IP SANs are
// checked by VerifyHostname rather than DNSName, so an IP is verified by
// chain-only here and asserted separately below.
func hostForVerify(host string) string {
	if host == "localhost" {
		return host
	}
	return ""
}

func TestControllerTLSLeafCarriesIPSANs(t *testing.T) {
	t.Setenv(EnvTLS, "1")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")
	t.Setenv(EnvTLSSANs, "")

	cfg, err := ControllerTLSConfig(t.TempDir(), []string{"10.7.7.7"})
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	leaf, err := x509.ParseCertificate(cfg.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	for _, want := range []string{"127.0.0.1", "10.7.7.7"} {
		if err := leaf.VerifyHostname(want); err != nil {
			t.Errorf("leaf does not cover %s: %v", want, err)
		}
	}
}

// Reissuing must be a no-op, since this runs on every boot.
func TestControllerTLSConfigIsIdempotent(t *testing.T) {
	t.Setenv(EnvTLS, "1")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")
	t.Setenv(EnvTLSSANs, "")

	base := t.TempDir()
	if _, err := ControllerTLSConfig(base, nil); err != nil {
		t.Fatalf("first ControllerTLSConfig: %v", err)
	}
	certPath := filepath.Join(base, TLSSubvolume, ControllerTLSDirName, townostls.LeafCertFileName)
	first, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read leaf: %v", err)
	}

	if _, err := ControllerTLSConfig(base, nil); err != nil {
		t.Fatalf("second ControllerTLSConfig: %v", err)
	}
	second, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("re-read leaf: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("leaf was rewritten on an unchanged SAN set")
	}
}

// An operator who asked for TLS and cannot get it must be told, not quietly
// served cleartext.
func TestControllerTLSConfigErrorsWithoutBtrfsBase(t *testing.T) {
	t.Setenv(EnvTLS, "1")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")

	if _, err := ControllerTLSConfig("", nil); err == nil {
		t.Fatal("ControllerTLSConfig with no btrfs base returned no error")
	}
}

func TestControllerTLSConfigLoadsExplicitPair(t *testing.T) {
	dir := t.TempDir()
	ca, err := townostls.EnsureCA(filepath.Join(dir, "ca"))
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	leafDir := filepath.Join(dir, "leaf")
	if err := ca.IssueLeaf(leafDir, []string{"localhost", "127.0.0.1"}); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}

	t.Setenv(EnvTLS, "")
	t.Setenv(EnvTLSCert, filepath.Join(leafDir, townostls.LeafCertFileName))
	t.Setenv(EnvTLSKey, filepath.Join(leafDir, townostls.LeafKeyFileName))

	// No btrfs base at all: an explicit pair must not consult the local CA.
	cfg, err := ControllerTLSConfig("", nil)
	if err != nil {
		t.Fatalf("ControllerTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatal("explicit certificate pair was not loaded")
	}
}

func TestListenAddrSANs(t *testing.T) {
	cases := map[string][]string{
		":5309":            nil,
		"10.0.0.5:5309":    {"10.0.0.5"},
		"nonsense":         nil,
		"[::1]:5309":       {"::1"},
	}
	for addr, want := range cases {
		if got := ListenAddrSANs(addr); !slices.Equal(got, want) {
			t.Errorf("ListenAddrSANs(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestControllerTLSScheme(t *testing.T) {
	t.Setenv(EnvTLS, "")
	t.Setenv(EnvTLSCert, "")
	t.Setenv(EnvTLSKey, "")
	if got := ControllerTLSScheme(); got != "http" {
		t.Errorf("ControllerTLSScheme() = %q, want http", got)
	}

	t.Setenv(EnvTLS, "1")
	if got := ControllerTLSScheme(); got != "https" {
		t.Errorf("ControllerTLSScheme() = %q, want https", got)
	}
}
