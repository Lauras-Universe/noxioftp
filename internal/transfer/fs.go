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
	Join(elem ...string) string
	Split(p string) (dir, base string)
}

// LocalFS is the local disk.
type LocalFS struct{}

func (LocalFS) Stat(p string) (fs.FileInfo, error)      { return os.Stat(p) }
func (LocalFS) Open(p string) (io.ReadCloser, error)    { return os.Open(p) }
func (LocalFS) Create(p string) (io.WriteCloser, error) { return os.Create(p) }
func (LocalFS) MkdirAll(p string) error                 { return os.MkdirAll(p, 0o755) }
func (LocalFS) Remove(p string) error                   { return os.Remove(p) }
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
func (r RemoteFS) Join(elem ...string) string              { return path.Join(elem...) }
func (r RemoteFS) Split(p string) (string, string)         { return path.Dir(p), path.Base(p) }
