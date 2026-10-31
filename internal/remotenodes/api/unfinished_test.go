// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"testing"
)

// An install that stopped and wasn't cleaned up is no node: its view says why
// it stopped, it is never shown as installing, and the config doesn't count it.
// An install with no job and no failure on record, which only a job that ended
// without settling it leaves, is shown the same way.
func TestAnUnfinishedInstallIsShownForWhatItIs(t *testing.T) {
	live := newFakeLive()
	r := newRigWith(t, "", withLive(live))
	r.populate()
	r.addUnfinished()
	// a live view that still says installing changes nothing for it
	live.forNode(3, NodeLive{Installing: &Installing{JobID: "old", Step: 5, Steps: 15}})

	var n Node
	if err := json.Unmarshal(r.get("/api/nodes/3"), &n); err != nil {
		t.Fatal(err)
	}
	u := n.Unfinished
	if n.Registered || n.Installing != nil || u == nil || u.StepKey != "upload_agent" || u.Step != "Upload the agent" ||
		u.Error != "Couldn't upload the agent: the connection broke." || u.Cleanup == "" || u.At == nil {
		t.Fatalf("view %+v, unfinished %+v", n, u)
	}
	// node 2 is installing with no job behind it: it stopped without saying why
	var two Node
	if err := json.Unmarshal(r.get("/api/nodes/2"), &two); err != nil {
		t.Fatal(err)
	}
	if two.Unfinished == nil || two.Unfinished.Error != stoppedInstall || two.Registered {
		t.Errorf("an install with no job: %+v", two.Unfinished)
	}
	// while its job runs it is an install like any other
	live.forNode(2, NodeLive{Installing: &Installing{JobID: "j", Step: 1, Steps: 15}})
	two = Node{}
	if err := json.Unmarshal(r.get("/api/nodes/2"), &two); err != nil {
		t.Fatal(err)
	}
	if two.Unfinished != nil || two.Installing == nil {
		t.Errorf("an install that runs: %+v, %+v", two.Unfinished, two.Installing)
	}
	// a set-up node never has one
	var one Node
	if err := json.Unmarshal(r.get("/api/nodes/1"), &one); err != nil || one.Unfinished != nil {
		t.Errorf("a set-up node: %+v, %v", one.Unfinished, err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(r.get("/api/config"), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.NodeCount != 2 {
		t.Errorf("nodeCount %d: an unfinished install isn't a node", cfg.NodeCount)
	}
	// and no subscription is offered on it
	var subs []Subscription
	if err := json.Unmarshal(r.get("/api/subscriptions"), &subs); err != nil {
		t.Fatal(err)
	}
	for _, s := range subs {
		for _, sn := range s.Nodes {
			if sn.NodeID != 1 {
				t.Errorf("%s is offered on node %d", s.Name, sn.NodeID)
			}
		}
	}
}
