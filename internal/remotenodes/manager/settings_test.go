// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

func ptrTo[T any](v T) *T { return &v }

func realKeys(t *testing.T) agentops.RealityKeys {
	t.Helper()
	k, err := agentops.NewRealityKeys()
	must(t, err)
	return k
}

// withKeys gives a node REALITY values that make a config.
func withKeys(k agentops.RealityKeys) func(*model.Node) {
	return func(n *model.Node) {
		n.RealityPrivateKey, n.RealityPublicKey, n.RealityShortID = k.PrivateKey, k.PublicKey, k.ShortID
	}
}

// setUp is a router with a node that is set up and answering, and the three
// subscriptions that are or aren't on it, ready for an operation.
type setUp struct {
	*opsRig
	node model.Node
	keys agentops.RealityKeys
	disk *scriptedStore
}

func newSetUp(t *testing.T, mods ...func(*model.Node)) *setUp {
	t.Helper()
	s := &setUp{keys: realKeys(t)}
	s.opsRig = newOpsRig(t, func(c *Config) { s.disk = newScripted(c.Store); c.Store = s.disk })
	s.node = addNode(t, s.cfg.Store, append([]func(*model.Node){withKeys(s.keys)}, mods...)...)
	ali, sara, off := addSubscription(t, s.cfg.Store, "ali"), addSubscription(t, s.cfg.Store, "sara"), addSubscription(t, s.cfg.Store, "off")
	enable(t, s.cfg.Store, ali.ID, s.node.ID, true)
	enable(t, s.cfg.Store, sara.ID, s.node.ID, true)
	enable(t, s.cfg.Store, off.ID, s.node.ID, false)
	return s
}

func (s *setUp) current() model.Node { return nodeOf(s.cfg.Store, s.node.ID) }

// clientsWanted are the two subscriptions that are on, as the node is told of them.
func (s *setUp) clientsWanted() []nodecfg.Client {
	return []nodecfg.Client{
		{UUID: clientIDOf(1, s.node.ID), Email: "sub-1@remote-nodes"},
		{UUID: clientIDOf(2, s.node.ID), Email: "sub-2@remote-nodes"},
	}
}

func (s *setUp) wantNoJob() {
	s.t.Helper()
	if l := s.jobs.List(); len(l) != 0 {
		s.t.Errorf("a job was started: %+v", l)
	}
	if n := s.ops.called("PushConfig"); n != 0 {
		s.t.Errorf("%d pushes", n)
	}
}

func TestANodeCanBeRenamed(t *testing.T) {
	r := newOpsRig(t)
	n := addNode(t, r.cfg.Store, func(n *model.Node) { n.Name = "Frankfurt" })
	other := addNode(t, r.cfg.Store, func(n *model.Node) { n.Name = "Berlin" })
	dead := addNode(t, r.cfg.Store, func(n *model.Node) { n.Name, n.State = "Dead", model.NodeInstalling })
	ctx := context.Background()

	must(t, r.m.RenameNode(ctx, n.ID, "  فرانکفورت "))
	if got := nodeOf(r.cfg.Store, n.ID).Name; got != "فرانکفورت" {
		t.Errorf("name = %q", got)
	}
	// its own name in another case is its own; another node's is taken, in any case
	must(t, r.m.RenameNode(ctx, n.ID, "فرانکفورت"))
	for _, name := range []string{"Berlin", "berlin", "BERLIN"} {
		if err := r.m.RenameNode(ctx, n.ID, name); !errors.Is(err, model.ErrNameTaken) {
			t.Errorf("rename to %q: %v", name, err)
		}
	}
	must(t, r.m.RenameNode(ctx, other.ID, "BERLIN"))
	for _, name := range []string{"", "  ", "a\nb", strings.Repeat("x", model.MaxNameRunes+1)} {
		if err := r.m.RenameNode(ctx, n.ID, name); !isInvalid(err) {
			t.Errorf("rename to %q: %v", name, err)
		}
	}
	if err := r.m.RenameNode(ctx, 99, "Nobody"); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("a node nobody has: %v", err)
	}
	// a node whose install never finished can be named too: its page shows the name
	must(t, r.m.RenameNode(ctx, dead.ID, "Dead one"))
	if r.ops.called("") != 0 || len(r.jobs.List()) != 0 {
		t.Error("a rename went to the node or started a job")
	}
}

