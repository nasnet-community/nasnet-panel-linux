//go:build linux

package usecase

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// Opt-in kernel regression: sudo NASNET_DOMESTIC_DRILL=1 go test
// ./internal/network/usecase -run '^TestDomesticFailoverLinux$' -v.
// The child owns a fresh network namespace; the host's routes never change.
func TestDomesticFailoverLinux(t *testing.T) {
	if os.Getenv("NASNET_DOMESTIC_DRILL") != "1" {
		t.Skip("set NASNET_DOMESTIC_DRILL=1 to run the isolated Linux drill")
	}
	// TestMain removes PATH to keep ordinary unit tests away from host tools.
	t.Setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin")
	if os.Getenv("NASNET_DOMESTIC_DRILL_CHILD") != "1" {
		cmd := exec.Command("unshare", "--net", os.Args[0], "-test.run=^TestDomesticFailoverLinux$", "-test.v")
		cmd.Env = append(os.Environ(), "NASNET_DOMESTIC_DRILL_CHILD=1")
		out, err := cmd.CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatalf("isolated drill: %v", err)
		}
		return
	}
	for _, args := range [][]string{
		{"link", "add", "eth0", "type", "dummy"},
		{"link", "add", "eth2", "type", "dummy"},
		{"link", "set", "eth0", "up"},
		{"link", "set", "eth2", "up"},
		{"address", "add", "192.0.2.2/24", "dev", "eth0", "noprefixroute"},
		{"address", "add", "198.51.100.2/24", "dev", "eth2", "noprefixroute"},
	} {
		if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
	}
	u, prober := twoDomesticFixture(t)
	repo := u.IfRepo.(*flowIfRepo)
	repo.rows = repo.rows[:2]
	repo.rows[0].ForceState = "down"
	u.VPNRepo = &fakeVPNRepo{}
	u.healthCfg = DefaultHealthConfig()
	u.healthCfg.FailoverToVPN = false
	be, err := system.NewNetlinkBackend()
	if err != nil {
		t.Fatal(err)
	}
	u.Backend = be
	u.health = NewHealthMonitor(be, &scriptedProbe{carrier: true, gateway: true}, DefaultDamping())
	ctx := t.Context()
	for _, route := range []system.Route{
		{Table: 201, Dest: "192.0.2.0/24", OifName: "eth0", Scope: "link"},
		{Table: 211, Dest: "198.51.100.0/24", OifName: "eth2", Scope: "link"},
		{Table: 201, Dest: "default", Gateway: "192.0.2.1", OifName: "eth0"},
		{Table: 201, Dest: "default", Gateway: "192.0.2.1", OifName: "eth0", Metric: probeRouteMetric},
		{Table: 211, Dest: "default", Gateway: "198.51.100.1", OifName: "eth2"},
	} {
		if err := be.RouteReplace(ctx, route); err != nil {
			t.Fatal(err)
		}
	}
	ups, err := u.uplinks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReconcileRules(ctx, be, AllRules(flowGroups(), ups, VPNRouteState{})); err != nil {
		t.Fatal(err)
	}

	tick(u, 7)
	route, err := be.RouteGet(ctx, "203.0.113.10", netmark.GroupMark(netmark.GroupDomestic))
	if err != nil || route.OifName != "eth2" {
		t.Fatalf("restart failed to use backup: %+v, %v", route, err)
	}
	view, err := u.TraceFlow(ctx, TraceRequest{Dest: "203.0.113.10", Source: "xray-domestic"})
	if err != nil {
		t.Fatal(err)
	}
	if view.FinalVerdict != "delivered-domestic" || !contains(view.PathNodes, "table-201") {
		t.Fatalf("backup trace: %+v", view)
	}
	for _, step := range view.Steps {
		if step.Verdict == "warn" || step.Verdict == "drop" {
			t.Fatalf("kernel and trace disagree: %+v", step)
		}
	}
	t.Log("saved force-down survived restart; backup route and trace agree")

	repo.rows[0].ForceState = ""
	now := time.Now().Add(DefaultDamping().FailbackDwell + time.Second)
	u.health.Now = func() time.Time { return now }
	tick(u, 7)
	route, err = be.RouteGet(ctx, "203.0.113.10", netmark.GroupMark(netmark.GroupDomestic))
	if err != nil || route.OifName != "eth0" {
		t.Fatalf("primary did not recover: %+v, %v", route, err)
	}
	t.Log("clearing force-down restored the primary route")

	prober.setDown("eth0", true)
	tick(u, 6)
	route, err = be.RouteGet(ctx, "203.0.113.10", netmark.GroupMark(netmark.GroupDomestic))
	if err != nil || route.OifName != "eth2" {
		t.Fatalf("sibling mirror failed: %+v, %v", route, err)
	}
	out, err := exec.Command("ip", "route", "get", "203.0.113.10", "oif", "eth0").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "via 192.0.2.1 dev eth0") {
		t.Fatalf("recovery probes lost their own path: %v: %s", err, out)
	}
	t.Log("sibling failover preserves the primary's recovery-probe route")
}
