// SPDX-License-Identifier: AGPL-3.0-or-later

package agentcheck

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/config"
)

// A handshake target of the test's own: a TLS server on a loopback port, so the
// agent's own check, which dials the target from the node, has something to
// shake hands with.

// selfSigned is a certificate for name, with as many other names as a test
// wants to make it long.
func selfSigned(t *testing.T, key crypto.Signer, name string, extraNames int) tls.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for i := 0; i < extraNames; i++ {
		tmpl.DNSNames = append(tmpl.DNSNames, fmt.Sprintf("a-name-that-makes-the-certificate-long-%04d.example.test", i))
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func ecKey(t *testing.T) crypto.Signer {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func rsaKey(t *testing.T) crypto.Signer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// tlsTarget serves cfg on a loopback port and returns localhost:port, so that
// the hello carries a server name.
func tlsTarget(t *testing.T, cfg *tls.Config) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				tls.Server(c, cfg).Handshake()
			}()
		}
	}()
	return fmt.Sprintf("localhost:%d", l.Addr().(*net.TCPAddr).Port)
}

// closedTarget is a localhost port nothing listens on.
func closedTarget(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprintf("localhost:%d", l.Addr().(*net.TCPAddr).Port)
}

func TestTheRealAgentChecksHandshakeTargets(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	c := r.connect()

	targets := map[string]string{
		"a good one":                  tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "small.example.test", 0)}, MinVersion: tls.VersionTLS13}),
		"one with only TLS 1.2":       tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "old.example.test", 0)}, MaxVersion: tls.VersionTLS12}),
		"one that insists on P-256":   tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "p256.example.test", 0)}, MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.CurveP256}}),
		"one with a huge certificate": tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "huge.example.test", 450)}, MinVersion: tls.VersionTLS13}),
		"a port nothing listens on":   closedTarget(t),
	}
	for _, want := range []struct {
		name              string
		ok, tls13, x25519 bool
		say               string
	}{
		{"a good one", true, true, true, ""},
		{"one with only TLS 1.2", false, false, false, "doesn't use TLS 1.3"},
		{"one that insists on P-256", false, true, false, "key exchange"},
		{"one with a huge certificate", false, true, true, "over 8 KB"},
		{"a port nothing listens on", false, false, false, "connect"},
	} {
		t.Run(want.name, func(t *testing.T) {
			chk, err := r.env.CheckTarget(bg, c, targets[want.name])
			if err != nil {
				t.Fatal(err)
			}
			if chk.OK != want.ok || chk.TLS13 != want.tls13 || chk.X25519 != want.x25519 {
				t.Errorf("%+v", chk)
			}
			if chk.OK {
				if chk.Error != "" || chk.HandshakeBytes <= 0 || chk.HandshakeBytes > 8192 || chk.LatencyMs < 0 {
					t.Errorf("a good target: %+v", chk)
				}
			} else if !strings.Contains(chk.Error, want.say) {
				t.Errorf("the error is %q, want %q in it", chk.Error, want.say)
			}
		})
	}
}

// Of two that work, the one with the smaller handshake is the choice, whatever
// order they come in.
func TestTheRealAgentChoosesTheSmallerHandshake(t *testing.T) {
	r := startRig(t, options{profile: config.ProfileManaged})
	c := r.connect()
	small := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "small.example.test", 0)}, MinVersion: tls.VersionTLS13})
	bigger := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, rsaKey(t), "bigger.example.test", 20)}, MinVersion: tls.VersionTLS13})
	broken := tlsTarget(t, &tls.Config{Certificates: []tls.Certificate{selfSigned(t, ecKey(t), "old.example.test", 0)}, MaxVersion: tls.VersionTLS12})

	for _, order := range [][]string{{bigger, small, broken}, {broken, small, bigger}, {small, bigger, broken}} {
		choice, err := r.env.ProbeTargets(bg, c, order)
		if err != nil {
			t.Fatal(err)
		}
		if choice.Best != small {
			t.Errorf("order %v: best %q, want %q (results %+v)", order, choice.Best, small, choice.Results)
		}
		if len(choice.Results) != 3 {
			t.Fatalf("%d results", len(choice.Results))
		}
		for i, res := range choice.Results {
			if res.Target != order[i] {
				t.Errorf("result %d is for %s, want %s: the order given", i, res.Target, order[i])
			}
		}
	}
	// when none works there is no best, and no error: the results say why
	choice, err := r.env.ProbeTargets(bg, c, []string{broken, closedTarget(t)})
	if err != nil || choice.Best != "" || len(choice.Results) != 2 {
		t.Fatalf("%+v, %v", choice, err)
	}
}
