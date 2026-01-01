package contract

import (
	"context"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
)

// NodeSyncer is used by subscription lifecycle to sync inbounds before provisioning.
type NodeSyncer interface {
	SyncInbounds(ctx context.Context, nodeID uint) error
}

// AccountManager consolidates account operations needed by subscription, admin, and other packages.
type AccountManager interface {
	CreateAccountForSubscription(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
	CreateAccountForSubscriptionNoEnqueue(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
	CreateAccountManual(ctx context.Context, inboundID uint, email, uuid, flow, encryption string) (uint, string, error)
	GetLinkByEmail(ctx context.Context, email string) (string, error)
	DisableAccountsBySubscription(ctx context.Context, subID uint) error
	EnableAccountsBySubscription(ctx context.Context, subID uint) error
	DeleteAccountsBySubscription(ctx context.Context, subID uint) error
	ForceDeleteAccountsBySubscription(ctx context.Context, subID uint) error
	DeleteAccount(ctx context.Context, id uint) error
	ListAccountsBySubscription(ctx context.Context, subID uint) ([]*accountDomain.Account, error)
	ListAllAccountsBySubscription(ctx context.Context, subID uint) ([]*accountDomain.Account, error)
	ClearAccountDataLimitsBySubscription(ctx context.Context, subID uint) error
	ResetAccountDataUsedBySubscription(ctx context.Context, subID uint) error
	SetAccountsUUIDBySubscription(ctx context.Context, subID uint, newUUID string) (int, error)
}

// AccountReader is used by subscription reconciliation to list active accounts.
type AccountReader interface {
	ListActiveAccountInfos(ctx context.Context) ([]*AccountInfo, error)
}

// AccountInfo is a simplified account struct for reconciliation.
type AccountInfo struct {
	ID         uint
	InboundID  uint
	Email      string
	UUID       string
	Flow       string
	Encryption string
}

// AccountSaver is used by payment usecase to create accounts on completion.
type AccountSaver interface {
	CreateAccountForSubscription(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
	// CreateAccountForSubscriptionNoEnqueue records an account without queuing a
	// provisioning retry — used for inbounds already provisioned successfully.
	CreateAccountForSubscriptionNoEnqueue(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
}
