// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentcheck holds one test file, which runs the real agent server
// and the real agent client against the certificates package pki issues.
//
// It lives apart from pki so that the agent's heavy imports (Xray, sing-box)
// are linked only here, and pki's own tests stay quick. Nothing outside
// tests imports this package.
package agentcheck
