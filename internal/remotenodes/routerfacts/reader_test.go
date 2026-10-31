// SPDX-License-Identifier: AGPL-3.0-or-later

package routerfacts

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func writeReport(t *testing.T, path, content string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// countingFS counts how often the file is opened. Stat doesn't count, since
// that is the cheap look every call is allowed.
type countingFS struct {
	fs.FS
	opens int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opens++
	return c.FS.Open(name)
}

func (c *countingFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(c.FS, name) }

// rig is a reader on a real file, with a clock and nothing to wait for.
type rig struct {
	r     *Reader
	fs    *countingFS
	path  string
	clock time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "router")
	g := &rig{path: filepath.Join(dir, "state"), clock: now}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	g.fs = &countingFS{FS: os.DirFS(dir)}
	g.r = newReader(g.fs, "state", g.path)
	g.r.now = func() time.Time { return g.clock }
	g.r.pause = 0
	return g
}

func (g *rig) read(t *testing.T) Facts {
	t.Helper()
	f, err := g.r.Read()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPathIsUnderTheRouterFolder(t *testing.T) {
	if got, want := Path("/data"), filepath.FromSlash("/data/router/state"); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestReadsTheFileAtThePath(t *testing.T) {
	path := Path(t.TempDir())
	writeReport(t, path, goodReport(), now.Add(-time.Hour))
	r := NewReader(path)
	got, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if want := mustParse(t, goodReport()); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if e := r.Entry(now, MinRouterOS); !e.On || e.Reason != "" {
		t.Fatalf("entry: on = %v, reason = %q", e.On, e.Reason)
	}
}

func TestAMissingFileGivesAnEntryThatIsOff(t *testing.T) {
	path := Path(t.TempDir()) // the router folder isn't there either
	r := NewReader(path)

	if _, err := r.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want not-exist", err)
	}
	e := r.Entry(now, MinRouterOS)
	if e.On || e.CheckedAt != nil || e.Stale {
		t.Errorf("on = %v, checkedAt = %v, stale = %v", e.On, e.CheckedAt, e.Stale)
	}
	if !strings.Contains(e.Reason, "has not written its report") || !strings.Contains(e.Reason, path) {
		t.Errorf("reason = %q", e.Reason)
	}
	if e.Domestic != LinkMissing || e.VPN != (VPN{State: LinkMissing}) || e.Bridge != BridgeStopped {
		t.Errorf("domestic = %q, vpn = %+v, bridge = %q", e.Domestic, e.VPN, e.Bridge)
	}
	if e.RouterOS != (RouterOS{Min: "7.20"}) {
		t.Errorf("routerOS = %+v", e.RouterOS)
	}
	if len(e.Routes) != 3 {
		t.Fatalf("routes = %+v, want the three rows, none of them ready", e.Routes)
	}
	for _, r := range e.Routes {
		if r.Ready {
			t.Errorf("route %+v is ready without a report", r)
		}
	}

	// and it comes on by itself once the script has written
	writeReport(t, path, goodReport(), now)
	if e = r.Entry(now, MinRouterOS); !e.On || e.CheckedAt == nil {
		t.Errorf("after the report appeared: on = %v, checkedAt = %v", e.On, e.CheckedAt)
	}
}

func TestAFileThatCannotBeReadGivesAnEntryThatIsOff(t *testing.T) {
	t.Run("a folder in its place", func(t *testing.T) {
		g := newRig(t)
		if err := os.Mkdir(g.path, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := g.r.Read()
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err = %v, want a read error", err)
		}
		if e := g.r.Entry(now, MinRouterOS); e.On || e.CheckedAt != nil || !strings.Contains(e.Reason, "can't be read") {
			t.Errorf("on = %v, checkedAt = %v, reason = %q", e.On, e.CheckedAt, e.Reason)
		}
	})
	t.Run("no permission", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads anything")
		}
		g := newRig(t)
		writeReport(t, g.path, goodReport(), now.Add(-time.Hour))
		if err := os.Chmod(g.path, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := g.r.Read(); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v, want a permission error", err)
		}
		e := g.r.Entry(now, MinRouterOS)
		if e.On || !strings.Contains(e.Reason, "permission denied") || strings.Contains(e.Reason, "open ") {
			t.Errorf("on = %v, reason = %q", e.On, e.Reason)
		}
	})
	t.Run("empty", func(t *testing.T) {
		g := newRig(t)
		writeReport(t, g.path, "", now.Add(-time.Hour))
		if _, err := g.r.Read(); !errors.Is(err, ErrEmpty) {
			t.Fatalf("err = %v, want ErrEmpty", err)
		}
		if e := g.r.Entry(now, MinRouterOS); e.On || !strings.Contains(e.Reason, "is empty") {
			t.Errorf("on = %v, reason = %q", e.On, e.Reason)
		}
	})
	t.Run("far too large", func(t *testing.T) {
		g := newRig(t)
		writeReport(t, g.path, "#"+strings.Repeat("x", maxFile), now.Add(-time.Hour))
		if _, err := g.r.Read(); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
		if g.fs.opens != 0 {
			t.Errorf("a file the size said was too large was opened %d times", g.fs.opens)
		}
		if e := g.r.Entry(now, MinRouterOS); e.On || !strings.Contains(e.Reason, "larger than it should be") {
			t.Errorf("on = %v, reason = %q", e.On, e.Reason)
		}
	})
}

