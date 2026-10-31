// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

const backupPass = "correct horse battery staple"

// anInstall is a hub with something to lose: three nodes (one whose install never
// finished), subscriptions in two scripts, one of them switched off somewhere,
// and the CA the manager made.
func anInstall(t testing.TB) (*Manager, Config, *logBuf) {
	t.Helper()
	cfg, log := baseConfig(t)
	n1 := addNode(t, cfg.Store, func(n *model.Node) { n.Name, n.Host = "تهران", "203.0.113.5" })
	n2 := addNode(t, cfg.Store, func(n *model.Node) { n.Name, n.Host, n.EntryPort = "Frankfurt", "node2.example.com", 8443 })
	addNode(t, cfg.Store, func(n *model.Node) { n.State = model.NodeInstalling })
	sara := addSubscription(t, cfg.Store, "Sara")
	ali := addSubscription(t, cfg.Store, "علی")
	enable(t, cfg.Store, sara.ID, n1.ID, true)
	enable(t, cfg.Store, sara.ID, n2.ID, false)
	enable(t, cfg.Store, ali.ID, n1.ID, true)
	m, err := New(cfg)
	must(t, err)
	return m, cfg, log
}

func encoded(t testing.TB, st Store) []byte {
	t.Helper()
	b, err := model.Encode(st.Read())
	must(t, err)
	return b
}

func TestABackupBringsBackNodesSubscriptionsAndTheCAOnAFreshInstall(t *testing.T) {
	ctx := context.Background()
	a, cfgA, _ := anInstall(t)
	name, file, err := a.ExportBackup(ctx, backupPass)
	must(t, err)
	if name != "remote-nodes-backup-2026-10-30.rnbackup" {
		t.Errorf("the file is called %q", name)
	}

	cfgB, _ := baseConfig(t)
	b, err := New(cfgB)
	must(t, err)
	if b.CA().Certificate().Equal(a.CA().Certificate()) {
		t.Fatal("the two installs started with one CA, so the test proves nothing")
	}
	nodes, err := b.ImportBackup(ctx, backupPass, file)
	must(t, err)
	if nodes != 3 {
		t.Errorf("%d nodes came back, want 3", nodes)
	}
	// the state is the other install's, as the file would have it: nodes with
	// their keys, the subscriptions with their client ids, the CA, the counters
	if got, want := encoded(t, cfgB.Store), encoded(t, cfgA.Store); !bytes.Equal(got, want) {
		t.Errorf("the restored state differs from the one exported:\n%s\n---\n%s", got, want)
	}
	st := cfgB.Store.Read()
	if len(st.Nodes) != 3 || len(st.Subscriptions) != 2 || len(st.Enablements) != 3 || st.NextNodeID != 4 || st.NextSubscriptionID != 3 {
		t.Errorf("restored %d nodes, %d subscriptions, %d clients, counters %d and %d", len(st.Nodes), len(st.Subscriptions), len(st.Enablements), st.NextNodeID, st.NextSubscriptionID)
	}
	if e := st.FindEnablement(1, 2); e == nil || e.Enabled || e.ClientUUID != clientIDOf(1, 2) {
		t.Errorf("a client that was off came back as %+v: its id must survive", e)
	}
	// the manager has the restored CA, not the one it made for itself
	if !b.CA().Certificate().Equal(a.CA().Certificate()) || b.CAError() != nil {
		t.Errorf("the manager uses another CA than the backup's: %v", b.CAError())
	}
}

// The file says when it was made, by the day in UTC, and so does the install
// that made it, so the page stops asking for a backup and a restored install
// doesn't start by asking either.
func TestAnExportNamesTheFileByTheUTCDayAndRecordsWhenItWasMade(t *testing.T) {
	ctx := context.Background()
	cfg, _ := baseConfig(t)
	// half past eleven at night in New York, which is the next day in UTC
	clk := newClock(time.Date(2026, 11, 2, 23, 30, 41, 0, time.FixedZone("EST", -5*3600)))
	cfg.Now = clk.Now
	m, err := New(cfg)
	must(t, err)
	if !cfg.Store.Read().LastBackupAt.IsZero() {
		t.Fatal("a backup is recorded before any was made")
	}
	writes := cfg.Store.Writes()

	name, file, err := m.ExportBackup(ctx, backupPass)
	must(t, err)
	at := time.Date(2026, 11, 3, 4, 30, 41, 0, time.UTC)
	if name != "remote-nodes-backup-2026-11-03.rnbackup" {
		t.Errorf("the file is called %q", name)
	}
	if got := cfg.Store.Read().LastBackupAt; !got.Equal(at) || got.Location() != time.UTC {
		t.Errorf("the install says its last backup was %v, want %v", got, at)
	}
	if cfg.Store.Writes() != writes+1 {
		t.Errorf("an export wrote %d times, want once", cfg.Store.Writes()-writes)
	}

	other, _ := baseConfig(t)
	m2, err := New(other)
	must(t, err)
	_, err = m2.ImportBackup(ctx, backupPass, file)
	must(t, err)
	if got := other.Store.Read().LastBackupAt; !got.Equal(at) {
		t.Errorf("the restored install thinks its last backup was %v", got)
	}

	// and the file is not the same twice: a new salt and nonce every time
	_, again, err := m.ExportBackup(ctx, backupPass)
	must(t, err)
	if bytes.Equal(file, again) {
		t.Error("two exports gave the same bytes")
	}
}

