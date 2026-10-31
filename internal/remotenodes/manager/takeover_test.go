// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
)

func takeoverReq(name, host string) api.InstallRequest {
	r := installReq(name, host)
	r.TakeOverXray = true
	return r
}

// An install that was asked to take over the server's Xray has the step in its
// plan and tells the operations so, and the Xray it kept is not sent again; one
// that wasn't asked has neither, and the form its job carries for another try
// never holds the question.
func TestATakeoverInstallHasItsStepAndTellsTheOperations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newOpsRig(t)
		r.start()
		defer r.stop()
		r.ops.install.TookOver, r.ops.install.XrayKept = true, true

		j := r.ok(r.add(takeoverReq("Frankfurt", "203.0.113.9")))
		with := agentops.InstallOptions{TakeOverXray: true}
		if got, want := stepNames(j), installSteps(with); !slices.Equal(got, want) {
			t.Fatalf("steps %q, want %q", got, want)
		}
		i := slices.Index(stepNames(j), agentops.StepTakeOverXray)
		if i < 0 || j.Steps[i].Key != "take_over_xray" || j.Steps[i].State != jobs.StepOK || j.Steps[i-1].Name != agentops.StepCertificate {
			t.Errorf("the takeover step: %+v", j.Steps)
		}
		if got := r.ops.installs[0].opts; !got.TakeOverXray {
			t.Errorf("the install was given %+v", got)
		}
		// the Xray the install kept is the one provisioning checks, and does not send
		if !r.ops.provisions[0].XrayKept {
			t.Errorf("provisioning was given %+v", r.ops.provisions[0])
		}
		// and the question is asked again each time: the form for another try doesn't hold it
		b, err := json.Marshal(j.Install)
		must(t, err)
		if string(b) != `{"name":"Frankfurt","host":"203.0.113.9","sshPort":22,"sshUser":"root","entryPort":8443}` {
			t.Errorf("the form for another try is %s", b)
		}

		// without the question: no step, and nothing asked of the operations
		r2 := newOpsRig(t)
		r2.start()
		defer r2.stop()
		j2 := r2.ok(r2.add(installReq("Berlin", "203.0.113.10")))
		if slices.Contains(stepNames(j2), agentops.StepTakeOverXray) || r2.ops.installs[0].opts.TakeOverXray || r2.ops.provisions[0].XrayKept {
			t.Errorf("a plain install: steps %q, options %+v, provision %+v", stepNames(j2), r2.ops.installs[0].opts, r2.ops.provisions[0])
		}
	})
}

// A refusal that the person can do something about comes with what the page
// needs to say what, in fields of its own and not in the sentence, and with the
// code the page words. The server was not touched.
func TestARefusedInstallCarriesItsCodeAndItsFacts(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		code string
		info *jobs.ErrorInfo
		say  string // in the job's error
	}{
		{"their Xray, another build", &agentops.ServerHasXrayError{Binary: true, Unit: true, Version: "26.9.1"}, api.CodeServerHasXray,
			&jobs.ErrorInfo{Found: []string{"binary", "unit"}, XrayVersion: "26.9.1"}, "take it over"},
		{"their Xray, the pinned build", &agentops.ServerHasXrayError{Binary: true, Process: true, Version: "26.10.21", SameBuild: true}, api.CodeServerHasXray,
			&jobs.ErrorInfo{Found: []string{"binary", "process"}, XrayVersion: "26.10.21", SameBuild: true}, "the same build as this plugin's"},
		{"their Xray, no binary of ours", &agentops.ServerHasXrayError{Unit: true}, api.CodeServerHasXray,
			&jobs.ErrorInfo{Found: []string{"unit"}}, "isn't at /usr/local/bin/xray"},
		{"another node of this hub", &agentops.ServerIsNodeError{NodeID: 3, Name: "Frankfurt"}, api.CodeServerIsNode,
			&jobs.ErrorInfo{NodeID: 3, NodeName: "Frankfurt"}, `the node "Frankfurt"`},
		{"a port in use", &agentops.PortInUseError{Port: 443, Role: "entry", Process: "nginx"}, api.CodePortInUse,
			&jobs.ErrorInfo{Port: 443, Role: "entry", Process: "nginx"}, "by nginx"},
		{"the agent's port, held by no one the server names", &agentops.PortInUseError{Port: 9090, Role: "agent"}, api.CodePortInUse,
			&jobs.ErrorInfo{Port: 9090, Role: "agent"}, "TCP port 9090"},
		{"a host key that changed", &agentops.HostKeyChangedError{Host: "203.0.113.9", Pinned: fingerprintA, Got: fingerprintB}, api.CodeHostKeyChanged,
			&jobs.ErrorInfo{PinnedKey: fingerprintA, ShownKey: fingerprintB}, "has changed"},
		{"a login the server refused", fmt.Errorf("%w: wrong password", agentops.ErrLoginFailed), api.CodeLoginRefused, nil, "wrong password"},
		{"a login that can't act as root", fmt.Errorf("%w: no sudo", agentops.ErrNeedsRoot), api.CodeNeedsRoot, nil, "no sudo"},
		{"an Xray that still runs", fmt.Errorf("%w: in a container", agentops.ErrXrayNotStopped), api.CodeXrayNotStopped, nil, "in a container"},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				r := newOpsRig(t)
				r.start()
				defer r.stop()
				r.ops.failAt(agentops.StepCheckServer, c.err)
				j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
				if j.Code != c.code {
					t.Errorf("code %q, want %q", j.Code, c.code)
				}
				if !reflect.DeepEqual(j.ErrorInfo, c.info) {
					t.Errorf("facts %+v, want %+v", j.ErrorInfo, c.info)
				}
				if !strings.Contains(j.Error, c.say) || !strings.HasSuffix(j.Error, "Nothing was changed on the server, and nothing was kept.") {
					t.Errorf("the job says %q", j.Error)
				}
				if len(r.nodes()) != 0 || r.ops.called("UndoInstall") != 0 {
					t.Errorf("nodes %v, calls %v", r.nodes(), r.ops.allCalls())
				}
				// and it is no business of the facts when there are none
				b, _ := json.Marshal(j)
				if c.info == nil && strings.Contains(string(b), "errorInfo") {
					t.Errorf("facts where there are none: %s", b)
				}
			})
		})
	}
}

