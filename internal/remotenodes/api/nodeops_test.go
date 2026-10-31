// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

const (
	secretPassword   = "MARKER-SSH-PASSWORD"
	secretKey        = "-----BEGIN OPENSSH PRIVATE KEY-----\nMARKER-SSH-PRIVATE-KEY\n-----END OPENSSH PRIVATE KEY-----\n"
	secretPassphrase = "MARKER-SSH-PASSPHRASE"
)

func aLogin() Login {
	return Login{SSHPort: 2222, SSHUser: "admin", Password: secretPassword, PrivateKey: secretKey, Passphrase: secretPassphrase, HostKey: "SHA256:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopq"}
}

// A login that is printed or logged by mistake shows none of its secrets, in
// any of the ways Go has of printing a value, or of the two log handlers.
func TestALoginIsNeverPrintedWithItsSecrets(t *testing.T) {
	login := aLogin()
	req := InstallRequest{Login: login, Name: "Frankfurt", Host: "203.0.113.9", EntryPort: 8443}
	rm := RemoveRequest{Force: false, Login: &login}
	values := map[string]any{
		"login": login, "login pointer": &login,
		"install": req, "install pointer": &req,
		"remove": rm, "remove pointer": &rm,
	}
	var text, js bytes.Buffer
	th := slog.New(slog.NewTextHandler(&text, nil))
	jh := slog.New(slog.NewJSONHandler(&js, nil))
	for name, v := range values {
		th.Info("a request", "req", v)
		jh.Info("a request", "req", v)
		for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
			out := fmt.Sprintf(verb, v)
			for _, secret := range []string{secretPassword, "MARKER-SSH-PRIVATE-KEY", secretPassphrase} {
				if strings.Contains(out, secret) {
					t.Errorf("%s printed with %s carries %q: %s", name, verb, secret, out)
				}
			}
			if !strings.Contains(out, "hidden") && !strings.Contains(out, "no login") {
				t.Errorf("%s printed with %s doesn't say it holds secrets: %s", name, verb, out)
			}
		}
	}
	for what, out := range map[string]string{"text log": text.String(), "json log": js.String()} {
		for _, secret := range []string{secretPassword, "MARKER-SSH-PRIVATE-KEY", secretPassphrase} {
			if strings.Contains(out, secret) {
				t.Errorf("the %s carries %q:\n%s", what, secret, out)
			}
		}
		if !strings.Contains(out, "Frankfurt") && what == "text log" {
			t.Errorf("the install is not named in the log: %s", out)
		}
	}
	// what is safe to say stays in
	if s := fmt.Sprint(req); !strings.Contains(s, "Frankfurt") || !strings.Contains(s, "203.0.113.9") || !strings.Contains(s, "2222") || !strings.Contains(s, "admin") {
		t.Errorf("an install request prints as %q", s)
	}
	if s := fmt.Sprint(Login{}); !strings.Contains(s, "password none") || strings.Contains(s, "hidden") {
		t.Errorf("an empty login prints as %q", s)
	}
}

// The page sends the install as one flat object, which is the contract's
// "InstallRequest extends Login".
func TestAnInstallRequestIsOneFlatObject(t *testing.T) {
	body := `{"sshPort":22,"sshUser":"root","password":"p","hostKey":"SHA256:x","name":"Frankfurt","host":"203.0.113.9","entryPort":8443}`
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var req InstallRequest
	if err := dec.Decode(&req); err != nil {
		t.Fatal(err)
	}
	if req.Name != "Frankfurt" || req.Host != "203.0.113.9" || req.EntryPort != 8443 || req.SSHPort != 22 || req.SSHUser != "root" || req.Password != "p" || req.HostKey != "SHA256:x" {
		t.Errorf("req = %#v", req.String())
	}
	var rm RemoveRequest
	dec = json.NewDecoder(strings.NewReader(`{"login":{"sshPort":22,"sshUser":"root","privateKey":"k","passphrase":"q","hostKey":"SHA256:x"}}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rm); err != nil || rm.Login == nil || rm.Login.PrivateKey != "k" || rm.Login.Passphrase != "q" || rm.Force {
		t.Errorf("remove = %v, %v", rm.String(), err)
	}
	// the settings tell a field that is left out from one that is empty
	var s SettingsRequest
	if err := json.Unmarshal([]byte(`{"sni":""}`), &s); err != nil || s.SNI == nil || *s.SNI != "" || s.EntryPort != nil || s.HandshakeTarget != nil {
		t.Errorf("settings = %+v, %v", s, err)
	}
}

// What the node endpoints answer with has no field named like a secret,
// whether or not a particular answer fills it.
func TestNoAnswerOfTheNodeEndpointsHasASecretField(t *testing.T) {
	names := map[string]string{}
	for _, v := range []any{Started{}, ProbeResult{}, HandshakeCheck{}} {
		jsonNames(reflect.TypeOf(v), reflect.TypeOf(v).Name(), names)
	}
	if len(names) != 1+2+6 {
		t.Fatalf("%d names found: %v", len(names), names)
	}
	for where, name := range names {
		if secretish(name) {
			t.Errorf("%s looks like a secret", where)
		}
	}
	want := map[string]string{"Started": "jobId", "ProbeResult": "fingerprint status", "HandshakeCheck": "ok tls13 x25519 handshakeBytes latencyMs error"}
	for typ, keys := range want {
		var v any
		switch typ {
		case "Started":
			v = Started{}
		case "ProbeResult":
			v = ProbeResult{}
		default:
			v = HandshakeCheck{}
		}
		raw, _ := json.Marshal(v)
		var got map[string]any
		_ = json.Unmarshal(raw, &got)
		wantKeys(t, typ, got, keys)
	}
}
