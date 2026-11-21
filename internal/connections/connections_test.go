package connections

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestProtocolsAndJumpHosts(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "c.json"), memSecrets{})

	// defaults per protocol
	for proto, port := range map[string]int{"": 22, ProtoSFTP: 22, ProtoFTP: 21, ProtoFTPS: 21, ProtoFTPSImplicit: 990} {
		p, err := s.Save(Profile{Host: "h", User: "u", AuthType: AuthPassword, Protocol: proto}, "")
		if err != nil || p.Port != port {
			t.Errorf("protocol %q: port %d, err %v", proto, p.Port, err)
		}
	}
	if _, err := s.Save(Profile{Host: "h", User: "u", AuthType: AuthPassword, Protocol: "gopher"}, ""); err == nil {
		t.Error("unknown protocol accepted")
	}
	if _, err := s.Save(Profile{Host: "h", User: "u", AuthType: AuthKey, KeyPath: "k", Protocol: ProtoFTP}, ""); err == nil {
		t.Error("key auth accepted for FTP")
	}

	jump, err := s.Save(Profile{Name: "bastion", Host: "b", User: "u", AuthType: AuthPassword}, "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Save(Profile{Host: "inner", User: "u", AuthType: AuthPassword, JumpID: jump.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Profile{Host: "h", User: "u", AuthType: AuthPassword, Protocol: ProtoFTP, JumpID: jump.ID}, ""); err == nil {
		t.Error("jump host accepted for FTP")
	}
	if _, err := s.Save(Profile{Host: "h", User: "u", AuthType: AuthPassword, JumpID: "nope"}, ""); err == nil {
		t.Error("unknown jump profile accepted")
	}
	self := p
	self.JumpID = p.ID
	if _, err := s.Save(self, ""); err == nil {
		t.Error("profile accepted as its own jump host")
	}

	// deleting the bastion clears the reference
	if err := s.Delete(jump.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(p.ID); got.JumpID != "" {
		t.Errorf("dangling jump reference: %q", got.JumpID)
	}
}

func TestOldProfilesWithoutProtocolBecomeSFTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	os.WriteFile(path, []byte(`[{"id":"1","name":"alt","host":"h","port":22,"user":"u","authType":"password"}]`), 0o600)
	s, err := NewStore(path, memSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get("1"); got.Protocol != ProtoSFTP {
		t.Fatalf("protocol = %q", got.Protocol)
	}
}

func TestHostKeysChangedKeyCanBeAcceptedExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	h := NewHostKeys(path)
	addr := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 22}
	oldKey, newKey, otherKey := testKey(t), testKey(t), testKey(t)

	h.Confirm = func(string, string, string) bool { return true }
	for host, k := range map[string]ssh.PublicKey{"example.org:22": oldKey, "other.example:22": otherKey} {
		if err := h.Callback()(host, addr, k); err != nil {
			t.Fatal(err)
		}
	}

	// refused by default (no ConfirmChanged), and when the user says no
	if err := h.Callback()("example.org:22", addr, newKey); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("default: %v", err)
	}
	var gotOld []string
	var gotNow string
	h.ConfirmChanged = func(host string, old []string, now string) bool { gotOld, gotNow = old, now; return false }
	if err := h.Callback()("example.org:22", addr, newKey); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("declined: %v", err)
	}
	if len(gotOld) != 1 || gotOld[0] != oldKey.Type()+" "+ssh.FingerprintSHA256(oldKey) || gotNow != newKey.Type()+" "+ssh.FingerprintSHA256(newKey) {
		t.Fatalf("dialog got old=%v now=%q", gotOld, gotNow)
	}
	// still the old key afterwards
	if err := h.Callback()("example.org:22", addr, oldKey); err != nil {
		t.Fatalf("old key lost after a declined change: %v", err)
	}

	// accepted: the new key is stored, the old one is gone, other hosts are untouched
	h.ConfirmChanged = func(string, []string, string) bool { return true }
	if err := h.Callback()("example.org:22", addr, newKey); err != nil {
		t.Fatalf("accepted: %v", err)
	}
	h.ConfirmChanged = nil
	if err := h.Callback()("example.org:22", addr, newKey); err != nil {
		t.Fatalf("new key not remembered: %v", err)
	}
	if err := h.Callback()("example.org:22", addr, oldKey); !errors.Is(err, ErrHostKeyChanged) {
		t.Fatalf("old key still accepted: %v", err)
	}
	if err := h.Callback()("other.example:22", addr, otherKey); err != nil {
		t.Fatalf("another host was affected: %v", err)
	}
	if b, _ := os.ReadFile(path); strings.Count(string(b), "\n") != 2 {
		t.Fatalf("known_hosts should have exactly 2 lines:\n%s", b)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts permissions = %v", fi.Mode().Perm())
	}
}