func TestChangingTheEntryPortPushesTheConfigTheStateMakesAndKeepsItOnlyThen(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		if got := s.bridge.last().Nodes[0].Port; got != 443 {
			t.Fatalf("the bridge dials port %d", got)
		}
		release := s.ops.holdAt("PushConfig")

		id, err := s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		synctest.Wait()
		if got := s.current().EntryPort; got != 443 {
			t.Errorf("the state has port %d while the node is being pushed the new one", got)
		}
		j, _ := s.jobs.Get(id)
		if j.Kind != jobs.KindChangeSettings || j.NodeID != s.node.ID || j.Target != s.node.Name || j.State != jobs.StateRunning {
			t.Errorf("job = %+v", j)
		}
		release()
		j = s.ok(id)
		if want := []string{agentops.StepApplyConfig, StepSaveNode}; !slices.Equal(stepNames(j), want) {
			t.Errorf("steps %v, want %v", stepNames(j), want)
		}

		// pushed over the node's own link, built from its state
		if len(s.ops.pushes) != 1 || s.ops.called("CheckTarget") != 0 {
			t.Fatalf("calls %v", s.ops.allCalls())
		}
		push := s.ops.pushes[0]
		if push.client.(*fakeConn).a != s.net.agent(s.node.ID) {
			t.Error("the config was pushed over another node's link")
		}
		want := nodecfg.Input{
			EntryPort: 8443, RealityDest: "www.example.com:443", ServerNames: []string{"www.example.com"},
			PrivateKey: s.keys.PrivateKey, ShortIDs: []string{s.keys.ShortID}, Clients: s.clientsWanted(), Bridge: bridgeOf(s.node),
		}
		if fmt.Sprintf("%+v", push.in) != fmt.Sprintf("%+v", want) {
			t.Errorf("pushed %+v\nwant   %+v", push.in, want)
		}
		got := s.current()
		if got.EntryPort != 8443 || got.RealityTarget != "www.example.com:443" || got.RealitySNI != "www.example.com" || got.RealityPrivateKey != s.keys.PrivateKey {
			t.Errorf("node = %+v", got)
		}
		// and the bridge dials the new port
		if p := s.bridge.last().Nodes[0]; p.Port != 8443 {
			t.Errorf("the bridge dials port %d", p.Port)
		}
	})
}

func TestChangingTheHandshakeTargetHasTheNodeTryItFirst(t *testing.T) {
	for _, c := range []struct {
		name        string
		node        func(*model.Node)
		req         api.SettingsRequest
		target      string
		sni         string
		checks      []string
		steps       []string
		wantInvalid string
	}{
		{
			name: "another site, and the name follows it", req: api.SettingsRequest{HandshakeTarget: ptrTo("www.cloudflare.com")},
			target: "www.cloudflare.com:443", sni: "www.cloudflare.com", checks: []string{"www.cloudflare.com:443"},
		},
		{
			name: "another port of the same host", req: api.SettingsRequest{HandshakeTarget: ptrTo("www.example.com:8443")},
			target: "www.example.com:8443", sni: "www.example.com", checks: []string{"www.example.com:8443"},
		},
		{
			name: "a name a person chose stays", node: func(n *model.Node) { n.RealitySNI = "cdn.example.net" },
			req: api.SettingsRequest{HandshakeTarget: ptrTo("www.cloudflare.com:443")}, target: "www.cloudflare.com:443", sni: "cdn.example.net",
			checks: []string{"www.cloudflare.com:443"},
		},
		{
			name: "a name given with the site", req: api.SettingsRequest{HandshakeTarget: ptrTo("www.cloudflare.com"), SNI: ptrTo("Front.Example.Net")},
			target: "www.cloudflare.com:443", sni: "front.example.net", checks: []string{"www.cloudflare.com:443"},
		},
		{
			name: "an address, with the name to say", req: api.SettingsRequest{HandshakeTarget: ptrTo("203.0.113.77:443"), SNI: ptrTo("front.example.net")},
			target: "203.0.113.77:443", sni: "front.example.net", checks: []string{"203.0.113.77:443"},
		},
		{
			name: "only the name: the site isn't tried again", req: api.SettingsRequest{SNI: ptrTo("front.example.net")},
			target: "www.example.com:443", sni: "front.example.net",
		},
		{
			name: "an empty name follows the site again", node: func(n *model.Node) { n.RealitySNI = "cdn.example.net" },
			req: api.SettingsRequest{SNI: ptrTo("")}, target: "www.example.com:443", sni: "www.example.com",
		},
		{
			name: "the same site written another way is the same site", req: api.SettingsRequest{HandshakeTarget: ptrTo(" WWW.Example.com "), EntryPort: ptrTo(9443)},
			target: "www.example.com:443", sni: "www.example.com",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				mods := []func(*model.Node){}
				if c.node != nil {
					mods = append(mods, c.node)
				}
				s := newSetUp(t, mods...)
				s.start()
				defer s.stop()
				id, err := s.m.ChangeSettings(context.Background(), s.node.ID, c.req)
				must(t, err)
				j := s.ok(id)
				want := []string{agentops.StepApplyConfig, StepSaveNode}
				if len(c.checks) > 0 {
					want = append([]string{agentops.StepChooseTarget}, want...)
				}
				if !slices.Equal(stepNames(j), want) {
					t.Errorf("steps %v, want %v", stepNames(j), want)
				}
				var tried []string
				for _, ck := range s.ops.checks {
					tried = append(tried, ck.target)
					if ck.client.(*fakeConn).a != s.net.agent(s.node.ID) {
						t.Error("the target was tried from another node")
					}
				}
				if !slices.Equal(tried, c.checks) {
					t.Errorf("the node was asked to try %v, want %v", tried, c.checks)
				}
				n := s.current()
				if n.RealityTarget != c.target || n.RealitySNI != c.sni {
					t.Errorf("the state has target %q and name %q, want %q and %q", n.RealityTarget, n.RealitySNI, c.target, c.sni)
				}
				push := s.ops.pushes[0].in
				if push.RealityDest != c.target || !slices.Equal(push.ServerNames, []string{c.sni}) {
					t.Errorf("pushed target %q and names %v", push.RealityDest, push.ServerNames)
				}
				if len(c.checks) > 0 {
					if d := stepDetail(j, agentops.StepChooseTarget); d != c.target {
						t.Errorf("the choosing step says %q", d)
					}
				}
			})
		})
	}
}

