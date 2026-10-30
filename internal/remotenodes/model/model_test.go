// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

const (
	uuidA = "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5f"
	uuidB = "0b9a8f7e-6d5c-4b3a-9281-706f5e4d3c2b"
	uuidC = "11111111-2222-4333-8444-555555555555"
)

func goodNode(name, host string) Node {
	return Node{Name: name, Host: host, SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: NodeRegistered, CreatedAt: t0}
}

func goodSub(name string) Subscription { return Subscription{Name: name, CreatedAt: t0} }

// small returns a valid state with two nodes and two subscriptions, switched
// on in three places.
func small(t *testing.T) State {
	t.Helper()
	s := NewState()
	for _, h := range []string{"203.0.113.1", "203.0.113.2"} {
		if _, err := s.AddNode(goodNode("node "+h, h)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"ali", "sara"} {
		if _, err := s.AddSubscription(goodSub(n)); err != nil {
			t.Fatal(err)
		}
	}
	s.Enablements = []Enablement{
		{SubscriptionID: 1, NodeID: 1, ClientUUID: uuidA, Enabled: true},
		{SubscriptionID: 1, NodeID: 2, ClientUUID: uuidB, Enabled: false},
		{SubscriptionID: 2, NodeID: 1, ClientUUID: uuidC, Enabled: true},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddNodeAssignsIDsAndStopsAtTheCap(t *testing.T) {
	s := NewState()
	for i := 1; i <= MaxNodes; i++ {
		n, err := s.AddNode(goodNode(fmt.Sprintf("n%d", i), fmt.Sprintf("203.0.113.%d", i)))
		if err != nil || n.ID != uint(i) {
			t.Fatalf("node %d: %+v, %v", i, n, err)
		}
	}
	_, err := s.AddNode(goodNode("fourth", "203.0.113.9"))
	if !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("the fourth node: %v", err)
	}
	if len(s.Nodes) != MaxNodes || s.NextNodeID != MaxNodes+1 {
		t.Fatalf("a refused node moved the state: %d nodes, counter %d", len(s.Nodes), s.NextNodeID)
	}

	// an id is in the node's certificate, so a removed node's id stays used
	if err := s.RemoveNode(2); err != nil {
		t.Fatal(err)
	}
	n, err := s.AddNode(goodNode("again", "203.0.113.10"))
	if err != nil || n.ID != MaxNodes+1 {
		t.Fatalf("after a removal: %+v, %v", n, err)
	}
}

func TestAddNodeRefusesABadNodeWithoutTouchingTheState(t *testing.T) {
	s := NewState()
	bad := goodNode("x", "203.0.113.1")
	bad.SSHPort = 0
	_, err := s.AddNode(bad)
	var inv *InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("a node with no ssh port: %v", err)
	}
	if len(s.Nodes) != 0 || s.NextNodeID != 1 {
		t.Fatalf("the state moved: %+v", s)
	}
}

func TestAddNodeStampsTheTimeInUTCToTheSecond(t *testing.T) {
	s := NewState()
	n := goodNode("x", "203.0.113.1")
	n.CreatedAt = time.Date(2026, 10, 30, 14, 0, 0, 123456789, time.FixedZone("x", 2*3600))
	got, err := s.AddNode(n)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(t0) || got.CreatedAt.Location() != time.UTC || got.CreatedAt.Nanosecond() != 0 {
		t.Fatalf("created at %v", got.CreatedAt)
	}
}

func TestAddSubscriptionStopsAtTheCapAndRefusesTakenNames(t *testing.T) {
	s := NewState()
	for i := 1; i <= MaxSubscriptions; i++ {
		sub, err := s.AddSubscription(goodSub(fmt.Sprintf("sub %d", i)))
		if err != nil || sub.ID != uint(i) {
			t.Fatalf("subscription %d: %+v, %v", i, sub, err)
		}
	}
	if _, err := s.AddSubscription(goodSub("eleventh")); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("the eleventh subscription: %v", err)
	}
	if len(s.Subscriptions) != MaxSubscriptions || s.NextSubscriptionID != MaxSubscriptions+1 {
		t.Fatal("a refused subscription moved the state")
	}

	if err := s.RemoveSubscription(3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(goodSub("SUB 4")); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a name differing only in case: %v", err)
	}
	if _, err := s.AddSubscription(goodSub("sub 3")); err != nil {
		t.Fatalf("a freed name: %v", err)
	}
}

