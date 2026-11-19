// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/api"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
)

// The outbounds are servers a node, or the router's own entry, can leave
// through instead of the router's VPN. Like the entry's clients, what the
// bridge should have of them follows from the state alone: a change goes into
// the state and the bridge's loop is kicked. An outbound that nothing leaves
// through is not in the bridge's spec, so adding, changing or removing one
// costs the tunnels nothing; a change of route, or of an outbound in use, is a
// new spec and a restart of the bridge.
var _ api.OutboundOps = (*Manager)(nil)

var errTestUnavailable = &api.Error{Code: api.CodeTestUnavailable, Msg: "This hub has no bridge program to try outbounds with."}

func (m *Manager) outboundView(id uint) (api.Outbound, error) {
	v, ok := api.ViewOutbound(m.cfg.Store.Read(), id, m)
	if !ok {
		return api.Outbound{}, api.ErrNoOutbound
	}
	return v, nil
}

// CreateOutbound keeps a new outbound. Its settings are checked first, its
// host name is looked up from the Foreign link and, where there is a bridge
// program, Xray is asked to build it, so that nothing it refuses is kept.
// Nothing leaves through it yet, so the bridge is not touched.
func (m *Manager) CreateOutbound(ctx context.Context, req api.OutboundCreate) (api.Outbound, error) {
	name, err := model.CleanOutboundName(req.Name)
	if err != nil {
		return api.Outbound{}, err
	}
	s, err := model.CleanOutboundSettings(req.Settings.Model())
	if err != nil {
		return api.Outbound{}, err
	}
	o := model.Outbound{Name: name, Fallback: req.Fallback, CreatedAt: m.now(), Settings: s}
	// a taken name or a full list is refused before the bridge program is asked
	trial := m.cfg.Store.Read()
	if _, err := trial.AddOutbound(o); err != nil {
		return api.Outbound{}, err
	}
	if err := m.checkOutbound(ctx, s); err != nil {
		return api.Outbound{}, err
	}
	err = m.cfg.Store.Update(func(st *model.State) error {
		var err error
		o, err = st.AddOutbound(o)
		return err
	})
	if err != nil {
		return api.Outbound{}, err
	}
	m.log.Info("an outbound was added", "outbound", o.ID, "protocol", s.Protocol, "host", s.Host, "port", s.Port)
	return m.outboundView(o.ID)
}

// ChangeOutbound renames an outbound, switches its fallback, or gives it new
// settings, which are checked as a new outbound's are. One that something
// leaves through is in the bridge's spec, and a change to it is a new spec.
func (m *Manager) ChangeOutbound(ctx context.Context, id uint, patch api.OutboundPatch) (api.Outbound, error) {
	if patch.Name == nil && patch.Fallback == nil && patch.Settings == nil {
		return api.Outbound{}, invalidf("there is nothing to change")
	}
	trial := m.cfg.Store.Read()
	if trial.FindOutbound(id) == nil {
		return api.Outbound{}, api.ErrNoOutbound
	}
	var name string
	if patch.Name != nil {
		var err error
		if name, err = model.CleanOutboundName(*patch.Name); err != nil {
			return api.Outbound{}, err
		}
		if err := trial.RenameOutbound(id, name); err != nil {
			return api.Outbound{}, err
		}
	}
	var settings *model.OutboundSettings
	if patch.Settings != nil {
		s, err := model.CleanOutboundSettings(patch.Settings.Model())
		if err != nil {
			return api.Outbound{}, err
		}
		if err := m.checkOutbound(ctx, s); err != nil {
			return api.Outbound{}, err
		}
		settings = &s
	}
	err := m.cfg.Store.Update(func(st *model.State) error {
		if st.FindOutbound(id) == nil {
			return api.ErrNoOutbound
		}
		if patch.Name != nil {
			if err := st.RenameOutbound(id, name); err != nil {
				return err
			}
		}
		o := st.FindOutbound(id)
		if patch.Fallback != nil {
			o.Fallback = *patch.Fallback
		}
		if settings != nil {
			o.Settings = *settings
		}
		return nil
	})
	if err != nil {
		return api.Outbound{}, err
	}
	m.log.Info("an outbound was changed", "outbound", id, "name", patch.Name != nil, "fallback", patch.Fallback != nil, "settings", settings != nil)
	m.kickBridge()
	return m.outboundView(id)
}

