package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// What the status card shows. Strings: they go straight to JSON.
const (
	PortMapVerdictPending      = "pending"
	PortMapVerdictDisabled     = "disabled"
	PortMapVerdictPublicDirect = "public_direct"
	PortMapVerdictOK           = "ok"
	PortMapVerdictPartial      = "partial"
	PortMapVerdictNestedNAT    = "nested_nat"
	PortMapVerdictNoService    = "no_service"
	PortMapVerdictDenied       = "denied"
	PortMapVerdictError        = "error"
)

const (
	// A probe answer is trusted this long; a gateway change wipes it early.
	portMapProbeTrust = 10 * time.Minute
	portMapLease      = 2 * time.Hour
	portMapOpTimeout  = 5 * time.Second
	// The port sshd listens on. Not configurable here — this is only ever used
	// to make an operator confirm before handing it to the internet.
	portMapSSHPort = 22
)

type pmLeaseKey struct {
	Proto string
	Port  uint16
}

func (k pmLeaseKey) String() string { return fmt.Sprintf("%s/%d", k.Proto, k.Port) }

type pmDesired struct {
	key    pmLeaseKey
	source string
	hint   uint16
}

// pmHeld is one live mapping and the row that asked for it.
type pmHeld struct {
	lease  system.PortMapLease
	source string
}

// pmFailure is a desired mapping the gateway would not grant. Held separately
// from the leases so the card can show the gap instead of hiding it.
type pmFailure struct {
	source string
	err    string
}

// pmWANState is one uplink's mapper state, guarded by pmMu. Network calls
// happen outside the lock: state is copied out, then written back.
type pmWANState struct {
	wan       system.PortMapWAN
	gateway   string
	probe     system.PortMapProbe
	probed    bool
	epoch     system.PortMapEpoch
	held      map[pmLeaseKey]pmHeld
	failed    map[pmLeaseKey]pmFailure
	verdict   string
	lastErr   string
	suspended bool
}

// pmInputs is one WAN's reconcile inputs, assembled by the caller so the core
// stays testable without repositories.
type pmInputs struct {
	wan     system.PortMapWAN
	key     string
	enabled bool
	desired []pmDesired
}

func (u *networkUsecase) portMapper() system.PortMapper {
	if u.PortMap != nil {
		return u.PortMap
	}
	return system.NewPortMapper()
}

func (u *networkUsecase) portMapKickCh() chan struct{} {
	u.pmMu.Lock()
	defer u.pmMu.Unlock()
	if u.pmKick == nil {
		u.pmKick = make(chan struct{}, 1)
	}
	return u.pmKick
}

// kickPortMap nudges the loop. A dropped nudge is fine, the tick catches up.
func (u *networkUsecase) kickPortMap() {
	ch := u.portMapKickCh()
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (u *networkUsecase) portMapStateFor(key string) *pmWANState {
	u.pmMu.Lock()
	defer u.pmMu.Unlock()
	if u.pmState == nil {
		u.pmState = map[string]*pmWANState{}
	}
	st, ok := u.pmState[key]
	if !ok {
		st = &pmWANState{held: map[pmLeaseKey]pmHeld{}, failed: map[pmLeaseKey]pmFailure{}}
		u.pmState[key] = st
	}
	return st
}

var cgnatRange = netip.MustParsePrefix("100.64.0.0/10")

// publicV4: an address the internet can dial. Only ever applied to what a
// mapping protocol itself reported — a WAN lease in this range proves nothing,
// domestic ISPs hand out CGNAT space on real uplinks.
func publicV4(a netip.Addr) bool {
	return a.IsValid() && a.Is4() && !a.IsPrivate() && !cgnatRange.Contains(a) &&
		!a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified()
}

// desiredPortMaps merges the auto set (enabled inbounds) with the manual rules
// for one uplink. Inbounds win a collision: they are the reconciler's own.
func desiredPortMaps(inbounds []InboundSpec, rules []domain.PortMapRule, uplinkKey string) []pmDesired {
	seen := map[pmLeaseKey]bool{}
	var out []pmDesired
	for _, ib := range inbounds {
		// A local-only proxy has no business on the internet, and the operator
		// never asked for one: only a manual rule may expose it.
		if !ib.Enabled || ib.NoAutoMap || ib.Port <= 0 || ib.Port > 65535 {
			continue
		}
		k := pmLeaseKey{Proto: ib.Proto, Port: uint16(ib.Port)}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, pmDesired{key: k, source: ib.Tag})
	}
	for _, r := range rules {
		if !r.Enabled || r.Port <= 0 || r.Port > 65535 {
			continue
		}
		if r.UplinkKey != "" && r.UplinkKey != uplinkKey {
			continue
		}
		k := pmLeaseKey{Proto: r.Proto, Port: uint16(r.Port)}
		if seen[k] {
			continue
		}
		seen[k] = true
		src := r.Comment
		if src == "" {
			src = "manual"
		}
		var hint uint16
		if r.ExternalHint > 0 && r.ExternalHint <= 65535 {
			hint = uint16(r.ExternalHint)
		}
		out = append(out, pmDesired{key: k, source: src, hint: hint})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].key.Proto != out[j].key.Proto {
			return out[i].key.Proto < out[j].key.Proto
		}
		return out[i].key.Port < out[j].key.Port
	})
	return out
}

