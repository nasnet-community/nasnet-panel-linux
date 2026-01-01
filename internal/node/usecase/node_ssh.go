package usecase

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

// === SSH Management ===

func (u *nodeUsecase) GetNodeSSHStatus(ctx context.Context, nodeID uint) (*domain.SSHStatus, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, ErrNodeNotFound
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	resp, err := client.GetSSHStatus(ctx)
	if err != nil {
		return nil, err
	}

	return &domain.SSHStatus{
		Enabled:  resp.Enabled,
		Port:     int(resp.Port),
		IsActive: resp.IsActive,
	}, nil
}

func (u *nodeUsecase) UpdateNodeSSHConfig(ctx context.Context, nodeID uint, enabled bool, port int) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return ErrNodeNotFound
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	return client.UpdateSSHConfig(ctx, enabled, port)
}

func (u *nodeUsecase) ClearNodeSSHLogs(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return ErrNodeNotFound
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	return client.ClearSSHLogs(ctx)
}

func (u *nodeUsecase) RestartNodeSSH(ctx context.Context, nodeID uint) error {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return ErrNodeNotFound
	}

	client, err := u.getAgentClient(node)
	if err != nil {
		return err
	}
	defer client.Close()

	return client.RestartSSH(ctx)
}
