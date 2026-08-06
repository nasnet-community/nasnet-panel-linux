package repository

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&domain.NetworkInterface{}, &domain.WANGroup{},
		&domain.WANGroupMember{}, &domain.ApplyRecord{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ux_netif_singleton_role
		ON network_interfaces (node_id, role)
		WHERE role IN ('lan','mgmt') AND deleted_at IS NULL`).Error; err != nil {
		t.Fatalf("index: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM network_interfaces")
		db.Exec("DELETE FROM wan_groups")
		db.Exec("DELETE FROM wan_group_members")
		db.Exec("DELETE FROM apply_records")
	})
	return db
}

func TestInterfaceRepository_UpsertIsKeyedNotIDKeyed(t *testing.T) {
	ctx := context.Background()
	r := NewInterfaceRepository(newDB(t))

	in := &domain.NetworkInterface{Key: "aa:bb:cc:dd:ee:01", KeyKind: "permaddr",
		IfName: "enp1s0", Source: "eth_onboard", Present: true}
	if err := r.Upsert(ctx, in); err != nil {
		t.Fatal(err)
	}
	// A replug reports the same key with a different kernel name.
	in2 := &domain.NetworkInterface{Key: "aa:bb:cc:dd:ee:01", KeyKind: "permaddr",
		IfName: "enp1s1", Source: "eth_onboard", Present: true}
	if err := r.Upsert(ctx, in2); err != nil {
		t.Fatal(err)
	}

	all, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("upsert created %d rows for one key", len(all))
	}
	if all[0].IfName != "enp1s1" {
		t.Errorf("IfName = %q, want the new kernel name", all[0].IfName)
	}
}

// A vanished device must keep its row and its role.
func TestInterfaceRepository_MarkAbsentPreservesRole(t *testing.T) {
	ctx := context.Background()
	r := NewInterfaceRepository(newDB(t))
	for _, k := range []string{"k1", "k2"} {
		if err := r.Upsert(ctx, &domain.NetworkInterface{Key: k, IfName: k,
			Role: domain.RoleWAN, Present: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.MarkAbsent(ctx, []string{"k1"}); err != nil {
		t.Fatal(err)
	}

	all, _ := r.List(ctx)
	for _, in := range all {
		switch in.Key {
		case "k1":
			if !in.Present {
				t.Error("k1 was present and got marked absent")
			}
		case "k2":
			if in.Present {
				t.Error("k2 should be absent")
			}
			if in.Role != domain.RoleWAN {
				t.Error("absence must not clear the role — a dongle keeps it across a replug")
			}
			if in.LastSeenAt == nil {
				t.Error("LastSeenAt must be stamped when a device goes absent")
			}
		}
	}
}

// The DB, not app validation, is the enforcement point for singletons.
func TestInterfaceRepository_SingletonRoleIsRejectedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	r := NewInterfaceRepository(db)

	for _, k := range []string{"a", "b"} {
		if err := r.Upsert(ctx, &domain.NetworkInterface{Key: k, IfName: k, Present: true}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := r.List(ctx)

	if err := r.SetRoleTx(ctx, db, rows[0].ID, domain.RoleLAN, domain.SlotNone); err != nil {
		t.Fatalf("first lan assignment: %v", err)
	}
	if err := r.SetRoleTx(ctx, db, rows[1].ID, domain.RoleLAN, domain.SlotNone); err == nil {
		t.Fatal("second lan assignment succeeded; the partial unique index is not doing its job")
	}
}

func TestGroupRepository_EnsureDefaultsIsIdempotent(t *testing.T) {
	ctx := context.Background()
	g := NewGroupRepository(newDB(t))
	for i := 0; i < 3; i++ {
		if err := g.EnsureDefaults(ctx); err != nil {
			t.Fatalf("EnsureDefaults #%d: %v", i, err)
		}
	}
	groups, err := g.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want exactly 2", len(groups))
	}

	dom, err := g.GetByName(ctx, "domestic")
	if err != nil {
		t.Fatal(err)
	}
	if dom.GroupIndex != 1 || dom.RuleBase != 110 || dom.RuleBlackhole != 149 {
		t.Errorf("domestic group = %+v, want index 1, rules 110/149", dom)
	}
	fgn, err := g.GetByName(ctx, "foreign")
	if err != nil {
		t.Fatal(err)
	}
	if fgn.GroupIndex != 2 || fgn.RuleBase != 150 || fgn.RuleBlackhole != 199 {
		t.Errorf("foreign group = %+v, want index 2, rules 150/199", fgn)
	}
}

func TestApplyRepository_LatestAndPhaseTransitions(t *testing.T) {
	ctx := context.Background()
	a := NewApplyRepository(newDB(t))

	rec := &domain.ApplyRecord{Phase: domain.PhasePlanned, Ops: []string{"mv /etc/netplan"}}
	if err := a.Create(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := a.SetPhase(ctx, rec.ID, domain.PhaseApplied, ""); err != nil {
		t.Fatal(err)
	}
	got, err := a.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != domain.PhaseApplied || len(got.Ops) != 1 {
		t.Errorf("latest = %+v", got)
	}

	if _, err := a.LatestConfirmed(ctx); err == nil {
		t.Error("LatestConfirmed returned a record before anything was confirmed")
	}
	if err := a.SetPhase(ctx, rec.ID, domain.PhaseConfirmed, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.LatestConfirmed(ctx); err != nil {
		t.Errorf("LatestConfirmed after confirm: %v", err)
	}
}
