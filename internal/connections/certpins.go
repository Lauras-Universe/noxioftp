package connections

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrCertRejected / ErrCertChanged are returned for untrusted TLS certificates.
var (
	ErrCertRejected = errors.New("E_CERT_REJECTED: Das Zertifikat des Servers wurde nicht vertraut")
	ErrCertChanged  = errors.New("E_CERT_CHANGED: ACHTUNG – das Zertifikat dieses Servers hat sich geändert! Die Verbindung wurde abgebrochen")
)

// CertPins verifies FTPS server certificates. Certificates that chain to a
// trusted CA are accepted as usual. Everything else (self-signed, private CA)
// falls back to trust-on-first-use: the SHA-256 fingerprint must be confirmed
// once, is stored, and any later change is refused.
type CertPins struct {
	mu   sync.Mutex
	path string
	// Confirm is asked for unknown certificates. verifyErr says why the
	// certificate is not trusted by the system.
	Confirm func(host, fingerprint, verifyErr string) bool
}

// NewCertPins creates a verifier backed by the file at path.
func NewCertPins(path string) *CertPins { return &CertPins{path: path} }

func fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (c *CertPins) lookup(host string) (string, error) {
	f, err := os.Open(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if h, fp, ok := strings.Cut(strings.TrimSpace(sc.Text()), " "); ok && h == host {
			return fp, nil
		}
	}
	return "", sc.Err()
}

func (c *CertPins) store(host, fp string) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(c.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", host, fp)
	return err
}

// TLSConfig returns a tls.Config for connecting to host.
func (c *CertPins) TLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		ClientSessionCache: tls.NewLRUClientSessionCache(32), // many servers require session reuse on data connections
		// Chain verification is done below so that pinned certificates are accepted too.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("der Server hat kein Zertifikat gesendet")
			}
			leaf := cs.PeerCertificates[0]
			inter := x509.NewCertPool()
			for _, ic := range cs.PeerCertificates[1:] {
				inter.AddCert(ic)
			}
			_, verr := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter})
			if verr == nil {
				return nil
			}

			c.mu.Lock()
			defer c.mu.Unlock()
			fp := fingerprint(leaf)
			pinned, err := c.lookup(host)
			if err != nil {
				return err
			}
			switch {
			case pinned == fp:
				return nil
			case pinned != "":
				return ErrCertChanged
			}
			if c.Confirm == nil || !c.Confirm(host, fp, verr.Error()) {
				return ErrCertRejected
			}
			return c.store(host, fp)
		},
	}
}
