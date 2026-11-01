// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// session is one operation's SSH connection to a server, with the ways it
// runs commands: every one under a time limit, its failure said in a
// sentence, and no login in it.
type session struct {
	secrets []string
	// reopen signs in again, to the same server with the same login and pinned
	// key. Nil for a session that has no way to.
	reopen func(context.Context) (Shell, error)

	mu sync.Mutex
	sh Shell
}

// keepAliveEvery is how often keepAlive speaks to the server. It is a
// variable so that a test can make it short.
var keepAliveEvery = 15 * time.Second

func (e *Env) openSession(ctx context.Context, node model.Node, login Login) (*session, error) {
	sh, err := e.openShell(ctx, node, login)
	if err != nil {
		return nil, err
	}
	return &session{
		sh: sh, secrets: login.secrets(),
		reopen: func(ctx context.Context) (Shell, error) { return e.openShell(ctx, node, login) },
	}, nil
}

// shell is the connection now in use, which is not always the one the session
// opened with: a dropped one is made again.
func (s *session) shell() Shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sh
}

func (s *session) close() { s.shell().Close() }

// connectionGone says whether a command failed because the connection to the
// server is gone, not because the command did. Everything a command that ran
// reports is an installer.CommandError, so any other failure to run it, such as
// a session that can't be opened on a connection something has cut, means it
// never started and is safe to run again.
func connectionGone(err error) bool {
	var ce *installer.CommandError
	switch {
	case err == nil || errors.As(err, &ce):
		return false
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return false
	case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed), errors.Is(err, syscall.EPIPE), errors.Is(err, syscall.ECONNRESET):
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected packet in response to channel open") || strings.Contains(msg, "ssh: disconnect") ||
		strings.Contains(msg, "use of closed network connection") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
}

// reconnect replaces a connection that is gone with a new one, unless
// something has done that already. gone is the connection that failed.
func (s *session) reconnect(ctx context.Context, gone Shell) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh != gone {
		return nil
	}
	if s.reopen == nil {
		return errors.New("agentops: the connection to the server is gone")
	}
	sh, err := s.reopen(ctx)
	if err != nil {
		return err
	}
	gone.Close()
	s.sh = sh
	return nil
}

