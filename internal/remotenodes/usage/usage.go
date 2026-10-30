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

// node is what the collector holds for one node.
type node struct {
	hist *nodeHist

	// diskThrough is hist.through as the file holds it, or as it will once
	// there is a file: the mark up to which the agent may be told to drop.
	diskThrough int64
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
// A file that can't be used never stops the hub: one that is damaged or can't
// be read is kept beside itself under another name and the history starts
// empty, and one from a newer release is left exactly as it is, with nothing
// written to disk or acknowledged to the agents until a build that reads it
// takes over. Either way it is said in the log.
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
	}
}

// Flush writes the history if it changed since the last write. The write goes
// through the store's atomic routine, so the file is the old one or the new
// one whatever happens.
func (c *Collector) Flush(ctx context.Context) error {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	_, err := c.write()
	return err
}

// write saves the history when its bytes differ from the file's and returns
// the mark of each node as it is on disk now. The caller holds flushMu.
func (c *Collector) write() (map[uint]int64, error) {
	if c.noWrite {
		return nil, nil
	}
	c.mu.Lock()
	c.pruneLocked(c.now())
	enc, err := encode(c.hists())
	marks := make(map[uint]int64, len(c.nodes))
	for id, n := range c.nodes {
		marks[id] = n.hist.through
	}
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("usage: %w", err)
	}

	// the disk is not touched under the lock: readers never wait for it
	if c.path != "" && !bytes.Equal(enc, c.enc) {
		if err := store.WriteFileAtomic(c.path, enc); err != nil {
			return nil, fmt.Errorf("usage: %w", err)
		}
		c.enc = enc
		c.writes.Add(1)
	}

	c.mu.Lock()
	for id, m := range marks {
		if n := c.nodes[id]; n != nil && m > n.diskThrough {
			n.diskThrough = m
		}
	}
	c.mu.Unlock()
	return marks, nil
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
