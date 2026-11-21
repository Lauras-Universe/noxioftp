package transfer

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/sftp"
)

// FS is the small file system interface the queue needs. It is implemented for
// the local disk and for an SFTP connection.
type FS interface {
	Stat(p string) (fs.FileInfo, error)
	Open(p string) (io.ReadCloser, error)
	Create(p string) (io.WriteCloser, error)
	MkdirAll(p string) error
	ReadDir(p string) ([]fs.FileInfo, error)
	Remove(p string) error
	RemoveAll(p string) error
	// Replace moves from onto to, overwriting an existing file. Writing a new
	// file and replacing the target afterwards works even where the target is
	// busy (a running program on Linux refuses to be truncated).
	Replace(from, to string) error
	// Real resolves symlinks and returns the canonical path.
	Real(p string) (string, error)
	Join(elem ...string) string
	Split(p string) (dir, base string)
}

// chmodFS is implemented by file systems that can keep the permissions of a
// file that gets replaced.
type chmodFS interface {
	Chmod(p string, mode fs.FileMode) error
}

// LocalFS is the local disk.
type LocalFS struct{}

func (LocalFS) Stat(p string) (fs.FileInfo, error)      { return os.Stat(p) }
func (LocalFS) Open(p string) (io.ReadCloser, error)    { return os.Open(p) }
func (LocalFS) Create(p string) (io.WriteCloser, error) { return os.Create(p) }
func (LocalFS) MkdirAll(p string) error                 { return os.MkdirAll(p, 0o755) }
func (LocalFS) Remove(p string) error                   { return os.Remove(p) }
func (LocalFS) RemoveAll(p string) error                { return os.RemoveAll(p) }
func (LocalFS) Replace(from, to string) error           { return os.Rename(from, to) }
func (LocalFS) Chmod(p string, m fs.FileMode) error     { return os.Chmod(p, m) }
func (LocalFS) Real(p string) (string, error)           { return filepath.EvalSymlinks(p) }
func (LocalFS) Join(elem ...string) string              { return filepath.Join(elem...) }
func (LocalFS) Split(p string) (string, string)         { return filepath.Dir(p), filepath.Base(p) }
func (LocalFS) ReadDir(p string) ([]fs.FileInfo, error) {
	des, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, 0, len(des))
	for _, de := range des {
		if fi, err := de.Info(); err == nil {
			out = append(out, fi)
		}
	}
	return out, nil
}

// RemoteFS is an SFTP connection.
type RemoteFS struct{ C *sftp.Client }

func (r RemoteFS) Stat(p string) (fs.FileInfo, error)      { return r.C.Stat(p) }
func (r RemoteFS) Open(p string) (io.ReadCloser, error)    { return r.C.Open(p) }
func (r RemoteFS) Create(p string) (io.WriteCloser, error) { return r.C.Create(p) }
func (r RemoteFS) MkdirAll(p string) error                 { return r.C.MkdirAll(p) }
func (r RemoteFS) ReadDir(p string) ([]fs.FileInfo, error) { return r.C.ReadDir(p) }
func (r RemoteFS) Remove(p string) error                   { return r.C.Remove(p) }
func (r RemoteFS) RemoveAll(p string) error                { return r.C.RemoveAll(p) }
func (r RemoteFS) Chmod(p string, m fs.FileMode) error     { return r.C.Chmod(p, m) }

// Replace uses the atomic posix-rename extension where the server has it and
// falls back to delete + rename otherwise.
func (r RemoteFS) Replace(from, to string) error {
	if err := r.C.PosixRename(from, to); err == nil {
		return nil
	}
	if err := r.C.Remove(to); err != nil {
		if _, serr := r.C.Lstat(to); serr == nil {
			return err
		}
	}
	return r.C.Rename(from, to)
}

func (r RemoteFS) Real(p string) (string, error)   { return r.C.RealPath(p) }
func (r RemoteFS) Join(elem ...string) string      { return path.Join(elem...) }
func (r RemoteFS) Split(p string) (string, string) { return path.Dir(p), path.Base(p) }
