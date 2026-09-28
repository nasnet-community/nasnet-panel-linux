package hub

import (
	"sync"

	hubpb "github.com/nasnet-community/nasnet-panel-linux/pkg/hub/pb"
)

// terminalSessionStream lets the browser-side owner release an individual
// reverse terminal RPC without closing the agent's shared gRPC connection.
type terminalSessionStream struct {
	hubpb.AgentHub_TerminalChannelServer
	done      chan struct{}
	closeOnce sync.Once
}

func (s *terminalSessionStream) Close() {
	s.closeOnce.Do(func() { close(s.done) })
}