func stepDetail(j jobs.Job, name string) string {
	for _, s := range j.Steps {
		if s.Name == name {
			return s.Detail
		}
	}
	return ""
}

func TestSettingsThatCantBeAreRefusedBeforeAJobStarts(t *testing.T) {
	s := newSetUp(t)
	for name, req := range map[string]api.SettingsRequest{
		"nothing":                   {},
		"an entry port of zero":     {EntryPort: ptrTo(0)},
		"an entry port too big":     {EntryPort: ptrTo(70000)},
		"a negative entry port":     {EntryPort: ptrTo(-1)},
		"the SSH port":              {EntryPort: ptrTo(22)},
		"the agent's port":          {EntryPort: ptrTo(9090)},
		"an empty target":           {HandshakeTarget: ptrTo("")},
		"a target that is a URL":    {HandshakeTarget: ptrTo("https://www.example.com")},
		"a target with a bad port":  {HandshakeTarget: ptrTo("www.example.com:0")},
		"an address with no name":   {HandshakeTarget: ptrTo("203.0.113.77:443")},
		"a name that is an address": {SNI: ptrTo("203.0.113.77")},
		"a name that isn't one":     {SNI: ptrTo("two words")},
		"what the node has":         {EntryPort: ptrTo(443), HandshakeTarget: ptrTo("www.example.com:443"), SNI: ptrTo("www.example.com")},
		"a name that is already so": {SNI: ptrTo("")},
	} {
		_, err := s.m.ChangeSettings(context.Background(), s.node.ID, req)
		if !isInvalid(err) {
			t.Errorf("%s: err = %v, want an invalid value", name, err)
		}
	}
	s.wantNoJob()
	if got := s.current(); got.EntryPort != 443 || got.RealityTarget != "www.example.com:443" {
		t.Errorf("the node changed: %+v", got)
	}
}

func TestSettingsCanOnlyBeChangedOnANodeThatIsReadyForThem(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		down := addNode(t, s.cfg.Store, withKeys(s.keys))
		s.net.agent(down.ID).setDown(true)
		unfinished := addNode(t, s.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		s.start()
		defer s.stop()
		req := api.SettingsRequest{EntryPort: ptrTo(8443)}
		var e *api.Error

		if _, err := s.m.ChangeSettings(context.Background(), 99, req); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		if _, err := s.m.ChangeSettings(context.Background(), unfinished.ID, req); !errors.As(err, &e) || e.Code != api.CodeBadRequest {
			t.Errorf("a node that never finished: %v", err)
		}
		if _, err := s.m.ChangeSettings(context.Background(), down.ID, req); !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable {
			t.Errorf("a node that doesn't answer: %v", err)
		}
		// a job that runs on it
		hold := make(chan struct{})
		_, err := s.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: s.node.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := s.m.ChangeSettings(context.Background(), s.node.ID, req); !errors.Is(err, jobs.ErrBusy) {
			t.Errorf("a node with a job: %v", err)
		}
		close(hold)
		synctest.Wait()
		s.wantNoJobsOf(t, s.node.ID, down.ID, unfinished.ID)
	})
}

