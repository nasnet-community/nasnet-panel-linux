// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agentops installs nasnet-agent on a node and keeps it up to date:
// the first install over SSH, then everything else over the agent's own
// gRPC connection. A login is used for one job and never kept.
//
// It is the operations on one node and nothing else. It doesn't touch the
// store or the job engine: the manager calls these from its jobs, each
// operation takes what it needs, reports its steps through a Progress, and
// returns an error that errors.Is can match to what went wrong.
//
// Everything bound for a node leaves from one address, Env.Local, which the
// router sends to the Domestic link: SSH, the gRPC link, and the name lookups
// for both go through one dialer.
//
// Over SSH (the first install, and taking an install away):
//
//	Probe, InstallAgent, Uninstall
//
// Over the agent's link (Connect proves the managed profile before it hands
// the client over; the manager keeps the client open):
//
//	Connect, PushXray, UpdateXray, UpdateAgent, RenewCert,
//	ProbeTargets, CheckTarget, NewRealityKeys, PushConfig, Provision
//
// What an install leaves on a node is marked in a file there, so that
// Uninstall takes away what the install added and not what was the server's.
package agentops
