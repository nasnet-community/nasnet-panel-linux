// SPDX-License-Identifier: AGPL-3.0-or-later

package subscheck

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// A node trusts the CA of the hub that installed it, and a hub that has
// another can't get in: the agent asks for a certificate that CA signed.
func TestAHubWithAnotherCACantReachTheNode(t *testing.T) {
	installer := newHub(t)
	n := startNode(t, installer.mgr.CA(), 1, time.Now())

	stranger := newHub(t)
	if stranger.mgr.CA().Certificate().Equal(installer.mgr.CA().Certificate()) {
		t.Fatal("two hubs made one CA")
	}
	node := stranger.addNode(n)
	stranger.run()
	// it tries and is turned away, and the log says it was the certificate
	eventually(t, "the hub with another CA was never turned away", func() bool {
		return strings.Contains(stranger.log.String(), "node did not answer")
	})
	if log := stranger.log.String(); !strings.Contains(log, "certificate") && !strings.Contains(log, "handshake") {
		t.Errorf("the refusal is not the certificate's:\n%s", log)
	}
	if _, held := stranger.mgr.Client(node.ID); held || stranger.connected(node.ID) {
		t.Fatal("a hub with a CA the node doesn't trust got in")
	}

	// the hub it trusts, given the same node, reaches it
	trusted := installer.addNode(n)
	installer.run()
	eventually(t, "the hub the node trusts never reached it", func() bool { return installer.connected(trusted.ID) })
}

// Backup, wipe the volume, import: the nodes come back and are reached again
// with no SSH login, over the mutual TLS the real agent insists on. Only the CA
// in the backup lets the new hub in, and its clients come from what the backup
// kept, under the same ids, so the links that were handed out still work.
func TestARestoredBackupReconnectsToARealAgentWithoutAnySSH(t *testing.T) {
	ctx := context.Background()

	// the install that makes the backup: the node trusts its CA, and it never runs
	old := newHub(t)
	n := startNode(t, old.mgr.CA(), 1, time.Now())
	node := old.addNode(n)
	_, err := old.mgr.CreateSubscription(ctx, "علی", []uint{node.ID})
	must(t, err)
	_, err = old.mgr.CreateSubscription(ctx, "Sara", []uint{node.ID})
	must(t, err)
	_, err = old.mgr.SetEnabled(ctx, 2, node.ID, false)
	must(t, err)
	ali, _ := clientOf(t, old, 1, node.ID)
	sara, _ := clientOf(t, old, 2, node.ID)
	linkBefore, err := old.mgr.Link(1, node.ID)
	must(t, err)
	_, file, err := old.mgr.ExportBackup(ctx, passphrase)
	must(t, err)
	if got := n.clients(t, 1, 2); len(got) != 1 || got[nodecfg.BridgeEmail(1)] != uuidBridge {
		t.Fatalf("the node holds %v before the restore, want only the bridge's client", got)
	}

	// a hub on a fresh volume, with a CA of its own, which the node doesn't trust
	fresh := newHub(t)
	if fresh.mgr.CA().Certificate().Equal(old.mgr.CA().Certificate()) {
		t.Fatal("the fresh hub has the old CA already")
	}
	fresh.run()
	got, err := fresh.mgr.ImportBackup(ctx, passphrase, file)
	must(t, err)
	if got != 1 {
		t.Errorf("%d nodes came back, want 1", got)
	}
	if !fresh.mgr.CA().Certificate().Equal(old.mgr.CA().Certificate()) {
		t.Fatal("the fresh hub does not use the backup's CA")
	}

	eventually(t, "the restored hub never reached the node", func() bool { return fresh.connected(node.ID) })
	eventually(t, "the node did not get the clients the backup kept", func() bool {
		c := n.clients(t, 1, 2)
		return c[nodecfg.SubscriptionEmail(1)] == ali && c[nodecfg.BridgeEmail(1)] == uuidBridge
	})
	// the one that was off is not on the node, and its id is kept for when it is
	if c := n.clients(t, 2); c[nodecfg.SubscriptionEmail(2)] != "" {
		t.Error("a client that was switched off came back on")
	}
	if id, on := clientOf(t, fresh, 2, node.ID); id != sara || on {
		t.Errorf("the off subscription came back as id %q, on %v", id, on)
	}
	// the link handed out before is the one the restored hub hands out, and it works: the id is on the node
	linkAfter, err := fresh.mgr.Link(1, node.ID)
	must(t, err)
	if linkAfter.Vless != linkBefore.Vless {
		t.Errorf("the link after the restore is %q, was %q", linkAfter.Vless, linkBefore.Vless)
	}
	// and the restored hub can change the node as any hub can
	_, err = fresh.mgr.SetEnabled(ctx, 2, node.ID, true)
	must(t, err)
	eventually(t, "the restored hub could not switch a subscription on", func() bool { return n.clients(t, 2)[nodecfg.SubscriptionEmail(2)] == sara })
}
