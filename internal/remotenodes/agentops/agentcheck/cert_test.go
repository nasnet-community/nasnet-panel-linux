// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// serverSerial is the serial of the certificate file the agent serves from.
func (r *rig) serverSerial() string {
	r.t.Helper()
	pair, err := tls.LoadX509KeyPair(r.cfg.TLS.ServerCert, r.cfg.TLS.ServerKey)
	if err != nil {
		r.t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		r.t.Fatal(err)
	}
	return leaf.SerialNumber.String()
}

func TestRenewCertWithTheRealAgent(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	c := r.connect()
	old := r.serverSerial()
	r.env.Now = func() time.Time { return time.Now().Add(3 * time.Second) } // so the new one ends later than the old, and is already valid

	info, err := r.env.RenewCert(bg, c, r.node)
	if err != nil {
		t.Fatal(err)
	}
	// the agent holds the certificate the hub says it sent, and a new link meets it
	if got := r.serverSerial(); got != info.Serial || got == old {
		t.Errorf("the agent's certificate is %s, the hub says %s, it was %s", got, info.Serial, old)
	}
	if !info.NotAfter.After(time.Now().AddDate(0, 11, 0)) || !info.NotBefore.Before(time.Now()) || !info.NotAfter.After(info.NotBefore) {
		t.Errorf("valid %v to %v", info.NotBefore, info.NotAfter)
	}
	c2, err := r.env.Connect(bg, r.node)
	if err != nil {
		t.Fatalf("the hub can't connect with the renewed certificate: %v", err)
	}
	c2.Close()
	// it is still this node's: the id in the common name is kept, whatever the name is now
	pair, _ := tls.LoadX509KeyPair(r.cfg.TLS.ServerCert, r.cfg.TLS.ServerKey)
	leaf, _ := x509.ParseCertificate(pair.Certificate[0])
	if !strings.HasPrefix(leaf.Subject.CommonName, "node-7-") || leaf.VerifyHostname("127.0.0.1") != nil {
		t.Errorf("CN %q, for 127.0.0.1: %v", leaf.Subject.CommonName, leaf.VerifyHostname("127.0.0.1"))
	}
	// the key was sent once and the hub kept none: the old link still works, which a
	// renewal must not break
	if _, err := c.GetVersion(bg); err != nil {
		t.Errorf("the manager's own link: %v", err)
	}
}

func TestRenewCertForAnotherNodeIsRefusedAndNothingChanges(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	c := r.connect()
	before, _ := os.ReadFile(r.cfg.TLS.ServerCert)
	wrong := r.node
	wrong.ID = 8 // the agent is node 7
	_, err := r.env.RenewCert(bg, c, wrong)
	if err == nil {
		t.Fatal("a certificate for another node was installed")
	}
	if !strings.Contains(err.Error(), "refused the new certificate") || !strings.Contains(err.Error(), "another node") {
		t.Errorf("err = %q", err)
	}
	if after, _ := os.ReadFile(r.cfg.TLS.ServerCert); string(after) != string(before) {
		t.Error("the agent's certificate changed")
	}
	var inv *model.InvalidError
	if errors.As(err, &inv) {
		t.Error("it is the agent that refused, not the input")
	}
	_ = agentops.ErrNoCA
}
