package ftpclient

import (
	"errors"
	"fmt"
	"net/textproto"
	"reflect"
	"strings"
	"unsafe"

	"github.com/jlaffaye/ftp"
)

// controlConn returns the control connection of an ftp.ServerConn.
//
// The ftp library has no way to send arbitrary commands such as SITE CHMOD, but
// the control connection it keeps is a plain *textproto.Conn. It is read via
// reflection; if a library update renames the field this fails with an error
// (and TestFTPChmod fails) instead of misbehaving.
func controlConn(c *ftp.ServerConn) (*textproto.Conn, error) {
	f := reflect.ValueOf(c).Elem().FieldByName("conn")
	if !f.IsValid() || f.Type() != reflect.TypeOf((*textproto.Conn)(nil)) {
		return nil, errors.New("interner Fehler: FTP-Steuerverbindung nicht zugänglich")
	}
	return *(**textproto.Conn)(unsafe.Pointer(f.UnsafeAddr())), nil
}

// Chmod sets permissions with SITE CHMOD, which most FTP servers support
// (vsftpd needs it enabled, some servers do not offer it at all).
func (c *Client) Chmod(p string, mode uint32) error {
	if strings.ContainsAny(p, "\r\n") {
		return errors.New("ungültiger Pfad")
	}
	return c.with(func(conn *ftp.ServerConn) error {
		tc, err := controlConn(conn)
		if err != nil {
			return err
		}
		// like the library itself: no response pipelining, one command at a time
		if _, err := tc.Cmd("SITE CHMOD %o %s", mode&0o7777, p); err != nil {
			return err
		}
		_, _, err = tc.ReadResponse(2) // any 2xx
		var te *textproto.Error
		if errors.As(err, &te) {
			switch te.Code {
			case 500, 501, 502, 504:
				return fmt.Errorf("Der Server unterstützt SITE CHMOD nicht (%s)", strings.TrimSpace(te.Msg))
			}
			return fmt.Errorf("Rechte ändern fehlgeschlagen: %s", strings.TrimSpace(te.Msg))
		}
		return err
	})
}
