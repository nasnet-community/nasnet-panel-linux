// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import (
	"context"
	"net/netip"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgectl"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/bridgespec"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/jobs"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/model"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/routerfacts"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/subscriptions"
	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/usage"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
)

// What the manager depends on is listed here, each as the few methods it
// actually calls, so that the real thing (named in each comment) and a fake
// both fit.

// Store is the state file. *store.Store is one.
type Store interface {
	Read() model.State
	Update(fn func(*model.State) error) error
	// Restore puts a backup's state in place of an empty install's.
	Restore(data []byte) error
	Writes() uint64
}

// Jobs is what the manager asks of the job engine: to run the node operations
// that take a while, and which job runs on a node, so that a node being
// installed says how far it has got. *jobs.Engine is one.
type Jobs interface {
	Start(spec jobs.Spec, fn func(ctx context.Context, r *jobs.Run) error) (jobs.Job, error)
	Running(nodeID uint) (jobs.Job, bool)
}

// Usage counts what the subscriptions use and answers for it. *usage.Collector
// is one.
type Usage interface {
	Poll(ctx context.Context, nodeID uint, src usage.Source) (usage.Sample, error)
	Node(nodeID uint) usage.NodeUsage
	Subscription(subID, nodeID uint) usage.SubUsage
	SubscriptionTotal(subID uint) usage.SubUsage
	Traffic(nodeID uint) []usage.Point
	Forget(nodeID uint)
	ForgetSubscription(subID uint)
}

// Bridge runs the bridge program. *bridgectl.Supervisor is one. Update starts
// the bridge when it isn't running, so there is no separate Start.
type Bridge interface {
	Update(spec *bridgespec.Spec) error
	Stop()
	Status() bridgectl.Status
}

// Facts reads the router's report. *routerfacts.Reader is one.
type Facts interface {
	Read() (routerfacts.Facts, error)
}

// Client is the narrow set of a node's agent calls that the manager makes: the
// stats the usage poll asks for and the acknowledgement of the traffic it has
// counted, the version, the clients on the entry, and a restart of Xray, which
// the node's loop asks for when the router's address has changed.
// *agent.Client is one.
//
// A slice that does more with a node (pushing a config, updating the agent)
// asks for a wider interface of the client it borrows with Manager.Client, and
// the connector that makes the clients returns something that satisfies it.
type Client interface {
	usage.Source
	subscriptions.Agent
	GetVersion(ctx context.Context) (*agent.VersionInfo, error)
	// RestartXray stops Xray and starts it again, after checking its config
	// first when validateConfig is set.
	RestartXray(ctx context.Context, validateConfig bool) error
	Close() error
}

var _ Client = (*agent.Client)(nil)

// Connector opens a client for a node's agent. The real one is made from the
// CA and the nodes' source address; until it is wired in the hub has a
// placeholder that never connects.
type Connector interface {
	Connect(ctx context.Context, n model.Node) (Client, error)
}

// ConnectorFunc is a function as a Connector.
type ConnectorFunc func(ctx context.Context, n model.Node) (Client, error)

func (f ConnectorFunc) Connect(ctx context.Context, n model.Node) (Client, error) { return f(ctx, n) }

// Resolver turns a node's host name into addresses. *net.Resolver is one, and
// netroles.Addr.Resolver is one that asks from the nodes' source address.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}
