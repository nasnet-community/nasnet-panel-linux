package usecase

import (
	"context"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/certwatch"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// SetCertWatch wires the sighting tracker: presented certs retire superseded
// ones, and admin revokes ask it whether a cert is the one a node uses.
func (u *nodeUsecase) SetCertWatch(w *certwatch.Watch) {
	u.certWatch = w
	w.SetOnChange(u.retireSuperseded)
	if u.certUC != nil {
		u.certUC.SetSightingLookup(func(nodeID uint) (string, bool) {
			s, ok := w.Sighting(nodeID)
			return s.Serial, ok
		})
	}
}

// EvictAllAgentClients drops every pooled direct conn so the next RPC does a
// full TLS handshake under the current revocation list. In strict mode it
// also ends the long-lived connections of nodes last seen with a now-revoked
// cert (reverse sessions, direct heartbeat streams); their reconnect is
// refused at the TLS handshake.
func (u *nodeUsecase) EvictAllAgentClients() {
	if u.clientPool != nil {
		u.clientPool.EvictAll()
	}
	w := u.certWatch
	if w == nil || !w.Strict() {
		return
	}
	for _, s := range w.RevokedSightings() {
		switch {
		case s.Mode == "reverse" && u.reverseRegistry != nil:
			if sess := u.reverseRegistry.GetByNodeID(s.NodeID); sess != nil {
				sess.Kick()
				logger.GetLogger().WithField("node_id", s.NodeID).Warn("[CertWatch] Closed reverse session using a revoked certificate")
			}
		case s.Mode == "direct" && u.heartbeatMgr != nil:
			// The resync redials within 30 s and is refused.
			u.heartbeatMgr.StopNode(s.NodeID)
		}
	}
}

// retireSuperseded revokes a node's older certs once it presents its newest.
func (u *nodeUsecase) retireSuperseded(nodeID uint, serial string) {
	if u.certUC == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	n, err := u.certUC.RetireSuperseded(ctx, nodeID, serial)
	if err != nil {
		logger.GetLogger().WithError(err).WithField("node_id", nodeID).Warn("[CertWatch] Failed to retire superseded certificates")
		return
	}
	if n > 0 {
		logger.GetLogger().WithFields(map[string]interface{}{"node_id": nodeID, "count": n}).Info("[CertWatch] Retired superseded certificates")
	}
}
