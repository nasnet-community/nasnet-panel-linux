// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// Defaults for the fields of Config left at zero.
const (
	DefaultFlushEvery  = 5 * time.Minute
	DefaultMissedPolls = 3

	// finalFlushWait is how long Run's last flush may take once its context is
	// done: the container has a few seconds to stop, and the file matters more
	// than the acknowledgements.
	finalFlushWait = 3 * time.Second
)

// Config says where the history lives and how the collector behaves.
type Config struct {
	// Dir is the data folder, the one the store uses. Empty keeps everything
	// in memory, which is for tests.
	Dir string
	// FlushEvery is how often Run writes the file when something changed. Zero
	// means DefaultFlushEvery.
	FlushEvery time.Duration
	// MissedPolls is how many polls in a row a node may fail before it stops
	// being live. Zero means DefaultMissedPolls.
	MissedPolls int
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
	// Logf receives what the collector has to say. Nil means the standard
	// logger.
	Logf func(format string, args ...any)
}

// node is what the collector holds for one node. Everything in it is under
// Collector.mu, except pollMu.
type node struct {
	hist *nodeHist

	// diskThrough is hist.through as the file holds it, or as it will once
	// there is a file: the mark up to which the agent may be told to drop.
	diskThrough int64

	pollMu sync.Mutex // one poll of the node at a time, the fetch included

	// Below is in memory only, and gone with the process.

	src       Source // what the last poll asked; Flush acknowledges through it
	acked     int64  // the mark the agent was last told, in this run
	staleSeen bool   // the agent was found holding buckets the file counts: tell it again
	// ackFailing is whether the last acknowledgement a poll tried failed, so a
	// failure that goes on is said once.
	ackFailing bool

	live      bool      // answered within the last MissedPolls polls
	misses    int       // polls in a row without an answer
	updatedAt time.Time // the last answer
	partial   *partial  // the newest bucket, still growing

	online   map[uint]int       // subscription -> client addresses, as of the last poll that said
	onlineAt time.Time          // when that was
	lastSeen map[uint]time.Time // subscription -> the last poll that found it online

	havePrev         bool // a poll with traffic data came before, so there is something to take a difference from
	lastData         time.Time
	obs              []observation
	rateUp, rateDown uint64
	ring             ring

	warnedAhead bool // buckets dated in the future were found, and said
}

// Collector counts what the subscriptions use on each node and answers for it.
// All its methods are safe for concurrent use; the ones that read never wait
// for the disk.
type Collector struct {
	cfg  Config
	path string // empty when the history is only in memory

	mu    sync.RWMutex // the nodes map, and everything in the nodes
	nodes map[uint]*node

	flushMu sync.Mutex // one flush at a time; guards enc
	enc     []byte     // the history as the file holds it, as far as this collector knows
	noWrite bool       // the file is from a newer release: never written, nothing acknowledged
	writes  atomic.Uint64
}

// Open starts a collector on cfg.Dir and loads the history in it. A missing
// file is an empty history, and nothing is written until something changes.
// A file that can't be used never stops the hub: a damaged or unreadable one is
// moved beside itself and the history starts empty, and one from a newer
// release is left as it is, with nothing written or acknowledged until a build
// that reads it takes over. The log says which.
func Open(cfg Config) (*Collector, error) {
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = DefaultFlushEvery
	}
	if cfg.MissedPolls <= 0 {
		cfg.MissedPolls = DefaultMissedPolls
	}
	c := &Collector{cfg: cfg, nodes: map[uint]*node{}}
	if cfg.Dir != "" {
		if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
			return nil, fmt.Errorf("usage: %w", err)
		}
		c.path = filepath.Join(cfg.Dir, FileName)
		dropStrayTemps(cfg.Dir)
		c.load()
	}
	var err error
	// what the file holds, before the old days are dropped: if there are any,
	// the first flush is a change
	if c.enc, err = encode(c.hists()); err != nil {
		return nil, fmt.Errorf("usage: %w", err)
	}
	c.pruneLocked(c.now())
	return c, nil
}

func (c *Collector) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now()
	}
	return time.Now()
}

