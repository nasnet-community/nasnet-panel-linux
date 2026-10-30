// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
)

// sshd is an SSH server in this process, in front of a sandbox: what a command
// sent to it does is what it would do on the server the sandbox stands for.
// The install talks to it through the real installer.Conn, so the host key
// check, the login, sudo with the password on stdin and the streaming of an
// upload are the real ones.
type sshd struct {
	t        *testing.T
	ln       net.Listener
	sb       *sandbox
	fp       string // the host key's fingerprint
	user     string
	password string

	attempts atomic.Int32 // logins tried
}

func newSSHD(t *testing.T, sb *sandbox, user, password string) *sshd {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	h := &sshd{t: t, sb: sb, fp: ssh.FingerprintSHA256(signer.PublicKey()), user: user, password: password}
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		h.attempts.Add(1)
		if c.User() == h.user && string(pw) == h.password {
			return nil, nil
		}
		return nil, errors.New("denied")
	}}
	cfg.AddHostKey(signer)
	if h.ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.ln.Close() })
	go func() {
		for {
			c, err := h.ln.Accept()
			if err != nil {
				return
			}
			go h.serve(c, cfg)
		}
	}()
	return h
}

func (h *sshd) port() int { return h.ln.Addr().(*net.TCPAddr).Port }

func (h *sshd) serve(nc net.Conn, cfg *ssh.ServerConfig) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, err := nch.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range creqs {
				if r.Type != "exec" {
					r.Reply(false, nil)
					continue
				}
				var p struct{ Cmd string }
				ssh.Unmarshal(r.Payload, &p)
				r.Reply(true, nil)
				code := h.sb.exec(strings.HasPrefix(p.Cmd, "sudo "), p.Cmd, ch, ch, ch.Stderr())
				ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
				ch.Close()
				return
			}
		}()
	}
}
