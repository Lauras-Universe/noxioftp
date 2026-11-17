package ftpclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"

	"sftp-client/internal/connections"
	"sftp-client/internal/remote"
	"sftp-client/internal/transfer"
)

type driver struct {
	root string
	tls  *tls.Config
	ln   net.Listener
	mode ftpserver.TLSRequirement
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	return &ftpserver.Settings{
		Listener:                 d.ln,
		PublicHost:               "127.0.0.1",
		PassiveTransferPortRange: ftpserver.PortRange{Start: 42000, End: 42200},
		TLSRequired:              d.mode,
	}, nil
}
func (d *driver) ClientConnected(ftpserver.ClientContext) (string, error) { return "ok", nil }
func (d *driver) ClientDisconnected(ftpserver.ClientContext)              {}
func (d *driver) GetTLSConfig() (*tls.Config, error) {
	if d.tls == nil {
		return nil, errors.New("no tls")
	}
	return d.tls, nil
}
func (d *driver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if user != "u" || pass != "p" {
		return nil, errors.New("bad credentials")
	}
	return afero.NewBasePathFs(afero.NewOsFs(), d.root), nil
}

func selfSigned(t *testing.T) *tls.Config {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
}

// startServer starts an FTP server on a free loopback port serving a temp dir.
func startServer(t *testing.T, withTLS bool, mode ftpserver.TLSRequirement) (root string, port int) {
	t.Helper()
	root = t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	d := &driver{root: root, ln: ln, mode: mode}
	if withTLS {
		d.tls = selfSigned(t)
	}
	srv := ftpserver.NewFtpServer(d)
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	t.Cleanup(func() { srv.Stop() })
	return root, ln.Addr().(*net.TCPAddr).Port
}

