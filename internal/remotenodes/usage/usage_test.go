// SPDX-License-Identifier: AGPL-3.0-or-later

package usage

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// at is a bucket's timestamp: an hour on the day of t0, which is the 30th.
func at(hour int) int64 { return unix(2026, 10, 30, hour, 0, 0) }

func TestOpeningAnEmptyFolderWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	c, _ := open(t, dir, newClock(t0))
	fi, err := os.Stat(dir)
	must(t, err)
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("the folder is %v, want 0700", fi.Mode().Perm())
	}
	must(t, c.Flush(context.Background()))
	if got := names(t, dir); len(got) != 0 || c.Writes() != 0 {
		t.Fatalf("folder holds %v after %d writes; nothing has been counted", got, c.Writes())
	}
}

func TestFlushWritesOnlyWhenSomethingChanged(t *testing.T) {
	dir := t.TempDir()
	c, _ := open(t, dir, newClock(t0))
	ctx := context.Background()
	path := filepath.Join(dir, FileName)

	c.inject(1, at(10), subs(1, 100, 900))
	must(t, c.Flush(ctx))
	if c.Writes() != 1 {
		t.Fatalf("writes = %d after the first change", c.Writes())
	}
	fi, err := os.Stat(path)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("usage.json is %v, want 0600", fi.Mode().Perm())
	}
	first := readFile(t, path)

	for i := 0; i < 20; i++ {
		must(t, c.Flush(ctx))
	}
	if c.Writes() != 1 || !bytes.Equal(readFile(t, path), first) {
		t.Fatalf("twenty flushes with nothing new wrote %d times", c.Writes()-1)
	}

	c.inject(1, at(11), subs(1, 1, 1))
	must(t, c.Flush(ctx))
	if c.Writes() != 2 || bytes.Equal(readFile(t, path), first) {
		t.Fatalf("a change wasn't written: writes = %d", c.Writes())
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("the folder holds %v, want only the file", got)
	}
}

func TestTheHistoryComesBackAfterARestart(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(t0)
	c, _ := open(t, dir, clk)
	c.inject(1, at(9), subs(1, 100, 900, 2, 5, 50))
	c.inject(1, at(10), subs(1, 1, 1))
	c.inject(2, at(10), subs(1, 7, 70))
	c.inject(1, unix(2026, 10, 29, 20, 0, 0), subs(1, 1000, 9000))
	c.mu.Lock()
	c.nodes[1].hist.bridge = Totals{Up: 500, Down: 600}
	c.mu.Unlock()
	must(t, c.Flush(context.Background()))

	again, _ := open(t, dir, clk)
	if again.Writes() != 0 {
		t.Fatalf("opening wrote %d times", again.Writes())
	}
	for _, id := range []uint{1, 2, 3} {
		if got, want := again.Node(id), c.Node(id); got != want {
			t.Errorf("node %d: %+v after the restart, %+v before", id, got, want)
		}
		for _, sub := range []uint{1, 2, 3} {
			if got, want := again.Subscription(sub, id), c.Subscription(sub, id); got != want {
				t.Errorf("subscription %d on node %d: %+v after, %+v before", sub, id, got, want)
			}
		}
		if got, want := again.Bridge(id), c.Bridge(id); got != want {
			t.Errorf("bridge of node %d: %+v after, %+v before", id, got, want)
		}
	}
	for _, sub := range []uint{1, 2} {
		if got, want := again.SubscriptionTotal(sub), c.SubscriptionTotal(sub); got != want {
			t.Errorf("subscription %d in all: %+v after, %+v before", sub, got, want)
		}
	}
	if got := again.Node(1); got.TodayUp != 106 || got.TodayDown != 951 || got.MonthUp != 1106 || got.MonthDown != 9951 {
		t.Errorf("node 1 reads %+v", got)
	}
	if got := again.Bridge(1); got != (Totals{Up: 500, Down: 600}) {
		t.Errorf("bridge = %+v", got)
	}
	must(t, again.Flush(context.Background()))
	if again.Writes() != 0 {
		t.Fatal("a flush after reading the file wrote it again")
	}
	// and the mark is where it was, so the agent is told the same thing
	if got := again.nodes[1].diskThrough; got != at(10) {
		t.Fatalf("the mark of node 1 is %d, want %d", got, at(10))
	}
}

func TestAFileFromANewerReleaseIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	newer := []byte(`{"version":99,"nodes":[{"id":1,"something":"this build has never seen"}]}` + "\n")
	must(t, os.WriteFile(path, newer, 0o600))

	c, l := open(t, dir, newClock(t0))
	if !strings.Contains(l.text(), "newer") {
		t.Errorf("the log doesn't say why: %q", l.text())
	}
	// it still counts, in memory
	c.inject(1, at(10), subs(1, 5, 50))
	must(t, c.Flush(context.Background()))
	if c.Writes() != 0 || !bytes.Equal(readFile(t, path), newer) {
		t.Fatalf("the newer file was touched: %q", readFile(t, path))
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("the folder holds %v: no copy or temp file belongs there", got)
	}
	if got := c.Node(1); got.TodayUp != 5 {
		t.Fatalf("the collector stopped counting: %+v", got)
	}
}

func TestADamagedFileIsKeptAndReplaced(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":    "this is not json at all",
		"empty":      "",
		"truncated":  `{"version":1,"nodes":[{"id":1,"acked_through":5,"days":{"2026-10-30":{"up":1`,
		"no version": `{"nodes":[]}`,
		"bad day":    `{"version":1,"nodes":[{"id":1,"days":{"yesterday":{"up":1,"down":1}},"subscriptions":[]}]}`,
		"zero id":    `{"version":1,"nodes":[{"id":0,"days":{},"subscriptions":[]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			must(t, os.WriteFile(path, []byte(content), 0o600))
			clk := newClock(t0)

			c, l := open(t, dir, clk)
			copies := corrupt(t, dir)
			if len(copies) != 1 {
				t.Fatalf("copies of the damaged file: %v", copies)
			}
			if got := string(readFile(t, copies[0])); got != content {
				t.Fatalf("the copy holds %q, want the damaged bytes", got)
			}
			if !strings.Contains(copies[0], "20261030T120000Z") {
				t.Errorf("the copy is named %s, want the time in it", filepath.Base(copies[0]))
			}
			if !strings.Contains(l.text(), copies[0]) {
				t.Errorf("the log doesn't say where the copy is: %q", l.text())
			}
			if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("the damaged file is still at its name: %v", err)
			}
			if got := c.Node(1); got != (NodeUsage{}) {
				t.Fatalf("the history doesn't start empty: %+v", got)
			}

			// it carries on, and the next write is a good file
			c.inject(1, at(10), subs(1, 5, 50))
			must(t, c.Flush(context.Background()))
			again, _ := open(t, dir, clk)
			if got := again.Node(1); got.TodayUp != 5 || got.TodayDown != 50 {
				t.Fatalf("the new file reads %+v", got)
			}
			if got := corrupt(t, dir); len(got) != 1 {
				t.Fatalf("copies after the second open: %v", got)
			}
		})
	}
}

func TestTwoDamagedFilesInOneSecondBothSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	clk := newClock(t0)
	for _, junk := range []string{"first junk", "second junk"} {
		must(t, os.WriteFile(path, []byte(junk), 0o600))
		open(t, dir, clk)
	}
	copies := corrupt(t, dir)
	if len(copies) != 2 {
		t.Fatalf("copies: %v", copies)
	}
	got := map[string]bool{}
	for _, p := range copies {
		got[string(readFile(t, p))] = true
	}
	if !got["first junk"] || !got["second junk"] {
		t.Fatalf("copies hold %v", got)
	}
}

func TestAFileThatCannotBeReadDoesNotStopTheCollector(t *testing.T) {
	// a directory where the file should be reads as an error, not as damage
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, FileName), 0o700))
	c, l := open(t, dir, newClock(t0))
	if len(corrupt(t, dir)) != 1 {
		t.Fatalf("the unreadable path was not moved aside: %v", names(t, dir))
	}
	if !strings.Contains(l.text(), "can't be read") {
		t.Errorf("log = %q", l.text())
	}
	c.inject(1, at(10), subs(1, 5, 50))
	must(t, c.Flush(context.Background()))
	if c.Writes() != 1 {
		t.Fatalf("writes = %d", c.Writes())
	}
}

func TestAFileWithoutPermissionDoesNotStopTheCollector(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	must(t, os.WriteFile(path, []byte(`{"version":1,"nodes":[]}`), 0o000))
	c, _ := open(t, dir, newClock(t0))
	c.inject(1, at(10), subs(1, 5, 50))
	must(t, c.Flush(context.Background()))
	if got := c.Node(1); got.TodayUp != 5 {
		t.Fatalf("node reads %+v", got)
	}
}

func TestLeftoverTempFilesAreRemovedOnOpen(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, ".usage.json-123456"), []byte("half"), 0o600))
	must(t, os.WriteFile(filepath.Join(dir, ".state.json-777"), []byte("the store's, not ours"), 0o600))
	open(t, dir, newClock(t0))
	if got := names(t, dir); len(got) != 1 || got[0] != ".state.json-777" {
		t.Fatalf("the folder holds %v", got)
	}
}

func TestDaysOutOfTheWindowAreDroppedWhenWrittenAndWhenLoaded(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(t0)
	c, _ := open(t, dir, clk)
	for back := 0; back < 45; back++ {
		c.inject(1, at(10)-int64(back)*secondsPerDay, subs(1, 1, 1))
	}
	must(t, c.Flush(context.Background()))
	full := len(readFile(t, filepath.Join(dir, FileName)))
	again, _ := open(t, dir, clk)
	again.mu.RLock()
	days, subDays := len(again.nodes[1].hist.days), len(again.nodes[1].hist.subs[1])
	again.mu.RUnlock()
	if days != keepDays || subDays != keepDays {
		t.Fatalf("the file holds %d days of the node and %d of the subscription, want %d", days, subDays, keepDays)
	}
	if got := again.Node(1); got.MonthUp != monthDays || got.TodayUp != 1 {
		t.Fatalf("node reads %+v, want a month of %d", got, monthDays)
	}

	// a file that has aged past its days is trimmed when it is loaded, and the
	// next flush writes the trimmed file
	clk.Advance(10 * 24 * time.Hour)
	later, _ := open(t, dir, clk)
	later.mu.RLock()
	days = len(later.nodes[1].hist.days)
	later.mu.RUnlock()
	if days != keepDays-10 {
		t.Fatalf("after ten days the history holds %d days, want %d", days, keepDays-10)
	}
	must(t, later.Flush(context.Background()))
	if later.Writes() != 1 {
		t.Fatalf("the trimmed history was not written: %d writes", later.Writes())
	}
	if got := len(readFile(t, filepath.Join(dir, FileName))); got >= full {
		t.Fatalf("the trimmed file is %d bytes, the full one was %d", got, full)
	}
}

func TestForgettingShrinksTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	c, _ := open(t, dir, newClock(t0))
	ctx := context.Background()
	for node := uint(1); node <= 3; node++ {
		for sub := uint64(1); sub <= 4; sub++ {
			c.inject(node, at(10), subs(sub, 100*uint64(node), 1000*uint64(node)))
		}
	}
	must(t, c.Flush(ctx))
	full := len(readFile(t, path))

	c.Forget(2)
	c.Forget(99) // one that was never there
	must(t, c.Flush(ctx))
	if got := c.Node(2); got != (NodeUsage{}) {
		t.Errorf("node 2 still reads %+v", got)
	}
	afterNode := len(readFile(t, path))
	if afterNode >= full || strings.Contains(string(readFile(t, path)), `"id":2,"acked_through"`) {
		t.Fatalf("the file is %d bytes, was %d, and still holds node 2", afterNode, full)
	}

	c.ForgetSubscription(3)
	must(t, c.Flush(ctx))
	if got := c.SubscriptionTotal(3); got != (SubUsage{}) {
		t.Errorf("subscription 3 still reads %+v", got)
	}
	if got := c.Subscription(1, 1); got.TodayUp != 100 {
		t.Errorf("another subscription lost its numbers: %+v", got)
	}
	// what it used is still in the node's total: it went through the node
	if got := c.Node(1); got.TodayUp != 400 {
		t.Errorf("node 1 reads %+v; the forgotten subscription's bytes belong to the node", got)
	}
	if after := len(readFile(t, path)); after >= afterNode {
		t.Fatalf("the file didn't shrink for the subscription: %d, was %d", after, afterNode)
	}
	if c.Writes() != 3 {
		t.Fatalf("writes = %d, want 3", c.Writes())
	}
}

func TestAFailedWriteChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	c, _ := open(t, dir, newClock(t0))
	ctx := context.Background()
	c.inject(1, at(10), subs(1, 5, 50))
	must(t, c.Flush(ctx))
	good := readFile(t, path)

	c.inject(1, at(11), subs(1, 1, 1))
	real := c.path
	c.path = filepath.Join(dir, "gone", FileName) // the folder is missing: the write fails
	if err := c.Flush(ctx); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	if c.Writes() != 1 || !bytes.Equal(readFile(t, real), good) {
		t.Fatalf("writes = %d after a failed write, and the file changed: %v", c.Writes(), !bytes.Equal(readFile(t, real), good))
	}
	if got := c.Node(1); got.TodayUp != 6 {
		t.Fatalf("the counts in memory went back: %+v", got)
	}
	// and the mark on disk did not move with it
	if got := c.nodes[1].diskThrough; got != at(10) {
		t.Fatalf("mark on disk = %d after a write that failed, want %d", got, at(10))
	}

	c.path = real
	must(t, c.Flush(ctx))
	if c.Writes() != 2 {
		t.Fatalf("the retry didn't write: %d", c.Writes())
	}
	if got := c.nodes[1].diskThrough; got != at(11) {
		t.Fatalf("mark on disk = %d, want %d", got, at(11))
	}
}

func TestWithoutAFolderEverythingStaysInMemory(t *testing.T) {
	c, err := Open(Config{Now: newClock(t0).Now})
	must(t, err)
	c.inject(1, at(10), subs(1, 5, 50))
	must(t, c.Flush(context.Background()))
	if c.Writes() != 0 {
		t.Fatalf("writes = %d with no folder", c.Writes())
	}
	if got := c.Node(1); got.TodayUp != 5 {
		t.Fatalf("node reads %+v", got)
	}
	// the memory counts as the disk: the mark moves as it would
	if got := c.nodes[1].diskThrough; got != at(10) {
		t.Fatalf("mark = %d", got)
	}
}

func TestOpenFailsWhenTheFolderCannotBeMade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a file")
	must(t, os.WriteFile(file, nil, 0o600))
	if _, err := Open(Config{Dir: filepath.Join(file, "data")}); err == nil {
		t.Fatal("a folder under a file was opened")
	}
}

func TestRunFlushesEveryIntervalAndOnTheWayOut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		c, err := Open(Config{Dir: dir, FlushEvery: 5 * time.Minute, Logf: t.Logf})
		must(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()

		c.inject(1, time.Now().Unix()-3600, subs(1, 5, 50))
		time.Sleep(4*time.Minute + 59*time.Second)
		synctest.Wait()
		if c.Writes() != 0 {
			t.Fatalf("a write came before the interval: %d", c.Writes())
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		if c.Writes() != 1 {
			t.Fatalf("writes = %d after the interval", c.Writes())
		}

		// quiet intervals write nothing
		time.Sleep(20 * time.Minute)
		synctest.Wait()
		if c.Writes() != 1 {
			t.Fatalf("writes = %d after quiet intervals", c.Writes())
		}

		// what came in since the last flush is written on the way out
		c.inject(1, time.Now().Unix()-1800, subs(1, 1, 1))
		cancel()
		<-done
		if c.Writes() != 2 {
			t.Fatalf("writes = %d after Run returned, want the last flush", c.Writes())
		}
	})
}

// Reads, writes and flushes from many goroutines at once, for -race.
func TestReadsAndFlushesRunTogether(t *testing.T) {
	dir := t.TempDir()
	c, _ := open(t, dir, newClock(t0))
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				c.inject(uint(g%2+1), at(i%20), subs(uint64(g+1), 1, 1))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				c.Node(uint(g%2 + 1))
				c.Subscription(uint(g+1), uint(g%2+1))
				c.SubscriptionTotal(uint(g + 1))
				c.Bridge(1)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_ = c.Flush(ctx)
				if i == 5 && g == 0 {
					c.Forget(2)
					c.ForgetSubscription(3)
				}
			}
		}()
	}
	wg.Wait()
	must(t, c.Flush(ctx))
	again, _ := open(t, dir, newClock(t0))
	if got, want := again.Node(1), c.Node(1); got != want {
		t.Fatalf("the file and memory disagree: %+v and %+v", got, want)
	}
}
