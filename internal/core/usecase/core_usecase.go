// Package usecase holds business logic for the tunnel core subsystem:
// validation, port allocation and the node reconcile push.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/nodebridge"
	mtprotoproto "github.com/nasnet-community/nasnet-panel-linux/internal/core/protocol/mtproto"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// Validation errors, comparable via errors.Is.
var (
	ErrInvalidProtocol  = errors.New("core: invalid protocol")
	ErrInvalidEgress    = errors.New("core: invalid egress mode")
	ErrInvalidPort      = errors.New("core: invalid port")
	ErrPortInUse        = errors.New("core: port already in use on node")
	ErrProtocolDisabled = errors.New("core: protocol disabled on node")
	ErrInvalidSecret    = errors.New("core: invalid secret")
	ErrDuplicateUser    = errors.New("core: user already exists on this inbound")
)

// ensureUniqueUser rejects a second user with the same email or username on
// one inbound (the unique indexes would, with a driver-specific message).
func (u *CoreUsecase) ensureUniqueUser(ctx context.Context, cu *domain.CoreUser) error {
	users, err := u.repo.ListUsersByInbound(ctx, cu.CoreInboundID)
	if err != nil {
		return err
	}
	for _, other := range users {
		if other.ID == cu.ID {
			continue
		}
		if other.Email == cu.Email {
			return fmt.Errorf("%w: email %q", ErrDuplicateUser, cu.Email)
		}
		if other.Username == cu.Username {
			return fmt.Errorf("%w: username %q", ErrDuplicateUser, cu.Username)
		}
	}
	return nil
}

// ExternalPortChecker reports whether a port is already used on a node by
// something outside the core subsystem (e.g. an Xray inbound). Optional.
type ExternalPortChecker func(ctx context.Context, nodeID uint, port int) (bool, error)

// NodeClientResolver returns a pusher for a node's agent (direct or reverse).
type NodeClientResolver func(ctx context.Context, nodeID uint) (CoreStatePusher, error)

// CoreUsecase orchestrates core_inbound / core_user management.
type CoreUsecase struct {
	repo            repository.CoreRepository
	externalPort    ExternalPortChecker
	resolveNode     NodeClientResolver
	statusResolve   NodeStatusResolver
	hostResolve     NodeHostResolver
	nodeNameResolve NodeNameResolver
	userPolicy      nodebridge.UserPolicyResolver
	inboundsChanged func(ctx context.Context, nodeID uint)
	relayed         func(nodeID uint) bool
	pushXray        func(ctx context.Context, nodeID uint) error
}

// WithXrayConfigPusher wires the Xray config push that follows a change to
// the SOCKS inbounds tunnels with egress=xray leave through.
func WithXrayConfigPusher(fn func(ctx context.Context, nodeID uint) error) Option {
	return func(u *CoreUsecase) { u.pushXray = fn }
}

// syncXray pushes the node's Xray config after its tunnel SOCKS inbounds
// changed. The tunnel change is saved either way; a failed push leaves the
// config drift check to retry it.
func (u *CoreUsecase) syncXray(ctx context.Context, nodeID uint) {
	if u.pushXray == nil {
		return
	}
	if err := u.pushXray(ctx, nodeID); err != nil {
		logger.GetLogger().WithError(err).WithField("node_id", nodeID).Warn("[core] Xray config push for tunnel egress failed")
	}
}

// TunnelSocks is the loopback SOCKS inbound one tunnel leaves through.
type TunnelSocks struct {
	InboundID uint
	Port      int
}

// XraySocks lists the SOCKS inbounds the node's Xray config must carry for
// its tunnels with egress=xray. Disabled tunnels keep theirs, so turning one
// on or off doesn't restart Xray.
func (u *CoreUsecase) XraySocks(ctx context.Context, nodeID uint) ([]TunnelSocks, error) {
	inbounds, err := u.repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	var out []TunnelSocks
	for _, in := range inbounds {
		if p := nodebridge.TunnelSocksPort(in); p != 0 {
			out = append(out, TunnelSocks{InboundID: in.ID, Port: p})
		}
	}
	return out, nil
}

