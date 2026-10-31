// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// What can go wrong with a node, as errors.Is sees it. The manager maps these
// to the API's codes; each error's text is a plain sentence the page can show
// as it is. Lower-level causes stay reachable with errors.Unwrap and
// errors.As, so a context error or a syscall error can still be told apart.
var (
	// The hub isn't set up for the call.
	ErrNoCA    = errors.New("agentops: the hub has no certificate authority yet")
	ErrNoFiles = errors.New("agentops: no source for the agent and Xray downloads")

	// Signing in over SSH.
	ErrNoLogin        = errors.New("agentops: no password or private key to sign in with")
	ErrNoHostKey      = errors.New("agentops: no pinned SSH host key to check the server against")
	ErrHostKeyChanged = errors.New("agentops: the server's SSH host key has changed")
	ErrLoginFailed    = errors.New("agentops: the server refused the login")
	ErrNeedsRoot      = errors.New("agentops: the login can't act as root")

	// The server itself.
	ErrUnsupportedHost = errors.New("agentops: the server can't run the agent")
	ErrPortInUse       = errors.New("agentops: a port the node needs is already in use")
	ErrOtherHub        = errors.New("agentops: the server's agent belongs to another hub")
	// An install found something on the server that it would take over: an
	// agent or an Xray that this plugin did not put there.
	ErrServerHasAgent = errors.New("agentops: the server already runs an agent this plugin did not install")
	ErrServerHasXray  = errors.New("agentops: the server already runs an Xray this plugin did not install")
	ErrServerIsNode   = errors.New("agentops: the server is already another node of this hub")
	// A takeover stopped the server's Xray service, and an Xray runs still: in a
	// container, by hand, or from another unit. Nothing else was changed.
	ErrXrayNotStopped  = errors.New("agentops: an Xray still runs on the server after its service was stopped")
	ErrUploadCorrupt   = errors.New("agentops: an upload arrived damaged")
	ErrAgentNotStarted = errors.New("agentops: the agent did not start")

	// Reaching a node.
	ErrUnreachable = errors.New("agentops: the node does not answer")
	ErrAgentCert   = errors.New("agentops: the agent's certificate was not accepted")

	// What the agent turns out to be.
	ErrNotManaged      = errors.New("agentops: the agent is not running the managed profile")
	ErrVersionMismatch = errors.New("agentops: the node runs another version than was sent")
	ErrAgentOutdated   = errors.New("agentops: the agent is too old for this")
	ErrAgentRefused    = errors.New("agentops: the agent refused the update")

	// What the node says about Xray.
	ErrNoTarget       = errors.New("agentops: no handshake target works from the node")
	ErrConfigRejected = errors.New("agentops: the node refused the Xray config")
	ErrXrayNotRunning = errors.New("agentops: Xray is not running on the node")
)

// opError is an error with a sentence of its own that errors.Is matches to a
// sentinel.
type opError struct {
	kind  error
	msg   string
	cause error
}

func newErr(kind error, cause error, format string, args ...any) *opError {
	return &opError{kind: kind, msg: fmt.Sprintf(format, args...), cause: cause}
}

func (e *opError) Error() string        { return e.msg }
func (e *opError) Is(target error) bool { return target == e.kind }
func (e *opError) Unwrap() error        { return e.cause }

// HostKeyChangedError is a server that shows another SSH host key than the one
// pinned. It wraps the installer's own error, so errors.As finds either.
type HostKeyChangedError struct {
	Host   string
	Pinned string // the fingerprint on record
	Got    string // what the server shows now
	err    *installer.HostKeyError
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("The SSH host key of %s has changed: it shows %s, and %s is on record. "+
		"The server may have been set up again, or something sits between it and the router.", e.Host, e.Got, e.Pinned)
}
func (e *HostKeyChangedError) Is(target error) bool { return target == ErrHostKeyChanged }
func (e *HostKeyChangedError) Unwrap() error {
	if e.err == nil {
		return nil
	}
	return e.err
}

// PortInUseError names the port that stopped an install and who has it, when
// the server says.
type PortInUseError struct {
	Port    int
	Role    string // "agent" or "entry"
	Process string // "" when unknown
}

func (e *PortInUseError) Error() string {
	who := ""
	if e.Process != "" {
		who = " by " + e.Process
	}
	return fmt.Sprintf("TCP port %d, which the %s needs, is already in use on the server%s. "+
		"Free it, or pick another port for the node.", e.Port, e.Role, who)
}
func (e *PortInUseError) Is(target error) bool { return target == ErrPortInUse }

// ServerHasXrayError is a server that runs an Xray of its own: this plugin
// did not install it, and the install would take over its binary, config and
// service. It says what was found, and whether the binary at the standard path
// is the build this plugin pins, since a takeover keeps that one as it is.
type ServerHasXrayError struct {
	// What was found: the file at the standard path, a service called
	// xray.service wherever systemd keeps it, a process named xray.
	Binary, Unit, Process bool
	// Version is the one that binary says it is, empty when there is none or it
	// can't be read.
	Version string
	// SameBuild says that binary has the pinned version and the pinned checksum.
	SameBuild bool
}

