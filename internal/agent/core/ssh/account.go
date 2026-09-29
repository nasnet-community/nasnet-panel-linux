// Package sshcore implements the pure-Go SSH protocol core: an in-binary SSH
// server that authenticates against the hub-pushed credential set (no daemon,
// no nftables), enforces device-limit K, accounts traffic in-process, and
// forwards direct-tcpip channels through the filtered egress dialer.
package sshcore

import (
	"crypto/subtle"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/core"
)

type account struct {
	username string
	password string
	email    string
	kLimit   int
	enabled  bool
}

// accountStore holds the current pushed credential set and validates logins.
// The set is hot-swapped on every Apply so credential changes take effect
// without restarting the listener.
type accountStore struct {
	mu     sync.RWMutex
	byUser map[string]account
}

func newAccountStore() *accountStore {
	return &accountStore{byUser: make(map[string]account)}
}

// replace atomically swaps in a new credential set.
func (s *accountStore) replace(users []core.UserSpec) {
	m := make(map[string]account, len(users))
	for _, u := range users {
		m[u.Username] = account{
			username: u.Username,
			password: u.Secret,
			email:    u.Email,
			kLimit:   u.KLimit,
			enabled:  u.Enabled,
		}
	}
	s.mu.Lock()
	s.byUser = m
	s.mu.Unlock()
}

// authenticate validates a username/password using a constant-time comparison
// and rejects disabled accounts.
func (s *accountStore) authenticate(username, password string) (account, bool) {
	s.mu.RLock()
	a, ok := s.byUser[username]
	s.mu.RUnlock()
	if !ok || !a.enabled {
		return account{}, false
	}
	if subtle.ConstantTimeCompare([]byte(a.password), []byte(password)) != 1 {
		return account{}, false
	}
	return a, true
}

// lookup returns the current account for a username (used by the sweeper).
func (s *accountStore) lookup(username string) (account, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.byUser[username]
	return a, ok
}