func TestPersianNamesAreOneNameToo(t *testing.T) {
	s := NewState()
	if _, err := s.AddSubscription(goodSub("علی")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscription(goodSub("علی")); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("the same Persian name twice: %v", err)
	}
	if _, err := s.AddSubscription(goodSub("سارا")); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveDropsTheClientsToo(t *testing.T) {
	s := small(t)
	if err := s.RemoveNode(1); err != nil {
		t.Fatal(err)
	}
	if len(s.Enablements) != 1 || s.Enablements[0].NodeID != 2 {
		t.Fatalf("clients left after removing node 1: %+v", s.Enablements)
	}
	if err := s.RemoveSubscription(1); err != nil {
		t.Fatal(err)
	}
	if len(s.Enablements) != 0 {
		t.Fatalf("clients left after removing subscription 1: %+v", s.Enablements)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("removals left a broken state: %v", err)
	}
	if err := s.RemoveNode(9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown node: %v", err)
	}
	if err := s.RemoveSubscription(9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown subscription: %v", err)
	}
}

func TestFind(t *testing.T) {
	s := small(t)
	if n := s.FindNode(2); n == nil || n.Host != "203.0.113.2" {
		t.Fatalf("node 2: %+v", n)
	}
	s.FindNode(2).Name = "renamed"
	if s.Nodes[1].Name != "renamed" {
		t.Fatal("FindNode did not point into the state")
	}
	if s.FindNode(9) != nil || s.FindSubscription(9) != nil || s.FindEnablement(9, 9) != nil {
		t.Fatal("found something that isn't there")
	}
	if e := s.FindEnablement(1, 2); e == nil || e.Enabled {
		t.Fatalf("enablement 1/2: %+v", e)
	}
	if s.FindSubscription(2).Name != "sara" {
		t.Fatal("wrong subscription")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	s := small(t)
	c := s.Clone()
	c.Nodes[0].Name = "changed"
	c.Subscriptions[0].Name = "changed"
	c.Enablements[0].Enabled = false
	c.Nodes = append(c.Nodes, Node{})
	if s.Nodes[0].Name == "changed" || s.Subscriptions[0].Name == "changed" || !s.Enablements[0].Enabled || len(s.Nodes) != 2 {
		t.Fatal("a clone shares memory with its original")
	}
}

// Clone copies slices shallowly: that is only a deep copy while the records
// have nothing inside them to share.
func TestRecordsHoldOnlyValues(t *testing.T) {
	for _, v := range []any{Node{}, Subscription{}, Enablement{}, Settings{}} {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			switch k := typ.Field(i).Type.Kind(); k {
			case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface, reflect.Chan, reflect.Func:
				t.Errorf("%s.%s is a %v: State.Clone would share it between copies", typ.Name(), typ.Field(i).Name, k)
			}
		}
	}
}

func TestValidateCatchesBrokenStates(t *testing.T) {
	cases := map[string]func(*State){
		"wrong version":             func(s *State) { s.Version = 7 },
		"zero node counter":         func(s *State) { s.NextNodeID = 0 },
		"zero subscription counter": func(s *State) { s.NextSubscriptionID = 0 },
		"bad settings":              func(s *State) { s.Settings.AgentPort = 0 },
		"settings ports collide":    func(s *State) { s.Settings.AgentPort = s.Settings.EntryPort },
		"node id used twice":        func(s *State) { s.Nodes[1].ID = 1 },
		"node id not below counter": func(s *State) { s.NextNodeID = 2 },
		"node without id":           func(s *State) { s.Nodes[0].ID = 0 },
		"bad node":                  func(s *State) { s.Nodes[0].Host = "not a host" },
		"subscription id twice":     func(s *State) { s.Subscriptions[1].ID = 1 },
		"subscription id counter":   func(s *State) { s.NextSubscriptionID = 2 },
		"bad subscription":          func(s *State) { s.Subscriptions[0].Name = "" },
		"client of a missing node":  func(s *State) { s.Enablements[0].NodeID = 9 },
		"client of a missing sub":   func(s *State) { s.Enablements[0].SubscriptionID = 9 },
		"client twice on a node":    func(s *State) { s.Enablements[1].NodeID = 1 },
		"client id shared":          func(s *State) { s.Enablements[1].ClientUUID = uuidA },
		"client without a uuid":     func(s *State) { s.Enablements[0].ClientUUID = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := small(t)
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("a broken state was accepted")
			}
		})
	}
}

