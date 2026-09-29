package cmd

import (
	"context"
	"errors"
	"net/netip"
	"net/url"

	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/nodebridge"
	coreRepository "github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	coreUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// sshdPort stays with the box's own sshd on every node.
const sshdPort = 22

type tunnelNodes interface {
	GetNode(ctx context.Context, id uint) (*nodeDomain.Node, error)
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*nodeDomain.Inbound, error)
}

type tunnelSubs interface {
	FindByID(ctx context.Context, id uint) (*subDomain.Subscription, error)
}

type tunnelDeps struct {
	Repo     coreRepository.CoreRepository
	Subs     tunnelSubs
	Nodes    tunnelNodes
	NodeUC   nodeUsecase.NodeUsecase
	Embedded *agentserver.Server
	// BaseURL is the panel's public address: the built-in node's links fall
	// back to its host when the node's own IP is loopback.
	BaseURL   string
	PanelPort int
	HubPort   int
	// InboundsChanged re-derives the box's firewall and port mapper; nil
	// outside router mode.
	InboundsChanged func(ctx context.Context) error
	// RouterMode: the built-in node sends MTProto through the marked relay.
	RouterMode bool
	Log        *logrus.Logger
}

// newTunnelUsecase builds the SSH/MTProto tunnel usecase. The built-in node is
// reached through the in-process agent without a database read, so a
// reconcile inside a transaction cannot block on SQLite's single connection.
func newTunnelUsecase(d tunnelDeps) *coreUsecase.CoreUsecase {
	client := tunnelClient(d)
	return coreUsecase.NewCoreUsecase(d.Repo,
		coreUsecase.WithNodeClientResolver(func(ctx context.Context, nodeID uint) (coreUsecase.CoreStatePusher, error) {
			return client(ctx, nodeID)
		}),
		coreUsecase.WithNodeStatusResolver(func(ctx context.Context, nodeID uint) (coreUsecase.CoreStatusReader, error) {
			return client(ctx, nodeID)
		}),
		coreUsecase.WithNodeHostResolver(func(ctx context.Context, nodeID uint) (string, error) {
			n, err := d.Nodes.GetNode(ctx, nodeID)
			if err != nil {
				return "", err
			}
			return tunnelHost(n, d.BaseURL), nil
		}),
		coreUsecase.WithNodeNameResolver(func(ctx context.Context, nodeID uint) (string, error) {
			n, err := d.Nodes.GetNode(ctx, nodeID)
			if err != nil {
				return "", err
			}
			return n.Name, nil
		}),
		coreUsecase.WithExternalPortChecker(func(ctx context.Context, nodeID uint, port int) (bool, error) {
			return tunnelPortTaken(ctx, d, nodeID, port)
		}),
		coreUsecase.WithUserPolicyResolver(func(ctx context.Context, subIDs []uint) (map[uint]nodebridge.UserPolicy, error) {
			return tunnelPolicies(ctx, d.Subs, subIDs), nil
		}),
		coreUsecase.WithRelayedNodes(func(nodeID uint) bool {
			return d.RouterMode && nodeID != 0 && nodeID == d.NodeUC.LocalNodeID()
		}),
		coreUsecase.WithInboundsChanged(func(ctx context.Context, nodeID uint) {
			if d.InboundsChanged == nil || nodeID != d.NodeUC.LocalNodeID() {
				return
			}
			if err := d.InboundsChanged(ctx); err != nil {
				d.Log.WithError(err).Warn("Firewall did not follow the tunnel change")
			}
		}),
	)
}

// newTunnelInstaller builds the tunnel binary installer; the install runs on
// each node through its agent.
func newTunnelInstaller(d tunnelDeps) *coreUsecase.InstallUsecase {
	client := tunnelClient(d)
	return coreUsecase.NewInstallUsecase(func(ctx context.Context, nodeID uint) (coreUsecase.DepAgent, error) {
		return client(ctx, nodeID)
	})
}

// tunnelClient resolves a node's agent: the in-process one for the built-in
// node, the pooled or reverse client otherwise.
func tunnelClient(d tunnelDeps) func(ctx context.Context, nodeID uint) (agent.NodeClient, error) {
	embedded := agent.NewEmbeddedClient(d.Embedded)
	return func(ctx context.Context, nodeID uint) (agent.NodeClient, error) {
		if nodeID != 0 && nodeID == d.NodeUC.LocalNodeID() {
			return embedded, nil
		}
		return d.NodeUC.GetNodeClient(ctx, nodeID)
	}
}

// tunnelPolicies pushes each tunnel user's subscription state with it: a
// subscription that no longer grants access keeps its tunnels off whatever
// their own flag says, and telemt enforces the expiry and speed cap natively.
// A subscription that cannot be read right now is left out (its users keep
// their own flag); one that is gone revokes them.
func tunnelPolicies(ctx context.Context, subs tunnelSubs, subIDs []uint) map[uint]nodebridge.UserPolicy {
	out := make(map[uint]nodebridge.UserPolicy, len(subIDs))
	for _, id := range subIDs {
		sub, err := subs.FindByID(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				out[id] = nodebridge.UserPolicy{Active: false}
			}
			continue
		}
		pol := nodebridge.UserPolicy{Active: sub.GrantsAccess()}
		if end := sub.GetEffectiveEndDate(); end != nil {
			pol.ExpiresAt = *end
		}
		if mbps := sub.GetEffectiveBandwidthLimit(); mbps > 0 {
			bps := int64(mbps) * 1_000_000 / 8
			pol.RateLimitUpBps, pol.RateLimitDownBps = bps, bps
		}
		out[id] = pol
	}
	return out
}

// tunnelHost is the address tunnel links point at: a remote node's IP; the
// built-in node's IP unless it is loopback or unspecified, then the panel's
// public host. Empty when neither is known.
func tunnelHost(n *nodeDomain.Node, baseURL string) string {
	if !n.IsLocal() {
		return n.IP
	}
	if ip, err := netip.ParseAddr(n.IP); n.IP != "" && (err != nil || !(ip.IsLoopback() || ip.IsUnspecified())) {
		return n.IP
	}
	if u, err := url.Parse(baseURL); err == nil {
		return u.Hostname()
	}
	return ""
}

// tunnelPortTaken reports whether something other than a tunnel holds port on
// the node: an xray inbound (port or range), the xray API, sshd, and on the
// built-in node the panel and the reverse-mode hub, or on a remote node its
// agent.
func tunnelPortTaken(ctx context.Context, d tunnelDeps, nodeID uint, port int) (bool, error) {
	n, err := d.Nodes.GetNode(ctx, nodeID)
	if err != nil {
		return false, err
	}
	reserved := []int{sshdPort, n.APIPort}
	if n.IsLocal() {
		reserved = append(reserved, d.PanelPort, d.HubPort)
	} else {
		reserved = append(reserved, n.AgentPort)
	}
	for _, p := range reserved {
		if p == port {
			return true, nil
		}
	}
	inbounds, err := d.Nodes.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return false, err
	}
	for _, ib := range inbounds {
		if ib.ListensOn(port) {
			return true, nil
		}
	}
	return false, nil
}
