package usecase

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/dohboot"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"gorm.io/gorm"
)

const defaultMgmtCIDR = "192.168.99.1/24"

// Written once at role assignment, then frozen — renderAll never rewrites it.
const mgmtFileName = "40-nasnet-mgmt.network"

// InterfaceView is one NIC as the UI sees
type InterfaceView struct {
	agent.NetInterface
	ID       uint             `json:"id"`
	Role     string           `json:"role"`
	Slot     string           `json:"slot"`
	Label    string           `json:"label"`
	Present  bool             `json:"present"`
	Healthy  bool             `json:"healthy"`
	Verdicts []domain.Verdict `json:"verdicts,omitempty"`
}

type UplinkView struct {
	IfName     string   `json:"if_name"`
	Slot       string   `json:"slot"`
	Label      string   `json:"label"`
	Table      int      `json:"table"`
	Addrs      []string `json:"addrs"`
	Gateway    string   `json:"gateway"`
	Healthy    bool     `json:"healthy"`
	Verdict    string   `json:"verdict"`
	ForceState string   `json:"force_state"`
}

type StateView struct {
	RouterMode          bool         `json:"router_mode"`
	TakeoverDone        bool         `json:"takeover_done"`
	Warnings            []string     `json:"warnings"`
	Uplinks             []UplinkView `json:"uplinks"`
	PendingPlanID       uint         `json:"pending_plan_id"`
	ConfirmDeadlineUnix int64        `json:"confirm_deadline_unix"`
	// Kept out of the uplink's Healthy: that loop withdraws routes, and a dead
	// tunnel is the wrong reason to.
	VPN VPNStateView `json:"vpn"`
}

// VPNStateView is enough for a chip on the uplink card; the detail is a tab away.
type VPNStateView struct {
	Active    bool `json:"active"`
	Connected bool `json:"connected"`
}

type PlanView struct {
	Ops      []string         `json:"ops"`
	Verdicts []domain.Verdict `json:"verdicts"`
}

type ApplyView struct {
	PlanID              uint     `json:"plan_id"`
	ConfirmDeadlineUnix int64    `json:"confirm_deadline_unix"`
	Ops                 []string `json:"ops"`
}

type NetworkUsecase interface {
	Enumerate(ctx context.Context) ([]InterfaceView, error)
	State(ctx context.Context) (*StateView, error)
	Groups(ctx context.Context) ([]domain.WANGroup, error)
	Plan(ctx context.Context, req domain.ChangeRequest) (*PlanView, error)
	Apply(ctx context.Context, req domain.ChangeRequest) (*ApplyView, error)
	Confirm(ctx context.Context, planID uint) error
	Rollback(ctx context.Context) error
	Reconcile(ctx context.Context) error
	StartHealthLoop(ctx context.Context, interval time.Duration)
	// SetHealthConfig swaps the probe config; live-reloaded from settings.
	SetHealthConfig(cfg HealthConfig)
	// HealthState reports the probe ladder; assembly only, never dials.
	HealthState(ctx context.Context) (*HealthView, error)
	SetUplinkForce(ctx context.Context, key, state string) error
	// StartRangesRefreshLoop keeps the domestic prefix list current. Zero uses
	// the default weekly cadence.
	StartRangesRefreshLoop(ctx context.Context, interval time.Duration)
	RefreshDomesticRanges(ctx context.Context) error
	SetLabel(ctx context.Context, key, label string) error
	IngressUplinkIfName() string

	GetLAN(ctx context.Context) (*LANView, error)
	UpdateLAN(ctx context.Context, cfg domain.LANConfig) ([]domain.Verdict, *ApplyView, error)

	// ListDevices reports what is on the LAN bridge. Derived per request; the
	// only stored part is the operator's name for a device.
	ListDevices(ctx context.Context) (*LANDeviceList, error)
	SetDeviceLabel(ctx context.Context, mac, label string) error

	// Only enable and disable ride the apply pipeline; role changes only
	// redistribute flows, and the rest is storage.
	ListVPNProfiles(ctx context.Context) ([]VPNProfileView, error)
	CreateVPNProfile(ctx context.Context, req CreateVPNProfileRequest) (*VPNProfileView, error)
	UpdateVPNProfile(ctx context.Context, id uint, req CreateVPNProfileRequest) (*VPNProfileView, error)
	DeleteVPNProfile(ctx context.Context, id uint) error
	ParseVPNInput(ctx context.Context, raw string) (*domain.WireGuardConfig, []domain.Verdict, error)
	GenerateVPNKeypair() (priv, pub string, err error)
	EnableVPNProfile(ctx context.Context, id uint) ([]domain.Verdict, *ApplyView, error)
	DisableVPNProfile(ctx context.Context, id uint) ([]domain.Verdict, *ApplyView, error)
	SetVPNProfileRole(ctx context.Context, id uint, priority, weight int) error
	VPNStatus(ctx context.Context) (*VPNPoolStatusView, error)

	// The flow page. All read-only: nothing here touches a packet.
	FlowGraph(ctx context.Context) (*FlowView, error)
	TraceFlow(ctx context.Context, req TraceRequest) (*TraceView, error)
	FlowConns(ctx context.Context) (*FlowConnsView, error)
	RecentNetworkEvents(ctx context.Context) ([]events.Event, error)

	ListPortForwards(ctx context.Context) ([]domain.PortForward, error)
	CreatePortForward(ctx context.Context, pf domain.PortForward, confirmed bool) ([]domain.Verdict, error)
	UpdatePortForward(ctx context.Context, pf domain.PortForward, confirmed bool) ([]domain.Verdict, error)
	DeletePortForward(ctx context.Context, id uint) error

	// OnInboundsChanged re-derives filter_in. Called from the inbound
	// create/update/delete paths so an accept cannot drift from its inbound.
	OnInboundsChanged(ctx context.Context) error
}

