package cache

import (
	"context"
	"sync"
	"time"
)

// OnlineUsersCache tracks online users with their last seen timestamps
type OnlineUsersCache struct {
	mu        sync.RWMutex
	users     map[string]time.Time          // email -> last seen time
	nodeUsers map[uint]map[string]time.Time // nodeID -> email -> last seen time
	userIPs   map[string]map[string]int64   // email -> {ip -> timestamp}
	maxAge    time.Duration                 // how long to consider a user "online" after last activity
}

// Global instance
var (
	onlineUsers = &OnlineUsersCache{
		users:     make(map[string]time.Time),
		nodeUsers: make(map[uint]map[string]time.Time),
		userIPs:   make(map[string]map[string]int64),
		maxAge:    15 * time.Second, // Consider online for 15 seconds after last activity (3× the 5s stats poll, matches inbound-row threshold)
	}
	cleanupOnce sync.Once
)

// StartCleanup launches a background goroutine that periodically removes expired entries.
// Safe to call multiple times; the goroutine is only started once.
func StartCleanup(ctx context.Context) {
	cleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					CleanExpired()
				}
			}
		}()
	})
}

// SetOnlineUsers updates the cache with currently active users
func SetOnlineUsers(emails []string) {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()

	now := time.Now()
	for _, email := range emails {
		onlineUsers.users[email] = now
	}
}

// SetNodeOnlineUsers updates the cache with active users for a specific node
func SetNodeOnlineUsers(nodeID uint, emails []string) {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()

	if onlineUsers.nodeUsers[nodeID] == nil {
		onlineUsers.nodeUsers[nodeID] = make(map[string]time.Time)
	}

	now := time.Now()
	for _, email := range emails {
		onlineUsers.nodeUsers[nodeID][email] = now
	}
}

// ClearNodeOnlineUser: drop ghost sessions from node-scoped counts so
// per-node counts don't lag global by a full maxAge window.
func ClearNodeOnlineUser(nodeID uint, email string) {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()
	if users, ok := onlineUsers.nodeUsers[nodeID]; ok {
		delete(users, email)
	}
}

// GetOnlineUsers: users with at least one live IP within maxAge. Reads
// userIPs (ghost-filtered) rather than users[] which can leak stale entries.
func GetOnlineUsers() []string {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()

	now := time.Now()
	result := make([]string, 0, len(onlineUsers.userIPs))
	for email := range onlineUsers.userIPs {
		lastSeen, exists := onlineUsers.users[email]
		if !exists || now.Sub(lastSeen) > onlineUsers.maxAge {
			continue
		}
		result = append(result, email)
	}
	return result
}

// GetOnlineCount returns the count of online users
func GetOnlineCount() int {
	return len(GetOnlineUsers())
}

// GetNodeOnlineCount returns the count of online users for a specific node
func GetNodeOnlineCount(nodeID uint) int {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()

	users, exists := onlineUsers.nodeUsers[nodeID]
	if !exists {
		return 0
	}

	now := time.Now()
	count := 0
	for _, lastSeen := range users {
		if now.Sub(lastSeen) <= onlineUsers.maxAge {
			count++
		}
	}
	return count
}

// IsOnline checks if a specific user is online (on any node)
func IsOnline(email string) bool {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()

	lastSeen, exists := onlineUsers.users[email]
	if !exists {
		return false
	}
	return time.Since(lastSeen) <= onlineUsers.maxAge
}

// IsOnlineOnNode checks if a specific user is online on a specific node
func IsOnlineOnNode(email string, nodeID uint) bool {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()

	users, exists := onlineUsers.nodeUsers[nodeID]
	if !exists {
		return false
	}
	lastSeen, exists := users[email]
	if !exists {
		return false
	}
	return time.Since(lastSeen) <= onlineUsers.maxAge
}

// GetUserOnlineCount returns 1 if user is online, 0 otherwise
// This matches the expected interface for session count
func GetUserOnlineCount(email string) int64 {
	if IsOnline(email) {
		return 1
	}
	return 0
}

// CleanExpired removes users who haven't been seen recently
func CleanExpired() {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()

	now := time.Now()
	// Clean global cache
	for email, lastSeen := range onlineUsers.users {
		if now.Sub(lastSeen) > onlineUsers.maxAge {
			delete(onlineUsers.users, email)
			delete(onlineUsers.userIPs, email)
		}
	}

	// Clean node caches
	for nodeID, users := range onlineUsers.nodeUsers {
		for email, lastSeen := range users {
			if now.Sub(lastSeen) > onlineUsers.maxAge {
				delete(users, email)
			}
		}
		if len(users) == 0 {
			delete(onlineUsers.nodeUsers, nodeID)
		}
	}
}

// SetUserOnlineIPs updates the cache with IPs for a specific user
func SetUserOnlineIPs(email string, ips map[string]int64) {
	onlineUsers.mu.Lock()
	defer onlineUsers.mu.Unlock()
	if len(ips) == 0 {
		delete(onlineUsers.userIPs, email)
		return
	}
	onlineUsers.userIPs[email] = ips
}

// GetUserOnlineIPs: nil when not currently online. Matches the dashboard
// rule (users[email] fresh within maxAge AND userIPs entry present).
func GetUserOnlineIPs(email string) map[string]int64 {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()
	lastSeen, exists := onlineUsers.users[email]
	if !exists || time.Since(lastSeen) > onlineUsers.maxAge {
		return nil
	}
	ips, exists := onlineUsers.userIPs[email]
	if !exists {
		return nil
	}
	// Return a copy to avoid concurrent map access
	result := make(map[string]int64, len(ips))
	for ip, ts := range ips {
		result[ip] = ts
	}
	return result
}

// GetAllOnlineIPs returns all cached user IPs (only for users currently online)
func GetAllOnlineIPs() map[string]map[string]int64 {
	onlineUsers.mu.RLock()
	defer onlineUsers.mu.RUnlock()

	now := time.Now()
	result := make(map[string]map[string]int64)
	for email, ips := range onlineUsers.userIPs {
		// Only include users that are still considered online
		lastSeen, exists := onlineUsers.users[email]
		if !exists || now.Sub(lastSeen) > onlineUsers.maxAge {
			continue
		}
		ipCopy := make(map[string]int64, len(ips))
		for ip, ts := range ips {
			ipCopy[ip] = ts
		}
		result[email] = ipCopy
	}
	return result
}
