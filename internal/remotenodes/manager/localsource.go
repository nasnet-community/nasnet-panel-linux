// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// localBucket is how long the entry's traffic is gathered under one
// timestamp, the width of an agent's. The usage collector writes a bucket
// down once a newer one exists, so this is how often the file changes while
// the entry is in use: once an hour.
const localBucket = time.Hour

var errNoEntry = errors.New("the bridge isn't running the router's own entry")

// localSource makes the router's own entry look like a node's agent to the
// usage collector, which counts what an agent holds in dated buckets and
// tells it to drop the ones it has written down.
//
// The bridge only says what each client has moved since it started. Here the
// difference since the last look goes into the bucket of the hour, and the
// buckets wait until the collector acknowledges them. They are in memory:
// what the entry moved in the hour the hub stops in is written at a tidy stop
// (see seal) and lost at a hard one.
type localSource struct {
	now func() time.Time
	// status is the bridge's word on the entry, the time that bridge started,
	// and whether there is one to ask.
	status func() (*bridgespec.LocalStatus, time.Time, bool)

	mu      sync.Mutex
	started time.Time                      // of the bridge the last counts were read from
	last    map[string][2]uint64           // by email: up and down at the last look
	buckets map[int64]map[string][2]uint64 // by timestamp, those not acknowledged yet
	open    int64                          // the bucket being filled, 0 when none
	sealed  bool                           // the open bucket takes no more
	high    int64                          // the highest timestamp used so far
}

// newLocalSource starts with nothing counted. after is the timestamp of the
// newest bucket the collector has counted for the entry (its Through): the
// buckets made here are dated past it, whatever the clock says, since the
// collector takes one dated at or before it for counted already. A router
// has no clock of its own while it is off, and may come back up with the time
// a little behind where it was.
func newLocalSource(now func() time.Time, status func() (*bridgespec.LocalStatus, time.Time, bool), after int64) *localSource {
	return &localSource{now: now, status: status, last: map[string][2]uint64{}, buckets: map[int64]map[string][2]uint64{}, high: after}
}

// GetStatsBundle reads the entry's counts and answers with every bucket still
// held, the way an agent would, and with who is connected.
func (s *localSource) GetStatsBundle(_ context.Context, includeOnlineIPs bool) (*agent.StatsBundle, error) {
	st, started, ok := s.status()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok || st == nil {
		// No bridge runs the entry. What it counted before it stopped is still
		// owed to the collector: the hour is closed so that it is taken, and
		// nobody is connected. Once it has been taken there is nothing to say,
		// which the collector counts as the entry not answering.
		s.sealLocked()
		if !s.pendingLocked() {
			s.buckets, s.open, s.sealed = map[int64]map[string][2]uint64{}, 0, false
			return nil, errNoEntry
		}
		out := &agent.StatsBundle{Buffered: s.recordsLocked()}
		if includeOnlineIPs {
			out.OnlineIPs = map[string]map[string]int64{}
			out.OnlineIPsCollectedAtUnixMs = s.now().UnixMilli()
		}
		return out, nil
	}

	now := s.now()
	if !started.Equal(s.started) {
		// another bridge: its counts start from nothing
		s.started, s.last = started, map[string][2]uint64{}
	}
	for _, c := range st.Clients {
		was := s.last[c.Email]
		up, down := since(c.BytesUp, was[0]), since(c.BytesDown, was[1])
		s.last[c.Email] = [2]uint64{c.BytesUp, c.BytesDown}
		if up == 0 && down == 0 {
			continue
		}
		b := s.bucketLocked(now)
		t := b[c.Email]
		b[c.Email] = [2]uint64{t[0] + up, t[1] + down}
	}

	out := &agent.StatsBundle{Buffered: s.recordsLocked()}
	if includeOnlineIPs {
		out.OnlineIPs = map[string]map[string]int64{}
		out.OnlineIPsCollectedAtUnixMs = now.UnixMilli()
		for _, c := range st.Clients {
			if c.Connections <= 0 {
				continue
			}
			// The collector counts the entries, which for an agent are client
			// addresses. The bridge knows the connections themselves, so there
			// is an entry for each, under its address where it has one.
			conns := map[string]int64{}
			for i := 0; i < c.Connections; i++ {
				key := "connection"
				if len(c.Addrs) > 0 {
					key = c.Addrs[i%len(c.Addrs)]
				}
				conns[key+"#"+strconv.Itoa(i)] = now.UnixMilli()
			}
			out.OnlineIPs[c.Email] = conns
		}
	}
	return out, nil
}

// since is how much a counter grew, or all of it when it went back: a
// counter that is lower than it was started again.
func since(now, was uint64) uint64 {
	if now >= was {
		return now - was
	}
	return now
}

// bucketLocked is the bucket what is counted now goes into. A new one is
// begun when there is none, when the open one has had its hour, and when it
// was sealed. Its timestamp is past every one used before, whatever the clock
// says: the collector takes a timestamp it has seen for counted already, so
// one used twice would lose what the second holds. That is also why they are
// not the hours of the clock.
func (s *localSource) bucketLocked(now time.Time) map[string][2]uint64 {
	ts := now.Unix()
	if s.open != 0 && !s.sealed && ts < s.open+int64(localBucket/time.Second) {
		return s.buckets[s.open]
	}
	if ts <= s.high {
		ts = s.high + 1
	}
	s.open, s.sealed, s.high = ts, false, ts
	s.buckets[ts] = map[string][2]uint64{}
	return s.buckets[ts]
}

func (s *localSource) recordsLocked() *agent.BufferedTrafficStats {
	out := &agent.BufferedTrafficStats{}
	for ts, b := range s.buckets {
		rec := &agent.TrafficRecord{Timestamp: ts, UserUplink: map[string]int64{}, UserDownlink: map[string]int64{}}
		for email, t := range b {
			rec.UserUplink[email], rec.UserDownlink[email] = int64(t[0]), int64(t[1])
		}
		out.Records = append(out.Records, rec)
	}
	return out
}

// AckBufferedTraffic drops the buckets the collector has written down.
func (s *localSource) AckBufferedTraffic(_ context.Context, through int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ts := range s.buckets {
		if ts <= through && ts != s.open {
			delete(s.buckets, ts)
		}
	}
	if s.open != 0 && s.open <= through && s.sealed {
		delete(s.buckets, s.open)
		s.open, s.sealed = 0, false
	}
	return nil
}

// seal closes the bucket being filled and puts an empty one after it, so that
// the collector takes the closed one for complete and writes it down. It is
// for a tidy stop: without it the last hour's counts would go with the hub.
func (s *localSource) seal() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sealLocked()
}

func (s *localSource) sealLocked() {
	if s.open == 0 || s.sealed {
		return
	}
	s.sealed = true
	s.high = s.open + 1
	s.buckets[s.high] = map[string][2]uint64{}
}

// pending is whether it holds counts the collector has not taken yet.
func (s *localSource) pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked()
}

func (s *localSource) pendingLocked() bool {
	for _, b := range s.buckets {
		if len(b) > 0 {
			return true
		}
	}
	return false
}
