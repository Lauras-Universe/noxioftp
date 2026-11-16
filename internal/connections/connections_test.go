package connections

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

type memSecrets map[string]string

func (m memSecrets) Get(id string) (string, error) { return m[id], nil }
func (m memSecrets) Set(id, s string) error        { m[id] = s; return nil }
func (m memSecrets) Delete(id string) error        { delete(m, id); return nil }

func TestStoreSaveLoadDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "connections.json")
	sec := memSecrets{}
	s, err := NewStore(path, sec)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Save(Profile{Host: " example.org ", User: "me", AuthType: AuthPassword}, "geheim")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Port != 22 || p.Name != "me@example.org" || !p.HasSecret {
		t.Fatalf("profile = %+v", p)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) == "" || contains(raw, "geheim") {
		t.Fatal("secret leaked into the JSON file")
	}

	// reload from disk
	s2, err := NewStore(path, sec)
	if err != nil || len(s2.List()) != 1 {
		t.Fatalf("reload: %v %v", s2.List(), err)
	}
	if got, _ := s2.Secret(p.ID); got != "geheim" {
		t.Fatalf("secret = %q", got)
	}

	// update without a new secret keeps the old one
	p.Name = "Neu"
	if _, err := s2.Save(p, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s2.Secret(p.ID); got != "geheim" {
		t.Fatal("secret lost on update")
	}

	if err := s2.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(s2.List()) != 0 || len(sec) != 0 {
		t.Fatal("profile or secret not deleted")
	}
}

func contains(b []byte, s string) bool {
	return len(s) > 0 && string(b) != "" && indexOf(string(b), s) >= 0
}
func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

func TestValidation(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "c.json"), memSecrets{})
	bad := []Profile{
		{User: "u", AuthType: AuthPassword},
		{Host: "h", AuthType: AuthPassword},
		{Host: "h", User: "u", AuthType: "magic"},
		{Host: "h", User: "u", AuthType: AuthKey},
		{Host: "h", User: "u", AuthType: AuthPassword, Port: 70000},
	}
	for i, p := range bad {
		if _, err := s.Save(p, ""); err == nil {
			t.Errorf("case %d accepted: %+v", i, p)
		}
	}
}

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestHostKeysTOFU(t *testing.T) {
	h := NewHostKeys(filepath.Join(t.TempDir(), "known_hosts"))
	addr := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 22}
	key := testKey(t)

	// unknown + declined
	asked := 0
	h.Confirm = func(string, string, string) bool { asked++; return false }
	if err := h.Callback()("example.org:22", addr, key); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("declined: %v", err)
	}

	// unknown + accepted -> remembered, no second prompt
	h.Confirm = func(string, string, string) bool { asked++; return true }
	if err := h.Callback()("example.org:22", addr, key); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	h.Confirm = func(string, string, string) bool { t.Fatal("asked again for known host"); return false }
	if err := h.Callback()("example.org:22", addr, key); err != nil {
		t.Fatalf("known host: %v", err)
	}

	// changed key is always refused, even if the user would say yes
	h.Confirm = func(string, string, string) bool { return true }
	if err := h.Callback()("example.org:22", addr, testKey(t)); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("changed: %v", err)
	}
	if asked != 2 {
		t.Fatalf("asked %d times", asked)
	}
}

func TestHostKeysNoConfirmRefuses(t *testing.T) {
	h := NewHostKeys(filepath.Join(t.TempDir(), "kh"))
	addr := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 22}
	if err := h.Callback()("h:22", addr, testKey(t)); !errors.Is(err, ErrHostKeyRejected) {
		t.Fatalf("err = %v", err)
	}
}
