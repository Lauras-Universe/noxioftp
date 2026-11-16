package connections

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Host key errors; the frontend matches on the prefix before the colon.
var (
	ErrHostKeyRejected = errors.New("E_HOSTKEY_REJECTED: Der Server-Schlüssel wurde nicht vertraut")
	ErrHostKeyChanged  = errors.New("E_HOSTKEY_CHANGED: ACHTUNG – der Host-Schlüssel dieses Servers hat sich geändert! Möglicherweise ein Man-in-the-Middle-Angriff. Die Verbindung wurde abgebrochen")
)

// HostKeys verifies server keys against a private known_hosts file using
// trust-on-first-use: unknown hosts need explicit confirmation, changed keys
// are always refused.
type HostKeys struct {
	mu   sync.Mutex
	path string
	// Confirm is asked for unknown hosts. It must return true to trust the key.
	Confirm func(host, keyType, fingerprint string) bool
}

// NewHostKeys creates a verifier backed by the file at path.
func NewHostKeys(path string) *HostKeys { return &HostKeys{path: path} }

// Callback returns the ssh.HostKeyCallback.
func (h *HostKeys) Callback() ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		h.mu.Lock()
		defer h.mu.Unlock()

		if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
			return err
		}
		if f, err := os.OpenFile(h.path, os.O_CREATE|os.O_RDONLY, 0o600); err == nil {
			f.Close()
		} else {
			return err
		}
		check, err := knownhosts.New(h.path)
		if err != nil {
			return err
		}
		err = check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		if len(ke.Want) > 0 {
			return ErrHostKeyChanged
		}
		if h.Confirm == nil || !h.Confirm(hostname, key.Type(), ssh.FingerprintSHA256(key)) {
			return ErrHostKeyRejected
		}
		f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = fmt.Fprintln(f, knownhosts.Line([]string{hostname}, key))
		return err
	}
}
