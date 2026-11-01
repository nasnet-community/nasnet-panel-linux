// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

func TestTheCodesAreTheContractsCodes(t *testing.T) {
	contract := strings.Fields(`bad_request forbidden not_found node_limit subscription_limit name_taken entry_off
		node_unreachable needs_login version_mismatch agent_too_old host_key_changed server_has_xray server_has_agent server_is_node
		xray_not_stopped port_in_use login_refused needs_root reality_target_failed busy backup_bad_passphrase backup_bad_file internal`)
	var have []string
	for code, status := range statuses {
		have = append(have, code)
		if status < 400 || status > 599 {
			t.Errorf("%s has status %d", code, status)
		}
	}
	slices.Sort(have)
	slices.Sort(contract)
	if !slices.Equal(have, contract) {
		t.Fatalf("codes have %v, the contract says %v", have, contract)
	}
	for code, want := range map[string]int{
		CodeBadRequest: 400, CodeForbidden: 403, CodeNotFound: 404, CodeNodeLimit: 409, CodeSubscriptionLimit: 409,
		CodeNameTaken: 409, CodeBusy: 409, CodeServerHasXray: 409, CodeServerHasAgent: 409, CodeServerIsNode: 409, CodeInternal: 500,
		CodeXrayNotStopped: 409, CodePortInUse: 409, CodeLoginRefused: 409, CodeNeedsRoot: 409, CodeRealityTargetFailed: 409,
	} {
		if statusFor(code) != want {
			t.Errorf("%s answers %d, want %d", code, statusFor(code), want)
		}
	}
	if statusFor("made_up") != http.StatusInternalServerError {
		t.Error("an unknown code should be a 500")
	}
}

