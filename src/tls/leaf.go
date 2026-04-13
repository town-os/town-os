// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Filenames for a per-package leaf certificate. Both files live in the same
// directory so the network controller only needs one mount path per package.
const (
	LeafCertFileName = "cert.pem"
	LeafKeyFileName  = "key.pem"

	leafValidity = 10 * 365 * 24 * time.Hour
)

// IssueLeaf generates (or refreshes) a per-package leaf certificate signed by
// this CA and writes it to outDir. The write is idempotent: when an existing
// certificate already covers exactly the requested SAN set and is still valid
// the function returns without touching disk. This lets reconcile call
// IssueLeaf on every boot without churning cert files.
//
// hostnames may include DNS names and IP literals. Duplicates are deduplicated
// and IP strings are added to IPAddresses; anything that is not a valid IP is
// treated as a DNS name.
func (c *CA) IssueLeaf(outDir string, hostnames []string) error {
	if c == nil {
		return errors.New("tls: ca is nil")
	}
	if outDir == "" {
		return errors.New("tls: leaf out dir is empty")
	}

	dnsNames, ipAddrs := splitHostnames(hostnames)
	if len(dnsNames) == 0 && len(ipAddrs) == 0 {
		return errors.New("tls: no SANs provided for leaf cert")
	}

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("tls: create leaf dir: %w", err)
	}

	certPath := filepath.Join(outDir, LeafCertFileName)
	keyPath := filepath.Join(outDir, LeafKeyFileName)

	// Idempotency: if both files exist, parse the current cert and skip
	// rewrite when its SANs exactly match what we would issue and it is
	// still valid for at least 30 days. Any mismatch falls through to a
	// full re-issue.
	if existing, ok := loadExistingLeaf(certPath, keyPath); ok {
		if leafMatches(existing, dnsNames, ipAddrs) {
			return nil
		}
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("tls: generate leaf key: %w", err)
	}

	serial, err := newSerial()
	if err != nil {
		return err
	}

	commonName := dnsNames[0]
	if commonName == "" && len(ipAddrs) > 0 {
		commonName = ipAddrs[0].String()
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName, Organization: []string{"Town OS"}},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddrs,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, c.Cert, &key.PublicKey, c.Key)
	if err != nil {
		return fmt.Errorf("tls: sign leaf cert: %w", err)
	}

	// Concatenate leaf cert + CA so servers can present the whole chain in
	// one file. The network controller reads exactly this concatenation via
	// tls.LoadX509KeyPair and Go will send both certificates in the handshake.
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	chainPEM := append([]byte{}, leafPEM...)
	chainPEM = append(chainPEM, c.CertPEM...)

	if err := writeFileAtomic(certPath, chainPEM, 0o644); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("tls: marshal leaf key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		return err
	}
	return nil
}

// serialMax is 2^128, the upper bound for random certificate serial numbers.
// Using a full 128-bit random serial satisfies the CAB forum requirement and
// guarantees leaf certs issued in the same microsecond do not collide.
var serialMax = new(big.Int).Lsh(big.NewInt(1), 128)

func newSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		return nil, fmt.Errorf("tls: generate serial: %w", err)
	}
	return serial, nil
}

// splitHostnames partitions a mixed DNS-and-IP host list. Duplicate entries
// are dropped; the resulting slices are sorted so the on-disk SAN order is
// deterministic (and comparable against an existing cert).
func splitHostnames(in []string) (dns []string, ips []net.IP) {
	seenDNS := map[string]bool{}
	seenIP := map[string]bool{}
	for _, h := range in {
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			canon := ip.String()
			if seenIP[canon] {
				continue
			}
			seenIP[canon] = true
			ips = append(ips, ip)
			continue
		}
		if seenDNS[h] {
			continue
		}
		seenDNS[h] = true
		dns = append(dns, h)
	}
	slices.Sort(dns)
	slices.SortFunc(ips, func(a, b net.IP) int {
		switch {
		case a.String() < b.String():
			return -1
		case a.String() > b.String():
			return 1
		default:
			return 0
		}
	})
	return dns, ips
}

// loadExistingLeaf reads a previously-issued leaf off disk. It returns
// (cert, true) only when both files exist and the cert parses cleanly; any
// other state is treated as "no existing leaf" so reconcile rewrites it.
func loadExistingLeaf(certPath, keyPath string) (*x509.Certificate, bool) {
	if _, err := os.Stat(keyPath); err != nil {
		return nil, false
	}
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // G304 -- path derived from trusted tls base
	if err != nil {
		return nil, false
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, false
	}
	return cert, true
}

// leafMatches reports whether an existing cert already covers the exact SAN
// set we would issue and has enough remaining lifetime to skip a re-issue.
func leafMatches(cert *x509.Certificate, dnsNames []string, ipAddrs []net.IP) bool {
	if len(cert.DNSNames) != len(dnsNames) {
		return false
	}
	existingDNS := append([]string(nil), cert.DNSNames...)
	slices.Sort(existingDNS)
	for i := range dnsNames {
		if existingDNS[i] != dnsNames[i] {
			return false
		}
	}

	if len(cert.IPAddresses) != len(ipAddrs) {
		return false
	}
	existingIPs := make([]string, len(cert.IPAddresses))
	for i, ip := range cert.IPAddresses {
		existingIPs[i] = ip.String()
	}
	slices.Sort(existingIPs)
	wantIPs := make([]string, len(ipAddrs))
	for i, ip := range ipAddrs {
		wantIPs[i] = ip.String()
	}
	slices.Sort(wantIPs)
	for i := range wantIPs {
		if existingIPs[i] != wantIPs[i] {
			return false
		}
	}

	// Skip re-issue only when the cert is still valid for a reasonable
	// window. A month is arbitrary but avoids churning after every clock
	// skew while still catching stale certs long before they expire.
	if time.Until(cert.NotAfter) < 30*24*time.Hour {
		return false
	}
	return true
}
