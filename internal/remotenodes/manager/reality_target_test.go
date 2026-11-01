// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func targetReq(name, host, target string) api.InstallRequest {
	r := installReq(name, host)
	r.HandshakeTarget = target
	return r
}

// noTarget is what the node says of one target it was told to use and can't.
func noTarget(target, why string) error {
	return &agentops.NoTargetError{Results: []agentops.TargetResult{{Target: target, Check: agentops.HandshakeCheck{Error: why}}}}
}

// A Reality target in the request is read the way the settings call reads one
// and goes to the install as it is, so that the node checks that one and no
// other. The job's form carries it for another try, and a request without one
// leaves the choice to the node and has nothing of the kind in its form.
func TestAnInstallUsesTheRealityTargetItIsGiven(t *testing.T) {
	for _, c := range []struct{ asked, want string }{
		{"www.debian.org", "www.debian.org:443"},
		{"  WWW.Debian.org:8443 ", "www.debian.org:8443"},
	} {
		t.Run(c.asked, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()

				j := r.ok(r.add(targetReq("Frankfurt", "203.0.113.9", c.asked)))
				if got := r.ops.provisions[0].Target; got != c.want {
					t.Errorf("the install was told to use %q, want %q", got, c.want)
				}
				if n := r.nodes()[0]; n.RealityTarget != c.want || n.RealitySNI != hostOf(c.want) {
					t.Errorf("the node kept %q and %q", n.RealityTarget, n.RealitySNI)
				}
				if j.Install == nil || j.Install.HandshakeTarget != c.want {
					t.Errorf("the form for another try is %+v", j.Install)
				}
				b, err := json.Marshal(j.Install)
				must(t, err)
				if !strings.Contains(string(b), `"handshakeTarget":"`+c.want+`"`) {
					t.Errorf("the form's JSON is %s", b)
				}
			})
		})
	}

	t.Run("none asked for", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			j := r.ok(r.add(installReq("Frankfurt", "203.0.113.9")))
			if got := r.ops.provisions[0].Target; got != "" {
				t.Errorf("the node was told to use %q", got)
			}
			// the node chose
			if n := r.nodes()[0]; n.RealityTarget != "www.google.com:443" {
				t.Errorf("the node kept %q", n.RealityTarget)
			}
			b, err := json.Marshal(j.Install)
			must(t, err)
			if strings.Contains(string(b), "handshakeTarget") {
				t.Errorf("the form for another try is %s", b)
			}
			// a blank one is none
			r.ok(r.add(targetReq("Berlin", "203.0.113.10", "  ")))
			if got := r.ops.provisions[1].Target; got != "" {
				t.Errorf("a blank target was passed on as %q", got)
			}
		})
	})
}

// A target that can't be one is refused with the request, a bad request before
// anything is saved or tried and no job started: the same words the settings
// call has for the same mistakes.
func TestARealityTargetThatCantBeOneIsRefusedWhole(t *testing.T) {
	for name, c := range map[string]struct{ target, says string }{
		"a URL":                    {"https://www.example.com", "must look like www.example.com:443"},
		"a path":                   {"www.example.com/index.html", "must look like"},
		"a space inside":           {"www example.com", "must look like"},
		"a port of zero":           {"www.example.com:0", "port"},
		"a port too big":           {"www.example.com:70000", "port"},
		"a port that is no number": {"www.example.com:https", "valid port"},
		"a host that isn't one":    {"not_a_host!", ""},
		"an address":               {"203.0.113.5:443", "host name"},
		"an address without port":  {"203.0.113.5", "host name"},
		"an IPv6 address":          {"[2001:db8::1]:443", "host name"},
	} {
		t.Run(name, func(t *testing.T) {
			r := newOpsRig(t)
			_, err := r.m.AddNode(context.Background(), targetReq("Frankfurt", "203.0.113.9", c.target))
			if !isInvalid(err) {
				t.Fatalf("err = %v, want an invalid value", err)
			}
			if c.says != "" && !strings.Contains(err.Error(), c.says) {
				t.Errorf("the message %q doesn't say %q", err, c.says)
			}
			if len(r.nodes()) != 0 || len(r.ops.allCalls()) != 0 {
				t.Errorf("a refused request left %d nodes and %v calls", len(r.nodes()), r.ops.allCalls())
			}
			if got := r.jobs.List(); len(got) != 0 {
				t.Errorf("a job was started: %+v", got)
			}
		})
	}
}

