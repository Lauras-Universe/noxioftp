package ftpclient

import (
	"os"
	"path/filepath"
	"testing"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

func TestPermBits(t *testing.T) {
	for in, want := range map[string]uint32{
		"rw-r--r--": 0o644, "rwxr-xr-x": 0o755, "---------": 0, "rwx------": 0o700,
		"rwsr-xr-x": 0o4755, "rwxr-sr-x": 0o2755, "rwxrwxrwt": 0o1777,
		"rwSr--r--": 0o4644, "rwxr-Sr-x": 0o2745, "rwxr-xr-T": 0o1754,
	} {
		if got := permBits(in); got != want {
			t.Errorf("permBits(%q) = %o, want %o", in, got, want)
		}
	}
}

func TestParseModes(t *testing.T) {
	listing := "total 12\r\n" +
		"drwxr-xr-x   2 ftp      ftp          4096 Nov 16 21:19 docs\r\n" +
		"-rw-r--r--   1 ftp      ftp            11 Nov 16 21:19 read me.txt\r\n" + // space in name
		"-rwsr-xr-x   1 root     wheel       12345 Jan  3  2021 old tool\r\n" + // year instead of time
		"lrwxrwxrwx   1 ftp      ftp             8 Nov 16 21:19 link -> docs\r\n" +
		"-rw-r-----   1 owner         99 2026-11-16 21:19 nogroup\r\n" + // no group column, ISO date
		"drwxrwxr-x+  3 ftp      ftp          4096 Nov 16 21:19 acl\r\n" + // ACL marker
		"11-16-26  09:19PM       <DIR>          iis-style\r\n" + // DOS style: no permissions
		"garbage line\r\n"
	got := parseModes(listing)
	want := map[string]modeInfo{
		"docs":        {"drwxr-xr-x", 0o755},
		"read me.txt": {"-rw-r--r--", 0o644},
		"old tool":    {"-rwsr-xr-x", 0o4755},
		"link":        {"lrwxrwxrwx", 0o777},
		"nogroup":     {"-rw-r-----", 0o640},
		"acl":         {"drwxrwxr-x", 0o775},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d entries: %v", len(got), got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%q = %+v, want %+v", k, got[k], w)
		}
	}
}

func TestFTPListShowsPermissions(t *testing.T) {
	root, port := startServer(t, false, ftpserver.ClearOrEncrypted)
	os.WriteFile(filepath.Join(root, ".hidden"), []byte("h"), 0o640)
	os.WriteFile(filepath.Join(root, "a b.txt"), []byte("x"), 0o600)
	os.Mkdir(filepath.Join(root, "d"), 0o750)
	os.Chmod(filepath.Join(root, "d"), 0o750)
	c := dialPlain(t, port)

	l, err := c.List("/")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	perm := map[string]uint32{}
	for _, e := range l.Entries {
		got[e.Name], perm[e.Name] = e.Mode, e.Perm
	}
	if got["d"] != "drwxr-x---" || perm["d"] != 0o750 {
		t.Errorf("d = %q %o", got["d"], perm["d"])
	}
	if got["a b.txt"] != "-rw-------" || perm["a b.txt"] != 0o600 {
		t.Errorf("a b.txt = %q %o", got["a b.txt"], perm["a b.txt"])
	}
	if got[".hidden"] != "-rw-r-----" {
		t.Errorf(".hidden = %q (dot files must be included)", got[".hidden"])
	}

	// after chmod the listing reflects the new mode, and the connection stays usable
	if err := c.Chmod("/a b.txt", 0o644); err != nil {
		t.Fatal(err)
	}
	l, err = c.List("/")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range l.Entries {
		if e.Name == "a b.txt" && (e.Mode != "-rw-r--r--" || e.Perm != 0o644) {
			t.Errorf("after chmod: %q %o", e.Mode, e.Perm)
		}
	}
	for i := 0; i < 5; i++ { // repeated listings keep the control connection in step
		if _, err := c.List("/"); err != nil {
			t.Fatalf("list #%d: %v", i, err)
		}
	}
}

func TestFTPSListShowsPermissions(t *testing.T) {
	root, port := startServer(t, true, ftpserver.ClearOrEncrypted)
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("x"), 0o640)
	c := dialTLS(t, port)
	l, err := c.List("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 || l.Entries[0].Mode != "-rw-r-----" || l.Entries[0].Perm != 0o640 {
		t.Fatalf("entries over TLS: %+v", l.Entries)
	}
}

func TestDropNestedRemovesRecursiveListing(t *testing.T) {
	mk := func(names ...string) []info {
		out := make([]info, len(names))
		for i, n := range names {
			out[i] = info{name: n, isDir: true}
		}
		return out
	}
	// what a server answering LIST recursively (Windows style separators) sends
	in := mk("languages", "languages\\plugins", "languages\\themes", "plugins", "plugins\\bbpress",
		"plugins\\bbpress\\includes\\admin\\assets", "wp-config.php", "uploads/2026/11")
	got := dropNested(in)
	want := []string{"languages", "plugins", "wp-config.php", "uploads/2026/11"} // the last has no "uploads" sibling: kept
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		if got[i].name != w {
			t.Errorf("entry %d = %q, want %q", i, got[i].name, w)
		}
	}
	// a normal listing is untouched, including names that contain a backslash
	plain := mk("a", "b", "odd\\name")
	if got := dropNested(plain); len(got) != 3 {
		t.Errorf("plain listing changed: %v", got)
	}
}
