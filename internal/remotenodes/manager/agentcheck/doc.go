// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentcheck runs the manager against a real agent: the agent's own
// server with mutual TLS and the managed profile, in front of a real Xray,
// and the agent's own client, built from certificates the manager's CA
// issued.
//
// It lives apart from the manager so that the agent's heavy imports (Xray,
// sing-box) are linked only here, and the manager's own tests stay quick and
// its dependency check stays true. Nothing outside tests imports this package.
package agentcheck
