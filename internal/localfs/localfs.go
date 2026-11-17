// Package localfs lists and modifies the local file system.
package localfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"noxioftp/internal/fsinfo"
)

// Home returns the user's home directory.
func Home() (string, error) { return os.UserHomeDir() }

// Roots returns the file system roots ("/" or the Windows drive letters).
func Roots() []string {
	if runtime.GOOS != "windows" {
		return []string{"/"}
	}
	var roots []string
	for c := 'A'; c <= 'Z'; c++ {
		r := string(c) + `:\`
		if _, err := os.Stat(r); err == nil {
			roots = append(roots, r)
		}
	}
	return roots
}

// List lists a directory. Symlinks to directories are reported as directories.
func List(dir string) (fsinfo.Listing, error) {
	dir = filepath.Clean(dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		return fsinfo.Listing{}, err
	}
	entries := make([]fsinfo.Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue // vanished between ReadDir and Info
		}
		full := filepath.Join(dir, de.Name())
		e := fsinfo.FromInfo(full, info)
		if e.IsLink {
			if st, err := os.Stat(full); err == nil {
				e.IsDir = st.IsDir()
				e.Size = st.Size()
			}
		}
		entries = append(entries, e)
	}
	fsinfo.Sort(entries)
	return fsinfo.Listing{
		Path:    dir,
		Parent:  filepath.Dir(dir),
		Crumbs:  fsinfo.BuildCrumbs(dir, filepath.Dir, filepath.Base),
		Entries: entries,
	}, nil
}

// Mkdir creates a directory.
func Mkdir(p string) error { return os.Mkdir(p, 0o755) }

// Rename renames or moves a file or directory; it never overwrites.
func Rename(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return fmt.Errorf("%s existiert bereits", filepath.Base(to))
	}
	return os.Rename(from, to)
}

// Delete removes files and directories (recursively).
func Delete(paths []string) error {
	for _, p := range paths {
		p = filepath.Clean(p)
		if filepath.Dir(p) == p {
			return errors.New("Wurzelverzeichnis kann nicht gelöscht werden")
		}
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}