// A takeover that goes wrong after it took the server's Xray says so plainly,
// whichever way the cleanup went: that Xray was stopped and removed as the
// person asked, and nothing puts it back. One that stopped before taking it
// has nothing to say of the kind.
func TestAFailedTakeoverSaysTheServersOwnXrayIsNotRestored(t *testing.T) {
	const note = "The server's own Xray was stopped and removed, as you asked, and is not restored."
	cause := &agentops.UnreachableError{What: "agent", Host: "203.0.113.9", Port: 9090}

	t.Run("cleaned up", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.install.TookOver = true
			r.ops.failAt(agentops.StepCheckAgent, cause)
			j := r.failed(r.add(takeoverReq("Frankfurt", "203.0.113.9")))
			if want := cause.Error() + " The server was cleaned up and nothing was kept. " + note; j.Error != want {
				t.Errorf("the job says %q\nwant %q", j.Error, want)
			}
			if j.Code != api.CodeNodeUnreachable || j.ErrorInfo == nil || !j.ErrorInfo.TookOver {
				t.Errorf("code %q, facts %+v", j.Code, j.ErrorInfo)
			}
			if r.ops.called("UndoInstall") != 1 || len(r.nodes()) != 0 {
				t.Errorf("calls %v, nodes %v", r.ops.allCalls(), r.nodes())
			}
		})
	})

	t.Run("the cleanup fails too", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.install.TookOver = true
			r.ops.failAt(agentops.StepCheckAgent, cause)
			r.ops.failAt(agentops.StepCleanServer, &agentops.UnreachableError{What: "ssh", Host: "203.0.113.9", Port: 22})
			j := r.failed(r.add(takeoverReq("Frankfurt", "203.0.113.9")))
			if !strings.Contains(j.Error, "kept as unfinished") || !strings.HasSuffix(j.Error, note) || j.ErrorInfo == nil || !j.ErrorInfo.TookOver {
				t.Errorf("the job says %q, facts %+v", j.Error, j.ErrorInfo)
			}
			// the record that waits for a cleanup says it too
			nodes := r.nodes()
			if len(nodes) != 1 || !nodes[0].Unfinished() || !strings.HasSuffix(nodes[0].Failure.Error, note) {
				t.Fatalf("nodes %+v", nodes)
			}
		})
	})

	t.Run("stopped before it took anything", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.install.TookOver = true // what it would have done: nothing, since it failed first
			r.ops.failAt(agentops.StepTakeOverXray, fmt.Errorf("%w: in a container", agentops.ErrXrayNotStopped))
			j := r.failed(r.add(takeoverReq("Frankfurt", "203.0.113.9")))
			if strings.Contains(j.Error, "not restored") || !strings.HasSuffix(j.Error, "Nothing was changed on the server, and nothing was kept.") {
				t.Errorf("the job says %q", j.Error)
			}
			if j.Code != api.CodeXrayNotStopped || j.ErrorInfo != nil {
				t.Errorf("code %q, facts %+v", j.Code, j.ErrorInfo)
			}
			if r.ops.called("UndoInstall") != 0 || len(r.nodes()) != 0 {
				t.Errorf("calls %v, nodes %v", r.ops.allCalls(), r.nodes())
			}
			// the step that failed is the takeover's, and the ones after it never started
			s := stepStates(j)
			if s[agentops.StepTakeOverXray] != jobs.StepFailed || s[agentops.StepPrepareServer] != jobs.StepPending || s[StepDiscardNode] != jobs.StepOK {
				t.Errorf("steps %+v", j.Steps)
			}
		})
	})

	// a plain install that fails after changing the server says nothing of the kind
	t.Run("not asked for", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newOpsRig(t)
			r.start()
			defer r.stop()
			r.ops.failAt(agentops.StepCheckAgent, cause)
			j := r.failed(r.add(installReq("Frankfurt", "203.0.113.9")))
			if strings.Contains(j.Error, "not restored") || (j.ErrorInfo != nil && j.ErrorInfo.TookOver) {
				t.Errorf("the job says %q, facts %+v", j.Error, j.ErrorInfo)
			}
		})
	})
}

