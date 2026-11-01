// SPDX-License-Identifier: AGPL-3.0-or-later

package agentops

import (
	"strings"
	"testing"
)

// The words people read: the error, the step and the sentence for a target that is
// refused call it a Reality target.
func TestTheRealityTargetIsCalledThatInTheHubsOwnWords(t *testing.T) {
	if StepChooseTarget != "Choose a Reality target" {
		t.Errorf("the step is %q", StepChooseTarget)
	}
	for _, text := range []string{
		(&NoTargetError{Results: []TargetResult{{Target: "a.example:443", Check: HandshakeCheck{Error: "It doesn't use TLS 1.3."}}}}).Error(),
		(&NoTargetError{Results: []TargetResult{{}, {}}}).Error(),
		ErrNoTarget.Error(),
		StepChooseTarget,
	} {
		if !strings.Contains(text, "Reality target") || strings.Contains(strings.ToLower(text), "handshake target") {
			t.Errorf("%q", text)
		}
	}
}