// keepAlive speaks to the server every keepAliveEvery until stop is called, so
// that a long wait with nothing to say to it (the agent's download, over a slow
// link) doesn't let a router, a firewall or the server itself forget the
// connection. A connection found dead is made again here, not by the next real
// command.
func (s *session) keepAlive(ctx context.Context) (stop func()) {
	kctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(keepAliveEvery)
		defer t.Stop()
		for {
			select {
			case <-kctx.Done():
				return
			case <-t.C:
			}
			sh := s.shell()
			cctx, release := context.WithTimeout(kctx, sshTimeout)
			_, err := sh.Run(cctx, "true", nil)
			release()
			if kctx.Err() == nil && connectionGone(err) {
				s.reconnect(kctx, sh)
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// run executes cmd as the login user.
func (s *session) run(ctx context.Context, what, cmd string) (string, error) {
	return s.exec(ctx, cmdTimeout, what, func(ctx context.Context) (string, error) { return s.shell().Run(ctx, cmd, nil) })
}

// root executes cmd as root, with stdin when there is some.
func (s *session) root(ctx context.Context, what, cmd string, stdin io.Reader) (string, error) {
	return s.exec(ctx, cmdTimeout, what, func(ctx context.Context) (string, error) { return s.shell().Root(ctx, cmd, stdin) })
}

// has says whether a command that only asks a question answered yes.
func (s *session) has(ctx context.Context, cmd string) bool {
	_, err := s.run(ctx, "", cmd)
	return err == nil
}

func (s *session) exec(ctx context.Context, limit time.Duration, what string, call func(context.Context) (string, error)) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	used := s.shell()
	out, err := call(cctx)
	if err != nil && ctx.Err() == nil && connectionGone(err) {
		// the command never started: sign in again and run it once more
		if rerr := s.reconnect(ctx, used); rerr == nil {
			out, err = call(cctx)
		}
	}
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return out, ctx.Err() // the job was cancelled, which isn't the command's doing
	}
	return out, s.fail(what, err)
}

// sudoSaid is what sudo prints when it won't do it.
var sudoSaid = []string{"incorrect password", "a password is required", "Sorry, try again", "not in the sudoers", "may not run sudo", "is not allowed to execute", "must have a tty"}

// fail says a failed command in a sentence, with the secrets taken out. what
// is what was being done ("Couldn't write the config"); empty means the
// failure is an answer and needs no words.
func (s *session) fail(what string, err error) error {
	err = scrub(err, s.secrets)
	var ce *installer.CommandError
	if errors.As(err, &ce) {
		for _, said := range sudoSaid {
			if strings.Contains(ce.Stderr, said) {
				return newErr(ErrNeedsRoot, err, "This login can't act as root: sudo didn't accept the password, or the user isn't allowed to use it.")
			}
		}
	}
	if what == "" {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &opError{msg: sentence(what + ": it took too long"), cause: err}
	}
	return &opError{msg: sentence(what + ": " + brief(err)), cause: err}
}

// write puts bytes in a file on the server, as cmdWrite says.
func (s *session) write(ctx context.Context, path, mode string, data []byte) error {
	_, err := s.root(ctx, "Couldn't write "+path, cmdWrite(path, mode), bytes.NewReader(data))
	return err
}

// listeners reads the TCP ports in use on the server, and who has them where
// the server says. ss is asked first, then the kernel's own table; a server
// that answers neither is taken to have nothing in the way, which a busy port
// then shows up as later.
func (s *session) listeners(ctx context.Context) map[int][]string {
	if out, err := s.root(ctx, "", cmdListeners, nil); err == nil {
		return parseListeners(out)
	}
	if out, err := s.root(ctx, "", cmdProcNet, nil); err == nil {
		return parseProcNet(out)
	}
	return nil
}

// serverState is what the server says about an earlier install.
type serverState struct {
	present map[string]bool
	nodeCA  []byte
	marker  *marker
}

// readState reads what an operation has to know before it acts: which of the
// paths it cares about exist, the CA the agent trusts, and the marker. All of
// it as root: a folder only root can open (the official Xray script leaves its
// config folder 750 root:xray) shows a login that isn't root none of what is in
// it, and a path taken for free is a path that gets written over.
func (s *session) readState(ctx context.Context) (serverState, error) {
	var st serverState
	out, err := s.root(ctx, "Couldn't look at the server", cmdPresent(presentPaths), nil)
	if err != nil {
		return st, err
	}
	st.present = parsePresent(out)
	if out, err = s.root(ctx, "Couldn't read the agent's CA file", cmdCA, nil); err != nil {
		return st, err
	}
	st.nodeCA = []byte(out)
	if out, err = s.root(ctx, "Couldn't read what the install left", cmdMarker, nil); err != nil {
		return st, err
	}
	if st.marker, err = parseMarker(out); err != nil {
		return st, &opError{msg: sentence(err.Error()), cause: err}
	}
	return st, nil
}

// ownership says whose agent is on the server: this hub's, another's, or
// nobody's. The CA file and the marker each say it, and either can be gone,
// so an agent is another hub's if either says so.
func (st serverState) ownership(ours *x509.Certificate) (mine, other bool) {
	ca := bytes.TrimSpace(st.nodeCA)
	if len(ca) > 0 {
		if isOurCA(ca, ours) {
			mine = true
		} else {
			other = true
		}
	}
	if st.marker != nil {
		if st.marker.CASHA256 == caFingerprint(ours) {
			mine = true
		} else {
			other = true
		}
	}
	return mine && !other, other
}

// allowedByUFW reads the TCP ports a ufw status lists as allowed.
func allowedByUFW(status string) []int {
	var ports []int
	for _, line := range strings.Split(status, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !slices.Contains(f, "ALLOW") {
			continue
		}
		spec, proto, _ := strings.Cut(f[0], "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if p, err := strconv.Atoi(spec); err == nil && !slices.Contains(ports, p) {
			ports = append(ports, p)
		}
	}
	return ports
}
