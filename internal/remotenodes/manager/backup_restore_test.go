// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

// trustedNet is a connector for nodes that trust one CA, as installed nodes
// do. Like the real one it makes the hub's certificate from the CA the manager
// holds at the moment it is asked, and a node lets it in only if that
// certificate was signed by the CA the node was installed under.
type trustedNet struct {
	Connector
	roots *x509.CertPool
	ca    func() *pki.CA // the manager's, asked on every try
	now   func() time.Time

	mu    sync.Mutex
	tries []trust
}

type trust struct {
	node uint
	ca   *x509.Certificate // the CA the hub's certificate was made from
	ok   bool
}

// verify is the node's side of the handshake for a hub certificate made by ca.
func (c *trustedNet) verify(ca *pki.CA) error {
	hub, err := ca.IssueHubClientCert(c.now())
	if err != nil {
		return err
	}
	block, _ := pem.Decode(hub.CertPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	_, err = leaf.Verify(x509.VerifyOptions{Roots: c.roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, CurrentTime: c.now()})
	return err
}

func (c *trustedNet) Connect(ctx context.Context, n model.Node) (Client, error) {
	ca := c.ca()
	if ca == nil {
		return nil, errors.New("the manager has no CA")
	}
	err := c.verify(ca)
	c.mu.Lock()
	c.tries = append(c.tries, trust{n.ID, ca.Certificate(), err == nil})
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("tls: bad certificate: %w", err)
	}
	return c.Connector.Connect(ctx, n)
}

func (c *trustedNet) triesOf(node uint) (all []trust) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, t := range c.tries {
		if t.node == node {
			all = append(all, t)
		}
	}
	return all
}

func (c *trustedNet) tried() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tries)
}

// A hub that starts on a fresh volume makes a CA of its own, and the nodes of
// a restored backup trust the old one. They are reached again, with no login:
// the connection is made from what the backup restored, under its CA.
func TestAfterAnImportEveryRestoredNodeIsConnectedUnderTheRestoredCA(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		a, _, _ := anInstall(t)
		_, file, err := a.ExportBackup(ctx, backupPass)
		must(t, err)

		trust := &trustedNet{roots: modelPool(a.CA().Certificate())}
		r := newRig(t, trustingNodes(trust))
		trust.ca = r.m.CA
		fresh := r.m.CA()
		if err := trust.verify(fresh); err == nil {
			t.Fatal("the nodes' side accepts a CA that is not theirs, so the test proves nothing")
		}
		r.start()
		defer r.stop()
		r.advance(time.Minute)
		if trust.tried() != 0 {
			t.Fatalf("a node was tried with none in the state: %v", trust.tries)
		}

		nodes, err := r.m.ImportBackup(ctx, backupPass, file)
		must(t, err)
		r.advance(2 * time.Second)
		if nodes != 3 {
			t.Errorf("%d nodes came back, want 3", nodes)
		}
		if r.m.CA() == fresh || !r.m.CA().Certificate().Equal(a.CA().Certificate()) {
			t.Error("the manager did not take the backup's CA")
		}
		// the two nodes that are installed connect, the one that never finished doesn't
		for _, id := range []uint{1, 2} {
			got := trust.triesOf(id)
			if len(got) != 1 || !got[0].ok || !got[0].ca.Equal(a.CA().Certificate()) {
				t.Errorf("node %d was tried as %+v, want once, under the backup's CA", id, got)
			}
			if p, _ := r.m.peek(id); !p.connected || p.misses != 0 || p.seen.IsZero() {
				t.Errorf("node %d after the restore: %+v", id, p)
			}
		}
		if got := trust.triesOf(3); len(got) != 0 {
			t.Errorf("the node whose install never finished was tried: %v", got)
		}
		// each node has the clients the state says, under the ids the backup kept
		if got, ok := holds(r.net.agent(1), subEmail(1)); !ok || got != clientIDOf(1, 1) {
			t.Errorf("node 1 holds %q (%v) for subscription 1", got, ok)
		}
		if got, ok := holds(r.net.agent(1), subEmail(2)); !ok || got != clientIDOf(2, 1) {
			t.Errorf("node 1 holds %q (%v) for subscription 2", got, ok)
		}
		if _, ok := holds(r.net.agent(2), subEmail(1)); ok {
			t.Error("node 2 has a client that was switched off there")
		}
		if r.m.CAError() != nil {
			t.Errorf("the CA is reported as unusable: %v", r.m.CAError())
		}
	})
}

// trustingNodes puts the trust-checking connector in front of the rig's fake
// network. The second node of anInstall is a host name, which the manager
// looks up for the bridge's sake, so it gets an answer that stays in the bubble.
func trustingNodes(trust *trustedNet) func(*Config) {
	return func(c *Config) {
		trust.Connector, trust.now = c.Connector, c.Now
		c.Connector = trust
		names := &fakeResolver{}
		names.set("node2.example.com", "203.0.113.9")
		c.Resolver = names
	}
}

func modelPool(cert *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(cert)
	return p
}

// An import before the loops have started is picked up by the first look.
func TestABackupImportedBeforeTheManagerRunsIsConnectedWhenItDoes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := context.Background()
		a, _, _ := anInstall(t)
		_, file, err := a.ExportBackup(ctx, backupPass)
		must(t, err)

		trust := &trustedNet{roots: modelPool(a.CA().Certificate())}
		r := newRig(t, trustingNodes(trust))
		trust.ca = r.m.CA
		_, err = r.m.ImportBackup(ctx, backupPass, file)
		must(t, err)
		if trust.tried() != 0 {
			t.Fatal("a node was tried before the manager ran")
		}
		r.start()
		defer r.stop()
		r.advance(time.Second)
		if got := trust.triesOf(1); len(got) != 1 || !got[0].ok {
			t.Errorf("node 1 was tried as %+v", got)
		}
	})
}

// The value CA returns changes when the CA does and not before, which is how
// whatever was made from it can tell it has to be made again.
func TestTheCAIsTheSameValueUntilItIsReplaced(t *testing.T) {
	cfg, log := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	first := m.CA()
	for i := 0; i < 3; i++ {
		m.Sync()
	}
	if m.CA() != first {
		t.Error("the same CA, read again, came back as another value")
	}

	other := newCA(t, epoch)
	setCA(t, cfg.Store, other)
	m.Sync()
	second := m.CA()
	if second == first || !second.Certificate().Equal(other.Certificate()) {
		t.Fatal("the manager still hands out the old CA")
	}
	m.Sync()
	m.Sync()
	if m.CA() != second {
		t.Error("the replaced CA, read again, came back as another value")
	}
	if log.count("made the CA") != 1 || log.count("the CA is in use", "replaced=true") != 1 {
		t.Errorf("the log should say once that a CA was made and once that another replaced it:\n%s", log)
	}
}
