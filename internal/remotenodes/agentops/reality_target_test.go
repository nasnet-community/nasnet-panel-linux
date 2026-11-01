// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import "testing"

// What a page needs of the error that no target works: the one target that was
// asked for with the node's reason, or, when the node chose among sites, only how
// many it tried.
func TestNoTargetErrorSaysWhichTargetAndWhyOrHowManyWereTried(t *testing.T) {
	why := "It doesn't use TLS 1.3."
	one := &NoTargetError{Results: []TargetResult{{Target: "a.example:443", Check: HandshakeCheck{Error: why}}}}
	if target, reason, tried := one.Failed(); target != "a.example:443" || reason != why || tried != 0 {
		t.Errorf("one target: %q, %q, %d", target, reason, tried)
	}
	many := &NoTargetError{Results: []TargetResult{{Target: "a.example:443"}, {Target: "b.example:443"}, {Target: "c.example:443"}}}
	if target, reason, tried := many.Failed(); target != "" || reason != "" || tried != 3 {
		t.Errorf("three sites: %q, %q, %d", target, reason, tried)
	}
	if target, reason, tried := (&NoTargetError{}).Failed(); target != "" || reason != "" || tried != 0 {
		t.Errorf("no results: %q, %q, %d", target, reason, tried)
	}
}
