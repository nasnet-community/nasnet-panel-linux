// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

func TestInstallingCountsTheStepsTheWayThePageDoes(t *testing.T) {
	step := func(name string, state jobs.StepState, detail string) jobs.Step {
		return jobs.Step{Name: name, State: state, Detail: detail}
	}
	for name, c := range map[string]struct {
		steps   []jobs.Step
		step    int
		label   string
		percent int // -1 for none
	}{
		"the first step running":            {[]jobs.Step{step("check", jobs.StepRunning, ""), step("upload", jobs.StepPending, "")}, 1, "check", -1},
		"a step with a share done":          {[]jobs.Step{step("release", jobs.StepOK, ""), step("upload", jobs.StepRunning, "38% · 8.4 of 13.6 MB"), step("agent", jobs.StepPending, "")}, 2, "upload", 38},
		"a share over a hundred":            {[]jobs.Step{step("upload", jobs.StepRunning, "250% · odd")}, 1, "upload", 100},
		"text that doesn't start with one":  {[]jobs.Step{step("upload", jobs.StepRunning, "about 38% done")}, 1, "upload", -1},
		"between steps":                     {[]jobs.Step{step("release", jobs.StepOK, ""), step("verify", jobs.StepOK, ""), step("upload", jobs.StepPending, ""), step("agent", jobs.StepPending, "")}, 3, "upload", -1},
		"a step that failed, being retried": {[]jobs.Step{step("release", jobs.StepOK, ""), step("upload", jobs.StepFailed, "boom"), step("agent", jobs.StepPending, "")}, 2, "upload", -1},
		"every step done":                   {[]jobs.Step{step("release", jobs.StepOK, ""), step("agent", jobs.StepOK, "")}, 2, "", -1},
		"no steps yet":                      {nil, 0, "", -1},
	} {
		t.Run(name, func(t *testing.T) {
			got := installingOf(jobs.Job{ID: "abc", Kind: jobs.KindInstall, State: jobs.StateRunning, Steps: c.steps})
			if got.JobID != "abc" || got.Steps != len(c.steps) || got.Step != c.step || got.Label != c.label {
				t.Fatalf("installing = %+v, want step %d of %d, label %q", got, c.step, len(c.steps), c.label)
			}
			if c.percent < 0 && got.Percent != nil || c.percent >= 0 && (got.Percent == nil || *got.Percent != c.percent) {
				t.Errorf("percent = %v, want %d", got.Percent, c.percent)
			}
		})
	}
}

// fakeJobs is the job engine as the manager sees it: which job runs on a node.
type fakeJobs map[uint]jobs.Job

func (f fakeJobs) Running(id uint) (jobs.Job, bool) { j, ok := f[id]; return j, ok }

func TestOnlyARunningInstallMakesANodeInstalling(t *testing.T) {
	cfg, _ := baseConfig(t)
	install := jobs.Job{ID: "a", Kind: jobs.KindInstall, State: jobs.StateRunning, Steps: []jobs.Step{{Name: "upload", State: jobs.StepRunning}}}
	update := install
	update.Kind = jobs.KindUpdateAgent
	over := install
	over.State = jobs.StateOK
	cfg.Jobs = fakeJobs{1: install, 2: update, 3: over}
	m, err := New(cfg)
	must(t, err)
	if got := m.NodeLive(1).Installing; got == nil || got.JobID != "a" {
		t.Errorf("a node being installed: %+v", got)
	}
	for _, id := range []uint{2, 3, 4} {
		if got := m.NodeLive(id).Installing; got != nil {
			t.Errorf("node %d is installing: %+v", id, got)
		}
	}
	cfg.Jobs = nil
	if m, _ = New(cfg); m.NodeLive(1).Installing != nil {
		t.Error("a hub with no job engine has a node installing")
	}
}

