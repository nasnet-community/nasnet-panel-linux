package usecase

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	mntDomain "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/domain"
)

// SettingPair is the key/value shape the usecase uses to persist settings.
// Exported so adapters can build a slice for UpdateMany.
type SettingPair struct {
	Key   string
	Value string
}

// SettingIO is the subset of the setting usecase we depend on.
type SettingIO interface {
	GetByKey(ctx context.Context, key string) (string, error)
	UpdateMany(ctx context.Context, pairs []*SettingPair) error
}

// NodeMaintenanceIO exposes the node columns we need.
type NodeMaintenanceIO interface {
	GetNodeMaintenance(ctx context.Context, nodeID uint) (active bool, message string, since *time.Time, err error)
	SetNodeMaintenance(ctx context.Context, nodeID uint, active bool, message string, since *time.Time) error
}

// SubMaintenanceIO exposes subscription maintenance columns + linked-node lookup.
type SubMaintenanceIO interface {
	GetSubMaintenance(ctx context.Context, subID uint) (active bool, message string, since *time.Time, err error)
	SetSubMaintenance(ctx context.Context, subID uint, active bool, message string, since *time.Time) error
	GetSubLinkedNodeIDs(ctx context.Context, subID uint) ([]uint, error)
}

// BroadcastFn optionally pushes a bot broadcast. Nil-safe.
type BroadcastFn func(ctx context.Context, message string) error

type Usecase interface {
	HydrateFromSettings(ctx context.Context) error
	IsGlobalActive() bool
	Resolve(ctx context.Context, userID uint, subID *uint, fallbackDefault string) mntDomain.Status
	SetGlobal(ctx context.Context, enabled bool, message string, notify bool) error
	SetNode(ctx context.Context, nodeID uint, enabled bool, message string) error
	SetSubscription(ctx context.Context, subID uint, enabled bool, message string) error
}

type maintenanceUsecase struct {
	settings  SettingIO
	nodes     NodeMaintenanceIO
	subs      SubMaintenanceIO
	broadcast BroadcastFn

	globalActive  atomic.Bool
	globalMessage atomic.Value // string
	globalSince   atomic.Value // time.Time
}

func newMaintenanceUsecase(s SettingIO, n NodeMaintenanceIO, sub SubMaintenanceIO, bcast BroadcastFn) *maintenanceUsecase {
	u := &maintenanceUsecase{settings: s, nodes: n, subs: sub, broadcast: bcast}
	u.globalMessage.Store("")
	u.globalSince.Store(time.Time{})
	return u
}

// NewUsecase is the public constructor for bootstrap.
func NewUsecase(s SettingIO, n NodeMaintenanceIO, sub SubMaintenanceIO, bcast BroadcastFn) Usecase {
	return newMaintenanceUsecase(s, n, sub, bcast)
}

func (u *maintenanceUsecase) HydrateFromSettings(ctx context.Context) error {
	v, err := u.settings.GetByKey(ctx, "maintenance_mode_enabled")
	if err != nil {
		return err
	}
	active, _ := strconv.ParseBool(strings.TrimSpace(v))
	u.globalActive.Store(active)

	msg, err := u.settings.GetByKey(ctx, "maintenance_mode_message")
	if err == nil {
		u.globalMessage.Store(msg)
	}

	sinceStr, _ := u.settings.GetByKey(ctx, "maintenance_mode_since")
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			u.globalSince.Store(t)
		}
	}
	return nil
}

func (u *maintenanceUsecase) IsGlobalActive() bool {
	return u.globalActive.Load()
}

func (u *maintenanceUsecase) Resolve(ctx context.Context, userID uint, subID *uint, fallbackDefault string) mntDomain.Status {
	if u.IsGlobalActive() {
		msg, _ := u.globalMessage.Load().(string)
		if msg == "" {
			msg = fallbackDefault
		}
		var sincePtr *time.Time
		if t, ok := u.globalSince.Load().(time.Time); ok && !t.IsZero() {
			tt := t
			sincePtr = &tt
		}
		return mntDomain.Status{
			Active:  true,
			Scope:   mntDomain.ScopeGlobal,
			Message: msg,
			Since:   sincePtr,
		}
	}

	if subID == nil {
		return mntDomain.Status{}
	}

	active, msg, since, err := u.subs.GetSubMaintenance(ctx, *subID)
	if err == nil && active {
		if msg == "" {
			msg = fallbackDefault
		}
		return mntDomain.Status{
			Active:  true,
			Scope:   mntDomain.ScopeSubscription,
			Message: msg,
			Since:   since,
		}
	}

	nodeIDs, err := u.subs.GetSubLinkedNodeIDs(ctx, *subID)
	if err != nil {
		return mntDomain.Status{}
	}
	for _, nid := range nodeIDs {
		nactive, nmsg, nsince, err := u.nodes.GetNodeMaintenance(ctx, nid)
		if err == nil && nactive {
			if nmsg == "" {
				nmsg = fallbackDefault
			}
			return mntDomain.Status{
				Active:  true,
				Scope:   mntDomain.ScopeNode,
				Message: nmsg,
				Since:   nsince,
			}
		}
	}
	return mntDomain.Status{}
}

func (u *maintenanceUsecase) SetGlobal(ctx context.Context, enabled bool, message string, notify bool) error {
	now := time.Now().UTC()
	pairs := []*SettingPair{
		{Key: "maintenance_mode_enabled", Value: strconv.FormatBool(enabled)},
		{Key: "maintenance_mode_message", Value: message},
	}
	if enabled {
		pairs = append(pairs, &SettingPair{Key: "maintenance_mode_since", Value: now.Format(time.RFC3339)})
	} else {
		pairs = append(pairs, &SettingPair{Key: "maintenance_mode_since", Value: ""})
	}
	if err := u.settings.UpdateMany(ctx, pairs); err != nil {
		return err
	}

	u.globalActive.Store(enabled)
	u.globalMessage.Store(message)
	if enabled {
		u.globalSince.Store(now)
	} else {
		u.globalSince.Store(time.Time{})
	}

	if enabled && notify && u.broadcast != nil {
		msg := message
		if msg == "" {
			msg = "Service maintenance in progress. Purchases and renewals are temporarily paused."
		}
		// Fire-and-forget: log but do not fail admin action.
		_ = u.broadcast(ctx, msg)
	}
	return nil
}

func (u *maintenanceUsecase) SetNode(ctx context.Context, nodeID uint, enabled bool, message string) error {
	var since *time.Time
	if enabled {
		t := time.Now().UTC()
		since = &t
	}
	return u.nodes.SetNodeMaintenance(ctx, nodeID, enabled, message, since)
}

func (u *maintenanceUsecase) SetSubscription(ctx context.Context, subID uint, enabled bool, message string) error {
	var since *time.Time
	if enabled {
		t := time.Now().UTC()
		since = &t
	}
	return u.subs.SetSubMaintenance(ctx, subID, enabled, message, since)
}
