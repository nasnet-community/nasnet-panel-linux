// SPDX-License-Identifier: AGPL-3.0-or-later

package subscheck

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/manager"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// clientOf is the id the state keeps for a subscription's client on a node.
func clientOf(t *testing.T, h *hub, sub, node uint) (id string, on bool) {
	t.Helper()
	st := h.st.Read()
	e := st.FindEnablement(sub, node)
	if e == nil {
		t.Fatalf("the state has no client for subscription %d on node %d", sub, node)
	}
	return e.ClientUUID, e.Enabled
}

func parseLink(t *testing.T, link string) (id, host, port, label string, q url.Values) {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "vless" || u.User == nil {
		t.Fatalf("%q is not a vless link: %v", link, err)
	}
	return u.User.Username(), u.Hostname(), u.Port(), u.Fragment, u.Query()
}

// A subscription is made, switched off and on, has its link replaced and is
// deleted, and the real agent does each of it to the real Xray behind it: the
// client is there under the id the state keeps and the link carries, gone when
// it is off, back under the same id when it is on again, under a new one when
// the link is replaced, and gone for good when the subscription is. The
// bridge's own client, which carries the reverse tag, is never touched.
func TestASubscriptionIsSwitchedAndItsLinkReplacedThroughARealAgent(t *testing.T) {
	h := newHub(t)
	n := startNode(t, h.mgr.CA(), 1, time.Now())
	node := h.addNode(n)
	h.run()
	ctx := context.Background()
	eventually(t, "the manager never reached the node", func() bool { return h.connected(node.ID) })
	if got := n.clients(t); len(got) != 1 || got[nodecfg.BridgeEmail(1)] != uuidBridge {
		t.Fatalf("the node starts with %v, want only the bridge's client", got)
	}
	bridge := func() {
		t.Helper()
		if got := n.clients(t)[nodecfg.BridgeEmail(1)]; got != uuidBridge {
			t.Fatalf("the bridge's client is %q, want it untouched", got)
		}
	}
	email := nodecfg.SubscriptionEmail(1)

	// made, with a Persian name, on the node that answers
	const name = "علی‌رضا کریمی"
	v, err := h.mgr.CreateSubscription(ctx, name, []uint{node.ID})
	must(t, err)
	if v.ID != 1 || len(v.Nodes) != 1 || !v.Nodes[0].Enabled {
		t.Fatalf("created = %+v", v)
	}
	first, on := clientOf(t, h, 1, node.ID)
	if !on {
		t.Fatal("the state says the new subscription is off")
	}
	eventually(t, "the node never got the client", func() bool { return n.clients(t, 1)[email] == first })
	bridge()

	// the link is what a client needs to get in: the id Xray holds, the entry's
	// port and the node's REALITY values, and the name comes out as typed
	link, err := h.mgr.Link(1, node.ID)
	must(t, err)
	id, host, port, label, q := parseLink(t, link.Vless)
	if id != first || host != "127.0.0.1" || port != strconv.Itoa(n.entryPort) || label != name ||
		q.Get("pbk") != n.publicKey || q.Get("sid") != "a1b2c3d4" || q.Get("sni") != "www.example.com" || q.Get("security") != "reality" {
		t.Errorf("link %s", link.Vless)
	}
	if got := n.clients(t, 1)[email]; got != id {
		t.Errorf("Xray holds %q, the link carries %q", got, id)
	}

	// off removes the client and the link; on gives the same ones back
	_, err = h.mgr.SetEnabled(ctx, 1, node.ID, false)
	must(t, err)
	eventually(t, "the client stayed on the node after the switch went off", func() bool { return n.clients(t, 1)[email] == "" })
	if _, err := h.mgr.Link(1, node.ID); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Errorf("a link for a switch that is off: %v", err)
	}
	if again, on := clientOf(t, h, 1, node.ID); again != first || on {
		t.Errorf("after off the state holds %q (on %v), want the same id and off", again, on)
	}
	_, err = h.mgr.SetEnabled(ctx, 1, node.ID, true)
	must(t, err)
	eventually(t, "the client did not come back", func() bool { return n.clients(t, 1)[email] == first })
	if again, err := h.mgr.Link(1, node.ID); err != nil || again.Vless != link.Vless {
		t.Errorf("the link after off and on is %q (%v), was %q", again.Vless, err, link.Vless)
	}
	bridge()

	// replaced: the old id stops working at once and the new one takes its place
	fresh, err := h.mgr.ReplaceLink(ctx, 1, node.ID)
	must(t, err)
	newID, _, _, newLabel, _ := parseLink(t, fresh.Vless)
	if newID == first || newLabel != name {
		t.Errorf("replaced link %s, old id %s", fresh.Vless, first)
	}
	if got := n.clients(t, 1)[email]; got != newID {
		t.Errorf("Xray holds %q after the replacement, want the new id %q", got, newID)
	}
	if got, _ := clientOf(t, h, 1, node.ID); got != newID {
		t.Errorf("the state holds %q, want the new id", got)
	}
	bridge()

	// a second subscription is its own, and deleting the first leaves it alone
	_, err = h.mgr.CreateSubscription(ctx, "Sara", []uint{node.ID})
	must(t, err)
	second, _ := clientOf(t, h, 2, node.ID)
	eventually(t, "the second subscription never got its client", func() bool { return n.clients(t, 2)[nodecfg.SubscriptionEmail(2)] == second })
	must(t, h.mgr.DeleteSubscription(ctx, 1))
	eventually(t, "a deleted subscription's client is still on the node", func() bool {
		got := n.clients(t, 1, 2)
		return got[email] == "" && got[nodecfg.SubscriptionEmail(2)] == second
	})
	bridge()

	// what the manager did to the node was everything the managed profile allows,
	// and nothing it refused
	for _, bad := range []string{"PermissionDenied", "permission denied", "could not all be reconciled", "level=ERROR", "did not go through"} {
		if strings.Contains(h.log.String(), bad) {
			t.Errorf("the log has %q:\n%s", bad, h.log)
		}
	}
	// the events are on the node's list, oldest first
	var kinds []string
	for _, e := range h.mgr.NodeEvents(node.ID) {
		kinds = append(kinds, e.Kind)
	}
	slices.Reverse(kinds)
	var subs []string
	for _, k := range kinds {
		if k == manager.EventSubscriptionOn || k == manager.EventSubscriptionOff {
			subs = append(subs, k)
		}
	}
	want := []string{manager.EventSubscriptionOn, manager.EventSubscriptionOff, manager.EventSubscriptionOn, manager.EventSubscriptionOn, manager.EventSubscriptionOn, manager.EventSubscriptionOff}
	if !slices.Equal(subs, want) {
		t.Errorf("the subscription events are %v, want %v", subs, want)
	}
}

