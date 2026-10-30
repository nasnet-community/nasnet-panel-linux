// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Timeouts for SSH. A command that hangs must not hold a job for good, and the
// agent's upload is the one thing allowed to take long.
const (
	sshTimeout    = 20 * time.Second
	cmdTimeout    = 2 * time.Minute
	uploadTimeout = 15 * time.Minute
)

// Shell is what the install needs of an SSH connection. An *installer.Conn is
// one; tests put a fake server behind it.
type Shell interface {
	// Run executes cmd as the login user.
	Run(ctx context.Context, cmd string, stdin io.Reader) (string, error)
	// Root executes cmd as root: directly for root, else through sudo with the
	// password on stdin, never on the command line.
	Root(ctx context.Context, cmd string, stdin io.Reader) (string, error)
	IsRoot() bool
	Close() error
}

var _ Shell = (*installer.Conn)(nil)

// Login is how to sign in to a server over SSH, for one operation. It is
// never kept and no error or log line carries it. The user, the port and the
// host key to hold the server to come from the node.
type Login struct {
	Password   string // also what sudo asks for, when the user isn't root
	PrivateKey string // PEM, instead of a password
	Passphrase string // for a private key that is encrypted
}

func (l Login) secrets() []string {
	var s []string
	for _, v := range []string{l.Password, l.Passphrase, l.PrivateKey} {
		if v != "" {
			s = append(s, v)
		}
	}
	return s
}

// scrub takes the secrets out of an error's text, in case a library echoes
// what it was given. What it returns still matches the same errors.Is and
// errors.As, but hides the original from anything that unwraps it, since that
// is where the secret would be.
func scrub(err error, secrets []string) error {
	if err == nil || len(secrets) == 0 {
		return err
	}
	msg := err.Error()
	clean := msg
	for _, s := range secrets {
		clean = strings.ReplaceAll(clean, s, "***")
	}
	if clean == msg {
		return err
	}
	return &scrubbed{msg: clean, err: err}
}

type scrubbed struct {
	msg string
	err error
}

func (e *scrubbed) Error() string        { return e.msg }
func (e *scrubbed) Is(target error) bool { return errors.Is(e.err, target) }
func (e *scrubbed) As(target any) bool   { return errors.As(e.err, target) }

// sshTarget says where to sign in and as whom, from the node and the login.
// needKey is false for a probe, which signs in to nothing.
func (e *Env) sshTarget(node model.Node, login Login, needKey bool) (installer.Target, error) {
	host, err := model.CleanHost(node.Host)
	if err != nil {
		return installer.Target{}, err
	}
	port := node.SSHPort
	if port == 0 {
		port = 22
	}
	if err := model.ValidPort(port); err != nil {
		return installer.Target{}, err
	}
	t := installer.Target{
		Host: host, Port: port, User: strings.TrimSpace(node.SSHUser), Timeout: sshTimeout,
		Dial: e.dialer().DialContext,
	}
	if t.User == "" {
		t.User = "root"
	}
	if !needKey {
		return t, nil
	}
	if login.Password == "" && login.PrivateKey == "" {
		return t, ErrNoLogin
	}
	if node.SSHHostKey == "" {
		return t, ErrNoHostKey
	}
	t.Password, t.Key, t.Passphrase, t.HostKey = login.Password, []byte(login.PrivateKey), []byte(login.Passphrase), node.SSHHostKey
	return t, nil
}

// Probe reads the SSH host key of a server, to pin it on the first install
// and to compare with the pin later. It signs in to nothing, so no login is
// sent to a server nobody has vouched for.
func (e *Env) Probe(ctx context.Context, host string, port int) (string, error) {
	node := model.Node{Host: host, SSHPort: port}
	t, err := e.sshTarget(node, Login{}, false)
	if err != nil {
		return "", err
	}
	probe := e.probeSSH
	if probe == nil {
		probe = installer.Probe
	}
	fp, err := probe(ctx, t)
	if err != nil {
		return "", e.sshFailure(ctx, err, t, nil)
	}
	return fp, nil
}

// openShell signs in with the node's pinned host key.
func (e *Env) openShell(ctx context.Context, node model.Node, login Login) (Shell, error) {
	t, err := e.sshTarget(node, login, true)
	if err != nil {
		return nil, err
	}
	connect := e.connectSSH
	if connect == nil {
		connect = func(ctx context.Context, t installer.Target) (Shell, error) {
			c, err := installer.Connect(ctx, t)
			if err != nil {
				return nil, err
			}
			return c, nil
		}
	}
	sh, err := connect(ctx, t)
	if err != nil {
		return nil, e.sshFailure(ctx, err, t, login.secrets())
	}
	return sh, nil
}

// sshFailure says what a failed connection means: a key that isn't the pinned
// one, a login the server refused, or a server that can't be reached.
func (e *Env) sshFailure(ctx context.Context, err error, t installer.Target, secrets []string) error {
	var hk *installer.HostKeyError
	var ce *installer.CommandError
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.As(err, &hk):
		return &HostKeyChangedError{Host: t.Host, Pinned: hk.Want, Got: hk.Got, err: hk}
	}
	err = scrub(err, secrets)
	msg := err.Error()
	switch {
	case strings.Contains(msg, "unable to authenticate"):
		return newErr(ErrLoginFailed, err, "The server didn't accept the login. Check the user name and the password or key.")
	case strings.Contains(msg, "private key"):
		return newErr(ErrLoginFailed, err, "The private key can't be used. It may need its passphrase, or be in a form that isn't supported.")
	case errors.As(err, &ce):
		return &opError{msg: sentence("Couldn't talk to the server: " + brief(err)), cause: err}
	}
	return &UnreachableError{What: "ssh", Host: t.Host, Port: t.Port, Err: err}
}

// brief is a failed command's reason in a line: what it printed on stderr, or
// else how it ended.
func brief(err error) string {
	var ce *installer.CommandError
	if errors.As(err, &ce) {
		for _, line := range strings.Split(ce.Stderr, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				return line
			}
		}
		return ce.Err.Error()
	}
	return tidyCause(err)
}
