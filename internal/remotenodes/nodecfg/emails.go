// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg

import (
	"fmt"
	"strconv"
	"strings"
)

// An email is the only name Xray keeps stats under (user>>>email>>>traffic>>>...)
// and reports online users by, so the hub reads a client's identity back from it.

// SubscriptionEmail is a subscription's name in stats, the same on every node.
// Ids start at 1.
func SubscriptionEmail(id int) string {
	return fmt.Sprintf("sub-%d@remote-nodes", id)
}

// BridgeEmail is the stats name of a node's bridge client. The portal sees the
// bridge as this user's online address for as long as the tunnel is up. It
// follows the panel's naming for hidden reverse logins (rvs-t<id>@reverse), so
// anything that already skips those keeps it out of the subscriptions' traffic.
func BridgeEmail(nodeID int) string {
	return fmt.Sprintf("rvs-t%d@reverse", nodeID)
}

// ParseSubscriptionEmail is the inverse of SubscriptionEmail.
func ParseSubscriptionEmail(email string) (id int, ok bool) {
	s, ok := strings.CutPrefix(email, "sub-")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, "@remote-nodes")
	if !ok {
		return 0, false
	}
	return positiveInt(s)
}

// ParseBridgeEmail is the inverse of BridgeEmail.
func ParseBridgeEmail(email string) (nodeID int, ok bool) {
	s, ok := strings.CutPrefix(email, "rvs-t")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, "@reverse")
	if !ok {
		return 0, false
	}
	return positiveInt(s)
}

// positiveInt accepts only what Sprintf("%d") would have written.
func positiveInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}
