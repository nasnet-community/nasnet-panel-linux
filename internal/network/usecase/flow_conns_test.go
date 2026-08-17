package usecase

import (
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func newConnsFixture(t *testing.T, flows []system.CTFlow) *networkUsecase {
	t.Helper()
	u := newFlowFixture(t, flowOpts{vpnActive: true, wgFresh: true})
	u.Flow = &system.FakeFlowSource{Flows: flows}
	return u
}

func TestFlowConnsDecodesMarksAndSorts(t *testing.T) {
	u := newConnsFixture(t, []system.CTFlow{
		{Proto: "tcp", SrcIP: "10.77.0.101", DstIP: "142.250.185.78", SrcPort: 50000, DstPort: 443,
			Mark: 0x2020000, OrigBytes: 900, ReplyBytes: 100},
		{Proto: "udp", SrcIP: "10.77.0.102", DstIP: "5.144.128.1", SrcPort: 40000, DstPort: 53,
			Mark: 0x1010000, OrigBytes: 5000, ReplyBytes: 5000},
	})
	v, err := u.FlowConns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if v.Total != 2 || len(v.Flows) != 2 {
		t.Fatalf("%+v", v)
	}
	// Biggest first: the flow worth looking at is the one moving data.
	if v.Flows[0].Group != "domestic" || v.Flows[0].Pin != 1 {
		t.Fatalf("%+v", v.Flows[0])
	}
	if v.Flows[1].Group != "foreign" || v.Flows[1].Src != "10.77.0.101:50000" {
		t.Fatalf("%+v", v.Flows[1])
	}
	if v.Flows[1].Dst != "142.250.185.78:443" {
		t.Fatalf("dst %q", v.Flows[1].Dst)
	}
}

func TestFlowConnsCapsAtLimit(t *testing.T) {
	flows := make([]system.CTFlow, 250)
	for i := range flows {
		flows[i] = system.CTFlow{Proto: "tcp", SrcIP: "10.77.0.1", DstIP: "8.8.8.8",
			OrigBytes: uint64(i)}
	}
	v, err := newConnsFixture(t, flows).FlowConns(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Flows) != maxFlowConns || v.Total != 250 {
		t.Fatalf("len %d total %d", len(v.Flows), v.Total)
	}
}

func TestRecentNetworkEventsIsEmptyWithoutARecorder(t *testing.T) {
	u := newConnsFixture(t, nil)
	got, err := u.RecentNetworkEvents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want an empty slice, got %v", got)
	}
}

func TestRecentNetworkEventsReadsTheRecorder(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	rec := events.NewRecorder(bus, "flow-test", 10, events.IsNetworkEvent)

	u := newConnsFixture(t, nil)
	u.Events = rec
	bus.Publish(events.Event{Type: events.EventVPNUp, Payload: map[string]any{"profile": "berlin"}})

	for range 200 {
		if got, _ := u.RecentNetworkEvents(t.Context()); len(got) == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the published event never reached the recorder")
}
