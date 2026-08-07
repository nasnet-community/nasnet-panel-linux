package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

// stubIfRepo serves a fixed row set; renderAll only ever reads.
type stubIfRepo struct{ rows []domain.NetworkInterface }

func (s *stubIfRepo) List(context.Context) ([]domain.NetworkInterface, error) { return s.rows, nil }
func (s *stubIfRepo) GetByKey(context.Context, string) (*domain.NetworkInterface, error) {
	return nil, nil
}
func (s *stubIfRepo) GetByRole(context.Context, domain.InterfaceRole) ([]domain.NetworkInterface, error) {
	return nil, nil
}
func (s *stubIfRepo) GetBySlot(context.Context, domain.UplinkSlot) (*domain.NetworkInterface, error) {
	return nil, nil
}
func (s *stubIfRepo) Upsert(context.Context, *domain.NetworkInterface) error { return nil }
func (s *stubIfRepo) MarkAbsent(context.Context, []string) error             { return nil }
func (s *stubIfRepo) SetHealth(context.Context, uint, bool) error            { return nil }
func (s *stubIfRepo) DB() *gorm.DB                                           { return nil }
func (s *stubIfRepo) SetRoleTx(context.Context, *gorm.DB, uint, domain.InterfaceRole, domain.UplinkSlot) error {
	return nil
}

func renderWith(t *testing.T, rows []domain.NetworkInterface) (*networkUsecase, error) {
	t.Helper()
	p := testPaths(t)
	u := &networkUsecase{Deps: Deps{IfRepo: &stubIfRepo{rows: rows}, Paths: p}}
	return u, u.renderAll(context.Background())
}

func TestRenderAll_RejectsAnUplinkWithNoSlot(t *testing.T) {
	_, err := renderWith(t, []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", PermMAC: "aa:bb:cc:dd:ee:01", Role: domain.RoleWAN},
	})
	if err == nil || !strings.Contains(err.Error(), "no slot") {
		t.Fatalf("a slotless uplink rendered RouteTable=0 instead of failing: %v", err)
	}
}

// Both rows name 10-nasnet-wan-domestic.network, so without the guard the
// second write wins and eth0 ends up with no unit at all.
func TestRenderAll_RejectsTwoUplinksInOneSlot(t *testing.T) {
	_, err := renderWith(t, []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", PermMAC: "aa:bb:cc:dd:ee:01",
			Role: domain.RoleWAN, Slot: domain.SlotDomestic},
		{ID: 2, IfName: "eth1", PermMAC: "aa:bb:cc:dd:ee:02",
			Role: domain.RoleWAN, Slot: domain.SlotDomestic},
	})
	if err == nil || !strings.Contains(err.Error(), "domestic") {
		t.Fatalf("a duplicate slot silently dropped an uplink: %v", err)
	}
}

func TestRenderAll_WritesOneUnitPerSlot(t *testing.T) {
	u, err := renderWith(t, []domain.NetworkInterface{
		{ID: 1, IfName: "eth0", PermMAC: "aa:bb:cc:dd:ee:01",
			Role: domain.RoleWAN, Slot: domain.SlotDomestic, Method: domain.MethodDHCP4},
		{ID: 2, IfName: "eth1", PermMAC: "aa:bb:cc:dd:ee:02",
			Role: domain.RoleWAN, Slot: domain.SlotSecondary, Method: domain.MethodDHCP4},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for slot, table := range map[string]string{"domestic": "201", "secondary": "202"} {
		b, err := os.ReadFile(filepath.Join(u.Paths.NetworkdDir, "10-nasnet-wan-"+slot+".network"))
		if err != nil {
			t.Fatalf("%s unit missing: %v", slot, err)
		}
		if !strings.Contains(string(b), "RouteTable="+table) {
			t.Errorf("%s unit should carry RouteTable=%s:\n%s", slot, table, b)
		}
	}
}
