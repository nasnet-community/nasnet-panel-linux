// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

var t0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

const uuidA = "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5f"

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func addNode(host string) func(*model.State) error {
	return func(s *model.State) error {
		_, err := s.AddNode(model.Node{Name: "node " + host, Host: host, SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: t0})
		return err
	}
}

func addSub(name string) func(*model.State) error {
	return func(s *model.State) error {
		_, err := s.AddSubscription(model.Subscription{Name: name, CreatedAt: t0})
		return err
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// same compares two states by what they would write: a nil list and an empty
// one are the same thing to the file.
func same(t *testing.T, a, b model.State) bool {
	t.Helper()
	ea, err := model.Encode(a)
	must(t, err)
	eb, err := model.Encode(b)
	must(t, err)
	return string(ea) == string(eb)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestRoundTripAndPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s := open(t, dir)
	if _, err := os.Stat(s.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("opening an empty store wrote a file: %v", err)
	}
	if got := s.Read(); got.Version != model.SchemaVersion || len(got.Nodes) != 0 {
		t.Fatalf("a fresh store reads %+v", got)
	}

	must(t, s.Update(func(st *model.State) error {
		n, err := st.AddNode(model.Node{
			Name: "تهران ۱", Host: "203.0.113.9", SSHPort: 22, SSHUser: "root", AgentPort: 9090, EntryPort: 443,
			State: model.NodeRegistered, CreatedAt: t0, Arch: "arm64",
			RealityTarget: "www.example.com:443", RealitySNI: "www.example.com",
			RealityPublicKey: "pub", RealityShortID: "0123abcd", RealityPrivateKey: "priv-secret",
			AgentVersion: "1.3.0", XrayVersion: "26.10.21", CertNotBefore: t0, CertNotAfter: t0.AddDate(1, 0, 0),
			SSHHostKey: "SHA256:pinned",
		})
		if err != nil {
			return err
		}
		sub, err := st.AddSubscription(model.Subscription{Name: "علی", CreatedAt: t0})
		if err != nil {
			return err
		}
		st.Enablements = append(st.Enablements, model.Enablement{SubscriptionID: sub.ID, NodeID: n.ID, ClientUUID: uuidA, Enabled: true})
		st.LastBackupAt = t0
		return nil
	}))

	fi, err := os.Stat(s.Path())
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state.json is %v, want 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	must(t, err)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("the data directory is %v, want 0700", di.Mode().Perm())
	}
	// the file is the one place the secrets live
	raw := string(readFile(t, s.Path()))
	for _, want := range []string{"priv-secret", "SHA256:pinned", uuidA} {
		if !strings.Contains(raw, want) {
			t.Errorf("state.json doesn't hold %q", want)
		}
	}

	again := open(t, dir)
	if !same(t, again.Read(), s.Read()) {
		t.Fatalf("the state changed over a restart:\n got %+v\nwant %+v", again.Read(), s.Read())
	}
	if s.Writes() != 1 || again.Writes() != 0 {
		t.Errorf("writes: %d before, %d after reopening; want 1 and 0", s.Writes(), again.Writes())
	}
}

func TestALooseExistingDirectoryIsTightened(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	must(t, os.Mkdir(dir, 0o755))
	must(t, os.Chmod(dir, 0o755)) // not through the umask
	open(t, dir)
	di, err := os.Stat(dir)
	must(t, err)
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("the data directory is %v, want 0700", di.Mode().Perm())
	}
}