// wantNoJobsOf checks that no job of the kinds this file starts ever ran for the nodes.
func (s *setUp) wantNoJobsOf(t *testing.T, ids ...uint) {
	t.Helper()
	for _, j := range s.jobs.List() {
		if j.Kind == jobs.KindChangeSettings || j.Kind == jobs.KindRotateKeys {
			t.Errorf("a job was started for node %d: %+v", j.NodeID, j)
		}
	}
	if s.ops.called("PushConfig") != 0 {
		t.Error("a config was pushed")
	}
}

// A site that the node can't use is refused where the node said so, and
// nothing is pushed.
func TestATargetTheNodeCantUseStopsTheChangeBeforeAnythingIsPushed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		s.ops.checkFor["www.cloudflare.com:443"] = agentops.HandshakeCheck{Error: "It doesn't use TLS 1.3."}

		id, err := s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{HandshakeTarget: ptrTo("www.cloudflare.com")})
		must(t, err)
		j := s.failed(id)
		const sentence = "The site www.cloudflare.com:443 doesn't work as a Reality target from this server: it doesn't use TLS 1.3."
		if j.Error != sentence || stepStates(j)[agentops.StepChooseTarget] != jobs.StepFailed || stepStates(j)[agentops.StepApplyConfig] != jobs.StepPending {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if s.ops.called("PushConfig") != 0 || s.current().RealityTarget != "www.example.com:443" {
			t.Error("the node or the state was touched")
		}

		// or the node can't even be asked
		s.ops.failAt("CheckTarget", &agentops.UnreachableError{What: "agent", Host: "h", Port: 9090})
		id, err = s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{HandshakeTarget: ptrTo("www.debian.org")})
		must(t, err)
		if j := s.failed(id); !strings.Contains(j.Error, "TCP port 9090") || s.ops.called("PushConfig") != 0 {
			t.Errorf("job error %q, pushes %d", j.Error, s.ops.called("PushConfig"))
		}
	})
}

// A config the node refuses was never applied, so there is nothing to put
// back. One that was applied and found wanting is put back, and the job says
// the node is as it was, or that it may not be.
func TestAPushThatFailsLeavesTheNodeAsItWas(t *testing.T) {
	entryShut := &agentops.UnreachableError{What: "entry", Host: "203.0.113.1", Port: 8443}
	for _, c := range []struct {
		name   string
		fail   map[string]error
		pushes int
		note   string
		text   string
	}{
		{"refused by the node", map[string]error{"PushConfig": &agentops.ConfigRejectedError{Message: "bad key"}}, 1, "", "The node's Xray refused the configuration: bad key."},
		{"refused as too old to take it", map[string]error{"PushConfig": agentops.ErrAgentOutdated}, 1, "", "too old"},
		{"a port that doesn't answer", map[string]error{"PushConfig#1": entryShut}, 2, "The node is back on its previous settings.", "TCP port 8443"},
		{"and the old config can't be put back", map[string]error{"PushConfig": entryShut}, 2, "could not be put back either", "TCP port 8443"},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSetUp(t)
				s.start()
				defer s.stop()
				for point, err := range c.fail {
					s.ops.failAt(point, err)
				}
				specs := len(s.bridge.specs)

				id, err := s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
				must(t, err)
				j := s.failed(id)
				if !strings.Contains(j.Error, c.text) || (c.note != "" && !strings.Contains(j.Error, c.note)) {
					t.Errorf("the job says %q, want %q and %q", j.Error, c.text, c.note)
				}
				if c.note == "" && strings.Contains(j.Error, "previous settings") {
					t.Errorf("a push the node refused is said to have been put back: %q", j.Error)
				}
				if stepStates(j)[agentops.StepApplyConfig] != jobs.StepFailed || stepStates(j)[StepSaveNode] != jobs.StepPending {
					t.Errorf("steps %v", j.Steps)
				}
				if len(s.ops.pushes) != c.pushes {
					t.Fatalf("%d pushes, want %d", len(s.ops.pushes), c.pushes)
				}
				if c.pushes == 2 {
					// the second is the config the state makes, the one the node had
					if got := s.ops.pushes[1].in.EntryPort; got != 443 {
						t.Errorf("the config put back has port %d", got)
					}
				}
				if got := s.current(); got.EntryPort != 443 {
					t.Errorf("the state has port %d", got.EntryPort)
				}
				if len(s.bridge.specs) != specs {
					t.Error("the bridge was given a spec for a change that didn't happen")
				}
			})
		})
	}
}

