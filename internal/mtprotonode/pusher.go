package mtprotonode

import (
	"context"
	"errors"
	"fmt"

	"github.com/nasnet-community/mtproto-remote-plugin/ctrl"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// Pusher takes a full node's payload, so subscription policy is folded in one place.
type Pusher struct {
	m      *Manager
	nodeID uint
}

func (m *Manager) Pusher(nodeID uint) *Pusher { return &Pusher{m: m, nodeID: nodeID} }

// ApplyCoreState pushes now so the caller hears the verdict; a down link keeps it queued.
func (p *Pusher) ApplyCoreState(ctx context.Context, payload *pb.CoreStatePayload) error {
	e := p.m.entry(p.nodeID)
	if e == nil {
		return ErrNotRunning
	}
	if payload.GetCoreType() != "mtproto" {
		if payload.GetEnabled() {
			return fmt.Errorf("an MTProto node runs only MTProto, not %q", payload.GetCoreType())
		}
		return nil // tearing down a core an MTProto node never had
	}

	var st ctrl.State
	inbound := payload.GetInboundId()
	switch {
	case !payload.GetEnabled() && payload.GetPort() == 0 && len(payload.GetSettings()) == 0:
		// prune what we last asked for, or a queued state brings the users back
		e.mu.Lock()
		base := e.desired
		e.mu.Unlock()
		if base == nil {
			return nil
		}
		st = *base
		st.Users = nil
		st.Rev = Rev(st)
		inbound = ""
	default:
		var err error
		st, err = StateFromPayload(payload, Extras{Prefixes: p.m.cfg.Allow.List.Prefixes(), MaxStreams: p.maxStreams(e)})
		if err != nil {
			return err
		}
	}

	// a new cover site must pass from the node first; the users change anyway
	e.mu.Lock()
	current := e.lite.Disguise
	e.mu.Unlock()
	var coverErr error
	if st.Disguise != "" && st.Disguise != current {
		if err := p.m.vetCover(ctx, e, st.Disguise); err != nil {
			coverErr = fmt.Errorf("the cover site was not changed: %w", err)
			st = withDisguise(st, current)
		}
	}

	e.mu.Lock()
	e.inboundID = inbound
	e.coverPending = ""
	if coverErr != nil {
		e.coverPending = coverErr.Error()
	}
	// held across SetDesired, so a new stream limit saved meanwhile can't be pushed over
	e.node.SetDesired(e.wantLocked(st))
	e.mu.Unlock()
	if _, err := e.node.PushNow(ctx); err != nil {
		return errors.Join(err, coverErr)
	}
	return coverErr
}

// withDisguise keeps a state on the given cover site.
func withDisguise(st ctrl.State, domain string) ctrl.State {
	st.Disguise = domain
	if raw, ok := st.Telemt.(map[string]any); ok {
		cp := make(map[string]any, len(raw))
		for k, v := range raw {
			cp[k] = v
		}
		cp["tls_domain"] = domain
		st.Telemt = cp
	}
	st.Rev = Rev(st)
	return st
}

func (p *Pusher) maxStreams(e *entry) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lite.MaxStreams
}

// GetCoreStatus reports the node as the one MTProto core of the node.
func (p *Pusher) GetCoreStatus(ctx context.Context, coreType string) (*pb.CoreStatusResponse, error) {
	if coreType != "mtproto" {
		return &pb.CoreStatusResponse{}, nil
	}
	v, ok := p.m.View(p.nodeID)
	if !ok {
		return nil, ErrNotRunning
	}
	snap := v.Snapshot
	out := &pb.CoreStatusResponse{Version: v.Lite.TelemtVersion, LiveSessions: int32(snap.Link.Streams)}
	switch {
	case snap.Link.Up == 0:
		out.Detail = "the node's link is down"
	case snap.Agent == nil:
		out.Detail = "waiting for the agent's first report"
	default:
		out.Running = snap.Agent.Telemt.Running
		switch {
		case snap.Agent.Telemt.CrashLoop:
			out.Detail = "telemt is crash-looping: " + snap.Agent.Telemt.LastExit
		case !snap.Agent.Telemt.Running && snap.Agent.Telemt.Held != "":
			out.Detail = "telemt is held stopped: " + snap.Agent.Telemt.Held
		case !snap.Agent.Telemt.Running:
			out.Detail = "telemt is not running"
		}
	}
	if e := p.m.entry(p.nodeID); e != nil {
		e.mu.Lock()
		if e.inboundID != "" {
			out.InboundIds = []string{e.inboundID}
		}
		e.mu.Unlock()
	}
	return out, nil
}
