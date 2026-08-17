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

// The tunnel's transport rides the secondary uplink's pin, which already has a
// lookup and a blackhole of its own at pref 52/53. That is what keeps WireGuard
// handshakes off the domestic line when the dish is down.
var vpnTransportMark = netmark.PinMark(uplinkIndexFor(domain.SlotSecondary))

// StarlinkDishSubnet is the dish's own management address space, reachable
// through the kill switch because it is link-scoped and carries no payload.
const StarlinkDishSubnet = "192.168.100.0/24"

// reresolveInterval bounds how often a silent tunnel triggers a fresh lookup.
// Each one is a DoH request out the raw uplink, so it is not free.
const reresolveInterval = 60 * time.Second

// VPNProfileView is one stored profile with its payload decoded.
type VPNProfileView struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Active bool   `json:"active"`
	// Config carries the private key as stored. Anyone reading this already
	// holds an admin session, which is the whole panel.
	Config    domain.WireGuardConfig `json:"config"`
	PublicKey string                 `json:"public_key"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
	// Unreadable rows are still listed, so they can be deleted.
	Unreadable string `json:"unreadable,omitempty"`
}

// VPNStatusView is the live tunnel, or the reason there isn't one.
type VPNStatusView struct {
	ActiveProfileID  *uint  `json:"active_profile_id"`
	Name             string `json:"name,omitempty"`
	Connected        bool   `json:"connected"`
	HandshakeAgeSecs *int64 `json:"handshake_age_seconds"`
	RxBytes          int64  `json:"rx_bytes"`
	TxBytes          int64  `json:"tx_bytes"`
	Endpoint         string `json:"endpoint,omitempty"`
	MTU              int    `json:"mtu"`
	KeepaliveSecs    int    `json:"keepalive_seconds"`
	LastError        string `json:"last_error,omitempty"`
	// SecondaryUplinkUp separates "the dish is down" from "the tunnel is down",
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

// vpnPlane is the tunnel as the rest of the reconcile needs to see it.
type vpnPlane struct {
	Profile *domain.VPNProfile
	Config  *domain.WireGuardConfig
}

func (p vpnPlane) Active() bool { return p.Profile != nil && p.Config != nil }

// vpnPlaneNow reads the active profile. Every failure answers "no tunnel": the
// wrong answer sends foreign traffic out the raw uplink, so it fails towards
// the blackhole rather than towards the leak.
func (u *networkUsecase) vpnPlaneNow(ctx context.Context) vpnPlane {
	if u.VPNRepo == nil {
		return vpnPlane{}
	}
	p, err := u.VPNRepo.Active(ctx)
	if err != nil || p == nil {
		return vpnPlane{}
	}
	cfg, err := decodeWGConfig(p)
	if err != nil {
		return vpnPlane{}
	}
	return vpnPlane{Profile: p, Config: cfg}
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
				ID: r.ID, Name: r.Name, Type: r.Type, Active: r.Active,
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
		ID: p.ID, Name: p.Name, Type: p.Type, Active: p.Active,
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
	p := &domain.VPNProfile{Name: name, Type: domain.VPNTypeWireGuard, Config: string(blob)}
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
	// Editing the running tunnel is a routing change, so it goes through the
	// same activate pipeline rather than mutating under a live device.
	if stored.Active {
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
			Message: fmt.Sprintf("This VPN only carries %s. Everything else bound for the "+
				"secondary uplink will be dropped, because that uplink never carries "+
				"traffic in the clear.", strings.Join(cfg.Peer.AllowedIPs, ", ")),
		})
	}
	return vs
}

// ---------------------------------------------------------------- activation

func (u *networkUsecase) ActivateVPN(ctx context.Context, id uint) ([]domain.Verdict, *ApplyView, error) {
	if u.VPNRepo == nil {
		return nil, nil, errors.New("no VPN storage configured")
	}
	profile, err := u.VPNRepo.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := decodeWGConfig(profile)
	if err != nil {
		return nil, nil, err
	}
	if err := domain.ValidateWireGuardConfig(cfg); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}

	verdicts := vpnConfigVerdicts(cfg)

	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !hasSlot(uplinks, domain.SlotSecondary) {
		// Provisioning before the dish arrives is a real workflow, and the kill
		// switch already covers every state in between.
		verdicts = append(verdicts, domain.Verdict{
			Rule:  "V33",
			Level: domain.LevelWarn,
			Message: "No secondary uplink is assigned yet, so the tunnel has nothing to " +
				"run over. It will connect on its own once one appears.",
		})
	}

	// Resolve now, so the tunnel can come up later without a resolver — the
	// foreign one is about to live inside it.
	host, err := domain.ParseWGEndpoint(cfg.Peer.Endpoint)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrValidationFailed, err)
	}
	addr, err := u.doh().Resolve(ctx, host)
	if err != nil {
		verdicts = append(verdicts, domain.Verdict{
			Rule:    "V34",
			Level:   domain.LevelReject,
			Message: fmt.Sprintf("The endpoint %q could not be resolved: %v", host, err),
		})
		return verdicts, nil, nil
	}
	cfg.PinnedEndpointIP = addr.String()

	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, nil, err
	}
	profile.Config = string(blob)

	previous, err := u.VPNRepo.Active(ctx)
	if err != nil {
		return nil, nil, err
	}

	var plan system.Plan
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: fmt.Sprintf("send all secondary-uplink traffic through %q", profile.Name),
			Do: func(ctx context.Context) error {
				if err := u.VPNRepo.Update(ctx, profile); err != nil {
					return err
				}
				return u.VPNRepo.SetActive(ctx, profile.ID)
			},
			Undo: func(ctx context.Context) error {
				if previous == nil {
					return u.VPNRepo.ClearActive(ctx)
				}
				return u.VPNRepo.SetActive(ctx, previous.ID)
			},
		},
		system.Op{
			Desc: "bring the tunnel interface up and configure its peer",
			Do:   func(ctx context.Context) error { return u.applyVPNDevice(ctx) },
			Undo: func(ctx context.Context) error { return u.applyVPNDevice(ctx) },
		},
		system.Op{
			// The secondary uplink's own resolver line and the tunnel's table
			// name live in rendered files, so they have to be rewritten here or
			// resolved keeps querying out the raw uplink.
			Desc: "render the uplink units and the routing table names",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "route the foreign group into the tunnel and arm the kill switch",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	)

	view, err := u.runVPNPlan(ctx, plan)
	if err != nil {
		return verdicts, nil, err
	}
	return verdicts, view, nil
}

func (u *networkUsecase) DeactivateVPN(ctx context.Context) ([]domain.Verdict, *ApplyView, error) {
	if u.VPNRepo == nil {
		return nil, nil, errors.New("no VPN storage configured")
	}
	previous, err := u.VPNRepo.Active(ctx)
	if err != nil {
		return nil, nil, err
	}
	if previous == nil {
		return []domain.Verdict{}, nil, nil
	}

	var plan system.Plan
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: "stop sending secondary-uplink traffic through the VPN",
			Do:   func(ctx context.Context) error { return u.VPNRepo.ClearActive(ctx) },
			Undo: func(ctx context.Context) error { return u.VPNRepo.SetActive(ctx, previous.ID) },
		},
		system.Op{
			Desc: "take the tunnel interface down",
			Do:   func(ctx context.Context) error { return u.applyVPNDevice(ctx) },
			Undo: func(ctx context.Context) error { return u.applyVPNDevice(ctx) },
		},
		system.Op{
			// The secondary uplink's own resolver line and the tunnel's table
			// name live in rendered files, so they have to be rewritten here or
			// resolved keeps querying out the raw uplink.
			Desc: "render the uplink units and the routing table names",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "leave the foreign group with nowhere to go, so nothing leaves in the clear",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
	)

	view, err := u.runVPNPlan(ctx, plan)
	if err != nil {
		return nil, nil, err
	}
	return []domain.Verdict{}, view, nil
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

// applyVPNDevice makes the tunnel interface match the stored state: present and
// configured when a profile is active, gone when none is. Idempotent, so boot,
// apply and undo all use the same path.
func (u *networkUsecase) applyVPNDevice(ctx context.Context) error {
	plane := u.vpnPlaneNow(ctx)
	if !plane.Active() {
		return u.wg().Delete(ctx)
	}
	// A profile restored from an older snapshot can carry a hostname with no
	// pinned address. Resolve it here rather than fail: this runs at boot, and
	// a tunnel that will not come up takes all foreign traffic with it.
	if err := u.ensurePinnedEndpoint(ctx, plane); err != nil {
		return err
	}
	apply, err := wgApplyConfig(plane.Config)
	if err != nil {
		return err
	}
	return u.wg().Ensure(ctx, apply)
}

// ensurePinnedEndpoint fills in a missing pin. Does nothing in the normal case,
// where activation pinned the address already.
func (u *networkUsecase) ensurePinnedEndpoint(ctx context.Context, plane vpnPlane) error {
	if plane.Config.PinnedEndpointIP != "" {
		return nil
	}
	host, err := domain.ParseWGEndpoint(plane.Config.Peer.Endpoint)
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
	plane.Config.PinnedEndpointIP = addr.String()
	if blob, err := json.Marshal(plane.Config); err == nil {
		plane.Profile.Config = string(blob)
		_ = u.VPNRepo.Update(ctx, plane.Profile)
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

// vpnForeignDNS is where the LAN's foreign lookups go. Inside the tunnel while
// one is up, and nowhere at all when none is: a resolver reachable in the clear
// would be both a leak and something the kill switch drops anyway.
func vpnForeignDNS(plane vpnPlane) ForeignDNS {
	if !plane.Active() {
		return ForeignDNS{}
	}
	server := DefaultForeignDNS
	// The provider put their own resolver in the config because it is the one
	// guaranteed reachable and unfiltered inside their tunnel.
	if plane.Config.DNS != "" {
		server = plane.Config.DNS
	}
	return ForeignDNS{IfName: system.WGLinkName, Server: server}
}

// ---------------------------------------------------------------- kernel state

// vpnRoutes is what table 203 holds while a tunnel is up.
func vpnRoutes(uplinks []Uplink) []system.Route {
	routes := []system.Route{
		{Table: system.WGTable, Dest: "default", OifName: system.WGLinkName},
	}
	// The dish's own management address stays reachable, which is the one thing
	// on the raw uplink worth keeping.
	for _, u := range uplinks {
		if u.Slot == domain.SlotSecondary {
			routes = append(routes, system.Route{
				Table: system.WGTable, Dest: StarlinkDishSubnet,
				OifName: u.IfName, Scope: "link",
			})
		}
	}
	return routes
}

// applyVPNRoutes makes table 203 match the tunnel's state. Everything in it is
// removed when no profile is active, so a stale default cannot outlive the
// device it points at.
func (u *networkUsecase) applyVPNRoutes(ctx context.Context, plane vpnPlane, uplinks []Uplink) error {
	if !plane.Active() {
		have, err := u.Backend.RouteList(ctx, system.WGTable)
		if err != nil {
			return nil // an absent table is the state we want anyway
		}
		for _, r := range have {
			if err := u.Backend.RouteDel(ctx, r); err != nil {
				return fmt.Errorf("clear the tunnel's routing table: %w", err)
			}
		}
		return nil
	}
	for _, r := range vpnRoutes(uplinks) {
		if err := u.Backend.RouteReplace(ctx, r); err != nil {
			return fmt.Errorf("route %s into the tunnel: %w", r.Dest, err)
		}
	}
	return nil
}

// ApplyKillSwitchState installs the chains that stop anything leaving the
// secondary uplink in the clear.
//
// Rendered whenever a secondary uplink exists, with or without a tunnel, and
// deliberately independent of the input firewall: that one is a setting the
// operator chooses, this one is not.
func ApplyKillSwitchState(ctx context.Context, m *nft.Manager, uplinks []Uplink, gateway string) error {
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

func (u *networkUsecase) VPNStatus(ctx context.Context) (*VPNStatusView, error) {
	out := &VPNStatusView{KillSwitch: true}

	uplinks, err := u.uplinks(ctx)
	if err == nil {
		out.SecondaryUplinkUp = uplinkHealthy(uplinks, u.healthyKeys(ctx), domain.SlotSecondary)
	}

	plane := u.vpnPlaneNow(ctx)
	if !plane.Active() {
		return out, nil
	}
	id := plane.Profile.ID
	out.ActiveProfileID, out.Name = &id, plane.Profile.Name
	out.MTU, out.KeepaliveSecs = plane.Config.MTU, plane.Config.Peer.PersistentKeepalive
	if out.MTU == 0 {
		out.MTU = domain.DefaultWGMTU
	}
	if out.KeepaliveSecs == 0 {
		out.KeepaliveSecs = domain.DefaultWGKeepalive
	}
	out.Endpoint = plane.Config.Peer.Endpoint
	if plane.Config.PinnedEndpointIP != "" {
		out.Endpoint = plane.Config.PinnedEndpointIP
	}

	st, err := u.wg().Status(ctx)
	if err != nil {
		if !errors.Is(err, system.ErrNoWGDevice) {
			out.LastError = err.Error()
		} else {
			out.LastError = "The tunnel interface is not present."
		}
		return out, nil
	}
	out.RxBytes, out.TxBytes = st.RxBytes, st.TxBytes
	if st.Endpoint != "" {
		out.Endpoint = st.Endpoint
	}
	if !st.LastHandshake.IsZero() {
		age := int64(time.Since(st.LastHandshake).Seconds())
		out.HandshakeAgeSecs = &age
	}
	out.Connected = st.Connected()
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

// checkVPNHealth runs on the health tick. It reports state changes and, when a
// hostname endpoint has gone quiet, looks it up again — providers move them.
func (u *networkUsecase) checkVPNHealth(ctx context.Context) {
	plane := u.vpnPlaneNow(ctx)
	if !plane.Active() {
		if u.vpnWasUp {
			u.vpnWasUp = false
			u.emit(events.EventVPNDown, map[string]any{"reason": "no active profile"})
		}
		return
	}

	st, err := u.wg().Status(ctx)
	up := err == nil && st.Connected()
	if up != u.vpnWasUp {
		u.vpnWasUp = up
		if up {
			u.emit(events.EventVPNUp, map[string]any{"profile": plane.Profile.Name})
		} else {
			u.emit(events.EventVPNDown, map[string]any{"profile": plane.Profile.Name})
		}
	}
	if up {
		return
	}
	u.reresolveEndpoint(ctx, plane)
}

// reresolveEndpoint looks the endpoint up again after a silence. Only for
// hostname endpoints: an address cannot have moved.
func (u *networkUsecase) reresolveEndpoint(ctx context.Context, plane vpnPlane) {
	host, err := domain.ParseWGEndpoint(plane.Config.Peer.Endpoint)
	if err != nil {
		return
	}
	if _, isAddr := netip.ParseAddr(host); isAddr == nil {
		return
	}
	if time.Since(u.vpnLastResolve) < reresolveInterval {
		return
	}
	u.vpnLastResolve = time.Now()

	addr, err := u.doh().Resolve(ctx, host)
	if err != nil || addr.String() == plane.Config.PinnedEndpointIP {
		return
	}

	port := plane.Config.Peer.Endpoint[strings.LastIndex(plane.Config.Peer.Endpoint, ":")+1:]
	ep, err := netip.ParseAddrPort(addr.String() + ":" + port)
	if err != nil {
		return
	}
	if err := u.wg().UpdateEndpoint(ctx, ep); err != nil {
		return
	}
	plane.Config.PinnedEndpointIP = addr.String()
	if blob, err := json.Marshal(plane.Config); err == nil {
		plane.Profile.Config = string(blob)
		_ = u.VPNRepo.Update(ctx, plane.Profile)
	}
}

// restoreVPN is the rollback hook. A nil profile means none was active, which
// has to take the device down: leaving it up would keep routes pointing into a
// tunnel the restored rules no longer know about.
func (u *networkUsecase) restoreVPN(ctx context.Context, p *domain.VPNProfile) error {
	if u.VPNRepo == nil {
		return nil
	}
	return NewVPNRestorer(u.VPNRepo, u.wg())(ctx, p)
}

// NewVPNRestorer builds the same hook for a caller that cannot construct the
// whole usecase — the dead-man runs in its own process, on purpose, because a
// bad network apply is most likely to break the panel itself.
func NewVPNRestorer(repo repository.VPNRepository, wg system.WGDevice) func(context.Context, *domain.VPNProfile) error {
	return func(ctx context.Context, p *domain.VPNProfile) error {
		if p == nil {
			if err := repo.ClearActive(ctx); err != nil {
				return err
			}
			return wg.Delete(ctx)
		}
		if err := repo.Update(ctx, p); err != nil {
			return err
		}
		if err := repo.SetActive(ctx, p.ID); err != nil {
			return err
		}
		cfg, err := decodeWGConfig(p)
		if err != nil {
			return err
		}
		// No resolution here: a revert must not depend on a resolver that may
		// only exist inside the tunnel being restored.
		apply, err := wgApplyConfig(cfg)
		if err != nil {
			return err
		}
		return wg.Ensure(ctx, apply)
	}
}
