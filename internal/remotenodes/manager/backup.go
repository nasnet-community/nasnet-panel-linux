// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The API exports and imports backups through the manager.
var _ api.BackupOps = (*Manager)(nil)

// MaxBackupBytes is the largest backup file ImportBackup looks at. A real one
// is a few KB, the state of three nodes and ten subscriptions.
const MaxBackupBytes = 4 << 20

// backupLock is the slot of applyLocks that keeps backups to one at a time.
const backupLock uint = 0

// ExportBackup seals the state under a passphrase and says what to call the
// file. The state holds the CA every node trusts, so this is what makes losing
// the router survivable, and the passphrase is all that keeps the file safe.
//
// The file records the time it was made, which is also what the page shows as
// the last backup, so that an install restored from it doesn't start by asking
// for one.
func (m *Manager) ExportBackup(ctx context.Context, passphrase string) (name string, data []byte, err error) {
	unlock, err := m.lockNodes(ctx, backupLock)
	if err != nil {
		return "", nil, err
	}
	defer unlock()

	at := m.now().UTC().Truncate(time.Second)
	st := m.cfg.Store.Read()
	if st.CACert == "" || st.CAKey == "" {
		// the clock was too far off to make one; a file without it brings nothing back
		return "", nil, &model.InvalidError{Msg: "there is no certificate authority yet, so there is nothing to back up"}
	}
	st.LastBackupAt = at
	// The usage counters live beside the state, not in it, and an install the
	// file is restored on starts them at zero: each subscription takes what it
	// has used with it, so its limit goes on from there.
	for i := range st.Subscriptions {
		sub := &st.Subscriptions[i]
		sub.TrafficCarried, sub.TrafficBase = m.trafficUsed(*sub), 0
	}
	snapshot, err := model.Encode(st)
	if err != nil {
		return "", nil, err
	}
	defer clear(snapshot)
	data, err = backup.Export(snapshot, passphrase)
	if err != nil {
		return "", nil, err
	}
	// The file is good whatever happens now; only the note of when is at stake.
	if err := m.cfg.Store.Update(func(s *model.State) error { s.LastBackupAt = at; return nil }); err != nil {
		m.log.Warn("the time of the backup could not be saved", "err", err)
	}
	m.log.Info("backup exported", "bytes", len(data))
	return "remote-nodes-backup-" + at.Format(time.DateOnly) + ".rnbackup", data, nil
}

// ImportBackup opens a backup and puts its state in place of an install's that
// has no nodes and no subscriptions, and returns how many nodes came back. The
// restored CA and nodes are picked up at once, and the nodes are connected to
// the way any node is, with the certificates the CA signed and no SSH login.
//
// Errors from the backup package say what was wrong with the file or the
// passphrase, and store.ErrNotEmpty that the install is not a fresh one. In
// each case nothing was changed.
func (m *Manager) ImportBackup(ctx context.Context, passphrase string, data []byte) (nodes int, err error) {
	// the size is judged before scrypt, which is the expensive part
	if len(data) > MaxBackupBytes {
		return 0, fmt.Errorf("%w: the file is larger than %d MiB", backup.ErrBadFile, MaxBackupBytes>>20)
	}
	unlock, err := m.lockNodes(ctx, backupLock)
	if err != nil {
		return 0, err
	}
	defer unlock()

	snapshot, err := backup.Import(data, passphrase)
	if err != nil {
		return 0, err
	}
	defer clear(snapshot)
	// What the file holds is the store's to judge, apart from this: a state with
	// no CA would only come back from there as "an edit dropped the CA".
	restored, decodeErr := model.Decode(snapshot)
	if decodeErr == nil && restored.CACert == "" {
		return 0, &model.InvalidError{Msg: "this backup has no certificate authority, so it could not bring any node back"}
	}
	if err := m.cfg.Store.Restore(snapshot); err != nil {
		return 0, err
	}
	// the CA first, then a loop for each node that came back
	m.Sync()
	m.log.Info("backup restored", "nodes", len(restored.Nodes), "subscriptions", len(restored.Subscriptions))
	return len(restored.Nodes), nil
}
