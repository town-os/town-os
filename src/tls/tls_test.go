package tls

import (
	"crypto/x509"
	gotls "crypto/tls"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCAGeneratesWhenMissing(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if ca.Cert == nil || ca.Key == nil {
		t.Fatal("EnsureCA returned nil fields")
	}
	if !ca.Cert.IsCA {
		t.Error("generated cert is not marked IsCA")
	}
	if ca.Cert.Subject.CommonName != "Town OS Root CA" {
		t.Errorf("CN = %q, want %q", ca.Cert.Subject.CommonName, "Town OS Root CA")
	}
	if _, err := os.Stat(filepath.Join(dir, CAFileName)); err != nil {
		t.Errorf("ca cert not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, CAKeyFileName)); err != nil {
		t.Errorf("ca key not written: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, CAKeyFileName))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("ca key perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureCAIdempotent(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("first EnsureCA: %v", err)
	}
	second, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("second EnsureCA: %v", err)
	}
	if first.Cert.SerialNumber.Cmp(second.Cert.SerialNumber) != 0 {
		t.Errorf("CA was regenerated across calls (serial changed)")
	}
}

func TestEnsureCARejectsHalfPresent(t *testing.T) {
	dir := t.TempDir()
	// Create only the cert file, leave the key missing.
	if err := os.WriteFile(filepath.Join(dir, CAFileName), []byte("not a real cert"), 0o644); err != nil {
		t.Fatalf("seed partial ca: %v", err)
	}
	if _, err := EnsureCA(dir); err == nil {
		t.Fatal("EnsureCA should reject half-present CA state")
	}
}

func TestIssueLeafCreatesCertAndChain(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	out := filepath.Join(dir, "leaves", "default", "nginx", "1.0")
	sans := []string{"nginx.default.home", "localhost", "127.0.0.1"}
	if err := ca.IssueLeaf(out, sans); err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}

	pair, err := gotls.LoadX509KeyPair(
		filepath.Join(out, LeafCertFileName),
		filepath.Join(out, LeafKeyFileName),
	)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	if len(pair.Certificate) < 2 {
		t.Fatalf("want cert chain with at least 2 certs, got %d", len(pair.Certificate))
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	if len(leaf.DNSNames) != 2 {
		t.Errorf("leaf DNSNames = %v, want 2", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "127.0.0.1" {
		t.Errorf("leaf IPAddresses = %v, want [127.0.0.1]", leaf.IPAddresses)
	}

	// Verify the leaf chains to the CA.
	roots := x509.NewCertPool()
	roots.AddCert(ca.Cert)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		DNSName:   "nginx.default.home",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Errorf("leaf verify: %v", err)
	}
}

func TestIssueLeafIdempotent(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	out := filepath.Join(dir, "leaves", "default", "nginx", "1.0")
	sans := []string{"nginx.default.home", "localhost", "127.0.0.1"}
	if err := ca.IssueLeaf(out, sans); err != nil {
		t.Fatalf("first IssueLeaf: %v", err)
	}

	certPath := filepath.Join(out, LeafCertFileName)
	info1, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("stat cert: %v", err)
	}
	modTime1 := info1.ModTime()

	if err := ca.IssueLeaf(out, sans); err != nil {
		t.Fatalf("second IssueLeaf: %v", err)
	}
	info2, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("stat cert again: %v", err)
	}
	if !info2.ModTime().Equal(modTime1) {
		t.Errorf("cert was rewritten; want idempotent no-op")
	}
}

func TestIssueLeafReissuesOnSANChange(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}

	out := filepath.Join(dir, "leaves", "default", "nginx", "1.0")
	if err := ca.IssueLeaf(out, []string{"nginx.default.home"}); err != nil {
		t.Fatalf("first IssueLeaf: %v", err)
	}
	certPEM1, err := os.ReadFile(filepath.Join(out, LeafCertFileName))
	if err != nil {
		t.Fatalf("read cert 1: %v", err)
	}

	// Add a second SAN — should trigger a re-issue.
	if err := ca.IssueLeaf(out, []string{"nginx.default.home", "nginx.alt.home"}); err != nil {
		t.Fatalf("second IssueLeaf: %v", err)
	}
	certPEM2, err := os.ReadFile(filepath.Join(out, LeafCertFileName))
	if err != nil {
		t.Fatalf("read cert 2: %v", err)
	}
	if string(certPEM1) == string(certPEM2) {
		t.Errorf("cert was not re-issued after SAN change")
	}

	block, _ := pem.Decode(certPEM2)
	if block == nil {
		t.Fatalf("decode cert: empty block")
		return
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	found := 0
	for _, name := range cert.DNSNames {
		if name == "nginx.default.home" || name == "nginx.alt.home" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("reissued cert DNSNames = %v, want both hosts", cert.DNSNames)
	}
}

func TestIssueLeafRejectsEmptySANs(t *testing.T) {
	dir := t.TempDir()
	ca, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA: %v", err)
	}
	if err := ca.IssueLeaf(filepath.Join(dir, "leaves", "x"), nil); err == nil {
		t.Error("IssueLeaf should reject empty SANs")
	}
	if err := ca.IssueLeaf(filepath.Join(dir, "leaves", "x"), []string{""}); err == nil {
		t.Error("IssueLeaf should reject whitespace-only SANs")
	}
}
