// SPDX-License-Identifier: AGPL-3.0-or-later

package pki

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func newTestCA(t *testing.T, now time.Time) *CA {
	t.Helper()
	ca, err := NewCA(now)
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func parse(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()
	b, rest := pem.Decode(certPEM)
	if b == nil || b.Type != "CERTIFICATE" || len(rest) != 0 {
		t.Fatalf("not one certificate block: %q", certPEM)
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func roots(ca *CA) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(ca.Certificate())
	return p
}

// verify is what a TLS peer does with a chain: the CA as the only root, one
// key usage asked for, and the clock at at.
func verify(leaf *x509.Certificate, ca *CA, usage x509.ExtKeyUsage, at time.Time) error {
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots(ca), KeyUsages: []x509.ExtKeyUsage{usage}, CurrentTime: at})
	return err
}

func issueNode(t *testing.T, ca *CA, id uint, name, host string) (*Issued, *x509.Certificate) {
	t.Helper()
	is, err := ca.IssueNodeCert(id, name, host, t0)
	if err != nil {
		t.Fatalf("IssueNodeCert(%d, %q, %q): %v", id, name, host, err)
	}
	return is, parse(t, is.CertPEM)
}

func p256Key(t *testing.T, keyPEM []byte) *ecdsa.PrivateKey {
	t.Helper()
	b, rest := pem.Decode(keyPEM)
	if b == nil || b.Type != "EC PRIVATE KEY" || len(rest) != 0 {
		t.Fatalf("not one EC PRIVATE KEY block: %q", keyPEM)
	}
	k, err := x509.ParseECPrivateKey(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if k.Curve != elliptic.P256() {
		t.Fatalf("key curve is %s", k.Curve.Params().Name)
	}
	return k
}

func TestNewCAIsASelfSignedP256CAForTenYears(t *testing.T) {
	ca := newTestCA(t, t0)
	c := ca.Certificate()
	if !c.IsCA || !c.BasicConstraintsValid || c.MaxPathLen != 0 || !c.MaxPathLenZero {
		t.Fatalf("not a CA that signs leaves only: %+v", c)
	}
	if c.KeyUsage != x509.KeyUsageCertSign|x509.KeyUsageCRLSign {
		t.Fatalf("key usage %v", c.KeyUsage)
	}
	if !c.NotBefore.Equal(t0.Add(-time.Hour)) || !c.NotAfter.Equal(t0.AddDate(10, 0, 0)) {
		t.Fatalf("valid %v to %v", c.NotBefore, c.NotAfter)
	}
	if pub, ok := c.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
		t.Fatalf("public key %T", c.PublicKey)
	}
	if err := c.CheckSignatureFrom(c); err != nil {
		t.Fatalf("not self-signed: %v", err)
	}
	if c.SerialNumber.Sign() <= 0 || len(c.SubjectKeyId) == 0 {
		t.Fatalf("serial %v, subject key id %x", c.SerialNumber, c.SubjectKeyId)
	}
	if !p256Key(t, ca.KeyPEM()).PublicKey.Equal(c.PublicKey) {
		t.Fatal("the key is not the certificate's")
	}
	if got := parse(t, ca.CertPEM()); !got.Equal(c) {
		t.Fatal("CertPEM is not the certificate")
	}
}

func TestANodeCertVerifiesForItsIPAddress(t *testing.T) {
	ca := newTestCA(t, t0)
	for _, host := range []string{"203.0.113.7", "2001:db8::7"} {
		is, leaf := issueNode(t, ca, 7, "Tehran", host)
		if err := verify(leaf, ca, x509.ExtKeyUsageServerAuth, t0); err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if err := leaf.VerifyHostname(host); err != nil {
			t.Errorf("%s: %v", host, err)
		}
		for _, other := range []string{"203.0.113.8", "2001:db8::8", "node.example.com", "localhost"} {
			if leaf.VerifyHostname(other) == nil {
				t.Errorf("%s: accepted for %s", host, other)
			}
		}
		if len(leaf.DNSNames) != 0 || len(leaf.IPAddresses) != 1 {
			t.Errorf("%s: SANs are %v and %v", host, leaf.IPAddresses, leaf.DNSNames)
		}
		if is.Serial != leaf.SerialNumber.String() {
			t.Errorf("serial %s, certificate %s", is.Serial, leaf.SerialNumber)
		}
	}
}

