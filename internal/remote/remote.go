// Package remote defines what the application needs from a server connection,
// independent of the protocol (SFTP, FTP, FTPS).
package remote

import (
	"bytes"
	"errors"
	"unicode/utf8"

	"noxioftp/internal/fsinfo"
	"noxioftp/internal/transfer"
)

// Error codes. The frontend matches on the prefix before the colon.
var (
	ErrModified = errors.New("E_MODIFIED: Die Datei wurde auf dem Server inzwischen geändert")
	ErrBinary   = errors.New("E_BINARY: Die Datei ist keine UTF-8-Textdatei und kann nicht bearbeitet werden")
	ErrTooLarge = errors.New("E_TOO_LARGE: Die Datei ist zu groß für den Editor")
)

// MaxEditSize is the largest file the editor will open.
const MaxEditSize = 5 << 20

// TextFile is the content of a file opened in the editor.
type TextFile struct {
	Content string `json:"content"`
	ModTime int64  `json:"modTime"`
	Size    int64  `json:"size"`
}

// CheckText returns ErrBinary / ErrTooLarge unless data can be edited as text.
func CheckText(data []byte) error {
	if len(data) > MaxEditSize {
		return ErrTooLarge
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return ErrBinary
	}
	return nil
}

// Session is an open connection to a server.
type Session interface {
	Home() (string, error)
	List(dir string) (fsinfo.Listing, error)
	Mkdir(p string) error
	Rename(from, to string) error
	Delete(paths []string) error
	Chmod(p string, mode uint32) error
	ReadText(p string) (TextFile, error)
	WriteText(p, content string, expectedMod int64, force bool) (int64, error)

	// FS is used by the transfer queue.
	FS() transfer.FS

	// Done is closed when the connection is gone.
	Done() <-chan struct{}
	Closed() bool
	Close()
}
