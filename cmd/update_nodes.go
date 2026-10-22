package cmd

import (
	"context"
	"time"

	nodeUC "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	updateUC "github.com/nasnet-community/nasnet-panel-linux/internal/update/usecase"
)

// updateNodes hands the node side to the Updates section's Other parts card.
type updateNodes struct{ uc nodeUC.NodeUsecase }

func (n updateNodes) AgentUpdates(ctx context.Context) (map[uint]bool, error) {
	return n.uc.AgentUpdates(ctx)
}

func (n updateNodes) XrayVersions(ctx context.Context) ([]updateUC.NodeXray, error) {
	vs, err := n.uc.XrayVersions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]updateUC.NodeXray, 0, len(vs))
	for _, v := range vs {
		out = append(out, updateUC.NodeXray{ID: v.ID, Name: v.Name, Version: v.Version, Local: v.Local})
	}
	return out, nil
}

// buildTime is when this binary, and the geofiles inside it, were built.
func buildTime() time.Time {
	t, _ := time.Parse(time.RFC3339, BuildTime)
	return t
}
