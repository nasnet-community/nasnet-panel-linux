package domain

import "testing"

func TestRule_ScopedNodeIDs(t *testing.T) {
	// Non-node scopes never decode node IDs, even with a payload present.
	global := &Rule{Scope: ScopeGlobal, ScopeValue: "[1,2,3]"}
	if got := global.ScopedNodeIDs(); got != nil {
		t.Errorf("global scope = %v, want nil", got)
	}

	// node_ids with empty payload is treated as no scope.
	if got := (&Rule{Scope: ScopeNodeIDs}).ScopedNodeIDs(); got != nil {
		t.Errorf("empty payload = %v, want nil", got)
	}

	ids := (&Rule{Scope: ScopeNodeIDs, ScopeValue: "[1,2,3]"}).ScopedNodeIDs()
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 3 {
		t.Errorf("decoded ids = %v, want [1 2 3]", ids)
	}

	// Malformed JSON degrades to nil rather than erroring out.
	if got := (&Rule{Scope: ScopeNodeIDs, ScopeValue: "not-json"}).ScopedNodeIDs(); got != nil {
		t.Errorf("bad json = %v, want nil", got)
	}
}