func TestABackupThatFailsChangesNothing(t *testing.T) {
	ctx := context.Background()
	a, _, _ := anInstall(t)
	_, file, err := a.ExportBackup(ctx, backupPass)
	must(t, err)

	flipped := bytes.Clone(file)
	flipped[len(flipped)/2] ^= 0xff
	header := bytes.Clone(file)
	header[len("RNBACKUP")+1] ^= 0x01 // the scrypt N, which GCM also covers

	for name, c := range map[string]struct {
		data []byte
		pass string
		want error
	}{
		"a wrong passphrase":     {file, "correct horse battery stapler", backup.ErrBadPassphrase},
		"an empty passphrase":    {file, "", backup.ErrBadPassphrase},
		"a changed byte":         {flipped, backupPass, backup.ErrBadPassphrase},
		"no data":                {nil, backupPass, backup.ErrBadFile},
		"garbage":                {[]byte("this is not a backup, just some text that is long enough to have a header"), backupPass, backup.ErrBadFile},
		"a file cut short":       {file[:30], backupPass, backup.ErrBadFile},
		"a file cut in the body": {file[:len(file)-5], backupPass, backup.ErrBadPassphrase},
		"a file that asks for N": {header, backupPass, backup.ErrBadFile},
		"more than the limit":    {make([]byte, MaxBackupBytes+1), backupPass, backup.ErrBadFile},
	} {
		cfg, _ := baseConfig(t)
		m, err := New(cfg)
		must(t, err)
		was, writes := encoded(t, cfg.Store), cfg.Store.Writes()
		n, err := m.ImportBackup(ctx, c.pass, c.data)
		if !errors.Is(err, c.want) || n != 0 {
			t.Errorf("%s: %d, %v, want %v", name, n, err, c.want)
		}
		if !bytes.Equal(encoded(t, cfg.Store), was) || cfg.Store.Writes() != writes {
			t.Errorf("%s: the state changed", name)
		}
	}
}

func TestAnInstallThatHasNodesOrSubscriptionsRefusesABackup(t *testing.T) {
	ctx := context.Background()
	a, _, _ := anInstall(t)
	_, file, err := a.ExportBackup(ctx, backupPass)
	must(t, err)

	for name, setup := range map[string]func(Store){
		"a node":         func(st Store) { addNode(t, st) },
		"a subscription": func(st Store) { addSubscription(t, st, "Sara") },
		"a node that never finished installing": func(st Store) {
			addNode(t, st, func(n *model.Node) { n.State = model.NodeInstalling })
		},
	} {
		cfg, _ := baseConfig(t)
		setup(cfg.Store)
		m, err := New(cfg)
		must(t, err)
		was, writes := encoded(t, cfg.Store), cfg.Store.Writes()
		if n, err := m.ImportBackup(ctx, backupPass, file); !errors.Is(err, store.ErrNotEmpty) || n != 0 {
			t.Errorf("an install with %s: %d, %v, want ErrNotEmpty", name, n, err)
		}
		if !bytes.Equal(encoded(t, cfg.Store), was) || cfg.Store.Writes() != writes {
			t.Errorf("an install with %s was changed", name)
		}
	}

	// One that had nodes and lost them again is empty, and takes it. The ids it
	// handed out stay handed out: one that is in a certificate somewhere must
	// not go to another node.
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	for i := 0; i < 6; i++ {
		n := addNode(t, cfg.Store)
		must(t, cfg.Store.Update(func(s *model.State) error { return s.RemoveNode(n.ID) }))
	}
	if _, err := m.ImportBackup(ctx, backupPass, file); err != nil {
		t.Fatalf("an install that was emptied: %v", err)
	}
	if st := cfg.Store.Read(); len(st.Nodes) != 3 || st.NextNodeID != 7 {
		t.Errorf("%d nodes and the next id %d, want 3 and 7", len(st.Nodes), st.NextNodeID)
	}
}

func TestAWeakPassphraseMakesNoBackupAndNoRecordOfOne(t *testing.T) {
	a, cfg, _ := anInstall(t)
	writes := cfg.Store.Writes()
	// "ثابت" is eight bytes and four characters, and a passphrase counts in characters
	for _, pass := range []string{"", "short", "seven77", "ثابت"} {
		name, data, err := a.ExportBackup(context.Background(), pass)
		if !errors.Is(err, backup.ErrWeakPassphrase) || name != "" || data != nil {
			t.Errorf("%q: %q, %d bytes, %v, want the weak passphrase refused", pass, name, len(data), err)
		}
	}
	if cfg.Store.Writes() != writes || !cfg.Store.Read().LastBackupAt.IsZero() {
		t.Error("a refused export was recorded as a backup")
	}
	if _, _, err := a.ExportBackup(context.Background(), "سلام سلام"); err != nil {
		t.Errorf("nine characters in Persian: %v", err)
	}
}

