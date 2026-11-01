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

// factErr is an error with facts a page can use.
type factErr struct {
	info ErrorInfo
	boom bool
}

func (e factErr) Error() string { return "has facts" }

// A failed job carries the facts of its error beside its code, in the shape the
// contract has, as a copy of its own and with the secrets taken out; a good job,
// an error with no facts, a panic and a table that dies carry none.
func TestAFailedJobCarriesTheFactsOfItsError(t *testing.T) {
	g := newRig(t, func(c *Config) {
		c.ErrorInfo = func(err error) *ErrorInfo {
			var fe factErr
			if errors.As(err, &fe) {
				if fe.boom {
					panic("the table broke")
				}
				i := fe.info
				return &i
			}
			return nil
		}
	})
	// the shape of each, as the page reads it
	for name, c := range map[string]struct {
		info ErrorInfo
		json string
	}{
		"a port":       {ErrorInfo{Port: 443, Role: "entry", Process: "nginx"}, `"errorInfo":{"port":443,"role":"entry","process":"nginx"}`},
		"a host key":   {ErrorInfo{PinnedKey: "SHA256:aaa", ShownKey: "SHA256:bbb"}, `"errorInfo":{"pinnedKey":"SHA256:aaa","shownKey":"SHA256:bbb"}`},
		"their xray":   {ErrorInfo{Found: []string{"binary", "unit"}, XrayVersion: "26.9.1", SameBuild: true}, `"errorInfo":{"found":["binary","unit"],"xrayVersion":"26.9.1","sameBuild":true}`},
		"a node":       {ErrorInfo{NodeID: 3, NodeName: "Frankfurt"}, `"errorInfo":{"nodeId":3,"nodeName":"Frankfurt"}`},
		"a took over":  {ErrorInfo{TookOver: true}, `"errorInfo":{"tookOver":true}`},
		"a target":     {ErrorInfo{Target: "www.example.com:443", Reason: "It doesn't use TLS 1.3."}, `"errorInfo":{"target":"www.example.com:443","reason":"It doesn't use TLS 1.3."}`},
		"no target":    {ErrorInfo{Tried: 8}, `"errorInfo":{"tried":8}`},
		"not the same": {ErrorInfo{Found: []string{"process"}, XrayVersion: "26.9.1"}, `"errorInfo":{"found":["process"],"xrayVersion":"26.9.1"}`},
	} {
		j := g.run(t, Spec{Kind: KindInstall}, func(context.Context, *Run) error { return factErr{info: c.info} })
		if j.ErrorInfo == nil || !strings.Contains(mustJSON(t, j), c.json) {
			t.Errorf("%s: %s", name, mustJSON(t, j))
		}
	}
	for name, fn := range map[string]func(context.Context, *Run) error{
		"ok":         func(context.Context, *Run) error { return nil },
		"no facts":   func(context.Context, *Run) error { return errors.New("plain") },
		"a panic":    func(context.Context, *Run) error { panic(factErr{info: ErrorInfo{Port: 1}}) },
		"table dies": func(context.Context, *Run) error { return factErr{boom: true} },
		"empty":      func(context.Context, *Run) error { return factErr{} },
	} {
		j := g.run(t, Spec{Kind: KindInstall}, fn)
		if j.ErrorInfo != nil || strings.Contains(mustJSON(t, j), "errorInfo") {
			t.Errorf("%s: %s", name, mustJSON(t, j))
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// What goes into the facts is held to the job's own rules: a secret that a
// process name or a key repeats is hidden, long text is cut, and a reader
// can't change the job through the copy it gets.
func TestTheFactsOfAnErrorAreHiddenCutAndCopied(t *testing.T) {
	g := newRig(t, func(c *Config) {
		c.ErrorInfo = func(err error) *ErrorInfo {
			return &ErrorInfo{
				Process: "tool-hunter2-run", NodeName: strings.Repeat("n", 200), PinnedKey: strings.Repeat("k", 300), Found: []string{"binary", "unit", "process", "a", "b", "c", "d", "e", "f", "g"},
				Target: "hunter2.example.com:443", Reason: strings.Repeat("r", 400),
			}
		}
	})
	started, _, end := g.begin(t, Spec{Kind: KindInstall, NodeID: 1, Secrets: []string{"hunter2"}})
	end(errors.New("it broke"))
	j := settle(t, g.e, started.ID)
	if strings.Contains(mustJSON(t, j), "hunter2") || j.ErrorInfo.Process != "tool-…-run" {
		t.Errorf("a secret is in the facts: %+v", j.ErrorInfo)
	}
	if n := len([]rune(j.ErrorInfo.NodeName)); n != maxName {
		t.Errorf("a node name of %d characters", n)
	}
	if n := len([]rune(j.ErrorInfo.PinnedKey)); n != maxFact || !strings.HasSuffix(j.ErrorInfo.PinnedKey, "…") {
		t.Errorf("a key of %d characters", n)
	}
	// a target is a name like the rest, and a reason is a sentence: longer, and cut all the same
	if strings.Contains(j.ErrorInfo.Target, "hunter2") {
		t.Errorf("a secret is in the target: %q", j.ErrorInfo.Target)
	}
	if n := len([]rune(j.ErrorInfo.Reason)); n != maxReason || !strings.HasSuffix(j.ErrorInfo.Reason, "…") {
		t.Errorf("a reason of %d characters", n)
	}
	if len(j.ErrorInfo.Found) != 8 {
		t.Errorf("found %v", j.ErrorInfo.Found)
	}
	j.ErrorInfo.Found[0] = "changed by the reader"
	j.ErrorInfo.Process = "changed too"
	again, _ := g.e.Get(started.ID)
	if again.ErrorInfo.Found[0] != "binary" || again.ErrorInfo.Process != "tool-…-run" {
		t.Errorf("the reader changed the job: %+v", again.ErrorInfo)
	}
}
