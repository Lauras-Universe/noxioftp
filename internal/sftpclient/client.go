// Package sftpclient wraps an SSH connection and the SFTP subsystem.
package sftpclient

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"sftp-client/internal/fsinfo"
)

// Error codes. The frontend matches on the prefix before the colon.
var (
	ErrPassphrase  = errors.New("E_PASSPHRASE: Der private Schlüssel ist durch eine Passphrase geschützt")
	ErrModified    = errors.New("E_MODIFIED: Die Datei wurde auf dem Server inzwischen geändert")
	ErrBinary      = errors.New("E_BINARY: Die Datei ist keine UTF-8-Textdatei und kann nicht bearbeitet werden")
	ErrTooLarge    = errors.New("E_TOO_LARGE: Die Datei ist zu groß für den Editor")
	ErrNoHostCheck = errors.New("kein Host-Key-Callback konfiguriert")
)

// MaxEditSize is the largest file the editor will open.
const MaxEditSize = 5 << 20

// Config describes how to connect.
type Config struct {
	Host            string
	Port            int
	User            string
	Password        string // password auth
	KeyPath         string // key auth
	Passphrase      string // optional, for encrypted keys
	HostKeyCallback ssh.HostKeyCallback
	Timeout         time.Duration
}

// Client is an open SFTP connection.
type Client struct {
	ssh  *ssh.Client
	sftp *sftp.Client
	done chan struct{}
	once sync.Once
}

func authMethods(cfg Config) ([]ssh.AuthMethod, error) {
	if cfg.KeyPath == "" {
		return []ssh.AuthMethod{
			ssh.Password(cfg.Password),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				answers := make([]string, len(qs))
				for i := range answers {
					answers[i] = cfg.Password
				}
				return answers, nil
			}),
		}, nil
	}
	pem, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("Schlüsseldatei nicht lesbar: %w", err)
	}
	var signer ssh.Signer
	if cfg.Passphrase == "" {
		signer, err = ssh.ParsePrivateKey(pem)
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(cfg.Passphrase))
	}
	var missing *ssh.PassphraseMissingError
	if errors.As(err, &missing) {
		return nil, ErrPassphrase
	}
	if err != nil {
		return nil, fmt.Errorf("Schlüssel ungültig: %w", err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// Dial connects and opens the SFTP subsystem.
func Dial(cfg Config) (*Client, error) {
	if cfg.HostKeyCallback == nil {
		return nil, ErrNoHostCheck
	}
	auth, err := authMethods(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(port)), &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: cfg.HostKeyCallback,
		Timeout:         cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c := &Client{ssh: conn, sftp: sc, done: make(chan struct{})}
	go func() {
		_ = sc.Wait()
		c.markClosed()
	}()
	go c.keepAlive()
	return c, nil
}

// NewFromSFTP wraps an existing sftp.Client (used in tests).
func NewFromSFTP(sc *sftp.Client) *Client {
	c := &Client{sftp: sc, done: make(chan struct{})}
	go func() {
		_ = sc.Wait()
		c.markClosed()
	}()
	return c
}

func (c *Client) markClosed() { c.once.Do(func() { close(c.done) }) }

func (c *Client) keepAlive() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			if _, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.Close()
				return
			}
		}
	}
}

// Done is closed when the connection is gone.
func (c *Client) Done() <-chan struct{} { return c.done }

// Closed reports whether the connection is gone.
func (c *Client) Closed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// Close closes the connection.
func (c *Client) Close() {
	c.sftp.Close()
	if c.ssh != nil {
		c.ssh.Close()
	}
	c.markClosed()
}

// SFTP exposes the underlying client for transfers.
func (c *Client) SFTP() *sftp.Client { return c.sftp }

// Home returns the initial (home) directory of the session.
func (c *Client) Home() (string, error) { return c.sftp.Getwd() }

// List lists a remote directory. Symlinks to directories are reported as directories.
func (c *Client) List(dir string) (fsinfo.Listing, error) {
	dir = path.Clean(dir)
	infos, err := c.sftp.ReadDir(dir)
	if err != nil {
		return fsinfo.Listing{}, err
	}
	entries := make([]fsinfo.Entry, 0, len(infos))
	for _, fi := range infos {
		full := path.Join(dir, fi.Name())
		e := fsinfo.FromInfo(full, fi)
		if e.IsLink {
			if st, err := c.sftp.Stat(full); err == nil {
				e.IsDir = st.IsDir()
				e.Size = st.Size()
			}
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

// Mkdir creates a remote directory.
func (c *Client) Mkdir(p string) error { return c.sftp.Mkdir(p) }

// Rename renames a remote file; it fails if the target exists.
func (c *Client) Rename(from, to string) error {
	if _, err := c.sftp.Lstat(to); err == nil {
		return fmt.Errorf("%s existiert bereits", path.Base(to))
	}
	return c.sftp.Rename(from, to)
}

// Delete removes remote files and directories (recursively).
func (c *Client) Delete(paths []string) error {
	for _, p := range paths {
		p = path.Clean(p)
		if p == "/" || p == "." {
			return errors.New("Wurzelverzeichnis kann nicht gelöscht werden")
		}
		fi, err := c.sftp.Lstat(p)
		if err != nil {
			return err
		}
		if fi.IsDir() {
			err = c.sftp.RemoveAll(p)
		} else {
			err = c.sftp.Remove(p)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// Chmod changes the permission bits.
func (c *Client) Chmod(p string, mode uint32) error {
	return c.sftp.Chmod(p, os.FileMode(mode&0o7777))
}

// TextFile is the content of a file opened in the editor.
type TextFile struct {
	Content string `json:"content"`
	ModTime int64  `json:"modTime"`
	Size    int64  `json:"size"`
}

// ReadText reads a remote file for the editor.
func (c *Client) ReadText(p string) (TextFile, error) {
	f, err := c.sftp.Open(p)
	if err != nil {
		return TextFile{}, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return TextFile{}, err
	}
	if fi.Size() > MaxEditSize {
		return TextFile{}, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxEditSize+1))
	if err != nil {
		return TextFile{}, err
	}
	if len(data) > MaxEditSize {
		return TextFile{}, ErrTooLarge
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return TextFile{}, ErrBinary
	}
	return TextFile{Content: string(data), ModTime: fi.ModTime().Unix(), Size: fi.Size()}, nil
}

// WriteText writes editor content back. Unless force is set, it refuses when
// the file's modification time differs from expectedMod. It returns the new
// modification time.
func (c *Client) WriteText(p, content string, expectedMod int64, force bool) (int64, error) {
	if !force {
		if fi, err := c.sftp.Stat(p); err == nil && fi.ModTime().Unix() != expectedMod {
			return 0, ErrModified
		}
	}
	f, err := c.sftp.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return 0, err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return 0, err
	}
	if err := f.Close(); err != nil {
		return 0, err
	}
	fi, err := c.sftp.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.ModTime().Unix(), nil
}