// noteEpoch folds a fresh epoch into the state and reports a reboot. An
// upstream that rebooted forgot every mapping it ever granted us.
func (u *networkUsecase) noteEpoch(st *pmWANState, e system.PortMapEpoch) bool {
	if !e.Known() {
		return false
	}
	u.pmMu.Lock()
	prev := st.epoch
	st.epoch = e
	u.pmMu.Unlock()
	return prev.Rebooted(e)
}

func (u *networkUsecase) reconcilePortMapWAN(ctx context.Context, in pmInputs) {
	st := u.portMapStateFor(in.key)
	mapper := u.portMapper()

	if !in.enabled {
		u.releasePortMapLeases(ctx, st, false)
		u.pmMu.Lock()
		st.wan, st.verdict, st.lastErr = in.wan, PortMapVerdictDisabled, ""
		st.failed = map[pmLeaseKey]pmFailure{}
		u.pmMu.Unlock()
		return
	}
	if !in.wan.Gateway.IsValid() {
		u.pmMu.Lock()
		st.wan = in.wan
		st.verdict, st.lastErr = PortMapVerdictError, "no gateway learned yet"
		u.pmMu.Unlock()
		return
	}

	// The gateway is read before the new WAN is adopted: releases for the old
	// router have to travel to the old router, not to whatever replaced it.
	gw := in.wan.Gateway.String()
	u.pmMu.Lock()
	moved := st.gateway != "" && st.gateway != gw
	u.pmMu.Unlock()
	if moved {
		// The old router keeps state we can no longer reach. Start over.
		u.releasePortMapLeases(ctx, st, true)
	}

	u.pmMu.Lock()
	st.wan = in.wan
	st.gateway = gw
	if moved {
		st.probed, st.probe, st.epoch = false, system.PortMapProbe{}, system.PortMapEpoch{}
	}
	needProbe := !st.probed || time.Since(st.probe.SeenAt) > portMapProbeTrust
	u.pmMu.Unlock()

	if publicV4(in.wan.SelfIP) {
		// No NAT in front of us here; anything held is leftover.
		u.releasePortMapLeases(ctx, st, false)
		u.pmMu.Lock()
		st.verdict, st.lastErr = PortMapVerdictPublicDirect, ""
		st.failed = map[pmLeaseKey]pmFailure{}
		u.pmMu.Unlock()
		return
	}

	if needProbe {
		pctx, cancel := context.WithTimeout(ctx, portMapOpTimeout)
		probe, err := mapper.Probe(pctx, in.wan)
		cancel()
		if err != nil {
			u.pmMu.Lock()
			st.verdict, st.lastErr = PortMapVerdictError, err.Error()
			u.pmMu.Unlock()
			return
		}
		if u.noteEpoch(st, probe.Epoch) {
			u.forgetPortMapLeases(st, "the upstream router rebooted")
		}
		u.pmMu.Lock()
		st.probe, st.probed = probe, true
		u.pmMu.Unlock()
	}

	u.pmMu.Lock()
	probe := st.probe
	u.pmMu.Unlock()

	if !probe.Any() {
		u.pmMu.Lock()
		prev := st.verdict
		if probe.Denied {
			st.verdict, st.lastErr = PortMapVerdictDenied, "the upstream router refuses port mappings"
		} else {
			st.verdict, st.lastErr = PortMapVerdictNoService, ""
		}
		verdict := st.verdict
		u.pmMu.Unlock()
		if verdict == PortMapVerdictDenied && prev != PortMapVerdictDenied {
			u.emit(events.EventPortMapDenied, map[string]any{"if_name": in.wan.IfName})
		}
		return
	}

	want := map[pmLeaseKey]pmDesired{}
	for _, d := range in.desired {
		want[d.key] = d
	}

	// Release what is no longer wanted.
	u.pmMu.Lock()
	var stale []system.PortMapLease
	for k, h := range st.held {
		if _, ok := want[k]; !ok {
			stale = append(stale, h.lease)
			delete(st.held, k)
		}
	}
	for k := range st.failed {
		if _, ok := want[k]; !ok {
			delete(st.failed, k)
		}
	}
	u.pmMu.Unlock()
	for _, l := range stale {
		uctx, cancel := context.WithTimeout(ctx, portMapOpTimeout)
		_ = mapper.Unmap(uctx, in.wan, l) // best effort; it expires anyway
		cancel()
	}

	// Acquire the missing, renew the due.
	deniedSeen, nestedSeen := false, false
	for _, d := range in.desired {
		u.pmMu.Lock()
		h, held := st.held[d.key]
		u.pmMu.Unlock()
		if held && time.Now().Before(h.lease.RenewAfter) {
			continue
		}
		req := system.PortMapRequest{
			Proto: d.key.Proto, InternalPort: d.key.Port, ExternalHint: d.hint,
			Lifetime: portMapLease, Description: "nasnet:" + d.source,
		}
		if held {
			// A renewal asks for the port it holds, or clients lose it, and
			// carries the nonce PCP checks the mapping's ownership against.
			req.ExternalHint = h.lease.External.Port()
			req.Renewal, req.Nonce = true, h.lease.Nonce
		}
		mctx, cancel := context.WithTimeout(ctx, portMapOpTimeout)
		nl, err := mapper.Map(mctx, in.wan, probe, req)
		cancel()
		if err != nil {
			switch {
			case errors.Is(err, system.ErrPortMapDenied):
				deniedSeen = true
			case errors.Is(err, system.ErrPortMapNestedNAT):
				nestedSeen = true
			}
			u.pmMu.Lock()
			st.failed[d.key] = pmFailure{source: d.source, err: err.Error()}
			expired := held && time.Now().After(h.lease.GoodUntil)
			if expired {
				// Gone upstream too; stop pretending we hold it.
				delete(st.held, d.key)
			}
			u.pmMu.Unlock()
			if expired {
				u.emit(events.EventPortMapLost, map[string]any{
					"if_name": in.wan.IfName, "proto": d.key.Proto, "port": int(d.key.Port)})
			}
			logger.GetLogger().WithError(err).Warnf(
				"[PortMap] %s could not map %s on %s", in.wan.IfName, d.key, gw)
			continue
		}
		// Every reply carries the server's uptime. A reboot seen here means the
		// other leases are already gone, whatever our clock says.
		if u.noteEpoch(st, nl.Epoch) {
			u.forgetPortMapLeases(st, "the upstream router rebooted")
		}
		u.pmMu.Lock()
		st.held[d.key] = pmHeld{lease: nl, source: d.source}
		delete(st.failed, d.key)
		u.pmMu.Unlock()
		if !held {
			u.emit(events.EventPortMapAcquired, map[string]any{
				"if_name": in.wan.IfName, "proto": d.key.Proto, "port": int(d.key.Port),
				"external": nl.External.String(), "method": nl.Method})
		}
	}

	u.setPortMapVerdict(st, in, deniedSeen, nestedSeen)
}

