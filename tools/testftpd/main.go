// Command testftpd is a tiny local, unencrypted FTP server for manually testing
// the client:
//
//	go run ./tools/testftpd -root /some/folder     # user "test", password "test"
//
// It listens on 127.0.0.1 only. Use for local testing only.
package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

type driver struct {
	root, user, pass string
	ln               net.Listener
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	return &ftpserver.Settings{
		Listener:                 d.ln,
		PublicHost:               "127.0.0.1",
		PassiveTransferPortRange: ftpserver.PortRange{Start: 42000, End: 42100},
	}, nil
}
func (d *driver) ClientConnected(ftpserver.ClientContext) (string, error) { return "testftpd", nil }
func (d *driver) ClientDisconnected(ftpserver.ClientContext)              {}
func (d *driver) GetTLSConfig() (*tls.Config, error)                      { return nil, errors.New("TLS not supported") }
func (d *driver) AuthUser(_ ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if user != d.user || pass != d.pass {
		return nil, errors.New("access denied")
	}
	return afero.NewBasePathFs(afero.NewOsFs(), d.root), nil // chrooted to -root
}

func main() {
	addr := flag.String("addr", "127.0.0.1:2121", "listen address (loopback only)")
	root := flag.String("root", ".", "directory to serve")
	user := flag.String("user", "test", "user name")
	pass := flag.String("pass", "test", "password")
	flag.Parse()

	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		log.Fatal("testftpd only listens on loopback addresses")
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := ftpserver.NewFtpServer(&driver{root: *root, user: *user, pass: *pass, ln: ln})
	if err := srv.Listen(); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "testftpd on %s (user %q), root %s\n", *addr, *user, *root)
	log.Fatal(srv.Serve())
}
