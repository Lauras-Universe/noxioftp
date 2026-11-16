package localfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndSymlink(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("12345"), 0o644)
	os.Mkdir(filepath.Join(dir, "d"), 0o755)
	os.Symlink(filepath.Join(dir, "d"), filepath.Join(dir, "link"))

	l, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 3 || l.Entries[0].Name != "d" || l.Entries[1].Name != "link" || l.Entries[2].Name != "b.txt" {
		t.Fatalf("entries = %+v", l.Entries)
	}
	if !l.Entries[1].IsDir || !l.Entries[1].IsLink {
		t.Fatalf("symlink to dir should be dir+link: %+v", l.Entries[1])
	}
	if l.Entries[2].Size != 5 {
		t.Fatalf("size = %d", l.Entries[2].Size)
	}
	if l.Parent != filepath.Dir(dir) {
		t.Fatalf("parent = %q", l.Parent)
	}
}

func TestRenameRefusesOverwriteAndDeleteRoot(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	if err := Rename(a, b); err == nil {
		t.Fatal("rename overwrote existing file")
	}
	if err := Delete([]string{string(filepath.Separator)}); err == nil {
		t.Fatal("deleting root must be refused")
	}
	if err := Delete([]string{a, b}); err != nil {
		t.Fatal(err)
	}
	if l, _ := List(dir); len(l.Entries) != 0 {
		t.Fatal("not deleted")
	}
}
