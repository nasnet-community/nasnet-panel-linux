package repository

import (
	"context"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestUpdateNodeLogSettingsPreservesConcurrentNodeFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	if err := db.Exec(`CREATE TABLE nodes (id INTEGER PRIMARY KEY, name TEXT, is_active BOOLEAN, log_level TEXT, log_access TEXT, log_error TEXT, log_dns BOOLEAN, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO nodes (id,name,is_active,log_level,log_access,log_error,log_dns) VALUES (6,'renamed concurrently',false,'warning','/old','/keep',true)`).Error; err != nil {
		t.Fatal(err)
	}
	level, access, dns := "error", "", false
	repo := NewNodeRepository(db)
	if err := repo.UpdateNodeLogSettings(context.Background(), 6, domain.XrayLogSettings{Level: &level, Access: &access, DNS: &dns}); err != nil {
		t.Fatal(err)
	}
	var node domain.Node
	if err := db.First(&node, 6).Error; err != nil {
		t.Fatal(err)
	}
	if node.Name != "renamed concurrently" || node.IsActive || node.LogLevel != "error" || node.LogAccess != "" || node.LogError != "/keep" || node.LogDNS {
		t.Fatalf("partial update lost unrelated or zero-valued settings: %+v", node)
	}
}