type Deps struct {
	IfRepo    repository.InterfaceRepository
	GroupRepo repository.GroupRepository
	ApplyRepo repository.ApplyRepository
	LANRepo   repository.LANRepository
	PFRepo    repository.PortForwardRepository
	// DeviceLabels is optional: with no storage the device list still renders,
	// it just carries no operator-assigned names.
	DeviceLabels repository.DeviceLabelRepository
	// VPNRepo holds the profiles. Nil means nothing can be active, so the
	// foreign group blackholes.
	VPNRepo repository.VPNRepository
	// WG owns the tunnel interface. Nil uses the live kernel; injected in tests.
	WG system.WGDevice
	// DoH resolves the endpoint hostname before any tunnel exists. Nil uses the
	// bootstrap resolvers.
	DoH dohboot.Resolver
	// Devices reads the bridge. Nil uses the live system; injected in tests.
	Devices system.DeviceSource
	// Nftr reads live kernel nftables state for the flow page. Nil execs nft.
	Nftr system.NftReader
	// Flow reads conntrack and interface counters. Nil uses the live kernel.
	Flow system.FlowSource
	// Prober dials the internet-layer targets. Nil uses the live kernel.
	Prober TargetProber
	// Events is the flow page's timeline history. Nil means no history.
	Events     *events.Recorder
	Backend    system.Backend
	Nft        *nft.Manager
	Agent      agent.NodeClient
	Paths      system.Paths
	RouterMode bool
	EventBus   *events.EventBus
	// PanelPort is the port filter_in must keep open, or the operator is
	// locked out of the box they just firewalled.
	PanelPort int
	// Inbounds is optional: with no source, filter_in stays off rather than
	// dropping every VPN it does not know about.
	Inbounds InboundSource

	// RangesURL is where the prefix list refreshes from; empty means the default.
	RangesURL string
	// RangesClient carries the fetch. Supplied so it can be routed like every
	// other outbound the panel makes.
	RangesClient *http.Client
}

type networkUsecase struct {
	Deps
	applier *system.Applier
	health  *HealthMonitor
	dnsmasq *system.DNSMasq

	// The geoip sets are thousands of prefixes; compiling them on every
	// reconcile tick would be waste.
	setsMu sync.Mutex
	// Serialises the two-phase applies; the dead-man marker holds one at a time.
	planMu    sync.Mutex
	lanSets   []nft.Set
	nftSetOK  bool
	setsBuilt bool

	// lastRolledBack is the revert this process has already reacted to.
	lastRolledBack uint

	// tunnelWasUp keeps the per-member events edge-triggered; tunnelLastResolve
	// stops a silent tunnel re-resolving its endpoint every tick.
	tunnelWasUp       map[string]bool
	tunnelLastResolve map[string]time.Time
	// lastPoolKey detects nexthop-set changes; poolNH is the set as applied.
	lastPoolKey  string
	poolKeyKnown bool
	poolNH       []system.Nexthop

	// resolverStatus is a seam: off systemd the real probe always answers
	// "running", which leaves the flow page's check untestable.
	resolverStatus func(context.Context) system.DNSMasqStatus

	// Everything the probe ladder knows, keyed by interface name.
	healthMu       sync.Mutex
	healthCfg      HealthConfig
	inetStates     map[string]*internetState
	bootTicks      map[string]int
	rings          map[string]*healthRing
	ladders        map[string]uplinkLadder
	degradedNow    map[string]bool
	lastEffective  map[string]bool
	failoverActive bool
}

func (u *networkUsecase) dnsmasqStatus(ctx context.Context) system.DNSMasqStatus {
	if u.resolverStatus != nil {
		return u.resolverStatus(ctx)
	}
	if u.dnsmasq == nil {
		return system.DNSMasqStatus{}
	}
	return u.dnsmasq.Status(ctx)
}

