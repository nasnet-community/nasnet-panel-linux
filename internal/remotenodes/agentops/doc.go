// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentops installs nasnet-agent on a node and keeps it up to date:
// the first install over SSH, then everything else over the agent's own
// gRPC connection. A login is used for one job and never kept.
package agentops
