package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

func newVPNRepo(t *testing.T) VPNRepository {
	t.Helper()
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.VPNProfile{}); err != nil {
		t.Fatal(err)
	}
	// The single-active rule is a database constraint, so the test has to carry
	// it too or it proves nothing about production.
	if err := EnsureVPNProfileIndex(db); err != nil {
		t.Fatal(err)
	}
	return NewVPNRepository(db)
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
	if got.Name != "frankfurt" || got.Config != p.Config || got.Active {
		t.Errorf("got %+v", got)
	}

	list, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("list = %d rows", len(list))
	}
}

func TestVPNRepository_NoActiveIsNotAnError(t *testing.T) {
	got, err := newVPNRepo(t).Active(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// Switching profiles has to be one step, or the partial unique index rejects
// the moment both rows are active.
func TestVPNRepository_SetActiveSwitchesAtomically(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	a, b := makeProfile("a"), makeProfile("b")
	if err := r.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, b); err != nil {
		t.Fatal(err)
	}

	if err := r.SetActive(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.SetActive(ctx, b.ID); err != nil {
		t.Fatalf("switching profiles failed: %v", err)
	}

	act, err := r.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if act == nil || act.ID != b.ID {
		t.Fatalf("active = %+v, want b", act)
	}
	// And the old one really let go, rather than the index just hiding it.
	old, err := r.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Active {
		t.Error("the previous profile is still marked active")
	}
}

func TestVPNRepository_ClearActive(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	p := makeProfile("a")
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.SetActive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearActive(ctx); err != nil {
		t.Fatal(err)
	}
	act, err := r.Active(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if act != nil {
		t.Errorf("active = %+v, want nil", act)
	}
}

func TestVPNRepository_SetActiveOnAMissingProfile(t *testing.T) {
	if err := newVPNRepo(t).SetActive(context.Background(), 999); err == nil {
		t.Error("activated a profile that does not exist")
	}
}

// A live tunnel with no row behind it is unrecoverable through the UI.
func TestVPNRepository_RefusesToDeleteTheActiveProfile(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	p := makeProfile("a")
	if err := r.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := r.SetActive(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, p.ID); !errors.Is(err, domain.ErrProfileActive) {
		t.Fatalf("err = %v, want ErrProfileActive", err)
	}
}

// The LANDeviceLabel trap, checked from the other side: the index is on active,
// not on a natural key, so a deleted profile can never block a later one.
func TestVPNRepository_DeletedProfileDoesNotBlockTheNextActive(t *testing.T) {
	ctx := context.Background()
	r := newVPNRepo(t)

	a := makeProfile("a")
	if err := r.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.SetActive(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.ClearActive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	b := makeProfile("b")
	if err := r.Create(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := r.SetActive(ctx, b.ID); err != nil {
		t.Fatalf("a deleted profile blocked the next one: %v", err)
	}
	list, err := r.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Errorf("list = %+v, want only b", list)
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
	// Update must not disturb which profile is active.
	if got.Active {
		t.Error("update activated the profile")
	}
}
