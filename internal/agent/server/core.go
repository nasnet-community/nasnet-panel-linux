package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/core"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// collectCoreTrafficOnce runs one core-traffic collection cycle: it pulls the
// read-and-reset delta from every registered core and accumulates it into the
// buffered traffic store (the same store the hub polls via GetBufferedTraffic).
func (s *Server) collectCoreTrafficOnce(ctx context.Context) {
	if s.trafficStore == nil || s.coreRegistry == nil {
		return
	}
	perType, err := s.coreRegistry.CollectTrafficByType(ctx)
	if err != nil || len(perType) == 0 {
		return
	}

	// Build one snapshot carrying BOTH the merged per-email totals (which feed the
	// sub aggregate, exactly as before) AND per-(protocol,email) core usage (which
	// lets the hub attribute per-tunnel core usage to the exact protocol, even when
	// several core protocols share one email on this node).
	snap := &traffic.XrayStatsSnapshot{
		UserUplink:   make(map[string]int64),
		UserDownlink: make(map[string]int64),
		CoreUsage:    make(map[string]map[string]traffic.CoreUsageCounts),
	}
	addCore := func(proto, email string, up, down int64) {
		byEmail := snap.CoreUsage[proto]
		if byEmail == nil {
			byEmail = make(map[string]traffic.CoreUsageCounts)
			snap.CoreUsage[proto] = byEmail
		}
		c := byEmail[email]
		c.Uplink += up
		c.Downlink += down
		byEmail[email] = c
	}
	for ctype, cs := range perType {
		proto := string(ctype)
		for email, v := range cs.UserUplink {
			snap.UserUplink[email] += v
			addCore(proto, email, v, 0)
		}
		for email, v := range cs.UserDownlink {
			snap.UserDownlink[email] += v
			addCore(proto, email, 0, v)
		}
		snap.TotalUplink += cs.TotalUplink
		snap.TotalDownlink += cs.TotalDownlink
	}

	if len(snap.UserUplink) == 0 && len(snap.UserDownlink) == 0 && snap.TotalUplink == 0 && snap.TotalDownlink == 0 {
		return
	}
	s.trafficStore.Accumulate(snap, time.Now())
}

// startCoreTrafficCollector launches the periodic core-traffic collection loop.
func (s *Server) startCoreTrafficCollector(interval time.Duration) {
	if s.trafficStore == nil {
		// Buffering off (Traffic.BufferEnabled=false): tunnel per-email usage is
		// NOT metered, so data limits never trip for tunnels. Surface it rather
		// than silently no-op.
		logrus.Warn("core traffic collection disabled: Traffic.BufferEnabled=false — tunnel usage/quota will not be enforced")
		return
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	s.coreTrafficStop = make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-s.coreTrafficStop:
				return
			case <-t.C:
				s.collectCoreTrafficOnce(context.Background())
			}
		}
	}()
}

// stopCoreTrafficCollector stops the core-traffic collection loop.
func (s *Server) stopCoreTrafficCollector() {
	if s.coreTrafficStop != nil {
		close(s.coreTrafficStop)
		s.coreTrafficStop = nil
	}
}

// desiredStateFromPayload translates a gRPC CoreStatePayload into the agent's
// internal core.DesiredState.
func desiredStateFromPayload(req *pb.CoreStatePayload) core.DesiredState {
	users := make([]core.UserSpec, 0, len(req.GetUsers()))
	for _, u := range req.GetUsers() {
		spec := core.UserSpec{
			Username:         u.GetUsername(),
			Secret:           u.GetSecret(),
			Email:            u.GetEmail(),
			KLimit:           int(u.GetKLimit()),
			Enabled:          u.GetEnabled(),
			RateLimitUpBps:   u.GetRateLimitUpBps(),
			RateLimitDownBps: u.GetRateLimitDownBps(),
		}
		if ts := u.GetExpiresAtUnix(); ts > 0 {
			spec.ExpiresAt = time.Unix(ts, 0).UTC()
		}
		users = append(users, spec)
	}
	files := make([]core.FileSpec, 0, len(req.GetFiles()))
	for _, f := range req.GetFiles() {
		files = append(files, core.FileSpec{
			RelPath: f.GetRelPath(),
			Content: f.GetContent(),
			Mode:    f.GetMode(),
		})
	}
	var settings json.RawMessage
	if s := req.GetSettings(); len(s) > 0 {
		settings = json.RawMessage(s)
	}
	return core.DesiredState{
		CoreType:   core.CoreType(req.GetCoreType()),
		InboundID:  req.GetInboundId(),
		Enabled:    req.GetEnabled(),
		Listen:     req.GetListen(),
		Port:       int(req.GetPort()),
		EgressMode: core.EgressMode(req.GetEgressMode()),
		Settings:   settings,
		Users:      users,
		Files:      files,
	}
}

