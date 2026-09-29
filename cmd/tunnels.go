package cmd

import (
	"context"
	"net/netip"
	"net/url"

	agentserver "github.com/nasnet-community/nasnet-panel-linux/internal/agent/server"
	coreRepository "github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	coreUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeUsecase "github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/sirupsen/logrus"
)

// sshdPort stays with the box's own sshd on every node.
const sshdPort = 22

type tunnelNodes interface {
	GetNode(ctx context.Context, id uint) (*nodeDomain.Node, error)
	ListInboundsByNode(ctx context.Context, nodeID uint) ([]*nodeDomain.Inbound, error)
}

type tunnelDeps struct {
	Repo     coreRepository.CoreRepository
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
	Log             *logrus.Logger
}

// newTunnelUsecase builds the SSH/MTProto tunnel usecase. The built-in node is
// reached through the in-process agent without a database read, so a
// reconcile inside a transaction cannot block on SQLite's single connection.
func newTunnelUsecase(d tunnelDeps) *coreUsecase.CoreUsecase {
	embedded := agent.NewEmbeddedClient(d.Embedded)
	client := func(ctx context.Context, nodeID uint) (agent.NodeClient, error) {
		if nodeID != 0 && nodeID == d.NodeUC.LocalNodeID() {
			return embedded, nil
		}
		return d.NodeUC.GetNodeClient(ctx, nodeID)
	}
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