// If the state can't be written once the node runs the new settings, the old
// ones are put back, since the links the state makes are the old ones.
func TestSettingsTheStateCantKeepAreTakenOffTheNodeAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		s.disk.failNext(1, errors.New("the disk is full")) // the one write of the job is the save

		id, err := s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		j := s.failed(id)
		if !strings.HasPrefix(j.Error, "the disk is full") || !strings.HasSuffix(j.Error, "The node is back on its previous settings.") || stepStates(j)[StepSaveNode] != jobs.StepFailed {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if len(s.ops.pushes) != 2 || s.ops.pushes[0].in.EntryPort != 8443 || s.ops.pushes[1].in.EntryPort != 443 || s.current().EntryPort != 443 {
			t.Errorf("pushes %+v, state port %d", s.ops.pushes, s.current().EntryPort)
		}
	})
}

// An error that has the REALITY key in it is shown without it, in the job and
// in the log, whichever push said it.
func TestTheRealityKeyShowsNowhereWhenAPushSaysIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		s.ops.failAt("PushConfig", &agentops.UnreachableError{What: "entry", Host: "h", Port: 8443, Err: errors.New("config had " + s.keys.PrivateKey)})

		id, err := s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		j := s.failed(id)
		if strings.Contains(fmt.Sprintf("%+v", j), s.keys.PrivateKey) || strings.Contains(s.log.String(), s.keys.PrivateKey) {
			t.Errorf("the key shows:\n%+v\n%s", j, s.log)
		}
		if !strings.Contains(j.Error, "…") {
			t.Errorf("job error %q", j.Error)
		}
	})
}

func TestAHandshakeCheckIsMadeFromTheNodeItself(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		down := addNode(t, s.cfg.Store, withKeys(s.keys))
		s.net.agent(down.ID).setDown(true)
		unfinished := addNode(t, s.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		s.start()
		defer s.stop()
		ctx := context.Background()
		s.ops.checkFor["www.cloudflare.com:443"] = agentops.HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 3860, LatencyMs: 21}
		s.ops.checkFor["big.example.com:443"] = agentops.HandshakeCheck{TLS13: true, X25519: true, HandshakeBytes: 9632, LatencyMs: 71, Error: "Its side of the handshake is 9632 bytes."}

		got, err := s.m.CheckHandshake(ctx, s.node.ID, " WWW.Cloudflare.com ")
		if err != nil || got != (api.HandshakeCheck{OK: true, TLS13: true, X25519: true, HandshakeBytes: 3860, LatencyMs: 21}) {
			t.Fatalf("CheckHandshake = %+v, %v", got, err)
		}
		if c := s.ops.checks[0]; c.target != "www.cloudflare.com:443" || c.client.(*fakeConn).a != s.net.agent(s.node.ID) {
			t.Errorf("the node was asked %+v", c)
		}
		// a site that won't do is an answer, not an error
		got, err = s.m.CheckHandshake(ctx, s.node.ID, "big.example.com")
		if err != nil || got.OK || got.HandshakeBytes != 9632 || !strings.Contains(got.Error, "9632") {
			t.Errorf("a site that won't do: %+v, %v", got, err)
		}

		for name, c := range map[string]struct {
			id     uint
			target string
		}{"a node nobody has": {99, "a.example:443"}, "an unfinished node": {unfinished.ID, "a.example:443"}, "a node that is down": {down.ID, "a.example:443"}, "a bad target": {s.node.ID, "not a target"}} {
			if _, err := s.m.CheckHandshake(ctx, c.id, c.target); err == nil {
				t.Errorf("%s: no error", name)
			}
		}
		var e *api.Error
		if _, err := s.m.CheckHandshake(ctx, down.ID, "a.example:443"); !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable {
			t.Errorf("a node that is down: %v", err)
		}
		if _, err := s.m.CheckHandshake(ctx, s.node.ID, ""); !isInvalid(err) {
			t.Errorf("an empty target: %v", err)
		}
		// what the node says went wrong is worded for the page
		s.ops.failAt("CheckTarget", agentops.ErrAgentOutdated)
		if _, err := s.m.CheckHandshake(ctx, s.node.ID, "a.example:443"); !errors.As(err, &e) || e.Code != api.CodeAgentTooOld {
			t.Errorf("an agent that can't check: %v", err)
		}
		// it reads, so a job running on the node doesn't stop it
		s.ops.failAt("CheckTarget", nil)
		hold := make(chan struct{})
		_, err = s.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: s.node.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := s.m.CheckHandshake(ctx, s.node.ID, "www.cloudflare.com"); err != nil {
			t.Errorf("with a job running: %v", err)
		}
		close(hold)
	})
}