func (u *networkUsecase) nftReader() system.NftReader {
	if u.Nftr != nil {
		return u.Nftr
	}
	return system.NewLiveNft()
}

func (u *networkUsecase) flowSource() system.FlowSource {
	if u.Flow != nil {
		return u.Flow
	}
	return system.NewFlowSource()
}

// vpnRouteState says what the foreign group has to leave by.
func (u *networkUsecase) vpnRouteState(ctx context.Context) VPNRouteState {
	return VPNRouteState{IfNames: u.vpnPoolNow(ctx).IfNames()}
}

func NewNetworkUsecase(d Deps) NetworkUsecase {
	u := &networkUsecase{
		Deps:              d,
		health:            NewHealthMonitor(d.Backend, NewKernelProbe(), DefaultDamping()),
		dnsmasq:           system.NewDNSMasq(),
		healthCfg:         DefaultHealthConfig(),
		inetStates:        map[string]*internetState{},
		bootTicks:         map[string]int{},
		rings:             map[string]*healthRing{},
		ladders:           map[string]uplinkLadder{},
		degradedNow:       map[string]bool{},
		lastEffective:     map[string]bool{},
		tunnelWasUp:       map[string]bool{},
		tunnelLastResolve: map[string]time.Time{},
	}
	snap := &system.Snapshotter{Backend: d.Backend, Nft: d.Nft, Paths: d.Paths}
	if d.LANRepo != nil {
		snap.CaptureLAN = func(ctx context.Context) (*domain.LANConfig, error) {
			return d.LANRepo.Get(ctx)
		}
		snap.RestoreLAN = func(ctx context.Context, cfg *domain.LANConfig) error {
			return d.LANRepo.Save(ctx, cfg)
		}
	}
	if d.VPNRepo != nil {
		snap.CapturePool = func(ctx context.Context) ([]domain.VPNProfile, error) {
			return d.VPNRepo.Enabled(ctx)
		}
		snap.RestorePool = u.restorePool
	}
	u.applier = &system.Applier{
		Snap:   snap,
		Repo:   d.ApplyRepo,
		Paths:  d.Paths,
		Reload: system.ReloadNetworkd,
		OnRollback: func(planID uint) {
			// Same reason as the dead-man's own path: a reverted apply must not
			// leave the one lockout-capable setting still armed.
			if u.LANRepo != nil {
				if err := u.LANRepo.DisarmInputFirewall(context.Background()); err != nil {
					_ = err
				}
			}
			u.emit(events.EventWANApplyRolledBack, map[string]any{"plan_id": planID})
		},
	}
	return u
}

func (u *networkUsecase) emit(t events.EventType, payload map[string]any) {
	if u.EventBus == nil {
		return
	}
	u.EventBus.Publish(events.Event{Type: t, Payload: payload})
}

// Reconcile makes the kernel and config dir match the DB. Runs at boot before
// the agent starts, and after every change. (doesn't regenerates xray's config)
func (u *networkUsecase) Reconcile(ctx context.Context) error {
	if !u.RouterMode {
		return nil
	}

	if err := u.GroupRepo.EnsureDefaults(ctx); err != nil {
		return fmt.Errorf("ensure groups: %w", err)
	}
	if err := u.refreshInterfaces(ctx); err != nil {
		return fmt.Errorf("refresh interfaces: %w", err)
	}

	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	if len(uplinks) == 0 {
		// Nothing assigned: netplan still owns the network. Touching kernel
		// state here would be a change outside the two-phase apply.
		return nil
	}

	groups, err := u.GroupRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("list groups: %w", err)
	}
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return fmt.Errorf("list interfaces: %w", err)
	}
	// The kill switch goes on before anything that can fail. One member with an
	// endpoint that will not resolve used to abort the whole reconcile ahead of
	// this line, and the secondary uplink spent that time carrying in the clear.
	if err := ApplyKillSwitchState(ctx, u.Nft, uplinks, secondaryGateway(uplinks, rows),
		u.healthConfigSnapshot().probeExemptIPs()); err != nil {
		return err
	}
	// Devices next: the rules below look up a table whose routes name links.
	if err := u.applyVPNDevices(ctx); err != nil {
		return fmt.Errorf("apply the tunnels: %w", err)
	}
	pool := u.vpnPoolNow(ctx)
	vpn := VPNRouteState{IfNames: pool.IfNames()}

	if err := ReconcileRules(ctx, u.Backend, AllRules(groups, uplinks, vpn)); err != nil {
		return err
	}
	if err := ApplyNftState(ctx, u.Nft, uplinks); err != nil {
		return err
	}

	lan := u.lanConfig(ctx)
	if err := u.applyLAN(ctx, lan, uplinks, pool); err != nil {
		return err
	}
	bridge, on := "", lan != nil && lan.Enabled
	if on {
		bridge = lan.BridgeName
	}
	if err := ApplySysctls(ctx, u.Backend, uplinks, on, bridge); err != nil {
		return err
	}
	// Last: the LAN can restart networkd, which flushes the pool's routes out
	// to the dish along with everything else on the links it owns.
	return u.applyVPNRoutes(ctx, pool, uplinks)
}

