package sshcore

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
)

// Device-limit strategies when an account exceeds its concurrent-device cap.
const (
	strategyAccept = "accept" // evict the oldest device to admit the new one (default)
	strategyReject = "reject" // deny the new device
)

type acct struct {
	up   atomic.Int64
	down atomic.Int64
}

type sessInfo struct {
	id    uint64
	email string
	// username and secret are the credentials the session logged in with,
	// so a push that changes or removes them can close it.
	username string
	secret   string
	srcIP    string
	inbound  string // core_inbound id this session belongs to
	since    time.Time
	account  *acct  // shared per-email byte counters (== registry.counters[email])
	closeFn  func() // force-close the underlying connection (eviction / kill)
}

// sessionRegistry tracks live SSH sessions per account, enforces the
// distinct-source-IP device limit, and accumulates per-account traffic.
type sessionRegistry struct {
	mu       sync.Mutex
	nextID   uint64
	counters map[string]*acct                // email -> byte counters
	live     map[string]map[uint64]*sessInfo // email -> session id -> session
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		counters: make(map[string]*acct),
		live:     make(map[string]map[uint64]*sessInfo),
	}
}

func effectiveK(k int) int {
	if k <= 0 {
		return 1
	}
	return k
}

func distinctIPs(sessions map[uint64]*sessInfo) map[string]bool {
	ips := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		ips[s.srcIP] = true
	}
	return ips
}

func oldestSession(sessions map[uint64]*sessInfo) *sessInfo {
	var oldest *sessInfo
	for _, s := range sessions {
		if oldest == nil || s.since.Before(oldest.since) {
			oldest = s
		}
	}
	return oldest
}

// admit atomically decides whether a new connection from srcIP for email is
// allowed under the device limit, and if so reserves+registers a fully
// initialised session for it. inboundID and closeFn are set here — before the
// session is published into r.live — so the session is immutable once other
// goroutines (killDisabledLocked, teardownLocked) can observe it; the caller
// must not mutate the returned session. It returns the new session, any
// sessions it evicted to make room (already removed from the registry; caller
// must close them), and whether the login is admitted. A connection from an IP
// already present is treated as the same device and always admitted without
// counting against the limit.
func (r *sessionRegistry) admit(login account, srcIP, inboundID string, strategy string, closeFn func()) (*sessInfo, []*sessInfo, bool) {
	email, kLimit := login.email, login.kLimit
	r.mu.Lock()
	defer r.mu.Unlock()

	k := effectiveK(kLimit)
	sessions := r.live[email]
	ips := distinctIPs(sessions)

	var evict []*sessInfo
	if !ips[srcIP] && len(ips) >= k {
		if strategy == strategyReject {
			return nil, nil, false
		}
		// A device is a source IP: evict whole devices, oldest first, until
		// the new one fits. Evicting one session would leave its IP's other
		// sessions counting against the limit.
		for len(ips) >= k {
			o := oldestSession(sessions)
			if o == nil {
				break
			}
			for id, s := range sessions {
				if s.srcIP == o.srcIP {
					evict = append(evict, s)
					delete(sessions, id)
				}
			}
			ips = distinctIPs(sessions)
		}
	}

	r.nextID++
	if r.counters[email] == nil {
		r.counters[email] = &acct{}
	}
	s := &sessInfo{
		id:       r.nextID,
		email:    email,
		username: login.username,
		secret:   login.password,
		srcIP:    srcIP,
		inbound:  inboundID,
		since:    time.Now(),
		// Cache the shared per-email counter so the byte-copy hot path
		// (Core.copyMetered) tallies with a lockless atomic add instead of
		// taking the registry mutex on every 32 KiB chunk.
		account: r.counters[email],
		closeFn: closeFn,
	}
	if r.live[email] == nil {
		r.live[email] = make(map[uint64]*sessInfo)
	}
	r.live[email][s.id] = s
	return s, evict, true
}

// unregister removes a session when its connection closes.
func (r *sessionRegistry) unregister(email string, id uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.live[email]; m != nil {
		delete(m, id)
		if len(m) == 0 {
			delete(r.live, email)
		}
	}
}

// collect returns and resets accumulated per-account traffic (read-and-reset).
func (r *sessionRegistry) collect() *traffic.XrayStatsSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := &traffic.XrayStatsSnapshot{
		UserUplink:   make(map[string]int64),
		UserDownlink: make(map[string]int64),
	}
	for email, c := range r.counters {
		up := c.up.Swap(0)
		down := c.down.Swap(0)
		if up != 0 {
			snap.UserUplink[email] = up
			snap.TotalUplink += up
		}
		if down != 0 {
			snap.UserDownlink[email] = down
			snap.TotalDownlink += down
		}
	}
	return snap
}

// sessionsByEmail returns a shallow copy of the live sessions for enforcement.
func (r *sessionRegistry) sessionsByEmail() map[string][]*sessInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][]*sessInfo, len(r.live))
	for email, m := range r.live {
		list := make([]*sessInfo, 0, len(m))
		for _, s := range m {
			list = append(list, s)
		}
		out[email] = list
	}
	return out
}

// totalLive returns the number of live sessions (for Status).
func (r *sessionRegistry) totalLive() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.live {
		n += len(m)
	}
	return n
}
