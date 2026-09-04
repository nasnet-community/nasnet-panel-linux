package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
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

// transportMark is the pin of the WAN this tunnel rides. Every uplink already
// has a lookup and a blackhole in the pin block, so the mark alone steers the
// handshake and nothing falls through to another WAN.
//
// No WAN dealt means none is assigned yet. Slot one's pin is still the answer:
// an unmarked handshake would take the domestic line, which is the leak this
// mark exists to stop.
func transportMark(up Uplink) uint32 {
	if up.UplinkIndex == 0 {
		return netmark.PinMark(uplinkIndexFor(domain.SlotSecondary))
	}
	return netmark.PinMark(up.UplinkIndex)
}

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
	ProfileID uint   `json:"profile_id"`
	Name      string `json:"name"`
	IfName    string `json:"if_name"`
	// The operator's order, first is 0. Only the chain acts on it.
	Position         int    `json:"position"`
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
	// Via is the WAN this tunnel's transport rides.
	Via *TunnelVia `json:"via,omitempty"`
}

// TunnelVia names a tunnel's WAN, and says whether an operator chose it.
type TunnelVia struct {
	IfName string `json:"if_name"`
	Label  string `json:"label"`
	Key    string `json:"key"`
	Pinned bool   `json:"pinned"`
}

// VPNUplinkView is one secondary the pool can ride.
type VPNUplinkView struct {
	Slot   string `json:"slot"`
	IfName string `json:"if_name"`
	Label  string `json:"label"`
	Key    string `json:"key"`
	Up     bool   `json:"up"`
}

