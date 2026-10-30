package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An agent told to run a profile it doesn't know stops at startup, before it
// opens a port or touches Xray, instead of serving as something else.
func TestAgentStopsAtStartupOnAnUnknownProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte("profile: restricted\nlisten_addr: \"127.0.0.1:0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldPath := configPath
	configPath = path
	t.Cleanup(func() { configPath = oldPath })

	err := run(nil, nil)
	if err == nil {
		t.Fatal("run returned without an error")
	}
	for _, want := range []string{"failed to load config", `unknown profile "restricted"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
