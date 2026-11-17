package ftpclient

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// The ftp library parses listings but drops the permission bits. To show and
// preset permissions (like FileZilla's "Permissions" column) the directory is
// listed once more with LIST over a data connection of our own and the Unix
// "ls -l" style lines are parsed for the mode. This is best effort: servers
// with DOS/IIS style listings simply have no permissions.

var unixLine = regexp.MustCompile(
	`^([\-dlbcpsDx])([rwxsStTl\-]{9})[+@.]?\s+\d+\s+(?:\S+\s+){1,2}\d+\s+` + // type+perms, links, owner [group], size
		`(?:[A-Za-z]{3}\s+\d{1,2}\s+(?:\d{4}|\d{1,2}:\d{2})|\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2})\s+` + // date
		`(.+?)\r?$`)

// modeInfo is the permission info of one directory entry.
type modeInfo struct {
	mode string // e.g. "-rw-r--r--"
	perm uint32 // e.g. 0o644, including setuid/setgid/sticky
}

// permBits converts the 9 permission characters ("rwxr-xr-x", "rwsr-sr-t") to
// bits, including setuid, setgid and sticky.
func permBits(s string) uint32 {
	masks := [9]uint32{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001}
	special := [3]uint32{0o4000, 0o2000, 0o1000}
	var bits uint32
	for i := 0; i < 9 && i < len(s); i++ {
		switch c := s[i]; i % 3 {
		case 0:
			if c == 'r' {
				bits |= masks[i]
			}
		case 1:
			if c == 'w' {
				bits |= masks[i]
			}
		default:
			switch c {
			case 'x':
				bits |= masks[i]
			case 's', 't': // special bit and executable
				bits |= masks[i] | special[i/3]
			case 'S', 'T': // special bit without executable
				bits |= special[i/3]
			}
		}
	}
	return bits
}

// parseModes extracts name -> permissions from "ls -l" style output.
func parseModes(listing string) map[string]modeInfo {
	out := map[string]modeInfo{}
	sc := bufio.NewScanner(strings.NewReader(listing))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := unixLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		name := m[3]
		if m[1] == "l" {
			if i := strings.Index(name, " -> "); i >= 0 {
				name = name[:i]
			}
		}
		out[name] = modeInfo{mode: m[1] + m[2], perm: permBits(m[2])}
	}
	return out
}

// passivePort asks the server for a passive data port (EPSV, falling back to PASV).
func passivePort(tc *textproto.Conn) (int, error) {
	if _, err := tc.Cmd("EPSV"); err != nil {
		return 0, err
	}
	if _, msg, err := tc.ReadResponse(2); err == nil {
		// "Entering Extended Passive Mode (|||6446|)"
		if i, j := strings.Index(msg, "(|||"), strings.LastIndex(msg, "|)"); i >= 0 && j > i {
			if p, err := strconv.Atoi(msg[i+4 : j]); err == nil {
				return p, nil
			}
		}
	}
	if _, err := tc.Cmd("PASV"); err != nil {
		return 0, err
	}
	_, msg, err := tc.ReadResponse(2)
	if err != nil {
		return 0, err
	}
	// "Entering Passive Mode (h1,h2,h3,h4,p1,p2)"
	i, j := strings.Index(msg, "("), strings.LastIndex(msg, ")")
	if i < 0 || j < i {
		return 0, errors.New("unerwartete PASV-Antwort")
	}
	parts := strings.Split(msg[i+1:j], ",")
	if len(parts) != 6 {
		return 0, errors.New("unerwartete PASV-Antwort")
	}
	hi, e1 := strconv.Atoi(strings.TrimSpace(parts[4]))
	lo, e2 := strconv.Atoi(strings.TrimSpace(parts[5]))
	if e1 != nil || e2 != nil {
		return 0, errors.New("unerwartete PASV-Antwort")
	}
	return hi<<8 | lo, nil
}

// rawList runs "LIST <args>" on conn and returns the raw listing text.
func (c *Client) rawList(conn *ftp.ServerConn, args string) (string, error) {
	tc, err := controlConn(conn)
	if err != nil {
		return "", err
	}
	port, err := passivePort(tc)
	if err != nil {
		return "", err
	}
	timeout := c.cfg.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	dc, err := net.DialTimeout("tcp", net.JoinHostPort(c.cfg.Host, strconv.Itoa(port)), timeout)
	if err != nil {
		return "", err
	}
	defer dc.Close()
	dc.SetDeadline(time.Now().Add(timeout))
	// Servers accept the data connection only once the command has arrived, so the
	// command goes out first; for FTPS the TLS handshake follows on the data connection.
	if _, err := tc.Cmd("LIST %s", args); err != nil {
		return "", err
	}
	var data net.Conn = dc
	if c.cfg.Mode == Explicit || c.cfg.Mode == Implicit {
		tcfg := c.cfg.TLS.Clone()
		if tcfg.ServerName == "" {
			tcfg.ServerName = c.cfg.Host
		}
		tlsConn := tls.Client(dc, tcfg)
		if err := tlsConn.Handshake(); err != nil {
			return "", err
		}
		data = tlsConn
	}
	if _, _, err := tc.ReadResponse(1); err != nil { // 150: about to open the data connection
		return "", err
	}
	body, rerr := io.ReadAll(io.LimitReader(data, 16<<20))
	data.Close()
	if _, _, err := tc.ReadResponse(2); err != nil { // 226: transfer complete
		return "", err
	}
	return string(body), rerr
}

// modes best-effort reads the permissions of every entry in dir.
func (c *Client) modes(conn *ftp.ServerConn, dir string) map[string]modeInfo {
	if strings.ContainsAny(dir, "\r\n") {
		return nil
	}
	// -a includes dot files like the library's own listing does; not every server accepts it
	listing, err := c.rawList(conn, "-a "+dir)
	if err != nil {
		var te *textproto.Error
		if !errors.As(err, &te) {
			return nil // not a protocol-level refusal: the connection state is unknown
		}
		if listing, err = c.rawList(conn, dir); err != nil {
			return nil
		}
	}
	return parseModes(listing)
}
