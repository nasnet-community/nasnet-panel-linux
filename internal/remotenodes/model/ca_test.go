// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

func newCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(t0)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// withCA is small(t) with a CA in it.
func withCA(t *testing.T) (State, *pki.CA) {
	t.Helper()
	ca := newCA(t)
	s := small(t)
	s.CACert, s.CAKey = string(ca.CertPEM()), string(ca.KeyPEM())
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, ca
}

func TestAStateWithoutACAIsValid(t *testing.T) {
	// a fresh install has none until the hub first needs one
	if err := NewState().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTheCAGoesToTheFileAndComesBack(t *testing.T) {
	s, ca := withCA(t)
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	// the file is the one place the CA lives, key and all
	for _, key := range []string{`"ca_cert"`, `"ca_key"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("the file has no %s", key)
		}
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.CACert != s.CACert || back.CAKey != s.CAKey {
		t.Fatal("the CA changed in the file")
	}
	loaded, err := pki.Load([]byte(back.CACert), []byte(back.CAKey))
	if err != nil || !loaded.Certificate().Equal(ca.Certificate()) {
		t.Fatalf("the CA that came back doesn't load as the same CA: %v", err)
	}
	// a state with none writes neither key
	empty, _ := Encode(NewState())
	if strings.Contains(string(empty), "ca_") {
		t.Errorf("a fresh state writes CA keys:\n%s", empty)
	}
}

func TestValidateTakesBothHalvesOfTheCAOrNeither(t *testing.T) {
	ca, other := newCA(t), newCA(t)
	node, err := ca.IssueNodeCert(1, "n", "203.0.113.7", t0)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ cert, key string }{
		"only the certificate": {string(ca.CertPEM()), ""},
		"only the key":         {"", string(ca.KeyPEM())},
		"spaces":               {" ", " "},
		"text":                 {"certificate", "key"},
		"another CA's key":     {string(ca.CertPEM()), string(other.KeyPEM())},
		"the halves swapped":   {string(ca.KeyPEM()), string(ca.CertPEM())},
		"a node's certificate": {string(node.CertPEM), string(node.KeyPEM)},
		"two certificates":     {string(ca.CertPEM()) + string(other.CertPEM()), string(ca.KeyPEM())},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			s := small(t)
			s.CACert, s.CAKey = c.cert, c.key
			err := s.Validate()
			var inv *InvalidError
			if !errors.As(err, &inv) {
				t.Fatalf("Validate = %v, want an InvalidError", err)
			}
			// it says what is wrong and never quotes what it was given
			secret := string(ca.KeyPEM())
			if body := strings.SplitN(secret, "\n", 3)[1]; strings.Contains(err.Error(), body[:24]) || strings.Contains(err.Error(), "BEGIN") {
				t.Fatalf("the message quotes a key: %v", err)
			}
		})
	}
}

// Decode and the store's Update both end in Validate, so a state with a
// broken CA is refused when it is read and when it is edited. A file that was
// hand-edited, or half written, can't turn into a hub that issues nothing.
func TestAStateWithABrokenCAIsRefusedOnReadAndOnEdit(t *testing.T) {
	good, _ := withCA(t)
	b, err := Encode(good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(b); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*State){
		"the key removed":      func(s *State) { s.CAKey = "" },
		"the certificate gone": func(s *State) { s.CACert = "" },
		"another CA's key":     func(s *State) { s.CAKey = string(newCA(t).KeyPEM()) },
	} {
		t.Run(name, func(t *testing.T) {
			edit := good.Clone()
			mutate(&edit)
			if err := edit.CheckEdit(good); err == nil {
				t.Fatal("the edit was allowed")
			}
			broken, err := Encode(edit)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(broken); err == nil {
				t.Fatal("the file was read")
			}
		})
	}
	// and setting the CA on a state that has none is an ordinary edit
	fresh := small(t)
	edit := fresh.Clone()
	ca := newCA(t)
	edit.CACert, edit.CAKey = string(ca.CertPEM()), string(ca.KeyPEM())
	if err := edit.CheckEdit(fresh); err != nil {
		t.Fatalf("setting the first CA: %v", err)
	}
}

// pki and the model each have a rule for what a host may be. A node the model
// takes has to get a certificate, and a host the model refuses shouldn't get
// one from pki either. This holds the two together.
func TestPKIAndTheModelAgreeOnHosts(t *testing.T) {
	ca := newCA(t)
	for _, host := range []string{
		"203.0.113.7", "2001:db8::7", "::1", "127.0.0.1", "::ffff:192.0.2.1", "localhost", "node1", "Node1.Example.COM",
		"a.b.c", "xn--bcher-kva.de", "a-b.example.org", "1a.example.com", strings.Repeat("a", 63) + ".com",
		"", " ", "0.0.0.0", "::", "fe80::1%eth0", "exa mple.com", "ex_ample.com", "-a.com", "a-.com", "a..com", ".a.com",
		"a.com.", "1.2.3", "256.1.1.1", "bücher.de", "*.example.com", "https://x.com", "x.com:443", "a/b",
		strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com",
	} {
		clean, err := CleanHost(host)
		if err == nil {
			// a node's Host is always in CleanHost's form
			if _, certErr := ca.IssueNodeCert(1, "n", clean, t0); certErr != nil {
				t.Errorf("%q: the model takes it as %q, pki refuses: %v", host, clean, certErr)
			}
			continue
		}
		if _, certErr := ca.IssueNodeCert(1, "n", host, t0); certErr == nil {
			t.Errorf("%q: the model refuses it (%v), pki issues a certificate", host, err)
		}
	}
}

// Dropping the CA is a valid state (a fresh install has none), so only the
// edit is refused. Another CA in its place is allowed: a backup import does it.
func TestAnEditCannotDropTheCAButCanReplaceIt(t *testing.T) {
	before, _ := withCA(t)

	dropped := before.Clone()
	dropped.CACert, dropped.CAKey = "", ""
	if err := dropped.Validate(); err != nil {
		t.Fatalf("a state with no CA is valid: %v", err)
	}
	var inv *InvalidError
	if err := dropped.CheckEdit(before); !errors.As(err, &inv) {
		t.Fatalf("dropping the CA: %v, want an InvalidError", err)
	}

	replaced := before.Clone()
	next := newCA(t)
	replaced.CACert, replaced.CAKey = string(next.CertPEM()), string(next.KeyPEM())
	if err := replaced.CheckEdit(before); err != nil {
		t.Fatalf("replacing the CA: %v", err)
	}
	// and with none to lose, nothing is dropped
	if err := before.CheckEdit(small(t)); err != nil {
		t.Fatalf("setting the first CA: %v", err)
	}
	if err := small(t).CheckEdit(small(t)); err != nil {
		t.Fatalf("an edit with no CA before or after: %v", err)
	}
}