func TestDuplicateSubscriptionNamesAreNameTaken(t *testing.T) {
	s := small(t)
	s.Subscriptions[1].Name = "ALI"
	if err := s.Validate(); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a rename onto a taken name: %v", err)
	}
}

func TestCheckEditRefusesGrowthPastTheCaps(t *testing.T) {
	before := small(t)
	edit := before.Clone()
	for i := len(edit.Nodes); i < MaxNodes; i++ {
		edit.AddNode(goodNode("x", fmt.Sprintf("203.0.113.%d", 50+i)))
	}
	if err := edit.CheckEdit(before); err != nil {
		t.Fatalf("filling the last slot: %v", err)
	}
	// an edit that skips AddNode still meets the cap
	edit.Nodes = append(edit.Nodes, goodNode("one too many", "203.0.113.99"))
	edit.Nodes[len(edit.Nodes)-1].ID = edit.NextNodeID
	edit.NextNodeID++
	if err := edit.CheckEdit(before); !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("a fourth node: %v", err)
	}

	edit = before.Clone()
	for i := len(edit.Subscriptions); i <= MaxSubscriptions; i++ {
		edit.Subscriptions = append(edit.Subscriptions, Subscription{ID: edit.NextSubscriptionID, Name: fmt.Sprintf("s%d", i), CreatedAt: t0})
		edit.NextSubscriptionID++
	}
	if err := edit.CheckEdit(before); !errors.Is(err, ErrSubscriptionLimit) {
		t.Fatalf("an eleventh subscription: %v", err)
	}
}

func TestCheckEditLetsAnOverfullStateShrinkAndRename(t *testing.T) {
	over := NewState()
	for i := 1; i <= MaxNodes+2; i++ {
		over.Nodes = append(over.Nodes, goodNode(fmt.Sprintf("n%d", i), fmt.Sprintf("203.0.113.%d", i)))
		over.Nodes[i-1].ID = uint(i)
	}
	over.NextNodeID = MaxNodes + 3
	if err := over.Validate(); err != nil {
		t.Fatalf("a state over the cap is still a valid state: %v", err)
	}

	renamed := over.Clone()
	renamed.Nodes[0].Name = "renamed"
	if err := renamed.CheckEdit(over); err != nil {
		t.Fatalf("a rename in an overfull state: %v", err)
	}
	shrunk := over.Clone()
	shrunk.RemoveNode(1)
	if err := shrunk.CheckEdit(over); err != nil {
		t.Fatalf("a removal from an overfull state: %v", err)
	}
	grown := over.Clone()
	n := goodNode("more", "203.0.113.99")
	n.ID = grown.NextNodeID
	grown.NextNodeID++
	grown.Nodes = append(grown.Nodes, n)
	if err := grown.CheckEdit(over); !errors.Is(err, ErrNodeLimit) {
		t.Fatalf("growing an overfull state: %v", err)
	}
}

func TestCheckEditKeepsTheCountersMovingForward(t *testing.T) {
	before := small(t)
	edit := before.Clone()
	edit.RemoveNode(2)
	edit.NextNodeID = 2 // the removed node's id would come round again
	if err := edit.CheckEdit(before); err == nil {
		t.Fatal("a counter turned back and nothing said so")
	}
	edit = before.Clone()
	edit.RemoveSubscription(2)
	edit.NextSubscriptionID = 2
	if err := edit.CheckEdit(before); err == nil {
		t.Fatal("a subscription counter turned back and nothing said so")
	}
}

func TestSecretsAreNeverInErrorMessages(t *testing.T) {
	n := goodNode("x", "203.0.113.1")
	n.ID = 1
	n.RealityPrivateKey = strings.Repeat("k", 200)
	err := n.Validate()
	if err == nil || strings.Contains(err.Error(), "kkkk") {
		t.Fatalf("an over-long private key: %v", err)
	}
}
