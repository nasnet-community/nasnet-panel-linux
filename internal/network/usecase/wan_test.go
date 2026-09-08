package usecase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func wanFixture(t *testing.T) (*networkUsecase, domain.NetworkInterface, *system.FakeBackend) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "wan.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.NetworkInterface{}, &domain.ApplyRecord{}); err != nil {
		t.Fatal(err)
	}
	row := domain.NetworkInterface{Key: "wan", KeyKind: "permaddr", PermMAC: "aa:bb:cc:dd:ee:01", IfName: "eth0", Source: "eth_onboard", Role: domain.RoleUnassigned, Method: domain.MethodDHCP4, Present: true, LearnedGateway: "10.2.0.1"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	be := system.NewFakeBackend()
	u := NewNetworkUsecase(Deps{IfRepo: repository.NewInterfaceRepository(db), ApplyRepo: repository.NewApplyRepository(db), Backend: be, Paths: testPaths(t)}).(*networkUsecase)
	u.applier.Reload = func(context.Context) error { return nil }
	u.applier.Snap.Restart = func(context.Context) error { return nil }
	u.reconfigureWAN = func(ctx context.Context, name string) error {
		m, err := system.ReadMarker(u.Paths)
		if err != nil || m == nil {
			t.Fatalf("WAN disruption happened before recovery was armed: %+v %v", m, err)
		}
		var saved domain.NetworkInterface
		if err := db.First(&saved, row.ID).Error; err != nil {
			return err
		}
		if saved.Method == domain.MethodStatic {
			return be.RouteReplace(ctx, system.Route{Table: 201, Dest: "default", Gateway: saved.StaticGateway, OifName: name, OnLink: saved.GatewayOnLink})
		}
		return nil
	}
	return u, row, be
}

func staticWANRequest(row domain.NetworkInterface) domain.ChangeRequest {
	return domain.ChangeRequest{InterfaceID: row.ID, Role: domain.RoleWAN, Slot: domain.SlotDomestic, Confirmed: true, RequestID: "wan-test", WAN: &domain.WANConfig{
		Method: domain.MethodStatic, StaticAddress: "10.2.0.5/32", StaticGateway: "10.2.0.1", GatewayOnLink: true, DNSMode: "custom", DNSServers: []string{"10.2.0.53", "10.2.0.54"},
	}}
}

