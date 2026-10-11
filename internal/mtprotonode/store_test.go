package mtprotonode

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/nasnet-community/mtproto-remote-plugin/hubnode"
	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
)

func testStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&LiteNode{}, &UsageBaseline{}); err != nil {
		t.Fatal(err)
	}
	return NewStore(db)
}

func TestStoreRoundTripAndSecretsStayOutOfJSON(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	n := &LiteNode{NodeID: 3, AgentID: "a-1", HubPort: 8443, CapturePort: 9443, SSHPort: 22, SSHUser: "root", HostKey: "SHA256:x",
		RealityPub: "aa", ShortID: "bb", Token: "cc", Disguise: "mirror.example.ir", MaxStreams: 2500}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, 3)
	if err != nil || got.Token != "cc" || got.Disguise != "mirror.example.ir" {
		t.Fatalf("got %+v err=%v", got, err)
	}
	s.UpdateObserved(ctx, 3, "1.2.3", "", time.Now())
	list, _ := s.List(ctx)
	if len(list) != 1 || list[0].AgentVersion != "1.2.3" {
		t.Fatalf("list = %+v", list)
	}
	if _, err := s.Get(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if err := s.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

// Background writers update, never create: a status report that lands after a
// delete must not bring the node back.
func TestBackgroundUpdatesNeverResurrectADeletedNode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Insert(ctx, &LiteNode{NodeID: 5, AgentID: "x", RealityPub: "a", ShortID: "b", Token: "c", Disguise: "d.example.ir"})
	if err := s.UpdateObserved(ctx, 5, "1.0", "3.4.25", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDisguise(ctx, 5, "e.example.ir"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, 5)
	if got.AgentVersion != "1.0" || got.TelemtVersion != "3.4.25" || got.Disguise != "e.example.ir" || got.LastSeenAt == nil {
		t.Fatalf("update lost: %+v", got)
	}
	s.Delete(ctx, 5)
	s.UpdateObserved(ctx, 5, "2.0", "x", time.Now())
	s.UpdateDisguise(ctx, 5, "z.example.ir")
	s.SaveBaseline(ctx, 5, map[string]hubnode.Usage{"late": {Up: 1}})
	if _, err := s.Get(ctx, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a late update resurrected the node: %v", err)
	}
	if b, _ := s.Baseline(ctx, 5); len(b) != 0 {
		t.Fatalf("a late baseline left orphans: %+v", b)
	}
}

func TestBaselinesUpsertAndGoWithTheNode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Insert(ctx, &LiteNode{NodeID: 1, AgentID: "x", RealityPub: "a", ShortID: "b", Token: "c", Disguise: "d.example.ir"})
	s.SaveBaseline(ctx, 1, map[string]hubnode.Usage{"alice": {Up: 10, Down: 20}, "bob": {Up: 1}})
	s.SaveBaseline(ctx, 1, map[string]hubnode.Usage{"alice": {Up: 15, Down: 25}})
	b, err := s.Baseline(ctx, 1)
	if err != nil || b["alice"] != (hubnode.Usage{Up: 15, Down: 25}) || b["bob"] != (hubnode.Usage{Up: 1}) {
		t.Fatalf("baseline = %+v err=%v", b, err)
	}
	if other, _ := s.Baseline(ctx, 2); len(other) != 0 {
		t.Fatalf("another node's baseline leaked: %+v", other)
	}
	s.Delete(ctx, 1)
	if b, _ := s.Baseline(ctx, 1); len(b) != 0 {
		t.Fatalf("baselines survived the node: %+v", b)
	}
	if err := s.SaveBaseline(ctx, 1, nil); err != nil {
		t.Fatalf("empty save: %v", err)
	}
}

func installRecord() *relayops.Record {
	return &relayops.Record{NodeID: 3, AgentID: "a-1", HubPort: 8443, CapturePort: 9443, SSHPort: 22, SSHUser: "root", HostKey: "SHA256:x",
		RealityPub: "aa", ShortID: "bb", Token: "t1", Disguise: "old.example.ir", MaxStreams: 2500, Arch: "amd64"}
}

// A rotate or repair saves a record it read when it started, maybe minutes ago:
// the cover site and versions the manager wrote since must survive it.
func TestRecordSaveKeepsWhatTheManagerOwns(t *testing.T) {
	s := testStore(t)
	rs := NewRecordStore(s)
	ctx := context.Background()
	rs.Installing(3)
	if err := rs.Save(ctx, installRecord()); err != nil {
		t.Fatal(err)
	}
	snap, err := rs.Get(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := time.Now()
	s.UpdateDisguise(ctx, 3, "new.example.ir")
	s.UpdateObserved(ctx, 3, "1.2.3", "3.4.25", seen)
	snap.Token, snap.RealityPub = "t2", "cc"
	if err := rs.Save(ctx, snap); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, 3)
	if got.Token != "t2" || got.RealityPub != "cc" {
		t.Fatalf("the job's credentials were not saved: %+v", got)
	}
	if got.Disguise != "new.example.ir" || got.AgentVersion != "1.2.3" || got.TelemtVersion != "3.4.25" || got.LastSeenAt == nil {
		t.Fatalf("a stale record overwrote the manager's columns: %+v", got)
	}
}

func TestRecordSaveNeverBringsBackADeletedNode(t *testing.T) {
	s := testStore(t)
	rs := NewRecordStore(s)
	ctx := context.Background()
	rs.Installing(3)
	if err := rs.Save(ctx, installRecord()); err != nil {
		t.Fatal(err)
	}
	snap, _ := rs.Get(ctx, 3)
	if err := s.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	_ = rs.Save(ctx, snap) // a rotation finishing after the node was removed
	if _, err := s.Get(ctx, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a late save brought the node back: %v", err)
	}
}