// lanConfig reads the stored LAN row. A read failure is not fatal: the rest of
// the reconcile still has to run, and a missing LAN just means no LAN.
func (u *networkUsecase) lanConfig(ctx context.Context) *domain.LANConfig {
	if u.LANRepo == nil {
		return nil
	}
	cfg, err := u.LANRepo.Get(ctx)
	if err != nil {
		return nil
	}
	return cfg
}

// domesticSets compiles the geoip prefixes once and remembers whether this
// dnsmasq build supports --nftset. Feature-detected, never version-sniffed.
func (u *networkUsecase) domesticSets(ctx context.Context) ([]nft.Set, bool, error) {
	u.setsMu.Lock()
	defer u.setsMu.Unlock()
	if u.setsBuilt {
		return u.lanSets, u.nftSetOK, nil
	}
	u.nftSetOK = system.NftSetSupported(ctx, "")
	// A successful fetch wins over the embedded list; the embedded one is the
	// floor that makes a censored or unreachable upstream a non-event.
	var fetched []string
	if c, err := geoip.LoadCachedRanges(u.rangesCachePath()); err == nil && c != nil {
		fetched = c.V4
	}
	sets, err := BuildDomesticSetsFrom(u.nftSetOK, fetched)
	if err != nil {
		return nil, false, err
	}
	u.lanSets, u.setsBuilt = sets, true
	return sets, u.nftSetOK, nil
}

// applyLAN turns the LAN plane on or off: nft classification, NAT, the forward
// filter, and the dnsmasq that serves it.
func (u *networkUsecase) applyLAN(ctx context.Context, lan *domain.LANConfig,
	uplinks []Uplink, pool vpnPool) error {

	vpn := VPNRouteState{IfNames: pool.IfNames()}
	if lan == nil || !lan.Enabled {
		if err := ApplyLANNftState(ctx, u.Nft, nil, uplinks, nil, vpn); err != nil {
			return err
		}
		return u.dnsmasq.Stop(ctx)
	}

	sets, nftSetOK, err := u.domesticSets(ctx)
	if err != nil {
		return err
	}
	if err := ApplyLANNftState(ctx, u.Nft, lan, uplinks, sets, vpn); err != nil {
		return err
	}

	bridge := lan.BridgeName
	if bridge == "" {
		bridge = system.LANBridgeName
	}
	// The .network files are on disk but networkd has not been told yet, and
	// dnsmasq cannot bind an address that does not exist.
	if err := system.ReloadNetworkd(ctx); err != nil {
		return err
	}
	if err := waitForBridgeAddr(ctx, u.Backend, bridge, bridgeSettleTimeout); err != nil {
		// A reload re-reads .network files but never creates a .netdev, so the
		// first time the bridge is rendered only a restart brings it into being.
		if err := system.RestartNetworkd(ctx); err != nil {
			return err
		}
		if err := waitForBridgeAddr(ctx, u.Backend, bridge, bridgeWaitTimeout); err != nil {
			return err
		}
	}

	cfg := LANDNSConfig(*lan, uplinks, DefaultDomesticDNS, poolForeignDNS(pool),
		DomesticSuffix, nftSetOK)
	if err := u.dnsmasq.Write(cfg); err != nil {
		return err
	}
	return u.dnsmasq.Restart(ctx)
}

const (
	// bridgeSettleTimeout is the short look before falling back to a restart —
	// on a reconcile where the bridge already exists, a reload is enough.
	bridgeSettleTimeout = 2 * time.Second
	// bridgeWaitTimeout is generous: networkd creates the device, then addresses
	// it, and a slow box mid-apply is exactly when this is tightest.
	bridgeWaitTimeout = 20 * time.Second
)

