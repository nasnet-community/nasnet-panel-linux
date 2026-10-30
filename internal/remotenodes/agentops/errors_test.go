// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"errors"
	"strings"
	"syscall"
	"testing"
	"unicode"

	"github.com/nasnet-community/mtproto-remote-plugin/installer"
)

func TestTypedErrorsMatchTheirSentinels(t *testing.T) {
	hk := &installer.HostKeyError{Got: "SHA256:new", Want: "SHA256:old"}
	cases := []struct {
		err  error
		is   error
		text []string // what a person needs to read
	}{
		{&HostKeyChangedError{Host: "203.0.113.7", Pinned: "SHA256:old", Got: "SHA256:new", err: hk}, ErrHostKeyChanged,
			[]string{"SHA256:new", "SHA256:old", "203.0.113.7"}},
		{&PortInUseError{Port: 443, Role: "entry", Process: "nginx"}, ErrPortInUse, []string{"TCP port 443", "nginx"}},
		{&PortInUseError{Port: 9090, Role: "agent"}, ErrPortInUse, []string{"TCP port 9090", "agent"}},
		{&UnreachableError{What: "agent", Host: "h", Port: 9090}, ErrUnreachable, []string{"TCP port 9090", "firewall"}},
		{&UnreachableError{What: "ssh", Host: "203.0.113.7", Port: 22}, ErrUnreachable, []string{"203.0.113.7", "22"}},
		{&UnreachableError{What: "entry", Host: "h", Port: 443}, ErrUnreachable, []string{"TCP port 443", "firewall"}},
		{&UnreachableError{What: "restart", Host: "h", Port: 9090}, ErrUnreachable, []string{"TCP port 9090", "update"}},
		{&VersionMismatchError{What: "Xray", Want: "26.10.21", Got: "26.10.10"}, ErrVersionMismatch, []string{"Xray", "26.10.21", "26.10.10"}},
		{&NotManagedError{AgentVersion: "v1.2.0"}, ErrNotManaged, []string{"managed profile", "v1.2.0"}},
		{newErr(ErrLoginFailed, nil, "The server didn't accept the login."), ErrLoginFailed, nil},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.is) {
			t.Errorf("%T is not %v", c.err, c.is)
		}
		msg := c.err.Error()
		for _, want := range c.text {
			if !strings.Contains(msg, want) {
				t.Errorf("%q should mention %q", msg, want)
			}
		}
		// the page shows these as they are: a sentence, with no package name in it
		r := []rune(msg)
		if !unicode.IsUpper(r[0]) || !strings.HasSuffix(msg, ".") || strings.Contains(msg, "agentops:") || strings.Contains(msg, "installer:") {
			t.Errorf("not a plain sentence: %q", msg)
		}
	}
}

func TestAChangedHostKeyKeepsTheInstallersErrorAndTheFingerprints(t *testing.T) {
	hk := &installer.HostKeyError{Got: "SHA256:new", Want: "SHA256:old"}
	err := error(&HostKeyChangedError{Host: "h", Pinned: hk.Want, Got: hk.Got, err: hk})
	var ours *HostKeyChangedError
	var theirs *installer.HostKeyError
	if !errors.As(err, &ours) || ours.Got != "SHA256:new" || ours.Pinned != "SHA256:old" {
		t.Errorf("errors.As(HostKeyChangedError): %+v", ours)
	}
	if !errors.As(err, &theirs) || theirs != hk {
		t.Errorf("errors.As(installer.HostKeyError): %v", theirs)
	}
	// with nothing under it, Unwrap says nil instead of a nil pointer in an interface
	if u := (&HostKeyChangedError{}).Unwrap(); u != nil {
		t.Errorf("Unwrap = %#v", u)
	}
}

func TestAnUnreachableNodeKeepsItsCause(t *testing.T) {
	err := error(&UnreachableError{What: "ssh", Host: "h", Port: 22, Err: syscall.EADDRNOTAVAIL})
	if !errors.Is(err, syscall.EADDRNOTAVAIL) {
		t.Error("the cause is lost")
	}
	if !strings.Contains(err.Error(), syscall.EADDRNOTAVAIL.Error()) {
		t.Errorf("the text hides the cause: %q", err)
	}
}

func TestWrappedErrorsStillMatch(t *testing.T) {
	cause := errors.New("exit status 1")
	err := newErr(ErrUploadCorrupt, cause, "The agent arrived damaged.")
	if !errors.Is(err, ErrUploadCorrupt) || !errors.Is(err, cause) {
		t.Errorf("Is: %v", err)
	}
	if errors.Is(err, ErrPortInUse) {
		t.Error("it matches a sentinel it isn't")
	}
}
