package repository

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"testing"
)

func TestInterfaceIntentRestoresConfigWithoutRevertingDiscovery(t *testing.T) {
	db, ctx := newDB(t), context.Background()
	row := domain.NetworkInterface{Key: "wan", IfName: "eth0", Role: domain.RoleWAN, Slot: domain.SlotDomestic,
		Method: domain.MethodStatic, StaticAddress: "10.2.0.5/32", StaticGateway: "10.2.0.1", GatewayOnLink: true,
		DNSServer: "10.2.0.53", DNSServer2: "10.2.0.54", DNSDomains: "~ir", LearnedGateway: "10.2.0.2"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	before, err := CaptureInterfaceIntent(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	row.SetWAN(domain.WANConfig{Method: domain.MethodDHCP4, DNSMode: "default"})
	if err := SaveWANIntent(ctx, db, row); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Updates(map[string]any{"Role": domain.RoleUnassigned, "Slot": "", "IfName": "eth2", "Present": false}).Error; err != nil {
		t.Fatal(err)
	}
	if err := RestoreInterfaceIntent(ctx, db, before); err != nil {
		t.Fatal(err)
	}
	var restored domain.NetworkInterface
	if err := db.First(&restored, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Role != domain.RoleWAN || restored.Slot != domain.SlotDomestic || restored.Method != domain.MethodStatic || restored.StaticAddress != "10.2.0.5/32" || !restored.GatewayOnLink || restored.DNSServer2 != "10.2.0.54" || restored.LearnedGateway != "10.2.0.2" {
		t.Fatalf("intent not restored: %+v", restored)
	}
	if restored.IfName != "eth2" || restored.Present {
		t.Fatalf("discovery was overwritten: %+v", restored)
	}
}
