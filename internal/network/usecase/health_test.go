package usecase

import (
	"context"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

type scriptedProbe struct {
	carrier bool
	gateway bool
}

func (s *scriptedProbe) Carrier(context.Context, string) (bool, error) { return s.carrier, nil }
func (s *scriptedProbe) GatewayReachable(context.Context, string, string) (bool, error) {
	return s.gateway, nil
}

func domesticUplink() Uplink {
	return Uplink{IfName: "enp1s0", Table: 201, UplinkIndex: 1,
		Slot: domain.SlotDomestic, GroupIndex: 1}
}

// One failure must not take an uplink down; three consecutive do.
func TestObserve_DampingRequiresConsecutiveFailures(t *testing.T) {
	ctx := context.Background()
	p := &scriptedProbe{carrier: true, gateway: true}
	h := NewHealthMonitor(system.NewFakeBackend(), p, DefaultDamping())
	u := domesticUplink()

	for i := 0; i < DefaultDamping().SuccessesToUp; i++ {
		if _, _, err := h.Observe(ctx, u, "192.168.1.1", ""); err != nil {
			t.Fatal(err)
		}
	}

	p.gateway = false
	for i := 1; i < DefaultDamping().FailuresToDown; i++ {
		up, _, err := h.Observe(ctx, u, "192.168.1.1", "")
		if err != nil {
			t.Fatal(err)
		}
		if !up {
			t.Fatalf("went down after %d failure(s); damping is %d", i, DefaultDamping().FailuresToDown)
		}
	}
	up, changed, err := h.Observe(ctx, u, "192.168.1.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if up || !changed {
		t.Errorf("did not go down on failure %d: up=%v changed=%v",
			DefaultDamping().FailuresToDown, up, changed)
	}
}

// A false down is a total outage, so allow an override.
func TestObserve_ForceStateWins(t *testing.T) {
	ctx := context.Background()
	h := NewHealthMonitor(system.NewFakeBackend(), &scriptedProbe{}, DefaultDamping())
	u := domesticUplink()

	up, _, err := h.Observe(ctx, u, "192.168.1.1", "up")
	if err != nil || !up {
		t.Errorf("force-up ignored: up=%v err=%v", up, err)
	}

	h2 := NewHealthMonitor(system.NewFakeBackend(), &scriptedProbe{carrier: true, gateway: true}, DefaultDamping())
	up, _, err = h2.Observe(ctx, u, "192.168.1.1", "down")
	if err != nil || up {
		t.Errorf("force-down ignored: up=%v err=%v", up, err)
	}
}

// No carrier is decisive.
func TestObserve_NoCarrierIsImmediatelyDown(t *testing.T) {
	ctx := context.Background()
	h := NewHealthMonitor(system.NewFakeBackend(),
		&scriptedProbe{carrier: false, gateway: true}, DefaultDamping())
	up, _, err := h.Observe(ctx, domesticUplink(), "192.168.1.1", "")
	if err != nil {
		t.Fatal(err)
	}
	if up {
		t.Error("an uplink with no carrier reported up")
	}
}

// One route operation: no config write, no rule churn.
func TestApplyRoute_IsExactlyOneRouteOperation(t *testing.T) {
	ctx := context.Background()
	be := system.NewFakeBackend()
	h := NewHealthMonitor(be, &scriptedProbe{}, DefaultDamping())
	u := domesticUplink()

	if err := h.ApplyRoute(ctx, u, "192.168.1.1", true); err != nil {
		t.Fatal(err)
	}
	routes, _ := be.RouteList(ctx, 201)
	if len(routes) != 1 || routes[0].Dest != "default" || routes[0].Gateway != "192.168.1.1" {
		t.Fatalf("healthy state = %+v, want one default via the gateway", routes)
	}
	rulesBefore, _ := be.RuleList(ctx)

	if err := h.ApplyRoute(ctx, u, "192.168.1.1", false); err != nil {
		t.Fatal(err)
	}
	routes, _ = be.RouteList(ctx, 201)
	if len(routes) != 0 {
		t.Errorf("down state left routes in table 201: %+v", routes)
	}
	rulesAfter, _ := be.RuleList(ctx)
	if len(rulesBefore) != len(rulesAfter) {
		t.Error("failover touched the rule set; it must be one route operation")
	}
}

// V29 is the only hard reject: no sysctl fixes two uplinks on one address.
func TestLeaseVerdicts_V29IdenticalAddressesReject(t *testing.T) {
	vs := LeaseVerdicts(
		[]system.Addr{
			{IfName: "enp1s0", CIDR: "192.168.1.34/24"},
			{IfName: "enp2s0", CIDR: "192.168.1.34/24"},
		},
		twoUplinks(), false, "")

	var found bool
	for _, v := range vs {
		if v.Rule == "V29" {
			found = true
			if v.Level != domain.LevelReject {
				t.Errorf("V29 level = %q, want reject", v.Level)
			}
		}
	}
	if !found {
		t.Fatalf("identical uplink addresses accepted: %+v", vs)
	}
}

// V30 warns: rejecting would lock out ISP CPE on 192.168.1.0/24.
func TestLeaseVerdicts_V30OverlappingSubnetsWarnAndNameBoth(t *testing.T) {
	vs := LeaseVerdicts(
		[]system.Addr{
			{IfName: "enp1s0", CIDR: "192.168.1.34/24"},
			{IfName: "enp2s0", CIDR: "192.168.1.35/24"},
		},
		twoUplinks(), false, "")

	var v30 *domain.Verdict
	for i := range vs {
		if vs[i].Rule == "V30" {
			v30 = &vs[i]
		}
	}
	if v30 == nil {
		t.Fatalf("no V30 verdict: %+v", vs)
	}
	if v30.Level != domain.LevelWarn {
		t.Errorf("V30 level = %q, want warn", v30.Level)
	}
	if !strings.Contains(v30.Message, "enp1s0") || !strings.Contains(v30.Message, "enp2s0") {
		t.Errorf("V30 must name both interfaces: %q", v30.Message)
	}
	if domain.Rejected(vs) {
		t.Error("overlapping subnets with distinct addresses must not reject")
	}
}

// V31 names the cause, not the symptom.
func TestLeaseVerdicts_V31DishAnswersButLeaseIsOutsideBypassSpace(t *testing.T) {
	vs := LeaseVerdicts(
		[]system.Addr{{IfName: "enp2s0", CIDR: "192.168.1.50/24"}},
		twoUplinks(), true, "192.168.1.50/24")

	var found bool
	for _, v := range vs {
		if v.Rule == "V31" {
			found = true
			if !strings.Contains(strings.ToLower(v.Message), "bypass") {
				t.Errorf("V31 message must say bypass mode: %q", v.Message)
			}
		}
	}
	if !found {
		t.Fatalf("no V31 warning: %+v", vs)
	}

	// A proper bypass lease must not warn.
	clean := LeaseVerdicts(
		[]system.Addr{{IfName: "enp2s0", CIDR: "100.64.1.9/10"}},
		twoUplinks(), true, "100.64.1.9/10")
	for _, v := range clean {
		if v.Rule == "V31" {
			t.Errorf("V31 fired on a proper bypass lease: %q", v.Message)
		}
	}
}
