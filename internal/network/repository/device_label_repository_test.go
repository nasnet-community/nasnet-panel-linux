package repository

import (
	"context"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// A real database, not a fake: column naming and the unique index are exactly
// what a hand-written fake gets wrong.
func newLabelRepo(t *testing.T) DeviceLabelRepository {
	t.Helper()
	db := newOwnDB(t)
	if err := db.AutoMigrate(&domain.LANDeviceLabel{}); err != nil {
		t.Fatal(err)
	}
	return NewDeviceLabelRepository(db)
}

func TestDeviceLabelRepository_SetAndRead(t *testing.T) {
	ctx := context.Background()
	r := newLabelRepo(t)

	if err := r.Set(ctx, "b8:27:eb:aa:bb:01", "the NAS"); err != nil {
		t.Fatal(err)
	}
	got, err := r.ByMAC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["b8:27:eb:aa:bb:01"] != "the NAS" {
		t.Errorf("got %+v", got)
	}
}

func TestDeviceLabelRepository_SetTwiceUpdatesInPlace(t *testing.T) {
	ctx := context.Background()
	r := newLabelRepo(t)

	for _, l := range []string{"first", "second"} {
		if err := r.Set(ctx, "b8:27:eb:aa:bb:01", l); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ByMAC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["b8:27:eb:aa:bb:01"] != "second" {
		t.Errorf("got %+v, want one row reading \"second\"", got)
	}
}

// The trap this table exists to avoid: with a soft-delete column, the cleared
// row keeps the unique index on mac and the name can never be set again.
func TestDeviceLabelRepository_ClearThenSetAgain(t *testing.T) {
	ctx := context.Background()
	r := newLabelRepo(t)
	const mac = "b8:27:eb:aa:bb:01"

	if err := r.Set(ctx, mac, "the NAS"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, mac, ""); err != nil {
		t.Fatal(err)
	}
	got, err := r.ByMAC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[mac]; ok {
		t.Fatalf("the name survived being cleared: %+v", got)
	}

	if err := r.Set(ctx, mac, "renamed"); err != nil {
		t.Fatalf("could not set a name after clearing one: %v", err)
	}
	got, err = r.ByMAC(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[mac] != "renamed" {
		t.Errorf("got %+v, want the new name", got)
	}
}

func TestDeviceLabelRepository_EmptyIsNotAnError(t *testing.T) {
	got, err := newLabelRepo(t).ByMAC(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}

func TestDeviceLabelRepository_RejectsAnEmptyMAC(t *testing.T) {
	if err := newLabelRepo(t).Set(context.Background(), "", "x"); err == nil {
		t.Error("an empty MAC was accepted")
	}
}