// A pending rotation must outlive a restart, and rows from before the column must still load.
func TestAPendingRotationIsStoredAndOlderRowsStillLoad(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&LiteNode{}, &UsageBaseline{}); err != nil {
		t.Fatal(err)
	}
	// the table as an older panel left it
	if err := db.Migrator().DropColumn(&LiteNode{}, "next_seed"); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO lite_nodes (node_id, agent_id, reality_pub, short_id, token, disguise, created_at, updated_at)
		VALUES (3, 'a-1', 'aa', 'bb', 't1', 'old.example.ir', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&LiteNode{}); err != nil {
		t.Fatal(err)
	}
	cols, _ := db.Migrator().ColumnTypes(&LiteNode{})
	for _, c := range cols {
		if c.Name() == "next_seed" {
			if null, ok := c.Nullable(); ok && !null {
				t.Fatal("next_seed must be nullable")
			}
		}
	}
	s := NewStore(db)
	rs := NewRecordStore(s)
	ctx := context.Background()
	old, err := rs.Get(ctx, 3)
	if err != nil || old.NextSeed != "" || old.Token != "t1" {
		t.Fatalf("an older row: %+v err=%v", old, err)
	}

	seed := strings.Repeat("5e", 32)
	old.NextSeed = seed
	if err := rs.Save(ctx, old); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, 3)
	if got.NextSeed != seed {
		t.Fatalf("the pending rotation was not saved: %+v", got)
	}
	if b, _ := json.Marshal(got); strings.Contains(string(b), seed) {
		t.Fatalf("the seed leaked into JSON: %s", b)
	}
	// the link follows a rotation through the store too
	if err := s.UpdateCreds(ctx, 3, "cc", "dd", "t2", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := rs.Get(ctx, 3); got.NextSeed != "" || got.Token != "t2" {
		t.Fatalf("after the rotation: %+v", got)
	}
}

// A name the column can't hold would fail the save, and with it the booking, on every poll.
func TestBaselinesSkipNamesTheTableCannotHold(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	s.Insert(ctx, &LiteNode{NodeID: 1, AgentID: "x", RealityPub: "a", ShortID: "b", Token: "c", Disguise: "d.example.ir"})
	err := s.SaveBaseline(ctx, 1, map[string]hubnode.Usage{
		"alice": {Up: 1}, strings.Repeat("x", 191): {Up: 2}, "nul\x00": {Up: 3}, "": {Up: 4}, "bad\xffutf8": {Up: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Baseline(ctx, 1)
	if len(b) != 1 || b["alice"].Up != 1 {
		t.Fatalf("baseline = %d rows, want only alice", len(b))
	}
}

// A rotation's record is read when it starts; a push can move the cover meanwhile, and
// another job's read (refused as busy) can land too. Its save must keep the move.
func TestARotationSavingAStaleCoverSiteKeepsTheNewerOne(t *testing.T) {
	s := testStore(t)
	rs := NewRecordStore(s)
	ctx := context.Background()
	rs.Installing(3)
	if err := rs.Save(ctx, installRecord()); err != nil {
		t.Fatal(err)
	}
	rot, _ := rs.Get(ctx, 3)
	s.UpdateDisguise(ctx, 3, "moved.example.ir")
	if _, err := rs.Get(ctx, 3); err != nil { // the refused job's read
		t.Fatal(err)
	}
	rot.Token = "t2"
	if err := rs.Save(ctx, rot); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, 3); got.Disguise != "moved.example.ir" || got.Token != "t2" {
		t.Fatalf("after the rotation: %q, token %q", got.Disguise, got.Token)
	}
}

// A settings change writes only the fields it names, and never creates a row.
func TestUpdateSettingsTouchesOnlyWhatItNames(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	n := &LiteNode{NodeID: 3, AgentID: "a-1", SSHPort: 22, SSHUser: "root", RealityPub: "aa", ShortID: "bb", Token: "cc", Disguise: "mirror.example.ir", MaxStreams: 2500}
	if err := s.Insert(ctx, n); err != nil {
		t.Fatal(err)
	}
	ms, port := 1200, 2222
	if err := s.UpdateSettings(ctx, 3, Settings{MaxStreams: &ms, SSHPort: &port}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, 3)
	if got.MaxStreams != 1200 || got.SSHPort != 2222 || got.SSHUser != "root" || got.Token != "cc" {
		t.Fatalf("row = %+v", got)
	}
	user := "admin"
	if err := s.UpdateSettings(ctx, 3, Settings{SSHUser: &user}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, 3); got.SSHUser != "admin" || got.MaxStreams != 1200 {
		t.Fatalf("row = %+v", got)
	}
	// a stop holds across a repair's rewrite of the install fields
	stop := true
	if err := s.UpdateSettings(ctx, 3, Settings{ProxyStopped: &stop}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, 3)
	got.Token = "rotated"
	if err := s.UpdateInstall(ctx, got); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, 3); !got.ProxyStopped || got.Token != "rotated" || got.SSHUser != "admin" {
		t.Fatalf("after a repair: %+v", got)
	}
	if err := s.UpdateSettings(ctx, 99, Settings{SSHUser: &user}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing node: %v", err)
	}
	if _, err := s.Get(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatal("a settings change created a row")
	}
}
