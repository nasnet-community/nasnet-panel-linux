package usecase

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// servicePushWait bounds the push that takes a node's users off or puts them back.
const servicePushWait = 25 * time.Second

// applyService pushes a node's config right after it went in or out of service.
// Out of service the config has no users, so nobody can connect while Xray keeps running.
func (u *nodeUsecase) applyService(ctx context.Context, node *domain.Node) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), servicePushWait)
	defer cancel()
	err := u.pushConfigToAgent(ctx, node)

	u.leftMu.Lock()
	defer u.leftMu.Unlock()
	log := logger.GetLogger().WithField("node_id", node.ID)
	switch {
	case err != nil && !node.IsActive:
		u.usersLeft[node.ID] = true
		log.WithError(err).Warn("[Service] Node is out of service but still holds its users; retrying on reconcile")
	case err != nil:
		delete(u.usersLeft, node.ID)
		log.WithError(err).Warn("[Service] Node is back in service; its users return on the next push")
	default:
		delete(u.usersLeft, node.ID)
	}
}

// UsersLeftOn reports whether an out-of-service node may still let its users in.
func (u *nodeUsecase) UsersLeftOn(nodeID uint) bool {
	u.leftMu.Lock()
	defer u.leftMu.Unlock()
	return u.usersLeft[nodeID]
}

// RemoveLeftoverUsers retries taking users off out-of-service nodes the panel couldn't reach.
// everyIdle covers every out-of-service node, for a fresh start that remembers none.
func (u *nodeUsecase) RemoveLeftoverUsers(ctx context.Context, everyIdle bool) {
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[Service] Could not list nodes")
		return
	}
	for _, n := range nodes {
		if n.IsLocal() || n.IsLite() {
			continue
		}
		if n.IsActive {
			u.leftMu.Lock()
			delete(u.usersLeft, n.ID) // back in service since
			u.leftMu.Unlock()
			continue
		}
		if everyIdle || u.UsersLeftOn(n.ID) {
			u.applyService(ctx, n)
		}
	}
}