// setPortMapVerdict reads the leases actually held against the set that was
// wanted. A mapping that never happened is not allowed to hide behind the ones
// that did.
func (u *networkUsecase) setPortMapVerdict(st *pmWANState, in pmInputs, deniedSeen, nestedSeen bool) {
	u.pmMu.Lock()
	prev := st.verdict
	privateExternal := false
	for _, h := range st.held {
		if !publicV4(h.lease.External.Addr()) {
			privateExternal = true
		}
	}
	missing := 0
	for _, d := range in.desired {
		if _, ok := st.held[d.key]; !ok {
			missing++
		}
	}
	firstFailure := ""
	for _, k := range sortedLeaseKeys(st.failed) {
		firstFailure = st.failed[k].err
		break
	}

	switch {
	case privateExternal || nestedSeen:
		st.verdict = PortMapVerdictNestedNAT
		st.lastErr = "the upstream router is itself behind NAT — its forward cannot reach the internet"
	case missing == 0:
		st.verdict, st.lastErr = PortMapVerdictOK, ""
	case deniedSeen && len(st.held) == 0:
		st.verdict, st.lastErr = PortMapVerdictDenied, "the upstream router refuses port mappings"
	case len(st.held) > 0:
		st.verdict = PortMapVerdictPartial
		st.lastErr = fmt.Sprintf("%d of %d ports could not be mapped", missing, len(in.desired))
	default:
		st.verdict, st.lastErr = PortMapVerdictError, firstFailure
	}
	verdict := st.verdict
	u.pmMu.Unlock()
	if verdict == PortMapVerdictDenied && prev != PortMapVerdictDenied {
		u.emit(events.EventPortMapDenied, map[string]any{"if_name": in.wan.IfName})
	}
}