func TestANodeCertVerifiesForItsDNSName(t *testing.T) {
	ca := newTestCA(t, t0)
	_, leaf := issueNode(t, ca, 2, "Frankfurt", "Node1.Example.COM")
	if err := verify(leaf, ca, x509.ExtKeyUsageServerAuth, t0); err != nil {
		t.Fatal(err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "node1.example.com" || len(leaf.IPAddresses) != 0 {
		t.Fatalf("SANs are %v and %v", leaf.IPAddresses, leaf.DNSNames)
	}
	for _, name := range []string{"node1.example.com", "NODE1.EXAMPLE.COM"} {
		if err := leaf.VerifyHostname(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, other := range []string{"203.0.113.7", "node2.example.com", "example.com", "x.node1.example.com"} {
		if leaf.VerifyHostname(other) == nil {
			t.Errorf("accepted for %s", other)
		}
	}
}

func TestANodeCertIsForServingAndTheHubCertIsForDialling(t *testing.T) {
	ca := newTestCA(t, t0)
	_, node := issueNode(t, ca, 1, "n", "203.0.113.7")
	hub, err := ca.IssueHubClientCert(t0)
	if err != nil {
		t.Fatal(err)
	}
	hubLeaf := parse(t, hub.CertPEM)

	if err := verify(node, ca, x509.ExtKeyUsageServerAuth, t0); err != nil {
		t.Fatal(err)
	}
	if err := verify(hubLeaf, ca, x509.ExtKeyUsageClientAuth, t0); err != nil {
		t.Fatal(err)
	}
	// each is refused for the other's job, so a node's certificate can't drive an agent
	var bad x509.CertificateInvalidError
	if err := verify(node, ca, x509.ExtKeyUsageClientAuth, t0); !errors.As(err, &bad) || bad.Reason != x509.IncompatibleUsage {
		t.Errorf("a node certificate as a client certificate: %v", err)
	}
	if err := verify(hubLeaf, ca, x509.ExtKeyUsageServerAuth, t0); !errors.As(err, &bad) || bad.Reason != x509.IncompatibleUsage {
		t.Errorf("the hub certificate as a server certificate: %v", err)
	}
	if len(node.ExtKeyUsage) != 1 || len(hubLeaf.ExtKeyUsage) != 1 {
		t.Errorf("usages %v and %v", node.ExtKeyUsage, hubLeaf.ExtKeyUsage)
	}
	for _, c := range []*x509.Certificate{node, hubLeaf} {
		if c.IsCA || !c.BasicConstraintsValid || c.KeyUsage != x509.KeyUsageDigitalSignature {
			t.Errorf("%s: CA %v, basic constraints %v, key usage %v", c.Subject.CommonName, c.IsCA, c.BasicConstraintsValid, c.KeyUsage)
		}
		if pub, ok := c.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
			t.Errorf("%s: public key %T", c.Subject.CommonName, c.PublicKey)
		}
		if string(c.AuthorityKeyId) != string(ca.Certificate().SubjectKeyId) {
			t.Errorf("%s: names another issuer key", c.Subject.CommonName)
		}
	}
	if hubLeaf.Subject.CommonName != HubCommonName || len(hubLeaf.DNSNames)+len(hubLeaf.IPAddresses) != 0 {
		t.Errorf("hub certificate: CN %q, SANs %v %v", hubLeaf.Subject.CommonName, hubLeaf.DNSNames, hubLeaf.IPAddresses)
	}
	if !p256Key(t, hub.KeyPEM).PublicKey.Equal(hubLeaf.PublicKey) {
		t.Error("the hub key is not its certificate's")
	}
}

// The agent reads the id out of the CN, accepts the hub's certificate as a
// client and turns a node's away. These are its own functions.
func TestTheAgentReadsWhatWeIssue(t *testing.T) {
	ca := newTestCA(t, t0)
	for _, id := range []uint{1, 7, 42, 4294967295} {
		_, leaf := issueNode(t, ca, id, "ali", "203.0.113.7")
		got, ok := certs.NodeIDFromCommonName(leaf.Subject.CommonName)
		if !ok || got != id {
			t.Errorf("id %d: the agent reads %d, %v from %q", id, got, ok, leaf.Subject.CommonName)
		}
		if certs.RejectNodeClient(leaf) == nil {
			t.Errorf("id %d: the agent would take a node certificate as a client", id)
		}
	}
	hub, err := ca.IssueHubClientCert(t0)
	if err != nil {
		t.Fatal(err)
	}
	hubLeaf := parse(t, hub.CertPEM)
	if id, ok := certs.NodeIDFromCommonName(hubLeaf.Subject.CommonName); ok {
		t.Errorf("the hub's CN reads as node %d", id)
	}
	if err := certs.RejectNodeClient(hubLeaf); err != nil {
		t.Errorf("the agent turns the hub away: %v", err)
	}
}

func TestCertsLastTwelveMonthsFromAnHourBack(t *testing.T) {
	ca := newTestCA(t, t0)
	is, leaf := issueNode(t, ca, 1, "n", "203.0.113.7")
	hub, err := ca.IssueHubClientCert(t0)
	if err != nil {
		t.Fatal(err)
	}
	wantFrom, wantTo := t0.Add(-time.Hour), t0.AddDate(0, 12, 0)
	for name, c := range map[string]struct{ from, to time.Time }{
		"node": {is.NotBefore, is.NotAfter}, "hub": {hub.NotBefore, hub.NotAfter},
		"node, as parsed": {leaf.NotBefore, leaf.NotAfter},
	} {
		if !c.from.Equal(wantFrom) || !c.to.Equal(wantTo) || c.from.Location() != time.UTC || c.to.Location() != time.UTC {
			t.Errorf("%s: valid %v to %v, want %v to %v", name, c.from, c.to, wantFrom, wantTo)
		}
	}
	for _, c := range []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"issued now", t0, true},
		{"an hour back, the first second", wantFrom, true},
		{"before that", wantFrom.Add(-time.Second), false},
		{"on the last day", wantTo, true},
		{"a second after", wantTo.Add(time.Second), false},
		{"in two years", t0.AddDate(2, 0, 0), false},
	} {
		err := verify(leaf, ca, x509.ExtKeyUsageServerAuth, c.at)
		var inv x509.CertificateInvalidError
		if c.ok && err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if !c.ok && (!errors.As(err, &inv) || inv.Reason != x509.Expired) {
			t.Errorf("%s: %v, want it expired", c.name, err)
		}
	}
	// the CA outlasts them, so a certificate renewed at 30 days left is still under a good CA
	if err := verify(leaf, ca, x509.ExtKeyUsageServerAuth, t0.AddDate(0, 11, 0)); err != nil {
		t.Errorf("eleven months on: %v", err)
	}
}

func TestSerialsAreRandomAndUnique(t *testing.T) {
	ca := newTestCA(t, t0)
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	big100 := new(big.Int).Lsh(big.NewInt(1), 100)
	seen := map[string]bool{ca.Certificate().SerialNumber.String(): true}
	large := 0
	for i := 0; i < 300; i++ {
		var is *Issued
		var err error
		if i%2 == 0 {
			is, err = ca.IssueNodeCert(uint(i+1), "n", "203.0.113.7", t0)
		} else {
			is, err = ca.IssueHubClientCert(t0)
		}
		if err != nil {
			t.Fatal(err)
		}
		n, ok := new(big.Int).SetString(is.Serial, 10)
		if !ok || n.Sign() <= 0 || n.Cmp(limit) >= 0 {
			t.Fatalf("serial %q is not a positive number under 2^128", is.Serial)
		}
		if is.Serial != parse(t, is.CertPEM).SerialNumber.String() {
			t.Fatalf("serial %s is not the certificate's", is.Serial)
		}
		if seen[is.Serial] {
			t.Fatalf("serial %s came twice", is.Serial)
		}
		seen[is.Serial] = true
		if n.Cmp(big100) > 0 {
			large++
		}
	}
	// a counter or a short random number would leave nearly all of them below 2^100
	if large < 250 {
		t.Fatalf("only %d of 300 serials are above 2^100", large)
	}
}

func TestEachCertificateGetsItsOwnKey(t *testing.T) {
	ca := newTestCA(t, t0)
	a, _ := issueNode(t, ca, 1, "n", "203.0.113.7")
	b, _ := issueNode(t, ca, 1, "n", "203.0.113.7")
	if string(a.KeyPEM) == string(b.KeyPEM) {
		t.Fatal("two certificates share a key")
	}
	if p256Key(t, a.KeyPEM).PublicKey.Equal(&ca.key.PublicKey) {
		t.Fatal("a node's key is the CA's")
	}
}

func TestNeedsRenewalAtThirtyDaysLeft(t *testing.T) {
	now := t0
	for _, c := range []struct {
		name     string
		notAfter time.Time
		want     bool
	}{
		{"a year left", now.AddDate(1, 0, 0), false},
		{"31 days left", now.Add(31 * 24 * time.Hour), false},
		{"a second over 30 days", now.Add(30*24*time.Hour + time.Second), false},
		{"a nanosecond over 30 days", now.Add(30*24*time.Hour + 1), false},
		{"exactly 30 days", now.Add(30 * 24 * time.Hour), true},
		{"a second under 30 days", now.Add(30*24*time.Hour - time.Second), true},
		{"a day left", now.Add(24 * time.Hour), true},
		{"ends this instant", now, true},
		{"ended a day ago", now.Add(-24 * time.Hour), true},
		{"no certificate on record", time.Time{}, true},
	} {
		if got := NeedsRenewal(c.notAfter, now); got != c.want {
			t.Errorf("%s: NeedsRenewal = %v, want %v", c.name, got, c.want)
		}
	}
	// a certificate the hub just issued is not up for renewal, one from 11 months ago is
	ca := newTestCA(t, t0)
	is, _ := issueNode(t, ca, 1, "n", "203.0.113.7")
	if NeedsRenewal(is.NotAfter, t0) || !NeedsRenewal(is.NotAfter, t0.AddDate(0, 11, 5)) {
		t.Errorf("renewal of a certificate ending %v", is.NotAfter)
	}
}

func TestPEMRoundTripsThroughLoad(t *testing.T) {
	ca := newTestCA(t, t0)
	back, err := Load(ca.CertPEM(), ca.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if string(back.CertPEM()) != string(ca.CertPEM()) || string(back.KeyPEM()) != string(ca.KeyPEM()) {
		t.Fatal("PEM came back different")
	}
	if !back.Certificate().Equal(ca.Certificate()) {
		t.Fatal("the certificate came back different")
	}
	// the CA that was loaded signs what the first one's certificate vouches for
	_, leaf := issueNode(t, back, 3, "n", "203.0.113.7")
	if err := verify(leaf, ca, x509.ExtKeyUsageServerAuth, t0); err != nil {
		t.Fatal(err)
	}
	// and what Load keeps is its own copy
	b := ca.CertPEM()
	b[10] ^= 0xff
	k := ca.KeyPEM()
	k[10] ^= 0xff
	if string(ca.CertPEM()) == string(b) || string(ca.KeyPEM()) == string(k) {
		t.Fatal("CertPEM or KeyPEM handed out the CA's own bytes")
	}
}

func TestLoadTakesAPKCS8KeyAndTrailingWhitespace(t *testing.T) {
	ca := newTestCA(t, t0)
	der, err := x509.MarshalPKCS8PrivateKey(ca.key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	back, err := Load(append(ca.CertPEM(), "\n\n  \n"...), append(pkcs8, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	// it comes out the way a new CA's does, whatever it went in as
	if string(back.KeyPEM()) != string(ca.KeyPEM()) {
		t.Fatal("a PKCS #8 key was not written back as SEC 1")
	}
}

// pemOf is one block, with a header if the map has any.
func pemOf(typ string, der []byte, headers map[string]string) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Headers: headers, Bytes: der})
}

func selfSigned(t *testing.T, tmpl *x509.Certificate, key any) ([]byte, []byte) {
	t.Helper()
	var pub any
	var keyDER []byte
	var err error
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		pub = &k.PublicKey
		keyDER, err = x509.MarshalECPrivateKey(k)
	case *rsa.PrivateKey:
		pub = &k.PublicKey
		keyDER, err = x509.MarshalPKCS8PrivateKey(k)
	}
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber = big.NewInt(1)
	tmpl.NotBefore, tmpl.NotAfter = t0.Add(-time.Hour), t0.AddDate(1, 0, 0)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	typ := "EC PRIVATE KEY"
	if _, ok := key.(*rsa.PrivateKey); ok {
		typ = "PRIVATE KEY"
	}
	return pemOf("CERTIFICATE", der, nil), pemOf(typ, keyDER, nil)
}

func TestLoadRefusesWhatIsNotOurCA(t *testing.T) {
	ca := newTestCA(t, t0)
	other := newTestCA(t, t0)
	node, err := ca.IssueNodeCert(1, "n", "203.0.113.7", t0)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := ca.IssueHubClientCert(t0)
	if err != nil {
		t.Fatal(err)
	}

	// a CA that another CA signed: a CA, but not the root of anything
	interKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	interDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "inter"}, NotBefore: t0.Add(-time.Hour), NotAfter: t0.AddDate(1, 0, 0),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}, ca.cert, &interKey.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	interKeyDER, _ := x509.MarshalECPrivateKey(interKey)

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaCert, rsaKeyPEM := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "rsa"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, rsaKey)
	p384Key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	p384Cert, p384KeyPEM := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "p384"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}, p384Key)
	noSignKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	noSignCert, noSignKeyPEM := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "nosign"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature}, noSignKey)
	notCAKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	notCACert, notCAKeyPEM := selfSigned(t, &x509.Certificate{Subject: pkix.Name{CommonName: "leaf"}}, notCAKey)

	encryptedKey := pemOf("EC PRIVATE KEY", []byte("x"), map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,00"})
	twoCerts := append(ca.CertPEM(), other.CertPEM()...)
	twoKeys := append(ca.KeyPEM(), other.KeyPEM()...)

	cases := []struct {
		name          string
		certPEM, keyP []byte
	}{
		{"the key of another CA", ca.CertPEM(), other.KeyPEM()},
		{"a node's certificate and key", node.CertPEM, node.KeyPEM},
		{"the hub's certificate and key", hub.CertPEM, hub.KeyPEM},
		{"a leaf with its own key", notCACert, notCAKeyPEM},
		{"a CA that another CA signed", pemOf("CERTIFICATE", interDER, nil), pemOf("EC PRIVATE KEY", interKeyDER, nil)},
		{"an RSA CA", rsaCert, rsaKeyPEM},
		{"a P-384 CA", p384Cert, p384KeyPEM},
		{"a CA that may not sign", noSignCert, noSignKeyPEM},
		{"the arguments the wrong way round", ca.KeyPEM(), ca.CertPEM()},
		{"an RSA key for our certificate", ca.CertPEM(), rsaKeyPEM},
		{"a node's key for our certificate", ca.CertPEM(), node.KeyPEM},
		{"nothing", nil, nil},
		{"no key", ca.CertPEM(), nil},
		{"no certificate", nil, ca.KeyPEM()},
		{"text", []byte("a certificate, please"), []byte("and its key")},
		{"two certificates", twoCerts, ca.KeyPEM()},
		{"two keys", ca.CertPEM(), twoKeys},
		{"something after the block", append(ca.CertPEM(), "trailing words"...), ca.KeyPEM()},
		{"an encrypted key", ca.CertPEM(), encryptedKey},
		{"a certificate that isn't one", pemOf("CERTIFICATE", []byte("not der"), nil), ca.KeyPEM()},
		{"a key that isn't one", ca.CertPEM(), pemOf("EC PRIVATE KEY", []byte("not der"), nil)},
		{"too much of it", append([]byte(strings.Repeat("# padding\n", 2000)), ca.CertPEM()...), ca.KeyPEM()},
	}
	secret := string(ca.KeyPEM())
	body := strings.ReplaceAll(strings.SplitN(secret, "\n", 3)[1], " ", "")
	for _, c := range cases {
		got, err := Load(c.certPEM, c.keyP)
		if got != nil || !errors.Is(err, ErrBadCA) {
			t.Errorf("%s: got %v, %v", c.name, got, err)
			continue
		}
		// what failed is said, never what was in it
		if msg := err.Error(); strings.Contains(msg, "BEGIN") || strings.Contains(msg, body[:24]) || len(msg) > 200 {
			t.Errorf("%s: the message carries more than it should: %q", c.name, msg)
		}
	}
}