// The node's agent goes away and comes back. What is switched while it is gone is
// the state's wish, and the reconcile on the way back makes the real Xray match
// it. A link can't be replaced meanwhile, since nothing could cut the old one.
func TestWhatIsSwitchedWhileARealAgentIsAwayIsAppliedWhenItIsBack(t *testing.T) {
	h := newHub(t)
	n := startNode(t, h.mgr.CA(), 1, time.Now())
	node := h.addNode(n)
	h.run()
	ctx := context.Background()
	eventually(t, "the manager never reached the node", func() bool { return h.connected(node.ID) })
	_, err := h.mgr.CreateSubscription(ctx, "Sara", []uint{node.ID})
	must(t, err)
	_, err = h.mgr.CreateSubscription(ctx, "Ali", nil)
	must(t, err)
	sara, _ := clientOf(t, h, 1, node.ID)
	eventually(t, "Sara never got her client", func() bool { return n.clients(t, 1)[nodecfg.SubscriptionEmail(1)] == sara })

	// the agent goes away while the manager still holds its connection
	n.stopAgent()
	_, err = h.mgr.SetEnabled(ctx, 1, node.ID, false)
	must(t, err)
	_, err = h.mgr.SetEnabled(ctx, 2, node.ID, true)
	must(t, err)
	ali, on := clientOf(t, h, 2, node.ID)
	if !on {
		t.Fatal("the state does not hold the switch")
	}
	// Xray is still up behind the dead agent, and nothing reached it
	if got := n.clients(t, 1, 2); got[nodecfg.SubscriptionEmail(1)] != sara || got[nodecfg.SubscriptionEmail(2)] != "" {
		t.Fatalf("the node holds %v while its agent is down", got)
	}
	var refusal *api.Error
	if _, err := h.mgr.ReplaceLink(ctx, 2, node.ID); !errors.As(err, &refusal) || refusal.Code != api.CodeNodeUnreachable {
		t.Errorf("replacing a link while the agent is down: %v", err)
	}
	if got, _ := clientOf(t, h, 2, node.ID); got != ali {
		t.Error("a refused replacement changed the id")
	}

	n.restartAgent(t)
	eventually(t, "the node did not take the switches once it was back", func() bool {
		got := n.clients(t, 1, 2)
		return got[nodecfg.SubscriptionEmail(1)] == "" && got[nodecfg.SubscriptionEmail(2)] == ali
	})
	if got := n.clients(t)[nodecfg.BridgeEmail(1)]; got != uuidBridge {
		t.Errorf("the bridge's client is %q", got)
	}
	eventually(t, "the manager never reached the node again", func() bool { return h.connected(node.ID) })
	// and with the agent back a link can be replaced again
	fresh, err := h.mgr.ReplaceLink(ctx, 2, node.ID)
	must(t, err)
	if id, _, _, _, _ := parseLink(t, fresh.Vless); id == ali || n.clients(t, 2)[nodecfg.SubscriptionEmail(2)] != id {
		t.Errorf("the link after the agent is back: %s", fresh.Vless)
	}
}
