// SPDX-License-Identifier: AGPL-3.0-or-later

package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A made-up OpenSSH key, long enough to have lines of its own.
const fakeKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBmYWtla2V5ZmFrZWtleWZha2VrZXlmYWtla2V5ZmFrZWtleWZha2U=
-----END OPENSSH PRIVATE KEY-----
`

func TestSecretsAreHiddenInEveryTextAJobReports(t *testing.T) {
	const pw, pass = "hunter2-correct-horse", "battery-staple-9"
	g := newRig(t)
	job, r, end := g.begin(t, Spec{
		Kind: KindInstall, NodeID: 1, Target: "Node 1", Steps: []string{"upload", "agent", "xray", "config"},
		Secrets: []string{pw, fakeKey, pass, ""},
	})
	r.Begin("upload")
	r.Detail("upload", "logged in with "+pw)
	r.Set("agent", StepRunning, "key was "+fakeKey)
	r.Fail("xray", fmt.Errorf("auth: tried %s and %s: %w", pw, pass, errors.New("denied")))
	r.Set("config", StepFailed, "pass phrase "+pass)

	mid, _ := g.e.Get(job.ID)
	if want := []Step{
		{"upload", StepRunning, "logged in with …"},
		{"agent", StepRunning, "key was …"},
		{"xray", StepFailed, "auth: tried … and …: denied"},
		{"config", StepFailed, "pass phrase …"},
	}; !slices.Equal(mid.Steps, want) {
		t.Fatalf("steps = %v\nwant    %v", mid.Steps, want)
	}

	// the step still running is closed with the error's text, and so is the job
	end(fmt.Errorf("install failed: %s did not work: %w", pw, errors.New(strings.TrimSpace(fakeKey))))
	done := settle(t, g.e, job.ID)
	if done.Error != "install failed: … did not work: …" {
		t.Fatalf("error = %q", done.Error)
	}
	body, err := json.Marshal(done)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{pw, pass, "b3BlbnNzaC1rZXktdjEA", "BEGIN OPENSSH PRIVATE KEY", "ZmFrZWtleWZha2V"} {
		if strings.Contains(string(body), secret) || strings.Contains(g.log.String(), secret) {
			t.Errorf("%q is in the job or the log:\n%s\n%s", secret, body, g.log.String())
		}
	}
}

func TestAKeyIsHiddenWhateverPartOfItIsQuoted(t *testing.T) {
	h := newHider([]string{fakeKey})
	lines := strings.Split(strings.TrimSpace(fakeKey), "\n")
	for name, text := range map[string]string{
		"all of it":             "bad key " + fakeKey + "!",
		"without its newline":   "bad key " + strings.TrimSpace(fakeKey) + ".",
		"with windows newlines": "bad key " + strings.ReplaceAll(fakeKey, "\n", "\r\n"),
		"one body line":         "line: " + lines[1],
		"another":               "line: " + lines[2],
		"the first line":        "header " + lines[0],
		"indented":              "key:\n\t" + lines[1] + "\n",
	} {
		got := h.hide(text)
		for _, line := range lines {
			if strings.Contains(got, line) {
				t.Errorf("%s: %q is still in %q", name, line, got)
			}
		}
	}
}

func TestShortLinesOfAKeyAreNotHiddenOnTheirOwn(t *testing.T) {
	h := newHider([]string{"ab\nthe real secret value\ncd"})
	if got := h.hide("ab and cd stay, the real secret value goes"); got != "ab and cd stay, … goes" {
		t.Fatalf("hide = %q", got)
	}
}

func TestALongerSecretIsHiddenBeforeOneInsideIt(t *testing.T) {
	for _, secrets := range [][]string{{"abc", "abcdef"}, {"abcdef", "abc"}} {
		h := newHider(secrets)
		if got := h.hide("x abcdef y abc z"); got != "x … y … z" {
			t.Errorf("secrets %v: hide = %q, want no piece of the longer one left", secrets, got)
		}
	}
}

func TestNothingIsHiddenWhenThereIsNothingToHide(t *testing.T) {
	for _, secrets := range [][]string{nil, {}, {""}, {"", "  ", "\n\t "}} {
		if h := newHider(secrets); h != nil {
			t.Errorf("secrets %q made a hider", secrets)
		}
	}
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}, Secrets: []string{"", " "}})
	r.Detail("a", "a plain  text with spaces")
	if got := g.stepsOf(t, job.ID)[0].Detail; got != "a plain  text with spaces" {
		t.Errorf("detail = %q", got)
	}
	end(nil)
}

func TestEveryOccurrenceIsHidden(t *testing.T) {
	h := newHider([]string{"s3cret"})
	if got := h.hide("s3cret and s3cret, and S3cret is another word, s3cret"); got != "… and …, and S3cret is another word, …" {
		t.Fatalf("hide = %q", got)
	}
}

// The secret goes before the text is cut. Cut first, and a secret that
// straddles the end would leave its first half behind.
func TestASecretAtTheEdgeOfTheCutDoesNotLeakItsStart(t *testing.T) {
	const secret = "S3CR3T-PASSWORD-12345"
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}, Secrets: []string{secret}})
	r.Detail("a", strings.Repeat("x", MaxDetail-5)+secret)
	got := g.stepsOf(t, job.ID)[0].Detail
	if strings.Contains(got, "S3CR") || !strings.HasSuffix(got, "…") {
		t.Fatalf("detail = %q", got)
	}
	end(errors.New(strings.Repeat("y", MaxError-5) + secret))
	if done := settle(t, g.e, job.ID); strings.Contains(done.Error, "S3CR") {
		t.Fatalf("error = %q", done.Error)
	}
}

func TestSecretsAreHiddenInAPanicToo(t *testing.T) {
	const secret = "panic-hunter2-secret"
	g := newRig(t)
	done := g.run(t, Spec{Kind: KindInstall, Steps: []string{"a"}, Secrets: []string{secret}}, func(ctx context.Context, r *Run) error {
		r.Begin("a")
		panic(fmt.Sprintf("login failed with %s", secret))
	})
	if body, _ := json.Marshal(done); done.Error != "internal error" || strings.Contains(string(body), secret) {
		t.Fatalf("job = %s", body)
	}
	log := g.log.String()
	if strings.Contains(log, secret) {
		t.Fatalf("the secret is in the log:\n%s", log)
	}
	if !strings.Contains(log, "login failed with …") || !strings.Contains(log, "goroutine") {
		t.Fatalf("the log should still have the panic, hidden, and its stack:\n%s", log)
	}
}

func TestTheEngineForgetsAJobsSecretsWhenItEnds(t *testing.T) {
	g := newRig(t)
	job, _, end := g.begin(t, Spec{Kind: KindInstall, Secrets: []string{"s3cret-value"}})
	g.e.mu.Lock()
	held := g.e.byID[job.ID].hide != nil
	g.e.mu.Unlock()
	if !held {
		t.Fatal("a running job has no secrets to hide")
	}
	end(nil)
	settle(t, g.e, job.ID)
	g.e.mu.Lock()
	still := g.e.byID[job.ID].hide != nil
	g.e.mu.Unlock()
	if still {
		t.Fatal("a finished job still holds its secrets")
	}
}

// The caller wipes its own copy once the job is on its way, as a login does.
func TestTheEngineKeepsItsOwnCopyOfTheSecrets(t *testing.T) {
	secrets := []string{"s3cret-one", "s3cret-two"}
	g := newRig(t)
	job, r, end := g.begin(t, Spec{Kind: KindInstall, Steps: []string{"a"}, Secrets: secrets})
	secrets[0], secrets[1] = "", ""
	r.Detail("a", "s3cret-one and s3cret-two")
	if got := g.stepsOf(t, job.ID)[0].Detail; got != "… and …" {
		t.Fatalf("detail = %q", got)
	}
	end(nil)
}

func TestSecretsBelongToTheirOwnJob(t *testing.T) {
	g := newRig(t)
	a, ra, endA := g.begin(t, Spec{Kind: KindInstall, NodeID: 1, Steps: []string{"x"}, Secrets: []string{"only-a-knows"}})
	b, rb, endB := g.begin(t, Spec{Kind: KindInstall, NodeID: 2, Steps: []string{"x"}})
	ra.Detail("x", "only-a-knows")
	rb.Detail("x", "only-a-knows")
	if got := g.stepsOf(t, a.ID)[0].Detail; got != "…" {
		t.Errorf("job a: %q", got)
	}
	if got := g.stepsOf(t, b.ID)[0].Detail; got != "only-a-knows" {
		t.Errorf("job b has no secrets, so its text stands: %q", got)
	}
	endA(nil)
	endB(nil)
}