func TestRotatingTheKeysChangesWhatEveryLinkCarries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		before := s.bridge.last().Nodes[0]

		id, err := s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		j := s.ok(id)
		if want := []string{agentops.StepCreateKeys, agentops.StepApplyConfig, StepSaveNode}; !slices.Equal(stepNames(j), want) || j.Kind != jobs.KindRotateKeys || j.Target != s.node.Name {
			t.Errorf("job = %+v", j)
		}

		// pushed with the new key and short id and nothing else changed
		if len(s.ops.pushes) != 1 {
			t.Fatalf("%d pushes", len(s.ops.pushes))
		}
		in := s.ops.pushes[0].in
		fresh := agentops.RealityKeys{PrivateKey: in.PrivateKey, ShortID: in.ShortIDs[0]}
		got := s.current()
		fresh.PublicKey = got.RealityPublicKey
		if err := fresh.Valid(); err != nil {
			t.Fatalf("the new keys: %v", err)
		}
		if in.PrivateKey == s.keys.PrivateKey || in.ShortIDs[0] == s.keys.ShortID || got.RealityPublicKey == s.keys.PublicKey {
			t.Error("a value was kept that should have changed")
		}
		if in.EntryPort != 443 || in.RealityDest != "www.example.com:443" || !slices.Equal(in.ServerNames, []string{"www.example.com"}) ||
			fmt.Sprint(in.Clients) != fmt.Sprint(s.clientsWanted()) || in.Bridge != bridgeOf(s.node) {
			t.Errorf("pushed %+v", in)
		}
		if got.RealityPrivateKey != in.PrivateKey || got.RealityShortID != in.ShortIDs[0] || got.EntryPort != 443 || got.RealityTarget != "www.example.com:443" {
			t.Errorf("node = %+v", got)
		}
		// the bridge logs in with them too
		after := s.bridge.last().Nodes[0]
		if after.PublicKey != got.RealityPublicKey || after.ShortID != got.RealityShortID || after.PublicKey == before.PublicKey {
			t.Errorf("the bridge has %+v, was %+v", after, before)
		}
		if ev := s.m.NodeEvents(s.node.ID); len(ev) != 1 || ev[0].Kind != EventKeysRotated {
			t.Errorf("events = %v", kinds(ev))
		}
		// each turn makes new ones
		id, err = s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		s.ok(id)
		if again := s.current(); again.RealityPrivateKey == got.RealityPrivateKey || again.RealityPublicKey == got.RealityPublicKey {
			t.Error("the second rotation made the same keys")
		}
	})
}

// A config push restarts Xray on purpose. Rotating the keys and changing a
// port each do it, and neither is a crash, however often they are done.
func TestPushesTheHubMakesAreNotCountedAsXrayCrashes(t *testing.T) {
	for _, what := range []string{"rotate", "settings"} {
		t.Run(what, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSetUp(t)
				s.start()
				defer s.stop()
				release := s.ops.holdAt("PushConfig")
				var id string
				var err error
				if what == "rotate" {
					id, err = s.m.RotateKeys(context.Background(), s.node.ID)
				} else {
					id, err = s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
				}
				must(t, err)
				for pid := int64(101); pid <= 103; pid++ {
					s.net.agent(s.node.ID).set(func(a *fakeAgent) { a.pid, a.started = pid, a.started+60 })
					s.advance(5 * time.Second)
				}
				release()
				s.ok(id)
				s.advance(5 * time.Second)
				if p, _ := s.m.peek(s.node.ID); p.restarts != 0 || p.agent.Xray.CrashLoop {
					t.Errorf("a %s left the node looking like it crash-loops: %+v", what, p)
				}
			})
		})
	}
}

