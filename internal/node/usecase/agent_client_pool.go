package usecase

import (
	"strconv"
	"sync"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// agentClientPoolTTL bounds how long a cached agent client is kept alive.
// Shorter = more TLS handshakes; longer = stale creds (cert rotation lag)
// and slower reaction to node IP changes. 5 minutes mirrors the ephemeral
// cert TTL in credCache.
const agentClientPoolTTL = 5 * time.Minute

// retiredCloseGrace backstops the refcount: a caller that never calls Close
// would otherwise pin a retired conn forever. Longer than the longest RPC
// deadline (SelfUpdate/UpdateXrayBinary, 5 min) so the backstop cannot fire
// under an RPC that is still legitimately running.
const retiredCloseGrace = 10 * time.Minute

// agentClientPool caches *agent.Client per node so TLS+gRPC setup
// amortizes (HTTP/2 multiplexes concurrent RPCs). Reverse-mode clients
// are stateless and bypass the pool.
type agentClientPool struct {
	mu      sync.Mutex
	clients map[uint]*pooledAgentEntry
}

type pooledAgentEntry struct {
	client    *agent.Client
	target    string // "ip:port" — evict on node address change
	expiresAt time.Time
	// refs counts borrowers still holding this conn. The conn is closed
	// once it has been retired from the map AND the last borrower has
	// released it — closing it out from under an in-flight RPC kills that
	// RPC with "code = Canceled", which is how outbound tests (up to a
	// 90s budget) died whenever a run straddled the TTL boundary.
	refs    int
	retired bool
}

func newAgentClientPool() *agentClientPool {
	return &agentClientPool{clients: make(map[uint]*pooledAgentEntry)}
}

// pooledAgentClient wraps an *agent.Client so Close releases the caller's
// borrow instead of tearing down the shared connection. Callers keep using
// `defer closeAgentClient(client)`; the pool owns the conn's real lifecycle
// via TTL + Evict, and closes it once nobody is using it.
type pooledAgentClient struct {
	agent.NodeClient
	release func()
	once    sync.Once
}

func (p *pooledAgentClient) Close() error {
	p.once.Do(func() {
		if p.release != nil {
			p.release()
		}
	})
	return nil
}

// borrowLocked hands out a reference to an entry. Caller holds p.mu.
func (p *agentClientPool) borrowLocked(nodeID uint, entry *pooledAgentEntry) agent.NodeClient {
	entry.refs++
	return &pooledAgentClient{
		NodeClient: entry.client,
		release:    func() { p.release(nodeID, entry) },
	}
}

// release drops one borrow, closing the conn if it is retired and idle.
func (p *agentClientPool) release(nodeID uint, entry *pooledAgentEntry) {
	p.mu.Lock()
	entry.refs--
	shouldClose := entry.retired && entry.refs <= 0
	p.mu.Unlock()
	if shouldClose {
		_ = entry.client.Close()
		logger.GetLogger().Debugf("agentClientPool: closed retired client for node %d", nodeID)
	}
}

// retireLocked removes an entry from the map. It closes the conn immediately
// only when nobody is using it; otherwise the last release does, with a timer
// as a backstop against a caller that never closes. Caller holds p.mu.
func (p *agentClientPool) retireLocked(nodeID uint, entry *pooledAgentEntry) {
	delete(p.clients, nodeID)
	if entry.retired {
		return
	}
	entry.retired = true
	if entry.refs <= 0 {
		_ = entry.client.Close()
		return
	}
	time.AfterFunc(retiredCloseGrace, func() {
		p.mu.Lock()
		stillHeld := entry.refs > 0
		entry.refs = 0
		p.mu.Unlock()
		if stillHeld {
			logger.GetLogger().Warnf("agentClientPool: force-closing retired client for node %d still held after %s", nodeID, retiredCloseGrace)
			_ = entry.client.Close()
		}
	})
}

// usableLocked reports whether an entry can still be handed out.
func (e *pooledAgentEntry) usable(target string) bool {
	return e.target == target && time.Now().Before(e.expiresAt) && !e.client.ConnBroken()
}

// get returns a cached client if target matches, TTL hasn't expired and the
// conn is healthy, else dials a fresh one.
//
// The dial runs WITHOUT the pool lock held. It is a blocking mTLS dial with a
// 10s timeout, so holding the lock across it let a single unreachable node
// serialize every other node's agent access: one dead node burned 10s per
// sweep while healthy nodes waited on the mutex, exhausted their own per-node
// budget and reported "context deadline exceeded" without ever sending an RPC.
// Racing callers may now dial the same node concurrently; the loser closes its
// spare conn, which is far cheaper than blocking the fleet.
func (p *agentClientPool) get(node *domain.Node, dial func() (*agent.Client, error)) (agent.NodeClient, error) {
	target := formatAgentTarget(node)

	p.mu.Lock()
	if entry, ok := p.clients[node.ID]; ok {
		if entry.usable(target) {
			client := p.borrowLocked(node.ID, entry)
			p.mu.Unlock()
			return client, nil
		}
		// Target changed, entry expired, or the conn failed underneath us.
		p.retireLocked(node.ID, entry)
	}
	p.mu.Unlock()

	client, err := dial()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	// Another caller may have installed a usable conn while we dialed.
	if entry, ok := p.clients[node.ID]; ok && entry.usable(target) {
		borrowed := p.borrowLocked(node.ID, entry)
		_ = client.Close()
		return borrowed, nil
	}
	if entry, ok := p.clients[node.ID]; ok {
		p.retireLocked(node.ID, entry)
	}
	entry := &pooledAgentEntry{
		client:    client,
		target:    target,
		expiresAt: time.Now().Add(agentClientPoolTTL),
	}
	p.clients[node.ID] = entry
	return p.borrowLocked(node.ID, entry), nil
}

// Evict retires the cached client for a node. Called on node delete, address
// change, or when a caller detects a hard agent-side failure that warrants
// re-establishing the connection. In-flight RPCs are allowed to finish.
func (p *agentClientPool) Evict(nodeID uint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if entry, ok := p.clients[nodeID]; ok {
		p.retireLocked(nodeID, entry)
	}
}

// EvictAll retires every pooled conn so the next RPC does a full handshake.
// Used after a revocation: resumed TLS sessions skip certificate checks.
func (p *agentClientPool) EvictAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, entry := range p.clients {
		p.retireLocked(id, entry)
	}
}

// Shutdown closes every cached connection. Called during master
// shutdown to release FDs promptly instead of waiting for process
// exit, so it does not wait for borrowers.
func (p *agentClientPool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, entry := range p.clients {
		entry.retired = true
		_ = entry.client.Close()
		delete(p.clients, id)
		logger.GetLogger().Debugf("agentClientPool: closed pooled client for node %d", id)
	}
}

// formatAgentTarget produces the "ip:port" string that uniquely
// identifies a node's agent endpoint. Kept in a helper so tests can
// compare exactly what the pool stores.
func formatAgentTarget(node *domain.Node) string {
	return node.IP + ":" + strconv.Itoa(node.AgentPort)
}
