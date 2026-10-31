// SPDX-License-Identifier: AGPL-3.0-or-later

// Package subscheck runs the manager's subscription and backup calls against a
// real agent: the agent's own server with mutual TLS and the managed profile, in
// front of a real Xray with the entry inbound a node has, and the agent's own
// client, made from certificates the manager's CA issued.
//
// It is a package of its own for the reason agentcheck is: the agent's heavy
// imports are linked here only, so the manager's tests stay quick and its
// dependency check stays true. Nothing outside tests imports it.
package subscheck
