package sftpclient

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

// newTestClient connects a Client to an in-process SFTP server that serves the
// real (temporary) file system.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	cr, sw := pipe()
	sr, cw := pipe()
	srv, err := sftp.NewServer(rwc{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	sc, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	c := NewFromSFTP(sc)
	t.Cleanup(func() { srv.Close(); c.Close() })
	return c
}

func TestListSortsDirsFirst(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "A.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "zdir"), 0o755)

	l, err := c.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range l.Entries {
		got = append(got, e.Name)
	}
	want := []string{"zdir", "A.txt", "b.txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if len(l.Crumbs) == 0 || l.Crumbs[0].Path != "/" {
		t.Fatalf("crumbs = %+v", l.Crumbs)
	}
}

func TestReadWriteText(t *testing.T) {
	c := newTestClient(t)
	p := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(p, []byte("hallo äöü"), 0o644)

	tf, err := c.ReadText(p)
	if err != nil || tf.Content != "hallo äöü" {
		t.Fatalf("ReadText = %+v, %v", tf, err)
	}
	mod, err := c.WriteText(p, "neu", tf.ModTime, false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "neu" {
		t.Fatalf("content = %q", b)
	}
	if mod == 0 {
		t.Fatal("modTime not returned")
	}
}

func TestWriteDetectsServerChange(t *testing.T) {
	c := newTestClient(t)
	p := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(p, []byte("v1"), 0o644)
	tf, _ := c.ReadText(p)

	// somebody else changes the file later
	later := time.Unix(tf.ModTime+100, 0)
	os.WriteFile(p, []byte("fremd"), 0o644)
	os.Chtimes(p, later, later)

	if _, err := c.WriteText(p, "meins", tf.ModTime, false); !errors.Is(err, ErrModified) {
		t.Fatalf("err = %v, want ErrModified", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "fremd" {
		t.Fatal("file was overwritten despite conflict")
	}
	if _, err := c.WriteText(p, "meins", tf.ModTime, true); err != nil {
		t.Fatalf("forced write: %v", err)
	}
}

func TestReadTextRejectsBinaryAndLarge(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	os.WriteFile(bin, []byte{'a', 0, 'b'}, 0o644)
	if _, err := c.ReadText(bin); !errors.Is(err, ErrBinary) {
		t.Fatalf("binary: %v", err)
	}
	latin := filepath.Join(dir, "latin")
	os.WriteFile(latin, []byte{0xe4, 0xf6}, 0o644)
	if _, err := c.ReadText(latin); !errors.Is(err, ErrBinary) {
		t.Fatalf("non-utf8: %v", err)
	}
	big := filepath.Join(dir, "big")
	os.WriteFile(big, make([]byte, MaxEditSize+1), 0o644)
	if _, err := c.ReadText(big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
}

func TestDeleteRenameMkdirChmod(t *testing.T) {
	c := newTestClient(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := c.Mkdir(sub); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(sub, "f"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "other"), []byte("x"), 0o644)

	if err := c.Rename(sub, filepath.Join(dir, "other")); err == nil {
		t.Fatal("rename onto existing file must fail")
	}
	if err := c.Chmod(filepath.Join(dir, "other"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "other")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v", fi.Mode().Perm())
	}
	if err := c.Delete([]string{sub}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("dir not removed recursively")
	}
	if err := c.Delete([]string{"/"}); err == nil {
		t.Fatal("deleting / must be refused")
	}
}

func TestDialRequiresHostKeyCallback(t *testing.T) {
	if _, err := Dial(Config{Host: "127.0.0.1", User: "x"}); !errors.Is(err, ErrNoHostCheck) {
		t.Fatalf("err = %v", err)
	}
}