// waitForBridgeAddr blocks until the bridge has an address. Failing loudly beats
// starting dnsmasq against nothing, which leaves the LAN with no DHCP at all.
func waitForBridgeAddr(ctx context.Context, be system.Backend, bridge string,
	timeout time.Duration) error {

	deadline := time.Now().Add(timeout)
	for {
		if addrs, err := be.Addrs(ctx); err == nil {
			for _, a := range addrs {
				if a.IfName == bridge {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s never got an address, so the LAN would have no "+
				"resolver and no DHCP; check `networkctl status %s`", bridge, bridge)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// refreshInterfaces upserts what the agent enumerated and marks the rest absent.
func (u *networkUsecase) refreshInterfaces(ctx context.Context) error {
	if u.Agent == nil {
		return nil
	}
	found, err := u.Agent.ListInterfaces(ctx)
	if err != nil {
		return err
	}

	before, _ := u.IfRepo.List(ctx)
	known := map[string]bool{}
	for _, b := range before {
		known[b.Key] = true
	}

	keys := make([]string, 0, len(found))
	for _, in := range found {
		// Our own bridge is not a port. It is classified virt_bridge like any
		// other, so it would otherwise be offered as one.
		if !in.Assignable || in.IfName == domain.ManagedBridgeName {
			continue
		}
		keys = append(keys, in.Key)
		row := &domain.NetworkInterface{
			Key: in.Key, KeyKind: in.KeyKind, IfName: in.IfName,
			PermMAC: in.PermMAC, IDPath: in.IDPath, Source: in.Source,
			SourceConfidence: in.Confidence, PhyName: in.Phy,
			USBSpeedMbit: in.USBSpeedMbit, Present: true,
		}
		if err := u.IfRepo.Upsert(ctx, row); err != nil {
			return err
		}
		if !known[in.Key] {
			u.emit(events.EventInterfaceAdded, map[string]any{"key": in.Key, "if_name": in.IfName})
		}
	}
	return u.IfRepo.MarkAbsent(ctx, keys)
}

// uplinks builds the routing identities from the WAN rows.
func (u *networkUsecase) uplinks(ctx context.Context) ([]Uplink, error) {
	if u.IfRepo == nil {
		return nil, nil
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	if err != nil {
		return nil, fmt.Errorf("list uplinks: %w", err)
	}
	out := make([]Uplink, 0, len(rows))
	for _, r := range rows {
		if !r.Present || r.Slot == domain.SlotNone {
			continue
		}
		out = append(out, Uplink{
			IfName: r.IfName, Key: r.Key, Table: tableFor(r.Slot),
			UplinkIndex: uplinkIndexFor(r.Slot), Slot: r.Slot,
			GroupIndex: groupIndexFor(r.Slot),
		})
	}
	return out, nil
}

func (u *networkUsecase) Enumerate(ctx context.Context) ([]InterfaceView, error) {
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	live := map[string]agent.NetInterface{}
	if u.Agent != nil {
		if found, err := u.Agent.ListInterfaces(ctx); err == nil {
			for _, in := range found {
				live[in.Key] = in
			}
		}
	}

	out := make([]InterfaceView, 0, len(rows))
	for _, r := range rows {
		// Rows are never deleted, so a bridge enumerated by an older build is
		// still here. It is not a port and must not be offered as one.
		if r.IfName == domain.ManagedBridgeName {
			continue
		}
		v := InterfaceView{
			ID: r.ID, Role: string(r.Role), Slot: string(r.Slot), Label: r.Label,
			Present: r.Present, Healthy: r.Healthy,
		}
		if in, ok := live[r.Key]; ok {
			v.NetInterface = in
		} else {
			v.NetInterface = agent.NetInterface{
				IfName: r.IfName, Key: r.Key, KeyKind: r.KeyKind,
				PermMAC: r.PermMAC, IDPath: r.IDPath, Source: r.Source,
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (u *networkUsecase) State(ctx context.Context) (*StateView, error) {
	st := &StateView{
		RouterMode:   u.RouterMode,
		TakeoverDone: system.TakeoverDone(u.Paths),
		Warnings:     []string{},
		Uplinks:      []UplinkView{},
	}
	if !st.TakeoverDone {
		st.Warnings = append(st.Warnings,
			"network not managed by nasnet yet — assign roles to finish setup")
	}

	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return nil, err
	}
	rows, _ := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	byName := map[string]domain.NetworkInterface{}
	for _, r := range rows {
		byName[r.IfName] = r
	}
	addrs, _ := u.Backend.Addrs(ctx)

	for _, up := range uplinks {
		r := byName[up.IfName]
		u.healthMu.Lock()
		verdict := u.ladders[up.IfName].Verdict
		u.healthMu.Unlock()
		v := UplinkView{
			IfName: up.IfName, Slot: string(up.Slot), Label: r.Label,
			Table: up.Table, Gateway: r.StaticGateway,
			Healthy: r.Healthy, Verdict: verdict, ForceState: r.ForceState,
		}
		for _, a := range addrs {
			if a.IfName == up.IfName {
				v.Addrs = append(v.Addrs, a.CIDR)
			}
		}
		st.Uplinks = append(st.Uplinks, v)
	}

	if m, err := system.ReadMarker(u.Paths); err == nil && m != nil {
		st.PendingPlanID, st.ConfirmDeadlineUnix = m.PlanID, m.DeadlineUnix
	}
	// Enough for a chip on the secondary uplink's card; the detail is a tab away.
	if pool := u.vpnPoolNow(ctx); pool.Active() {
		st.VPN.Active = true
		for _, tn := range pool.Tunnels {
			if s, err := u.wg().Status(ctx, tn.IfName); err == nil && s.Connected() {
				st.VPN.Connected = true
				break
			}
		}
	}
	if len(uplinks) < 2 {
		st.Warnings = append(st.Warnings,
			fmt.Sprintf("%d uplink(s) assigned — no failover and no split routing", len(uplinks)))
	}
	return st, nil
}

func (u *networkUsecase) Groups(ctx context.Context) ([]domain.WANGroup, error) {
	return u.GroupRepo.List(ctx)
}

func (u *networkUsecase) SetLabel(ctx context.Context, key, label string) error {
	row, err := u.IfRepo.GetByKey(ctx, key)
	if err != nil {
		return err
	}
	return u.IfRepo.DB().WithContext(ctx).Model(&domain.NetworkInterface{}).
		Where("id = ?", row.ID).Update("label", label).Error
}

// Plan is a dry run: validate, build the op list, write nothing.
func (u *networkUsecase) Plan(ctx context.Context, req domain.ChangeRequest) (*PlanView, error) {
	in, err := u.validationInput(ctx, req)
	if err != nil {
		return nil, err
	}
	verdicts := domain.Validate(in)
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}

	ops := []string{}
	if !domain.Rejected(verdicts) {
		plan, err := u.buildPlan(ctx, req)
		if err != nil {
			return nil, err
		}
		ops = plan.Descriptions()
	}
	return &PlanView{Ops: ops, Verdicts: verdicts}, nil
}

func (u *networkUsecase) validationInput(ctx context.Context, req domain.ChangeRequest) (domain.ValidationInput, error) {
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return domain.ValidationInput{}, err
	}
	// V14 runs against the live LAN, not a hypothetical one: a static uplink
	// address that overlaps the configured bridge is the real failure.
	return domain.ValidationInput{
		Rows: rows, Req: req, LAN: u.lanConfig(ctx), MgmtCIDR: defaultMgmtCIDR,
		HostapdInstalled: binaryExists("hostapd"),
		IWDInstalled:     binaryExists("iwd"),
	}, nil
}

// buildPlan turns a validated request into ops. The first apply carries the
// netplan takeover.
func (u *networkUsecase) buildPlan(ctx context.Context, req domain.ChangeRequest) (system.Plan, error) {
	var plan system.Plan

	if !system.TakeoverDone(u.Paths) {
		plan.Ops = append(plan.Ops, system.TakeoverOps(u.Paths)...)
	}

	plan.Ops = append(plan.Ops, system.Op{
		Desc: fmt.Sprintf("assign %s to interface %d", req.Role, req.InterfaceID),
		Do: func(ctx context.Context) error {
			return u.IfRepo.DB().Transaction(func(tx *gorm.DB) error {
				if req.EvictID != nil {
					if err := u.IfRepo.SetRoleTx(ctx, tx, *req.EvictID,
						domain.RoleUnassigned, domain.SlotNone); err != nil {
						return err
					}
				}
				return u.IfRepo.SetRoleTx(ctx, tx, req.InterfaceID, req.Role, req.Slot)
			})
		},
	})

	plan.Ops = append(plan.Ops, system.Op{
		Desc: "render networkd units, rt_tables and the sysctl drop-in",
		Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
	})

	plan.Ops = append(plan.Ops, system.Op{
		Desc: "install routing policy rules, nft ingress pins and sysctls",
		Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
	})

	return plan, nil
}

// renderAll writes every owned file. mgmt is written once and never re-rendered:
// a role change elsewhere must not touch the reserved recovery port.
func (u *networkUsecase) renderAll(ctx context.Context) error {
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return err
	}

	lan := u.lanConfig(ctx)
	lanOn := lan != nil && lan.Enabled
	bridge := ""
	if lanOn {
		bridge = lan.BridgeName
		if bridge == "" {
			bridge = system.LANBridgeName
		}
	}

	var files []system.UplinkFile
	// Named even with no tunnel up: "empty" beats "no such table".
	tables := map[int]string{system.WGTable: system.WGTableName}
	var uplinkNames []string
	// Written once and frozen, so it survives the prune while the role exists.
	mgmtFile := ""

	if lanOn {
		files = append(files, system.RenderLANNetdev(bridge), system.RenderLANNetwork(*lan))
	}

	for _, in := range rows {
		// The port holding the lan role is enslaved like any other member; the
		// bridge itself is the device we created.
		if lanOn && in.Present && (in.Role == domain.RoleLAN || in.Role == domain.RoleLANMember) {
			if f := system.RenderLANMember(in, bridge); f.Name != "" {
				files = append(files, f)
			}
			continue
		}
		switch in.Role {
		case domain.RoleWAN:
			table := tableFor(in.Slot)
			if table == 0 {
				return fmt.Errorf("uplink %s has no slot, so it has no routing table", in.IfName)
			}
			if _, taken := tables[table]; taken {
				return fmt.Errorf("two uplinks claim the %s slot", in.Slot)
			}
			files = append(files, system.RenderUplink(in, table))
			tables[table] = "nasnet-" + string(in.Slot)
			uplinkNames = append(uplinkNames, in.IfName)
		case domain.RoleMgmt:
			mgmtFile = mgmtFileName
			mgmtPath := filepath.Join(u.Paths.NetworkdDir, mgmtFileName)
			if _, err := os.Stat(mgmtPath); os.IsNotExist(err) {
				files = append(files, system.RenderMgmt(in, defaultMgmtCIDR))
			}
		}
	}

	// A role that went away takes its file with it, or networkd keeps applying it.
	keep := []string(nil)
	if mgmtFile != "" {
		keep = append(keep, mgmtFile)
	}
	if err := system.WriteFilesExactly(u.Paths.NetworkdDir, files, keep...); err != nil {
		return err
	}
	// Must take effect before the reload or networkd removes the rules we install next
	if err := system.EnsureNetworkdConf(ctx, u.Paths); err != nil {
		return err
	}
	if err := os.MkdirAll(u.Paths.RTTablesDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(u.Paths.RTTablesDir, "nasnet.conf"),
		[]byte(system.RenderRTTables(tables)), 0o644); err != nil {
		return err
	}
	if err := os.MkdirAll(u.Paths.SysctlDir, 0o755); err != nil {
		return err
	}
	sysctl := system.RenderSysctl(uplinkNames, false)
	if lanOn {
		sysctl = system.RenderSysctlWithLAN(uplinkNames, bridge)
	}
	return os.WriteFile(filepath.Join(u.Paths.SysctlDir, "99-nasnet-router.conf"),
		[]byte(sysctl), 0o644)
}

func (u *networkUsecase) Apply(ctx context.Context, req domain.ChangeRequest) (*ApplyView, error) {
	plan, err := u.buildPlan(ctx, req)
	if err != nil {
		return nil, err
	}
	takeover := !system.TakeoverDone(u.Paths)
	before := u.rolesOf(ctx, req.InterfaceID, req.EvictID)

	rec, err := u.applier.Apply(ctx, plan, takeover)
	if err != nil {
		// The snapshot covers files, rules and nft, not the database. Without
		// this the role would stick after a failed apply, so the next attempt
		// would look like it worked and the box would disagree with the UI.
		u.restoreRoles(ctx, before)
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
	u.emit(events.EventWANApplied, map[string]any{"plan_id": rec.ID, "ops": ops})
	return view, nil
}

// shouldReconcileAfterRollback reports whether rec is a revert this process has
// not already reacted to.
func shouldReconcileAfterRollback(rec *domain.ApplyRecord, lastSeen uint) bool {
	return rec != nil && rec.Phase == domain.PhaseRolledBack && rec.ID != lastSeen
}

// watchForRollback re-derives the runtime after the dead-man fires. It runs in
// its own process, so the restored intent would otherwise sit in the database
// with nothing acting on it — a reverted LAN change left dnsmasq down.
func (u *networkUsecase) watchForRollback(ctx context.Context) {
	if u.ApplyRepo == nil {
		return
	}
	rec, err := u.ApplyRepo.Latest(ctx)
	if err != nil || !shouldReconcileAfterRollback(rec, u.lastRolledBack) {
		return
	}
	u.lastRolledBack = rec.ID
	if err := u.Reconcile(ctx); err != nil {
		u.emit(events.EventWANApplyRolledBack, map[string]any{
			"plan_id": rec.ID, "error": err.Error(),
		})
	}
}

// roleSnapshot is what a role change has to be able to undo.
type roleSnapshot struct {
	ID   uint
	Role domain.InterfaceRole
	Slot domain.UplinkSlot
}

func (u *networkUsecase) rolesOf(ctx context.Context, ids ...any) []roleSnapshot {
	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return nil
	}
	want := map[uint]bool{}
	for _, id := range ids {
		switch v := id.(type) {
		case uint:
			want[v] = true
		case *uint:
			if v != nil {
				want[*v] = true
			}
		}
	}
	var out []roleSnapshot
	for _, r := range rows {
		if want[r.ID] {
			out = append(out, roleSnapshot{ID: r.ID, Role: r.Role, Slot: r.Slot})
		}
	}
	return out
}

func (u *networkUsecase) restoreRoles(ctx context.Context, snaps []roleSnapshot) {
	for _, s := range snaps {
		_ = u.IfRepo.SetRoleTx(ctx, nil, s.ID, s.Role, s.Slot)
	}
}

func (u *networkUsecase) Confirm(ctx context.Context, planID uint) error {
	return u.applier.Confirm(ctx, planID)
}

func (u *networkUsecase) Rollback(ctx context.Context) error {
	_, err := u.applier.Rollback(ctx, false)
	return err
}

// IngressUplinkIfName is the uplink clients arrive on, i.e. the domestic one.
func (u *networkUsecase) IngressUplinkIfName() string {
	if !u.RouterMode {
		return ""
	}
	row, err := u.IfRepo.GetBySlot(context.Background(), domain.SlotDomestic)
	if err != nil || row == nil || !row.Present {
		return ""
	}
	return row.IfName
}

// StartHealthLoop probes each uplink and applies one route operation per change.
func (u *networkUsecase) StartHealthLoop(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				u.watchForRollback(ctx)
				u.probeOnce(ctx)
				u.checkVPNHealth(ctx)
			}
		}
	}()
}

func (u *networkUsecase) probeOnce(ctx context.Context) {
	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleWAN)
	if err != nil {
		return
	}
	gwByIf, forceByIf, idByIf := map[string]string{}, map[string]string{}, map[string]uint{}
	learnedByIf := map[string]string{}
	for _, r := range rows {
		gwByIf[r.IfName], forceByIf[r.IfName], idByIf[r.IfName] = r.StaticGateway, r.ForceState, r.ID
		learnedByIf[r.IfName] = r.LearnedGateway
	}

	for _, up := range uplinks {
		gw := gwByIf[up.IfName]
		if gw == "" {
			// DHCP uplink: the gateway is whatever its own table holds. Remember
			// it, because failover deletes the route we read it from.
			if routes, err := u.Backend.RouteList(ctx, up.Table); err == nil {
				for _, r := range routes {
					if r.Dest == "default" && r.Gateway != "" {
						gw = r.Gateway
					}
				}
			}
			if gw != "" && gw != learnedByIf[up.IfName] {
				_ = u.IfRepo.SetLearnedGateway(ctx, idByIf[up.IfName], gw)
				// The kill switch names this gateway; the probe has to get past it.
				if up.Slot == domain.SlotSecondary {
					_ = ApplyKillSwitchState(ctx, u.Nft, uplinks, gw,
						u.healthConfigSnapshot().probeExemptIPs())
				}
			}
			if gw == "" {
				gw = learnedByIf[up.IfName]
			}
		}

		force := forceByIf[up.IfName]
		gatewayUp, _, err := u.health.Observe(ctx, up, gw, force)
		if err != nil {
			continue
		}

		cfg := u.healthConfigSnapshot()
		targets := cfg.targetsFor(up.Slot)
		var mark uint32
		if up.Slot == domain.SlotSecondary {
			// Only the secondary leg meets the kill switch.
			mark = netmark.PinMark(netmark.PinProbe)
		}
		results := probeAll(ctx, u.targetProber(), up.IfName, mark, targets)
		// A forced or gateway-dead uplink cannot blame its targets; skip the damper.
		inetUp := true
		inetKnown := len(targets) > 0 && gatewayUp && force == ""
		if inetKnown {
			inetUp, _ = u.inetState(up.IfName).observe(anyUp(results), defaultInternetLimits(), time.Now())
		}
		if inetKnown {
			u.ring(up.IfName).push(tickSample(time.Now(), results))
		}
		u.observeDegraded(up, cfg, u.ring(up.IfName))

		routeErr := u.applyRouteState(ctx, up, gw, routeStateFor(routeInputs{
			Slot: up.Slot, GatewayUp: gatewayUp, InternetUp: inetUp,
			FailoverOn: cfg.FailoverToVPN, VPNUp: u.poolConnectedNow(ctx),
		}))

		u.storeLadder(ctx, up, force, gatewayUp, inetUp, inetKnown, results)
		// Kernel refused the route op: recording a recovery would be a lie.
		effective := gatewayUp && inetUp && routeErr == nil
		if u.effectiveChanged(up.IfName, effective) {
			_ = u.IfRepo.SetHealth(ctx, idByIf[up.IfName], effective)
			t := events.EventWANDown
			if effective {
				t = events.EventWANUp
			}
			u.emit(t, map[string]any{"if_name": up.IfName, "slot": string(up.Slot),
				"gateway": gatewayUp, "internet": inetUp})
		}
	}

	u.probePool(ctx, u.healthConfigSnapshot())

	if addrs, err := u.Backend.Addrs(ctx); err == nil {
		for _, v := range LeaseVerdicts(addrs, uplinks, false, "") {
			u.emit(events.EventWANLeaseWarning, map[string]any{
				"rule": v.Rule, "level": string(v.Level), "message": v.Message,
			})
		}
	}
}
