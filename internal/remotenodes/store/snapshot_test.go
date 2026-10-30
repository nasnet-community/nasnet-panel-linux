// SPDX-License-Identifier: AGPL-3.0-or-later

package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

const uuidB = "0b9a8f7e-6d5c-4b3a-9281-706f5e4d3c2b"

// populate gives s two nodes and two subscriptions, one of them switched on
// for both nodes, with secrets in the records.
func populate(t *testing.T, s *Store) {
	t.Helper()
	must(t, s.Update(func(st *model.State) error {
		var ids []uint
		for _, h := range []string{"203.0.113.1", "203.0.113.2"} {
			n, err := st.AddNode(model.Node{
				Name: "node " + h, Host: h, SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: t0,
				RealityPublicKey: "pub", RealityShortID: "0123abcd", RealityPrivateKey: "priv-secret-" + h, SSHHostKey: "SHA256:pinned-" + h,
			})
			if err != nil {
				return err
			}
			ids = append(ids, n.ID)
		}
		ali, err := st.AddSubscription(model.Subscription{Name: "علی", CreatedAt: t0})
		if err != nil {
			return err
		}
		if _, err := st.AddSubscription(model.Subscription{Name: "sara", CreatedAt: t0}); err != nil {
			return err
		}
		st.Enablements = []model.Enablement{
			{SubscriptionID: ali.ID, NodeID: ids[0], ClientUUID: uuidA, Enabled: true},
			{SubscriptionID: ali.ID, NodeID: ids[1], ClientUUID: uuidB, Enabled: false},
		}
		st.LastBackupAt = t0
		return nil
	}))
}

func snapshot(t *testing.T, s *Store) []byte {
	t.Helper()
	b, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestASnapshotIsWhatTheFileHolds(t *testing.T) {
	s := open(t, t.TempDir())
	populate(t, s)
	snap := snapshot(t, s)
	if !bytes.Equal(snap, readFile(t, s.Path())) {
		t.Fatal("the snapshot is not the bytes of state.json")
	}
	// all of it, the secrets included: a backup that left them out would be of no use
	for _, want := range []string{"priv-secret-203.0.113.1", "SHA256:pinned-203.0.113.2", uuidA, uuidB, "علی"} {
		if !strings.Contains(string(snap), want) {
			t.Errorf("the snapshot doesn't hold %q", want)
		}
	}
	if got, err := model.Decode(snap); err != nil || !same(t, got, s.Read()) {
		t.Fatalf("the snapshot doesn't decode to the state: %v", err)
	}

	// it is the caller's own copy
	snap[0] = 'X'
	if !bytes.Equal(snapshot(t, s), readFile(t, s.Path())) {
		t.Fatal("changing a snapshot changed the store's")
	}
}

func TestASnapshotOfAFreshStoreWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	snap := snapshot(t, s)
	want, err := model.Encode(model.NewState())
	must(t, err)
	if !bytes.Equal(snap, want) {
		t.Fatalf("snapshot of a fresh store:\n%s", snap)
	}
	if _, err := os.Stat(s.Path()); !errors.Is(err, fs.ErrNotExist) || s.Writes() != 0 {
		t.Fatalf("taking a snapshot wrote (%d writes, stat: %v)", s.Writes(), err)
	}
}

func TestASnapshotRestoresIntoAFreshInstall(t *testing.T) {
	a := open(t, t.TempDir())
	populate(t, a)
	snap := snapshot(t, a)

	dir := t.TempDir()
	b := open(t, dir)
	must(t, b.Restore(snap))
	if !same(t, b.Read(), a.Read()) {
		t.Fatalf("restored:\n got %+v\nwant %+v", b.Read(), a.Read())
	}
	if b.Writes() != 1 {
		t.Errorf("writes = %d, want one for one restore", b.Writes())
	}
	if !bytes.Equal(readFile(t, b.Path()), snap) {
		t.Error("what was written is not what was backed up")
	}
	fi, err := os.Stat(b.Path())
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state.json is %v, want 0600", fi.Mode().Perm())
	}
	if got := names(t, dir); len(got) != 1 || got[0] != FileName {
		t.Errorf("the folder holds %v", got)
	}

	// and it is what a restart finds, with the next ids picking up where the backup left off
	again := open(t, dir)
	if !same(t, again.Read(), a.Read()) {
		t.Fatal("the restored state is not what a restart reads")
	}
	must(t, again.Update(addNode("203.0.113.3")))
	if got := again.Read().Nodes[2].ID; got != 3 {
		t.Errorf("the next node got id %d, want 3", got)
	}
}

