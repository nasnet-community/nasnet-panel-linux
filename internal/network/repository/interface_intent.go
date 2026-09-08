package repository

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"gorm.io/gorm"
)

// CaptureInterfaceIntent reads only operator intent and the remembered lease.
func CaptureInterfaceIntent(ctx context.Context, db *gorm.DB) ([]domain.InterfaceIntent, error) {
	var rows []domain.InterfaceIntent
	err := db.WithContext(ctx).Model(&domain.NetworkInterface{}).Find(&rows).Error
	return rows, err
}

func RestoreInterfaceIntent(ctx context.Context, db *gorm.DB, rows []domain.InterfaceIntent) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Release singleton roles before swapping them back.
		for _, r := range rows {
			if err := tx.Model(&domain.NetworkInterface{}).Where("id = ?", r.ID).
				Updates(map[string]any{"Role": domain.RoleUnassigned, "Slot": domain.SlotNone}).Error; err != nil {
				return err
			}
		}
		for _, r := range rows {
			if err := tx.Model(&domain.NetworkInterface{}).Where("id = ?", r.ID).
				Updates(map[string]any{"Role": r.Role, "Slot": r.Slot, "MasterID": r.MasterID,
					"Method": r.Method, "StaticAddress": r.StaticAddress, "StaticGateway": r.StaticGateway,
					"GatewayOnLink": r.GatewayOnLink, "DNSServer": r.DNSServer, "DNSServer2": r.DNSServer2,
					"DNSDomains": r.DNSDomains, "LearnedGateway": r.LearnedGateway}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func SaveWANIntent(ctx context.Context, tx *gorm.DB, row domain.NetworkInterface) error {
	return tx.WithContext(ctx).Model(&domain.NetworkInterface{}).Where("id = ?", row.ID).
		Updates(map[string]any{"Method": row.Method, "StaticAddress": row.StaticAddress,
			"StaticGateway": row.StaticGateway, "GatewayOnLink": row.GatewayOnLink,
			"DNSServer": row.DNSServer, "DNSServer2": row.DNSServer2,
			"DNSDomains": row.DNSDomains, "LearnedGateway": row.LearnedGateway}).Error
}