// What a backup is sealed with is nobody's business but the owner's: not the
// log's, and not an error's.
func TestThePassphraseIsNeverLoggedOrEchoed(t *testing.T) {
	ctx := context.Background()
	a, _, log := anInstall(t)
	pass := "swordfish-" + "MARKER-PASSPHRASE"
	_, file, err := a.ExportBackup(ctx, pass)
	must(t, err)
	cfgB, logB := baseConfig(t)
	b, err := New(cfgB)
	must(t, err)

	var said []string
	_, err = b.ImportBackup(ctx, pass+"x", file)
	said = append(said, err.Error())
	_, err = b.ImportBackup(ctx, pass, file[:20])
	said = append(said, err.Error())
	_, _, err = a.ExportBackup(ctx, "MARKER-S") // fine, 8 characters
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = a.ExportBackup(ctx, "MARKER")
	said = append(said, err.Error())
	_, err = b.ImportBackup(ctx, pass, file)
	must(t, err)

	for _, s := range said {
		if strings.Contains(s, "MARKER") {
			t.Errorf("an error carries the passphrase: %q", s)
		}
	}
	for _, l := range []*logBuf{log, logB} {
		if strings.Contains(l.String(), "MARKER") || strings.Contains(l.String(), "swordfish") {
			t.Errorf("the log carries a passphrase:\n%s", l)
		}
	}
}

// A hub that has no CA yet, because its clock was too far off to make one, has
// nothing a backup could bring back, and a backup that has none couldn't bring
// anything back either.
func TestABackupWithoutACAIsNeitherMadeNorTaken(t *testing.T) {
	ctx := context.Background()
	cfg, _ := baseConfig(t)
	cfg.Now = newClock(time.Unix(0, 0)).Now
	m, err := New(cfg)
	must(t, err)
	var inv *model.InvalidError
	if _, _, err := m.ExportBackup(ctx, backupPass); !errors.As(err, &inv) || !strings.Contains(err.Error(), "certificate authority") {
		t.Errorf("an export with no CA: %v", err)
	}

	// one made by a build that allowed it, say, or by hand
	bare := model.NewState()
	bare.Subscriptions = []model.Subscription{{ID: 1, Name: "Sara", CreatedAt: epoch}}
	bare.NextSubscriptionID = 2
	snap, err := model.Encode(bare)
	must(t, err)
	file, err := backup.Export(snap, backupPass)
	must(t, err)

	cfg2, _ := baseConfig(t)
	m2, err := New(cfg2)
	must(t, err)
	was := encoded(t, cfg2.Store)
	if _, err := m2.ImportBackup(ctx, backupPass, file); !errors.As(err, &inv) || !strings.Contains(err.Error(), "certificate authority") {
		t.Errorf("an import with no CA: %v", err)
	}
	if !bytes.Equal(encoded(t, cfg2.Store), was) {
		t.Error("the install lost its CA to a backup that has none")
	}
}

// A backup from a newer release opens fine and then can't be used, which is
// said as such and not as a wrong passphrase.
func TestABackupFromANewerReleaseIsNotTakenAndSaysSo(t *testing.T) {
	ctx := context.Background()
	file, err := backup.Export([]byte(`{"version": 99, "somethingNew": true}`), backupPass)
	must(t, err)
	cfg, _ := baseConfig(t)
	m, err := New(cfg)
	must(t, err)
	was := encoded(t, cfg.Store)
	_, err = m.ImportBackup(ctx, backupPass, file)
	if !errors.Is(err, store.ErrBadSnapshot) || !errors.Is(err, model.ErrNewer) {
		t.Errorf("a version 99 backup: %v", err)
	}
	if !bytes.Equal(encoded(t, cfg.Store), was) {
		t.Error("the state changed")
	}

	// sealed rightly but not a state at all
	file, err = backup.Export([]byte(`{"version": 1, "nodes": "none"}`), backupPass)
	must(t, err)
	if _, err := m.ImportBackup(ctx, backupPass, file); !errors.Is(err, store.ErrBadSnapshot) {
		t.Errorf("a backup that is not a state: %v", err)
	}
}

// Backups are one at a time, since each takes 32 MiB of scrypt, and a request
// that is given up while it waits does nothing.
func TestBackupsWaitForEachOtherAndGiveUpWithTheirRequest(t *testing.T) {
	a, cfg, _ := anInstall(t)
	unlock, err := a.lockNodes(context.Background(), backupLock)
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writes := cfg.Store.Writes()
	if _, _, err := a.ExportBackup(ctx, backupPass); !errors.Is(err, context.Canceled) {
		t.Errorf("an export that waited and was given up: %v", err)
	}
	if _, err := a.ImportBackup(ctx, backupPass, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Errorf("an import that waited and was given up: %v", err)
	}
	if cfg.Store.Writes() != writes {
		t.Error("an export that was given up wrote the state")
	}
	unlock()
	if _, _, err := a.ExportBackup(context.Background(), backupPass); err != nil {
		t.Errorf("after the lock was let go: %v", err)
	}
}