// RemoveOutbound forgets an outbound that nothing leaves through. One that a
// node or the router's own entry names stays: where to send them instead is
// the admin's choice, and the refusal says who they are.
func (m *Manager) RemoveOutbound(_ context.Context, id uint) error {
	err := m.cfg.Store.Update(func(st *model.State) error {
		if st.FindOutbound(id) == nil {
			return api.ErrNoOutbound
		}
		if err := st.RemoveOutbound(id); errors.Is(err, model.ErrOutboundInUse) {
			return inUse(*st, id)
		} else if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.log.Info("an outbound was removed", "outbound", id)
	return nil
}

// inUse is the refusal to remove an outbound, with who leaves through it.
func inUse(st model.State, id uint) error {
	nodes, router := st.LeavingThrough(id)
	var who []string
	for _, nid := range nodes {
		if n := st.FindNode(nid); n != nil {
			who = append(who, n.Name)
		}
	}
	if router {
		who = append(who, "This router")
	}
	verb, them := "leave", "them"
	if len(who) == 1 {
		verb, them = "leaves", "it"
	}
	return &api.Error{Code: api.CodeOutboundInUse, Msg: fmt.Sprintf("%s can't be removed while %s %s through it. Choose another way out for %s first.",
		st.FindOutbound(id).Name, andList(who), verb, them)}
}

// andList is "a", "a and b", or "a, b and c".
func andList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// TestOutbound has the bridge program make a web request through a saved
// outbound, from the Foreign link, as the bridge's own check does.
func (m *Manager) TestOutbound(ctx context.Context, id uint) (api.TestResult, error) {
	st := m.cfg.Store.Read()
	o := st.FindOutbound(id)
	if o == nil {
		return api.TestResult{}, api.ErrNoOutbound
	}
	if m.cfg.Prober == nil {
		return api.TestResult{}, errTestUnavailable
	}
	addr, err := m.outAddrOf(ctx, *o)
	if err == nil {
		err = m.familyFault(addr)
	}
	if err != nil {
		return api.TestResult{Error: clipText(sentence(fmt.Sprintf("the host %s can't be reached: %v", o.Settings.Host, err)))}, nil
	}
	return m.tryOutbound(ctx, *o, addr, st.ProbeURL)
}

// TestOutboundSettings does the same for settings that are not saved, as in
// the dialog that adds one.
func (m *Manager) TestOutboundSettings(ctx context.Context, req api.OutboundSettings) (api.TestResult, error) {
	s, err := model.CleanOutboundSettings(req.Model())
	if err != nil {
		return api.TestResult{}, err
	}
	if m.cfg.Prober == nil {
		return api.TestResult{}, errTestUnavailable
	}
	addr, err := m.lookupOutbound(ctx, s)
	if err != nil {
		return api.TestResult{Error: clipText(sentence(err.Error()))}, nil
	}
	return m.tryOutbound(ctx, model.Outbound{Settings: s}, addr, m.cfg.Store.Read().ProbeURL)
}

// tryOutbound runs a probe that makes a web request, and says how it went.
func (m *Manager) tryOutbound(ctx context.Context, o model.Outbound, addr netip.Addr, url string) (api.TestResult, error) {
	res, err := m.probe(ctx, o, addr, url, true)
	secrets := []string{o.Settings.ID, o.Settings.Password}
	switch {
	case errors.Is(err, bridgectl.ErrNoProgram):
		return api.TestResult{}, errTestUnavailable
	case err != nil && ctx.Err() != nil:
		return api.TestResult{}, ctx.Err()
	case err != nil:
		said := without(err.Error(), secrets...)
		m.log.Warn("an outbound could not be tried", "outbound", o.ID, "err", said)
		return api.TestResult{Error: clipText(sentence(said))}, nil
	case !res.OK:
		return api.TestResult{Error: clipText(sentence(without(res.Error, secrets...)))}, nil
	}
	return api.TestResult{OK: true, DelayMs: res.DelayMS, Status: res.Status}, nil
}

// probe asks the bridge program about an outbound at addr, from the Foreign
// link when an interface here holds it: a web request when connect is set,
// else only whether Xray builds it.
func (m *Manager) probe(ctx context.Context, o model.Outbound, addr netip.Addr, url string, connect bool) (bridgespec.ProbeResult, error) {
	req := bridgespec.ProbeRequest{Outbound: specOutbound(o, addr), Connect: connect, URL: url}
	if o.ID == 0 {
		req.Outbound.Tag = "" // not saved, so it has none
	}
	if f := m.cfg.Addrs.Downloads; f.Local {
		req.From = f.IP.String()
	}
	return m.cfg.Prober.Probe(ctx, req)
}

// checkOutbound is what keeps settings from being saved: a host name that
// doesn't resolve from the Foreign link, or settings Xray won't build. Where
// there is no bridge program Xray isn't asked, and the settings are kept on
// the strength of the hub's own checks.
func (m *Manager) checkOutbound(ctx context.Context, s model.OutboundSettings) error {
	addr, err := m.lookupOutbound(ctx, s)
	if err != nil {
		return err
	}
	if m.cfg.Prober == nil {
		return nil
	}
	res, err := m.probe(ctx, model.Outbound{Settings: s}, addr, "", false)
	secrets := []string{s.ID, s.Password}
	switch {
	case errors.Is(err, bridgectl.ErrNoProgram):
		m.log.Info("an outbound's settings are kept without Xray building them first: there is no bridge program here")
		return nil
	case err != nil && ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		said := without(err.Error(), secrets...)
		m.log.Warn("an outbound's settings could not be checked", "err", said)
		return &api.Error{Code: api.CodeTestUnavailable, Msg: clipText(sentence("the settings could not be checked: "+said) + " Nothing was saved.")}
	case !res.OK:
		// the bridge program's own sentence, which already says that Xray refused them
		return invalidf("%s", strings.TrimSuffix(clipText(without(res.Error, secrets...)), "."))
	}
	return nil
}

// lookupOutbound is the address to try settings at: the host, or what its
// name resolves to now from the Foreign link.
func (m *Manager) lookupOutbound(ctx context.Context, s model.OutboundSettings) (netip.Addr, error) {
	if a, err := netip.ParseAddr(s.Host); err == nil {
		a = a.Unmap()
		return a, m.familyFault(a)
	}
	addrs, err := m.lookupAll(ctx, m.cfg.OutboundResolver, s.Host, m.cfg.Addrs.Downloads.IP)
	if err != nil {
		if ctx.Err() != nil {
			return netip.Addr{}, ctx.Err()
		}
		return netip.Addr{}, invalidf("settings.host: %s does not resolve from the router's Foreign link: %v", s.Host, err)
	}
	return addrs[0], nil
}

// familyFault refuses an address the bridge can't reach from the Foreign
// link, which has one family.
func (m *Manager) familyFault(addr netip.Addr) error {
	if from := m.cfg.Addrs.Downloads.IP; addr.Is4() != from.Is4() {
		return invalidf("settings.host: %s is not of the address family of the router's Foreign link, %s", addr, from)
	}
	return nil
}

// SetOutboundCheck sets the address the bridge asks through each outbound to
// see that it answers. Empty is bridgespec's default. It is in the bridge's
// spec only while an outbound is.
func (m *Manager) SetOutboundCheck(_ context.Context, url string) (api.OutboundCheck, error) {
	url = strings.TrimSpace(url)
	if err := bridgespec.CheckProbeURL(url); err != nil {
		return api.OutboundCheck{}, invalidf("the check's url: %v", err)
	}
	if err := m.cfg.Store.Update(func(st *model.State) error { st.ProbeURL = url; return nil }); err != nil {
		return api.OutboundCheck{}, err
	}
	m.log.Info("the outbounds' check changed", "url", url)
	m.kickBridge()
	return api.CheckOf(m.cfg.Store.Read()), nil
}

// noOutbound refuses a route to an outbound nobody has.
func noOutbound(id uint) error { return invalidf("there is no outbound %d to leave through", id) }

// SetNodeRoute says which way a node's traffic leaves: through an outbound,
// or through the router's VPN for 0. The bridge gets the new spec at once.
func (m *Manager) SetNodeRoute(_ context.Context, nodeID, outboundID uint) error {
	changed := false
	err := m.cfg.Store.Update(func(st *model.State) error {
		n := st.FindNode(nodeID)
		if n == nil {
			return notFound("node", nodeID)
		}
		if outboundID != 0 && st.FindOutbound(outboundID) == nil {
			return noOutbound(outboundID)
		}
		changed, n.OutboundID = n.OutboundID != outboundID, outboundID
		return nil
	})
	if err != nil {
		return err
	}
	if changed {
		m.log.Info("a node's way out changed", "node", nodeID, "outbound", outboundID)
	}
	m.kickBridge()
	return nil
}

// SetLocalRoute does the same for the router's own entry. It can be set while
// the entry is off, and holds when it is switched on.
func (m *Manager) SetLocalRoute(_ context.Context, outboundID uint) (api.Local, error) {
	changed := false
	err := m.cfg.Store.Update(func(st *model.State) error {
		if outboundID != 0 && st.FindOutbound(outboundID) == nil {
			return noOutbound(outboundID)
		}
		changed, st.Local.OutboundID = st.Local.OutboundID != outboundID, outboundID
		return nil
	})
	if err != nil {
		return api.Local{}, err
	}
	if changed {
		m.log.Info("the router's own entry's way out changed", "outbound", outboundID)
	}
	m.kickBridge()
	return m.localView(), nil
}

// OutboundLive is how an outbound is doing. It is idle while nothing that
// carries traffic leaves through it, down with the hub's own reason when the
// bridge can't be given it, and otherwise what the bridge's check last found:
// unknown until it has found something, or while the bridge isn't running.
func (m *Manager) OutboundLive(id uint) api.OutboundState {
	st := m.cfg.Store.Read()
	if st.FindOutbound(id) == nil {
		return api.OutboundState{}
	}
	if !st.CarriedThrough(id) {
		return api.OutboundState{Status: api.OutboundIdle}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if use := m.bv.outs[id]; use.err != "" {
		return api.OutboundState{Status: api.OutboundDown, Error: sentence(use.err), OnFallback: use.fallback}
	}
	out := api.OutboundState{Status: api.OutboundUnknown}
	tag, ok := m.bv.appliedOuts[id]
	if !ok || !m.bv.up() {
		return out
	}
	for _, s := range m.bv.status.Bridge.Outbounds {
		if s.Tag != tag {
			continue
		}
		out.LastTryAt, out.LastSeenAt = timePtr(s.LastTry), timePtr(s.LastSeen)
		out.BytesUp, out.BytesDown = s.BytesUp, s.BytesDown
		switch {
		case !s.Checked:
		case s.Alive:
			out.Status, out.DelayMs = api.OutboundUp, s.DelayMS
		default:
			out.Status, out.Error, out.OnFallback = api.OutboundDown, s.Error, s.Fallback
		}
	}
	return out
}
