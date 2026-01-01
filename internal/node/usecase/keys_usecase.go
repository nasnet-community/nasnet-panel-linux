package usecase

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// GenerateVLESSKeys routes the request through any available agent node
// that has xray-core installed to run `xray vlessenc`.
func (u *nodeUsecase) GenerateVLESSKeys(ctx context.Context) ([]map[string]string, error) {
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no nodes available to generate VLESS keys")
	}

	// Try each node until one succeeds
	for _, node := range nodes {
		client, err := u.getAgentClient(node)
		if err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", node.ID).Debug("Skipping node for VLESS key generation")
			continue
		}
		defer client.Close()

		keys, err := client.GenerateVLESSKeys(ctx)
		if err != nil {
			logger.GetLogger().WithError(err).WithField("node_id", node.ID).Debug("VLESS key generation failed on node")
			continue
		}

		result := make([]map[string]string, 0, len(keys))
		for _, k := range keys {
			result = append(result, map[string]string{
				"label":      k.Label,
				"decryption": k.Decryption,
				"encryption": k.Encryption,
			})
		}
		return result, nil
	}

	return nil, fmt.Errorf("VLESS key generation failed on all available nodes")
}
