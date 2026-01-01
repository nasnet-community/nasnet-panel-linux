package cache

import (
	"testing"
	"time"
)

// resetForTest clears the singleton between cases. The cache is a
// package-level global, so tests that don't reset it cross-contaminate.
func resetForTest() {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()
	onlineUsers.users = make(map[string]time.Time)
	onlineUsers.nodeUsers = make(map[uint]map[string]time.Time)
	onlineUsers.userIPs = make(map[string]map[string]int64)
	onlineUsers.maxAge = 15 * time.Second
}

// TestGhostSessionConsistency: per-node and global online views must agree
// after a ghost observation (bulk sync reports email with empty IPs —
// xray's online counter lingers under XHTTP).
func TestGhostSessionConsistency(t *testing.T) {
	resetForTest()

	const nodeID uint = 42
	const email = "ghost@panel"

	// Live phase: normal sync marks the user online with a real IP.
	SetNodeOnlineUsers(nodeID, []string{email})
	SetOnlineUsers([]string{email})
	SetUserOnlineIPs(email, map[string]int64{"1.2.3.4": time.Now().Unix()})

	if got := GetOnlineCount(); got != 1 {
		t.Fatalf("live phase: GetOnlineCount = %d, want 1", got)
	}
	if got := GetNodeOnlineCount(nodeID); got != 1 {
		t.Fatalf("live phase: GetNodeOnlineCount = %d, want 1", got)
	}
	if !IsOnlineOnNode(email, nodeID) {
		t.Fatalf("live phase: IsOnlineOnNode = false, want true")
	}
	if ips := GetUserOnlineIPs(email); len(ips) != 1 {
		t.Fatalf("live phase: GetUserOnlineIPs len = %d, want 1", len(ips))
	}

	// Ghost phase: the next sync observes the email with empty IPs.
	// Matches node_stats.go's bulk branch: userIPs cleared, node map
	// pruned via ClearNodeOnlineUser. users[email] timestamp is
	// intentionally left (other nodes may still see the email live).
	SetUserOnlineIPs(email, map[string]int64{})
	ClearNodeOnlineUser(nodeID, email)

	if got := GetOnlineCount(); got != 0 {
		t.Errorf("ghost phase: GetOnlineCount = %d, want 0", got)
	}
	if got := GetNodeOnlineCount(nodeID); got != 0 {
		t.Errorf("ghost phase: GetNodeOnlineCount = %d, want 0 (bug: lingers for maxAge)", got)
	}
	if IsOnlineOnNode(email, nodeID) {
		t.Errorf("ghost phase: IsOnlineOnNode = true, want false")
	}
	if ips := GetUserOnlineIPs(email); ips != nil {
		t.Errorf("ghost phase: GetUserOnlineIPs = %v, want nil", ips)
	}
}

// TestGetUserOnlineIPsStaleAfterDropOut: when a user drops out of bulkIPs
// entirely, userIPs isn't cleared until CleanExpired (60s) but users[email]
// ages past maxAge (15s). GetUserOnlineIPs must honor the freshness window.
func TestGetUserOnlineIPsStaleAfterDropOut(t *testing.T) {
	resetForTest()

	const email = "dropped@panel"

	// Seed as if the user was live well in the past (beyond maxAge).
	onlineUsers.mu.Lock()
	past := time.Now().Add(-time.Minute)
	onlineUsers.users[email] = past
	onlineUsers.userIPs[email] = map[string]int64{"5.6.7.8": past.Unix()}
	onlineUsers.mu.Unlock()

	if ips := GetUserOnlineIPs(email); ips != nil {
		t.Errorf("stale entry: GetUserOnlineIPs = %v, want nil", ips)
	}
	if GetOnlineCount() != 0 {
		t.Errorf("stale entry: GetOnlineCount != 0")
	}
}

// TestMultiNodeLiveStaysOnline makes sure ClearNodeOnlineUser on one
// node doesn't knock an email offline on another node where it's still
// live.
func TestMultiNodeLiveStaysOnline(t *testing.T) {
	resetForTest()

	const email = "roaming@panel"
	const nodeMain uint = 1
	const nodeRasp uint = 2

	SetNodeOnlineUsers(nodeMain, []string{email})
	SetOnlineUsers([]string{email})
	SetUserOnlineIPs(email, map[string]int64{"9.9.9.9": time.Now().Unix()})

	// Rasp observes the email as a ghost.
	ClearNodeOnlineUser(nodeRasp, email)

	if !IsOnlineOnNode(email, nodeMain) {
		t.Errorf("main: IsOnlineOnNode should still be true")
	}
	if IsOnlineOnNode(email, nodeRasp) {
		t.Errorf("rasp: IsOnlineOnNode should be false")
	}
	if GetOnlineCount() != 1 {
		t.Errorf("global: GetOnlineCount = %d, want 1", GetOnlineCount())
	}
}
