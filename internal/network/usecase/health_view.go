package usecase

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
)

type TargetStatusView struct {
	Address string `json:"address"`
	Proto   string `json:"proto"`
	OK      bool   `json:"ok"`
	RTTms   int    `json:"rtt_ms"`
	Error   string `json:"error,omitempty"`
}

type UplinkHealthView struct {
	Slot        string             `json:"slot"`
	IfName      string             `json:"if_name"`
	Carrier     string             `json:"carrier"`
	Gateway     string             `json:"gateway"`
	Internet    string             `json:"internet"`
	Verdict     string             `json:"verdict"`
	ForceState  string             `json:"force_state"`
	Degraded    bool               `json:"degraded"`
	LossPct     int                `json:"loss_pct"`
	MedianRTTms int                `json:"median_rtt_ms"`
	Targets     []TargetStatusView `json:"targets"`
	History     []HealthSample     `json:"history"`
}

type TunnelHealthView struct {
	ProfileID   uint               `json:"profile_id"`
	Name        string             `json:"name"`
	IfName      string             `json:"if_name"`
	Priority    int                `json:"priority"`
	Weight      int                `json:"weight"`
	InPool      bool               `json:"in_pool"`
	Verdict     string             `json:"verdict"`
	Degraded    bool               `json:"degraded"`
	LossPct     int                `json:"loss_pct"`
	MedianRTTms int                `json:"median_rtt_ms"`
	Targets     []TargetStatusView `json:"targets"`
	History     []HealthSample     `json:"history"`
}

type VPNPoolHealthView struct {
	Present     bool               `json:"present"`
	ActiveTier  int                `json:"active_tier"`
	LossPct     int                `json:"loss_pct"`
	MedianRTTms int                `json:"median_rtt_ms"`
	PoolHistory []HealthSample     `json:"pool_history"`
	Tunnels     []TunnelHealthView `json:"tunnels"`
}

type HealthView struct {
	GeneratedUnix  int64              `json:"generated_unix"`
	FailoverActive bool               `json:"failover_active"`
	Uplinks        []UplinkHealthView `json:"uplinks"`
	VPN            *VPNPoolHealthView `json:"vpn"`
}

func targetViews(results []ProbeResult) []TargetStatusView {
	out := make([]TargetStatusView, 0, len(results))
	for _, r := range results {
		out = append(out, TargetStatusView{
			Address: r.Target.Address, Proto: r.Target.Proto,
			OK: r.OK, RTTms: int(r.RTT.Milliseconds()), Error: r.Err,
		})
	}
	return out
}

// HealthState is pure assembly of what the loop already measured.
func (u *networkUsecase) HealthState(ctx context.Context) (*HealthView, error) {
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, err
	}
	forceByIf := map[string]string{}
	if u.IfRepo != nil {
		if rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN); err == nil {
			for _, r := range rows {
				forceByIf[r.IfName] = r.ForceState
			}
		}
	}

	// [] and not null; the page maps over this.
	view := &HealthView{GeneratedUnix: time.Now().Unix(), Uplinks: []UplinkHealthView{}}
	u.healthMu.Lock()
	u.ensureHealthMaps()
	view.FailoverActive = u.failoverActive
	ladders := make(map[string]uplinkLadder, len(u.ladders))
	for k, v := range u.ladders {
		ladders[k] = v
	}
	u.healthMu.Unlock()

	for _, up := range uplinks {
		l := ladders[up.IfName]
		// The page only draws 15 minutes.
		samples := window(u.ring(up.IfName).snapshot(), 180)
		view.Uplinks = append(view.Uplinks, UplinkHealthView{
			Slot: string(up.Slot), IfName: up.IfName,
			Carrier: l.Carrier, Gateway: l.Gateway, Internet: l.Internet,
			Verdict: l.Verdict, ForceState: forceByIf[up.IfName],
			Degraded: l.Degraded, LossPct: lossPct(samples, 20),
			MedianRTTms: medianRTT(samples, 20),
			Targets:     targetViews(l.Results), History: samples,
		})
	}

	if pool := u.vpnPoolNow(ctx); pool.Active() {
		inPool := map[string]bool{}
		for _, n := range u.currentPoolNexthops() {
			inPool[n.OifName] = true
		}
		v := &VPNPoolHealthView{Present: true, ActiveTier: -1, Tunnels: []TunnelHealthView{}}
		var histories [][]HealthSample
		for _, t := range pool.Tunnels {
			l := ladders[t.IfName]
			samples := window(u.ring(t.IfName).snapshot(), 180)
			tv := TunnelHealthView{
				ProfileID: t.Profile.ID, Name: t.Profile.Name, IfName: t.IfName,
				Priority: t.Profile.Priority, Weight: t.Profile.Weight,
				InPool: inPool[t.IfName], Verdict: l.Verdict, Degraded: l.Degraded,
				LossPct: lossPct(samples, 20), MedianRTTms: medianRTT(samples, 20),
				Targets: targetViews(l.Results), History: samples,
			}
			if tv.InPool {
				histories = append(histories, samples)
				if v.ActiveTier == -1 || t.Profile.Priority < v.ActiveTier {
					v.ActiveTier = t.Profile.Priority
				}
			}
			v.Tunnels = append(v.Tunnels, tv)
		}
		if v.ActiveTier == -1 {
			v.ActiveTier = 0
		}
		v.PoolHistory = mergeHistories(histories)
		v.LossPct = lossPct(v.PoolHistory, 20)
		v.MedianRTTms = medianRTT(v.PoolHistory, 20)
		view.VPN = v
	}
	return view, nil
}