func TestLoadDoesNotLookAtTheClock(t *testing.T) {
	// a CA made eleven years ago is out of date, and still a CA that loads
	old := newTestCA(t, t0.AddDate(-11, 0, 0))
	back, err := Load(old.CertPEM(), old.KeyPEM())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := back.IssueNodeCert(1, "n", "203.0.113.7", t0); !errors.Is(err, ErrCAExpired) {
		t.Fatalf("issuing from an expired CA: %v", err)
	}
	if _, err := back.IssueHubClientCert(t0); !errors.Is(err, ErrCAExpired) {
		t.Fatalf("a hub certificate from an expired CA: %v", err)
	}
}

func TestIssuingStaysInsideTheCAsLife(t *testing.T) {
	ca := newTestCA(t, t0)
	from, to := ca.Certificate().NotBefore, ca.Certificate().NotAfter

	// a clock behind the CA's start, as on a router that has not found the time yet
	for _, behind := range []time.Time{from.Add(-time.Second), time.Unix(0, 0)} {
		if _, err := ca.IssueNodeCert(1, "n", "203.0.113.7", behind); !errors.Is(err, ErrCANotYetValid) {
			t.Errorf("at %v: %v", behind, err)
		}
		if _, err := ca.IssueHubClientCert(behind); !errors.Is(err, ErrCANotYetValid) {
			t.Errorf("hub certificate at %v: %v", behind, err)
		}
	}
	if _, err := ca.IssueNodeCert(1, "n", "203.0.113.7", to.Add(time.Second)); !errors.Is(err, ErrCAExpired) {
		t.Errorf("after the CA ended: %v", err)
	}

	// at the very start the certificate can't reach back past the CA
	is, err := ca.IssueNodeCert(1, "n", "203.0.113.7", from)
	if err != nil || !is.NotBefore.Equal(from) {
		t.Fatalf("at the CA's start: %+v, %v", is, err)
	}
	// near the end it can't outlast it
	late := to.Add(-10 * 24 * time.Hour)
	is, err = ca.IssueNodeCert(1, "n", "203.0.113.7", late)
	if err != nil || !is.NotAfter.Equal(to) || !is.NotBefore.Equal(late.Add(-time.Hour)) {
		t.Fatalf("ten days before the CA ends: %+v, %v", is, err)
	}
	if err := verify(parse(t, is.CertPEM), ca, x509.ExtKeyUsageServerAuth, late); err != nil {
		t.Fatal(err)
	}
	if !NeedsRenewal(is.NotAfter, late) {
		t.Fatal("a certificate cut short by the CA's end should be up for renewal")
	}
	if _, err := ca.IssueNodeCert(1, "n", "203.0.113.7", to); err != nil {
		t.Fatalf("on the last second: %v", err)
	}
}

