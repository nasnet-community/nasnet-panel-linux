// SPDX-License-Identifier: AGPL-3.0-or-later

package api

// Link is a subscription's way in at one node: the vless:// address to hand
// over. It carries the client's id, so it and the backup file are the only
// answers that hold a secret, and neither is ever logged.
type Link struct {
	NodeID   uint   `json:"nodeId"`
	NodeName string `json:"nodeName"`
	Vless    string `json:"vless"`
}
