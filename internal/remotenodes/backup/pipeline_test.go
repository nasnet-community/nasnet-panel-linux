// SPDX-License-Identifier: AGPL-3.0-or-later

package backup_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/backup"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/store"
)

// The whole way a backup goes: one install's state sealed under a passphrase,
// opened, and restored onto a fresh install, where the nodes and the links are
// as they were.
func TestAnInstallComesBackFromItsBackup(t *testing.T) {
	const passphrase = "correct horse battery staple"
	const privateKey = "priv-secret-REALITY"
	const clientID = "6f1c1c1e-2b3a-4c5d-8e9f-0a1b2c3d4e5f"
	const bridgeID = "0b9a8f7e-6d5c-4b3a-9281-706f5e4d3c2b"
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

	old, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Update(func(st *model.State) error {
		n, err := st.AddNode(model.Node{
			Name: "تهران", Host: "203.0.113.9", SSHPort: 22, AgentPort: 9090, EntryPort: 443, State: model.NodeRegistered, CreatedAt: at,
			RealityPublicKey: "pub", RealityShortID: "0123abcd", RealityPrivateKey: privateKey, SSHHostKey: "SHA256:pinned",
			BridgeUUID: bridgeID,
		})
		if err != nil {
			return err
		}
		sub, err := st.AddSubscription(model.Subscription{Name: "علی", CreatedAt: at})
		if err != nil {
			return err
		}
		st.Enablements = append(st.Enablements, model.Enablement{SubscriptionID: sub.ID, NodeID: n.ID, ClientUUID: clientID, Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := old.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	file, err := backup.Export(snap, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{privateKey, clientID, bridgeID, "SHA256:pinned", "203.0.113.9"} {
		if bytes.Contains(file, []byte(secret)) {
			t.Errorf("%q is in the backup file as it is", secret)
		}
	}

	// the volume is wiped, and the backup goes onto what is a fresh install
	fresh, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := backup.Import(file, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Restore(opened); err != nil {
		t.Fatal(err)
	}
	after, err := fresh.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, snap) {
		t.Fatalf("the restored install differs from the one that was backed up:\n%s\n%s", after, snap)
	}
	// without it the bridge couldn't log in to the node it was restored with
	if got := fresh.Read(); len(got.Nodes) != 1 || got.Nodes[0].BridgeUUID != bridgeID {
		t.Fatalf("the restored node lost its bridge client id: %+v", got.Nodes)
	}

	// importing again is no way to wipe an install that is in use
	if err := fresh.Restore(opened); !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("a second restore: %v, want ErrNotEmpty", err)
	}
}
