package embedded

import (
	"context"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

func TestEmbeddedClientCannotPushAnXrayBuild(t *testing.T) {
	err := New(&server.Server{}).PushXrayBinary(context.Background(), "26.11.1", "abc", strings.NewReader("x"), true, false)

	if !agent.IsUnimplemented(err) {
		t.Fatalf("error = %v, want Unimplemented", err)
	}
}