func TestWANApplyAssignsStaticDirectlyAndStandaloneRollbackRestoresIntent(t *testing.T) {
	u, row, be := wanFixture(t)
	ctx := context.Background()
	req := staticWANRequest(row)
	oldRoute := system.Route{Table: 201, Dest: "default", Gateway: "10.2.0.1", OifName: "eth0", Metric: 100}
	_ = be.RouteReplace(ctx, oldRoute)
	plan, err := u.Plan(ctx, req)
	if err != nil || domain.Rejected(plan.Verdicts) {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if before, _ := u.IfRepo.GetByKey(ctx, row.Key); before.Method != domain.MethodDHCP4 || before.Role != domain.RoleUnassigned {
		t.Fatal("preview mutated intent")
	}
	applied, err := u.Apply(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := u.IfRepo.GetByKey(ctx, row.Key)
	if got.Role != domain.RoleWAN || got.Method != domain.MethodStatic || got.StaticAddress != req.WAN.StaticAddress || got.DNSServer2 != "10.2.0.54" || got.LearnedGateway != "" {
		t.Fatalf("saved %+v", got)
	}
	content, err := os.ReadFile(filepath.Join(u.Paths.NetworkdDir, "10-nasnet-wan-domestic.network"))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"DHCP=no", "Address=10.2.0.5/32", "GatewayOnLink=yes", "Destination=10.2.0.1/32", "DNS=10.2.0.53", "DNS=10.2.0.54", "Domains=~ir"} {
		if !strings.Contains(string(content), token) {
			t.Errorf("missing %s", token)
		}
	}
	state, err := u.State(ctx)
	if err != nil || state.PendingRequestID != "wan-test" || state.LastApplyPhase != domain.PhaseApplied {
		t.Fatalf("status: %+v %v", state, err)
	}
	// A new Applier is the standalone timer: nothing relies on process memory.
	standalone := system.Applier{Paths: u.Paths, Repo: u.ApplyRepo, Reload: func(context.Context) error { return nil }, Now: func() time.Time { return time.Unix(applied.ConfirmDeadlineUnix+1, 0) }, Snap: &system.Snapshotter{Paths: u.Paths, Backend: be, Restart: func(context.Context) error { return nil }, RestoreInterfaces: func(ctx context.Context, rows []domain.InterfaceIntent) error {
		return repository.RestoreInterfaceIntent(ctx, u.IfRepo.DB(), rows)
	}}}
	did, err := standalone.Rollback(ctx, true)
	if err != nil || !did {
		t.Fatalf("rollback: %v %v", did, err)
	}
	restored, _ := u.IfRepo.GetByKey(ctx, row.Key)
	if restored.Role != row.Role || restored.Method != row.Method || restored.DNSServer != "" || restored.DNSServer2 != "" || restored.GatewayOnLink || restored.LearnedGateway != row.LearnedGateway {
		t.Fatalf("not restored: %+v", restored)
	}
	routes, _ := be.RouteList(ctx, 201)
	if len(routes) != 1 || routes[0].Gateway != oldRoute.Gateway || routes[0].Metric != 100 || routes[0].OnLink {
		t.Fatalf("routes not restored: %+v", routes)
	}
}

func TestWANFailedActivationRestoresSavedSettings(t *testing.T) {
	u, row, _ := wanFixture(t)
	u.reconfigureWAN = func(context.Context, string) error { return errors.New("activation refused") }
	if _, err := u.Apply(context.Background(), staticWANRequest(row)); err == nil {
		t.Fatal("activation error hidden")
	}
	got, _ := u.IfRepo.GetByKey(context.Background(), row.Key)
	if got.Role != row.Role || got.StaticAddress != "" || got.DNSServer2 != "" {
		t.Fatalf("failed apply left saved changes: %+v", got)
	}
	if m, err := system.ReadMarker(u.Paths); err != nil || m != nil {
		t.Fatalf("restored failure stayed armed: %+v %v", m, err)
	}
}

func TestWANStaticToDHCPClearsForeignDefaultsAndSavedGateway(t *testing.T) {
	u, row, be := wanFixture(t)
	ctx := context.Background()
	applied, err := u.Apply(ctx, staticWANRequest(row))
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Confirm(ctx, applied.PlanID); err != nil {
		t.Fatal(err)
	}
	req := domain.ChangeRequest{InterfaceID: row.ID, Role: domain.RoleWAN, Slot: domain.SlotDomestic, Confirmed: true, WAN: &domain.WANConfig{Method: domain.MethodDHCP4, DNSMode: "default"}}
	if _, err := u.Apply(ctx, req); err != nil {
		t.Fatal(err)
	}
	got, _ := u.IfRepo.GetByKey(ctx, row.Key)
	if got.StaticGateway != "" || got.StaticAddress != "" || got.GatewayOnLink || got.LearnedGateway != "" {
		t.Fatalf("stale DHCP state: %+v", got)
	}
	routes, _ := be.RouteList(ctx, 201)
	if len(routes) != 0 {
		t.Fatalf("old static defaults survived: %+v", routes)
	}
	if err := u.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = u.IfRepo.GetByKey(ctx, row.Key)
	if got.Method != domain.MethodStatic || !got.GatewayOnLink {
		t.Fatalf("manual rollback lost static intent: %+v", got)
	}
}

func TestWANOnLinkSurvivesRecoveryAndSiblingFailover(t *testing.T) {
	be := system.NewFakeBackend()
	u := &networkUsecase{Deps: Deps{Backend: be}, health: NewHealthMonitor(be, nil, DampingConfig{})}
	up := Uplink{IfName: "eth0", Table: 201, GatewayOnLink: true}
	if err := u.applyRouteState(context.Background(), up, "10.0.0.1", routeUp); err != nil {
		t.Fatal(err)
	}
	routes, _ := be.RouteList(context.Background(), 201)
	if len(routes) != 1 || !routes[0].OnLink {
		t.Fatalf("recovery lost onlink: %+v", routes)
	}
	if err := u.applyDomesticRoute(context.Background(), domesticRoute{
		Up: Uplink{IfName: "eth1", Table: 211}, ForcedDown: true, Via: viaSibling,
		Sibling: domesticObs{Up: up, Gateway: "10.0.0.1"},
	}); err != nil {
		t.Fatal(err)
	}
	routes, _ = be.RouteList(context.Background(), 211)
	if len(routes) != 1 || !routes[0].OnLink || routes[0].OifName != "eth0" {
		t.Fatalf("sibling failover lost onlink: %+v", routes)
	}
}

func TestWANExpiredConfirmAndStaleRevertCannotDisarmAnotherPlan(t *testing.T) {
	a, _, paths := newApplier(t)
	ctx := context.Background()
	plan := system.Plan{Ops: []system.Op{{Desc: "verify early arm", Do: func(context.Context) error {
		m, err := system.ReadMarker(paths)
		if err != nil || m == nil {
			t.Fatalf("not armed before ops: %+v %v", m, err)
		}
		if _, err := a.Rollback(ctx, false); err == nil {
			t.Fatal("rollback raced an in-progress apply")
		}
		return nil
	}}}}
	rec, err := a.Apply(ctx, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.RollbackPlan(ctx, false, rec.ID+1); err == nil {
		t.Fatal("stale revert was accepted")
	}
	a.Now = func() time.Time { return rec.Deadline.Add(time.Second) }
	if err := a.Confirm(ctx, rec.ID); err == nil {
		t.Fatal("expired confirmation was accepted")
	}
	if m, _ := system.ReadMarker(paths); m == nil {
		t.Fatal("expired confirm disarmed recovery")
	}
	if _, err := a.RollbackPlan(ctx, false, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RollbackPlan(ctx, false, rec.ID); err == nil {
		t.Fatal("no-op revert falsely reported restoration")
	}
}

func TestWANTwoCustomResolversReachLANDNS(t *testing.T) {
	u, row, _ := wanFixture(t)
	if _, err := u.Apply(context.Background(), staticWANRequest(row)); err != nil {
		t.Fatal(err)
	}
	ups, err := u.uplinks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	config := LANDNSConfig(testLAN(), ups, system.DefaultDomesticDNS, nil, DomesticSuffix, false)
	got := system.RenderDNSMasq(config)
	for _, line := range []string{"server=/ir/10.2.0.53@eth0", "server=/ir/10.2.0.54@eth0", "no-resolv"} {
		if !strings.Contains(got, line) {
			t.Fatalf("missing %q in LAN resolver: %s", line, got)
		}
	}
	if strings.Contains(got, "server=10.2.0.53") || strings.Contains(got, "strict-order") {
		t.Fatalf("custom domestic resolvers changed DNS policy: %s", got)
	}
}
