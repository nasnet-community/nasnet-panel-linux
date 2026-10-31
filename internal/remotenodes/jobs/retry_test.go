// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// An install job carries what it was asked for, so that the page can fill its
// form in again after a reload. Only an install has it, and every copy is the
// caller's own.
func TestAnInstallCarriesWhatItWasAskedFor(t *testing.T) {
	g := newRig(t)
	form := &InstallForm{Name: "Frankfurt", Host: "203.0.113.9", SSHPort: 2222, SSHUser: "admin", EntryPort: 8443}
	started, _, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 1, Steps: []string{"a"}, Install: form, Secrets: []string{"hunter2"}})
	form.Name = "changed by the caller"
	if started.Install == nil || started.Install.Name != "Frankfurt" || started.Install.SSHPort != 2222 {
		t.Fatalf("started with %+v", started.Install)
	}
	started.Install.Host = "changed by the reader"
	end(errors.New("it broke"))
	done := settle(t, g.e, started.ID)
	want := InstallForm{Name: "Frankfurt", Host: "203.0.113.9", SSHPort: 2222, SSHUser: "admin", EntryPort: 8443}
	if done.Install == nil || *done.Install != want {
		t.Fatalf("the finished job carries %+v", done.Install)
	}
	b, _ := json.Marshal(done)
	if !strings.Contains(string(b), `"install":{"name":"Frankfurt","host":"203.0.113.9","sshPort":2222,"sshUser":"admin","entryPort":8443}`) {
		t.Errorf("JSON: %s", b)
	}
	if strings.Contains(string(b), "hunter2") {
		t.Errorf("a secret is in the job: %s", b)
	}
	other := g.run(t, Spec{Kind: KindRemove}, func(context.Context, *Run) error { return nil })
	if other.Install != nil {
		t.Errorf("a removal carries %+v", other.Install)
	}
	if b, _ := json.Marshal(other); strings.Contains(string(b), `"install"`) {
		t.Errorf("a removal's JSON: %s", b)
	}
}

type codedErr struct{ code string }

func (e codedErr) Error() string { return "coded " + e.code }

// A failed job carries its error's code, which the page words; a good job, a
// panic and an error with no code carry none.
func TestAFailedJobCarriesItsErrorsCode(t *testing.T) {
	g := newRig(t, func(c *Config) {
		c.ErrorCode = func(err error) string {
			var ce codedErr
			if errors.As(err, &ce) {
				if ce.code == "panic" {
					panic("the code table broke")
				}
				return ce.code
			}
			return ""
		}
	})
	coded := g.run(t, Spec{Kind: KindInstall}, func(context.Context, *Run) error { return codedErr{"server_has_xray"} })
	if coded.Code != "server_has_xray" || coded.Error != "coded server_has_xray" {
		t.Errorf("coded: %+v", coded)
	}
	for name, fn := range map[string]func(context.Context, *Run) error{
		"ok":         func(context.Context, *Run) error { return nil },
		"no code":    func(context.Context, *Run) error { return errors.New("plain") },
		"a panic":    func(context.Context, *Run) error { panic(codedErr{"server_has_xray"}) },
		"table dies": func(context.Context, *Run) error { return codedErr{"panic"} },
	} {
		if j := g.run(t, Spec{Kind: KindInstall}, fn); j.Code != "" {
			t.Errorf("%s: code %q", name, j.Code)
		}
	}
}

// FailedStep names the first step that failed, for a job that goes on after
// a failure to say where it stopped.
func TestFailedStepIsTheFirstThatFailed(t *testing.T) {
	g := newRig(t)
	_, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a", "b", "c"}})
	defer end(nil)
	if s := r.FailedStep(); s != "" {
		t.Errorf("before any failure: %q", s)
	}
	r.Done("a")
	r.Fail("b", errors.New("no"))
	r.Fail("c", errors.New("no either"))
	if s := r.FailedStep(); s != "b" {
		t.Errorf("FailedStep = %q", s)
	}
}