// The facts are read out of the typed errors wherever they sit in what was
// returned, and an error with none gives none.
func TestTheFactsAreReadOutOfTheTypedErrorsAtAnyDepth(t *testing.T) {
	deep := func(err error) error {
		return &failedInstall{cause: fmt.Errorf("installing: %w", err), outcome: cleanedUp}
	}
	pu := &agentops.PortInUseError{Port: 8443, Role: "entry", Process: "caddy"}
	if got := JobErrorInfo(deep(pu)); !reflect.DeepEqual(got, &jobs.ErrorInfo{Port: 8443, Role: "entry", Process: "caddy"}) {
		t.Errorf("a port: %+v", got)
	}
	hx := &agentops.ServerHasXrayError{Binary: true, Version: "26.10.21", SameBuild: true}
	if got := JobErrorInfo(deep(hx)); !reflect.DeepEqual(got, &jobs.ErrorInfo{Found: []string{"binary"}, XrayVersion: "26.10.21", SameBuild: true}) {
		t.Errorf("their Xray: %+v", got)
	}
	both := &failedInstall{cause: pu, outcome: cleanedUp, tookOver: true}
	if got := JobErrorInfo(both); !reflect.DeepEqual(got, &jobs.ErrorInfo{Port: 8443, Role: "entry", Process: "caddy", TookOver: true}) {
		t.Errorf("a port after a takeover: %+v", got)
	}
	for _, err := range []error{nil, errors.New("plain"), deep(errors.New("plain")), deep(agentops.ErrLoginFailed), &failedInstall{cause: errors.New("x"), outcome: leftAlone}} {
		if got := JobErrorInfo(err); got != nil {
			t.Errorf("%v gave %+v", err, got)
		}
	}
}

// What the page words are the codes the hub sends for a refusal that a person
// can act on, and each has its words in both languages and its place in the
// page's list of the codes it words.
func TestTheCodesOfAFixableFailureHaveWordsOnThePage(t *testing.T) {
	const web = "../../../cmd/remote-nodes/web/src/"
	read := func(file string) string {
		t.Helper()
		b, err := os.ReadFile(web + file)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	codes, english, persian := read("i18n/errors.ts"), read("i18n/en.ts"), read("i18n/fa.ts")
	for _, code := range []string{
		api.CodeServerHasXray, api.CodeServerHasAgent, api.CodeServerIsNode, api.CodeXrayNotStopped, api.CodePortInUse,
		api.CodeLoginRefused, api.CodeNeedsRoot, api.CodeHostKeyChanged,
	} {
		if !strings.Contains(codes, "'"+code+"',") {
			t.Errorf("%s is not among the codes the page words (i18n/errors.ts)", code)
		}
		for lang, text := range map[string]string{"en.ts": english, "fa.ts": persian} {
			if !strings.Contains(text, "'error."+code+"':") {
				t.Errorf("%s has no words in i18n/%s: add 'error.%s'", code, lang, code)
			}
		}
	}
}

// The page's type for the facts has every field the hub can send, under the name
// the hub sends it by, so that a field the hub adds can't go unread.
func TestThePagesErrorInfoHasEveryFieldTheHubSends(t *testing.T) {
	b, err := os.ReadFile("../../../cmd/remote-nodes/web/src/lib/api.ts")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)export interface ErrorInfo \{(.*?)\n\}`).FindSubmatch(b)
	if block == nil {
		t.Fatal("api.ts has no ErrorInfo interface")
	}
	typ := reflect.TypeOf(jobs.ErrorInfo{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if !regexp.MustCompile(`(?m)^\s+` + name + `\?: `).Match(block[1]) {
			t.Errorf("the page's ErrorInfo has no optional field %q", name)
		}
	}
	if got := len(regexp.MustCompile(`(?m)^\s+\w+\?: `).FindAll(block[1], -1)); got != typ.NumField() {
		t.Errorf("the page's ErrorInfo has %d fields, the hub's has %d", got, typ.NumField())
	}
}