func TestANodeNothingIsKnownOfReadsAsOffWithNoNumbers(t *testing.T) {
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	got := m.NodeLive(42)
	if got.LastSeenAt != nil || got.Agent != nil || got.Installing != nil || got.Tunnel.State != "off" || got.Tunnel.Reason != "" {
		t.Errorf("node 42 = %+v", got)
	}
	if got.Usage != (api.NodeUsage{}) || got.Updates != (api.Updates{}) {
		t.Errorf("node 42 = %+v", got)
	}
	if tr := m.Traffic(42); tr.StepSec != 5 || tr.Samples == nil || len(tr.Samples) != 0 {
		t.Errorf("traffic = %+v", tr)
	}
	if ev := m.NodeEvents(42); ev == nil || len(ev) != 0 {
		t.Errorf("events = %v", ev)
	}
	if got := m.SubscriptionOn(1, 42); got != (api.SubscriptionUsage{}) {
		t.Errorf("a subscription on a node nobody has = %+v", got)
	}
	if m.Xray() != pin.Version {
		t.Errorf("Xray = %q", m.Xray())
	}
}

// bucket is a traffic record of the agent's: bytes of one subscription (and of
// the bridge's own client, which is not a subscription's) in an hour.
func bucket(at time.Time, sub, up, down int64) *agent.TrafficRecord {
	return &agent.TrafficRecord{
		Timestamp:    at.Unix(),
		UserUplink:   map[string]int64{"sub-1@remote-nodes": up, "rvs-t1@reverse": 5 * up},
		UserDownlink: map[string]int64{"sub-1@remote-nodes": down, "rvs-t1@reverse": 5 * down},
	}
}

func TestNodeLiveAndTheSubscriptionsCarryWhatTheCollectorCounts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newBridgeRig(t)
		st := r.cfg.Store
		n := addNode(t, st)
		ali, sara := addSubscription(t, st, "ali"), addSubscription(t, st, "sara")
		enable(t, st, ali.ID, n.ID, true)
		enable(t, st, sara.ID, n.ID, false)
		ag := r.net.agent(n.ID)
		ag.set(func(a *fakeAgent) {
			a.buffered = []*agent.TrafficRecord{bucket(epoch.Add(-time.Hour), 1, 100, 900), bucket(epoch, 1, 10, 20)}
			a.online = map[string]map[string]int64{"sub-1@remote-nodes": {"198.51.100.7": 1, "198.51.100.8": 2}, "sub-2@remote-nodes": {}}
		})
		r.start()
		defer r.stop()
		r.bridge.tunnel("node-1", up(2, 30, 10))
		r.advance(12 * time.Second)

		l := r.m.NodeLive(n.ID)
		if l.LastSeenAt == nil || !l.LastSeenAt.Equal(epoch.Add(10*time.Second)) || l.Tunnel.State != "up" || l.Tunnel.Streams != 2 {
			t.Errorf("node = %+v", l)
		}
		if l.Agent == nil || l.Agent.Version != "1.3.0" || l.Agent.XrayVersion != pin.Version || !l.Agent.Xray.Running {
			t.Errorf("agent = %+v", l.Agent)
		}
		// the hour that is over is counted, and the one that is not is shown with it
		u := l.Usage
		if !u.Live || u.Online != 1 || u.Connections != 2 || u.TodayUp != 110 || u.TodayDown != 920 || u.MonthUp != 110 || u.UpdatedAt == nil || !u.UpdatedAt.Equal(epoch.Add(10*time.Second)) {
			t.Errorf("usage = %+v", u)
		}
		if l.Updates != (api.Updates{}) || l.Installing != nil {
			t.Errorf("node = %+v", l)
		}

		a := r.m.SubscriptionOn(ali.ID, n.ID)
		if !a.Live || !a.Online || a.Connections != 2 || a.TodayUp != 110 || a.TodayDown != 920 || a.LastSeenAt == nil || !a.LastSeenAt.Equal(epoch.Add(10*time.Second)) {
			t.Errorf("ali on the node = %+v", a)
		}
		if s := r.m.SubscriptionOn(sara.ID, n.ID); !s.Live || s.Online || s.Connections != 0 || s.TodayUp != 0 || s.LastSeenAt != nil {
			t.Errorf("sara on the node = %+v", s)
		}
		if tot := r.m.SubscriptionTotal(ali.ID); !tot.Online || tot.TodayUp != 110 || tot.MonthDown != 920 {
			t.Errorf("ali in all = %+v", tot)
		}
		// the chart has the polls that had something to compare with
		tr := r.m.Traffic(n.ID)
		if tr.StepSec != 5 || len(tr.Samples) != 2 || !tr.Samples[0].At.Equal(epoch.Add(5*time.Second)) || !tr.Samples[1].At.Equal(epoch.Add(10*time.Second)) {
			t.Errorf("traffic = %+v", tr)
		}

		// answers are copies
		l.Agent.Version = "changed"
		*l.LastSeenAt = time.Time{}
		l2 := r.m.NodeLive(n.ID)
		if l2.Agent.Version != "1.3.0" || l2.LastSeenAt.IsZero() {
			t.Error("a change to an answer reached the manager")
		}
		// and a node that stops answering keeps what it last said
		ag.setDown(true)
		r.advance(20 * time.Second)
		l3 := r.m.NodeLive(n.ID)
		if l3.Agent == nil || l3.LastSeenAt == nil || !l3.LastSeenAt.Equal(epoch.Add(10*time.Second)) || l3.Usage.Live {
			t.Errorf("an unreachable node = %+v", l3)
		}
		if l3.Usage.TodayUp != 110 {
			t.Errorf("the numbers went with the node: %+v", l3.Usage)
		}
	})
}