func sortedLeaseKeys[V any](m map[pmLeaseKey]V) []pmLeaseKey {
	keys := make([]pmLeaseKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Proto != keys[j].Proto {
			return keys[i].Proto < keys[j].Proto
		}
		return keys[i].Port < keys[j].Port
	})
	return keys
}

// forgetPortMapLeases drops the local record without dialing. For the cases
// where the upstream state is already gone: a reboot, a vanished gateway.
func (u *networkUsecase) forgetPortMapLeases(st *pmWANState, why string) {
	u.pmMu.Lock()
	n := len(st.held)
	ifName := st.wan.IfName
	st.held = map[pmLeaseKey]pmHeld{}
	u.pmMu.Unlock()
	if n == 0 {
		return
	}
	logger.GetLogger().Warnf("[PortMap] %s dropped %d mapping(s): %s", ifName, n, why)
	u.emit(events.EventPortMapLost, map[string]any{"if_name": ifName, "count": n, "reason": why})
}

// releasePortMapLeases gives every mapping back to the router it was granted
// by, then forgets it.
func (u *networkUsecase) releasePortMapLeases(ctx context.Context, st *pmWANState, lost bool) {
	u.pmMu.Lock()
	wan := st.wan
	leases := make([]system.PortMapLease, 0, len(st.held))
	for _, h := range st.held {
		leases = append(leases, h.lease)
	}
	n := len(st.held)
	st.held = map[pmLeaseKey]pmHeld{}
	st.probed = false
	u.pmMu.Unlock()
	if n == 0 {
		return
	}
	mapper := u.portMapper()
	for _, l := range leases {
		uctx, cancel := context.WithTimeout(ctx, portMapOpTimeout)
		_ = mapper.Unmap(uctx, wan, l)
		cancel()
	}
	if lost {
		u.emit(events.EventPortMapLost, map[string]any{"if_name": wan.IfName, "count": n})
	}
}

// releaseAllPortMaps is the shutdown and disable path. Best effort, bounded.
func (u *networkUsecase) releaseAllPortMaps(ctx context.Context) {
	u.pmMu.Lock()
	keys := make([]string, 0, len(u.pmState))
	for k := range u.pmState {
		keys = append(keys, k)
	}
	u.pmMu.Unlock()
	for _, k := range keys {
		u.releasePortMapLeases(ctx, u.portMapStateFor(k), false)
	}
}

// prunePortMapStates releases the mappings of uplinks that are no longer
// uplinks. Without this a demoted WAN's forwards live on upstream, invisible.
func (u *networkUsecase) prunePortMapStates(ctx context.Context, live []pmInputs) {
	keep := make(map[string]bool, len(live))
	for _, in := range live {
		keep[in.key] = true
	}
	u.pmMu.Lock()
	var gone []string
	for k := range u.pmState {
		if !keep[k] {
			gone = append(gone, k)
		}
	}
	u.pmMu.Unlock()
	for _, k := range gone {
		st := u.portMapStateFor(k)
		u.releasePortMapLeases(ctx, st, true)
		u.pmMu.Lock()
		delete(u.pmState, k)
		u.pmMu.Unlock()
	}
}