func TestNothingIsWrittenUntilSomethingChanges(t *testing.T) {
	s := open(t, t.TempDir())
	noop := func(*model.State) error { return nil }

	must(t, s.Update(noop))
	if _, err := os.Stat(s.Path()); !errors.Is(err, fs.ErrNotExist) || s.Writes() != 0 {
		t.Fatalf("an update that changed nothing wrote (%d writes, stat: %v)", s.Writes(), err)
	}

	must(t, s.Update(addNode("203.0.113.1")))
	if s.Writes() != 1 {
		t.Fatalf("writes = %d after the first real change", s.Writes())
	}
	first, err := os.Stat(s.Path())
	must(t, err)

	same := func(st *model.State) error { st.Nodes[0].Name = "node 203.0.113.1"; return nil } // what it already is
	undone := func(st *model.State) error {
		old := st.Nodes[0].Name
		st.Nodes[0].Name = "changed"
		st.Nodes[0].Name = old
		return nil
	}
	refused := func(st *model.State) error { st.Nodes[0].Name = "changed"; return errors.New("no") }
	must(t, s.Update(noop))
	must(t, s.Update(same))
	must(t, s.Update(undone))
	if err := s.Update(refused); err == nil {
		t.Fatal("the error from fn was lost")
	}
	now, err := os.Stat(s.Path())
	must(t, err)
	if s.Writes() != 1 || !os.SameFile(first, now) {
		t.Fatalf("writes = %d, same file = %v: updates that changed nothing rewrote it", s.Writes(), os.SameFile(first, now))
	}

	must(t, s.Update(func(st *model.State) error { st.Nodes[0].Name = "renamed"; return nil }))
	now, err = os.Stat(s.Path())
	must(t, err)
	if s.Writes() != 2 || os.SameFile(first, now) {
		t.Fatalf("writes = %d, same file = %v after a real change", s.Writes(), os.SameFile(first, now))
	}
	if got := s.Read().Nodes[0].Name; got != "renamed" {
		t.Fatalf("name = %q", got)
	}
}

func TestTheCapsHold(t *testing.T) {
	s := open(t, t.TempDir())
	for i := 1; i <= model.MaxNodes; i++ {
		must(t, s.Update(addNode(fmt.Sprintf("203.0.113.%d", i))))
	}
	for i := 1; i <= model.MaxSubscriptions; i++ {
		must(t, s.Update(addSub(fmt.Sprintf("sub %d", i))))
	}
	writes, file := s.Writes(), readFile(t, s.Path())

	// through AddNode, and by an edit that skips it
	skip := func(st *model.State) error {
		st.Nodes = append(st.Nodes, st.Nodes[0])
		st.Nodes[len(st.Nodes)-1].ID = st.NextNodeID
		st.NextNodeID++
		return nil
	}
	skipSub := func(st *model.State) error {
		st.Subscriptions = append(st.Subscriptions, st.Subscriptions[0])
		st.Subscriptions[len(st.Subscriptions)-1].ID = st.NextSubscriptionID
		st.Subscriptions[len(st.Subscriptions)-1].Name = "one too many"
		st.NextSubscriptionID++
		return nil
	}
	for name, c := range map[string]struct {
		fn   func(*model.State) error
		want error
	}{
		"fourth node":             {addNode("203.0.113.99"), model.ErrNodeLimit},
		"fourth node, no helper":  {skip, model.ErrNodeLimit},
		"eleventh sub":            {addSub("eleventh"), model.ErrSubscriptionLimit},
		"eleventh sub, no helper": {skipSub, model.ErrSubscriptionLimit},
	} {
		if err := s.Update(c.fn); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
	if got := s.Read(); len(got.Nodes) != model.MaxNodes || len(got.Subscriptions) != model.MaxSubscriptions {
		t.Errorf("a refused update changed the state: %d nodes, %d subscriptions", len(got.Nodes), len(got.Subscriptions))
	}
	if s.Writes() != writes || string(readFile(t, s.Path())) != string(file) {
		t.Error("a refused update touched the file")
	}

	// making room works, and the freed slot can be used again
	must(t, s.Update(func(st *model.State) error { return st.RemoveNode(1) }))
	must(t, s.Update(addNode("203.0.113.50")))
	if got := s.Read().Nodes; len(got) != model.MaxNodes || got[len(got)-1].ID != model.MaxNodes+1 {
		t.Errorf("after a removal and an add: %+v", got)
	}
}

func TestAFileWithMoreThanTheCapsStillWorks(t *testing.T) {
	// a past release allowed more, or somebody edited the file
	dir := t.TempDir()
	st := model.NewState()
	for i := 1; i <= model.MaxNodes+1; i++ {
		st.Nodes = append(st.Nodes, model.Node{
			ID: uint(i), Name: "n" + strconv.Itoa(i), Host: fmt.Sprintf("203.0.113.%d", i),
			SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: t0,
		})
	}
	st.NextNodeID = model.MaxNodes + 2
	b, err := model.Encode(st)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(dir, FileName), b, 0o600))

	s := open(t, dir)
	must(t, s.Update(func(st *model.State) error { st.Nodes[0].Name = "renamed"; return nil }))
	must(t, s.Update(func(st *model.State) error { return st.RemoveNode(2) }))
	if err := s.Update(addNode("203.0.113.99")); !errors.Is(err, model.ErrNodeLimit) {
		t.Fatalf("growing past the cap: %v", err)
	}
}