// errorsFromTheModel are the real ones, from the cap and the checks.
func errorsFromTheModel(t *testing.T) (nodeLimit, subLimit, taken, invalid error) {
	t.Helper()
	s := model.NewState()
	for i := 0; i < model.MaxNodes; i++ {
		if _, err := s.AddNode(model.Node{Name: "n", Host: fmt.Sprintf("203.0.113.%d", i+1), SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	_, nodeLimit = s.AddNode(model.Node{Name: "n", Host: "203.0.113.9", SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: now})
	for i := 0; i < model.MaxSubscriptions; i++ {
		if _, err := s.AddSubscription(model.Subscription{Name: fmt.Sprint("s", i), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	_, subLimit = s.AddSubscription(model.Subscription{Name: "more", CreatedAt: now})
	s.Subscriptions = s.Subscriptions[:1]
	_, taken = s.AddSubscription(model.Subscription{Name: "S0", CreatedAt: now})
	_, invalid = model.CleanName("a\nb")
	return
}

func TestFailGivesEveryErrorItsCode(t *testing.T) {
	nodeLimit, subLimit, taken, invalid := errorsFromTheModel(t)
	for name, err := range map[string]error{"node limit": nodeLimit, "subscription limit": subLimit, "name taken": taken, "invalid": invalid} {
		if err == nil {
			t.Fatalf("the model gave no %s error", name)
		}
	}
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"the fourth node", nodeLimit, 409, "node_limit"},
		{"the fourth node, wrapped", fmt.Errorf("adding: %w", nodeLimit), 409, "node_limit"},
		{"the eleventh subscription", subLimit, 409, "subscription_limit"},
		{"a taken name", taken, 409, "name_taken"},
		{"a bad name", invalid, 400, "bad_request"},
		{"a bad name inside a node", fmt.Errorf("node 2: %w", invalid), 400, "bad_request"},
		{"not found", fmt.Errorf("%w: node 4", model.ErrNotFound), 404, "not_found"},
		{"a coded error", &Error{CodeBusy, "A job is already running on that node."}, 409, "busy"},
		{"a coded error, wrapped", fmt.Errorf("removing: %w", &Error{CodeHostKeyChanged, "The host key changed."}), 409, "host_key_changed"},
		{"anything else", errors.New("open /data/state.json: permission denied"), 500, "internal"},
	}
	for _, c := range cases {
		log := &syncBuf{}
		a := &API{cfg: Config{Log: slog.New(slog.NewTextHandler(log, nil))}}
		rec := httptest.NewRecorder()
		a.fail(rec, c.err)
		body := decode[map[string]any](t, rec.Body.Bytes())
		if rec.Code != c.status || body["code"] != c.code {
			t.Errorf("%s: %d %s, want %d %s", c.name, rec.Code, rec.Body, c.status, c.code)
		}
		if msg, _ := body["error"].(string); msg == "" || !strings.HasSuffix(msg, ".") || msg[0] < 'A' || msg[0] > 'Z' {
			t.Errorf("%s: the sentence is %q", c.name, body["error"])
		}
		if _, ok := body["message"]; ok {
			t.Errorf("%s: the sentence belongs in error, not message", c.name)
		}
		if len(body) != 2 {
			t.Errorf("%s: extra keys in %s", c.name, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content type %q", c.name, ct)
		}
		// only the hub's own faults are logged, and never shown
		if internal := c.code == "internal"; internal != strings.Contains(log.String(), "state.json") {
			t.Errorf("%s: log = %q", c.name, log.String())
		}
		if c.code == "internal" && strings.Contains(rec.Body.String(), "state.json") {
			t.Errorf("%s: the page got the error's own text: %s", c.name, rec.Body)
		}
	}
}

func TestTheLimitSentencesSayTheNumbers(t *testing.T) {
	if e := classify(model.ErrNodeLimit); !strings.Contains(e.Msg, "3") {
		t.Errorf("node limit: %q", e.Msg)
	}
	if e := classify(model.ErrSubscriptionLimit); !strings.Contains(e.Msg, "10") {
		t.Errorf("subscription limit: %q", e.Msg)
	}
	if classify(errors.New("x")) != nil {
		t.Error("an unknown error was given a code")
	}
}

// A handler passes the error of Engine.Start on as it is, and the page gets
// the busy code. The job's id is in the sentence and nowhere else: the
// contract has no field for it.
func TestASecondJobOnANodeIsTheBusyCode(t *testing.T) {
	e := jobs.New(jobs.Config{Log: slog.New(slog.NewTextHandler(&syncBuf{}, nil))})
	release := make(chan struct{})
	t.Cleanup(func() { close(release); _ = e.Close(context.Background()) })
	first, err := e.Start(jobs.Spec{Kind: jobs.KindInstall, NodeID: 3}, func(ctx context.Context, _ *jobs.Run) error {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, fromStart := e.Start(jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: 3}, func(context.Context, *jobs.Run) error { return nil })
	if fromStart == nil {
		t.Fatal("the engine took a second job for the node")
	}

	for name, err := range map[string]error{
		"as Start gives it": fromStart,
		"wrapped":           fmt.Errorf("updating node 3: %w", fromStart),
	} {
		rec := httptest.NewRecorder()
		(&API{cfg: Config{Log: slog.New(slog.NewTextHandler(&syncBuf{}, nil))}}).fail(rec, err)
		body := decode[map[string]any](t, rec.Body.Bytes())
		msg, _ := body["error"].(string)
		if rec.Code != http.StatusConflict || body["code"] != "busy" || len(body) != 2 || !strings.HasSuffix(msg, ".") || msg[0] != 'A' ||
			!strings.Contains(msg, first.ID) {
			t.Errorf("%s: %d %s, want 409 busy naming job %s", name, rec.Code, rec.Body, first.ID)
		}
	}
	// without the job's id, in a bare ErrBusy, it still says busy
	rec := httptest.NewRecorder()
	(&API{cfg: Config{}}).fail(rec, jobs.ErrBusy)
	body := decode[map[string]any](t, rec.Body.Bytes())
	if rec.Code != http.StatusConflict || body["code"] != "busy" || body["error"] != "A job is already running on that node." {
		t.Errorf("a bare ErrBusy: %d %s", rec.Code, rec.Body)
	}
}

func TestNeedsLoginSaysSoInTheBodyToo(t *testing.T) {
	a := &API{cfg: Config{}}
	rec := httptest.NewRecorder()
	a.fail(rec, &Error{CodeNeedsLogin, "The node can't be reached: send its login."})
	body := decode[map[string]any](t, rec.Body.Bytes())
	if rec.Code != http.StatusConflict || body["code"] != "needs_login" || body["needLogin"] != true {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"the name is required":                 "The name is required.",
		"ssh port: a port must be 1 to 65535.": "Ssh port: a port must be 1 to 65535.",
		"":                                     "",
		"علی is taken":                         "علی is taken.",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}