func TestRotatingTheKeysFailsWithTheNodeAndTheStateAsTheyWere(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		before := s.current()
		events := len(s.m.NodeEvents(s.node.ID))

		// refused by the node
		s.ops.failAt("PushConfig", &agentops.ConfigRejectedError{Message: "bad short id"})
		id, err := s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		j := s.failed(id)
		if !strings.Contains(j.Error, "bad short id") || stepStates(j)[agentops.StepCreateKeys] != jobs.StepOK || stepStates(j)[agentops.StepApplyConfig] != jobs.StepFailed {
			t.Errorf("job = %q %v", j.Error, j.Steps)
		}
		if s.current() != before || len(s.ops.pushes) != 1 {
			t.Error("the state changed, or a node that refused the config was pushed to again")
		}

		// the node took it and the entry doesn't answer: the old keys are put back
		s.ops.failAt("PushConfig", nil)
		s.ops.failAt(fmt.Sprintf("PushConfig#%d", s.ops.count["PushConfig"]+1), &agentops.UnreachableError{What: "entry", Host: "h", Port: 443})
		id, err = s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		j = s.failed(id)
		if !strings.HasSuffix(j.Error, "The node is back on its previous settings.") {
			t.Errorf("job error %q", j.Error)
		}
		if n := len(s.ops.pushes); n != 3 || s.ops.pushes[2].in.PrivateKey != s.keys.PrivateKey || s.ops.pushes[1].in.PrivateKey == s.keys.PrivateKey {
			t.Errorf("%d pushes: the last should put the old key back", n)
		}

		// the state can't be written
		s.disk.failNext(1, errors.New("the disk is full"))
		id, err = s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		j = s.failed(id)
		if !strings.HasPrefix(j.Error, "the disk is full") || stepStates(j)[StepSaveNode] != jobs.StepFailed || len(s.ops.pushes) != 5 || s.ops.pushes[4].in.PrivateKey != s.keys.PrivateKey {
			t.Errorf("job = %q, %d pushes", j.Error, len(s.ops.pushes))
		}

		if s.current() != before {
			t.Errorf("the node changed: %+v", s.current())
		}
		if len(s.m.NodeEvents(s.node.ID)) != events {
			t.Errorf("events = %v", kinds(s.m.NodeEvents(s.node.ID)))
		}
		if n, _ := s.bridge.counts(); n != 1 {
			t.Errorf("the bridge was given %d specs", n)
		}
	})
}

// A rotation that fails says neither the old key nor the new one, which a
// node that echoed the config would have said both of: the first push has
// the new key and the one that puts the old config back has the old.
func TestTheOldAndNewRealityKeysAreHiddenFromARotationThatFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		s.ops.leakKeys = true
		s.ops.failAt("PushConfig", &agentops.UnreachableError{What: "entry", Host: "h", Port: 443})
		id, err := s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		j := s.failed(id)
		if len(s.ops.pushes) != 2 {
			t.Fatalf("%d pushes", len(s.ops.pushes))
		}
		fresh, old := s.ops.pushes[0].in.PrivateKey, s.ops.pushes[1].in.PrivateKey
		if old != s.keys.PrivateKey || fresh == old {
			t.Fatalf("the pushes carried %q then %q", fresh, old)
		}
		for _, key := range []string{fresh, old} {
			if strings.Contains(fmt.Sprintf("%+v", j), key) || strings.Contains(s.log.String(), key) {
				t.Errorf("the key %q shows:\n%+v\n%s", key, j, s.log)
			}
		}
		if !strings.Contains(j.Error, "…") || !strings.Contains(s.log.String(), "could not be put back") {
			t.Errorf("job error %q\n%s", j.Error, s.log)
		}
	})
}

func TestRotatingAndChangingNeedANodeThatIsReadyAndFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		down := addNode(t, s.cfg.Store, withKeys(s.keys))
		s.net.agent(down.ID).setDown(true)
		unfinished := addNode(t, s.cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
		s.start()
		defer s.stop()
		var e *api.Error
		if _, err := s.m.RotateKeys(context.Background(), 99); !errors.Is(err, model.ErrNotFound) {
			t.Errorf("a node nobody has: %v", err)
		}
		if _, err := s.m.RotateKeys(context.Background(), unfinished.ID); !errors.As(err, &e) || e.Code != api.CodeBadRequest {
			t.Errorf("a node that never finished: %v", err)
		}
		if _, err := s.m.RotateKeys(context.Background(), down.ID); !errors.As(err, &e) || e.Code != api.CodeNodeUnreachable {
			t.Errorf("a node that doesn't answer: %v", err)
		}
		hold := make(chan struct{})
		_, err := s.m.startJob(jobs.Spec{Kind: jobs.KindRenewCert, NodeID: s.node.ID}, func(context.Context, *jobs.Run) error { <-hold; return nil })
		must(t, err)
		if _, err := s.m.RotateKeys(context.Background(), s.node.ID); !errors.Is(err, jobs.ErrBusy) {
			t.Errorf("a node with a job: %v", err)
		}
		close(hold)
		synctest.Wait()
		s.wantNoJobsOf(t)
	})
}

// A node with no key on record can't be given a config, and says so in words
// a person can act on.
func TestANodeWithNoKeyOnRecordCantBeGivenAConfig(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		editNode(t, s.cfg.Store, s.node.ID, func(n *model.Node) { n.RealityPrivateKey = "" })
		s.start()
		defer s.stop()
		id, err := s.m.RotateKeys(context.Background(), s.node.ID)
		must(t, err)
		if j := s.failed(id); !strings.Contains(j.Error, "no REALITY key") || s.ops.called("PushConfig") != 0 {
			t.Errorf("job error %q", j.Error)
		}
		editNode(t, s.cfg.Store, s.node.ID, func(n *model.Node) { n.RealityPrivateKey, n.BridgeUUID = s.keys.PrivateKey, "" })
		id, err = s.m.ChangeSettings(context.Background(), s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		if j := s.failed(id); !strings.Contains(j.Error, "client id for the router's bridge") {
			t.Errorf("job error %q", j.Error)
		}
	})
}