func TestAFailedUpdateChangesNothing(t *testing.T) {
	s := open(t, t.TempDir())
	must(t, s.Update(addNode("203.0.113.1")))
	want, file, writes := s.Read(), readFile(t, s.Path()), s.Writes()
	check := func(what string) {
		t.Helper()
		if !same(t, s.Read(), want) || s.Writes() != writes || string(readFile(t, s.Path())) != string(file) {
			t.Fatalf("%s changed the store", what)
		}
	}

	boom := errors.New("boom")
	if err := s.Update(func(st *model.State) error { st.Nodes[0].Name = "x"; return boom }); err != boom {
		t.Fatalf("err = %v, want exactly what fn returned", err)
	}
	check("an error from fn")

	var inv *model.InvalidError
	err := s.Update(func(st *model.State) error { st.Nodes[0].SSHPort = 0; return nil })
	if !errors.As(err, &inv) {
		t.Fatalf("an invalid result: %v", err)
	}
	check("an invalid result")

	if err := s.Update(func(st *model.State) error { st.NextNodeID = 1; return nil }); err == nil {
		t.Fatal("an id counter turned back and nothing said so")
	}
	check("a counter turned back")

	func() {
		defer func() { _ = recover() }()
		s.Update(func(*model.State) error { panic("fn blew up") })
	}()
	check("a panic")
	must(t, s.Update(addNode("203.0.113.2"))) // and the lock is free again
}

func TestAnUpdateCannotReachBackIntoTheStore(t *testing.T) {
	s := open(t, t.TempDir())
	var kept *model.State
	must(t, s.Update(func(st *model.State) error {
		kept = st // against the rules, but it must do no harm
		return addNode("203.0.113.1")(st)
	}))
	kept.Nodes[0].Name = "changed behind its back"
	if got := s.Read().Nodes[0].Name; got == "changed behind its back" {
		t.Fatal("a kept pointer reached the published state")
	}
	snap := s.Read()
	snap.Nodes[0].Name = "changed by a reader"
	if got := s.Read().Nodes[0].Name; got == "changed by a reader" {
		t.Fatal("a snapshot shares memory with the store")
	}
}

func TestACorruptFileIsKeptAndNeverReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	junk := []byte(`{"version": 1, "nodes": [`)
	must(t, os.WriteFile(path, junk, 0o600))

	_, err := Open(dir)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if string(readFile(t, path)) != string(junk) {
		t.Fatal("the corrupt file was changed")
	}
	copies, _ := filepath.Glob(path + ".corrupt-*")
	if len(copies) != 1 {
		t.Fatalf("copies = %v, want one", copies)
	}
	if string(readFile(t, copies[0])) != string(junk) {
		t.Fatal("the copy differs from the file")
	}
	if fi, _ := os.Stat(copies[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("the copy is %v, want 0600", fi.Mode().Perm())
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), copies[0]) {
		t.Errorf("the error should name the file and its copy: %v", err)
	}

	// a restart loop must not pile up copies of the same file
	for i := 0; i < 3; i++ {
		if _, err := Open(dir); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("reopening: %v", err)
		}
	}
	if copies, _ = filepath.Glob(path + ".corrupt-*"); len(copies) != 1 {
		t.Fatalf("copies after reopening = %v, want still one", copies)
	}

	// a different broken file gets a copy of its own
	must(t, os.WriteFile(path, []byte("garbage"), 0o600))
	if _, err := Open(dir); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("a second broken file: %v", err)
	}
	if copies, _ = filepath.Glob(path + ".corrupt-*"); len(copies) != 2 {
		t.Fatalf("copies = %v, want two", copies)
	}
}

