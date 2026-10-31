// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"slices"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// What goes into a node's Xray config besides the node's own settings: the
// clients of the subscriptions that are switched on there, and the client the
// router's bridge logs in as. The state is where both are kept, so the config
// is built from it each time and can't drift from what the links say.

// bridgeOf is the client the bridge logs in as on a node. Its id is made when
// the node is added, and its name and reverse tag follow from the node's id.
func bridgeOf(n model.Node) nodecfg.Bridge {
	return nodecfg.Bridge{
		UUID:       n.BridgeUUID,
		Email:      nodecfg.BridgeEmail(int(n.ID)),
		ReverseTag: nodecfg.BridgeReverseTag(int(n.ID)),
	}
}

// clientsOf are the subscriptions switched on at a node, as clients, in the
// order of their ids so that the same state gives the same config.
func clientsOf(st model.State, nodeID uint) []nodecfg.Client {
	var on []model.Enablement
	for _, e := range st.Enablements {
		if e.NodeID == nodeID && e.Enabled {
			on = append(on, e)
		}
	}
	slices.SortFunc(on, func(a, b model.Enablement) int { return int(a.SubscriptionID) - int(b.SubscriptionID) })
	out := make([]nodecfg.Client, len(on))
	for i, e := range on {
		out[i] = nodecfg.Client{UUID: e.ClientUUID, Email: nodecfg.SubscriptionEmail(int(e.SubscriptionID))}
	}
	return out
}
