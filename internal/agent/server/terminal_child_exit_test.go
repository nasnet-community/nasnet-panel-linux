package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestTerminalExitDoesNotWaitForBackgroundPTYHolder(t *testing.T) {
	dir := t.TempDir()
	childPID := filepath.Join(dir, "child.pid")
	t.Setenv("TERMINAL_TEST_CHILD_PID", childPID)
	shell := filepath.Join(dir, "shell")
	script := `#!/bin/sh
trap '' HUP
/bin/sh -c 'trap "" HUP; echo $$ > "$TERMINAL_TEST_CHILD_PID"; exec sleep 30' &
while [ ! -s "$TERMINAL_TEST_CHILD_PID" ]; do sleep 0.01; done
printf 'shell-finished\n'
exit 7
`
	if err := os.WriteFile(shell, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	stream, cancel, done := openDirectTerminalFixture(t, shell)
	t.Cleanup(func() {
		if data, err := os.ReadFile(childPID); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				if child, err := os.FindProcess(pid); err == nil {
					_ = child.Kill()
				}
			}
		}
	})
	deadline := time.AfterFunc(2*time.Second, cancel)
	defer deadline.Stop()
	var output strings.Builder
	for {
		frame, err := stream.Recv()
		if err != nil {
			t.Fatalf("shell exit was held open by a background PTY holder: %v; output=%q", err, output.String())
		}
		switch p := frame.Payload.(type) {
		case *pb.TerminalOutput_Data:
			output.Write(p.Data)
		case *pb.TerminalOutput_ExitCode:
			if p.ExitCode != 7 || !strings.Contains(output.String(), "shell-finished") {
				t.Fatalf("lost final output or exit status: %d %q", p.ExitCode, output.String())
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("terminal handler did not complete")
			}
			return
		}
	}
}