// secret-looking things the state holds and no body may carry
const (
	privateKeyMarker = "MARKER-REALITY-PRIVATE-KEY"
	hostKeyMarker    = "SHA256:MARKER-HOST-KEY-PIN"
)

// The manager behind the API's own handler: the shapes the page reads, filled
// from live state, and none of what the state keeps secret in any body.
func TestTheManagerBehindTheAPI(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		eng := jobs.New(jobs.Config{Now: simNow(), Log: nil})
		r := newBridgeRig(t, func(c *Config) { c.Jobs = eng })
		st := r.cfg.Store
		n1 := addNode(t, st, func(n *model.Node) { n.RealityPrivateKey, n.SSHHostKey = privateKeyMarker, hostKeyMarker })
		n2 := addNode(t, st, func(n *model.Node) { n.State = model.NodeInstalling })
		ali, sara := addSubscription(t, st, "علی"), addSubscription(t, st, "sara")
		enable(t, st, ali.ID, n1.ID, true)
		enable(t, st, sara.ID, n1.ID, false)
		r.net.agent(n1.ID).set(func(a *fakeAgent) {
			a.buffered = []*agent.TrafficRecord{bucket(epoch.Add(-time.Hour), 1, 100, 900), bucket(epoch, 1, 10, 20)}
			a.online = map[string]map[string]int64{"sub-1@remote-nodes": {"198.51.100.7": 1}}
		})

		// node 2's install is running, in its second step
		stop := make(chan struct{})
		job, err := eng.Start(jobs.Spec{Kind: jobs.KindInstall, NodeID: n2.ID, Target: n2.Name, Steps: []string{"release", "upload", "agent"}},
			func(ctx context.Context, run *jobs.Run) error {
				run.Done("release")
				run.Begin("upload")
				run.Detail("upload", "40% · 1.2 of 3 MB")
				select {
				case <-stop:
				case <-ctx.Done():
				}
				return nil
			})
		must(t, err)
		defer func() {
			close(stop)
			must(t, eng.Close(context.Background()))
		}()

		r.start()
		defer r.stop()
		r.bridge.tunnel("node-1", up(1, 30, 10))
		r.advance(12 * time.Second)

		h := api.New(api.Config{Store: st, Jobs: eng, Live: r.m, Version: "9.9.9", Commit: "abc", Log: r.cfg.Log, Now: r.cfg.Now})
		get := func(path string) []byte {
			t.Helper()
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set(api.UIHeader, "1")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
			}
			return rec.Body.Bytes()
		}
		decode := func(body []byte, into any) {
			t.Helper()
			if err := json.Unmarshal(body, into); err != nil {
				t.Fatalf("%v\n%s", err, body)
			}
		}

		var cfg api.AppConfig
		decode(get("/api/config"), &cfg)
		if !cfg.Entry.On || cfg.Entry.Bridge != "running" || cfg.Entry.Domestic != "online" || cfg.Entry.VPN.Kind != "wireguard" || len(cfg.Entry.Routes) != 3 || cfg.Xray != pin.Version {
			t.Errorf("config = %+v", cfg)
		}

		var nodes []api.Node
		decode(get("/api/nodes"), &nodes)
		a, b := nodes[0], nodes[1]
		if a.Tunnel.State != "up" || a.Agent == nil || a.Agent.Version != "1.3.0" || !a.Usage.Live || a.Usage.Online != 1 || a.LastSeenAt == nil || a.Events != nil || a.Installing != nil {
			t.Errorf("node 1 = %+v", a)
		}
		if b.Registered || b.Installing == nil || b.Installing.JobID != job.ID || b.Installing.Step != 2 || b.Installing.Steps != 3 || b.Installing.Label != "upload" ||
			b.Installing.Percent == nil || *b.Installing.Percent != 40 || b.Tunnel.State != "off" || b.Tunnel.Reason != "" {
			t.Errorf("node 2 = %+v", b)
		}

		var one api.Node
		decode(get("/api/nodes/1"), &one)
		if one.Events == nil || len(*one.Events) != 1 || (*one.Events)[0].Kind != "tunnel_up" {
			t.Errorf("node 1's events = %v", one.Events)
		}

		var rows []api.NodeSubscription
		decode(get("/api/nodes/1/subscriptions"), &rows)
		if len(rows) != 2 || !rows[0].Enabled || rows[0].Online == nil || !*rows[0].Online || *rows[0].Connections != 1 || rows[0].TodayUp != 110 ||
			rows[1].Enabled || rows[1].Online != nil || rows[1].Connections != nil {
			t.Errorf("rows = %+v", rows)
		}

		var subs []api.Subscription
		decode(get("/api/subscriptions"), &subs)
		if subs[0].Online == nil || !*subs[0].Online || subs[0].TodayUp != 110 || subs[0].MonthDown != 920 || subs[1].Online == nil || *subs[1].Online {
			t.Errorf("subscriptions = %+v", subs)
		}

		var tr api.Traffic
		decode(get("/api/nodes/1/traffic"), &tr)
		if tr.StepSec != 5 || len(tr.Samples) == 0 {
			t.Errorf("traffic = %+v", tr)
		}

		// and nothing the state keeps secret is in any of it
		state := st.Read()
		secrets := []string{
			privateKeyMarker, hostKeyMarker, bridgeIDOf(1), bridgeIDOf(2), clientIDOf(ali.ID, n1.ID), clientIDOf(sara.ID, n1.ID),
			"BEGIN EC PRIVATE KEY", "BEGIN CERTIFICATE", pemBody(state.CAKey), pemBody(state.CACert),
		}
		for _, p := range []string{"/api/config", "/api/nodes", "/api/nodes/1", "/api/nodes/2", "/api/nodes/1/subscriptions", "/api/nodes/1/traffic", "/api/subscriptions", "/api/jobs"} {
			body := get(p)
			for _, s := range secrets {
				if s != "" && bytes.Contains(body, []byte(s)) {
					t.Errorf("GET %s carries %q", p, s)
				}
			}
		}
		if len(pemBody(state.CAKey)) < 50 || state.Nodes[0].BridgeUUID == "" {
			t.Fatal("the state holds no CA or bridge id to hide")
		}
		if strings.Contains(r.log.String(), privateKeyMarker) || strings.Contains(r.log.String(), pemBody(state.CAKey)) {
			t.Error("the log carries a secret")
		}
	})
}

// pemBody is the base64 of a PEM block without its lines.
func pemBody(pem string) string {
	var body []string
	for _, line := range strings.Split(pem, "\n") {
		if line != "" && !strings.HasPrefix(line, "-----") {
			body = append(body, line)
		}
	}
	return strings.Join(body, "")
}
