package cache

import (
	"testing"
	"time"
)

func TestCachedOnlineSnapshotDoesNotRefreshLastSeen(t *testing.T) {
	resetForTest()
	at := time.Now().Add(-5 * time.Second).Truncate(time.Millisecond)
	users := map[string]map[string]int64{"cached@example": {"1.2.3.4": 1}}
	ApplyNodeOnlineIPSnapshot(1, users, at, at.UnixMilli())
	// The source timestamp, not the repeated RPC's receipt, drives TTL.
	ApplyNodeOnlineIPSnapshot(1, users, time.Now(), at.UnixMilli())
	if !onlineUsers.users["cached@example"].Equal(at) || !onlineUsers.nodeUsers[1]["cached@example"].Equal(at) {
		t.Fatal("reading a cached snapshot refreshed online last_seen")
	}
	onlineUsers.maxAge = time.Second
	if IsOnline("cached@example") || IsOnlineOnNode("cached@example", 1) {
		t.Fatal("expired sample still marked online")
	}
}

func TestOlderOnlineSnapshotCannotReplaceNewerCrossNodeData(t *testing.T) {
	resetForTest()
	at := time.Now().Add(-time.Second)
	ApplyNodeOnlineIPSnapshot(1, map[string]map[string]int64{"a": {"new-ip": 1}}, at, at.UnixMilli())
	older := at.Add(-time.Second)
	ApplyNodeOnlineIPSnapshot(2, map[string]map[string]int64{"a": {}}, older, older.UnixMilli())
	ApplyNodeOnlineIPSnapshot(1, map[string]map[string]int64{"a": {"old-ip": 1}}, older, older.UnixMilli())
	ips := GetUserOnlineIPs("a")
	if len(ips) != 1 || ips["new-ip"] != 1 || !onlineUsers.users["a"].Equal(at) {
		t.Fatalf("older observation replaced newer data: %v", ips)
	}
	if !IsOnlineOnNode("a", 1) {
		t.Fatal("older snapshot cleared newer node presence")
	}
}

func TestFutureOnlineSnapshotKeepsStableClampedTimeAfterCleanup(t *testing.T) {
	resetForTest()
	future := time.Now().Add(time.Hour)
	users := map[string]map[string]int64{"future": {"ip": 1}}
	at := ApplyNodeOnlineIPSnapshot(1, users, future, future.UnixMilli())
	if at.After(time.Now()) {
		t.Fatal("future source timestamp was not clamped")
	}
	onlineUsers.maxAge = time.Nanosecond
	CleanExpired()
	if got := ApplyNodeOnlineIPSnapshot(1, users, time.Now(), future.UnixMilli()); !got.Equal(at) {
		t.Fatal("repeated future source timestamp acquired a newer observation time")
	}
	if IsOnline("future") {
		t.Fatal("repeated future sample revived an expired user")
	}
}

func TestOnlineSnapshotNilAndLegacySemantics(t *testing.T) {
	resetForTest()
	users := map[string]map[string]int64{"legacy": {"ip": 1}}
	before := time.Now()
	ApplyNodeOnlineIPSnapshot(1, users, time.Time{}, 0)
	if onlineUsers.users["legacy"].Before(before) {
		t.Fatal("legacy sample did not use receipt time")
	}
	users["legacy"]["ip"] = 99
	if GetUserOnlineIPs("legacy")["ip"] != 1 {
		t.Fatal("caller mutation changed cached snapshot")
	}
	ApplyNodeOnlineIPSnapshot(1, nil, time.Now(), 100)
	if !IsOnlineOnNode("legacy", 1) {
		t.Fatal("missing snapshot changed cache")
	}
	ApplyNodeOnlineIPSnapshot(1, map[string]map[string]int64{"legacy": {}}, time.Now(), 0)
	if IsOnlineOnNode("legacy", 1) || GetUserOnlineIPs("legacy") != nil {
		t.Fatal("completed empty user map did not clear ghost session")
	}
}
