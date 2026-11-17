// Package ftpclient implements remote.Session for FTP and FTPS (explicit and
// implicit TLS). FTP connections handle one operation at a time, so the client
// keeps a small pool of connections and uses one per operation / transfer.
package ftpclient

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/textproto"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"

	"sftp-client/internal/fsinfo"
	"sftp-client/internal/remote"
	"sftp-client/internal/transfer"
)

// Mode selects the transport security.
type Mode string

const (
	Plain    Mode = "plain"    // unencrypted FTP
	Explicit Mode = "explicit" // FTPS: AUTH TLS on the normal port
	Implicit Mode = "implicit" // FTPS: TLS from the first byte (port 990)
)

// Config describes how to connect.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	Mode     Mode
	TLS      *tls.Config // required for Explicit / Implicit
	Timeout  time.Duration
	MaxConns int // parallel connections, default 4
}

// Client is a pooled FTP connection.
type Client struct {
	cfg  Config
	home string

	sem    chan struct{} // limits open connections
	mu     sync.Mutex
	idle   []*ftp.ServerConn
	done   chan struct{}
	closed sync.Once
}

var _ remote.Session = (*Client)(nil)

func (cfg Config) dial() (*ftp.ServerConn, error) {
	port := cfg.Port
	if port == 0 {
		port = 21
		if cfg.Mode == Implicit {
			port = 990
		}
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	opts := []ftp.DialOption{ftp.DialWithTimeout(timeout)}
	switch cfg.Mode {
	case Explicit, Implicit:
		if cfg.TLS == nil {
			return nil, errors.New("TLS-Konfiguration fehlt")
		}
		if cfg.Mode == Explicit {
			opts = append(opts, ftp.DialWithExplicitTLS(cfg.TLS))
		} else {
			opts = append(opts, ftp.DialWithTLS(cfg.TLS))
		}
	case Plain, "":
	default:
		return nil, fmt.Errorf("unbekannter FTP-Modus %q", cfg.Mode)
	}
	c, err := ftp.Dial(net.JoinHostPort(cfg.Host, strconv.Itoa(port)), opts...)
	if err != nil {
		return nil, err
	}
	if err := c.Login(cfg.User, cfg.Password); err != nil {
		c.Quit()
		return nil, err
	}
	return c, nil
}

// Dial connects and logs in once to verify the credentials.
func Dial(cfg Config) (*Client, error) {
	if cfg.MaxConns < 1 {
		cfg.MaxConns = 4
	}
	c, err := cfg.dial()
	if err != nil {
		return nil, err
	}
	home, err := c.CurrentDir()
	if err != nil || home == "" {
		home = "/"
	}
	cl := &Client{cfg: cfg, home: home, sem: make(chan struct{}, cfg.MaxConns), done: make(chan struct{})}
	cl.sem <- struct{}{} // the connection we hold counts against the limit
	cl.idle = append(cl.idle, c)
	return cl, nil
}

var errClosed = errors.New("Verbindung getrennt")

func (c *Client) acquire() (*ftp.ServerConn, error) {
	select {
	case c.sem <- struct{}{}:
	case <-c.done:
		return nil, errClosed
	}
	c.mu.Lock()
	for len(c.idle) > 0 {
		conn := c.idle[len(c.idle)-1]
		c.idle = c.idle[:len(c.idle)-1]
		c.mu.Unlock()
		if conn.NoOp() == nil {
			return conn, nil
		}
		conn.Quit()
		c.mu.Lock()
	}
	c.mu.Unlock()
	conn, err := c.cfg.dial()
	if err != nil {
		<-c.sem
		return nil, err
	}
	return conn, nil
}

// release returns a connection to the pool, or drops it if it is unusable.
func (c *Client) release(conn *ftp.ServerConn) {
	defer func() { <-c.sem }()
	select {
	case <-c.done:
		conn.Quit()
		return
	default:
	}
	if conn.NoOp() != nil {
		conn.Quit()
		return
	}
	c.mu.Lock()
	c.idle = append(c.idle, conn)
	c.mu.Unlock()
}

// with runs fn on a pooled connection.
func (c *Client) with(fn func(*ftp.ServerConn) error) error {
	conn, err := c.acquire()
	if err != nil {
		return err
	}
	defer c.release(conn)
	return fn(conn)
}

// Done is closed after Close.
func (c *Client) Done() <-chan struct{} { return c.done }

// Closed reports whether Close was called.
func (c *Client) Closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Close closes all connections.
func (c *Client) Close() {
	c.closed.Do(func() {
		close(c.done)
		c.mu.Lock()
		for _, conn := range c.idle {
			conn.Quit()
		}
		c.idle = nil
		c.mu.Unlock()
	})
}

func (c *Client) Home() (string, error) { return c.home, nil }

// ----------------------------------------------------------- entry helpers

type info struct {
	name  string
	size  int64
	mod   time.Time
	isDir bool
}

func (i info) Name() string { return i.name }
func (i info) Size() int64  { return i.size }
func (i info) Mode() fs.FileMode {
	if i.isDir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i info) ModTime() time.Time { return i.mod }
func (i info) IsDir() bool        { return i.isDir }
func (i info) Sys() any           { return nil }

// toInfo converts a listing entry; symlinks are resolved to what they point to.
func toInfo(conn *ftp.ServerConn, dir string, e *ftp.Entry) (info, bool) {
	switch e.Type {
	case ftp.EntryTypeFolder:
		return info{e.Name, 0, e.Time, true}, true
	case ftp.EntryTypeFile:
		return info{e.Name, int64(e.Size), e.Time, false}, true
	case ftp.EntryTypeLink:
		full := path.Join(dir, e.Name)
		if e.Target != "" {
			full = e.Target
			if !path.IsAbs(full) {
				full = path.Join(dir, full)
			}
		}
		if conn.ChangeDir(full) == nil {
			return info{e.Name, 0, e.Time, true}, true
		}
		if sz, err := conn.FileSize(full); err == nil {
			return info{e.Name, sz, e.Time, false}, true
		}
		return info{}, false // dangling
	}
	return info{}, false
}

func listInfos(conn *ftp.ServerConn, dir string) ([]info, error) {
	entries, err := conn.List(dir)
	if err != nil {
		return nil, err
	}
	out := make([]info, 0, len(entries))
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		if i, ok := toInfo(conn, dir, e); ok {
			out = append(out, i)
		}
	}
	return dropNested(out), nil
}

