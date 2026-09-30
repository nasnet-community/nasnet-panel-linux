package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	auditdomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/starlink"
	slpb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
)

var (
	ErrStarlinkAgentOutdated   = errors.New("the node agent is too old for this Starlink feature; update the agent")
	ErrStarlinkConfirmMismatch = errors.New("confirmation does not match the node name")
	ErrStarlinkUnknownAction   = errors.New("unknown Starlink action")
)

// StarlinkStatusView is the dish status plus whether the node's agent can
// run the passthrough features (live history, device info, actions).
type StarlinkStatusView struct {
	*starlink.StarlinkStatus
	Passthrough bool `json:"passthrough"`
}

// agentDishHandler sends dish requests through the agent's passthrough RPC.
// Dish refusals come back as *starlink.DishError; an agent without the RPC
// becomes ErrStarlinkAgentOutdated.
func agentDishHandler(pc agent.StarlinkPassthroughClient, addr string) starlink.Handler {
	return func(ctx context.Context, req *slpb.Request) (*slpb.Response, error) {
		// The built-in node's agent is in-process, so no gRPC deadline would
		// bound a silent dish; set one per request kind. The remote client's
		// interceptor only fills a missing deadline, so this governs both.
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, dishCallTimeout(req))
			defer cancel()
		}
		raw, err := proto.Marshal(req)
		if err != nil {
			return nil, err
		}
		resp, err := pc.StarlinkHandle(ctx, &pb.StarlinkHandleRequest{DishAddress: addr, Request: raw})
		if err != nil {
			if isUnimplementedErr(err) {
				return nil, ErrStarlinkAgentOutdated
			}
			return nil, err
		}
		if resp.Code != 0 {
			return nil, &starlink.DishError{Code: codes.Code(resp.Code), Message: resp.Message}
		}
		var out slpb.Response
		if err := proto.Unmarshal(resp.Response, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
}

// dishCallTimeout budgets one dish request, agent hop included (the agent
// trims 2 s off for its own reply). Reads are quick on a LAN; a detailed
// self-test runs the dish's checks before answering.
func dishCallTimeout(req *slpb.Request) time.Duration {
	switch req.GetRequest().(type) {
	case *slpb.Request_GetStatus, *slpb.Request_GetHistory:
		return 5 * time.Second
	case *slpb.Request_SelfTest:
		return 55 * time.Second
	default:
		return 20 * time.Second
	}
}

// starlinkDish loads the node, checks monitoring is on and returns a dish
// client over the agent. Close the returned agent client when done.
func (u *nodeUsecase) starlinkDish(ctx context.Context, nodeID uint) (*domain.Node, *starlink.Client, agent.NodeClient, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, nil, nil, err
	}
	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return nil, nil, nil, fmt.Errorf("starlink monitoring is not enabled on this node")
	}
	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, nil, nil, err
	}
	pc, ok := client.(agent.StarlinkPassthroughClient)
	if !ok {
		closeAgentClient(client)
		return nil, nil, nil, ErrStarlinkAgentOutdated
	}
	return node, starlink.NewClientWithHandler(agentDishHandler(pc, settings.DishAddress)), client, nil
}

// StarlinkErrorCode maps a Starlink usecase error to its API error code, or
// "" for errors that stay a 500.
func StarlinkErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrStarlinkAgentOutdated):
		return "STARLINK_AGENT_OUTDATED"
	case errors.Is(err, ErrStarlinkConfirmMismatch):
		return "STARLINK_CONFIRM_MISMATCH"
	case errors.Is(err, ErrStarlinkUnknownAction):
		return "STARLINK_UNKNOWN_ACTION"
	}
	var de *starlink.DishError
	if errors.As(err, &de) {
		switch de.Code {
		case codes.PermissionDenied:
			return "DISH_PERMISSION_DENIED"
		case codes.Unimplemented:
			return "DISH_UNSUPPORTED"
		case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
			return "DISH_UNREACHABLE"
		default:
			return "DISH_ERROR"
		}
	}
	return ""
}