func TestOnlyUsableNodeIDsAndHostsAreIssued(t *testing.T) {
	ca := newTestCA(t, t0)
	for _, id := range []uint{0, 4294967296, 1 << 40} {
		if _, err := ca.IssueNodeCert(id, "n", "203.0.113.7", t0); !errors.Is(err, ErrBadNodeID) {
			t.Errorf("id %d: %v", id, err)
		}
	}
	for _, host := range []string{
		"", "  ", "0.0.0.0", "::", "fe80::1%eth0", "exa mple.com", "ex_ample.com", "-a.com", "a-.com", "a..com", ".a.com",
		"a.com.", "1.2.3", "256.1.1.1", "bücher.de", "*.example.com", "https://x.com", "x.com:443", "a/b", "x\x00.com",
		strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 130) + "com", "203.0.113.7 ", " example.com",
	} {
		if _, err := ca.IssueNodeCert(1, "n", host, t0); !errors.Is(err, ErrBadHost) {
			t.Errorf("host %q: %v", host, err)
		}
	}
	for _, host := range []string{"localhost", "node1", "a.b.c", "xn--bcher-kva.de", "127.0.0.1", "::1", strings.Repeat("a", 63) + ".com", "a1b2.example-host.org"} {
		if _, err := ca.IssueNodeCert(1, "n", host, t0); err != nil {
			t.Errorf("host %q: %v", host, err)
		}
	}
}

func TestACAIsSafeForSeveralGoroutines(t *testing.T) {
	ca := newTestCA(t, t0)
	var wg sync.WaitGroup
	serials := make(chan string, 80)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				is, err := ca.IssueNodeCert(uint(g+1), "n", "203.0.113.7", t0)
				if err != nil {
					t.Error(err)
					return
				}
				serials <- is.Serial
			}
		}()
	}
	wg.Wait()
	close(serials)
	seen := map[string]bool{}
	for s := range serials {
		if seen[s] {
			t.Fatalf("serial %s came twice", s)
		}
		seen[s] = true
	}
	if len(seen) != 80 {
		t.Fatalf("%d serials", len(seen))
	}
}