// gatherPortMapInputs assembles one pmInputs per uplink. A WAN whose gateway
// is unreachable is skipped: leases can be neither renewed nor released over a
// dead link, and wan.up re-enters here anyway.
func (u *networkUsecase) gatherPortMapInputs(ctx context.Context) []pmInputs {
	uplinks, err := u.uplinks(ctx)
	if err != nil || len(uplinks) == 0 || u.IfRepo == nil {
		return nil
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	if err != nil {
		return nil
	}
	byIf := map[string]domain.NetworkInterface{}
	for _, r := range rows {
		byIf[r.IfName] = r
	}

	enabled := u.healthConfigSnapshot().PortMapEnabled
	// Nothing downstream of here is read while the feature is off, and the
	// pass still has to run: disabling is what releases the leases.
	var inbounds []InboundSpec
	var rules []domain.PortMapRule
	selfByIf := map[string]netip.Addr{}
	if enabled {
		if u.Inbounds != nil {
			inbounds, _ = u.Inbounds.EnabledInbounds(ctx)
		}
		if u.PortMapRepo != nil {
			rules, _ = u.PortMapRepo.List(ctx)
		}
		if u.Backend != nil {
			if addrs, err := u.Backend.Addrs(ctx); err == nil {
				for _, a := range addrs {
					p, err := netip.ParsePrefix(a.CIDR)
					if err != nil || !p.Addr().Is4() || p.Addr().IsLinkLocalUnicast() {
						continue
					}
					if _, seen := selfByIf[a.IfName]; !seen {
						selfByIf[a.IfName] = p.Addr()
					}
				}
			}
		}
	}

	var out []pmInputs
	for _, up := range uplinks {
		row := byIf[up.IfName]
		st := u.portMapStateFor(up.Key)
		if enabled && !row.Healthy {
			u.pmMu.Lock()
			st.suspended = true
			u.pmMu.Unlock()
			continue
		}
		gwStr := row.StaticGateway
		if gwStr == "" {
			gwStr = row.LearnedGateway
		}
		var gw netip.Addr
		if a, err := netip.ParseAddr(gwStr); err == nil {
			gw = a
		}
		u.pmMu.Lock()
		st.suspended = false
		u.pmMu.Unlock()
		out = append(out, pmInputs{
			wan: system.PortMapWAN{
				IfName: up.IfName, Gateway: gw, SelfIP: selfByIf[up.IfName],
			},
			key: up.Key, enabled: enabled,
			desired: desiredPortMaps(inbounds, rules, up.Key),
		})
	}
	return out
}

func (u *networkUsecase) portMapPass(ctx context.Context) {
	in := u.gatherPortMapInputs(ctx)
	// A suspended uplink is still ours; only one that stopped being an uplink
	// gets its state released.
	if len(in) > 0 {
		u.prunePortMapStates(ctx, append(in, u.suspendedPortMapInputs()...))
	}
	for _, one := range in {
		u.reconcilePortMapWAN(ctx, one)
	}
}

// suspendedPortMapInputs names the states that gatherPortMapInputs skipped
// because the uplink is down. They keep their leases.
func (u *networkUsecase) suspendedPortMapInputs() []pmInputs {
	u.pmMu.Lock()
	defer u.pmMu.Unlock()
	var out []pmInputs
	for k, st := range u.pmState {
		if st.suspended {
			out = append(out, pmInputs{key: k})
		}
	}
	return out
}

// StartPortMapLoop keeps upstream mappings alive: a slow tick for renewals,
// nudged early by wan.up, a gateway move, an inbound edit or the toggle.
func (u *networkUsecase) StartPortMapLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	kick := u.portMapKickCh()
	var sub events.Subscriber
	if u.EventBus != nil {
		sub = u.EventBus.SubscribeFiltered("portmap-loop", func(e events.Event) bool {
			return e.Type == events.EventWANUp || e.Type == events.EventWANGatewayChanged
		})
	}
	done := make(chan struct{})
	u.pmMu.Lock()
	u.pmDone = done
	u.pmMu.Unlock()
	go func() {
		defer close(done)
		if u.EventBus != nil {
			defer u.EventBus.Unsubscribe("portmap-loop")
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				// Going down: the releases need a clock of their own.
				rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				u.releaseAllPortMaps(rctx)
				cancel()
				return
			case <-t.C:
			case <-kick:
			case _, ok := <-sub:
				if !ok {
					// The bus closed before us. Keep the ticker; a nil channel
					// blocks forever, which is what an absent bus should do.
					sub = nil
				}
			}
			u.portMapPass(ctx)
		}
	}()
}

