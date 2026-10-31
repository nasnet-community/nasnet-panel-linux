// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"fmt"
	"log/slog"
)

// NodeOps is what the endpoints that change a node ask of the manager.
// *manager.Manager is one, and with none in Config those endpoints are not
// there and answer as any other path that isn't.
//
// What takes more than a moment is a job: the call starts it and gives its
// id, and the page follows it through the job endpoints. A failure the caller
// can name comes back as an *Error with the contract's code, or as one of the
// model's errors or jobs.ErrBusy, all of which fail knows how to word. Any
// other error is the hub's own and is not shown.
type NodeOps interface {
	// Probe reads a server's SSH host key and says whether it is the one
	// pinned for a node there.
	Probe(ctx context.Context, req ProbeRequest) (ProbeResult, error)
	// AddNode saves the node as one whose install has not finished and starts
	// the job that finishes it. A job that fails cleans the server up and
	// leaves no node, or, when it can't, an unfinished install.
	AddNode(ctx context.Context, req InstallRequest) (jobID string, err error)
	RenameNode(ctx context.Context, id uint, name string) error
	// ChangeSettings starts the job that puts new settings on a node's entry.
	ChangeSettings(ctx context.Context, id uint, req SettingsRequest) (jobID string, err error)
	// CheckHandshake has the node try a handshake target.
	CheckHandshake(ctx context.Context, id uint, target string) (HandshakeCheck, error)
	// CheckUpdates looks up the latest release again, so that the node's
	// updates are as fresh as the release server's answer.
	CheckUpdates(ctx context.Context, id uint) error
	// UpdateNode starts the job that updates a node's "agent" or "xray".
	UpdateNode(ctx context.Context, id uint, kind string) (jobID string, err error)
	RenewCert(ctx context.Context, id uint) (jobID string, err error)
	RotateKeys(ctx context.Context, id uint) (jobID string, err error)
	// RemoveNode starts the job that takes a node out: with a login it takes
	// the agent off the server first, with force it only forgets the node.
	RemoveNode(ctx context.Context, id uint, req RemoveRequest) (jobID string, err error)
}

// Started is the answer to a call that starts a job.
type Started struct {
	JobID string `json:"jobId"`
}

type ProbeRequest struct {
	Host    string `json:"host"`
	SSHPort int    `json:"sshPort"`
	SSHUser string `json:"sshUser"`
}

// The states of a host key that a probe reports.
const (
	ProbeNew     = "new"     // no node of this router is on that server
	ProbeKnown   = "known"   // it is the key pinned for the node there
	ProbeChanged = "changed" // the node there has another key pinned
)

type ProbeResult struct {
	Fingerprint string `json:"fingerprint"`
	Status      string `json:"status"` // ProbeNew, ProbeKnown or ProbeChanged
}

// Login is how to sign in to a server over SSH, for one call. The secrets in
// it are used and dropped: nothing keeps them, and a login that is printed or
// logged by mistake shows none of them.
type Login struct {
	SSHPort    int    `json:"sshPort"`
	SSHUser    string `json:"sshUser"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	// HostKey is the fingerprint the person was shown and agreed to, which is
	// the one pinned for the node.
	HostKey string `json:"hostKey"`
}

func hidden(secret string) string {
	if secret == "" {
		return "none"
	}
	return "hidden"
}

func (l Login) String() string {
	return fmt.Sprintf("ssh login (port %d, user %q, host key %q, password %s, private key %s, passphrase %s)",
		l.SSHPort, l.SSHUser, l.HostKey, hidden(l.Password), hidden(l.PrivateKey), hidden(l.Passphrase))
}

func (l Login) GoString() string { return l.String() }

// LogValue keeps a JSON log handler from writing the fields.
func (l Login) LogValue() slog.Value { return slog.StringValue(l.String()) }

// InstallRequest is a new node: where it is, what to call it, which port its
// entry listens on (the router's default when left out), and the login to set
// it up with.
type InstallRequest struct {
	Login
	Name      string `json:"name"`
	Host      string `json:"host"`
	EntryPort int    `json:"entryPort,omitempty"`
}

func (r InstallRequest) String() string {
	return fmt.Sprintf("install %q at %s, entry port %d, with an %v", r.Name, r.Host, r.EntryPort, r.Login)
}

func (r InstallRequest) GoString() string { return r.String() }

func (r InstallRequest) LogValue() slog.Value { return slog.StringValue(r.String()) }

// RemoveRequest says how to take a node out: Force forgets it without
// touching the server, and Login signs in to the server to take the agent off.
type RemoveRequest struct {
	Force bool   `json:"force,omitempty"`
	Login *Login `json:"login,omitempty"`
}

func (r RemoveRequest) String() string {
	if r.Login == nil {
		return fmt.Sprintf("remove (force %t, no login)", r.Force)
	}
	return fmt.Sprintf("remove (force %t, with an %v)", r.Force, *r.Login)
}

func (r RemoveRequest) GoString() string { return r.String() }

func (r RemoveRequest) LogValue() slog.Value { return slog.StringValue(r.String()) }

// SettingsRequest is what to change of a node's entry. A field that is left
// out stays as it is; an empty SNI means the handshake target's host.
type SettingsRequest struct {
	EntryPort       *int    `json:"entryPort,omitempty"`
	HandshakeTarget *string `json:"handshakeTarget,omitempty"`
	SNI             *string `json:"sni,omitempty"`
}

// HandshakeCheck is what a node found out about a handshake target.
type HandshakeCheck struct {
	OK             bool   `json:"ok"`
	TLS13          bool   `json:"tls13"`
	X25519         bool   `json:"x25519"`
	HandshakeBytes int    `json:"handshakeBytes"`
	LatencyMs      int    `json:"latencyMs"`
	Error          string `json:"error"` // why it won't do, in a sentence; empty when it will
}