// Found names what was found, as the API's error info does.
func (e *ServerHasXrayError) Found() []string {
	var out []string
	for _, f := range []struct {
		on   bool
		name string
	}{{e.Binary, "binary"}, {e.Unit, "unit"}, {e.Process, "process"}} {
		if f.on {
			out = append(out, f.name)
		}
	}
	return out
}

func (e *ServerHasXrayError) Error() string {
	var found []string
	if e.Binary {
		found = append(found, xrayBin)
	}
	if e.Unit {
		found = append(found, "an xray.service unit")
	}
	if e.Process {
		found = append(found, "a running xray process")
	}
	msg := "This server already runs an Xray that Remote Nodes did not install"
	if len(found) > 0 {
		msg += " (" + strings.Join(found, ", ") + ")"
	}
	return msg + ". " + e.buildSentence() + " Use a server without one, or take it over."
}

// buildSentence says which Xray the binary is, and whether it is the pinned build.
func (e *ServerHasXrayError) buildSentence() string {
	switch {
	case e.SameBuild:
		return "It is Xray " + e.Version + ", the same build as this plugin's."
	case e.Version != "":
		return "It is Xray " + e.Version + ", not the same build as this plugin's."
	case e.Binary:
		return "Its version can't be read, so it isn't taken for the same build as this plugin's."
	}
	return "Its binary isn't at " + xrayBin + ", so it isn't the same build as this plugin's."
}
func (e *ServerHasXrayError) Is(target error) bool { return target == ErrServerHasXray }

// ServerIsNodeError is a server that this hub's marker shows is already
// another node of its own, which is the same server under another address.
type ServerIsNodeError struct {
	NodeID uint
	Name   string
}

func (e *ServerIsNodeError) Error() string {
	return fmt.Sprintf("This server is already the node %q here, under another address. Change that node instead of adding the server again.", e.Name)
}
func (e *ServerIsNodeError) Is(target error) bool { return target == ErrServerIsNode }

// UnreachableError is a node that doesn't answer where it should.
type UnreachableError struct {
	What string // "ssh", "agent", "entry" or "restart"
	Host string
	Port int
	Err  error
}

func (e *UnreachableError) Error() string {
	cause := ""
	if e.Err != nil {
		cause = " (" + tidyCause(e.Err) + ")"
	}
	switch e.What {
	case "ssh":
		return fmt.Sprintf("Can't reach SSH on %s port %d%s. Check the address and the port, and that the server's firewall lets the router in.", e.Host, e.Port, cause)
	case "entry":
		return fmt.Sprintf("Xray runs, but TCP port %d doesn't answer from the router%s. The server's firewall (or the provider's) may block it.", e.Port, cause)
	case "restart":
		return fmt.Sprintf("The agent did not come back on TCP port %d after the update%s. Its log on the server (journalctl -u nasnet-agent) says why.", e.Port, cause)
	}
	return fmt.Sprintf("The agent doesn't answer on TCP port %d%s. The server's firewall (or the provider's) may block it.", e.Port, cause)
}
func (e *UnreachableError) Is(target error) bool { return target == ErrUnreachable }
func (e *UnreachableError) Unwrap() error        { return e.Err }

// VersionMismatchError is a node that runs something other than what was sent
// to it.
type VersionMismatchError struct {
	What string // "agent" or "Xray"
	Want string
	Got  string
}

func (e *VersionMismatchError) Error() string {
	got := e.Got
	if got == "" {
		got = "no version"
	}
	return fmt.Sprintf("The %s on the node reports %s, but %s was sent.", e.What, got, e.Want)
}
func (e *VersionMismatchError) Is(target error) bool { return target == ErrVersionMismatch }

// NotManagedError is an agent that answered a call the managed profile refuses,
// so it runs the full profile. An agent older than the profile does that,
// since it ignores the line in its config.
type NotManagedError struct{ AgentVersion string }

func (e *NotManagedError) Error() string {
	v := ""
	if e.AgentVersion != "" {
		v = " (it reports " + e.AgentVersion + ")"
	}
	return "This agent isn't running the managed profile" + v + ". Agents before 1.3.0 don't have it, and a node set up by hand may run the full one. Install the agent again with this plugin."
}
func (e *NotManagedError) Is(target error) bool { return target == ErrNotManaged }

// ctxDone says whether err is a context ending, which is no one's fault but
// the caller's. gRPC says a cancelled call as a status, not as the context's own error.
func ctxDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.Canceled
}

// ctxErr is the context's own error when the caller's context has ended and
// err is what that did to a call: the caller asked for it, and a gRPC status
// or a "doesn't answer" would only blur that.
func ctxErr(ctx context.Context, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// tidyCause is an error's text without the prefixes that only say which
// package made it.
func tidyCause(err error) string {
	s := reasonTail(err.Error())
	for _, p := range []string{"installer: connect: ", "installer: ", "agentops: "} {
		s = strings.TrimPrefix(s, p)
	}
	return s
}

// sentence is a message as a sentence: it starts with a capital and a
// single-line one ends with a full stop.
func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	s = string(r)
	if !strings.Contains(s, "\n") && !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
		s += "."
	}
	return s
}