func TestAnUnchangedFileIsNotReadAgain(t *testing.T) {
	g := newRig(t)
	writeReport(t, g.path, goodReport(), now.Add(-time.Hour))
	first := g.read(t)
	opens := g.fs.opens
	if opens == 0 {
		t.Fatal("the file was never read")
	}
	for i := 0; i < 50; i++ {
		if got := g.read(t); !reflect.DeepEqual(got, first) {
			t.Fatalf("call %d: %+v", i, got)
		}
		g.r.Entry(now, MinRouterOS)
	}
	if g.fs.opens != opens {
		t.Fatalf("the file was opened %d times for 100 calls that found it unchanged", g.fs.opens-opens)
	}
}

func TestAChangedTimeOrSizeIsReadAgain(t *testing.T) {
	g := newRig(t)
	old := now.Add(-time.Hour)
	up, down := "domestic=1\nvpn=1\n", "domestic=0\nvpn=0\n" // the same length
	writeReport(t, g.path, up, old)
	if f := g.read(t); !f.Domestic || !f.VPN {
		t.Fatalf("first read: %+v", f)
	}

	// the same size under a new time
	writeReport(t, g.path, down, old.Add(time.Minute))
	if f := g.read(t); f.Domestic || f.VPN {
		t.Fatalf("a new time wasn't noticed: %+v", f)
	}

	// a new size under the same time
	writeReport(t, g.path, up+"route_vpn=1\n", old.Add(time.Minute))
	if f := g.read(t); !f.Domestic || !f.VPN || !f.RouteVPN {
		t.Fatalf("a new size wasn't noticed: %+v", f)
	}
}

// A file's time can be as coarse as a second or two, and the same size is
// likely: a flag flipping from 1 to 0 doesn't change it. A read that fresh
// can't vouch for the file, so the next call looks again.
func TestAChangeWithinTheFilesTimeGrainIsStillSeen(t *testing.T) {
	g := newRig(t)
	mod := now.Add(-time.Second)
	up, down := "domestic=1\nvpn=1\n", "domestic=0\nvpn=0\n"
	writeReport(t, g.path, up, mod)
	if f := g.read(t); !f.Domestic {
		t.Fatalf("first read: %+v", f)
	}
	writeReport(t, g.path, down, mod) // same size, same time
	if f := g.read(t); f.Domestic || f.VPN {
		t.Fatalf("a rewrite inside the time's grain was missed: %+v", f)
	}

	// Once the file is old enough that nothing can still land in its second,
	// what has been read is believed until the time or size changes.
	g.clock = now.Add(time.Hour)
	g.read(t) // the read just before was still fresh, so this one looks again
	opens := g.fs.opens
	for i := 0; i < 5; i++ {
		g.read(t)
	}
	if g.fs.opens != opens {
		t.Fatalf("a settled file was opened %d more times", g.fs.opens-opens)
	}
}

