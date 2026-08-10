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
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"gorm.io/gorm"
)

const defaultMgmtCIDR = "192.168.99.1/24"

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
	ForceState string   `json:"force_state"`
}

type StateView struct {
	RouterMode          bool         `json:"router_mode"`
	TakeoverDone        bool         `json:"takeover_done"`
	Warnings            []string     `json:"warnings"`
	Uplinks             []UplinkView `json:"uplinks"`
	PendingPlanID       uint         `json:"pending_plan_id"`
	ConfirmDeadlineUnix int64        `json:"confirm_deadline_unix"`
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
	// StartRangesRefreshLoop keeps the domestic prefix list current. Zero uses
	// the default weekly cadence.
	StartRangesRefreshLoop(ctx context.Context, interval time.Duration)
	RefreshDomesticRanges(ctx context.Context) error
	SetLabel(ctx context.Context, key, label string) error
	IngressUplinkIfName() string

	GetLAN(ctx context.Context) (*LANView, error)
	UpdateLAN(ctx context.Context, cfg domain.LANConfig) ([]domain.Verdict, *ApplyView, error)

	ListPortForwards(ctx context.Context) ([]domain.PortForward, error)
	CreatePortForward(ctx context.Context, pf domain.PortForward, confirmed bool) ([]domain.Verdict, error)
	UpdatePortForward(ctx context.Context, pf domain.PortForward, confirmed bool) ([]domain.Verdict, error)
	DeletePortForward(ctx context.Context, id uint) error

	// OnInboundsChanged re-derives filter_in. Called from the inbound
	// create/update/delete paths so an accept cannot drift from its inbound.
	OnInboundsChanged(ctx context.Context) error
}

type Deps struct {
	IfRepo     repository.InterfaceRepository
	GroupRepo  repository.GroupRepository
	ApplyRepo  repository.ApplyRepository
	LANRepo    repository.LANRepository
	PFRepo     repository.PortForwardRepository
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
	// RangesUserID is upstream's optional tracking parameter, omitted when empty.
	RangesURL    string
	RangesUserID string
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
	setsMu    sync.Mutex
	lanSets   []nft.Set
	nftSetOK  bool
	setsBuilt bool

	// lastRolledBack is the revert this process has already reacted to.
	lastRolledBack uint
}

func NewNetworkUsecase(d Deps) NetworkUsecase {
	u := &networkUsecase{
		Deps:    d,
		health:  NewHealthMonitor(d.Backend, NewKernelProbe(), DefaultDamping()),
		dnsmasq: system.NewDNSMasq(),
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
	if err := ReconcileRules(ctx, u.Backend, AllRules(groups, uplinks)); err != nil {
		return err
	}
	if err := ApplyNftState(ctx, u.Nft, uplinks); err != nil {
		return err
	}

	lan := u.lanConfig(ctx)
	if err := u.applyLAN(ctx, lan, uplinks); err != nil {
		return err
	}
	bridge, on := "", lan != nil && lan.Enabled
	if on {
		bridge = lan.BridgeName
	}
	return ApplySysctls(ctx, u.Backend, uplinks, on, bridge)
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
func (u *networkUsecase) applyLAN(ctx context.Context, lan *domain.LANConfig, uplinks []Uplink) error {
	if lan == nil || !lan.Enabled {
		if err := ApplyLANNftState(ctx, u.Nft, nil, uplinks, nil); err != nil {
			return err
		}
		return u.dnsmasq.Stop(ctx)
	}

	sets, nftSetOK, err := u.domesticSets(ctx)
	if err != nil {
		return err
	}
	if err := ApplyLANNftState(ctx, u.Nft, lan, uplinks, sets); err != nil {
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

	cfg := LANDNSConfig(*lan, uplinks, DefaultDomesticDNS, DefaultForeignDNS,
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
		v := UplinkView{
			IfName: up.IfName, Slot: string(up.Slot), Label: r.Label,
			Table: up.Table, Gateway: r.StaticGateway,
			Healthy: r.Healthy, ForceState: r.ForceState,
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
	tables := map[int]string{}
	var uplinkNames []string

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
			mgmtPath := filepath.Join(u.Paths.NetworkdDir, "40-nasnet-mgmt.network")
			if _, err := os.Stat(mgmtPath); os.IsNotExist(err) {
				files = append(files, system.RenderMgmt(in, defaultMgmtCIDR))
			}
		}
	}

	if err := system.WriteFiles(u.Paths.NetworkdDir, files); err != nil {
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
			}
			if gw == "" {
				gw = learnedByIf[up.IfName]
			}
		}

		healthy, changed, err := u.health.Observe(ctx, up, gw, forceByIf[up.IfName])
		if err != nil {
			continue
		}
		// Never withdraw a route we could not put back: with no gateway, or
		// before the uplink has ever been seen healthy, leave networkd's alone.
		if gw != "" && (healthy || u.health.EverUp(up.IfName)) {
			if err := u.health.ApplyRoute(ctx, up, gw, healthy); err != nil {
				continue
			}
		}
		if changed {
			_ = u.IfRepo.SetHealth(ctx, idByIf[up.IfName], healthy)
			t := events.EventWANDown
			if healthy {
				t = events.EventWANUp
			}
			u.emit(t, map[string]any{"if_name": up.IfName, "slot": string(up.Slot)})
		}
	}

	if addrs, err := u.Backend.Addrs(ctx); err == nil {
		for _, v := range LeaseVerdicts(addrs, uplinks, false, "") {
			u.emit(events.EventWANLeaseWarning, map[string]any{
				"rule": v.Rule, "level": string(v.Level), "message": v.Message,
			})
		}
	}
}