// StopPortMap waits for the loop to hand every lease back. The caller has
// already cancelled the loop's context; this only stops the process from
// exiting mid-release.
func (u *networkUsecase) StopPortMap(ctx context.Context) {
	u.pmMu.Lock()
	done := u.pmDone
	u.pmMu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
		logger.GetLogger().Warn("[PortMap] shutdown timed out before every lease was released")
	}
}

type PortMapProbeView struct {
	PMP    bool       `json:"pmp"`
	PCP    bool       `json:"pcp"`
	UPnP   bool       `json:"upnp"`
	SeenAt *time.Time `json:"seen_at,omitempty"`
}

type PortMapLeaseView struct {
	Source       string    `json:"source"`
	Proto        string    `json:"proto"`
	InternalPort int       `json:"internal_port"`
	ExternalIP   string    `json:"external_ip"`
	ExternalPort int       `json:"external_port"`
	Method       string    `json:"method"`
	RenewsAt     time.Time `json:"renews_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	Warning      string    `json:"warning,omitempty"`
}

// PortMapFailureView is a port that was asked for and not granted.
type PortMapFailureView struct {
	Source       string `json:"source"`
	Proto        string `json:"proto"`
	InternalPort int    `json:"internal_port"`
	Error        string `json:"error"`
}

type PortMapWANView struct {
	Key            string               `json:"key"`
	IfName         string               `json:"if_name"`
	Label          string               `json:"label"`
	Gateway        string               `json:"gateway,omitempty"`
	Verdict        string               `json:"verdict"`
	Error          string               `json:"error,omitempty"`
	Suspended      bool                 `json:"suspended"`
	Probe          PortMapProbeView     `json:"probe"`
	ExternalIP     string               `json:"external_ip,omitempty"`
	Leases         []PortMapLeaseView   `json:"leases"`
	Failures       []PortMapFailureView `json:"failures"`
	UnmappedRanges []string             `json:"unmapped_ranges,omitempty"`
}

type PortMapStatusView struct {
	Enabled bool             `json:"enabled"`
	WANs    []PortMapWANView `json:"wans"`
}

// portMapWANView snapshots one WAN. rangeTags names the inbounds whose
// PortRange cannot be mapped.
func (u *networkUsecase) portMapWANView(key, ifName, label string, st *pmWANState, rangeTags []string) PortMapWANView {
	u.pmMu.Lock()
	defer u.pmMu.Unlock()
	wv := PortMapWANView{
		Key: key, IfName: ifName, Label: label,
		Gateway: st.gateway, Verdict: st.verdict, Error: st.lastErr,
		Suspended: st.suspended, Leases: []PortMapLeaseView{},
		Failures: []PortMapFailureView{}, UnmappedRanges: rangeTags,
	}
	if wv.Verdict == "" {
		wv.Verdict = PortMapVerdictPending
	}
	if st.probed {
		seen := st.probe.SeenAt
		wv.Probe = PortMapProbeView{PMP: st.probe.PMP, PCP: st.probe.PCP, UPnP: st.probe.UPnP, SeenAt: &seen}
		if st.probe.ExternalIP.IsValid() {
			wv.ExternalIP = st.probe.ExternalIP.String()
		}
	}
	for _, k := range sortedLeaseKeys(st.held) {
		h := st.held[k]
		l := h.lease
		lv := PortMapLeaseView{
			Source: h.source, Proto: l.Proto, InternalPort: int(l.InternalPort),
			ExternalIP: l.External.Addr().String(), ExternalPort: int(l.External.Port()),
			Method: l.Method, RenewsAt: l.RenewAfter, ExpiresAt: l.GoodUntil,
		}
		if l.External.Port() != l.InternalPort {
			// The links keep advertising the internal port. Say so.
			lv.Warning = "granted external port differs from the advertised port"
		}
		wv.Leases = append(wv.Leases, lv)
		wv.ExternalIP = l.External.Addr().String()
	}
	for _, k := range sortedLeaseKeys(st.failed) {
		f := st.failed[k]
		wv.Failures = append(wv.Failures, PortMapFailureView{
			Source: f.source, Proto: k.Proto, InternalPort: int(k.Port), Error: f.err,
		})
	}
	return wv
}

// PortMapStatus is assembly only — it never dials.
func (u *networkUsecase) PortMapStatus(ctx context.Context) (*PortMapStatusView, error) {
	enabled := u.healthConfigSnapshot().PortMapEnabled
	view := &PortMapStatusView{Enabled: enabled, WANs: []PortMapWANView{}}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, err
	}
	var rangeTags []string
	labelByIf := map[string]string{}
	if enabled {
		if u.Inbounds != nil {
			if specs, err := u.Inbounds.EnabledInbounds(ctx); err == nil {
				seen := map[string]bool{}
				for _, s := range specs {
					if s.Enabled && !s.NoAutoMap && s.PortRange != "" && !seen[s.Tag] {
						seen[s.Tag] = true
						rangeTags = append(rangeTags, s.Tag)
					}
				}
			}
		}
		if u.IfRepo != nil {
			if rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN); err == nil {
				for _, r := range rows {
					labelByIf[r.IfName] = r.Label
				}
			}
		}
	}
	for _, up := range uplinks {
		st := u.portMapStateFor(up.Key)
		view.WANs = append(view.WANs, u.portMapWANView(up.Key, up.IfName, labelByIf[up.IfName], st, rangeTags))
	}
	return view, nil
}

// ForcePortMapProbe forgets the caches so the next pass asks again.
func (u *networkUsecase) ForcePortMapProbe(context.Context) {
	u.pmMu.Lock()
	for _, st := range u.pmState {
		st.probed = false
	}
	u.pmMu.Unlock()
	u.kickPortMap()
}

func (u *networkUsecase) ListPortMapRules(ctx context.Context) ([]domain.PortMapRule, error) {
	if u.PortMapRepo == nil {
		return []domain.PortMapRule{}, nil
	}
	rows, err := u.PortMapRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.PortMapRule{}
	}
	return rows, nil
}

func (u *networkUsecase) checkPortMapRule(ctx context.Context, r domain.PortMapRule, confirmed bool) ([]domain.Verdict, error) {
	if u.PortMapRepo == nil {
		return nil, fmt.Errorf("no port-map storage configured")
	}
	existing, err := u.PortMapRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(uplinks))
	for _, up := range uplinks {
		keys = append(keys, up.Key)
	}
	var forwards []domain.PortForward
	if u.PFRepo != nil {
		forwards, _ = u.PFRepo.List(ctx)
	}
	verdicts := domain.ValidatePortMapRule(domain.PortMapRuleInput{
		Existing: existing, New: r,
		PanelPort: u.PanelPort, SSHPort: portMapSSHPort,
		InboundPorts: u.autoMappedInboundPorts(ctx),
		Forwards:     forwards,
		UplinkKeys:   keys,
	})
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}
	if domain.Rejected(verdicts) {
		return verdicts, ErrValidationFailed
	}
	if !confirmed && needsConfirm(verdicts) {
		return verdicts, ErrConfirmRequired
	}
	return verdicts, nil
}

// afterPortMapRuleChange: the accept set and the desired set both moved.
func (u *networkUsecase) afterPortMapRuleChange(ctx context.Context) error {
	u.kickPortMap()
	return u.reapplyFilterInput(ctx)
}

func (u *networkUsecase) CreatePortMapRule(ctx context.Context, r domain.PortMapRule, confirmed bool) ([]domain.Verdict, error) {
	verdicts, err := u.checkPortMapRule(ctx, r, confirmed)
	if err != nil {
		return verdicts, err
	}
	if err := u.PortMapRepo.Create(ctx, &r); err != nil {
		return verdicts, err
	}
	return verdicts, u.afterPortMapRuleChange(ctx)
}

func (u *networkUsecase) UpdatePortMapRule(ctx context.Context, r domain.PortMapRule, confirmed bool) ([]domain.Verdict, error) {
	verdicts, err := u.checkPortMapRule(ctx, r, confirmed)
	if err != nil {
		return verdicts, err
	}
	if err := u.PortMapRepo.Update(ctx, &r); err != nil {
		return verdicts, err
	}
	return verdicts, u.afterPortMapRuleChange(ctx)
}

func (u *networkUsecase) DeletePortMapRule(ctx context.Context, id uint) error {
	if u.PortMapRepo == nil {
		return fmt.Errorf("no port-map storage configured")
	}
	if err := u.PortMapRepo.Delete(ctx, id); err != nil {
		return err
	}
	return u.afterPortMapRuleChange(ctx)
}