// dropNested removes the descendants from a recursive listing. Some servers
// answer LIST with the whole tree below the directory, using relative names
// such as "plugins\bbpress\includes". Entries whose first path segment is
// another entry of the same listing are such descendants; only the direct
// children remain.
func dropNested(list []info) []info {
	top := make(map[string]bool, len(list))
	for _, i := range list {
		if !strings.ContainsAny(i.name, `/\`) {
			top[i.name] = true
		}
	}
	out := make([]info, 0, len(list))
	for _, i := range list {
		if k := strings.IndexAny(i.name, `/\`); k > 0 && top[i.name[:k]] {
			continue
		}
		out = append(out, i)
	}
	return out
}

// stat finds one file or directory. MLST is used when available, otherwise
// the parent directory is listed.
func stat(conn *ftp.ServerConn, p string) (info, error) {
	p = path.Clean(p)
	if p == "/" {
		return info{name: "/", isDir: true}, nil
	}
	if e, err := conn.GetEntry(p); err == nil && e != nil {
		e.Name = path.Base(p)
		if i, ok := toInfo(conn, path.Dir(p), e); ok {
			return i, nil
		}
	}
	list, err := listInfos(conn, path.Dir(p))
	if err != nil {
		var te *textproto.Error
		if errors.As(err, &te) && te.Code == 550 { // parent directory does not exist (or is unreadable)
			return info{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
		}
		return info{}, err
	}
	for _, i := range list {
		if i.name == path.Base(p) {
			return i, nil
		}
	}
	return info{}, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

// ------------------------------------------------------------ remote.Session

func (c *Client) List(dir string) (fsinfo.Listing, error) {
	dir = path.Clean(dir)
	var infos []info
	var modes map[string]modeInfo
	err := c.with(func(conn *ftp.ServerConn) (err error) {
		if infos, err = listInfos(conn, dir); err != nil {
			return err
		}
		modes = c.modes(conn, dir) // permissions: best effort, see listmode.go
		return nil
	})
	if err != nil {
		return fsinfo.Listing{}, err
	}
	entries := make([]fsinfo.Entry, 0, len(infos))
	for _, i := range infos {
		e := fsinfo.FromInfo(path.Join(dir, i.name), i)
		e.Mode, e.Perm = "", 0 // unknown unless the server's LIST output told us
		if m, ok := modes[i.name]; ok {
			e.Mode, e.Perm = m.mode, m.perm
		}
		entries = append(entries, e)
	}
	fsinfo.Sort(entries)
	return fsinfo.Listing{
		Path:    dir,
		Parent:  path.Dir(dir),
		Crumbs:  fsinfo.BuildCrumbs(dir, path.Dir, path.Base),
		Entries: entries,
	}, nil
}

func (c *Client) Mkdir(p string) error {
	return c.with(func(conn *ftp.ServerConn) error { return conn.MakeDir(p) })
}

func (c *Client) Rename(from, to string) error {
	return c.with(func(conn *ftp.ServerConn) error {
		if _, err := stat(conn, to); err == nil {
			return fmt.Errorf("%s existiert bereits", path.Base(to))
		}
		return conn.Rename(from, to)
	})
}

func (c *Client) Delete(paths []string) error {
	return c.with(func(conn *ftp.ServerConn) error {
		for _, p := range paths {
			if err := removeAll(conn, p); err != nil {
				return err
			}
		}
		return nil
	})
}

func removeAll(conn *ftp.ServerConn, p string) error {
	p = path.Clean(p)
	if p == "/" || p == "." {
		return errors.New("Wurzelverzeichnis kann nicht gelöscht werden")
	}
	i, err := stat(conn, p)
	if err != nil {
		return err
	}
	if i.isDir {
		return conn.RemoveDirRecur(p)
	}
	return conn.Delete(p)
}

func (c *Client) ReadText(p string) (remote.TextFile, error) {
	var out remote.TextFile
	err := c.with(func(conn *ftp.ServerConn) error {
		i, err := stat(conn, p)
		if err != nil {
			return err
		}
		if i.size > remote.MaxEditSize {
			return remote.ErrTooLarge
		}
		r, err := conn.Retr(p)
		if err != nil {
			return err
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, remote.MaxEditSize+1))
		if err != nil {
			return err
		}
		if err := remote.CheckText(data); err != nil {
			return err
		}
		out = remote.TextFile{Content: string(data), ModTime: i.mod.Unix(), Size: int64(len(data))}
		return nil
	})
	return out, err
}

func (c *Client) WriteText(p, content string, expectedMod int64, force bool) (int64, error) {
	var mod int64
	err := c.with(func(conn *ftp.ServerConn) error {
		if !force {
			if i, err := stat(conn, p); err == nil && i.mod.Unix() != expectedMod {
				return remote.ErrModified
			}
		}
		if err := conn.Stor(p, bytes.NewReader([]byte(content))); err != nil {
			return err
		}
		i, err := stat(conn, p)
		if err != nil {
			return err
		}
		mod = i.mod.Unix()
		return nil
	})
	return mod, err
}

// ------------------------------------------------------------------- FS

// FS returns the file system view used by the transfer queue.
func (c *Client) FS() transfer.FS { return &fsView{c} }

type fsView struct{ c *Client }

func (f *fsView) Join(elem ...string) string      { return path.Join(elem...) }
func (f *fsView) Split(p string) (string, string) { return path.Dir(p), path.Base(p) }

// Real: FTP has no portable way to resolve symlinks. Loops are cut by the
// walker's depth limit instead.
func (f *fsView) Real(p string) (string, error) { return path.Clean(p), nil }

func (f *fsView) Stat(p string) (fs.FileInfo, error) {
	var i info
	err := f.c.with(func(conn *ftp.ServerConn) (err error) {
		i, err = stat(conn, p)
		return
	})
	if err != nil {
		return nil, err
	}
	return i, nil
}

func (f *fsView) ReadDir(p string) ([]fs.FileInfo, error) {
	var infos []info
	err := f.c.with(func(conn *ftp.ServerConn) (err error) {
		infos, err = listInfos(conn, p)
		return
	})
	out := make([]fs.FileInfo, len(infos))
	for i := range infos {
		out[i] = infos[i]
	}
	return out, err
}

func (f *fsView) MkdirAll(p string) error {
	return f.c.with(func(conn *ftp.ServerConn) error {
		p = path.Clean(p)
		if p == "/" || p == "." {
			return nil
		}
		cur := ""
		if strings.HasPrefix(p, "/") {
			cur = "/"
		}
		for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
			cur = path.Join(cur, seg)
			if i, err := stat(conn, cur); err == nil {
				if !i.isDir {
					return fmt.Errorf("%s ist kein Ordner", cur)
				}
				continue
			}
			if err := conn.MakeDir(cur); err != nil {
				// a parallel transfer may have created it in the meantime
				if i, serr := stat(conn, cur); serr == nil && i.isDir {
					continue
				}
				return err
			}
		}
		return nil
	})
}

func (f *fsView) Remove(p string) error {
	return f.c.with(func(conn *ftp.ServerConn) error {
		i, err := stat(conn, p)
		if err != nil {
			return err
		}
		if i.isDir {
			return conn.RemoveDir(p)
		}
		return conn.Delete(p)
	})
}

func (f *fsView) RemoveAll(p string) error {
	return f.c.with(func(conn *ftp.ServerConn) error { return removeAll(conn, p) })
}

// readCloser hands the connection back to the pool when the download ends.
type readCloser struct {
	r    *ftp.Response
	c    *Client
	conn *ftp.ServerConn
	once sync.Once
}

func (r *readCloser) Read(b []byte) (int, error) { return r.r.Read(b) }
func (r *readCloser) Close() error {
	var err error
	r.once.Do(func() {
		err = r.r.Close()
		r.c.release(r.conn)
	})
	return err
}

func (f *fsView) Open(p string) (io.ReadCloser, error) {
	conn, err := f.c.acquire()
	if err != nil {
		return nil, err
	}
	r, err := conn.Retr(p)
	if err != nil {
		f.c.release(conn)
		return nil, err
	}
	return &readCloser{r: r, c: f.c, conn: conn}, nil
}

// writer streams data to STOR through a pipe.
type writer struct {
	pw   *io.PipeWriter
	done chan error
}

func (w *writer) Write(b []byte) (int, error) { return w.pw.Write(b) }
func (w *writer) Close() error {
	w.pw.Close()
	return <-w.done
}

func (f *fsView) Create(p string) (io.WriteCloser, error) {
	conn, err := f.c.acquire()
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := conn.Stor(p, pr)
		pr.CloseWithError(err) // unblocks a writer if STOR failed early
		f.c.release(conn)
		done <- err
	}()
	return &writer{pw: pw, done: done}, nil
}