func dialPlain(t *testing.T, port int) *Client {
	t.Helper()
	c, err := Dial(Config{Host: "127.0.0.1", Port: port, User: "u", Password: "p", Mode: Plain, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

// dialTLS connects with explicit TLS to a self-signed test server (certificate auto-trusted).
func dialTLS(t *testing.T, port int) *Client {
	t.Helper()
	pins := connections.NewCertPins(filepath.Join(t.TempDir(), "known_certs"))
	pins.Confirm = func(string, string, string) bool { return true }
	c, err := Dial(Config{Host: "127.0.0.1", Port: port, User: "u", Password: "p", Mode: Explicit, TLS: pins.TLSConfig("127.0.0.1"), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestFTPBasicOperations(t *testing.T) {
	root, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("hallo äöü"), 0o644)
	os.Mkdir(filepath.Join(root, "zdir"), 0o755)
	c := dialPlain(t, port)

	if h, _ := c.Home(); h != "/" {
		t.Fatalf("home = %q", h)
	}
	l, err := c.List("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 || l.Entries[0].Name != "zdir" || !l.Entries[0].IsDir || l.Entries[1].Name != "b.txt" {
		t.Fatalf("entries = %+v", l.Entries)
	}

	if err := c.Mkdir("/new"); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename("/new", "/zdir"); err == nil {
		t.Fatal("rename onto existing must fail")
	}
	if err := c.Rename("/new", "/renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed")); err != nil {
		t.Fatal("rename not applied")
	}

	os.WriteFile(filepath.Join(root, "zdir", "f"), []byte("x"), 0o644)
	if err := c.Delete([]string{"/zdir", "/renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "zdir")); !os.IsNotExist(err) {
		t.Fatal("dir not removed recursively")
	}
	if err := c.Delete([]string{"/"}); err == nil {
		t.Fatal("deleting / must be refused")
	}
}

func TestFTPEditorReadWrite(t *testing.T) {
	root, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	p := filepath.Join(root, "a.txt")
	os.WriteFile(p, []byte("v1 äöü"), 0o644)
	c := dialPlain(t, port)

	tf, err := c.ReadText("/a.txt")
	if err != nil || tf.Content != "v1 äöü" {
		t.Fatalf("ReadText = %+v, %v", tf, err)
	}
	if _, err := c.WriteText("/a.txt", "v2", tf.ModTime, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "v2" {
		t.Fatalf("content = %q", b)
	}

	// somebody else changes the file
	later := time.Now().Add(time.Hour)
	os.WriteFile(p, []byte("fremd"), 0o644)
	os.Chtimes(p, later, later)
	if _, err := c.WriteText("/a.txt", "meins", tf.ModTime, false); !errors.Is(err, remote.ErrModified) {
		t.Fatalf("err = %v, want ErrModified", err)
	}
	if _, err := c.WriteText("/a.txt", "meins", tf.ModTime, true); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(root, "bin"), []byte{'a', 0, 'b'}, 0o644)
	if _, err := c.ReadText("/bin"); !errors.Is(err, remote.ErrBinary) {
		t.Fatalf("binary err = %v", err)
	}
}

func TestFTPTransfersThroughQueue(t *testing.T) {
	root, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	c := dialPlain(t, port)
	local := t.TempDir()
	os.MkdirAll(filepath.Join(local, "tree", "sub"), 0o755)
	os.WriteFile(filepath.Join(local, "tree", "a.txt"), []byte("A"), 0o644)
	os.WriteFile(filepath.Join(local, "tree", "sub", "b.txt"), []byte("BB"), 0o644)
	big := make([]byte, 3<<20)
	for i := range big {
		big[i] = byte(i)
	}
	os.WriteFile(filepath.Join(local, "big.bin"), big, 0o644)

	q := transfer.NewQueue(3, transfer.LocalFS{}, nil)
	q.SetRemote(c.FS())
	wait := func() {
		deadline := time.Now().Add(20 * time.Second)
		for q.ActiveCount() > 0 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
	}
	q.Enqueue(transfer.Upload, "tree", filepath.Join(local, "tree"), "/up/tree", transfer.Overwrite)
	q.Enqueue(transfer.Upload, "big.bin", filepath.Join(local, "big.bin"), "/up/big.bin", transfer.Overwrite)
	wait()
	for _, j := range q.List() {
		if j.Status != transfer.Done {
			t.Fatalf("upload job %+v", j)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "up", "tree", "sub", "b.txt")); string(b) != "BB" {
		t.Fatalf("uploaded content = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "up", "big.bin")); len(b) != len(big) || b[12345] != big[12345] {
		t.Fatal("big file corrupted")
	}

	dl := t.TempDir()
	q.ClearFinished()
	q.Enqueue(transfer.Download, "tree", "/up/tree", filepath.Join(dl, "tree"), transfer.Overwrite)
	q.Enqueue(transfer.Download, "big.bin", "/up/big.bin", filepath.Join(dl, "big.bin"), transfer.Overwrite)
	wait()
	for _, j := range q.List() {
		if j.Status != transfer.Done {
			t.Fatalf("download job %+v", j)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "tree", "a.txt")); string(b) != "A" {
		t.Fatalf("downloaded content = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dl, "big.bin")); len(b) != len(big) {
		t.Fatalf("big download size = %d", len(b))
	}

	// sync plan against FTP: nothing left to do after the uploads
	plan, err := q.PlanSync(context.Background(), local, "/up", transfer.SyncOptions{Direction: transfer.Upload, KeepNewer: true})
	if err != nil {
		t.Fatal(err)
	}
	if plan.CopyCount != 0 {
		t.Fatalf("plan after sync should be empty: %+v", plan.Actions)
	}
}

func TestFTPBadPassword(t *testing.T) {
	_, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	if _, err := Dial(Config{Host: "127.0.0.1", Port: port, User: "u", Password: "wrong", Timeout: 5 * time.Second}); err == nil {
		t.Fatal("login with a wrong password succeeded")
	}
}

func TestFTPSExplicitCertificatePinning(t *testing.T) {
	root, port := startServer(t, true, ftpserver.ClearOrEncrypted)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o644)
	pins := connections.NewCertPins(filepath.Join(t.TempDir(), "known_certs"))
	cfg := func() Config {
		return Config{Host: "127.0.0.1", Port: port, User: "u", Password: "p", Mode: Explicit, TLS: pins.TLSConfig("127.0.0.1"), Timeout: 5 * time.Second}
	}

	// self-signed and unknown: declined -> refused
	pins.Confirm = func(string, string, string) bool { return false }
	if _, err := Dial(cfg()); err == nil {
		t.Fatal("untrusted certificate was accepted")
	}

	// accepted once -> works, and is remembered (also for data connections)
	asked := 0
	pins.Confirm = func(string, string, string) bool { asked++; return true }
	c, err := Dial(cfg())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if l, err := c.List("/"); err != nil || len(l.Entries) != 1 {
		t.Fatalf("list over TLS: %+v, %v", l, err)
	}
	if tf, err := c.ReadText("/f.txt"); err != nil || tf.Content != "x" {
		t.Fatalf("read over TLS: %+v, %v", tf, err)
	}
	pins.Confirm = func(string, string, string) bool { t.Fatal("asked again for a pinned certificate"); return false }
	c2, err := Dial(cfg())
	if err != nil {
		t.Fatalf("pinned certificate rejected: %v", err)
	}
	c2.Close()
	if asked != 1 {
		t.Fatalf("asked %d times", asked)
	}
}

func TestParseModeDefaultsPort(t *testing.T) {
	// implicit TLS defaults to 990, everything else to 21 (verified through the error text of a refused connection)
	for mode, want := range map[Mode]int{Plain: 21, Explicit: 21, Implicit: 990} {
		_, err := Dial(Config{Host: "127.0.0.1", User: "u", Mode: mode, TLS: &tls.Config{}, Timeout: 200 * time.Millisecond})
		if err == nil {
			t.Skip("something listens on the default FTP port here")
		}
		if want == 990 && !contains(err.Error(), "990") || want == 21 && !contains(err.Error(), "21") {
			t.Errorf("mode %s: %v", mode, err)
		}
	}
	_ = strconv.Itoa
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestFTPChmod(t *testing.T) {
	root, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	os.WriteFile(filepath.Join(root, "a b.txt"), []byte("x"), 0o644) // a space in the name
	os.Mkdir(filepath.Join(root, "d"), 0o755)
	c := dialPlain(t, port)

	if err := c.Chmod("/a b.txt", 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(root, "a b.txt")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
	}
	if err := c.Chmod("/d", 0o700); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(root, "d")); fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %v", fi.Mode().Perm())
	}
	// the connection is still usable afterwards (responses are not out of step)
	if l, err := c.List("/"); err != nil || len(l.Entries) != 2 {
		t.Fatalf("list after chmod: %+v, %v", l, err)
	}
	if err := c.Chmod("/missing", 0o644); err == nil {
		t.Fatal("chmod of a missing file succeeded")
	}
	if err := c.Chmod("/x\r\nDELE /a b.txt", 0o644); err == nil {
		t.Fatal("command injection through the path was not rejected")
	}
	if _, err := os.Stat(filepath.Join(root, "a b.txt")); err != nil {
		t.Fatal("file was deleted through an injected command")
	}
}