// GetStarlinkStatus reads status through the passthrough RPC and falls back
// to the legacy status RPC for agents that predate it.
func (u *nodeUsecase) GetStarlinkStatus(ctx context.Context, nodeID uint) (*StarlinkStatusView, error) {
	node, dish, client, err := u.starlinkDish(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	st, err := dish.GetStatus(ctx)
	if err == nil {
		return &StarlinkStatusView{StarlinkStatus: st, Passthrough: true}, nil
	}
	if errors.Is(err, ErrStarlinkAgentOutdated) {
		legacy, lerr := client.GetStarlinkStatus(ctx, node.GetStarlinkSettingsOrDefault().DishAddress)
		if lerr != nil {
			return nil, lerr
		}
		return &StarlinkStatusView{StarlinkStatus: statusFromLegacy(legacy)}, nil
	}
	var de *starlink.DishError
	if errors.As(err, &de) {
		// Dish down or refusing — the same "unavailable" shape the legacy RPC gives.
		return &StarlinkStatusView{StarlinkStatus: &starlink.StarlinkStatus{}, Passthrough: true}, nil
	}
	return nil, err
}

func (u *nodeUsecase) GetStarlinkLive(ctx context.Context, nodeID uint, step int) (*starlink.History, error) {
	_, dish, client, err := u.starlinkDish(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)
	return dish.GetHistory(ctx, step)
}

func (u *nodeUsecase) GetStarlinkDevice(ctx context.Context, nodeID uint) (*starlink.DeviceInfo, error) {
	_, dish, client, err := u.starlinkDish(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)
	return dish.GetDevice(ctx)
}

func statusFromLegacy(a *agent.StarlinkStatus) *starlink.StarlinkStatus {
	return &starlink.StarlinkStatus{
		Available:                        a.Available,
		DownlinkThroughputBps:            a.DownlinkThroughputBps,
		UplinkThroughputBps:              a.UplinkThroughputBps,
		PopPingLatencyMs:                 a.PopPingLatencyMs,
		PopPingDropRate:                  a.PopPingDropRate,
		EthSpeedMbps:                     a.EthSpeedMbps,
		ObstructionFraction:              a.ObstructionFraction,
		CurrentlyObstructed:              a.CurrentlyObstructed,
		AvgProlongedObstructionDurationS: a.AvgProlongedObstructionDurationS,
		AvgProlongedObstructionIntervalS: a.AvgProlongedObstructionIntervalS,
		AlertThermalThrottle:             a.AlertThermalThrottle,
		AlertThermalShutdown:             a.AlertThermalShutdown,
		AlertIsHeating:                   a.AlertIsHeating,
		AlertSlowEthernet:                a.AlertSlowEthernet,
		AlertPowerSaveIdle:               a.AlertPowerSaveIdle,
		AlertMotorsStuck:                 a.AlertMotorsStuck,
		AlertNoEthernetLink:              a.AlertNoEthernetLink,
		AlertUnexpectedLocation:          a.AlertUnexpectedLocation,
		AlertRoaming:                     a.AlertRoaming,
		AlertMastNotNearVert:             a.AlertMastNotNearVert,
		AlertInstallPending:              a.AlertInstallPending,
		HardwareVersion:                  a.HardwareVersion,
		SoftwareVersion:                  a.SoftwareVersion,
		CountryCode:                      a.CountryCode,
		UptimeS:                          a.UptimeS,
		BootCount:                        a.BootCount,
		GpsValid:                         a.GpsValid,
		GpsSats:                          a.GpsSats,
		TiltAngleDeg:                     a.TiltAngleDeg,
		BoresightAzimuthDeg:              a.BoresightAzimuthDeg,
		BoresightElevationDeg:            a.BoresightElevationDeg,
		AttitudeUncertaintyDeg:           a.AttitudeUncertaintyDeg,
		AttitudeEstimationState:          a.AttitudeEstimationState,
		DesiredBoresightAzimuthDeg:       a.DesiredBoresightAzimuthDeg,
		DesiredBoresightElevationDeg:     a.DesiredBoresightElevationDeg,
		ActuatorState:                    a.ActuatorState,
		HasActuators:                     a.HasActuators,
		SoftwareUpdateState:              a.SoftwareUpdateState,
		SoftwareUpdateProgress:           a.SoftwareUpdateProgress,
		OutageCause:                      a.OutageCause,
		OutageDurationNs:                 a.OutageDurationNs,
		DisablementCode:                  a.DisablementCode,
		MobilityClass:                    a.MobilityClass,
		ClassOfService:                   a.ClassOfService,
		CellID:                           a.CellID,
		SatelliteID:                      a.SatelliteID,
		GatewayID:                        a.GatewayID,
		OnBackupBeam:                     a.OnBackupBeam,
		Latitude:                         a.Latitude,
		Longitude:                        a.Longitude,
		Altitude:                         a.Altitude,
		IsSnrAboveNoiseFloor:             a.IsSnrAboveNoiseFloor,
		IsSnrPersistentlyLow:             a.IsSnrPersistentlyLow,
	}
}

const starlinkSelfTestTimeout = 55 * time.Second

type StarlinkActionRequest struct {
	Action    string
	Confirm   string
	ActorID   uint
	ActorName string
	IPAddress string
}

type StarlinkActionResult struct {
	Action   string                   `json:"action"`
	Message  string                   `json:"message"`
	SelfTest *starlink.SelfTestResult `json:"self_test,omitempty"`
}

// droppedMidRestart: a dish that reboots can close the channel before it
// answers. For restarting actions that means the command landed.
func droppedMidRestart(err error) bool {
	var de *starlink.DishError
	if !errors.As(err, &de) {
		return false
	}
	return de.Code == codes.Unavailable || de.Code == codes.DeadlineExceeded || de.Code == codes.Canceled
}

// restartDish runs a command that restarts the dish. A dropped call counts as
// landed only for a dish that answered a moment ago: an unreachable dish
// drops the call the same way, and would otherwise report "rebooting".
func restartDish(ctx context.Context, dish *starlink.Client, run func(context.Context) error) error {
	if _, err := dish.GetStatus(ctx); err != nil {
		return err
	}
	if err := run(ctx); err != nil && !droppedMidRestart(err) {
		return err
	}
	return nil
}

func (u *nodeUsecase) RunStarlinkAction(ctx context.Context, nodeID uint, req StarlinkActionRequest) (*StarlinkActionResult, error) {
	node, dish, client, err := u.starlinkDish(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	res := &StarlinkActionResult{Action: req.Action}
	switch req.Action {
	case "reboot":
		err = restartDish(ctx, dish, dish.Reboot)
		res.Message = "Dish is rebooting. The link drops for 1–3 minutes."
	case "stow":
		err = dish.Stow(ctx, false)
		res.Message = "Dish stowed. The link stays down until you unstow it."
	case "unstow":
		err = dish.Stow(ctx, true)
		res.Message = "Dish unstowed. It reconnects within a couple of minutes."
	case "clear-map":
		err = dish.ClearObstructionMap(ctx)
		res.Message = "Obstruction map cleared. It rebuilds over the next hours."
	case "self-test":
		// A detailed self-test outlasts the 20 s passthrough default.
		testCtx, cancel := context.WithTimeout(ctx, starlinkSelfTestTimeout)
		res.SelfTest, err = dish.SelfTest(testCtx)
		cancel()
		res.Message = "Self-test finished."
	case "gps-inhibit":
		err = dish.InhibitGPS(ctx, true)
		res.Message = "GPS inhibited. The dish positions itself without GPS."
	case "gps-restore":
		err = dish.InhibitGPS(ctx, false)
		res.Message = "GPS restored."
	case "factory-reset":
		if req.Confirm != node.Name {
			return nil, ErrStarlinkConfirmMismatch
		}
		err = restartDish(ctx, dish, dish.FactoryReset)
		res.Message = "Factory reset sent. The dish restarts with default settings."
	default:
		return nil, ErrStarlinkUnknownAction
	}

	u.auditStarlinkAction(ctx, node, req, err)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (u *nodeUsecase) auditStarlinkAction(ctx context.Context, node *domain.Node, req StarlinkActionRequest, actionErr error) {
	if u.auditUC == nil {
		return
	}
	result := "ok"
	if actionErr != nil {
		result = actionErr.Error()
	}
	payload, _ := json.Marshal(map[string]string{"action": req.Action, "result": result})
	u.auditUC.Log(ctx, &auditdomain.AuditLog{
		Action:     "starlink_" + strings.ReplaceAll(req.Action, "-", "_"),
		ActorID:    req.ActorID,
		ActorName:  req.ActorName,
		EntityType: "node",
		EntityID:   node.ID,
		NewValues:  string(payload),
		IPAddress:  req.IPAddress,
		Source:     "admin_api",
	})
}

// GetStarlinkObstructionMap retrieves the live obstruction map via the node's agent.
func (u *nodeUsecase) GetStarlinkObstructionMap(ctx context.Context, nodeID uint) (*agent.StarlinkObstructionMap, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return nil, fmt.Errorf("starlink monitoring is not enabled on this node")
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer closeAgentClient(client)

	// In process on the built-in node nothing else bounds the call; a dish
	// that accepts TCP but stops answering would pin the request.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	return client.GetStarlinkObstructionMap(ctx, settings.DishAddress)
}

// GetStarlinkHistory returns historical Starlink stats from the database.
func (u *nodeUsecase) GetStarlinkHistory(ctx context.Context, nodeID uint, limit int, since *time.Time) ([]*domain.StarlinkStat, error) {
	if limit <= 0 {
		limit = 60
	}
	if limit > 1000 {
		limit = 1000
	}
	return u.nodeRepo.GetStarlinkStatsHistory(ctx, nodeID, limit, since)
}

// starlinkSyncTimeout: 3s inner ceiling so a wedged dish can't starve
// the 10s per-node sweep. Covers status + context + location RPCs.
const starlinkSyncTimeout = 3 * time.Second

// syncStarlinkStats collects a Starlink stat snapshot for a node (called from SyncNodeStats).
func (u *nodeUsecase) syncStarlinkStats(ctx context.Context, node *domain.Node, client agent.NodeClient) {
	settings := node.GetStarlinkSettingsOrDefault()
	if !settings.Enabled {
		return
	}

	log := logger.GetLogger().WithField("node", node.Name)

	pollCtx, cancel := context.WithTimeout(ctx, starlinkSyncTimeout)
	defer cancel()

	status, err := client.GetStarlinkStatus(pollCtx, settings.DishAddress)
	if err != nil {
		log.Debugf("Failed to get starlink status: %v", err)
		return
	}

	if !status.Available {
		return
	}
	u.maybeSyncStarlinkEvents(node, settings.DishAddress)

	stat := &domain.StarlinkStat{
		// UTC like the other time series: SQLite compares these as text and
		// the history query binds UTC.
		CreatedAt:             time.Now().UTC(),
		NodeID:                node.ID,
		DownlinkThroughputBps: status.DownlinkThroughputBps,
		UplinkThroughputBps:   status.UplinkThroughputBps,
		PopPingLatencyMs:      status.PopPingLatencyMs,
		PopPingDropRate:       status.PopPingDropRate,
		ObstructionFraction:   status.ObstructionFraction,
		CurrentlyObstructed:   status.CurrentlyObstructed,
		TiltAngleDeg:          status.TiltAngleDeg,
		BoresightAzimuthDeg:   status.BoresightAzimuthDeg,
		BoresightElevationDeg: status.BoresightElevationDeg,
		GpsValid:              status.GpsValid,
		AlertFlags: domain.AlertFlagsFromBooleans(
			status.AlertThermalShutdown, status.AlertThermalThrottle,
			status.AlertMotorsStuck, status.AlertNoEthernetLink,
			status.AlertIsHeating, status.AlertSlowEthernet,
			status.AlertPowerSaveIdle, status.AlertMastNotNearVert,
			status.AlertRoaming, status.AlertUnexpectedLocation,
			status.AlertInstallPending,
		),
	}

	if err := u.nodeRepo.CreateStarlinkStat(ctx, stat); err != nil {
		log.Warnf("Failed to save starlink stat: %v", err)
	}
}

const (
	starlinkEventSyncInterval = 5 * time.Minute
	starlinkEventSyncTimeout  = 15 * time.Second
)

// maybeSyncStarlinkEvents copies the dish event log into the DB at most once
// per interval per node. It runs off the stats sweep with its own agent
// client: history is ~75 KB and must not eat the sweep's 3 s dish budget.
func (u *nodeUsecase) maybeSyncStarlinkEvents(node *domain.Node, dishAddr string) {
	if last, ok := u.starlinkEventSyncAt.Load(node.ID); ok && time.Since(last.(time.Time)) < starlinkEventSyncInterval {
		return
	}
	// Stamp before fetching so an old agent or a dead dish is retried per
	// interval, not per 10 s tick.
	u.starlinkEventSyncAt.Store(node.ID, time.Now())

	go func() {
		log := logger.GetLogger().WithField("node", node.Name)
		ctx, cancel := context.WithTimeout(context.Background(), starlinkEventSyncTimeout)
		defer cancel()

		client, err := u.getAgentClient(node)
		if err != nil || client == nil {
			return
		}
		defer closeAgentClient(client)
		pc, ok := client.(agent.StarlinkPassthroughClient)
		if !ok {
			return
		}
		hist, err := starlink.NewClientWithHandler(agentDishHandler(pc, dishAddr)).GetHistory(ctx, 60)
		if err != nil {
			log.Debugf("Starlink event sync skipped: %v", err)
			return
		}
		if err := u.nodeRepo.UpsertStarlinkEvents(ctx, starlinkEventRows(node.ID, hist)); err != nil {
			log.Warnf("Failed to save starlink events: %v", err)
		}
	}()
}

// starlinkEventRows turns the dish event log into rows. The event log
// already lists outages (EVENT_REASON_OUTAGE_*), so the separate outage list
// is only used when a firmware sends no event log.
func starlinkEventRows(nodeID uint, h *starlink.History) []*domain.StarlinkEvent {
	seen := map[string]bool{}
	var rows []*domain.StarlinkEvent
	add := func(reason, severity string, start time.Time, durMs int64) {
		start = start.Truncate(time.Millisecond).UTC()
		key := reason + "|" + start.Format(time.RFC3339Nano)
		if seen[key] {
			return
		}
		seen[key] = true
		rows = append(rows, &domain.StarlinkEvent{NodeID: nodeID, Reason: reason, Severity: severity, StartAt: start, DurationMs: durMs})
	}
	for _, e := range h.Events {
		add(e.Reason, e.Severity, e.StartAt, e.DurationMs)
	}
	if len(h.Events) == 0 {
		for _, o := range h.Outages {
			add("EVENT_REASON_OUTAGE_"+o.Cause, "EVENT_SEVERITY_WARNING", o.StartAt, o.DurationMs)
		}
	}
	return rows
}

func (u *nodeUsecase) GetStarlinkEvents(ctx context.Context, nodeID uint, since time.Time, limit int) ([]*domain.StarlinkEvent, error) {
	if limit <= 0 || limit > 2000 {
		limit = 2000
	}
	return u.nodeRepo.ListStarlinkEvents(ctx, nodeID, since, limit)
}