// The node checks the target at "Choose a Reality target", after the agent and
// Xray are on the server, and a target it can't use fails the install there.
// The cleanup runs as it does for any failure and nothing is kept; the job has
// a code of its own with the target and the node's reason in its facts, and its
// form carries the target for another try. Another target then goes through.
func TestATargetTheNodeCantUseFailsTheInstallAfterCleaningUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		const why = "It doesn't use TLS 1.3."
		r.ops.failAt(agentops.StepChooseTarget, noTarget("www.example.com:443", why))

		j := r.failed(r.add(targetReq("Frankfurt", "203.0.113.9", "www.example.com")))
		if j.Code != api.CodeRealityTargetFailed {
			t.Errorf("code %q", j.Code)
		}
		if want := &(jobs.ErrorInfo{Target: "www.example.com:443", Reason: why}); !reflect.DeepEqual(j.ErrorInfo, want) {
			t.Errorf("facts %+v, want %+v", j.ErrorInfo, want)
		}
		wantText := "The site www.example.com:443 doesn't work as a Reality target from this server: it doesn't use TLS 1.3. The server was cleaned up and nothing was kept."
		if j.Error != wantText {
			t.Errorf("the job says %q, want %q", j.Error, wantText)
		}
		// it got as far as the node's own check, and everything it had put there is taken off
		if got, want := r.ops.allCalls(), []string{"InstallAgent 1", "Connect 1", "Provision 1", "UndoInstall 1"}; !slices.Equal(got, want) {
			t.Errorf("calls %v, want %v", got, want)
		}
		states := stepStates(j)
		if states[agentops.StepChooseTarget] != jobs.StepFailed || states[agentops.StepCleanServer] != jobs.StepOK || states[StepDiscardNode] != jobs.StepOK {
			t.Errorf("steps %+v", j.Steps)
		}
		if i := slices.IndexFunc(j.Steps, func(s jobs.Step) bool { return s.Key == "choose_target" }); i < 0 || j.Steps[i].Name != "Choose a Reality target" {
			t.Errorf("the failed step: %+v", j.Steps)
		}
		if len(r.nodes()) != 0 {
			t.Fatalf("a node was kept: %+v", r.nodes())
		}
		// the form says what was asked, for the page's fix to start from
		if j.Install == nil || j.Install.HandshakeTarget != "www.example.com:443" {
			t.Errorf("the form is %+v", j.Install)
		}

		// another target, on the same server and name, which the cleanup left free
		r.ops.failAt(agentops.StepChooseTarget, nil)
		r.ok(r.add(targetReq("Frankfurt", "203.0.113.9", "www.debian.org")))
		if n := r.nodes(); len(n) != 1 || n[0].State != model.NodeRegistered || n[0].RealityTarget != "www.debian.org:443" {
			t.Errorf("the next try: %+v", n)
		}
	})
}

// When the node chose among sites and none worked there is no one target to
// name: the facts say how many were tried, and the code is the same.
func TestNoSiteThatWorksIsTheSameCodeWithTheCount(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		none := &agentops.NoTargetError{Results: []agentops.TargetResult{{Target: "a.example:443"}, {Target: "b.example:443"}, {Target: "c.example:443"}}}
		r.ops.failAt(agentops.StepChooseTarget, none)

		j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
		if j.Code != api.CodeRealityTargetFailed {
			t.Errorf("code %q", j.Code)
		}
		if want := &(jobs.ErrorInfo{Tried: 3}); !reflect.DeepEqual(j.ErrorInfo, want) {
			t.Errorf("facts %+v, want %+v", j.ErrorInfo, want)
		}
		if !strings.HasPrefix(j.Error, "None of the 3 sites tried works as a Reality target from this server.") {
			t.Errorf("the job says %q", j.Error)
		}
		if j.Install == nil || j.Install.HandshakeTarget != "" {
			t.Errorf("the form is %+v", j.Install)
		}
	})
}

// An answer with that error has the code too, and the job engine is given the
// same code and facts for it, whatever wraps it. The facts are what the typed
// error holds, and a target is never taken from its words.
func TestTheRealityTargetErrorHasItsCodeAndFacts(t *testing.T) {
	one := noTarget("www.example.com:443", "It asks for the key exchange to start over, which Xray can't follow.")
	wrapped := &failedInstall{cause: one, outcome: cleanedUp}
	for name, err := range map[string]error{"plain": one, "wrapped": wrapped} {
		var e *api.Error
		if !errors.As(opFailure(err), &e) || e.Code != api.CodeRealityTargetFailed || !strings.Contains(e.Msg, "Reality target") {
			t.Errorf("%s: opFailure = %v", name, opFailure(err))
		}
		if got := JobErrorCode(err); got != api.CodeRealityTargetFailed {
			t.Errorf("%s: code %q", name, got)
		}
		want := &jobs.ErrorInfo{Target: "www.example.com:443", Reason: "It asks for the key exchange to start over, which Xray can't follow."}
		if got := JobErrorInfo(err); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: facts %+v, want %+v", name, got, want)
		}
	}
}