func TestRestoreRefusesAnInstallThatHasSomething(t *testing.T) {
	a := open(t, t.TempDir())
	populate(t, a)
	snap := snapshot(t, a)

	for name, setup := range map[string]func(*model.State) error{
		"a node":         addNode("203.0.113.9"),
		"a subscription": addSub("someone"),
		"both": func(st *model.State) error {
			if err := addNode("203.0.113.9")(st); err != nil {
				return err
			}
			return addSub("someone")(st)
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := open(t, t.TempDir())
			must(t, b.Update(setup))
			state, file, writes := b.Read(), readFile(t, b.Path()), b.Writes()

			err := b.Restore(snap)
			if !errors.Is(err, ErrNotEmpty) || errors.Is(err, ErrBadSnapshot) {
				t.Fatalf("err = %v, want ErrNotEmpty", err)
			}
			if !same(t, b.Read(), state) || b.Writes() != writes || !bytes.Equal(readFile(t, b.Path()), file) {
				t.Fatal("a refused restore changed the install")
			}
		})
	}

	// emptied again, it can be restored into
	b := open(t, t.TempDir())
	must(t, b.Update(addNode("203.0.113.9")))
	must(t, b.Update(addSub("someone")))
	if err := b.Restore(snap); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v", err)
	}
	must(t, b.Update(func(st *model.State) error { return st.RemoveNode(1) }))
	must(t, b.Update(func(st *model.State) error { return st.RemoveSubscription(1) }))
	must(t, b.Restore(snap))
	if got := b.Read(); len(got.Nodes) != 2 || len(got.Subscriptions) != 2 || got.Validate() != nil {
		t.Fatalf("after an emptied install was restored: %+v", got)
	}
}

func TestRestoreKeepsTheIdCountersFromGoingBack(t *testing.T) {
	a := open(t, t.TempDir())
	must(t, a.Update(addNode("203.0.113.1")))
	must(t, a.Update(addSub("ali")))
	snap := snapshot(t, a) // counters at 2 and 2

	// an install that handed out three ids of each, then lost them again
	b := open(t, t.TempDir())
	for i := 1; i <= 3; i++ {
		must(t, b.Update(addNode(fmt.Sprintf("203.0.113.%d", 10+i))))
		must(t, b.Update(addSub("sub "+strconv.Itoa(i))))
	}
	for i := uint(1); i <= 3; i++ {
		must(t, b.Update(func(st *model.State) error { return st.RemoveNode(i) }))
		must(t, b.Update(func(st *model.State) error { return st.RemoveSubscription(i) }))
	}
	if got := b.Read(); len(got.Nodes) != 0 || got.NextNodeID != 4 || got.NextSubscriptionID != 4 {
		t.Fatalf("setup: %+v", got)
	}

	must(t, b.Restore(snap))
	got := b.Read()
	if len(got.Nodes) != 1 || len(got.Subscriptions) != 1 {
		t.Fatalf("the backup's records are not all there: %+v", got)
	}
	if got.NextNodeID != 4 || got.NextSubscriptionID != 4 {
		t.Fatalf("counters %d and %d: ids 1 to 3 were given out here and must not come back", got.NextNodeID, got.NextSubscriptionID)
	}
	must(t, b.Update(addNode("203.0.113.99")))
	if n := b.Read().Nodes[1]; n.ID != 4 {
		t.Fatalf("the next node got id %d, want 4", n.ID)
	}
}

func TestRestoreRefusesWhatItCannotUse(t *testing.T) {
	a := open(t, t.TempDir())
	populate(t, a)
	good := string(snapshot(t, a))

	over := model.NewState()
	for i := 1; i <= model.MaxNodes+1; i++ {
		over.Nodes = append(over.Nodes, model.Node{
			ID: uint(i), Name: "n" + strconv.Itoa(i), Host: fmt.Sprintf("203.0.113.%d", i),
			SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: t0,
		})
	}
	over.NextNodeID = model.MaxNodes + 2
	overBytes, err := model.Encode(over)
	must(t, err)

	// a change that isn't there to make would leave a good snapshot, and the test would prove nothing
	edit := func(old, with string) string {
		if !strings.Contains(good, old) {
			t.Fatalf("the snapshot has no %q to change", old)
		}
		return strings.Replace(good, old, with, 1)
	}
	for name, c := range map[string]struct {
		data string
		msg  string // what the error says, so each case is refused for the reason it is there for
		is   error  // besides ErrBadSnapshot
	}{
		"nothing":                {"", "unreadable", nil},
		"not json":               {"hello", "unreadable", nil},
		"cut off":                {good[:len(good)/2], "unreadable", nil},
		"no version":             {`{"nodes": []}`, "no version", nil},
		"a newer release's":      {`{"version": 2, "nodes": {"shape": "from the future"}}`, "newer", model.ErrNewer},
		"a port that can't be":   {edit(`"ssh_port": 22`, `"ssh_port": 0`), "ssh port", nil},
		"a client with no node":  {edit(`"node_id": 2`, `"node_id": 7`), "doesn't exist", nil},
		"a client id that isn't": {edit(uuidA, "not-a-uuid"), "UUID", nil},
		"an id counter at zero":  {edit(`"next_node_id": 3`, `"next_node_id": 0`), "counter", nil},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			b := open(t, dir)
			err := b.Restore([]byte(c.data))
			if !errors.Is(err, ErrBadSnapshot) || (c.is != nil && !errors.Is(err, c.is)) || !strings.Contains(err.Error(), c.msg) {
				t.Fatalf("err = %v, want ErrBadSnapshot saying %q", err, c.msg)
			}
			if got := names(t, dir); len(got) != 0 || b.Writes() != 0 || len(b.Read().Nodes) != 0 {
				t.Fatalf("a refused restore left %v, %d writes, %d nodes", got, b.Writes(), len(b.Read().Nodes))
			}
		})
	}

	// a state that is fine but holds more than this build allows is turned away by the caps
	dir := t.TempDir()
	b := open(t, dir)
	if err := b.Restore(overBytes); !errors.Is(err, model.ErrNodeLimit) {
		t.Fatalf("err = %v, want ErrNodeLimit", err)
	}
	if got := names(t, dir); len(got) != 0 || b.Writes() != 0 {
		t.Fatalf("a refused restore left %v, %d writes", got, b.Writes())
	}
}