// ApplyCoreState reconciles one protocol core to the pushed desired state.
func (s *Server) ApplyCoreState(ctx context.Context, req *pb.CoreStatePayload) (*pb.CommandResponse, error) {
	if s.coreRegistry == nil {
		return &pb.CommandResponse{Success: false, Message: "core registry not initialized"}, nil
	}
	// Persist the hub's desired state on receipt (before Apply) so a reboot can
	// replay it even if this Apply fails transiently. Enabled=false is a teardown,
	// so drop the record. Persistence is best-effort — a disk hiccup must not fail
	// the hub push.
	if s.coreState != nil {
		var perr error
		if req.GetEnabled() {
			perr = s.coreState.Save(req)
		} else {
			perr = s.coreState.Remove(req.GetInboundId())
		}
		if perr != nil {
			logrus.WithError(perr).WithField("inbound_id", req.GetInboundId()).
				Warn("core state persist failed (reboot recovery may be incomplete)")
		}
	}
	d := desiredStateFromPayload(req)
	if err := s.coreRegistry.Apply(ctx, d); err != nil {
		return &pb.CommandResponse{Success: false, Message: err.Error()}, nil
	}
	return &pb.CommandResponse{Success: true, Message: "core state applied"}, nil
}

// replayCoreState re-applies every persisted core desired state on agent
// startup, reopening listeners and respawning daemons after a reboot/restart.
// Best-effort per inbound: a failure is logged and left for the next hub
// reconcile to retry. Called from StartBackgroundServices.
func (s *Server) replayCoreState(ctx context.Context) {
	if s.coreState == nil || s.coreRegistry == nil {
		return
	}
	payloads, err := s.coreState.LoadAll()
	if err != nil {
		logrus.WithError(err).Warn("core state replay: failed to load persisted state")
		return
	}
	if len(payloads) == 0 {
		return
	}
	logrus.WithField("count", len(payloads)).Info("Replaying persisted core state after restart")
	for _, p := range payloads {
		if err := s.coreRegistry.Apply(ctx, desiredStateFromPayload(p)); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"core_type":  p.GetCoreType(),
				"inbound_id": p.GetInboundId(),
			}).Warn("core state replay: Apply failed (will retry on next hub reconcile)")
		}
	}
}

// GetCoreStatus reports the runtime state of one protocol core.
func (s *Server) GetCoreStatus(ctx context.Context, req *pb.CoreStatusRequest) (*pb.CoreStatusResponse, error) {
	if s.coreRegistry == nil {
		return nil, status.Error(codes.Unavailable, "core registry not initialized")
	}
	st, err := s.coreRegistry.Status(ctx, core.CoreType(req.GetCoreType()))
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "core status: %v", err)
	}
	return &pb.CoreStatusResponse{
		Running:         st.Running,
		Version:         st.Version,
		LiveSessions:    int32(st.LiveSessions),
		DistinctDevices: int32(st.DistinctDevices),
		Detail:          st.Detail,
		InboundIds:      s.coreInboundIDs(req.GetCoreType(), st.InboundIDs),
	}, nil
}

// coreInboundIDs adds the inbounds persisted for replay to the ones the core
// runs: an inbound whose Apply failed at startup is still replayed on the next
// boot, so the hub has to see it to tear it down.
func (s *Server) coreInboundIDs(coreType string, running []string) []string {
	if s.coreState == nil {
		return running
	}
	payloads, err := s.coreState.LoadAll()
	if err != nil {
		return running
	}
	seen := make(map[string]bool, len(running))
	out := append([]string(nil), running...)
	for _, id := range running {
		seen[id] = true
	}
	for _, p := range payloads {
		if p.GetCoreType() == coreType && !seen[p.GetInboundId()] {
			seen[p.GetInboundId()] = true
			out = append(out, p.GetInboundId())
		}
	}
	return out
}
