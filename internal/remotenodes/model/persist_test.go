// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fill gives every exported field a value of its own, so a field the file
// form drops shows up as a difference.
func fill(v reflect.Value, n *int) {
	*n++
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(t0.Add(time.Duration(*n) * time.Second)))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fill(v.Field(i), n)
			}
		}
	case reflect.String:
		v.SetString(fmt.Sprintf("value-%d", *n))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(*n))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(*n))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0), n)
		v.Set(s)
	default:
		panic("fill: no rule for " + v.Kind().String())
	}
}

// Every field has to reach the file and come back: the file is the only copy
// of the CA's users, keys and links, so a field with a json:"-" or a missing
// tag would lose data on the next restart.
func TestEveryFieldSurvivesTheFile(t *testing.T) {
	var s State
	fill(reflect.ValueOf(&s).Elem(), new(int))
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("the file lost something:\n got %+v\nwant %+v", back, s)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	s := small(t)
	s.Nodes[0].RealityPrivateKey = "private-key-material"
	s.Nodes[0].SSHHostKey = "SHA256:pinned"
	s.Nodes[0].BridgeUUID = "11111111-2222-4333-8444-666666666666"
	s.LastBackupAt = t0
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(b, []byte("}\n")) {
		t.Fatalf("the file should end in a newline: %q", b[len(b)-3:])
	}
	// the file is the one place the secrets live
	for _, want := range []string{"private-key-material", "SHA256:pinned", uuidA, "11111111-2222-4333-8444-666666666666"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("the file does not hold %q", want)
		}
	}
	// the second node has no bridge client yet, and says nothing of one
	if n := bytes.Count(b, []byte(`"bridge_uuid"`)); n != 1 {
		t.Errorf("the file has %d bridge ids, want 1", n)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round trip changed the state:\n got %+v\nwant %+v", back, s)
	}
	again, _ := Encode(back)
	if !bytes.Equal(b, again) {
		t.Fatal("encoding twice gave different bytes")
	}
}

func TestEncodeWritesEmptyListsNotNull(t *testing.T) {
	b, err := Encode(NewState())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"nodes": []`, `"subscriptions": []`, `"enablements": []`, `"local_clients": []`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("missing %s in\n%s", key, b)
		}
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("a null in\n%s", b)
	}
	back, err := Decode(b)
	if err != nil || back.Version != SchemaVersion || back.NextNodeID != 1 {
		t.Fatalf("an empty state: %+v, %v", back, err)
	}
}

// A file written before the router had an entry of its own is version 1 and
// says nothing of one. It loads with the entry off, and is version 2 from the
// next write on.
func TestAnOlderFileLoadsWithTheEntryOff(t *testing.T) {
	want := small(t)
	b, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	doc["version"] = 1
	delete(doc, "local")
	delete(doc, "local_clients")
	old, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Decode(old)
	if err != nil {
		t.Fatalf("a version 1 file: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a version 1 file came back as\n%+v\nwant\n%+v", got, want)
	}
	if got.Version != SchemaVersion || got.Local != (Local{}) || got.LocalClients != nil {
		t.Fatalf("version %d, entry %+v, clients %v", got.Version, got.Local, got.LocalClients)
	}
	again, err := Encode(got)
	if err != nil || !bytes.Equal(again, b) {
		t.Fatalf("written again it is not this version's file: %v", err)
	}
}

// The router's entry and its clients go through the file like the rest, the
// private key included: the file is the only copy.
func TestTheRoutersEntrySurvivesTheFile(t *testing.T) {
	s := withLocal(t)
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{localPrivate, uuidD} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("the file does not hold %q", want)
		}
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, back) {
		t.Fatalf("round trip changed the state:\n got %+v\nwant %+v", back, s)
	}
}

func TestDecodeAnswersWhatItCannotUse(t *testing.T) {
	good, _ := Encode(small(t))
	cases := []struct {
		name  string
		in    string
		newer bool
	}{
		{"truncated", string(good[:len(good)/2]), false},
		{"empty", "", false},
		{"not json", "state", false},
		{"null", "null", false},
		{"an array", "[]", false},
		{"no version", `{"nodes": []}`, false},
		{"version zero", `{"version": 0}`, false},
		{"version as text", `{"version": "1"}`, false},
		{"nodes of the wrong type", `{"version": 1, "nodes": {"a": 1}}`, false},
		{"counters missing", `{"version": 1}`, false},
		{"valid json, broken contents", strings.Replace(string(good), `"ssh_port": 22`, `"ssh_port": 0`, 1), false},
		{"a newer version", `{"version": 3}`, true},
		{"a newer shape", `{"version": 3, "nodes": {"a": 1}}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Decode([]byte(c.in))
			if err == nil {
				t.Fatal("accepted")
			}
			if errors.Is(err, ErrNewer) != c.newer {
				t.Fatalf("ErrNewer = %v, want %v (err: %v)", errors.Is(err, ErrNewer), c.newer, err)
			}
		})
	}
	if _, err := Decode(good); err != nil {
		t.Fatalf("the good one: %v", err)
	}
}

// No job outlives the hub, so an install a file holds as running stopped
// when that hub did. It comes back as an unfinished install, which says it
// can't tell why; one that already says why keeps its words, and a node
// that is set up is untouched.
func TestAnInstallTheFileHoldsAsRunningComesBackUnfinished(t *testing.T) {
	s := small(t)
	s.Nodes[0].State = NodeInstalling
	kept := InstallFailure{Step: "Upload the agent", StepKey: "upload_agent", Error: "The connection broke.", At: t0}
	s.Nodes[1].State, s.Nodes[1].Failure = NodeInstalling, kept
	n, err := s.AddNode(goodNode("set up", "203.0.113.3"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Nodes[0]; !got.Unfinished() || got.Failure.Error != unknownFailure {
		t.Errorf("the running install came back as %+v", got)
	}
	if got := back.Nodes[1].Failure; got != kept {
		t.Errorf("the failure on record became %+v", got)
	}
	if got := *back.FindNode(n.ID); got.Unfinished() || got.Failure != (InstallFailure{}) {
		t.Errorf("a set-up node came back as %+v", got)
	}
	if back.NodeCount() != 1 {
		t.Errorf("%d nodes count", back.NodeCount())
	}
}
