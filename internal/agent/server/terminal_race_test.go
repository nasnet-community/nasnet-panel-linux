package server

import (
	"sync"
	"sync/atomic"
	"testing"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// fakeTermStream counts concurrent Send calls; if two run at the same
// moment the test fails under -race.
type fakeTermStream struct {
	pb.NodeAgent_OpenTerminalServer
	inFlight int32
	maxSeen  int32
	sendMu   sync.Mutex // serialized by sendTerminal() in terminal.go
}

func (f *fakeTermStream) Send(_ *pb.TerminalOutput) error {
	n := atomic.AddInt32(&f.inFlight, 1)
	if n > atomic.LoadInt32(&f.maxSeen) {
		atomic.StoreInt32(&f.maxSeen, n)
	}
	atomic.AddInt32(&f.inFlight, -1)
	return nil
}

// TestTerminalSendSerialization asserts that the helper sendTerminal()
// exposed by terminal.go prevents two concurrent Send calls.
func TestTerminalSendSerialization(t *testing.T) {
	s := &fakeTermStream{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sendTerminal(s, &s.sendMu, &pb.TerminalOutput{})
		}()
	}
	wg.Wait()
	if max := atomic.LoadInt32(&s.maxSeen); max > 1 {
		t.Fatalf("observed %d concurrent Send calls; expected 1", max)
	}
}
