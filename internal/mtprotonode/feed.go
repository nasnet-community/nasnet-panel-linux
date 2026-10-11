package mtprotonode

import (
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
)

// Event is one thing that happened on an MTProto node, as its activity feed shows it.
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Level   string    `json:"level"`
	Detail  string    `json:"detail,omitempty"`
	Count   int       `json:"count,omitempty"`
	Subject string    `json:"subject,omitempty"`
}

// EventFeed is a node's recent activity, newest first.
type EventFeed struct {
	Since  time.Time `json:"since"` // the feed is kept in memory: nothing before this
	Events []Event   `json:"events"`
}

const (
	kindLinkUp         = "link_up"
	kindLinkDown       = "link_down"
	kindProxyUp        = "proxy_up"
	kindProxyDown      = "proxy_down"
	kindProxyCrashLoop = "proxy_crash_loop"
	kindCoverOK        = "cover_ok"
	kindCoverFailed    = "cover_failed"
	kindApplied        = "applied"
	kindPushFailed     = "push_failed"
	kindRefused        = "refused"
	kindEgressFailed   = "egress_failed"
	kindJobOK          = "job_ok"
	kindJobFailed      = "job_failed"

	levelInfo  = "info"
	levelWarn  = "warn"
	levelError = "error"
)

const (
	feedCap = 200
	// floodMerge: a flood that keeps coming stays one entry; after a quiet minute it is news again
	floodMerge = time.Minute
	detailMax  = 500
)

// feed is one node's ring of events, plus the last state seen of each thing it
// watches, so a restarted entry does not repeat a transition.
type feed struct {
	mu      sync.Mutex
	ring    []Event // oldest first
	link    *bool
	proxy   *proxyState
	cover   *bool
	applies string // the last state recorded as applied
	pushErr string // the last push failure recorded since then
}

type proxyState struct{ running, looping bool }

func clip(s string) string {
	if len(s) <= detailMax {
		return s
	}
	n := detailMax
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (f *feed) addLocked(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	ev.Detail = clip(ev.Detail)
	if len(f.ring) == feedCap {
		copy(f.ring, f.ring[1:])
		f.ring = f.ring[:feedCap-1]
	}
	f.ring = append(f.ring, ev)
}

// the nil checks let an entry built without a feed (tests, a node mid-removal) record nothing
func (f *feed) add(ev Event) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addLocked(ev)
}

// flood folds a burst into the same flood's entry while it keeps coming, and moves it to the top.
func (f *feed) flood(ev Event) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.ring) - 1; i >= 0 && ev.At.Sub(f.ring[i].At) < floodMerge; i-- {
		if f.ring[i].Kind != ev.Kind {
			continue
		}
		merged := f.ring[i]
		merged.Count += ev.Count
		if ev.At.After(merged.At) {
			merged.At = ev.At
		}
		if ev.Detail != "" {
			merged.Detail = clip(ev.Detail)
		}
		copy(f.ring[i:], f.ring[i+1:])
		f.ring[len(f.ring)-1] = merged
		return
	}
	f.addLocked(ev)
}

func (f *feed) list(limit int) []Event {
	out := []Event{}
	if f == nil {
		return out
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.ring) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, f.ring[i])
	}
	return out
}

// linkSeen records the link's transitions; the first look says something only when it is down.
func (f *feed) linkSeen(up bool, why string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.link
	f.link = &up
	switch {
	case up && prev != nil && !*prev:
		f.addLocked(Event{Kind: kindLinkUp, Level: levelInfo})
	case !up && (prev == nil || *prev):
		f.addLocked(Event{Kind: kindLinkDown, Level: levelError, Detail: why})
	}
}

// proxySeen follows telemt across status reports; inside a crash loop only the loop is news.
func (f *feed) proxySeen(st *ctrl.Status) {
	if f == nil || st == nil {
		return
	}
	t := st.Telemt
	cur := proxyState{running: t.Running, looping: t.CrashLoop}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.proxy
	f.proxy = &cur
	down := func() {
		// held on purpose (no users) is not a fault
		if t.Held != "" {
			f.addLocked(Event{Kind: kindProxyDown, Level: levelInfo, Detail: t.Held})
			return
		}
		f.addLocked(Event{Kind: kindProxyDown, Level: levelWarn, Detail: t.LastExit})
	}
	switch {
	case cur.looping:
		if prev == nil || !prev.looping {
			f.addLocked(Event{Kind: kindProxyCrashLoop, Level: levelError, Detail: t.LastExit})
		}
	case prev == nil:
		if !cur.running {
			down()
		}
	case prev.looping || prev.running != cur.running:
		if cur.running {
			f.addLocked(Event{Kind: kindProxyUp, Level: levelInfo})
		} else {
			down()
		}
	}
}

// coverSeen records the cover-site check's transitions; a first check that passes is not news.
func (f *feed) coverSeen(ok bool, detail string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	prev := f.cover
	f.cover = &ok
	switch {
	case ok && prev != nil && !*prev:
		f.addLocked(Event{Kind: kindCoverOK, Level: levelInfo, Detail: detail})
	case !ok && (prev == nil || *prev):
		f.addLocked(Event{Kind: kindCoverFailed, Level: levelWarn, Detail: detail})
	}
}

// applied records a state the agent took, once: a reconnect pushes the same one again.
func (f *feed) applied(rev string, users int) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushErr = ""
	if rev == f.applies {
		return
	}
	f.applies = rev
	f.addLocked(Event{Kind: kindApplied, Level: levelInfo, Detail: plural(users, "user")})
}

// pushFailed records a push failure, once per error until a push lands: retries back off but go on.
func (f *feed) pushFailed(why string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if why == f.pushErr {
		return
	}
	f.pushErr = why
	f.addLocked(Event{Kind: kindPushFailed, Level: levelError, Detail: why})
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}
