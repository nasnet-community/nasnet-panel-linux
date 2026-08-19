package usecase

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

func TestRouteStateUpKeepsGatewayRoute(t *testing.T) {
	got := routeStateFor(routeInputs{
		Slot: domain.SlotDomestic, GatewayUp: true, InternetUp: true,
		FailoverOn: true, VPNUp: true,
	})
	if got != routeUp {
		t.Fatalf("want routeUp, got %v", got)
	}
}

func TestRouteStateDomesticInternetDeadFailsOverWhenVPNUp(t *testing.T) {
	got := routeStateFor(routeInputs{
		Slot: domain.SlotDomestic, GatewayUp: true, InternetUp: false,
		FailoverOn: true, VPNUp: true,
	})
	if got != routeFailover {
		t.Fatalf("want routeFailover, got %v", got)
	}
}

func TestRouteStateFailoverNeedsTheToggleAndTheVPN(t *testing.T) {
	// Internet-dead with a live gateway keeps the route: withdrawing would
	// blind the probe.
	for _, in := range []routeInputs{
		{Slot: domain.SlotDomestic, GatewayUp: true, FailoverOn: false, VPNUp: true},
		{Slot: domain.SlotDomestic, GatewayUp: true, FailoverOn: true, VPNUp: false},
		{Slot: domain.SlotSecondary, GatewayUp: true, FailoverOn: true, VPNUp: true},
	} {
		if got := routeStateFor(in); got != routeUp {
			t.Fatalf("%+v: want routeUp, got %v", in, got)
		}
	}
}

func TestRouteStateOnlyAGatewayDeathWithdraws(t *testing.T) {
	got := routeStateFor(routeInputs{
		Slot: domain.SlotSecondary, GatewayUp: false, InternetUp: false,
		FailoverOn: true, VPNUp: true,
	})
	if got != routeWithdraw {
		t.Fatalf("want routeWithdraw, got %v", got)
	}
}

func TestRouteStateGatewayDeadStillFailsOver(t *testing.T) {
	// wg0 rides the secondary uplink; a dead domestic NIC is the point.
	got := routeStateFor(routeInputs{
		Slot: domain.SlotDomestic, GatewayUp: false, InternetUp: false,
		FailoverOn: true, VPNUp: true,
	})
	if got != routeFailover {
		t.Fatalf("want routeFailover, got %v", got)
	}
}

func TestVerdictLadderCollapsesTopDown(t *testing.T) {
	cases := []struct {
		name                                                string
		force                                               string
		carrier, gwKnown, gwUp, inetUp, inetKnown, degraded bool
		want                                                string
	}{
		{"forced down wins over everything", "down", true, true, true, true, true, false, "forced-down"},
		{"forced up wins too", "up", false, true, false, false, true, false, "forced-up"},
		{"no carrier", "", false, true, true, true, true, false, "no-carrier"},
		{"cold damper is warm-up, not an outage", "", true, false, false, true, true, false, ""},
		{"gateway dead", "", true, true, false, true, true, false, "no-gateway"},
		{"internet dead", "", true, true, true, false, true, false, "no-internet"},
		{"internet unknown falls back to gateway", "", true, true, true, false, false, false, "up"},
		{"degraded", "", true, true, true, true, true, true, "degraded"},
		{"clean", "", true, true, true, true, true, false, "up"},
	}
	for _, c := range cases {
		if got := verdictFor(c.force, c.carrier, c.gwKnown, c.gwUp, c.inetUp, c.inetKnown, c.degraded); got != c.want {
			t.Fatalf("%s: want %q got %q", c.name, c.want, got)
		}
	}
}

func TestSetUplinkForceRejectsGarbage(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	if err := u.SetUplinkForce(context.Background(), "eth0", "sideways"); err == nil {
		t.Fatal("junk state must be rejected")
	}
	if err := u.SetUplinkForce(context.Background(), "nosuch", "up"); err == nil {
		t.Fatal("unknown key must be rejected")
	}
}

func TestSetUplinkForceAcceptsTheThreeStates(t *testing.T) {
	u := healthFixture(t, flowOpts{}, true)
	for _, state := range []string{"up", "down", ""} {
		if err := u.SetUplinkForce(context.Background(), "eth0", state); err != nil {
			t.Fatalf("state %q rejected: %v", state, err)
		}
	}
}

// Found on the VM: failover replaced the only route the domestic probe could
// use, so recovery became unobservable. The gateway path must stay alive.
func TestFailoverKeepsAProbeRouteOutTheRealUplink(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	be := u.Backend.(*system.FakeBackend)
	up := Uplink{IfName: "eth0", Key: "eth0", Table: 201, Slot: domain.SlotDomestic}
	u.health.Observe(context.Background(), up, "192.0.2.1", "up") // mark EverUp

	u.applyRouteState(context.Background(), up, "192.0.2.1", routeFailover)
	routes, _ := be.RouteList(context.Background(), 201)
	var viaTunnel, viaGateway bool
	for _, r := range routes {
		if r.Dest != "default" {
			continue
		}
		if r.OifName == system.WGLinkName {
			viaTunnel = true
		}
		if r.Gateway == "192.0.2.1" && r.Metric > 0 {
			viaGateway = true
		}
	}
	if !viaTunnel {
		t.Fatal("failover did not route into the tunnel")
	}
	if !viaGateway {
		t.Fatal("failover starved the probe: no gateway route left in the table")
	}
}

// The kernel deletes only the lowest-metric default per call, so a withdraw
// after failover must clear the probe helper route too.
func TestWithdrawAfterFailoverClearsBothDefaults(t *testing.T) {
	u := healthFixture(t, flowOpts{vpnActive: true, wgFresh: true}, true)
	be := u.Backend.(*system.FakeBackend)
	up := Uplink{IfName: "eth0", Key: "eth0", Table: 201, Slot: domain.SlotDomestic}
	u.health.Observe(context.Background(), up, "192.0.2.1", "up")

	u.applyRouteState(context.Background(), up, "192.0.2.1", routeFailover)
	u.applyRouteState(context.Background(), up, "192.0.2.1", routeWithdraw)
	routes, _ := be.RouteList(context.Background(), 201)
	for _, r := range routes {
		if r.Dest == "default" {
			t.Fatalf("a default survived the withdraw: %+v", r)
		}
	}
}
