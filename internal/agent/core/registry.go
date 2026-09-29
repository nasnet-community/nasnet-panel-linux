package core

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/traffic"
)

// Registry holds the protocol cores enabled on this agent and dispatches
// hub-driven operations (Apply/Status/Sweep) to the matching core. It also
// aggregates traffic across all cores into a single snapshot for the buffered
// traffic pipeline.
type Registry struct {
	cores map[CoreType]Core
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{cores: make(map[CoreType]Core)}
}

// Register adds a core keyed by its Type. Registering the same type twice is an
// error to prevent silent overwrites during wiring.
func (r *Registry) Register(c Core) error {
	t := c.Type()
	if _, exists := r.cores[t]; exists {
		return fmt.Errorf("core %q already registered", t)
	}
	r.cores[t] = c
	return nil
}

// Get returns the registered core for a type.
func (r *Registry) Get(t CoreType) (Core, bool) {
	c, ok := r.cores[t]
	return c, ok
}

// Types returns the registered core types (unordered).
func (r *Registry) Types() []CoreType {
	out := make([]CoreType, 0, len(r.cores))
	for t := range r.cores {
		out = append(out, t)
	}
	return out
}

// Apply dispatches a desired state to the matching core.
func (r *Registry) Apply(ctx context.Context, d DesiredState) error {
	c, ok := r.cores[d.CoreType]
	if !ok {
		return fmt.Errorf("no core registered for type %q", d.CoreType)
	}
	return c.Apply(ctx, d)
}

// Status reports the runtime state of one core.
func (r *Registry) Status(ctx context.Context, t CoreType) (CoreStatus, error) {
	c, ok := r.cores[t]
	if !ok {
		return CoreStatus{}, fmt.Errorf("no core registered for type %q", t)
	}
	return c.Status(ctx)
}

// Sweep dispatches enforcement to the matching core.
func (r *Registry) Sweep(ctx context.Context, d DesiredState) error {
	c, ok := r.cores[d.CoreType]
	if !ok {
		return fmt.Errorf("no core registered for type %q", d.CoreType)
	}
	return c.Sweep(ctx, d)
}

// CollectTraffic merges the traffic snapshots of every registered core into one
// snapshot suitable for traffic.Store.Accumulate. Cores that return nil (no
// traffic) are skipped.
func (r *Registry) CollectTraffic(ctx context.Context) (*traffic.XrayStatsSnapshot, error) {
	merged := &traffic.XrayStatsSnapshot{
		UserUplink:       make(map[string]int64),
		UserDownlink:     make(map[string]int64),
		InboundUplink:    make(map[string]int64),
		InboundDownlink:  make(map[string]int64),
		OutboundUplink:   make(map[string]int64),
		OutboundDownlink: make(map[string]int64),
	}
	for _, c := range r.cores {
		snap, err := c.CollectTraffic(ctx)
		if err != nil {
			return nil, fmt.Errorf("collect traffic for %q: %w", c.Type(), err)
		}
		if snap == nil {
			continue
		}
		mergeCounts(merged.UserUplink, snap.UserUplink)
		mergeCounts(merged.UserDownlink, snap.UserDownlink)
		mergeCounts(merged.InboundUplink, snap.InboundUplink)
		mergeCounts(merged.InboundDownlink, snap.InboundDownlink)
		mergeCounts(merged.OutboundUplink, snap.OutboundUplink)
		mergeCounts(merged.OutboundDownlink, snap.OutboundDownlink)
		merged.TotalUplink += snap.TotalUplink
		merged.TotalDownlink += snap.TotalDownlink
	}
	return merged, nil
}

func mergeCounts(dst, src map[string]int64) {
	for k, v := range src {
		dst[k] += v
	}
}

// CollectTrafficByType returns each registered core's traffic snapshot keyed by
// its type, WITHOUT merging across cores. Cores that return nil (no traffic)
// are omitted. The collector uses this to tag per-protocol core
// usage so the hub can attribute it to the exact core tunnel even when several
// protocols share one email on a node.
func (r *Registry) CollectTrafficByType(ctx context.Context) (map[CoreType]*traffic.XrayStatsSnapshot, error) {
	out := make(map[CoreType]*traffic.XrayStatsSnapshot, len(r.cores))
	for t, c := range r.cores {
		snap, err := c.CollectTraffic(ctx)
		if err != nil {
			return nil, fmt.Errorf("collect traffic for %q: %w", t, err)
		}
		if snap == nil {
			continue
		}
		out[t] = snap
	}
	return out, nil
}

// Shutdown stops every core that holds listeners or daemons (the optional
// Shutdown(ctx) method), returning the first error.
func (r *Registry) Shutdown(ctx context.Context) error {
	var first error
	for t, c := range r.cores {
		sd, ok := c.(interface{ Shutdown(context.Context) error })
		if !ok {
			continue
		}
		if err := sd.Shutdown(ctx); err != nil && first == nil {
			first = fmt.Errorf("shutdown %q: %w", t, err)
		}
	}
	return first
}