// WithRelayedNodes names the nodes whose MTProto egress goes through the
// marked loopback relay (the built-in node in router mode).
func WithRelayedNodes(fn func(nodeID uint) bool) Option {
	return func(u *CoreUsecase) { u.relayed = fn }
}

func (u *CoreUsecase) isRelayed(nodeID uint) bool {
	return u.relayed != nil && u.relayed(nodeID)
}

// Option configures a CoreUsecase.
type Option func(*CoreUsecase)

// WithExternalPortChecker wires a cross-subsystem port-collision check
// (e.g. against Xray inbounds) into inbound creation/update.
func WithExternalPortChecker(fn ExternalPortChecker) Option {
	return func(u *CoreUsecase) { u.externalPort = fn }
}

// WithInboundsChanged wires a hook run after a node's set of listening core
// inbounds changes (create, update, delete, protocol toggle), so the node's
// firewall and port mapper follow.
func WithInboundsChanged(fn func(ctx context.Context, nodeID uint)) Option {
	return func(u *CoreUsecase) { u.inboundsChanged = fn }
}

func (u *CoreUsecase) notifyInboundsChanged(ctx context.Context, nodeID uint) {
	if u.inboundsChanged != nil {
		u.inboundsChanged(ctx, nodeID)
	}
}

// WithNodeClientResolver wires the agent-client lookup used to push core state
// to a node's agent after a CRUD change.
func WithNodeClientResolver(fn NodeClientResolver) Option {
	return func(u *CoreUsecase) { u.resolveNode = fn }
}

// ReconcileNodeByID resolves the node's agent client and pushes the full core
// state. Best-effort: returns an error the caller may surface, but the DB
// change that triggered it has already been persisted.
func (u *CoreUsecase) ReconcileNodeByID(ctx context.Context, nodeID uint) error {
	if u.resolveNode == nil {
		return nil
	}
	pusher, err := u.resolveNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("resolve node %d client: %w", nodeID, err)
	}
	return u.ReconcileNode(ctx, pusher, nodeID)
}

// NewCoreUsecase builds a CoreUsecase.
func NewCoreUsecase(repo repository.CoreRepository, opts ...Option) *CoreUsecase {
	u := &CoreUsecase{repo: repo}
	for _, o := range opts {
		o(u)
	}
	return u
}

// CreateInbound validates and persists a new core_inbound. It defaults an empty
// egress mode to "direct" and rejects protocol/egress/port errors and port
// collisions (within the core subsystem and, if configured, externally).
func (u *CoreUsecase) CreateInbound(ctx context.Context, in *domain.CoreInbound) error {
	if in.EgressMode == "" {
		in.EgressMode = domain.EgressDirect
	}
	if err := u.validate(in); err != nil {
		return err
	}
	disabled, err := u.repo.DisabledProtocolsForNode(ctx, in.NodeID)
	if err != nil {
		return err
	}
	if disabled[in.Protocol] {
		return fmt.Errorf("%w: node=%d protocol=%s", ErrProtocolDisabled, in.NodeID, in.Protocol)
	}
	if err := u.checkPortFree(ctx, in.NodeID, in.Port, 0); err != nil {
		return err
	}
	if err := prepareInboundSettings(in, u.isRelayed(in.NodeID)); err != nil {
		return err
	}
	if err := u.repo.CreateInbound(ctx, in); err != nil {
		return err
	}
	// A tunnel's loopback ports derive from its ID, known only now.
	if err := u.checkLoopbackPorts(ctx, in); err != nil {
		if delErr := u.repo.DeleteInbound(ctx, in.ID); delErr != nil {
			return errors.Join(err, delErr)
		}
		return err
	}
	if nodebridge.TunnelSocksPort(in) != 0 {
		u.syncXray(ctx, in.NodeID)
	}
	u.notifyInboundsChanged(ctx, in.NodeID)
	return nil
}

