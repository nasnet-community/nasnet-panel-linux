package usecase

import (
	"context"
	"strings"
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
	// Note names a state the ladder can see but not explain, empty when there
	// is nothing to say.
	Note string `json:"note,omitempty"`
}

type TunnelHealthView struct {
	ProfileID uint   `json:"profile_id"`
	Name      string `json:"name"`
	IfName    string `json:"if_name"`
	// Position is the operator's order, first is 0.
	Position    int                `json:"position"`
	InPool      bool               `json:"in_pool"`
	Verdict     string             `json:"verdict"`
	Degraded    bool               `json:"degraded"`
	LossPct     int                `json:"loss_pct"`
	MedianRTTms int                `json:"median_rtt_ms"`
	Targets     []TargetStatusView `json:"targets"`
	History     []HealthSample     `json:"history"`
}

type VPNPoolHealthView struct {
	Present bool `json:"present"`
	// Carrier is empty unless one tunnel carries at a time.
	Strategy    PoolStrategy       `json:"strategy"`
	Carrier     string             `json:"carrier,omitempty"`
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
	forceByIf, sourceByIf := map[string]string{}, map[string]string{}
	if u.IfRepo != nil {
		if rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN); err == nil {
			for _, r := range rows {
				forceByIf[r.IfName] = r.ForceState
				sourceByIf[r.IfName] = r.Source
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
		_, everUp := u.inetState(up.IfName).snapshot()
		view.Uplinks = append(view.Uplinks, UplinkHealthView{
			Slot: string(up.Slot), IfName: up.IfName,
			Carrier: l.Carrier, Gateway: l.Gateway, Internet: l.Internet,
			Verdict: l.Verdict, ForceState: forceByIf[up.IfName],
			Degraded: l.Degraded, LossPct: lossPct(samples, 20),
			MedianRTTms: medianRTT(samples, 20),
			Targets:     targetViews(l.Results), History: samples,
			Note: uplinkNote(sourceByIf[up.IfName], l.Gateway, l.Internet, everUp),
		})
	}

	if pool := u.vpnPoolNow(ctx); pool.Active() {
		inPool := map[string]bool{}
		for _, n := range u.currentPoolNexthops() {
			inPool[n.OifName] = true
		}
		strategy := u.poolStrategyNow()
		v := &VPNPoolHealthView{
			Present: true, Strategy: strategy, Tunnels: []TunnelHealthView{},
		}
		if strategy.SingleCarrier() {
			v.Carrier = u.poolCarrierNow()
		}
		var histories [][]HealthSample
		for _, t := range pool.Tunnels {
			l := ladders[t.IfName]
			samples := window(u.ring(t.IfName).snapshot(), 180)
			tv := TunnelHealthView{
				ProfileID: t.Profile.ID, Name: t.Profile.Name, IfName: t.IfName,
				Position: t.Profile.Priority,
				InPool:   inPool[t.IfName], Verdict: l.Verdict, Degraded: l.Degraded,
				LossPct: lossPct(samples, 20), MedianRTTms: medianRTT(samples, 20),
				Targets: targetViews(l.Results), History: samples,
			}
			if tv.InPool {
				histories = append(histories, samples)
			}
			v.Tunnels = append(v.Tunnels, tv)
		}
		v.PoolHistory = mergeHistories(histories)
		v.LossPct = lossPct(v.PoolHistory, 20)
		v.MedianRTTms = medianRTT(v.PoolHistory, 20)
		view.VPN = v
	}
	return view, nil
}

// A vanished AP does not clear carrier (see the StationClient comment), so a
// wifi uplink's outage arrives through the gateway rung, not no-carrier.
//
// uplinkNote explains the one failure shape a wifi uplink hits constantly: an
// upstream captive portal answers ARP and DHCP but blocks everything else, so
// the ladder reads "gateway up, internet down" forever. Only claimed while the
// internet has never answered — a link that worked and stopped is an outage.
func uplinkNote(source, gateway, internet string, everUp bool) string {
	if !strings.HasPrefix(source, "wifi_") || everUp {
		return ""
	}
	if gateway != "up" || internet != "down" {
		return ""
	}
	return "the gateway answers but the internet never has — a captive portal on the " +
		"upstream network is the usual cause; open its login page from a device on it"
}
