// Package connections stores connection profiles, their secrets and the
// trusted host keys.
package connections

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const keyringService = "sftp-client"

// Auth types.
const (
	AuthPassword = "password"
	AuthKey      = "key"
)

// Protocols.
const (
	ProtoSFTP         = "sftp"
	ProtoFTP          = "ftp"           // plain, unencrypted
	ProtoFTPS         = "ftps"          // explicit TLS (AUTH TLS)
	ProtoFTPSImplicit = "ftps-implicit" // implicit TLS
)

// Profile is a saved server. Secrets (password / key passphrase) are never
// stored here but in the OS keyring.
type Profile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"` // see Proto*, empty = sftp
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	AuthType  string `json:"authType"`
	KeyPath   string `json:"keyPath"`
	RemoteDir string `json:"remoteDir"` // optional start directory
	JumpID    string `json:"jumpId"`    // optional SFTP profile to tunnel through (SFTP only)
	HasSecret bool   `json:"hasSecret"`
}

// Secrets abstracts the OS keyring so it can be replaced in tests.
type Secrets interface {
	Get(id string) (string, error)
	Set(id, secret string) error
	Delete(id string) error
}

// KeyringSecrets stores secrets in the OS keyring.
type KeyringSecrets struct{}

func (KeyringSecrets) Get(id string) (string, error) { return keyring.Get(keyringService, id) }
func (KeyringSecrets) Set(id, s string) error        { return keyring.Set(keyringService, id, s) }
func (KeyringSecrets) Delete(id string) error {
	err := keyring.Delete(keyringService, id)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Store persists profiles as JSON.
type Store struct {
	mu       sync.Mutex
	path     string
	profiles []Profile
	secrets  Secrets
}

// NewStore loads (or creates) the profile file at path.
func NewStore(path string, secrets Secrets) (*Store, error) {
	s := &Store{path: path, secrets: secrets}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(data, &s.profiles); err != nil {
			return nil, fmt.Errorf("%s ist beschädigt: %w", path, err)
		}
		for i := range s.profiles { // profiles from before FTP support
			if s.profiles[i].Protocol == "" {
				s.profiles[i].Protocol = ProtoSFTP
			}
		}
	}
	return s, nil
}

func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.profiles, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns all profiles.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Profile{}, s.profiles...)
}

// Get returns one profile.
func (s *Store) Get(id string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.profiles {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}

func validate(p *Profile) error {
	p.Host = strings.TrimSpace(p.Host)
	p.User = strings.TrimSpace(p.User)
	p.Name = strings.TrimSpace(p.Name)
	if p.Host == "" || p.User == "" {
		return errors.New("Host und Benutzer sind erforderlich")
	}
	switch p.Protocol {
	case "", ProtoSFTP:
		p.Protocol = ProtoSFTP
	case ProtoFTP, ProtoFTPS, ProtoFTPSImplicit:
	default:
		return errors.New("Ungültiges Protokoll")
	}
	if p.Port == 0 {
		switch p.Protocol {
		case ProtoSFTP:
			p.Port = 22
		case ProtoFTPSImplicit:
			p.Port = 990
		default:
			p.Port = 21
		}
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("Ungültiger Port")
	}
	if p.AuthType != AuthPassword && p.AuthType != AuthKey {
		return errors.New("Ungültige Anmeldeart")
	}
	if p.Protocol != ProtoSFTP {
		if p.AuthType != AuthPassword {
			return errors.New("FTP unterstützt nur die Anmeldung per Passwort")
		}
		if p.JumpID != "" {
			return errors.New("Jump-Hosts gibt es nur bei SFTP")
		}
	}
	if p.AuthType == AuthKey && p.KeyPath == "" {
		return errors.New("Für die Schlüssel-Anmeldung wird eine Schlüsseldatei benötigt")
	}
	if p.Name == "" {
		p.Name = p.User + "@" + p.Host
	}
	return nil
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Save creates or updates a profile. A non-empty secret replaces the stored
// password / passphrase; an empty one leaves it unchanged.
func (s *Store) Save(p Profile, secret string) (Profile, error) {
	if err := validate(&p); err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idx := -1
	for i, e := range s.profiles {
		if e.ID == p.ID {
			idx = i
		}
	}
	if p.JumpID != "" {
		if p.JumpID == p.ID {
			return Profile{}, errors.New("Ein Profil kann nicht sein eigener Jump-Host sein")
		}
		found := false
		for _, e := range s.profiles {
			if e.ID == p.JumpID && e.Protocol == ProtoSFTP {
				found = true
			}
		}
		if !found {
			return Profile{}, errors.New("Der Jump-Host muss ein gespeichertes SFTP-Profil sein")
		}
	}
	if idx >= 0 {
		p.HasSecret = s.profiles[idx].HasSecret
	} else {
		p.ID = newID()
		p.HasSecret = false
	}
	if secret != "" {
		if err := s.secrets.Set(p.ID, secret); err != nil {
			return Profile{}, fmt.Errorf("Schlüsselbund: %w", err)
		}
		p.HasSecret = true
	}
	if idx >= 0 {
		s.profiles[idx] = p
	} else {
		s.profiles = append(s.profiles, p)
	}
	return p, s.persist()
}

// Delete removes a profile and its secret.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.profiles {
		if p.ID == id {
			if p.HasSecret {
				if err := s.secrets.Delete(id); err != nil {
					return err
				}
			}
			s.profiles = append(s.profiles[:i], s.profiles[i+1:]...)
			for j := range s.profiles { // profiles that tunnelled through it no longer do
				if s.profiles[j].JumpID == id {
					s.profiles[j].JumpID = ""
				}
			}
			return s.persist()
		}
	}
	return nil
}

// Secret returns the stored secret ("" if there is none).
func (s *Store) Secret(id string) (string, error) {
	p, ok := s.Get(id)
	if !ok || !p.HasSecret {
		return "", nil
	}
	return s.secrets.Get(id)
}
