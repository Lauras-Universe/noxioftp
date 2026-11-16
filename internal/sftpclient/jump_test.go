package sftpclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sshServer is a minimal SSH server: password auth "u"/"p", the sftp
// subsystem, and (if forward is set) direct-tcpip forwarding for ProxyJump.
func sshServer(t *testing.T, forward bool) (host string, port int, accepted *int) {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "u" && string(pw) == "p" {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	count := new(int)
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for ch := range chans {
					switch {
					case ch.ChannelType() == "session":
						c, creqs, _ := ch.Accept()
						go func() {
							for r := range creqs {
								ok := r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp"
								r.Reply(ok, nil)
								if ok {
									if srv, err := sftp.NewServer(c); err == nil {
										srv.Serve()
										srv.Close()
									}
									return
								}
							}
						}()
					case ch.ChannelType() == "direct-tcpip" && forward:
						// payload: string host, uint32 port, string origHost, uint32 origPort
						p := ch.ExtraData()
						hl := binary.BigEndian.Uint32(p)
						h := string(p[4 : 4+hl])
						pt := binary.BigEndian.Uint32(p[4+hl:])
						dst, err := net.Dial("tcp", net.JoinHostPort(h, strconv.Itoa(int(pt))))
						if err != nil {
							ch.Reject(ssh.ConnectionFailed, err.Error())
							continue
						}
						*count++
						c, _, _ := ch.Accept()
						go func() { io.Copy(dst, c); dst.Close() }()
						go func() { io.Copy(c, dst); c.Close() }()
					default:
						ch.Reject(ssh.UnknownChannelType, "no")
					}
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port, count
}

func TestDialThroughJumpHost(t *testing.T) {
	tHost, tPort, _ := sshServer(t, false)
	jHost, jPort, forwarded := sshServer(t, true)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "behind.txt"), []byte("x"), 0o644)

	cfg := Config{
		Host: tHost, Port: tPort, User: "u", Password: "p",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // test servers only
		Jump:            &Config{Host: jHost, Port: jPort, User: "u", Password: "p", HostKeyCallback: ssh.InsecureIgnoreHostKey()},
	}
	c, err := Dial(cfg)
	if err != nil {
		t.Fatal(err)
	}
	l, err := c.List(dir)
	if err != nil || len(l.Entries) != 1 || l.Entries[0].Name != "behind.txt" {
		t.Fatalf("list through jump host: %+v, %v", l, err)
	}
	if *forwarded != 1 {
		t.Fatalf("connection did not go through the jump host (forwarded=%d)", *forwarded)
	}
	c.Close()
	select {
	case <-c.Done():
	default:
		t.Fatal("Done not closed after Close")
	}

	// wrong password on the jump host fails cleanly
	cfg.Jump.Password = "wrong"
	if _, err := Dial(cfg); err == nil {
		t.Fatal("dial succeeded with a wrong jump host password")
	}
}

func TestJumpChainDepthLimit(t *testing.T) {
	cfg := Config{Host: "h", User: "u", Password: "p", HostKeyCallback: ssh.InsecureIgnoreHostKey()}
	for i := 0; i < 6; i++ {
		inner := cfg
		cfg = Config{Host: "h", User: "u", Password: "p", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Jump: &inner}
	}
	if _, err := Dial(cfg); !errors.Is(err, ErrJumpDepth) {
		t.Fatalf("err = %v, want ErrJumpDepth", err)
	}
}