func (u *CoreUsecase) validate(in *domain.CoreInbound) error {
	if !in.Protocol.IsValid() {
		return fmt.Errorf("%w: %q", ErrInvalidProtocol, in.Protocol)
	}
	if !in.EgressMode.IsValid() {
		return fmt.Errorf("%w: %q", ErrInvalidEgress, in.EgressMode)
	}
	if in.Port < 1 || in.Port > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidPort, in.Port)
	}
	return nil
}

// UpdateInbound validates and persists changes to an existing core_inbound.
// Protocol and node are immutable (changing either would orphan
// protocol-specific settings or the listener's node placement); port collisions
// are re-checked excluding the inbound itself, and settings preparation re-runs
// idempotently (it only fills generated material — host keys, PKI — when absent,
// so an edit never clobbers existing secrets).
func (u *CoreUsecase) UpdateInbound(ctx context.Context, in *domain.CoreInbound) error {
	existing, err := u.repo.FindInboundByID(ctx, in.ID)
	if err != nil {
		return err
	}
	// Protocol and node placement are immutable across an update.
	in.Protocol = existing.Protocol
	in.NodeID = existing.NodeID
	if in.EgressMode == "" {
		in.EgressMode = domain.EgressDirect
	}
	if err := u.validate(in); err != nil {
		return err
	}
	if err := u.checkPortFree(ctx, in.NodeID, in.Port, in.ID); err != nil {
		return err
	}
	if err := u.checkLoopbackPorts(ctx, in); err != nil {
		return err
	}
	if err := prepareInboundSettings(in, u.isRelayed(in.NodeID)); err != nil {
		return err
	}
	if err := u.repo.UpdateInbound(ctx, in); err != nil {
		return err
	}
	if nodebridge.TunnelSocksPort(existing) != nodebridge.TunnelSocksPort(in) {
		u.syncXray(ctx, in.NodeID)
	}
	u.notifyInboundsChanged(ctx, in.NodeID)
	return nil
}

// UpdateUser persists changes to an existing core_user. The email/username/
// k_limit/enabled fields are mutable; the secret is preserved when the caller
// leaves it blank (it is never returned, so an edit form cannot echo it back).
func (u *CoreUsecase) UpdateUser(ctx context.Context, cu *domain.CoreUser) error {
	existing, err := u.repo.FindUserByID(ctx, cu.ID)
	if err != nil {
		return err
	}
	cu.CoreInboundID = existing.CoreInboundID
	// Enabled is owned by SetUserEnabled (the instant-toggle endpoint); a profile
	// edit must never silently flip it via a zero-value bool.
	cu.Enabled = existing.Enabled
	if cu.Username == "" {
		cu.Username = existing.Username
	}
	if cu.Secret == "" {
		cu.Secret = existing.Secret
	}
	if cu.Email == "" {
		cu.Email = existing.Email
	}
	if cu.KLimit < 1 {
		cu.KLimit = 1
	}
	in, err := u.repo.FindInboundByID(ctx, cu.CoreInboundID)
	if err != nil {
		return err
	}
	if err := normalizeUserSecret(in, cu); err != nil {
		return err
	}
	if err := u.ensureUniqueUser(ctx, cu); err != nil {
		return err
	}
	return u.repo.UpdateUser(ctx, cu)
}

// SetUserEnabled flips a core_user's enabled flag and returns the updated user.
// Disabling denies new dials and sweep-kills live sessions on the next reconcile.
func (u *CoreUsecase) SetUserEnabled(ctx context.Context, id uint, enabled bool) (*domain.CoreUser, error) {
	cu, err := u.repo.FindUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	cu.Enabled = enabled
	if err := u.repo.UpdateUser(ctx, cu); err != nil {
		return nil, err
	}
	return cu, nil
}

