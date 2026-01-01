package xray

import (
	"context"
)

// XrayClient defines the interface for interacting with Xray Core
type XrayClient interface {
	// User Management
	AddUser(ctx context.Context, target string, inboundTag string, user *User) error
	RemoveUser(ctx context.Context, target string, inboundTag, email string) error
	GetInboundUsers(ctx context.Context, target string, inboundTag, email string) ([]*User, error)
	GetInboundUsersCount(ctx context.Context, target string, inboundTag string) (int64, error)
	GetUserStats(ctx context.Context, target string, email string, reset bool) (*UserStats, error)

	// Inbound Management
	AddInbound(ctx context.Context, target string, config *InboundConfig) error
	UpdateInbound(ctx context.Context, target string, config *InboundConfig) error
	RemoveInbound(ctx context.Context, target string, tag string) error
	ListInbounds(ctx context.Context, target string, onlyTags bool) ([]*InboundInfo, error)

	// Outbound Management
	AddOutbound(ctx context.Context, target string, config *OutboundConfig) error
	RemoveOutbound(ctx context.Context, target string, tag string) error
	ListOutbounds(ctx context.Context, target string) ([]*OutboundInfo, error)

	// Routing Management
	AddRoutingRule(ctx context.Context, target string, cfg *RoutingRuleConfig) error
	RemoveRoutingRule(ctx context.Context, target string, tag string) error

	// System & Diagnostics
	GetSysStats(ctx context.Context, target string) (*SysStats, error)
	PingTarget(ctx context.Context, target string) (int64, error)
	CloseAll()
}
