// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// fixturesFile is the page's copy of what this API sends, written in the page's
// own types so that its typecheck and its tests run against the hub's real
// shapes. It sits in the page's source, which is where the page looks.
const fixturesFile = "../../../cmd/remote-nodes/web/src/lib/hub-fixtures.ts"

var updateFixtures = flag.Bool("update-fixtures", false, "rewrite the page's copy of the API's answers ("+fixturesFile+")")

// testStepKey gives the steps these tests use the keys the manager gives them.
// The manager's table is the real one, which this package can't import.
func testStepKey(name string) string {
	return map[string]string{
		"Check the server": "check_server", "Download the agent": "download_agent", "Upload the agent": "upload_agent",
		"Start the agent": "start_agent", "Save the node": "save_node", "Forget the node": "forget_node",
		"Clean up the server": "clean_server", "Discard the node": "discard_node",
	}[name]
}

// testErrorCode gives a job the code of an *Error, as the manager's
// JobErrorCode does for the errors it words.
func testErrorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// addUnfinished puts an install that stopped and couldn't be cleaned up beside
// the populated nodes, as node 3.
func (r *rig) addUnfinished() {
	r.t.Helper()
	err := r.st.Update(func(s *model.State) error {
		_, err := s.AddNode(model.Node{
			Name: "stopped", Host: "203.0.113.3", SSHPort: 22, SSHUser: "root", AgentPort: 9090, EntryPort: 443,
			State: model.NodeInstalling, CreatedAt: now, SSHHostKey: hostKeyPin,
			Failure: model.InstallFailure{
				Step: "Upload the agent", StepKey: "upload_agent", Error: "Couldn't upload the agent: the connection broke.",
				Cleanup: "Can't reach SSH on 203.0.113.3 port 22.", At: now.Add(-90 * time.Second),
			},
		})
		return err
	})
	if err != nil {
		r.t.Fatal(err)
	}
}

