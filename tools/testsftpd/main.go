// Command testsftpd is a tiny local SFTP server for manually testing the client.
//
//	go run ./tools/testsftpd -root /some/folder          # user "test", password "test"
//
// It listens on 127.0.0.1 only, serves the given folder as the start directory
// and prints a known_hosts line so the host key can be pre-trusted.
// NOTE: the underlying SFTP server is not chrooted. Use for local testing only.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:2222", "listen address (keep it on loopback)")
	root := flag.String("root", ".", "start directory of the SFTP session")
	user := flag.String("user", "test", "user name")
	pass := flag.String("pass", "test", "password")
	flag.Parse()

	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		log.Fatal("testsftpd only listens on loopback addresses")
	}

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, p []byte) (*ssh.Permissions, error) {
			if c.User() == *user && string(p) == *pass {
				return nil, nil
			}
			return nil, fmt.Errorf("access denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(knownhosts.Line([]string{*addr}, signer.PublicKey()))
	fmt.Fprintf(os.Stderr, "testsftpd on %s (user %q), root %s\n", *addr, *user, *root)

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go serve(c, cfg, *root)
	}
}

func serve(c net.Conn, cfg *ssh.ServerConfig, root string) {
	defer c.Close()
	_, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range creqs {
				ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
				r.Reply(ok, nil)
				if ok {
					srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
					if err != nil {
						return
					}
					if err := srv.Serve(); err != nil && err != io.EOF {
						log.Print(err)
					}
					srv.Close()
					return
				}
			}
		}()
	}
}