func TestRestoringWhatIsAlreadyThereWritesNothing(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	must(t, s.Restore(snapshot(t, s))) // an empty install into an empty install
	if _, err := os.Stat(s.Path()); !errors.Is(err, fs.ErrNotExist) || s.Writes() != 0 {
		t.Fatalf("a restore that changed nothing wrote (%d writes, stat: %v)", s.Writes(), err)
	}
}

func TestAFailedRestoreLeavesTheInstallAsItWas(t *testing.T) {
	a := open(t, t.TempDir())
	populate(t, a)
	snap := snapshot(t, a)

	for name, breakIt := range map[string]func(*Store){
		"rename": func(s *Store) { s.ops.rename = func(string, string) error { return errDisk } },
		"sync": func(s *Store) {
			breakTemp(s, func(f *fakeFile) { f.sync = func() error { return errDisk } })
		},
		"half a write": func(s *Store) {
			breakTemp(s, func(f *fakeFile) {
				f.write = func(p []byte) (int, error) {
					n, _ := f.tempFile.Write(p[:len(p)/2])
					return n, errDisk
				}
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			// an install that has had a node and lost it: there is a file, and nothing in it
			s := open(t, t.TempDir())
			must(t, s.Update(addNode("203.0.113.9")))
			must(t, s.Update(func(st *model.State) error { return st.RemoveNode(1) }))
			breakIt(s)
			survives(t, s, func() error { return s.Restore(snap) })

			s.ops = osOps()
			must(t, s.Restore(snap))
			if !same(t, open(t, filepath.Dir(s.path)).Read(), a.Read()) {
				t.Fatal("once the disk works again, the restore didn't land")
			}
		})
	}

	t.Run("the first write of all", func(t *testing.T) {
		dir := t.TempDir()
		s := open(t, dir)
		s.ops.rename = func(string, string) error { return errDisk }
		if err := s.Restore(snap); !errors.Is(err, errDisk) {
			t.Fatalf("err = %v", err)
		}
		if got := names(t, dir); len(got) != 0 || s.Writes() != 0 || len(s.Read().Nodes) != 0 {
			t.Fatalf("the folder holds %v, %d writes, %d nodes", got, s.Writes(), len(s.Read().Nodes))
		}
	})
}

// The check that the install is empty and the write are one step, so of
// several restores at once exactly one lands.
func TestOnlyOneOfSeveralRestoresLands(t *testing.T) {
	a := open(t, t.TempDir())
	populate(t, a)
	snap := snapshot(t, a)

	s := open(t, t.TempDir())
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Restore(snap)
		}()
	}
	wg.Wait()

	var landed, refused int
	for _, err := range errs {
		switch {
		case err == nil:
			landed++
		case errors.Is(err, ErrNotEmpty):
			refused++
		default:
			t.Errorf("err = %v", err)
		}
	}
	if landed != 1 || refused != n-1 || s.Writes() != 1 {
		t.Fatalf("%d landed, %d refused, %d writes; want 1, %d and 1", landed, refused, s.Writes(), n-1)
	}
	if !same(t, s.Read(), a.Read()) {
		t.Fatal("the state is not the backup's")
	}
}

// A snapshot taken while updates land is a whole state, never half of one.
func TestSnapshotsDuringUpdatesAreWhole(t *testing.T) {
	s := open(t, t.TempDir())
	populate(t, s)
	var wg sync.WaitGroup
	for w := 0; w < 3; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				must(t, s.Update(func(st *model.State) error { st.Nodes[0].AgentVersion = strconv.Itoa(i); return nil }))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				snap, err := s.Snapshot()
				if err != nil {
					t.Error(err)
					return
				}
				if st, err := model.Decode(snap); err != nil || len(st.Nodes) != 2 || len(st.Enablements) != 2 {
					t.Errorf("a snapshot that isn't a whole state: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
