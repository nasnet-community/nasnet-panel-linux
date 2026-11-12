// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Encode is the file's form of s: indented, with empty lists written as []
// rather than null.
func Encode(s State) ([]byte, error) {
	if s.Nodes == nil {
		s.Nodes = []Node{}
	}
	if s.Subscriptions == nil {
		s.Subscriptions = []Subscription{}
	}
	if s.Enablements == nil {
		s.Enablements = []Enablement{}
	}
	if s.LocalClients == nil {
		s.LocalClients = []LocalClient{}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode reads what Encode wrote, by this release or an older one. ErrNewer
// means a newer release wrote it;
// any other error means the contents can't be used as they are. An install
// the file holds as running comes back as an unfinished one: whatever ran it
// is gone.
func Decode(b []byte) (State, error) {
	// the version first: a newer file may differ in ways the full decode would trip on
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return State{}, fmt.Errorf("unreadable state: %w", err)
	}
	switch {
	case head.Version > SchemaVersion:
		return State{}, fmt.Errorf("%w: the file is version %d and this build reads up to %d", ErrNewer, head.Version, SchemaVersion)
	case head.Version < 1:
		return State{}, errors.New("unreadable state: the file has no version")
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("unreadable state: %w", err)
	}
	// An older file lacks only what later versions added, and the zero value
	// of each addition is what it meant: so far, the router's entry off.
	s.Version = SchemaVersion
	if len(s.LocalClients) == 0 {
		s.LocalClients = nil // as a state that never had one, which is what the file's [] stands for
	}
	if err := s.Validate(); err != nil {
		return State{}, err
	}
	s.settleInstalls()
	return s, nil
}
