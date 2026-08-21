package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/dohboot"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// Every tunnel's transport rides the secondary uplink's pin, which already has
// a lookup and a blackhole of its own at pref 52/53. That is what keeps
// WireGuard handshakes off the domestic line when the dish is down.
var vpnTransportMark = netmark.PinMark(uplinkIndexFor(domain.SlotSecondary))

// StarlinkDishSubnet is the dish's own management address space, reachable
// through the kill switch because it is link-scoped and carries no payload.
const StarlinkDishSubnet = "192.168.100.0/24"

// reresolveInterval bounds how often a silent tunnel triggers a fresh lookup.
// Each one is a DoH request out the raw uplink, so it is not free.
const reresolveInterval = 60 * time.Second

// VPNProfileView is one stored profile with its payload decoded.
type VPNProfileView struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
	Weight   int    `json:"weight"`
	WGSlot   *int   `json:"wg_slot"`
	// Config carries the private key as stored. Anyone reading this already
	// holds an admin session, which is the whole panel.
	Config    domain.WireGuardConfig `json:"config"`
	PublicKey string                 `json:"public_key"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	// Unreadable rows are still listed, so they can be deleted.
	Unreadable string `json:"unreadable,omitempty"`
}

// TunnelStatusView is one pool member's live state.
type TunnelStatusView struct {
	ProfileID        uint   `json:"profile_id"`
	Name             string `json:"name"`
	IfName           string `json:"if_name"`
	Priority         int    `json:"priority"`
	Weight           int    `json:"weight"`
	Connected        bool   `json:"connected"`
	HandshakeAgeSecs *int64 `json:"handshake_age_seconds"`
	RxBytes          int64  `json:"rx_bytes"`
	TxBytes          int64  `json:"tx_bytes"`
	Endpoint         string `json:"endpoint,omitempty"`
	MTU              int    `json:"mtu"`
	KeepaliveSecs    int    `json:"keepalive_seconds"`
	LastError        string `json:"last_error,omitempty"`
	// InPool says whether this member is in the nexthop set right now.
	InPool bool `json:"in_pool"`
}

// VPNPoolStatusView is the pool, or the reason there isn't one.
type VPNPoolStatusView struct {
	Tunnels []TunnelStatusView `json:"tunnels"`
	// SecondaryUplinkUp separates "the dish is down" from "the pool is down",
	// which need different things done about them.
	SecondaryUplinkUp bool `json:"secondary_uplink_up"`
	// KillSwitch is always true. Reported so the UI can state it rather than
	// offer it.
	KillSwitch bool `json:"kill_switch"`
}

// CreateVPNProfileRequest carries either something pasted or a filled-in form.
type CreateVPNProfileRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// Raw is a wireguard:// URI or the contents of a .conf file.
	Raw string `json:"raw"`
	// Config is the manual-entry path, used when Raw is empty.
	Config *domain.WireGuardConfig `json:"config"`
}

// tunnel is one enabled profile as the datapath sees it.
type tunnel struct {
	Profile *domain.VPNProfile
	Config  *domain.WireGuardConfig
	IfName  string
}

type vpnPool struct{ Tunnels []tunnel }

func (p vpnPool) Active() bool { return len(p.Tunnels) > 0 }

func (p vpnPool) IfNames() []string {
	out := make([]string, 0, len(p.Tunnels))
	for _, t := range p.Tunnels {
		out = append(out, t.IfName)
	}
	return out
}

// vpnPoolNow reads the enabled set. Every failure answers "not in the pool":
// the wrong answer sends foreign traffic out the raw uplink, so it fails
// towards the blackhole rather than towards the leak.
func (u *networkUsecase) vpnPoolNow(ctx context.Context) vpnPool {
	if u.VPNRepo == nil {
		return vpnPool{}
	}
	rows, err := u.VPNRepo.Enabled(ctx)
	if err != nil {
		return vpnPool{}
	}
	var pool vpnPool
	for i := range rows {
		p := &rows[i]
		if p.WGSlot == nil {
			continue
		}
		cfg, err := decodeWGConfig(p)
		if err != nil {
			continue
		}
		pool.Tunnels = append(pool.Tunnels, tunnel{
			Profile: p, Config: cfg, IfName: system.WGLinkNameFor(*p.WGSlot),
		})
	}
	return pool
}

func decodeWGConfig(p *domain.VPNProfile) (*domain.WireGuardConfig, error) {
	var cfg domain.WireGuardConfig
	if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
		return nil, fmt.Errorf("profile %q has an unreadable config: %w", p.Name, err)
	}
	return &cfg, nil
}

func (u *networkUsecase) wg() system.WGDevice {
	if u.WG != nil {
		return u.WG
	}
	return system.NewWGDevice()
}

func (u *networkUsecase) doh() dohboot.Resolver {
	if u.DoH != nil {
		return u.DoH
	}
	return dohboot.New(vpnTransportMark)
}

// ---------------------------------------------------------------- CRUD

func (u *networkUsecase) ListVPNProfiles(ctx context.Context) ([]VPNProfileView, error) {
	if u.VPNRepo == nil {
		return nil, errors.New("no VPN storage configured")
	}
	rows, err := u.VPNRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]VPNProfileView, 0, len(rows))
	for i := range rows {
		v, err := profileView(&rows[i])
		if err != nil {
			// One bad row must not hide the rest.
			r := rows[i]
			out = append(out, VPNProfileView{
				ID: r.ID, Name: r.Name, Type: r.Type, Enabled: r.Enabled,
				Priority: r.Priority, Weight: r.Weight, WGSlot: r.WGSlot,
				CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
				Unreadable: err.Error(),
			})
			continue
		}
		out = append(out, *v)
	}
	return out, nil
}

func profileView(p *domain.VPNProfile) (*VPNProfileView, error) {
	cfg, err := decodeWGConfig(p)
	if err != nil {
		return nil, err
	}
	v := &VPNProfileView{
		ID: p.ID, Name: p.Name, Type: p.Type, Enabled: p.Enabled,
		Priority: p.Priority, Weight: p.Weight, WGSlot: p.WGSlot,
		Config: *cfg, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	// Derived, never stored: the operator pastes a private key and needs the
	// public half to give their server.
	if pub, err := domain.WGPublicKeyOf(cfg.PrivateKey); err == nil {
		v.PublicKey = pub
	}
	return v, nil
}

// ParseVPNInput turns pasted text into a config without storing anything, so
// the UI can show what it understood before the operator commits.
func (u *networkUsecase) ParseVPNInput(_ context.Context, raw string) (*domain.WireGuardConfig, []domain.Verdict, error) {
	cfg, err := domain.ParseWireGuardConfig(raw)
	if err != nil {
		return nil, nil, err
	}
	if err := domain.ValidateWireGuardConfig(&cfg); err != nil {
		return nil, nil, err
	}
	return &cfg, vpnConfigVerdicts(&cfg), nil
}

func (u *networkUsecase) GenerateVPNKeypair() (priv, pub string, err error) {
	return domain.GenerateWGKeypair()
}

func (u *networkUsecase) CreateVPNProfile(ctx context.Context, req CreateVPNProfileRequest) (*VPNProfileView, error) {
	if u.VPNRepo == nil {
		return nil, errors.New("no VPN storage configured")
	}
	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(cfg.SuggestedName)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: the profile needs a name", ErrValidationFailed)
	}
	cfg.SuggestedName = ""

	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	p := &domain.VPNProfile{Name: name, Type: domain.VPNTypeWireGuard, Config: string(blob), Weight: 1}
	if err := u.VPNRepo.Create(ctx, p); err != nil {
		return nil, err
	}
	return profileView(p)
}

func (u *networkUsecase) UpdateVPNProfile(ctx context.Context, id uint, req CreateVPNProfileRequest) (*VPNProfileView, error) {
	if u.VPNRepo == nil {
		return nil, errors.New("no VPN storage configured")
	}
	stored, err := u.VPNRepo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// Editing a pool member is a routing change, so it goes through the same
	// enable pipeline rather than mutating under a live device.
	if stored.Enabled {
		return nil, fmt.Errorf("%w: turn this VPN off before editing it", ErrValidationFailed)
	}

	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}
	cfg.SuggestedName = ""
	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	stored.Config = string(blob)
	if n := strings.TrimSpace(req.Name); n != "" {
		stored.Name = n
	}
	if err := u.VPNRepo.Update(ctx, stored); err != nil {
		return nil, err
	}
	return profileView(stored)
}

func (u *networkUsecase) DeleteVPNProfile(ctx context.Context, id uint) error {
	if u.VPNRepo == nil {
		return errors.New("no VPN storage configured")
	}
	return u.VPNRepo.Delete(ctx, id)
}

func configFromRequest(req CreateVPNProfileRequest) (*domain.WireGuardConfig, error) {
	var cfg domain.WireGuardConfig
	switch {
	case strings.TrimSpace(req.Raw) != "":
		parsed, err := domain.ParseWireGuardConfig(req.Raw)
		if err != nil {
			return nil, err
		}
		cfg = parsed
	case req.Config != nil:
		cfg = *req.Config
	default:
		return nil, fmt.Errorf("%w: nothing to import", ErrValidationFailed)
	}
	if err := domain.ValidateWireGuardConfig(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	return &cfg, nil
}

// ---------------------------------------------------------------- verdicts

// vpnConfigVerdicts reports what is legal but surprising about a config.
func vpnConfigVerdicts(cfg *domain.WireGuardConfig) []domain.Verdict {
	vs := []domain.Verdict{}
	if !domain.CoversDefaultRoute(cfg.Peer.AllowedIPs) {
		vs = append(vs, domain.Verdict{
			Rule:  "V32",
			Level: domain.LevelWarn,
			Message: fmt.Sprintf("This VPN only carries %s. It cannot join the pool: "+
				"a member has to carry everything (0.0.0.0/0).",
				strings.Join(cfg.Peer.AllowedIPs, ", ")),
		})
	}
	return vs
}

// ---------------------------------------------------------------- pool membership

func (u *networkUsecase) EnableVPNProfile(ctx context.Context, id uint) ([]domain.Verdict, *ApplyView, error) {
	if u.VPNRepo == nil {
		return nil, nil, errors.New("no VPN storage configured")
	}
	profile, err := u.VPNRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if profile.Enabled {
		return []domain.Verdict{}, nil, nil
	}
	cfg, err := decodeWGConfig(profile)
	if err != nil {
		return nil, nil, err
	}
	if err := domain.ValidateWireGuardConfig(cfg); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	verdicts := []domain.Verdict{}
	enabled, err := u.VPNRepo.Enabled(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Members carry the default route; anything narrower silently blackholes
	// most of the LAN's foreign traffic.
	if !domain.CoversDefaultRoute(cfg.Peer.AllowedIPs) {
		verdicts = append(verdicts, domain.Verdict{Rule: "V32", Level: domain.LevelReject,
			Message: fmt.Sprintf("This VPN only carries %s. A pool member has to carry "+
				"everything (0.0.0.0/0).", strings.Join(cfg.Peer.AllowedIPs, ", "))})
	}
	if len(enabled) >= domain.MaxEnabledProfiles {
		verdicts = append(verdicts, domain.Verdict{Rule: "V37", Level: domain.LevelReject,
			Message: "All 8 tunnel slots are in use. Turn one off first."})
	}
	for i := range enabled {
		other, err := decodeWGConfig(&enabled[i])
		if err != nil {
			continue
		}
		if cfg.ListenPort != 0 && cfg.ListenPort == other.ListenPort {
			verdicts = append(verdicts, domain.Verdict{Rule: "V35", Level: domain.LevelReject,
				Message: fmt.Sprintf("%q already listens on port %d.", enabled[i].Name, cfg.ListenPort)})
		}
		if cfg.Address == other.Address {
			verdicts = append(verdicts, domain.Verdict{Rule: "V36", Level: domain.LevelReject,
				Message: fmt.Sprintf("%q already uses the tunnel address %s.", enabled[i].Name, cfg.Address)})
		}
	}
	if domain.Rejected(verdicts) {
		return verdicts, nil, nil
	}

	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !hasSlot(uplinks, domain.SlotSecondary) {
		// Provisioning before the dish arrives is a real workflow, and the kill
		// switch already covers every state in between.
		verdicts = append(verdicts, domain.Verdict{Rule: "V33", Level: domain.LevelWarn,
			Message: "No secondary uplink is assigned yet, so the tunnel has nothing to " +
				"run over. It will connect on its own once one appears."})
	}

	// Resolve now, so the tunnel can come up later without a resolver — the
	// foreign one lives inside the pool.
	host, err := domain.ParseWGEndpoint(cfg.Peer.Endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	addr, err := u.doh().Resolve(ctx, host)
	if err != nil {
		verdicts = append(verdicts, domain.Verdict{Rule: "V34", Level: domain.LevelReject,
			Message: fmt.Sprintf("The endpoint %q could not be resolved: %v", host, err)})
		return verdicts, nil, nil
	}
	cfg.PinnedEndpointIP = addr.String()

	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	profile.Config = string(blob)

	var plan system.Plan
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: fmt.Sprintf("add %q to the VPN pool", profile.Name),
			Do: func(ctx context.Context) error {
				if err := u.VPNRepo.Update(ctx, profile); err != nil {
					return err
				}
				return u.VPNRepo.SetEnabled(ctx, profile.ID, true)
			},
			Undo: func(ctx context.Context) error {
				return u.VPNRepo.SetEnabled(ctx, profile.ID, false)
			},
		},
		system.Op{
			Desc: "bring the pool's tunnel interfaces up",
			Do:   func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
			Undo: func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
		},
		system.Op{
			// The tunnels' resolver lines and table names live in rendered files,
			// so they have to be rewritten here or resolved keeps querying out
			// the raw uplink.
			Desc: "render the uplink units and the routing table names",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "route the foreign group into the pool and arm the kill switch",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	)

	view, err := u.runVPNPlan(ctx, plan)
	if err != nil {
		return verdicts, nil, err
	}
	return verdicts, view, nil
}

func (u *networkUsecase) DisableVPNProfile(ctx context.Context, id uint) ([]domain.Verdict, *ApplyView, error) {
	if u.VPNRepo == nil {
		return nil, nil, errors.New("no VPN storage configured")
	}
	profile, err := u.VPNRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !profile.Enabled {
		return []domain.Verdict{}, nil, nil
	}

	var plan system.Plan
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: fmt.Sprintf("remove %q from the VPN pool", profile.Name),
			Do:   func(ctx context.Context) error { return u.VPNRepo.SetEnabled(ctx, profile.ID, false) },
			Undo: func(ctx context.Context) error { return u.VPNRepo.SetEnabled(ctx, profile.ID, true) },
		},
		system.Op{
			Desc: "take the member's tunnel interface down",
			Do:   func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
			Undo: func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
		},
		system.Op{
			Desc: "render the uplink units and the routing table names",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "reroute the pool, or blackhole the foreign group if it is now empty",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	)

	view, err := u.runVPNPlan(ctx, plan)
	if err != nil {
		return nil, nil, err
	}
	return []domain.Verdict{}, view, nil
}

// SetVPNProfileRole is instant: redistribution can't strand the box, so it
// skips the dead-man.
func (u *networkUsecase) SetVPNProfileRole(ctx context.Context, id uint, priority, weight int) error {
	if u.VPNRepo == nil {
		return errors.New("no VPN storage configured")
	}
	if err := domain.ValidatePoolRole(priority, weight); err != nil {
		return fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	if err := u.VPNRepo.SetRole(ctx, id, priority, weight); err != nil {
		return err
	}
	return u.applyPoolRoutes(ctx)
}

func (u *networkUsecase) runVPNPlan(ctx context.Context, plan system.Plan) (*ApplyView, error) {
	rec, err := u.applier.Apply(ctx, plan, !system.TakeoverDone(u.Paths))
	if err != nil {
		return nil, err
	}
	ops := rec.Ops
	if ops == nil {
		ops = []string{}
	}
	view := &ApplyView{PlanID: rec.ID, Ops: ops}
	if rec.Deadline != nil {
		view.ConfirmDeadlineUnix = rec.Deadline.Unix()
	}
	return view, nil
}

// applyVPNDevices makes the tunnel links match the stored pool: one per
// enabled profile, none else. Idempotent, so boot, apply and undo all use it.
func (u *networkUsecase) applyVPNDevices(ctx context.Context) error {
	pool := u.vpnPoolNow(ctx)
	want := map[string]bool{}
	for _, t := range pool.Tunnels {
		want[t.IfName] = true
	}
	// Orphans first: a disabled profile's link must not keep answering.
	if have, err := u.wg().List(ctx); err == nil {
		for _, name := range have {
			if !want[name] {
				if err := u.wg().Delete(ctx, name); err != nil {
					return err
				}
			}
		}
	}
	for _, t := range pool.Tunnels {
		// A profile restored from an older snapshot can carry a hostname with
		// no pinned address. Resolve here rather than fail: this runs at boot,
		// and a member that will not come up drags the whole tier's odds down.
		if err := u.ensurePinnedEndpoint(ctx, t); err != nil {
			return err
		}
		apply, err := wgApplyConfig(t.Config)
		if err != nil {
			return err
		}
		if err := u.wg().Ensure(ctx, t.IfName, apply); err != nil {
			return err
		}
	}
	return nil
}

// ensurePinnedEndpoint fills in a missing pin. Does nothing in the normal case,
// where the enable pinned the address already.
func (u *networkUsecase) ensurePinnedEndpoint(ctx context.Context, t tunnel) error {
	if t.Config.PinnedEndpointIP != "" {
		return nil
	}
	host, err := domain.ParseWGEndpoint(t.Config.Peer.Endpoint)
	if err != nil {
		return err
	}
	if _, isAddr := netip.ParseAddr(host); isAddr == nil {
		return nil // already an address; nothing to pin
	}
	addr, err := u.doh().Resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve the endpoint %q: %w", host, err)
	}
	t.Config.PinnedEndpointIP = addr.String()
	if blob, err := json.Marshal(t.Config); err == nil {
		t.Profile.Config = string(blob)
		_ = u.VPNRepo.Update(ctx, t.Profile)
	}
	return nil
}

// wgApplyConfig turns the stored profile into what the kernel needs, filling in
// the defaults a config is allowed to leave out.
func wgApplyConfig(cfg *domain.WireGuardConfig) (system.WGApplyConfig, error) {
	host, err := domain.ParseWGEndpoint(cfg.Peer.Endpoint)
	if err != nil {
		return system.WGApplyConfig{}, err
	}
	port := cfg.Peer.Endpoint[strings.LastIndex(cfg.Peer.Endpoint, ":")+1:]

	// The pinned address, or the endpoint itself when it was already one.
	target := cfg.PinnedEndpointIP
	if target == "" {
		target = host
	}
	endpoint, err := netip.ParseAddrPort(target + ":" + port)
	if err != nil {
		return system.WGApplyConfig{}, fmt.Errorf("endpoint %q: %w", cfg.Peer.Endpoint, err)
	}

	address, err := netip.ParsePrefix(cfg.Address)
	if err != nil {
		return system.WGApplyConfig{}, fmt.Errorf("address %q: %w", cfg.Address, err)
	}

	out := system.WGApplyConfig{
		PrivateKey:    cfg.PrivateKey,
		PeerPublicKey: cfg.Peer.PublicKey,
		PresharedKey:  cfg.Peer.PresharedKey,
		Endpoint:      endpoint,
		Address:       address,
		MTU:           cfg.MTU,
		ListenPort:    cfg.ListenPort,
		FirewallMark:  vpnTransportMark,
	}
	if out.MTU == 0 {
		out.MTU = domain.DefaultWGMTU
	}
	keepalive := cfg.Peer.PersistentKeepalive
	if keepalive == 0 {
		// Starlink is behind CGNAT: with no keepalive the mapping expires and
		// the far side can never reach us again.
		keepalive = domain.DefaultWGKeepalive
	}
	out.Keepalive = time.Duration(keepalive) * time.Second

	for _, s := range cfg.Peer.AllowedIPs {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return system.WGApplyConfig{}, fmt.Errorf("allowed IP %q: %w", s, err)
		}
		out.AllowedIPs = append(out.AllowedIPs, p)
	}
	if ip, err := netip.ParseAddr(cfg.DNS); err == nil && ip.Is4() {
		out.DNS = ip
	} else if ip, err := netip.ParseAddr(DefaultForeignDNS); err == nil {
		out.DNS = ip
	}
	return out, nil
}

// ---------------------------------------------------------------- kernel state

// poolMember is what the nexthop choice needs to know about one tunnel.
type poolMember struct {
	IfName   string
	Slot     int
	Priority int
	Weight   int
	Healthy  bool
}

// poolNexthops picks the active tier: the best priority holding a healthy
// member, ejecting its dead siblings. A pool with no healthy member anywhere
// keeps its best tier routed anyway — a dead tunnel beats a blackhole, and
// the probes need the path to see the recovery.
func poolNexthops(members []poolMember) []system.Nexthop {
	if len(members) == 0 {
		return nil
	}
	best, anyHealthy := 0, false
	for _, m := range members {
		if m.Healthy && (!anyHealthy || m.Priority < best) {
			best, anyHealthy = m.Priority, true
		}
	}
	if !anyHealthy {
		best = members[0].Priority
		for _, m := range members {
			if m.Priority < best {
				best = m.Priority
			}
		}
	}
	var out []system.Nexthop
	for _, m := range members {
		if m.Priority != best || (anyHealthy && !m.Healthy) {
			continue
		}
		out = append(out, system.Nexthop{OifName: m.IfName, Weight: m.Weight})
	}
	return out
}

// poolMembers pairs the pool with what the dampers currently believe. A member
// with no damper yet is healthy: the damper itself starts optimistic.
func (u *networkUsecase) poolMembers(pool vpnPool) []poolMember {
	u.healthMu.Lock()
	defer u.healthMu.Unlock()
	u.ensureHealthMaps()
	out := make([]poolMember, 0, len(pool.Tunnels))
	for _, t := range pool.Tunnels {
		healthy := true
		if s, ok := u.inetStates[t.IfName]; ok {
			healthy = !s.down
		}
		out = append(out, poolMember{
			IfName: t.IfName, Slot: *t.Profile.WGSlot,
			Priority: t.Profile.Priority, Weight: t.Profile.Weight, Healthy: healthy,
		})
	}
	return out
}

// applyVPNRoutes makes table 203 match the pool. Everything in it is removed
// when the pool is empty, so a stale default cannot outlive its device.
func (u *networkUsecase) applyVPNRoutes(ctx context.Context, pool vpnPool, uplinks []Uplink) error {
	if !pool.Active() {
		have, err := u.Backend.RouteList(ctx, system.WGTable)
		if err != nil {
			// An empty table is not an error, so this read really failed.
			return fmt.Errorf("read the pool's routing table: %w", err)
		}
		for _, r := range have {
			if err := u.Backend.RouteDel(ctx, r); err != nil {
				return fmt.Errorf("clear the pool's routing table: %w", err)
			}
		}
		return nil
	}

	members := u.poolMembers(pool)
	routes := []system.Route{{
		Table: system.WGTable, Dest: "default", Nexthops: poolNexthops(members),
	}}
	// One escape hatch per member: a probe bound to an ejected tunnel still
	// needs a route out of it, or the recovery is unobservable.
	valid := map[int]bool{}
	for _, m := range members {
		valid[probeRouteMetric+m.Slot] = true
		routes = append(routes, system.Route{
			Table: system.WGTable, Dest: "default", OifName: m.IfName,
			Metric: probeRouteMetric + m.Slot,
		})
	}
	// The dish's own management address stays reachable, which is the one thing
	// on the raw uplink worth keeping.
	for _, up := range uplinks {
		if up.Slot == domain.SlotSecondary {
			routes = append(routes, system.Route{
				Table: system.WGTable, Dest: StarlinkDishSubnet,
				OifName: up.IfName, Scope: "link",
			})
		}
	}
	for _, r := range routes {
		if err := u.Backend.RouteReplace(ctx, r); err != nil {
			return fmt.Errorf("route %s into the pool: %w", r.Dest, err)
		}
	}
	// A slot freed by a disable leaves its escape hatch behind; sweep it.
	if have, err := u.Backend.RouteList(ctx, system.WGTable); err == nil {
		for _, r := range have {
			if r.Dest == "default" && r.Metric >= probeRouteMetric &&
				r.Metric < probeRouteMetric+system.MaxWGSlots && !valid[r.Metric] {
				_ = u.Backend.RouteDel(ctx, r)
			}
		}
	}
	return nil
}

// ApplyKillSwitchState installs the chains that stop anything leaving the
// secondary uplink in the clear.
//
// Rendered whenever a secondary uplink exists, with or without a pool, and
// deliberately independent of the input firewall: that one is a setting the
// operator chooses, this one is not.
func ApplyKillSwitchState(ctx context.Context, m *nft.Manager, uplinks []Uplink, gateway string, probeIPs []string) error {
	if m == nil {
		return nil
	}
	var secondary string
	for _, up := range uplinks {
		if up.Slot == domain.SlotSecondary {
			secondary = up.IfName
		}
	}
	return m.Update(ctx, func(rs *nft.Ruleset) {
		if secondary == "" {
			rs.KillSwitch = nil
			return
		}
		rs.KillSwitch = &nft.KillSwitch{
			SecondaryIfName: secondary,
			GatewayIP:       gateway,
			DishSubnet:      StarlinkDishSubnet,
			MarkMask:        netmark.MaskPin,
			MarkValue:       vpnTransportMark,
			BootstrapIPs:    dohboot.BootstrapIPs(),
			ProbeMark:       netmark.PinMark(netmark.PinProbe),
			ProbeIPs:        probeIPs,
		}
	})
}

// secondaryGateway is the address the health probe has to reach, which is the
// one exemption that cannot be written until DHCP has answered.
func secondaryGateway(uplinks []Uplink, rows []domain.NetworkInterface) string {
	var key string
	for _, up := range uplinks {
		if up.Slot == domain.SlotSecondary {
			key = up.Key
		}
	}
	if key == "" {
		return ""
	}
	for i := range rows {
		if rows[i].Key != key {
			continue
		}
		if rows[i].StaticGateway != "" {
			return rows[i].StaticGateway
		}
		return rows[i].LearnedGateway
	}
	return ""
}

// ---------------------------------------------------------------- status

func (u *networkUsecase) VPNStatus(ctx context.Context) (*VPNPoolStatusView, error) {
	out := &VPNPoolStatusView{Tunnels: []TunnelStatusView{}, KillSwitch: true}

	uplinks, err := u.uplinks(ctx)
	if err == nil {
		out.SecondaryUplinkUp = uplinkHealthy(uplinks, u.healthyKeys(ctx), domain.SlotSecondary)
	}

	pool := u.vpnPoolNow(ctx)
	inPool := map[string]bool{}
	for _, n := range u.currentPoolNexthops() {
		inPool[n.OifName] = true
	}
	for _, t := range pool.Tunnels {
		tv := TunnelStatusView{
			ProfileID: t.Profile.ID, Name: t.Profile.Name, IfName: t.IfName,
			Priority: t.Profile.Priority, Weight: t.Profile.Weight,
			MTU: t.Config.MTU, KeepaliveSecs: t.Config.Peer.PersistentKeepalive,
			InPool: inPool[t.IfName],
		}
		if tv.MTU == 0 {
			tv.MTU = domain.DefaultWGMTU
		}
		if tv.KeepaliveSecs == 0 {
			tv.KeepaliveSecs = domain.DefaultWGKeepalive
		}
		tv.Endpoint = t.Config.Peer.Endpoint
		if t.Config.PinnedEndpointIP != "" {
			tv.Endpoint = t.Config.PinnedEndpointIP
		}

		st, err := u.wg().Status(ctx, t.IfName)
		if err != nil {
			if !errors.Is(err, system.ErrNoWGDevice) {
				tv.LastError = err.Error()
			} else {
				tv.LastError = "The tunnel interface is not present."
			}
			out.Tunnels = append(out.Tunnels, tv)
			continue
		}
		tv.RxBytes, tv.TxBytes = st.RxBytes, st.TxBytes
		if st.Endpoint != "" {
			tv.Endpoint = st.Endpoint
		}
		if !st.LastHandshake.IsZero() {
			age := int64(time.Since(st.LastHandshake).Seconds())
			tv.HandshakeAgeSecs = &age
		}
		tv.Connected = st.Connected()
		out.Tunnels = append(out.Tunnels, tv)
	}
	return out, nil
}

func hasSlot(uplinks []Uplink, slot domain.UplinkSlot) bool {
	for _, u := range uplinks {
		if u.Slot == slot {
			return true
		}
	}
	return false
}

// healthyKeys reads which uplinks the health loop currently believes are up.
func (u *networkUsecase) healthyKeys(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if u.IfRepo == nil {
		return out
	}
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return out
	}
	for i := range rows {
		out[rows[i].Key] = rows[i].Healthy
	}
	return out
}

func uplinkHealthy(uplinks []Uplink, healthy map[string]bool, slot domain.UplinkSlot) bool {
	for _, u := range uplinks {
		if u.Slot == slot {
			return healthy[u.Key]
		}
	}
	return false
}

// ---------------------------------------------------------------- health loop

// checkVPNHealth runs on the health tick. It reports per-member state changes
// and, when a hostname endpoint has gone quiet, looks it up again — providers
// move them.
func (u *networkUsecase) checkVPNHealth(ctx context.Context) {
	pool := u.vpnPoolNow(ctx)
	present := map[string]bool{}
	for _, t := range pool.Tunnels {
		present[t.IfName] = true
	}
	u.healthMu.Lock()
	u.ensureHealthMaps()
	var gone []string
	for name := range u.tunnelWasUp {
		if !present[name] {
			gone = append(gone, name)
			delete(u.tunnelWasUp, name)
			delete(u.tunnelLastResolve, name)
		}
	}
	u.healthMu.Unlock()
	for range gone {
		u.emit(events.EventVPNDown, map[string]any{"reason": "removed from the pool"})
	}

	for _, t := range pool.Tunnels {
		st, err := u.wg().Status(ctx, t.IfName)
		up := err == nil && st.Connected()
		u.healthMu.Lock()
		was := u.tunnelWasUp[t.IfName]
		u.tunnelWasUp[t.IfName] = up
		u.healthMu.Unlock()
		if up != was {
			ev := events.EventVPNDown
			if up {
				ev = events.EventVPNUp
			}
			u.emit(ev, map[string]any{"profile_id": t.Profile.ID, "name": t.Profile.Name})
		}
		if !up {
			u.reresolveEndpoint(ctx, t)
		}
	}
}

// reresolveEndpoint looks the endpoint up again after a silence. Only for
// hostname endpoints: an address cannot have moved.
func (u *networkUsecase) reresolveEndpoint(ctx context.Context, t tunnel) {
	host, err := domain.ParseWGEndpoint(t.Config.Peer.Endpoint)
	if err != nil {
		return
	}
	if _, isAddr := netip.ParseAddr(host); isAddr == nil {
		return
	}
	u.healthMu.Lock()
	u.ensureHealthMaps()
	last := u.tunnelLastResolve[t.IfName]
	if time.Since(last) < reresolveInterval {
		u.healthMu.Unlock()
		return
	}
	u.tunnelLastResolve[t.IfName] = time.Now()
	u.healthMu.Unlock()

	addr, err := u.doh().Resolve(ctx, host)
	if err != nil || addr.String() == t.Config.PinnedEndpointIP {
		return
	}

	port := t.Config.Peer.Endpoint[strings.LastIndex(t.Config.Peer.Endpoint, ":")+1:]
	ep, err := netip.ParseAddrPort(addr.String() + ":" + port)
	if err != nil {
		return
	}
	if err := u.wg().UpdateEndpoint(ctx, t.IfName, ep); err != nil {
		return
	}
	t.Config.PinnedEndpointIP = addr.String()
	if blob, err := json.Marshal(t.Config); err == nil {
		t.Profile.Config = string(blob)
		_ = u.VPNRepo.Update(ctx, t.Profile)
	}
}

// restorePool is the rollback hook. An empty set is a real value: it disables
// everything and tears the devices down, because leaving them up would keep
// routes pointing into tunnels the restored rules no longer know about.
func (u *networkUsecase) restorePool(ctx context.Context, want []domain.VPNProfile) error {
	if u.VPNRepo == nil {
		return nil
	}
	return NewVPNPoolRestorer(u.VPNRepo, u.wg())(ctx, want)
}

// NewVPNPoolRestorer builds the same hook for a caller that cannot construct
// the whole usecase — the dead-man runs in its own process, on purpose, because
// a bad network apply is most likely to break the panel itself.
func NewVPNPoolRestorer(repo repository.VPNRepository, wg system.WGDevice) func(context.Context, []domain.VPNProfile) error {
	return func(ctx context.Context, want []domain.VPNProfile) error {
		if err := repo.SetPool(ctx, want); err != nil {
			return err
		}
		keep := map[string]bool{}
		for i := range want {
			p := &want[i]
			if p.WGSlot == nil {
				continue
			}
			name := system.WGLinkNameFor(*p.WGSlot)
			keep[name] = true
			cfg, err := decodeWGConfig(p)
			if err != nil {
				return err
			}
			// No resolution here: a revert must not depend on a resolver that
			// may only exist inside a tunnel being restored.
			apply, err := wgApplyConfig(cfg)
			if err != nil {
				return err
			}
			if err := wg.Ensure(ctx, name, apply); err != nil {
				return err
			}
		}
		if have, err := wg.List(ctx); err == nil {
			for _, name := range have {
				if !keep[name] {
					if err := wg.Delete(ctx, name); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
}
