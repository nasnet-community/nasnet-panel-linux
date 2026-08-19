package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// gwRecorder captures what probeOnce persists, on top of the fixed row set.
type gwRecorder struct {
	stubIfRepo
	learned map[uint]string
}

func (g *gwRecorder) GetByRole(context.Context, domain.InterfaceRole) ([]domain.NetworkInterface, error) {
	return g.rows, nil
}

func (g *gwRecorder) SetLearnedGateway(_ context.Context, id uint, gw string) error {
	if g.learned == nil {
		g.learned = map[uint]string{}
	}
	g.learned[id] = gw
	return nil
}

func probeFixture(t *testing.T, reachable bool) (*networkUsecase, *gwRecorder, *system.FakeBackend) {
	t.Helper()
	repo := &gwRecorder{stubIfRepo: stubIfRepo{rows: []domain.NetworkInterface{{
		ID: 1, IfName: "eth0", Role: domain.RoleWAN, Slot: domain.SlotDomestic, Present: true,
	}}}}
	be := system.NewFakeBackend()
	if err := be.RouteReplace(context.Background(), system.Route{
		Table: 201, Dest: "default", Gateway: "10.0.2.2", OifName: "eth0",
	}); err != nil {
		t.Fatal(err)
	}
	// Prober is faked or the internet layer would dial real targets on linux.
	u := &networkUsecase{Deps: Deps{
		IfRepo: repo, Backend: be, Paths: testPaths(t), Prober: &fakeProber{},
	}}
	u.health = NewHealthMonitor(be, &scriptedProbe{carrier: true, gateway: reachable}, DefaultDamping())
	return u, repo, be
}

func defaultsIn(t *testing.T, be *system.FakeBackend, table int) int {
	t.Helper()
	rs, err := be.RouteList(context.Background(), table)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range rs {
		if r.Dest == "default" {
			n++
		}
	}
	return n
}

// Damping needs several successes before an uplink counts as up. Withdrawing the
// route in the meantime is unrecoverable: the gateway lives only in that route.
func TestProbeOnce_KeepsTheRouteUntilTheUplinkHasBeenHealthyOnce(t *testing.T) {
	u, _, be := probeFixture(t, true)

	u.probeOnce(context.Background())
	if got := defaultsIn(t, be, 201); got != 1 {
		t.Fatalf("default route withdrawn before the uplink was ever up (defaults=%d)", got)
	}
}

// An unreachable gateway must not strand a cold uplink either.
func TestProbeOnce_ColdUnreachableUplinkKeepsItsRoute(t *testing.T) {
	u, _, be := probeFixture(t, false)

	for range 5 {
		u.probeOnce(context.Background())
	}
	if got := defaultsIn(t, be, 201); got != 1 {
		t.Fatalf("a never-healthy uplink lost its route (defaults=%d)", got)
	}
}

func TestProbeOnce_RemembersTheDHCPGateway(t *testing.T) {
	u, repo, _ := probeFixture(t, true)

	u.probeOnce(context.Background())
	if got := repo.learned[1]; got != "10.0.2.2" {
		t.Fatalf("learned gateway = %q, want 10.0.2.2 — failover cannot restore the route without it", got)
	}
}

func TestEverUp_FalseUntilTheUplinkComesUp(t *testing.T) {
	be := system.NewFakeBackend()
	h := NewHealthMonitor(be, &scriptedProbe{carrier: true, gateway: true}, DefaultDamping())
	up := Uplink{IfName: "eth0", Table: 201, UplinkIndex: 1,
		Slot: domain.SlotDomestic, GroupIndex: netmark.GroupDomestic}

	if h.EverUp("eth0") {
		t.Fatal("EverUp true before any observation")
	}
	for range DefaultDamping().SuccessesToUp + 1 {
		if _, _, err := h.Observe(context.Background(), up, "10.0.2.2", ""); err != nil {
			t.Fatal(err)
		}
	}
	if !h.EverUp("eth0") {
		t.Error("EverUp still false after the uplink came up")
	}
}
