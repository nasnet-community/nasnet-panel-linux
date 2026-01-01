package provisioning

import (
	"context"
	"fmt"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning/repository"
)

type ProvisioningService interface {
	// EnqueueAddUser schedules a user to be added to an Xray node
	EnqueueAddUser(ctx context.Context, account *accountDomain.Account, targetAddress string) error

	// EnqueueRemoveUser schedules a user to be removed from an Xray node
	EnqueueRemoveUser(ctx context.Context, account *accountDomain.Account, targetAddress string) error

	// CancelTasksForNode cancels all pending tasks for a specific node
	CancelTasksForNode(ctx context.Context, nodeID uint) error
}

type provisioningService struct {
	repo repository.ProvisioningRepository
}

func NewProvisioningService(repo repository.ProvisioningRepository) ProvisioningService {
	return &provisioningService{
		repo: repo,
	}
}

func (s *provisioningService) EnqueueAddUser(ctx context.Context, account *accountDomain.Account, targetAddress string) error {
	if account.Inbound == nil {
		return fmt.Errorf("account inbound is nil, cannot enqueue task")
	}

	task := &domain.ProvisioningTask{
		AccountID:      account.ID,
		NodeID:         account.Inbound.NodeID,
		Type:           domain.TypeAddUser,
		Status:         domain.StatusPending,
		TargetAddress:  targetAddress,
		InboundTag:     account.Inbound.Tag,
		UserEmail:      account.Email,
		UserUUID:       account.UUID,
		UserFlow:       account.Flow,
		UserEncryption: account.Encryption,
		Protocol:       account.Inbound.Protocol,
		UserLevel:      0, // Default level
	}

	return s.repo.Enqueue(ctx, task)
}

func (s *provisioningService) EnqueueRemoveUser(ctx context.Context, account *accountDomain.Account, targetAddress string) error {
	if account.Inbound == nil {
		return fmt.Errorf("account inbound is nil, cannot enqueue task")
	}

	task := &domain.ProvisioningTask{
		AccountID:     account.ID,
		NodeID:        account.Inbound.NodeID,
		Type:          domain.TypeRemoveUser,
		Status:        domain.StatusPending,
		TargetAddress: targetAddress,
		InboundTag:    account.Inbound.Tag,
		UserEmail:     account.Email,
		// UUID/Flow/Protocol not strictly needed for removal, but good for logs
		UserUUID: account.UUID,
		Protocol: account.Inbound.Protocol,
	}

	return s.repo.Enqueue(ctx, task)
}

func (s *provisioningService) CancelTasksForNode(ctx context.Context, nodeID uint) error {
	return s.repo.CancelTasksForNode(ctx, nodeID)
}
