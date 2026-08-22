package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

func newVPNDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.VPNProfile{}); err != nil {
		t.Fatal(err)
	}
	// The slot-uniqueness rule is a database constraint, so the test has to
	// carry it too or it proves nothing about production.
	if err := EnsureVPNPoolMigration(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func newVPNRepo(t *testing.T) VPNRepository {
	t.Helper()
	return NewVPNRepository(newVPNDB(t))
}

func makeProfile(name string) *domain.VPNProfile {
	return &domain.VPNProfile{
		Name:   name,
		Type:   domain.VPNTypeWireGuard,
		Config: `{"private_key":"k","address":"10.66.0.2/32"}`,
	}
}

func TestVPNRepository_CreateAndRead(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	p := makeProfile("frankfurt")
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 {
		t.Fatal("no ID assigned")
	}

	got, err := r.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "frankfurt" || got.Config != p.Config || got.Enabled {
		t.Errorf("got %+v", got)
	}
	if got.Weight != 1 {
		t.Errorf("weight = %d, want the default 1", got.Weight)
	}

	list, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("list = %d rows", len(list))
	}
}

func TestSetEnabled_AllocatesLowestFreeSlot(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	for _, n := range []string{"a", "b", "c"} {
		if err := r.Create(ctx, makeProfile(n)); err != nil {
			t.Fatal(err)
		}
	}
	for id := uint(1); id <= 3; id++ {
		if err := r.SetEnabled(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	// b freed slot 1; the next enable must reuse it, not append.
	if err := r.SetEnabled(ctx, 2, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, makeProfile("d")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 4, true); err != nil {
		t.Fatal(err)
	}
	rows, err := r.Enabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]int{}
	for _, p := range rows {
		if p.WGSlot == nil {
			t.Fatalf("%s enabled with no slot", p.Name)
		}
		slots[p.Name] = *p.WGSlot
	}
	if slots["a"] != 0 || slots["c"] != 2 || slots["d"] != 1 {
		t.Fatalf("slots = %v", slots)
	}
}

func TestSetEnabled_RefusesANinthMember(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	for i := 0; i < 9; i++ {
		if err := r.Create(ctx, makeProfile(fmt.Sprintf("p%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	for id := uint(1); id <= 8; id++ {
		if err := r.SetEnabled(ctx, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetEnabled(ctx, 9, true); !errors.Is(err, domain.ErrPoolFull) {
		t.Fatalf("err = %v, want ErrPoolFull", err)
	}
}

func TestSetEnabled_IsIdempotentWhileOn(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	if err := r.Create(ctx, makeProfile("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	// A second enable must not shuffle the slot.
	if err := r.SetEnabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.Enabled(ctx)
	if len(rows) != 1 || rows[0].WGSlot == nil || *rows[0].WGSlot != 0 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestMigration_ConvertsTheActiveRowOnce(t *testing.T) {
	ctx := context.Background()
	db := newVPNDB(t)
	r := NewVPNRepository(db)
	if err := r.Create(ctx, makeProfile("old")); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE vpn_profiles SET active = true WHERE id = 1`)
	if err := EnsureVPNPoolMigration(db); err != nil {
		t.Fatal(err)
	}
	rows, _ := r.Enabled(ctx)
	if len(rows) != 1 || !rows[0].Enabled || rows[0].WGSlot == nil || *rows[0].WGSlot != 0 ||
		rows[0].Weight != 1 || rows[0].Priority != 0 {
		t.Fatalf("migrated rows = %+v", rows)
	}
	// Idempotent: a disable must survive a second run.
	if err := r.SetEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := EnsureVPNPoolMigration(db); err != nil {
		t.Fatal(err)
	}
	if rows, _ := r.Enabled(ctx); len(rows) != 0 {
		t.Fatal("a second migration re-enabled a profile the operator turned off")
	}
}

func TestDelete_RefusesAnEnabledProfile(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	if err := r.Create(ctx, makeProfile("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, 1); !errors.Is(err, domain.ErrProfileActive) {
		t.Fatalf("err = %v, want ErrProfileActive", err)
	}
}

func TestSetRole_ValidatesAndWrites(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	if err := r.Create(ctx, makeProfile("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRole(ctx, 1, 3, 40); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, 1)
	if got.Priority != 3 || got.Weight != 40 {
		t.Fatalf("got %+v", got)
	}
	if err := r.SetRole(ctx, 1, 8, 1); err == nil {
		t.Error("priority 8 accepted")
	}
	if err := r.SetRole(ctx, 1, 0, 0); err == nil {
		t.Error("weight 0 accepted")
	}
	if err := r.SetRole(ctx, 999, 0, 1); !errors.Is(err, domain.ErrProfileNotFound) {
		t.Errorf("err = %v, want ErrProfileNotFound", err)
	}
}

// The rollback path: the enabled set becomes exactly what the snapshot held.
func TestSetPool_RestoresTheExactSet(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	for _, n := range []string{"a", "b", "c"} {
		if err := r.Create(ctx, makeProfile(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SetEnabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 2, true); err != nil {
		t.Fatal(err)
	}
	want, err := r.Enabled(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Drift: a off, c on with a different role.
	if err := r.SetEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 3, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetRole(ctx, 3, 5, 9); err != nil {
		t.Fatal(err)
	}

	if err := r.SetPool(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := r.Enabled(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
		t.Fatalf("restored set = %+v", got)
	}
	if got[0].WGSlot == nil || *got[0].WGSlot != 0 || got[1].WGSlot == nil || *got[1].WGSlot != 1 {
		t.Fatalf("slots came back wrong: %+v", got)
	}
}

// A deleted profile's slot must not block a later enable.
func TestSetEnabled_DeletedProfileFreesItsSlot(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)
	if err := r.Create(ctx, makeProfile("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, makeProfile("b")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled(ctx, 2, true); err != nil {
		t.Fatalf("a deleted profile's slot blocked the next one: %v", err)
	}
}

// An Update with no config used to erase a working profile's keys.
func TestVPNRepository_UpdateRefusesAnEmptyConfig(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	p := makeProfile("a")
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	stored := p.Config

	blank := *p
	blank.Config = ""
	if err := r.Update(ctx, &blank); err == nil {
		t.Fatal("an empty config was written over a stored one")
	}
	got, err := r.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config != stored {
		t.Errorf("config = %q, want the stored %q", got.Config, stored)
	}
}

// A stale list is not a server fault.
func TestVPNRepository_MissingProfileIsNotFound(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	if _, err := r.Get(ctx, 999); !errors.Is(err, domain.ErrProfileNotFound) {
		t.Errorf("Get = %v, want ErrProfileNotFound", err)
	}
	if err := r.Delete(ctx, 999); !errors.Is(err, domain.ErrProfileNotFound) {
		t.Errorf("Delete = %v, want ErrProfileNotFound", err)
	}
	if err := r.SetEnabled(ctx, 999, true); !errors.Is(err, domain.ErrProfileNotFound) {
		t.Errorf("SetEnabled = %v, want ErrProfileNotFound", err)
	}
}

func TestVPNRepository_UpdateWritesNameAndConfig(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	p := makeProfile("a")
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.Name = "renamed"
	p.Config = `{"private_key":"other"}`
	if err := r.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" || got.Config != `{"private_key":"other"}` {
		t.Errorf("got %+v", got)
	}
	// Update must not disturb pool membership.
	if got.Enabled {
		t.Error("update enabled the profile")
	}
}

// Only one row can hold slot 0. Two active rows used to fail the whole
// migration, and the retry failed identically on every boot after it.
func TestMigration_SurvivesTwoActiveRows(t *testing.T) {
	ctx := context.Background()
	db := newVPNDB(t)
	r := NewVPNRepository(db)
	for _, name := range []string{"a", "b"} {
		if err := r.Create(ctx, makeProfile(name)); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`UPDATE vpn_profiles SET active = true`)

	if err := EnsureVPNPoolMigration(db); err != nil {
		t.Fatalf("migration failed on two active rows: %v", err)
	}
	rows, _ := r.Enabled(ctx)
	if len(rows) != 1 || rows[0].WGSlot == nil || *rows[0].WGSlot != 0 {
		t.Fatalf("enabled rows = %+v, want exactly one on slot 0", rows)
	}
	// Nothing is left active, so a second run cannot re-enable anything.
	if err := EnsureVPNPoolMigration(db); err != nil {
		t.Fatal(err)
	}
	if again, _ := r.Enabled(ctx); len(again) != 1 {
		t.Fatalf("second run changed the pool: %+v", again)
	}
}
