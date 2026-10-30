// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/artifacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netbind"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
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

	dialOnce sync.Once
	dial     *net.Dialer

	mu  sync.Mutex // guards hub
	hub *hubCert
}

func (e *Env) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
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