// VPNPoolStatusView is the pool, or the reason there isn't one.
type VPNPoolStatusView struct {
	Tunnels []TunnelStatusView `json:"tunnels"`
	// Uplinks separates "the WANs are down" from "the pool is down", which
	// need different things done about them.
	Uplinks []VPNUplinkView `json:"uplinks"`
	// KillSwitch is always true. Reported so the UI can state it rather than
	// offer it.
	KillSwitch bool `json:"kill_switch"`
	// Carrier is empty unless one tunnel carries at a time.
	Strategy PoolStrategy `json:"strategy"`
	Carrier  string       `json:"carrier,omitempty"`
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
//
// Callers that DELETE on the strength of the answer want vpnPoolRead instead —
// an unreadable pool looks identical to an empty one, and tearing every tunnel
// down because sqlite was busy is not failing safe.
func (u *networkUsecase) vpnPoolNow(ctx context.Context) vpnPool {
	pool, _ := u.vpnPoolRead(ctx)
	return pool
}

func (u *networkUsecase) vpnPoolRead(ctx context.Context) (vpnPool, error) {
	if u.VPNRepo == nil {
		return vpnPool{}, nil
	}
	rows, err := u.VPNRepo.Enabled(ctx)
	if err != nil {
		return vpnPool{}, fmt.Errorf("read the enabled profiles: %w", err)
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
	// Slot order, not tier order: an oif rule's pref is its position in this
	// list, so sorting by priority would move the rules every time someone
	// edits a tier — without anything reinstalling them.
	sort.Slice(pool.Tunnels, func(i, j int) bool {
		return *pool.Tunnels[i].Profile.WGSlot < *pool.Tunnels[j].Profile.WGSlot
	})
	return pool, nil
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

func (u *networkUsecase) doh(ctx context.Context) dohboot.Resolver {
	if u.DoH != nil {
		return u.DoH
	}
	return dohboot.New(u.bootstrapMark(ctx))
}

// bootstrapMark is the first secondary's pin. Endpoint lookups happen before
// any tunnel is up, and that leg is the one the kill switch exempts for them.
func (u *networkUsecase) bootstrapMark(ctx context.Context) uint32 {
	if uplinks, err := u.uplinks(ctx); err == nil {
		best := Uplink{}
		for _, up := range secondariesOf(uplinks) {
			if best.UplinkIndex == 0 || up.UplinkIndex < best.UplinkIndex {
				best = up
			}
		}
		if best.UplinkIndex != 0 {
			return transportMark(best)
		}
	}
	return netmark.PinMark(uplinkIndexFor(domain.SlotSecondary))
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

// Same inner IP is a collision whatever the mask says, so compare addresses.
func sameTunnelIP(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return pa.Addr() == pb.Addr()
}

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
		if sameTunnelIP(cfg.Address, other.Address) {
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
	if len(secondariesOf(uplinks)) == 0 {
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
	addr, err := u.doh(ctx).Resolve(ctx, host)
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
				if err := u.VPNRepo.SetEnabled(ctx, profile.ID, true); err != nil {
					return err
				}
				// At position 0 it would take the traffic off the first one.
				return u.appendToChain(ctx, profile.ID)
			},
		},
		system.Op{
			Desc: "bring the pool's tunnel interfaces up",
			Do:   func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
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
		},
		system.Op{
			Desc: "take the member's tunnel interface down",
			Do:   func(ctx context.Context) error { return u.applyVPNDevices(ctx) },
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

// SetVPNProfileTransport pins a tunnel to one WAN, or clears the pin. Instant
// like a role edit: a mis-pin kills one tunnel, never the box.
func (u *networkUsecase) SetVPNProfileTransport(ctx context.Context, id uint, uplinkKey string) error {
	if u.VPNRepo == nil {
		return errors.New("no VPN storage configured")
	}
	if uplinkKey != "" {
		uplinks, err := u.uplinks(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, up := range secondariesOf(uplinks) {
			if up.Key == uplinkKey {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("%w: the pin must name a secondary uplink", ErrValidationFailed)
		}
	}
	if err := u.VPNRepo.SetTransport(ctx, id, uplinkKey); err != nil {
		return err
	}
	// The mark changes now, not on the next damper edge.
	return u.applyTransportAssignments(ctx)
}

func (u *networkUsecase) runVPNPlan(ctx context.Context, plan system.Plan) (*ApplyView, error) {
	// One at a time: the applier checks for an armed change and then writes the
	// marker, so two plans racing leave the first one armed but unconfirmable.
	u.planMu.Lock()
	defer u.planMu.Unlock()
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
	pool, err := u.vpnPoolRead(ctx)
	if err != nil {
		return err
	}
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	deal := assignTransport(pool, secondariesOf(uplinks), u.healthySecondaries(uplinks))
	u.rememberTransport(deal)
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
	// Per member, not all-or-nothing: this runs inside Reconcile at boot, and
	// one tunnel that will not come up must not cost the box its rules, its
	// nft state and its LAN.
	var errs []error
	for _, t := range pool.Tunnels {
		// A profile restored from an older snapshot can carry a hostname with
		// no pinned address, so resolve it here.
		if err := u.ensurePinnedEndpoint(ctx, t); err != nil {
			errs = append(errs, err)
			continue
		}
		apply, err := wgApplyConfig(t.Config, transportMark(deal[t.IfName]))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := u.wg().Ensure(ctx, t.IfName, apply); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
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
	addr, err := u.doh(ctx).Resolve(ctx, host)
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
func wgApplyConfig(cfg *domain.WireGuardConfig, mark uint32) (system.WGApplyConfig, error) {
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
		FirewallMark:  mark,
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
// Priority is the operator's drag order, and only the failover chain reads it.
type poolMember struct {
	IfName   string
	Slot     int
	Priority int
	RTTms    int
	Healthy  bool
}

// One weight for everyone: the strategy says who carries, not in what ratio.
func poolNexthops(members []poolMember, strategy PoolStrategy, carrier string) []system.Nexthop {
	var out []system.Nexthop
	for _, m := range carriersFor(members, strategy, carrier) {
		out = append(out, system.Nexthop{OifName: m.IfName, Weight: 1})
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
			down, _ := s.snapshot()
			healthy = !down
		}
		rtt := 0
		if r, ok := u.rings[t.IfName]; ok {
			rtt = medianRTT(r.snapshot(), 20)
		}
		out = append(out, poolMember{
			IfName: t.IfName, Slot: *t.Profile.WGSlot,
			Priority: t.Profile.Priority, RTTms: rtt, Healthy: healthy,
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
		u.publishPoolNH(nil)
		return u.applyViaRoutes(ctx, pool, uplinks)
	}

	members := u.poolMembers(pool)
	nh := poolNexthops(members, u.poolStrategyNow(), u.poolCarrierNow())
	routes := []system.Route{{
		Table: system.WGTable, Dest: "default", Nexthops: nh,
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
	// Same again: a member whose link vanished must not stop the others being
	// routed, nor skip the sweep and the publish below.
	var errs []error
	for _, r := range routes {
		if err := u.Backend.RouteReplace(ctx, r); err != nil {
			errs = append(errs, fmt.Errorf("route %s into the pool: %w", r.Dest, err))
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
	if err := u.applyViaRoutes(ctx, pool, uplinks); err != nil {
		errs = append(errs, err)
	}
	u.publishPoolNH(nh)
	return errors.Join(errs...)
}

// applyViaRoutes makes tables 207-210 each hold one WAN's slice of the pool:
// the tunnels whose transport rides it, best tier only, same ejection as 203.
// An empty slice empties its table and the via blackhole rule takes over —
// never the whole pool, never the raw uplink.
func (u *networkUsecase) applyViaRoutes(ctx context.Context, pool vpnPool, uplinks []Uplink) error {
	byWAN := map[string][]poolMember{}
	if pool.Active() {
		members := u.poolMembers(pool)
		u.healthMu.Lock()
		for _, m := range members {
			if wan := u.lastTransport[m.IfName]; wan != "" {
				byWAN[wan] = append(byWAN[wan], m)
			}
		}
		u.healthMu.Unlock()
	}

	secByIdx := map[uint32]Uplink{}
	for _, up := range secondariesOf(uplinks) {
		secByIdx[up.UplinkIndex] = up
	}

	// A via-marked flow obeys the pool's strategy, not a second policy.
	strategy, carrier := u.poolStrategyNow(), u.poolCarrierNow()

	var errs []error
	for _, slot := range domain.SecondarySlots() {
		idx := uplinkIndexFor(slot)
		table := vpnViaTableFor(idx)
		var nh []system.Nexthop
		if up, ok := secByIdx[idx]; ok {
			nh = poolNexthops(byWAN[up.IfName], strategy, carrier)
		}
		if len(nh) > 0 {
			if err := u.Backend.RouteReplace(ctx, system.Route{
				Table: table, Dest: "default", Nexthops: nh,
			}); err != nil {
				errs = append(errs, fmt.Errorf("route the %s slice: %w", slot, err))
			}
			continue
		}
		have, err := u.Backend.RouteList(ctx, table)
		if err != nil {
			errs = append(errs, fmt.Errorf("read the %s slice: %w", slot, err))
			continue
		}
		for _, r := range have {
			if err := u.Backend.RouteDel(ctx, r); err != nil {
				errs = append(errs, fmt.Errorf("clear the %s slice: %w", slot, err))
			}
		}
	}
	return errors.Join(errs...)
}

// ApplyKillSwitchState installs the chains that stop anything leaving the
// secondary uplink in the clear.
//
// Rendered whenever a secondary uplink exists, with or without a pool, and
// deliberately independent of the input firewall: that one is a setting the
// operator chooses, this one is not.
func ApplyKillSwitchState(ctx context.Context, m *nft.Manager, uplinks []Uplink,
	gateways map[string]string, probeIPs []string) error {
	if m == nil {
		return nil
	}
	legs := killSwitchLegs(uplinks, gateways)
	return m.Update(ctx, func(rs *nft.Ruleset) {
		if len(legs) == 0 {
			rs.KillSwitch = nil
			return
		}
		rs.KillSwitch = &nft.KillSwitch{
			Legs:         legs,
			DishSubnet:   StarlinkDishSubnet,
			MarkMask:     netmark.MaskPin,
			BootstrapIPs: dohboot.BootstrapIPs(),
			ProbeMark:    netmark.PinMark(netmark.PinProbe),
			ProbeIPs:     probeIPs,
			PortmapMark:  netmark.PinMark(netmark.PinPortmap),
		}
	})
}

// killSwitchLegs is one leg per secondary, in slot order.
func killSwitchLegs(uplinks []Uplink, gateways map[string]string) []nft.KillSwitchLeg {
	ordered := secondariesOf(uplinks)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].UplinkIndex < ordered[j].UplinkIndex })
	legs := make([]nft.KillSwitchLeg, 0, len(ordered))
	for _, up := range ordered {
		legs = append(legs, nft.KillSwitchLeg{
			IfName: up.IfName, GatewayIP: gateways[up.IfName], PinValue: transportMark(up),
		})
	}
	return legs
}

// secondaryGateways is each leg's one exemption that cannot be written until
// DHCP has answered.
func secondaryGateways(uplinks []Uplink, rows []domain.NetworkInterface) map[string]string {
	byKey := map[string]domain.NetworkInterface{}
	for i := range rows {
		byKey[rows[i].Key] = rows[i]
	}
	out := map[string]string{}
	for _, up := range secondariesOf(uplinks) {
		r, ok := byKey[up.Key]
		if !ok {
			continue
		}
		if r.StaticGateway != "" {
			out[up.IfName] = r.StaticGateway
		} else {
			out[up.IfName] = r.LearnedGateway
		}
	}
	return out
}

// ---------------------------------------------------------------- status

func (u *networkUsecase) VPNStatus(ctx context.Context) (*VPNPoolStatusView, error) {
	strategy := u.poolStrategyNow()
	out := &VPNPoolStatusView{
		Tunnels: []TunnelStatusView{}, Uplinks: []VPNUplinkView{}, KillSwitch: true,
		Strategy: strategy,
	}
	if strategy.SingleCarrier() {
		out.Carrier = u.poolCarrierNow()
	}

	uplinks, _ := u.uplinks(ctx)
	labels := u.uplinkLabels(ctx)
	healthy := u.healthyKeys(ctx)
	secondaries := secondariesOf(uplinks)
	sort.Slice(secondaries, func(i, j int) bool {
		return secondaries[i].UplinkIndex < secondaries[j].UplinkIndex
	})
	for _, up := range secondaries {
		out.Uplinks = append(out.Uplinks, VPNUplinkView{
			Slot: string(up.Slot), IfName: up.IfName, Label: labels[up.IfName],
			Key: up.Key, Up: healthy[up.Key],
		})
	}

	pool := u.vpnPoolNow(ctx)
	deal := assignTransport(pool, secondaries, u.healthySecondaries(uplinks))
	inPool := map[string]bool{}
	for _, n := range u.currentPoolNexthops() {
		inPool[n.OifName] = true
	}
	for _, t := range pool.Tunnels {
		tv := TunnelStatusView{
			ProfileID: t.Profile.ID, Name: t.Profile.Name, IfName: t.IfName,
			Position: t.Profile.Priority,
			MTU:      t.Config.MTU, KeepaliveSecs: t.Config.Peer.PersistentKeepalive,
			InPool: inPool[t.IfName],
		}
		if wan, ok := deal[t.IfName]; ok {
			tv.Via = &TunnelVia{
				IfName: wan.IfName, Label: labels[wan.IfName], Key: wan.Key,
				Pinned: t.Profile.TransportUplink != "",
			}
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

// uplinkLabels is the operator's name for each WAN, falling back to the kernel's.
func (u *networkUsecase) uplinkLabels(ctx context.Context) map[string]string {
	out := map[string]string{}
	if u.IfRepo == nil {
		return out
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	if err != nil {
		return out
	}
	for i := range rows {
		if rows[i].Label != "" {
			out[rows[i].IfName] = rows[i].Label
		} else {
			out[rows[i].IfName] = rows[i].IfName
		}
	}
	return out
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

	addr, err := u.doh(ctx).Resolve(ctx, host)
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
			if want[i].WGSlot != nil {
				keep[system.WGLinkNameFor(*want[i].WGSlot)] = true
			}
		}
		// Orphans first, same as applyVPNDevices: a link the restored rules no
		// longer name must not outlive them because a later Ensure failed.
		var errs []error
		if have, err := wg.List(ctx); err == nil {
			for _, name := range have {
				if !keep[name] {
					if err := wg.Delete(ctx, name); err != nil {
						errs = append(errs, err)
					}
				}
			}
		} else {
			errs = append(errs, err)
		}
		for i := range want {
			p := &want[i]
			if p.WGSlot == nil {
				continue
			}
			cfg, err := decodeWGConfig(p)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			// No resolution here: a revert must not depend on a resolver that
			// may only exist inside a tunnel being restored. The mark is slot
			// one's; the panel re-deals on its next health tick.
			apply, err := wgApplyConfig(cfg, netmark.PinMark(uplinkIndexFor(domain.SlotSecondary)))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if err := wg.Ensure(ctx, system.WGLinkNameFor(*p.WGSlot), apply); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}
