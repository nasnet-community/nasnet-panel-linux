// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// Probe reads the SSH host key of a server, so that the person can agree to it
// before any login is sent there, and says whether this router already knows
// the server: "new" when no node of it is on that host and port, "known" when
// a node there has this key pinned, and "changed" when it has another, which a
// rebuilt server has and a machine that stands in its way would too.
//
// It signs in to nothing, so the user name is only checked.
func (m *Manager) Probe(ctx context.Context, req api.ProbeRequest) (api.ProbeResult, error) {
	host, err := model.CleanHost(req.Host)
	if err != nil {
		return api.ProbeResult{}, err
	}
	port, err := sshPortOf(req.SSHPort)
	if err != nil {
		return api.ProbeResult{}, err
	}
	if _, err := cleanUser(req.SSHUser); err != nil {
		return api.ProbeResult{}, err
	}
	ops, err := m.operate()
	if err != nil {
		return api.ProbeResult{}, err
	}
	fingerprint, err := ops.Probe(ctx, host, port)
	if err != nil {
		return api.ProbeResult{}, opFailure(err)
	}
	return api.ProbeResult{Fingerprint: fingerprint, Status: m.keyStatus(host, port, fingerprint)}, nil
}

// keyStatus compares a key with the one pinned for a node at that address. A
// node that was started and never finished has its key pinned too, since the
// key was agreed to before the install began.
func (m *Manager) keyStatus(host string, port int, fingerprint string) string {
	st := m.cfg.Store.Read()
	for _, n := range st.Nodes {
		if n.Host != host || n.SSHPort != port || n.SSHHostKey == "" {
			continue
		}
		if n.SSHHostKey == fingerprint {
			return api.ProbeKnown
		}
		return api.ProbeChanged
	}
	return api.ProbeNew
}