// A config replaces every client a node has, so what is switched on while one
// is pushed would be lost to it. The push has the node's turn, and a switch
// that comes during it waits and is made on the node that runs the config.
func TestASubscriptionSwitchedDuringAPushWaitsForItAndIsAddedAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		ctx := context.Background()
		release := s.ops.holdAt("PushConfig")

		id, err := s.m.ChangeSettings(ctx, s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		synctest.Wait() // the push is under way

		done := make(chan error, 1)
		go func() { _, err := s.m.SetEnabled(ctx, 3, s.node.ID, true); done <- err }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("the switch didn't wait for the push (%v)", err)
		default:
		}
		st := s.cfg.Store.Read()
		if e := st.FindEnablement(3, s.node.ID); e == nil || e.Enabled {
			t.Errorf("the switch was made while the config was pushed: %+v", e)
		}

		release()
		s.ok(id)
		must(t, <-done)
		// the config was read before the switch, and says so
		if got := s.ops.pushes[0].in.Clients; fmt.Sprint(got) != fmt.Sprint(s.clientsWanted()) {
			t.Errorf("the push had the clients %+v", got)
		}
		// and the client was added to the node after the push, which is the only way it is on the node that runs it
		calls := s.net.agent(s.node.ID).callsFrom(0)
		push, add := slices.Index(calls, "PushConfig"), slices.Index(calls, "AddUser sub-3@remote-nodes")
		if push < 0 || add < 0 || add < push {
			t.Errorf("the node's calls were %v: the client should be added after the push", calls)
		}
		st = s.cfg.Store.Read()
		if e := st.FindEnablement(3, s.node.ID); e == nil || !e.Enabled {
			t.Errorf("after the push: %+v", e)
		}
	})
}

// A switch that is under way when a change starts is in the config, which is
// read when it is the change's turn and not before.
func TestAPushIsBuiltFromWhatTheStateSaysWhenItsTurnComes(t *testing.T) {
	for _, kind := range []string{"settings", "rotation"} {
		t.Run(kind, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSetUp(t)
				s.start()
				defer s.stop()
				ctx := context.Background()
				// somebody is switching the third subscription on, and has the node's turn
				unlock, err := s.m.lockNodes(ctx, s.node.ID)
				must(t, err)
				var id string
				if kind == "settings" {
					id, err = s.m.ChangeSettings(ctx, s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
				} else {
					id, err = s.m.RotateKeys(ctx, s.node.ID)
				}
				must(t, err)
				synctest.Wait()
				if s.ops.called("PushConfig") != 0 {
					t.Fatal("the config was pushed while somebody else had the node's turn")
				}
				enable(t, s.cfg.Store, 3, s.node.ID, true)
				unlock()
				s.ok(id)
				want := append(s.clientsWanted(), nodecfg.Client{UUID: clientIDOf(3, s.node.ID), Email: "sub-3@remote-nodes"})
				if got := s.ops.pushes[0].in.Clients; fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("the push had the clients %+v, want %+v", got, want)
				}
			})
		})
	}
}

// The node's turn is given back by a change that fails, or a switch after it
// would wait for ever.
func TestAFailedPushGivesTheNodesTurnBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSetUp(t)
		s.start()
		defer s.stop()
		ctx := context.Background()
		s.ops.failAt("PushConfig", &agentops.ConfigRejectedError{Message: "no"})
		id, err := s.m.ChangeSettings(ctx, s.node.ID, api.SettingsRequest{EntryPort: ptrTo(8443)})
		must(t, err)
		s.failed(id)
		id, err = s.m.RotateKeys(ctx, s.node.ID)
		must(t, err)
		s.failed(id)
		s.ops.failAt("PushConfig", nil)
		s.disk.failNext(1, errors.New("the disk is full"))
		id, err = s.m.RotateKeys(ctx, s.node.ID)
		must(t, err)
		s.failed(id)
		// a change of the state that can't be written, and one that fails before the node's turn comes
		s.net.agent(s.node.ID).setDown(true)
		s.advance(30 * time.Second)

		unlock, err := s.m.lockNodes(ctx, s.node.ID)
		if err != nil {
			t.Fatalf("the node's turn was kept: %v", err)
		}
		unlock()
	})
}