func (c *Collector) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// load reads the file into c.nodes. It runs before the collector is shared.
func (c *Collector) load() {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		c.setAside(fmt.Errorf("can't be read: %w", err))
		return
	}
	hists, err := decode(b)
	switch {
	case errors.Is(err, errNewer):
		c.noWrite = true
		c.logf("usage: %s: %v; it is left alone, and until a build that reads it is running nothing is written or acknowledged", c.path, err)
	case err != nil:
		c.setAside(err)
	default:
		for id, h := range hists {
			c.nodes[id] = &node{hist: h, diskThrough: h.through}
		}
	}
}

// hists is the nodes' histories, for encoding. The caller holds c.mu, or is
// the only one with the collector.
func (c *Collector) hists() map[uint]*nodeHist {
	out := make(map[uint]*nodeHist, len(c.nodes))
	for id, n := range c.nodes {
		out[id] = n.hist
	}
	return out
}

// nodeLocked is the node, made if it is new. The caller holds c.mu for writing.
func (c *Collector) nodeLocked(id uint) *node {
	n := c.nodes[id]
	if n == nil {
		n = &node{hist: newNodeHist()}
		c.nodes[id] = n
	}
	return n
}

// pruneLocked drops the days that are out of the window, and the caller holds
// c.mu for writing, or is the only one with the collector.
func (c *Collector) pruneLocked(now time.Time) {
	keepFrom := dayOfTime(now) - (keepDays - 1)
	for _, n := range c.nodes {
		n.hist.prune(keepFrom)
	}
}

// Writes is how many times the file has been written since Open.
func (c *Collector) Writes() uint64 { return c.writes.Load() }

// Forget drops everything about a node that has been removed.
func (c *Collector) Forget(nodeID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.nodes, nodeID)
}

// ForgetSubscription drops everything about a subscription that has been
// deleted, on every node. What it used stays in its nodes' totals: it went
// through them. An agent's bucket that is still waiting to be counted can give
// the subscription a few bytes again; they age out of the history with the
// rest.
func (c *Collector) ForgetSubscription(subID uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.nodes {
		delete(n.hist.subs, subID)
		delete(n.online, subID)
		delete(n.lastSeen, subID)
		if n.partial != nil {
			delete(n.partial.b.subs, subID)
		}
	}
}

// Flush writes the history if it changed since the last write, and then, only
// then, tells each node's agent to drop the buckets the file now counts. The
// write goes through the store's atomic routine, so the file is the old one
// or the new one whatever happens; if it fails nothing is acknowledged. An
// acknowledgement that fails is returned, naming the node, and is tried again
// by the next flush: the agent keeps what it wasn't told to drop.
//
// A node is acknowledged through the Source of its last poll, so one that
// hasn't been polled since the hub started isn't acknowledged yet.
func (c *Collector) Flush(ctx context.Context) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	if c.noWrite {
		return nil
	}
	if err := c.write(); err != nil {
		return err
	}
	return c.acknowledge(ctx)
}

// write saves the history when its bytes differ from the file's, and records
// each node's mark as it is on disk now. The caller holds flushMu.
func (c *Collector) write() error {
	c.mu.Lock()
	c.pruneLocked(c.now())
	enc, err := encode(c.hists())
	marks := make(map[uint]int64, len(c.nodes))
	for id, n := range c.nodes {
		marks[id] = n.hist.through
	}
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("usage: %w", err)
	}

	// the disk is not touched under the lock: readers never wait for it
	if c.path != "" && !bytes.Equal(enc, c.enc) {
		if err := store.WriteFileAtomic(c.path, enc); err != nil {
			return fmt.Errorf("usage: %w", err)
		}
		c.enc = enc
		c.writes.Add(1)
	}

	c.mu.Lock()
	for id, m := range marks {
		if n := c.nodes[id]; n != nil {
			n.diskThrough = max(n.diskThrough, m)
		}
	}
	c.mu.Unlock()
	return nil
}

// Run flushes every FlushEvery until ctx is done, and once more then, so a
// clean shutdown writes what is left. The last flush has a few seconds of its
// own, since ctx is gone by then. The manager can also call Flush itself.
func (c *Collector) Run(ctx context.Context) {
	t := time.NewTicker(c.cfg.FlushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			last, cancel := context.WithTimeout(context.WithoutCancel(ctx), finalFlushWait)
			if err := c.Flush(last); err != nil {
				c.logf("%v", err)
			}
			cancel()
			return
		case <-t.C:
			if err := c.Flush(ctx); err != nil {
				c.logf("%v", err)
			}
		}
	}
}
