package connections

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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
// trust-on-first-use: unknown hosts need explicit confirmation. A changed key
// is refused unless ConfirmChanged is set and the user explicitly accepts it.
type HostKeys struct {
	mu   sync.Mutex
	path string
	// Confirm is asked for unknown hosts. It must return true to trust the key.
	Confirm func(host, keyType, fingerprint string) bool
	// ConfirmChanged is asked when a known host presents a different key. old
	// lists the stored keys ("type SHA256:..."), now is the presented one. Only
	// an explicit yes replaces the stored key; nil means "always refuse".
	ConfirmChanged func(host string, old []string, now string) bool
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
			if h.ConfirmChanged == nil {
				return ErrHostKeyChanged
			}
			old := make([]string, 0, len(ke.Want))
			for _, w := range ke.Want {
				old = append(old, w.Key.Type()+" "+ssh.FingerprintSHA256(w.Key))
			}
			if !h.ConfirmChanged(hostname, old, key.Type()+" "+ssh.FingerprintSHA256(key)) {
				return ErrHostKeyChanged
			}
			return h.replace(hostname, key)
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

// replace swaps the stored keys of a host for the presented one: every stored
// line for this host is dropped, then the new key is appended. Hashed host
// entries are not recognised (the app never writes them).
func (h *HostKeys) replace(hostname string, key ssh.PublicKey) error {
	norm := knownhosts.Normalize(hostname)
	data, err := os.ReadFile(h.path)
	if err != nil {
		return err
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && !strings.HasPrefix(line, "#") && !strings.HasPrefix(fields[0], "@") {
			hit := false
			for _, hn := range strings.Split(fields[0], ",") {
				if hn == norm {
					hit = true
				}
			}
			if hit {
				continue
			}
		}
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	kept = append(kept, knownhosts.Line([]string{hostname}, key))
	tmp := h.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, h.path)
}
