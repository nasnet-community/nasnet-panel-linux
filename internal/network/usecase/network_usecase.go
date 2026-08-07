package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
	"gorm.io/gorm"
)

const defaultMgmtCIDR = "192.168.99.1/24"

// InterfaceView is one NIC as the UI sees
type InterfaceView struct {
	agent.NetInterface
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
	SetLabel(ctx context.Context, key, label string) error
	IngressUplinkIfName() string
}

type Deps struct {
	IfRepo     repository.InterfaceRepository
	GroupRepo  repository.GroupRepository
	ApplyRepo  repository.ApplyRepository
	Backend    system.Backend
	Nft        *nft.Manager
	Agent      agent.NodeClient
	Paths      system.Paths
	RouterMode bool
	EventBus   *events.EventBus
}

type networkUsecase struct {
	Deps
	applier *system.Applier
	health  *HealthMonitor
}

func NewNetworkUsecase(d Deps) NetworkUsecase {
	u := &networkUsecase{
		Deps:   d,
		health: NewHealthMonitor(d.Backend, NewKernelProbe(), DefaultDamping()),
	}
	u.applier = &system.Applier{
		Snap:   &system.Snapshotter{Backend: d.Backend, Nft: d.Nft, Paths: d.Paths},
		Repo:   d.ApplyRepo,
		Paths:  d.Paths,
		Reload: system.ReloadNetworkd,
		OnRollback: func(planID uint) {
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
	return ApplySysctls(ctx, u.Backend, uplinks, false)
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
		if !in.Assignable {
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
			IfName: r.IfName, Table: tableFor(r.Slot),
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
		v := InterfaceView{
			Role: string(r.Role), Slot: string(r.Slot), Label: r.Label,
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
	st := &StateView{RouterMode: u.RouterMode, TakeoverDone: system.TakeoverDone(u.Paths)}
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

	var ops []string
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
	return domain.ValidationInput{
		Rows: rows, Req: req, MgmtCIDR: defaultMgmtCIDR,
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

	var files []system.UplinkFile
	tables := map[int]string{}
	var uplinkNames []string

	for _, in := range rows {
		switch in.Role {
		case domain.RoleWAN:
			table := tableFor(in.Slot)
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
	return os.WriteFile(filepath.Join(u.Paths.SysctlDir, "99-nasnet-router.conf"),
		[]byte(system.RenderSysctl(uplinkNames, false)), 0o644)
}

func (u *networkUsecase) Apply(ctx context.Context, req domain.ChangeRequest) (*ApplyView, error) {
	plan, err := u.buildPlan(ctx, req)
	if err != nil {
		return nil, err
	}
	takeover := !system.TakeoverDone(u.Paths)

	rec, err := u.applier.Apply(ctx, plan, takeover)
	if err != nil {
		return nil, err
	}
	view := &ApplyView{PlanID: rec.ID, Ops: rec.Ops}
	if rec.Deadline != nil {
		view.ConfirmDeadlineUnix = rec.Deadline.Unix()
	}
	return view, nil
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
	for _, r := range rows {
		gwByIf[r.IfName], forceByIf[r.IfName], idByIf[r.IfName] = r.StaticGateway, r.ForceState, r.ID
	}

	for _, up := range uplinks {
		gw := gwByIf[up.IfName]
		if gw == "" {
			// DHCP uplink: the gateway is whatever its own table holds.
			if routes, err := u.Backend.RouteList(ctx, up.Table); err == nil {
				for _, r := range routes {
					if r.Dest == "default" && r.Gateway != "" {
						gw = r.Gateway
					}
				}
			}
		}

		healthy, changed, err := u.health.Observe(ctx, up, gw, forceByIf[up.IfName])
		if err != nil {
			continue
		}
		if err := u.health.ApplyRoute(ctx, up, gw, healthy); err != nil {
			continue
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