func TestFilesThatCannotBeUsedAreCorrupt(t *testing.T) {
	good, err := model.Encode(model.NewState())
	must(t, err)
	for name, content := range map[string]string{
		"empty":                      "",
		"not json":                   "hello",
		"no version":                 `{"nodes": []}`,
		"valid json, broken content": strings.Replace(string(good), `"agent_port": 9090`, `"agent_port": 0`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			must(t, os.WriteFile(path, []byte(content), 0o600))
			if _, err := Open(dir); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("err = %v, want ErrCorrupt", err)
			}
			if string(readFile(t, path)) != content {
				t.Fatal("the file was changed")
			}
		})
	}
}

func TestAFileFromANewerReleaseIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	newer := `{"version": 2, "nodes": {"shape": "from the future"}}`
	must(t, os.WriteFile(path, []byte(newer), 0o600))

	_, err := Open(dir)
	if !errors.Is(err, model.ErrNewer) || errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrNewer and not ErrCorrupt", err)
	}
	if string(readFile(t, path)) != newer {
		t.Fatal("the newer file was changed")
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("the folder holds %v; a newer file is not a corrupt one and needs no copy", got)
	}
}

func TestAnUnreadableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	must(t, os.Mkdir(filepath.Join(dir, FileName), 0o700)) // reading a folder fails
	_, err := Open(dir)
	if err == nil || errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want a read error", err)
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("the folder holds %v", got)
	}
}

func TestStrayTempFilesAreRemovedOnOpen(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, ".state.json-123456")
	other := filepath.Join(dir, "keep-me")
	must(t, os.WriteFile(stray, []byte("half a write"), 0o600))
	must(t, os.WriteFile(other, []byte("x"), 0o600))
	open(t, dir)
	if _, err := os.Stat(stray); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the stray temp file is still there: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a file that isn't ours was touched: %v", err)
	}
}

func TestOnlyTheStateFileIsLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	must(t, s.Update(addNode("203.0.113.1")))
	for i := 0; i < 20; i++ {
		must(t, s.Update(func(st *model.State) error { st.Nodes[0].Name = "n" + strconv.Itoa(i); return nil }))
	}
	if got := names(t, dir); len(got) != 1 || got[0] != FileName {
		t.Fatalf("the folder holds %v", got)
	}
}

func TestConcurrentUpdatesDoNotLoseOrDuplicate(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	must(t, s.Update(addNode("203.0.113.1")))
	base := s.Writes()

	const workers, rounds = 6, 8
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(2)
		go func() { // a counter kept in a field: a lost update would leave it short
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				err := s.Update(func(st *model.State) error {
					n, _ := strconv.Atoi(st.Nodes[0].AgentVersion)
					st.Nodes[0].AgentVersion = strconv.Itoa(n + 1)
					return nil
				})
				if err != nil {
					t.Error(err)
				}
			}
		}()
		go func() { // readers run alongside and must always see a whole state
			defer wg.Done()
			for i := 0; i < rounds*4; i++ {
				if st := s.Read(); st.Validate() != nil || len(st.Nodes) != 1 {
					t.Errorf("a reader saw a broken state: %+v", st)
				}
			}
		}()
	}
	wg.Wait()

	if got := s.Read().Nodes[0].AgentVersion; got != strconv.Itoa(workers*rounds) {
		t.Fatalf("counter = %s, want %d: updates were lost", got, workers*rounds)
	}
	if got := s.Writes() - base; got != workers*rounds {
		t.Fatalf("%d writes for %d changes", got, workers*rounds)
	}
	if !same(t, open(t, dir).Read(), s.Read()) {
		t.Fatal("the file and the memory disagree")
	}
}

func TestConcurrentAddsStopAtTheCap(t *testing.T) {
	s := open(t, t.TempDir())
	var wg sync.WaitGroup
	var mu sync.Mutex
	var added, refused int
	for i := 0; i < 2*model.MaxSubscriptions; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Update(addSub("sub " + strconv.Itoa(i)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				added++
			case errors.Is(err, model.ErrSubscriptionLimit):
				refused++
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if added != model.MaxSubscriptions || refused != model.MaxSubscriptions {
		t.Fatalf("%d added and %d refused, want %d each", added, refused, model.MaxSubscriptions)
	}
	if got := s.Read(); len(got.Subscriptions) != model.MaxSubscriptions || got.Validate() != nil {
		t.Fatalf("state: %+v", got)
	}
}
