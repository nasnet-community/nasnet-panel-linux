// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netbind"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// Files is where an install takes the agent and Xray from. An
// *artifacts.Holder is one; tests hand in a stand-in with builds of their own.
type Files interface {
	Agent(ctx context.Context, arch string) (*artifacts.Agent, error)
	Xray(ctx context.Context, arch string) (*artifacts.Xray, error)
}

// Env is what every operation needs, and nothing about one node. The manager
// makes one and uses it for all nodes, from as many jobs at once as it likes.
// Set the fields before the first call and don't copy an Env that has been
// used.
type Env struct {
	// Local is the address everything bound for a node leaves from: SSH, the
	// agent's gRPC link and the name lookups for both (the router's
	// 192.168.50.18). Zero lets the system choose, which is what tests want.
	Local netip.Addr
	// CA signs the node certificates and the hub's own client certificate.
	CA *pki.CA
	// Files is where the agent and Xray builds come from.
	Files Files
	// Now is the clock; nil is time.Now.
	Now func() time.Time
	// Log gets what happened to a node, never a login. Nil is slog.Default.
	Log *slog.Logger
	// DialTimeout is how long one connection to an agent may take to come up.
	// Zero is 15 seconds.
	DialTimeout time.Duration
	// Settle is how long a node has to settle after a change: to report the
	// Xray that was sent, to run it, to answer on the entry port. Zero is each
	// wait's own, 15 to 30 seconds.
	Settle time.Duration
	// LiveNode says whether this hub still keeps a set-up node with the given
	// id, and its name. An install over this hub's own marker for another node
	// is the same server under another address, and is refused rather than
	// repaired. Nil keeps no nodes.
	LiveNode func(id uint) (name string, ok bool)

	// What a test puts in the place of the real thing. Nil is the real one.
	connectSSH func(context.Context, installer.Target) (Shell, error)
	probeSSH   func(context.Context, installer.Target) (string, error)
	verify     func(context.Context, model.Node, *artifacts.Agent) (Versions, error)
	startWait  time.Duration // how long a started agent has to listen; 0 is 30 s
	// the build of Xray this plugin pins for an architecture; a test makes its own,
	// since the real binary can't be made in one
	pinnedBuild func(arch string) (version, sha string)

	dialOnce sync.Once
	dial     *net.Dialer

	mu  sync.Mutex // guards hub
	hub *hubCert
}

// pinnedXray is the Xray version this plugin pins and the checksum of its
// binary for an architecture ("" for an architecture it has none for).
func (e *Env) pinnedXray(arch string) (version, sha string) {
	if e.pinnedBuild != nil {
		return e.pinnedBuild(arch)
	}
	return pin.Version, pin.BinarySHA256[arch]
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Env) dialTimeout() time.Duration {
	if e.DialTimeout > 0 {
		return e.DialTimeout
	}
	return defaultDialTimeout
}

// settleTime is how long to wait for a node to settle after a change: the
// wait's own, unless a test shortens them all.
func (e *Env) settleTime(own time.Duration) time.Duration {
	if e.Settle > 0 {
		return e.Settle
	}
	return own
}

func (e *Env) log() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// dialer is the one dialer all node traffic goes through. SSH takes its
// DialContext, and so does the gRPC link, so a node given as a host name is
// looked up from Local as well.
func (e *Env) dialer() *net.Dialer {
	e.dialOnce.Do(func() { e.dial = netbind.Dialer(e.Local) })
	return e.dial
}

// hubCert is the certificate the hub shows an agent. It is made when it is
// first needed and kept in memory, never written anywhere: making one is
// cheap and the CA can always make another. A connection already open keeps
// the one it was made with, so replacing it near its end costs nothing.
type hubCert struct {
	certPEM, keyPEM string
	notAfter        time.Time
}

func (e *Env) hubClientCert() (certPEM, keyPEM string, err error) {
	if e.CA == nil {
		return "", "", ErrNoCA
	}
	now := e.now()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.hub == nil || pki.NeedsRenewal(e.hub.notAfter, now) {
		is, err := e.CA.IssueHubClientCert(now)
		if err != nil {
			return "", "", err
		}
		e.hub = &hubCert{certPEM: string(is.CertPEM), keyPEM: string(is.KeyPEM), notAfter: is.NotAfter}
	}
	return e.hub.certPEM, e.hub.keyPEM, nil
}
