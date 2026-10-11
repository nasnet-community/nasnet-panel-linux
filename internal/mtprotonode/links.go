package mtprotonode

import (
	"context"
	"io"

	"github.com/nasnet-community/mtproto-remote-plugin/relayops"
)

// Links adapts the manager to what relayops drives.
func (m *Manager) Links() relayops.Links { return linksAdapter{m} }

type linksAdapter struct{ m *Manager }

var _ relayops.CoverAdopter = linksAdapter{} // relayops asserts it at run time; a typo would skip it

func (a linksAdapter) AdoptCover(ctx context.Context, id uint, domain string) error {
	return a.m.AdoptCover(ctx, id, domain)
}

func (a linksAdapter) Ensure(ctx context.Context, id uint) error { return a.m.Ensure(ctx, id) }
func (a linksAdapter) IsRunning(id uint) bool                    { return a.m.IsRunning(id) }
func (a linksAdapter) Remove(id uint)                            { a.m.Remove(id) }
func (a linksAdapter) Redial(ctx context.Context, id uint) error { return a.m.Redial(ctx, id) }
func (a linksAdapter) SetCreds(ctx context.Context, r *relayops.Record) error {
	return a.m.SetCreds(ctx, r)
}
func (a linksAdapter) Call(ctx context.Context, id uint, method string, req, resp any) error {
	return a.m.Call(ctx, id, method, req, resp)
}
func (a linksAdapter) CallRaw(ctx context.Context, id uint, method string, req any, body io.Reader, resp any) error {
	return a.m.CallRaw(ctx, id, method, req, body, resp)
}
func (a linksAdapter) View(id uint) (relayops.LinkView, bool) {
	v, ok := a.m.View(id)
	if !ok {
		return relayops.LinkView{}, false
	}
	return report(v), true
}

// report is the node's view as relayops reads it.
func report(v View) relayops.LinkView {
	out := relayops.LinkView{Up: v.Snapshot.Link.Up > 0}
	if a := v.Snapshot.Agent; a != nil {
		out.AgentVersion, out.AgentBuild, out.TelemtVersion = a.AgentVersion, a.AgentBuild, a.TelemtVersion
	}
	return out
}