// The address is what the hub watches for a change, and a new one is as long
// as the old one more often than not.
func TestANewAddressOfTheSameLengthIsReadAgain(t *testing.T) {
	g := newRig(t)
	was, is := netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("192.168.1.33")
	text := func(a netip.Addr) string { return "domestic=1\ndomestic_addr=" + a.String() + "\n" }
	old := now.Add(-time.Hour)
	writeReport(t, g.path, text(was), old)
	if f := g.read(t); f.DomesticAddr != was {
		t.Fatalf("first read: %v", f.DomesticAddr)
	}
	writeReport(t, g.path, text(is), old.Add(time.Minute))
	if f := g.read(t); f.DomesticAddr != is {
		t.Fatalf("a new address under a new time wasn't noticed: %v", f.DomesticAddr)
	}

	// and inside the time's grain, where only the contents differ
	g.clock = now
	mod := now.Add(-time.Second)
	writeReport(t, g.path, text(was), mod)
	g.read(t)
	writeReport(t, g.path, text(is), mod)
	if f := g.read(t); f.DomesticAddr != is {
		t.Fatalf("a rewrite inside the time's grain was missed: %v", f.DomesticAddr)
	}
}

func TestAFileThatVanishesAndComesBack(t *testing.T) {
	g := newRig(t)
	old := now.Add(-time.Hour)
	writeReport(t, g.path, goodReport(), old)
	g.read(t)

	if err := os.Remove(g.path); err != nil {
		t.Fatal(err)
	}
	if _, err := g.r.Read(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("after it was removed: %v", err)
	}
	if e := g.r.Entry(now, MinRouterOS); e.On || e.CheckedAt != nil {
		t.Fatalf("a report that is gone still counts: on = %v, checkedAt = %v", e.On, e.CheckedAt)
	}

	// the same bytes under the same time are still read, not mistaken for what was cached
	writeReport(t, g.path, goodReport(), old)
	opens := g.fs.opens
	if f := g.read(t); !f.Domestic {
		t.Fatalf("after it came back: %+v", f)
	}
	if g.fs.opens == opens {
		t.Fatal("the report that came back was not read")
	}
}

// scriptFS serves a file whose contents are whatever gen says for each open,
// which is how a rewrite that lands in the middle of a read looks from here.
type scriptFS struct {
	gen   func(open int) string
	opens int
}

type scriptInfo struct{ fs.FileInfo }

func (scriptInfo) Name() string       { return "state" }
func (scriptInfo) Size() int64        { return 100 }
func (scriptInfo) Mode() fs.FileMode  { return 0o644 }
func (scriptInfo) ModTime() time.Time { return now.Add(-time.Hour) }
func (scriptInfo) IsDir() bool        { return false }
func (scriptInfo) Sys() any           { return nil }

func (s *scriptFS) Stat(string) (fs.FileInfo, error) { return scriptInfo{}, nil }

type scriptFile struct{ io.Reader }

func (scriptFile) Stat() (fs.FileInfo, error) { return scriptInfo{}, nil }
func (scriptFile) Close() error               { return nil }

func (s *scriptFS) Open(string) (fs.File, error) {
	s.opens++
	return scriptFile{strings.NewReader(s.gen(s.opens))}, nil
}

func scripted(gen func(open int) string) (*Reader, *scriptFS) {
	s := &scriptFS{gen: gen}
	r := newReader(s, "state", "/data/router/state")
	r.now = func() time.Time { return now }
	r.pause = 0
	return r, s
}

const (
	reportUp   = "domestic=1\nvpn=1\nroute_foreign=1\nroute_vpn=1\nroute_domestic=1\nros=7.24.5\nupdated=1793361600\n"
	reportDown = "domestic=0\nvpn=0\nroute_foreign=0\nroute_vpn=0\nroute_domestic=0\nros=7.20.0\nupdated=1793361900\n"
)

func TestAReportCaughtBeingRewrittenIsNeverReturned(t *testing.T) {
	// what a parse of either whole report gives, and what a mix of the two would
	wantUp, wantDown := parse([]byte(reportUp)), parse([]byte(reportDown))
	cut := strings.Index(reportUp, "route_foreign")

	for name, reads := range map[string][]string{
		// the first read got only the top of the old report, the rest the new one
		"the top of one, then the other":   {reportUp[:cut], reportDown, reportDown, reportDown},
		"the top of one, then a whole one": {reportUp[:cut], reportUp, reportUp, reportUp},
		"emptied, then filled":             {"", reportDown, reportDown, reportDown},
		"a mix of old and new":             {reportUp[:cut] + reportDown[strings.Index(reportDown, "route_foreign"):], reportDown, reportDown},
		"changing twice before it holds":   {reportUp[:cut], "", reportDown[:cut], reportUp, reportUp, reportUp},
	} {
		t.Run(name, func(t *testing.T) {
			r, _ := scripted(func(open int) string { return reads[min(open, len(reads))-1] })
			got, err := r.Read()
			if err != nil {
				t.Fatal(err)
			}
			// whichever whole report it settled on, never a blend
			if !reflect.DeepEqual(got, wantUp) && !reflect.DeepEqual(got, wantDown) {
				t.Fatalf("a half report came back: %+v", got)
			}
			if got.BadLines != 0 {
				t.Fatalf("%d bad lines: a torn report was parsed", got.BadLines)
			}
		})
	}
}