// GetInbound returns one core_inbound.
func (u *CoreUsecase) GetInbound(ctx context.Context, id uint) (*domain.CoreInbound, error) {
	return u.repo.FindInboundByID(ctx, id)
}

// ListInbounds returns a node's core_inbounds.
func (u *CoreUsecase) ListInbounds(ctx context.Context, nodeID uint) ([]*domain.CoreInbound, error) {
	return u.repo.ListInboundsByNode(ctx, nodeID)
}

// ListAllInbounds returns every core_inbound across all nodes (admin cross-node
// overview). Users are preloaded; Secret never serializes.
func (u *CoreUsecase) ListAllInbounds(ctx context.Context) ([]*domain.CoreInbound, error) {
	return u.repo.ListAllInbounds(ctx)
}

// DeleteInbound removes a core_inbound and its users.
func (u *CoreUsecase) DeleteInbound(ctx context.Context, id uint) error {
	in, err := u.repo.FindInboundByID(ctx, id)
	if err != nil {
		return err
	}
	if err := u.repo.DeleteInbound(ctx, id); err != nil {
		return err
	}
	if nodebridge.TunnelSocksPort(in) != 0 {
		u.syncXray(ctx, in.NodeID)
	}
	u.notifyInboundsChanged(ctx, in.NodeID)
	return nil
}

// ListUsers returns an inbound's core_users.
func (u *CoreUsecase) ListUsers(ctx context.Context, inboundID uint) ([]*domain.CoreUser, error) {
	return u.repo.ListUsersByInbound(ctx, inboundID)
}

// GetUser returns one core_user.
func (u *CoreUsecase) GetUser(ctx context.Context, id uint) (*domain.CoreUser, error) {
	return u.repo.FindUserByID(ctx, id)
}

// DeleteUser removes a core_user.
func (u *CoreUsecase) DeleteUser(ctx context.Context, id uint) error {
	return u.repo.DeleteUser(ctx, id)
}

// NodeProtocolState is the per-node enable state of one core protocol, with the
// count of inbounds of that protocol currently on the node.
type NodeProtocolState struct {
	Protocol     domain.CoreProtocol `json:"protocol"`
	Enabled      bool                `json:"enabled"`
	InboundCount int                 `json:"inbound_count"`
}

// ListNodeProtocols returns the enable state of every core protocol on a node,
// in stable display order. Absent settings default to enabled.
func (u *CoreUsecase) ListNodeProtocols(ctx context.Context, nodeID uint) ([]NodeProtocolState, error) {
	disabled, err := u.repo.DisabledProtocolsForNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	inbounds, err := u.repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	counts := map[domain.CoreProtocol]int{}
	for _, in := range inbounds {
		counts[in.Protocol]++
	}
	protos := domain.AllCoreProtocols()
	out := make([]NodeProtocolState, 0, len(protos))
	for _, p := range protos {
		out = append(out, NodeProtocolState{Protocol: p, Enabled: !disabled[p], InboundCount: counts[p]})
	}
	return out, nil
}

// ToggleNodeProtocol flips a node's per-protocol enable flag and returns the new
// state. The caller reconciles the node so listeners come up / tear down.
func (u *CoreUsecase) ToggleNodeProtocol(ctx context.Context, nodeID uint, protoStr string) (domain.CoreProtocol, bool, error) {
	proto := domain.CoreProtocol(protoStr)
	if !proto.IsValid() {
		return "", false, fmt.Errorf("%w: %q", ErrInvalidProtocol, protoStr)
	}
	disabled, err := u.repo.DisabledProtocolsForNode(ctx, nodeID)
	if err != nil {
		return "", false, err
	}
	// currently disabled -> enable; currently enabled -> disable.
	newEnabled := disabled[proto]
	if err := u.repo.SetNodeProtocolEnabled(ctx, nodeID, proto, newEnabled); err != nil {
		return "", false, err
	}
	u.notifyInboundsChanged(ctx, nodeID)
	return proto, newEnabled, nil
}

