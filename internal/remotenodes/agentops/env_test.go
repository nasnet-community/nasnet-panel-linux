// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/pki"
)

func testCA(t *testing.T) *pki.CA {
	t.Helper()
	ca, err := pki.NewCA(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func TestTheHubCertificateIsMadeOnceAndRenewedNearItsEnd(t *testing.T) {
	clock := time.Now()
	e := &Env{CA: testCA(t), Now: func() time.Time { return clock }}

	cert1, key1, err := e.hubClientCert()
	if err != nil {
		t.Fatal(err)
	}
	cert2, key2, err := e.hubClientCert()
	if err != nil || cert1 != cert2 || key1 != key2 {
		t.Fatalf("a second call made another certificate (%v)", err)
	}

	// well before the end it stays the same
	clock = clock.AddDate(0, 6, 0)
	if cert3, _, _ := e.hubClientCert(); cert3 != cert1 {
		t.Error("the certificate was replaced half way through its life")
	}

	// with the renewal window left (30 days) it is replaced, once
	clock = e.hub.notAfter.Add(-pki.RenewBefore + time.Hour)
	cert4, key4, err := e.hubClientCert()
	if err != nil {
		t.Fatal(err)
	}
	if cert4 == cert1 || key4 == key1 {
		t.Fatal("the certificate was kept into its renewal window")
	}
	if cert5, _, _ := e.hubClientCert(); cert5 != cert4 {
		t.Error("the renewed certificate was replaced again at once")
	}
}

func TestTheHubCertificateIsForClientsAndSignedByTheCA(t *testing.T) {
	ca := testCA(t)
	e := &Env{CA: ca}
	certPEM, keyPEM, err := e.hubClientCert()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := parseCert([]byte(certPEM))
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != pki.HubCommonName {
		t.Errorf("CN %q", leaf.Subject.CommonName)
	}
	if err := leaf.CheckSignatureFrom(ca.Certificate()); err != nil {
		t.Errorf("not signed by the CA: %v", err)
	}
	if keyPEM == "" {
		t.Error("no key")
	}
}

func TestNoCAIsSaidPlainly(t *testing.T) {
	if _, _, err := (&Env{}).hubClientCert(); err != ErrNoCA {
		t.Fatalf("err = %v, want ErrNoCA", err)
	}
}

// One Env serves every node, from several jobs at once.
func TestTheEnvIsSafeForConcurrentUse(t *testing.T) {
	e := &Env{CA: testCA(t), Local: netip.MustParseAddr("127.0.0.1")}
	var wg sync.WaitGroup
	certs := make([]string, 32)
	dialers := make([]*net.Dialer, 32)
	for i := range certs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _, err := e.hubClientCert()
			if err != nil {
				t.Error(err)
			}
			certs[i], dialers[i] = c, e.dialer()
		}()
	}
	wg.Wait()
	for i := range certs {
		if certs[i] != certs[0] || dialers[i] != dialers[0] {
			t.Fatalf("goroutine %d got its own certificate or dialer", i)
		}
	}
}

// The hub's certificate lives in memory only. The test runs where it can see
// anything that would be written next to it, in the working directory and in
// the temp directory.
func TestNothingIsWrittenToDisk(t *testing.T) {
	work, tmp := t.TempDir(), t.TempDir()
	t.Chdir(work)
	t.Setenv("TMPDIR", tmp)
	e := &Env{CA: testCA(t)}
	for range 3 {
		if _, _, err := e.hubClientCert(); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{work, tmp} {
		if left, _ := os.ReadDir(dir); len(left) != 0 {
			t.Errorf("%s holds %d files", dir, len(left))
		}
	}
}

func TestTheDialerIsBoundToLocal(t *testing.T) {
	e := &Env{Local: netip.MustParseAddr("192.168.50.18")}
	d := e.dialer()
	if la, ok := d.LocalAddr.(*net.TCPAddr); !ok || la.IP.String() != "192.168.50.18" {
		t.Fatalf("local address %v", d.LocalAddr)
	}
	if d.Resolver == nil || !d.Resolver.PreferGo || d.Resolver.Dial == nil {
		t.Error("name lookups are not bound to the address")
	}
	if e.dialer() != d {
		t.Error("a second call built another dialer")
	}
	// no address: the system chooses
	if d := (&Env{}).dialer(); d.LocalAddr != nil || d.Resolver != nil {
		t.Errorf("an Env with no Local is bound to %v", d.LocalAddr)
	}
}