func TestAReportThatNeverHoldsStillIsAnError(t *testing.T) {
	changing := true
	r, _ := scripted(func(open int) string {
		if changing {
			return fmt.Sprintf("domestic=1\nfree_mem=%d\n", open) // different every time
		}
		return reportUp
	})
	_, err := r.Read()
	if !errors.Is(err, ErrUnsettled) {
		t.Fatalf("err = %v, want ErrUnsettled", err)
	}
	if e := r.Entry(now, MinRouterOS); e.On || e.CheckedAt != nil || !strings.Contains(e.Reason, "kept changing") {
		t.Fatalf("entry: on = %v, checkedAt = %v, reason = %q", e.On, e.CheckedAt, e.Reason)
	}

	// nothing from the failed tries is kept
	changing = false
	got, err := r.Read()
	if err != nil || !reflect.DeepEqual(got, parse([]byte(reportUp))) {
		t.Fatalf("once it held still: %+v, %v", got, err)
	}
}

func TestAReportThatStaysEmptyIsAnError(t *testing.T) {
	r, s := scripted(func(int) string { return "" })
	if _, err := r.Read(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
	if want := 2 * settleTries; s.opens != want {
		t.Fatalf("%d reads before giving up, want %d", s.opens, want)
	}
}

func TestAReportThatGrowsPastTheLimitWhileBeingRead(t *testing.T) {
	// the size was small when it was looked at
	r, _ := scripted(func(int) string { return strings.Repeat("a", maxFile+1) })
	if _, err := r.Read(); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

// Against a real file rewritten the way the router does it, in place, and the
// way that is safe, by rename: every read is a whole report or an error.
func TestARealWriterNeverShowsAHalfReport(t *testing.T) {
	up := strings.Replace(goodReport(), "updated=", "# the same time every time\nupdated=", 1)
	down := strings.NewReplacer("=1\n", "=0\n").Replace(up)
	wantUp, wantDown := parse([]byte(up)), parse([]byte(down))
	if reflect.DeepEqual(wantUp, wantDown) {
		t.Fatal("the two reports are the same")
	}

	for _, mode := range []string{"in place", "by rename"} {
		t.Run(mode, func(t *testing.T) {
			path := Path(t.TempDir())
			writeReport(t, path, up, now)
			r := NewReader(path)

			stop := make(chan struct{})
			var writer sync.WaitGroup
			writer.Add(1)
			go func() {
				defer writer.Done()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					content := up
					if i%2 == 1 {
						content = down
					}
					if mode == "in place" {
						_ = os.WriteFile(path, []byte(content), 0o644)
					} else {
						_ = os.WriteFile(path+".new", []byte(content), 0o644)
						_ = os.Rename(path+".new", path)
					}
					time.Sleep(time.Millisecond)
				}
			}()

			var mu sync.Mutex
			var clean int
			var sawUp, sawDown bool
			var readers sync.WaitGroup
			deadline := time.Now().Add(300 * time.Millisecond)
			for i := 0; i < 4; i++ {
				readers.Add(1)
				go func() {
					defer readers.Done()
					for time.Now().Before(deadline) {
						f, err := r.Read()
						if err != nil {
							if !errors.Is(err, ErrEmpty) && !errors.Is(err, ErrUnsettled) {
								t.Errorf("err = %v", err)
							}
							continue
						}
						mu.Lock()
						clean++
						sawUp = sawUp || reflect.DeepEqual(f, wantUp)
						sawDown = sawDown || reflect.DeepEqual(f, wantDown)
						mu.Unlock()
						if !reflect.DeepEqual(f, wantUp) && !reflect.DeepEqual(f, wantDown) {
							t.Errorf("a half report came back: %+v", f)
							return
						}
					}
				}()
			}
			readers.Wait()
			close(stop)
			writer.Wait()
			t.Logf("%d clean reads", clean)
			if !sawUp || !sawDown {
				t.Errorf("saw the first report: %v, the second: %v; the test never saw the file change", sawUp, sawDown)
			}
		})
	}
}