// NodePorts lists every port the node's tunnel inbounds hold: each listener,
// plus an MTProto inbound's loopback relay and metrics ports.
func (u *CoreUsecase) NodePorts(ctx context.Context, nodeID uint) ([]int, error) {
	inbounds, err := u.repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return inboundPorts(inbounds, 0), nil
}

func inboundPorts(inbounds []*domain.CoreInbound, excludeInboundID uint) []int {
	ports := make([]int, 0, len(inbounds))
	for _, in := range inbounds {
		if in.ID == excludeInboundID {
			continue
		}
		ports = append(ports, in.Port)
		ports = append(ports, loopbackPorts(in)...)
	}
	return ports
}

// loopbackPorts are the 127.0.0.1 ports a tunnel holds besides its listener:
// an MTProto inbound's relay (or Xray SOCKS) and metrics ports, an SSH
// inbound's Xray SOCKS port with egress=xray.
func loopbackPorts(in *domain.CoreInbound) []int {
	switch in.Protocol {
	case domain.CoreProtoMTProto:
		if st, err := mtprotoproto.ParseSettings(in.Settings); err == nil {
			relay, metrics := st.LoopbackPorts(in.ID)
			return []int{relay, metrics}
		}
	case domain.CoreProtoSSH:
		if p := nodebridge.TunnelSocksPort(in); p != 0 {
			return []int{p}
		}
	}
	return nil
}

// checkLoopbackPorts refuses a tunnel whose loopback ports (127.0.0.1,
// base+id or pinned in settings) are its own listener or each other, another
// tunnel's port, or held by something else on the node.
func (u *CoreUsecase) checkLoopbackPorts(ctx context.Context, in *domain.CoreInbound) error {
	if in.ID == 0 {
		return nil
	}
	if in.Protocol == domain.CoreProtoMTProto {
		if _, err := mtprotoproto.ParseSettings(in.Settings); err != nil {
			return err
		}
	}
	ports := loopbackPorts(in)
	for i, p := range ports {
		if p == in.Port || slices.Contains(ports[:i], p) {
			return fmt.Errorf("%w: node=%d: the loopback ports %v must differ from each other and from the listener", ErrPortInUse, in.NodeID, ports)
		}
	}
	if len(ports) == 0 {
		return nil
	}
	inbounds, err := u.repo.ListInboundsByNode(ctx, in.NodeID)
	if err != nil {
		return err
	}
	taken := inboundPorts(inbounds, in.ID)
	for _, p := range ports {
		if p < 1 || p > 65535 {
			return fmt.Errorf("%w: %d", ErrInvalidPort, p)
		}
		for _, t := range taken {
			if t == p {
				return fmt.Errorf("%w: node=%d port=%d (this inbound's loopback port)", ErrPortInUse, in.NodeID, p)
			}
		}
		if u.externalPort != nil {
			used, err := u.externalPort(ctx, in.NodeID, p)
			if err != nil {
				return err
			}
			if used {
				return fmt.Errorf("%w (external): node=%d port=%d (this inbound's loopback port)", ErrPortInUse, in.NodeID, p)
			}
		}
	}
	return nil
}

func (u *CoreUsecase) checkPortFree(ctx context.Context, nodeID uint, port int, excludeInboundID uint) error {
	inbounds, err := u.repo.ListInboundsByNode(ctx, nodeID)
	if err != nil {
		return err
	}
	for _, p := range inboundPorts(inbounds, excludeInboundID) {
		if p == port {
			return fmt.Errorf("%w: node=%d port=%d", ErrPortInUse, nodeID, port)
		}
	}
	if u.externalPort != nil {
		extUsed, err := u.externalPort(ctx, nodeID, port)
		if err != nil {
			return err
		}
		if extUsed {
			return fmt.Errorf("%w (external): node=%d port=%d", ErrPortInUse, nodeID, port)
		}
	}
	return nil
}