// The hub's answers to the page's GET calls are written as they are sent into a
// file of the page's types. The page then fails to compile when its types stop
// fitting what the hub sends, and this fails when the file is not what the hub
// sends now. After a change that is meant:
//
//	go test ./internal/remotenodes/api -run TestThePagesFixtures -update-fixtures
func TestThePagesFixturesAreWhatTheHubSends(t *testing.T) {
	// a hub that knows everything: a node with its agent, one still installing,
	// subscriptions with connections, the router's report and the jobs of the tray
	live := newFakeLive()
	r := newRigWith(t, "", withLive(live))
	r.populate()
	// and one that knows nothing but its state, as a hub with no manager does
	bare := newRig(t, "")
	bare.populate()
	// and on both, an install that stopped and couldn't be cleaned up: no node, and not counted as one
	r.addUnfinished()
	bare.addUnfinished()

	// the tray, newest first: an install under way, an update that failed, a removal that worked
	gone := r.finished(jobs.Spec{Kind: jobs.KindRemove, NodeID: 3, Target: "Berlin", Steps: []string{"Forget the node"}}, func(run *jobs.Run) error {
		run.Begin("Forget the node")
		run.Done("Forget the node")
		return nil
	})
	failed := r.finished(jobs.Spec{Kind: jobs.KindUpdateAgent, NodeID: 1, Target: "تهران", Steps: []string{"Download the agent", "Upload the agent", "Start the agent"}}, func(run *jobs.Run) error {
		run.Set("Download the agent", jobs.StepOK, "v1.3.1 · 18 MB")
		run.Fail("Upload the agent", errors.New("The connection broke."))
		return errors.New("The connection broke.")
	})
	// an install refused at its first step: nothing was changed, nothing kept, and the form for another try
	const xrayHere = "This server already runs an Xray that Remote Nodes did not install (/usr/local/bin/xray). Use a server without one."
	refused := r.finished(jobs.Spec{
		Kind: jobs.KindInstall, NodeID: 4, Target: "third", Steps: []string{"Check the server", "Download the agent", "Discard the node"},
		Install: &jobs.InstallForm{Name: "third", Host: "203.0.113.4", SSHPort: 22, SSHUser: "root", EntryPort: 443},
	}, func(run *jobs.Run) error {
		run.Fail("Check the server", errors.New(xrayHere))
		run.Set("Discard the node", jobs.StepOK, "")
		return &Error{CodeServerHasXray, xrayHere + " Nothing was changed on the server, and nothing was kept."}
	})
	install, run, end := r.begin(jobs.Spec{Kind: jobs.KindInstall, NodeID: 2, Target: "second", Steps: []string{"Check the server", "Download the agent", "Upload the agent", "Start the agent", "Save the node"}})
	defer end(nil)
	run.Set("Check the server", jobs.StepOK, "arm64 · Debian GNU/Linux 12 (bookworm)")
	run.Set("Download the agent", jobs.StepOK, "v1.3.1 · 18 MB")
	run.Set("Upload the agent", jobs.StepRunning, "38% · 6.8 MB sent")

	live.entry, live.xray = wantLiveEntry, "26.10.21"
	// what the manager fills in for a node: it has no source for a round trip, the agent's own uptime and
	// memory, or whether the settings are applied, and those stay as they are here
	live.forNode(1, NodeLive{
		LastSeenAt: at(-1),
		Tunnel:     TunnelState{State: "up", Since: at(-60), Streams: 3, BytesToSubs: 900, BytesFromSubs: 100},
		Agent: &AgentState{
			Version: "1.3.0", XrayVersion: "26.10.21", ReportedAt: *at(-1),
			Xray: XrayState{Running: true, Restarts: 2, UptimeSec: 600},
		},
		Updates: Updates{Agent: true, AgentVersion: "1.3.1", XrayVersion: "26.10.21"},
		Usage:   NodeUsage{Live: true, Online: 1, Connections: 3, RateUp: 10, RateDown: 20, TodayUp: 1, TodayDown: 2, MonthUp: 3, MonthDown: 4, UpdatedAt: at(-1)},
	}).forNode(2, NodeLive{
		Installing: &Installing{JobID: install.ID, Step: 3, Steps: 5, Key: "upload_agent", Label: "Upload the agent", Percent: ptr(38)},
	})
	live.events[1] = []NodeEvent{{At: *at(-1), Kind: "tunnel_up"}, {At: *at(-30), Kind: "agent_updated", Detail: "1.2.0 to 1.3.0"}}
	live.on[[2]uint{1, 1}] = SubscriptionUsage{Live: true, Online: true, Connections: 2, LastSeenAt: at(-1), TodayUp: 5, TodayDown: 6, MonthUp: 7, MonthDown: 8}
	live.total[1] = SubscriptionUsage{Live: true, Online: true, LastSeenAt: at(-1), TodayUp: 5, TodayDown: 6, MonthUp: 7, MonthDown: 8}
	live.on[[2]uint{2, 1}] = SubscriptionUsage{LastSeenAt: at(-600), MonthUp: 1, MonthDown: 2}
	live.total[2] = SubscriptionUsage{LastSeenAt: at(-600), MonthUp: 1, MonthDown: 2}
	live.traffic[1] = Traffic{StepSec: 5, Samples: []TrafficSample{{At: *at(-1), Up: 7, Down: 8}, {At: *at(0), Up: 9, Down: 10}}}

	fixtures := []struct {
		name, typ string
		body      []byte
	}{
		{"config", "AppConfig", r.get("/api/config")},
		{"bareConfig", "AppConfig", bare.get("/api/config")},
		{"nodes", "Node[]", r.get("/api/nodes")},
		{"node", "Node", r.get("/api/nodes/1")},
		{"installingNode", "Node", r.get("/api/nodes/2")},
		{"bareNode", "Node", bare.get("/api/nodes/1")},
		{"unfinishedNode", "Node", bare.get("/api/nodes/2")},
		{"stoppedNode", "Node", r.get("/api/nodes/3")},
		{"subscriptions", "Subscription[]", r.get("/api/subscriptions")},
		{"bareSubscriptions", "Subscription[]", bare.get("/api/subscriptions")},
		{"nodeSubscriptions", "NodeSubscription[]", r.get("/api/nodes/1/subscriptions")},
		{"traffic", "Traffic", r.get("/api/nodes/1/traffic")},
		{"jobs", "Job[]", r.get("/api/jobs")},
	}

	var out strings.Builder
	out.WriteString(`// Generated by "go test ./internal/remotenodes/api -run TestThePagesFixtures -update-fixtures". Do not edit.
//
// What the hub sends for the page's GET calls, as the page's types. A hub with everything
// known (config, nodes, node, installingNode, stoppedNode, subscriptions, nodeSubscriptions,
// traffic, jobs) and one that knows only its state (the bare ones). A field the hub adds or drops, or sends
// as null where a type says otherwise, stops this file compiling, and so the build.

import type { AppConfig, Job, Node, NodeSubscription, Subscription, Traffic } from './api'
`)
	for _, f := range fixtures {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, bytes.TrimSpace(f.body), "", "  "); err != nil {
			t.Fatalf("%s is not JSON: %v\n%s", f.name, err, f.body)
		}
		text := pretty.String()
		// job ids are random: each has a name of its own in the file
		for id, name := range map[string]string{gone.ID: "job-remove", failed.ID: "job-update", install.ID: "job-install", refused.ID: "job-refused"} {
			text = strings.ReplaceAll(text, id, name)
		}
		fmt.Fprintf(&out, "\nexport const %s: %s = %s\n", f.name, f.typ, text)
	}

	want := out.String()
	if *updateFixtures {
		if err := os.WriteFile(fixturesFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile(fixturesFile)
	if err != nil {
		t.Fatalf("the page's fixtures can't be read (%v); write them with: go test ./internal/remotenodes/api -run TestThePagesFixtures -update-fixtures", err)
	}
	if string(have) != want {
		t.Errorf("%s is not what the hub sends now (first difference at line %d).\nIf the change is meant, write it again and commit it:\n  go test ./internal/remotenodes/api -run TestThePagesFixtures -update-fixtures", fixturesFile, firstDifference(string(have), want))
	}
}

// firstDifference is the line number, from 1, where two texts first differ.
func firstDifference(a, b string) int {
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < min(len(x), len(y)); i++ {
		if x[i] != y[i] {
			return i + 1
		}
	}
	return min(len(x), len(y)) + 1
}
