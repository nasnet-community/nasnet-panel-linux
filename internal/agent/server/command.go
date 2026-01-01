package server

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
)

// ExecuteCommand runs a shell command on the node and returns the result.
// Security note: this RPC is gated behind mTLS — only the hub can call it.
func (s *Server) ExecuteCommand(ctx context.Context, req *pb.ExecuteCommandRequest) (*pb.ExecuteCommandResponse, error) {
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	logrus.WithField("command", req.Command).WithField("timeout", timeout).Warn("[ExecuteCommand] Running command on node")

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", req.Command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	return &pb.ExecuteCommandResponse{
		Success:  exitCode == 0,
		ExitCode: int32(exitCode),
		Stdout:   truncateStr(stdout.String(), 4096),
		Stderr:   truncateStr(stderr.String(), 4096),
	}, nil
}

// truncateStr returns the first maxLen bytes of s, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
