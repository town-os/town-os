// IRON RULE: make test-full must always be able to run simultaneously in the
// same repository without conflicting. Nothing else matters more than this.

// Package tls manages a local X.509 certificate authority and per-package leaf
// certificates for Town OS. The CA and its key live under the btrfs root so
// they survive reboots; leaves are re-issued idempotently at install/reconcile
// time. Only the Town OS network controller consumes the leaf certs to
// terminate TLS for HTTP-supplying packages.
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
	"os"
	"path/filepath"
	"time"
)

// Filenames used for the root CA on disk. The cert is world-readable so that
// external clients can fetch it via GET /tls/ca.crt; the key is restricted to
// the owning user and must never be served over the network.
const (
	CAFileName    = "ca.crt"
	CAKeyFileName = "ca.key"

	caValidity = 10 * 365 * 24 * time.Hour
)

// CA is a loaded X.509 certificate authority backed by an ECDSA P-256 key
// pair. It is safe for concurrent use: the Cert/Key fields are immutable
// after load and IssueLeaf only writes to per-package output directories.
type CA struct {
	Dir     string            // directory that holds ca.crt and ca.key
	Cert    *x509.Certificate // parsed CA certificate
	CertPEM []byte            // PEM-encoded CA cert (for GET /tls/ca.crt)
	Key     *ecdsa.PrivateKey // CA signing key
}

// EnsureCA loads an existing CA from dir or generates a new one when the
// files are missing. The directory is created on demand. Returns an error
// when files exist but are unreadable or malformed; callers should treat CA
// failure as non-fatal and log.
func EnsureCA(dir string) (*CA, error) {
	if dir == "" {
		return nil, errors.New("tls: ca dir is empty")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("tls: create ca dir: %w", err)
	}

	certPath := filepath.Join(dir, CAFileName)
	keyPath := filepath.Join(dir, CAKeyFileName)

	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	if certErr == nil && keyErr == nil {
		return loadCA(dir, certPath, keyPath)
	}
	if certErr != nil && !os.IsNotExist(certErr) {
		return nil, fmt.Errorf("tls: stat ca cert: %w", certErr)
	}
	if keyErr != nil && !os.IsNotExist(keyErr) {
		return nil, fmt.Errorf("tls: stat ca key: %w", keyErr)
	}
	// One file missing and the other present is treated as corruption — the
	// safer move is to refuse and let the operator resolve it rather than
	// silently regenerate (which would orphan every previously-issued leaf).
	if certErr == nil {
		return nil, fmt.Errorf("tls: incomplete CA in %s: key missing: %w", dir, keyErr)
	}
	if keyErr == nil {
		return nil, fmt.Errorf("tls: incomplete CA in %s: cert missing: %w", dir, certErr)
	}

	return generateCA(dir, certPath, keyPath)
}

func loadCA(dir, certPath, keyPath string) (*CA, error) {
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // G304 -- path derived from trusted btrfs base
	if err != nil {
		return nil, fmt.Errorf("tls: read ca cert: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("tls: ca cert is not a PEM CERTIFICATE")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("tls: parse ca cert: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath) //nolint:gosec // G304 -- path derived from trusted btrfs base
	if err != nil {
		return nil, fmt.Errorf("tls: read ca key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("tls: ca key is not a PEM block")
	}
	key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("tls: parse ca key: %w", err)
	}

	return &CA{Dir: dir, Cert: cert, CertPEM: certPEM, Key: key}, nil
}

func generateCA(dir, certPath, keyPath string) (_ *CA, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tls: generate ca key: %w", err)
	}

	serial, err := newSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Town OS Root CA",
			Organization: []string{"Town OS"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("tls: create ca cert: %w", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("tls: parse fresh ca cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeFileAtomic(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("tls: marshal ca key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		// Best-effort: remove the cert so we don't leave a cert without a key.
		if rmErr := os.Remove(certPath); rmErr != nil && !os.IsNotExist(rmErr) {
			err = errors.Join(err, fmt.Errorf("tls: cleanup partial ca cert: %w", rmErr))
		}
		return nil, err
	}

	return &CA{Dir: dir, Cert: cert, CertPEM: certPEM, Key: key}, nil
}

// writeFileAtomic writes data to path via a temp file + rename so readers
// never observe a partial file. The temp file lives in the same directory
// as the target so the rename stays on one filesystem.
func writeFileAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("tls: create temp %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			if rmErr := os.Remove(tmpName); rmErr != nil && !os.IsNotExist(rmErr) {
				err = errors.Join(err, fmt.Errorf("tls: remove temp %s: %w", tmpName, rmErr))
			}
		}
	}()

	if _, werr := tmp.Write(data); werr != nil {
		if cerr := tmp.Close(); cerr != nil {
			werr = errors.Join(werr, fmt.Errorf("tls: close temp %s: %w", tmpName, cerr))
		}
		return fmt.Errorf("tls: write temp %s: %w", tmpName, werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("tls: close temp %s: %w", tmpName, cerr)
	}
	if cerr := os.Chmod(tmpName, mode); cerr != nil {
		return fmt.Errorf("tls: chmod temp %s: %w", tmpName, cerr)
	}
	if rerr := os.Rename(tmpName, path); rerr != nil {
		return fmt.Errorf("tls: rename temp %s -> %s: %w", tmpName, path, rerr)
	}
	return nil
}
