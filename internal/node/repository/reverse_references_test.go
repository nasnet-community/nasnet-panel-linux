package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestReverseTagReferencesExactArrayMembership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	// Keep the test isolated from the rest of the panel schema.
	if err = db.Exec(`CREATE TABLE reverse_proxies (id INTEGER PRIMARY KEY, node_id INTEGER, tag TEXT, interconnection_tag TEXT, outbound_tag TEXT, interconnection_tags JSONB, inbound_tags JSONB, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	special := `a_%"\\b`
	if err = db.Exec(`INSERT INTO reverse_proxies (id,node_id,tag,interconnection_tag,outbound_tag,interconnection_tags,inbound_tags) VALUES (1,10,'rp','bridge-out','exit',json_array(?),json_array('public')), (2,11,'other-node','','',json_array(?),json_array('public')), (3,10,'substring','','',json_array('prefix-public-suffix'),json_array('other'))`, special, special).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewNodeRepository(db)
	for _, tag := range []string{"bridge-out", "exit", "public", special} {
		refs, err := repo.ListReverseProxiesByReferencedTag(context.Background(), 10, tag)
		if err != nil || len(refs) != 1 || refs[0].ID != 1 {
			t.Fatalf("%q: refs=%v error=%v", tag, refs, err)
		}
	}
	for _, tag := range []string{"pub", "%", "_", `a_%`} {
		refs, err := repo.ListReverseProxiesByReferencedTag(context.Background(), 10, tag)
		if err != nil || len(refs) != 0 {
			t.Fatalf("partial/wildcard %q matched %v: %v", tag, refs, err)
		}
	}
	// Exercise GORM's actual PostgreSQL SQL generation without a live service.
	capture := &referenceSQLLogger{Interface: logger.Default}
	pg, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: capture})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewNodeRepository(pg).ListReverseProxiesByReferencedTag(context.Background(), 10, special)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(capture.sql, "LIKE") || !strings.Contains(capture.sql, "interconnection_tags @>") || !strings.Contains(capture.sql, "inbound_tags @>") || !strings.Contains(capture.sql, "::jsonb") {
		t.Fatalf("wrong PostgreSQL query: %s", capture.sql)
	}
}

type referenceSQLLogger struct {
	logger.Interface
	sql string
}

func (l *referenceSQLLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	l.sql, _ = fc()
}
