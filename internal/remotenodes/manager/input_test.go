// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func isInvalid(err error) bool {
	var inv *model.InvalidError
	return errors.As(err, &inv)
}

func TestTheSSHPortDefaultsTo22(t *testing.T) {
	for in, want := range map[int]int{0: 22, 22: 22, 2222: 2222, 65535: 65535, 1: 1} {
		if got, err := sshPortOf(in); err != nil || got != want {
			t.Errorf("sshPortOf(%d) = %d, %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []int{-1, 65536, 100000} {
		if _, err := sshPortOf(in); !isInvalid(err) {
			t.Errorf("sshPortOf(%d) = %v, want an invalid value", in, err)
		}
	}
}

func TestTheSSHUserDefaultsToRootAndIsPlain(t *testing.T) {
	for in, want := range map[string]string{"": "root", "  ": "root", "admin": "admin", " ubuntu ": "ubuntu", "deploy.me@corp": "deploy.me@corp"} {
		if got, err := cleanUser(in); err != nil || got != want {
			t.Errorf("cleanUser(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"two words", "tab\tname", "new\nline", "café", strings.Repeat("a", 65), "nul\x00"} {
		if _, err := cleanUser(in); !isInvalid(err) {
			t.Errorf("cleanUser(%q) = %v, want an invalid value", in, err)
		}
	}
}

func TestALoginNeedsTheKeyThePersonAgreedToAndASecret(t *testing.T) {
	good := api.Login{SSHPort: 2222, SSHUser: " admin ", Password: "p4ss", HostKey: " " + fingerprintA + " "}
	got, err := cleanLogin(good)
	if err != nil || got.port != 2222 || got.user != "admin" || got.hostKey != fingerprintA || got.creds.Password != "p4ss" {
		t.Fatalf("cleanLogin = %+v, %v", got, err)
	}
	// the defaults, and a key in place of a password
	got, err = cleanLogin(api.Login{PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----\n", Passphrase: "open", HostKey: fingerprintB})
	if err != nil || got.port != 22 || got.user != "root" || got.creds.Passphrase != "open" || got.creds.Password != "" {
		t.Fatalf("cleanLogin with a key = %+v, %v", got, err)
	}

	for name, l := range map[string]api.Login{
		"no host key":             {Password: "p"},
		"a host key of no shape":  {Password: "p", HostKey: "SHA256:short"},
		"an MD5 fingerprint":      {Password: "p", HostKey: "MD5:aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"},
		"a key with a tail":       {Password: "p", HostKey: fingerprintA + "x"},
		"nothing to sign in with": {HostKey: fingerprintA},
		"a blank private key":     {PrivateKey: " \n\t", HostKey: fingerprintA},
		"a bad port":              {Password: "p", HostKey: fingerprintA, SSHPort: 70000},
		"a bad user":              {Password: "p", HostKey: fingerprintA, SSHUser: "a b"},
		"a huge password":         {Password: strings.Repeat("x", maxPassword+1), HostKey: fingerprintA},
		"a huge passphrase":       {PrivateKey: "k", Passphrase: strings.Repeat("x", maxPassphrase+1), HostKey: fingerprintA},
		"a huge key":              {PrivateKey: strings.Repeat("x", maxPrivateKey+1), HostKey: fingerprintA},
	} {
		_, err := cleanLogin(l)
		if !isInvalid(err) {
			t.Errorf("%s: err = %v, want an invalid value", name, err)
			continue
		}
		// a message names what is wrong and quotes no secret
		for _, secret := range []string{l.Password, l.Passphrase, l.PrivateKey} {
			if len(secret) > 3 && strings.Contains(err.Error(), strings.TrimSpace(secret)) {
				t.Errorf("%s: the message quotes a secret: %q", name, err)
			}
		}
	}
}

func TestTheSecretsOfALoginAreWhatWasGiven(t *testing.T) {
	l, err := cleanLogin(api.Login{Password: "pw", PrivateKey: "key", Passphrase: "phrase", HostKey: fingerprintA})
	must(t, err)
	if got := l.secrets("extra", ""); !slices.Equal(got, []string{"pw", "phrase", "key", "extra"}) {
		t.Errorf("secrets = %q", got)
	}
	l, err = cleanLogin(api.Login{Password: "pw", HostKey: fingerprintA})
	must(t, err)
	if got := l.secrets(); !slices.Equal(got, []string{"pw"}) {
		t.Errorf("secrets = %q", got)
	}
}

func TestAHandshakeTargetIsHostAndPort(t *testing.T) {
	for in, want := range map[string]string{
		"www.example.com:443": "www.example.com:443", " WWW.Example.COM ": "www.example.com:443", "example.com:8443": "example.com:8443",
		"203.0.113.9": "203.0.113.9:443", "203.0.113.9:8443": "203.0.113.9:8443", "[2001:db8::1]:443": "[2001:db8::1]:443", "2001:db8::1": "[2001:db8::1]:443",
	} {
		if got, err := cleanTarget(in); err != nil || got != want {
			t.Errorf("cleanTarget(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "  ", "https://www.example.com", "www.example.com/path", "a b:443", "example.com:0", "example.com:99999", "example.com:port", "-bad-.example:443", "1.2.3:443"} {
		if got, err := cleanTarget(in); !isInvalid(err) {
			t.Errorf("cleanTarget(%q) = %q, %v, want an invalid value", in, got, err)
		}
	}
	if hostOf("www.example.com:443") != "www.example.com" || hostOf("[2001:db8::1]:443") != "2001:db8::1" || hostOf("nope") != "" {
		t.Error("hostOf")
	}
}

func TestAServerNameIsAHostNameNotAnAddress(t *testing.T) {
	if got, err := cleanSNI(" WWW.Example.com "); err != nil || got != "www.example.com" {
		t.Errorf("cleanSNI = %q, %v", got, err)
	}
	for _, in := range []string{"", "203.0.113.9", "2001:db8::1", "a b.example", "x_y.example"} {
		if got, err := cleanSNI(in); !isInvalid(err) {
			t.Errorf("cleanSNI(%q) = %q, %v, want an invalid value", in, got, err)
		}
	}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestAnIdIsARandomLowerCaseUUID(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		id, err := newUUID()
		if err != nil || !uuidRe.MatchString(id) || seen[id] {
			t.Fatalf("newUUID = %q, %v (seen before: %v)", id, err, seen[id])
		}
		seen[id] = true
		// the state takes it as it is
		s := model.NewState()
		if _, err := s.AddNode(model.Node{Name: "n", Host: "203.0.113.1", SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeInstalling, CreatedAt: epoch, BridgeUUID: id}); err != nil {
			t.Fatalf("the model refuses %q: %v", id, err)
		}
	}
}
