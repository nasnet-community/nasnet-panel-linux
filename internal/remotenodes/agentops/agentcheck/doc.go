// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentcheck runs the real agent, in this process, against the hub's
// operations: Connect, the Xray and config pushes, the Reality target
// checks, the certificate renewal. It holds only tests.
//
// It lives apart from agentops so that the agent's heavy imports (Xray,
// sing-box) are linked only here, and agentops itself stays light, which its
// own tests hold it to. Nothing outside tests imports this package.
//
// The agent has one thing a test build can't pass: PushXrayBinary insists that
// what it is sent is an ELF file for the host, and the check can't be set from
// another package. The pushes of an Xray build are tested against a stand-in
// agent in agentops itself, and here only as far as that check.
package agentcheck
