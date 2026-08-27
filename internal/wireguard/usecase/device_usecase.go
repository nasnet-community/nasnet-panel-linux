package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	wgDomain "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/domain"
	wgRepo "github.com/nasnet-community/nasnet-panel-linux/internal/wireguard/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/wgkey"
)

type NodePusher interface {
	PushFullConfig(ctx context.Context, nodeID uint) error
}
type NodeInboundReader interface {
	GetInboundWithNode(ctx context.Context, inboundID uint) (*nodeDomain.Inbound, error)
	// GetInboundWithNodeAndHosts also loads the presentation hosts, so a peer's
	// pinned host can be resolved into the endpoint its .conf dials.
	GetInboundWithNodeAndHosts(ctx context.Context, inboundID uint) (*nodeDomain.Inbound, error)
}
type SubReader interface {
	FindByID(ctx context.Context, id uint) (*subDomain.Subscription, error)
}

// AccountReader exposes a subscription's directly-attached inbounds (manual
// accounts), so a WG inbound added to a sub outside its plan is still usable.
type AccountReader interface {
	ListBySubscriptionID(ctx context.Context, subID uint) ([]*accountDomain.Account, error)
}

var (
	ErrDeviceCapReached = errors.New("device limit reached for this subscription")
	ErrNoWGServer       = errors.New("subscription has no WireGuard server")
	// ErrConfigUnavailable means the peer predates the stored-private-key column,
	// so its .conf can't be re-rendered — the user must rotate to get a new one.
	ErrConfigUnavailable = errors.New("device has no stored private key")
	// ErrHostUnavailable means the requested host isn't an enabled host of the
	// requested inbound (stale UI, host deleted or disabled mid-flight).
	ErrHostUnavailable = errors.New("wireguard host not available for this inbound")
)

// DeviceConfig is returned on create/rotate — the .conf is delivered ONCE.
type DeviceConfig struct {
	Peer *wgDomain.WGPeer
	Conf string
}

// WGServerOption is one pickable endpoint: an inbound, optionally viewed
// through one of its presentation hosts. An inbound with N enabled hosts yields
// N options (one per host); with none it yields a single direct option.
type WGServerOption struct {
	InboundID uint   `json:"inbound_id"`
	HostID    uint   `json:"host_id"` // 0 = the inbound's own address:port
	NodeName  string `json:"node_name"`
	Country   string `json:"country_code"`
	Label     string `json:"label"`    // host remark, template rendered; "" for direct
	Endpoint  string `json:"endpoint"` // host:port the client will dial
}

// CreateDeviceInput carries the picked endpoint. HostID 0 means "no host
// chosen": the first enabled host is used when the inbound has any, else the
// inbound's own endpoint — so links stay consistent with /sub output.
type CreateDeviceInput struct {
	InboundID uint
	HostID    uint
	Label     string
}

type DeviceUsecase interface {
	ListServers(ctx context.Context, subID uint) ([]WGServerOption, error)
	ListDevices(ctx context.Context, subID uint) ([]*wgDomain.WGPeer, error)
	MaxDevices(ctx context.Context, subID uint) (int, error)
	CreateDevice(ctx context.Context, subID uint, in CreateDeviceInput) (*DeviceConfig, error)
	// DeviceConfig re-renders an existing device's .conf from its stored private
	// key — a re-download, so the device keeps working.
	DeviceConfig(ctx context.Context, subID, deviceID uint) (*DeviceConfig, error)
	RotateDevice(ctx context.Context, subID, deviceID uint) (*DeviceConfig, error)
	RemoveDevice(ctx context.Context, subID, deviceID uint) error
	DeactivateSubscription(ctx context.Context, subID uint) error
	ActivateSubscription(ctx context.Context, subID uint) error
}

type deviceUsecase struct {
	peers    wgRepo.WGPeerRepository
	nodes    NodeInboundReader
	subs     SubReader
	accounts AccountReader
	pusher   NodePusher
}

func NewDeviceUsecase(p wgRepo.WGPeerRepository, n NodeInboundReader, s SubReader, acct AccountReader, push NodePusher) DeviceUsecase {
	return &deviceUsecase{peers: p, nodes: n, subs: s, accounts: acct, pusher: push}
}

// resolveMaxDevices returns the device cap for a subscription. 0 = unlimited.
func (u *deviceUsecase) resolveMaxDevices(sub *subDomain.Subscription) int {
	return sub.MaxDevices
}

// inboundAddress is the inbound's own public address — its override, else the
// node IP it listens on.
func inboundAddress(in *nodeDomain.Inbound) string {
	if in.Address != "" {
		return in.Address
	}
	if in.Node != nil {
		return in.Node.IP
	}
	return ""
}

func clientEndpoint(in *nodeDomain.Inbound) string {
	return fmt.Sprintf("%s:%d", inboundAddress(in), in.Port)
}

// hostEndpoint applies a host's address/port override on top of the inbound.
func hostEndpoint(in *nodeDomain.Inbound, h *nodeDomain.Host) string {
	addr := h.Address
	if addr == "" {
		addr = inboundAddress(in)
	}
	port := in.Port
	if h.Port != nil && *h.Port > 0 {
		port = *h.Port
	}
	return fmt.Sprintf("%s:%d", addr, port)
}

// activeHost finds an enabled host of this inbound by ID. Requires Hosts to be
// loaded (GetInboundWithNodeAndHosts); returns nil when gone or disabled.
func activeHost(in *nodeDomain.Inbound, hostID uint) *nodeDomain.Host {
	for i := range in.Hosts {
		if in.Hosts[i].ID == hostID && !in.Hosts[i].IsDisabled {
			return &in.Hosts[i]
		}
	}
	return nil
}

// resolveHostID validates the picked host against the inbound. A zero request
// means the caller didn't choose one: pin to the highest-priority enabled host
// when the inbound has any (that's the endpoint /sub links advertise), else
// leave the peer unpinned to the inbound's own address.
func resolveHostID(in *nodeDomain.Inbound, requested uint) (*uint, error) {
	if requested == 0 {
		if hosts := in.GetActiveHosts(); len(hosts) > 0 {
			id := hosts[0].ID
			return &id, nil
		}
		return nil, nil
	}
	if h := activeHost(in, requested); h != nil {
		id := h.ID
		return &id, nil
	}
	return nil, ErrHostUnavailable
}

// peerEndpoint is the endpoint a peer's .conf dials: its pinned host when that
// host still exists and is enabled, else the inbound's own endpoint. Falling
// back keeps old configs working after a host is deleted or disabled.
func peerEndpoint(peer *wgDomain.WGPeer, in *nodeDomain.Inbound) string {
	if peer != nil && peer.HostID != nil {
		if h := activeHost(in, *peer.HostID); h != nil {
			return hostEndpoint(in, h)
		}
	}
	return clientEndpoint(in)
}

// renderPeerConf builds a peer's client .conf from its stored keys and the
// inbound's current server settings (endpoint, DNS, MTU may have changed since
// the peer was created — the freshest values win).
func renderPeerConf(peer *wgDomain.WGPeer, in *nodeDomain.Inbound) (string, error) {
	wg := in.GetWireGuardSettingsOrDefault()
	serverPub, err := wgkey.PublicKey(wg.SecretKey)
	if err != nil {
		return "", fmt.Errorf("derive server public key: %w", err)
	}
	return buildClientConf(clientConfParams{
		PrivateKey: peer.PrivateKey, Address: peer.AssignedIP, DNS: wg.ClientDNS, MTU: wg.MTU,
		ServerPublicKey: serverPub, PresharedKey: peer.PresharedKey, Endpoint: peerEndpoint(peer, in),
	}), nil
}

// ownedPeer loads a peer and asserts it belongs to the given subscription, so a
// public-panel caller can't touch another sub's device by guessing an ID.
func (u *deviceUsecase) ownedPeer(ctx context.Context, subID, deviceID uint) (*wgDomain.WGPeer, error) {
	peer, err := u.peers.FindByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if peer.SubscriptionID != subID {
		return nil, ErrNoWGServer
	}
	return peer, nil
}

// wgInboundsForSub resolves a sub's usable WireGuard inbounds (active node,
// enabled) from the inbounds attached to the sub via its account records.
// Deduped by inbound ID.
func (u *deviceUsecase) wgInboundsForSub(ctx context.Context, subID uint) (*subDomain.Subscription, []*nodeDomain.Inbound, error) {
	sub, err := u.subs.FindByID(ctx, subID)
	if err != nil {
		return nil, nil, err
	}

	var wg []*nodeDomain.Inbound
	seen := map[uint]bool{}
	add := func(in *nodeDomain.Inbound) {
		if in == nil || in.IsDisabled || !strings.EqualFold(in.Protocol, "wireguard") {
			return
		}
		if in.Node == nil || !in.Node.IsActive {
			return
		}
		if seen[in.ID] {
			return
		}
		seen[in.ID] = true
		wg = append(wg, in)
	}

	// WireGuard inbounds are from the sub's account records
	if u.accounts != nil {
		if accts, err := u.accounts.ListBySubscriptionID(ctx, subID); err == nil {
			for _, a := range accts {
				add(a.Inbound)
			}
		}
	}

	if len(wg) == 0 {
		return sub, nil, ErrNoWGServer
	}
	return sub, wg, nil
}

// hostOptionLabel renders a host's remark template with the node facts we have
// here (usage variables aren't available on a server picker, so they resolve to
// empty and any separator they left behind is trimmed).
func hostOptionLabel(in *nodeDomain.Inbound, h *nodeDomain.Host) string {
	if h.Remark == "" {
		return ""
	}
	cc, node := "", ""
	if in.Node != nil {
		cc, node = in.Node.CountryCode, in.Node.Name
	}
	port := in.Port
	if h.Port != nil && *h.Port > 0 {
		port = *h.Port
	}
	label := product.RenderRemark(h.Remark, product.RemarkContext{
		Country:     cc,
		CountryCode: cc,
		Node:        node,
		Port:        port,
		Protocol:    in.Protocol,
		Network:     in.Network,
		Security:    in.Security,
	})
	label = strings.Join(strings.Fields(label), " ")
	return strings.Trim(label, " |-·•/,")
}

func (u *deviceUsecase) ListServers(ctx context.Context, subID uint) ([]WGServerOption, error) {
	_, wg, err := u.wgInboundsForSub(ctx, subID)
	if err != nil {
		return nil, err
	}
	out := make([]WGServerOption, 0, len(wg))
	for _, in := range wg {
		name, cc := "", ""
		if in.Node != nil {
			name, cc = in.Node.Name, in.Node.CountryCode
		}
		// Hosts are the customer-facing endpoints when defined — the inbound's
		// own address is then an internal detail and isn't offered.
		hosts := in.GetActiveHosts()
		if len(hosts) == 0 {
			out = append(out, WGServerOption{
				InboundID: in.ID, NodeName: name, Country: cc, Endpoint: clientEndpoint(in),
			})
			continue
		}
		for i := range hosts {
			out = append(out, WGServerOption{
				InboundID: in.ID,
				HostID:    hosts[i].ID,
				NodeName:  name,
				Country:   cc,
				Label:     hostOptionLabel(in, &hosts[i]),
				Endpoint:  hostEndpoint(in, &hosts[i]),
			})
		}
	}
	return out, nil
}

func (u *deviceUsecase) ListDevices(ctx context.Context, subID uint) ([]*wgDomain.WGPeer, error) {
	return u.peers.ListBySubscription(ctx, subID)
}

// MaxDevices returns the device cap for a subscription, mirroring the limit
// enforced in CreateDevice. Resolves sub-level override → plan → default(1).
func (u *deviceUsecase) MaxDevices(ctx context.Context, subID uint) (int, error) {
	sub, err := u.subs.FindByID(ctx, subID)
	if err != nil {
		return 0, err
	}
	return u.resolveMaxDevices(sub), nil
}

func (u *deviceUsecase) CreateDevice(ctx context.Context, subID uint, input CreateDeviceInput) (*DeviceConfig, error) {
	sub, wgInbounds, err := u.wgInboundsForSub(ctx, subID)
	if err != nil {
		return nil, err
	}

	// Pick the requested server, or the first one when none specified.
	var target *nodeDomain.Inbound
	if input.InboundID == 0 && len(wgInbounds) > 0 {
		target = wgInbounds[0]
	} else {
		for _, in := range wgInbounds {
			if in.ID == input.InboundID {
				target = in
				break
			}
		}
	}
	if target == nil {
		return nil, ErrNoWGServer
	}

	hostID, err := resolveHostID(target, input.HostID)
	if err != nil {
		return nil, err
	}

	count, err := u.peers.CountActiveBySubscription(ctx, subID)
	if err != nil {
		return nil, err
	}
	// 0 = unlimited
	if limit := u.resolveMaxDevices(sub); limit > 0 && int(count) >= limit {
		return nil, ErrDeviceCapReached
	}

	full, err := u.nodes.GetInboundWithNodeAndHosts(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	wg := full.GetWireGuardSettingsOrDefault()
	if wg.SecretKey == "" || wg.PeerPoolCIDR == "" {
		return nil, fmt.Errorf("wireguard inbound %s missing secretKey or peerPoolCidr", full.Tag)
	}
	if _, err := wgkey.PublicKey(wg.SecretKey); err != nil {
		return nil, fmt.Errorf("derive server public key: %w", err)
	}

	label := input.Label
	if label == "" {
		label = fmt.Sprintf("Device %d", count+1)
	}
	peer, err := u.allocateAndCreate(ctx, subID, full.ID, hostID, label, wg.PeerPoolCIDR, wg.WGServerIP())
	if err != nil {
		return nil, err
	}

	if err := u.pusher.PushFullConfig(ctx, full.NodeID); err != nil {
		_ = u.peers.Delete(ctx, peer.ID) // don't leak an IP for a peer that isn't live
		return nil, fmt.Errorf("apply config to node: %w", err)
	}

	conf, err := renderPeerConf(peer, full)
	if err != nil {
		return nil, err
	}
	return &DeviceConfig{Peer: peer, Conf: conf}, nil
}

// DeviceConfig re-renders a device's .conf from the private key kept alongside
// the peer. Unlike RotateDevice this changes nothing server-side, so the config
// the user already installed keeps working.
func (u *deviceUsecase) DeviceConfig(ctx context.Context, subID, deviceID uint) (*DeviceConfig, error) {
	peer, err := u.ownedPeer(ctx, subID, deviceID)
	if err != nil {
		return nil, err
	}
	if peer.PrivateKey == "" {
		return nil, ErrConfigUnavailable
	}
	full, err := u.nodes.GetInboundWithNodeAndHosts(ctx, peer.InboundID)
	if err != nil {
		return nil, err
	}
	conf, err := renderPeerConf(peer, full)
	if err != nil {
		return nil, err
	}
	return &DeviceConfig{Peer: peer, Conf: conf}, nil
}

// allocateAndCreate generates a keypair + IP and persists the peer, retrying on
// the unique-IP constraint (concurrent allocation race).
func (u *deviceUsecase) allocateAndCreate(ctx context.Context, subID, inboundID uint, hostID *uint, label, pool, serverIP string) (*wgDomain.WGPeer, error) {
	const maxAttempts = 8
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		usedSlice, err := u.peers.ListUsedIPs(ctx, inboundID)
		if err != nil {
			return nil, err
		}
		used := make(map[string]bool, len(usedSlice))
		for _, ip := range usedSlice {
			used[ip] = true
		}
		ip, err := nextFreeIP(pool, serverIP, used)
		if err != nil {
			return nil, err
		}
		priv, err := wgkey.GeneratePrivateKey()
		if err != nil {
			return nil, err
		}
		pub, err := wgkey.PublicKey(priv)
		if err != nil {
			return nil, err
		}
		psk, err := wgkey.GeneratePresharedKey()
		if err != nil {
			return nil, err
		}
		peer := &wgDomain.WGPeer{
			SubscriptionID: subID, InboundID: inboundID, HostID: hostID, Label: label,
			PublicKey: pub, PresharedKey: psk, PrivateKey: priv, AssignedIP: ip,
			Status: wgDomain.WGPeerStatusActive,
		}
		if err := u.peers.Create(ctx, peer); err != nil {
			lastErr = err
			if isUniqueViolation(err) {
				continue
			}
			return nil, err
		}
		return peer, nil
	}
	return nil, fmt.Errorf("allocate peer after %d attempts: %w", maxAttempts, lastErr)
}

func isUniqueViolation(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate") || strings.Contains(s, "constraint")
}

func (u *deviceUsecase) RotateDevice(ctx context.Context, subID, deviceID uint) (*DeviceConfig, error) {
	peer, err := u.ownedPeer(ctx, subID, deviceID)
	if err != nil {
		return nil, err
	}
	full, err := u.nodes.GetInboundWithNodeAndHosts(ctx, peer.InboundID)
	if err != nil {
		return nil, err
	}
	priv, err := wgkey.GeneratePrivateKey()
	if err != nil {
		return nil, err
	}
	pub, err := wgkey.PublicKey(priv)
	if err != nil {
		return nil, err
	}
	psk, err := wgkey.GeneratePresharedKey()
	if err != nil {
		return nil, err
	}
	peer.PublicKey, peer.PresharedKey, peer.PrivateKey, peer.Status = pub, psk, priv, wgDomain.WGPeerStatusActive
	if err := u.peers.Update(ctx, peer); err != nil {
		return nil, err
	}
	if err := u.pusher.PushFullConfig(ctx, full.NodeID); err != nil {
		return nil, fmt.Errorf("apply config to node: %w", err)
	}
	conf, err := renderPeerConf(peer, full)
	if err != nil {
		return nil, err
	}
	return &DeviceConfig{Peer: peer, Conf: conf}, nil
}

func (u *deviceUsecase) RemoveDevice(ctx context.Context, subID, deviceID uint) error {
	peer, err := u.ownedPeer(ctx, subID, deviceID)
	if err != nil {
		return err
	}
	full, err := u.nodes.GetInboundWithNode(ctx, peer.InboundID)
	if err != nil {
		return err
	}
	if err := u.peers.Delete(ctx, peer.ID); err != nil {
		return err
	}
	return u.pusher.PushFullConfig(ctx, full.NodeID)
}

func (u *deviceUsecase) setStatusAndPush(ctx context.Context, subID uint, status wgDomain.WGPeerStatus) error {
	peers, err := u.peers.ListBySubscription(ctx, subID)
	if err != nil {
		return err
	}
	if err := u.peers.SetStatusBySubscription(ctx, subID, status); err != nil {
		return err
	}
	nodeIDs := map[uint]bool{}
	for _, p := range peers {
		if full, err := u.nodes.GetInboundWithNode(ctx, p.InboundID); err == nil {
			nodeIDs[full.NodeID] = true
		}
	}
	log := logger.GetLogger()
	for nid := range nodeIDs {
		if err := u.pusher.PushFullConfig(ctx, nid); err != nil {
			log.WithError(err).Warnf("[wireguard] push after status change failed for node %d", nid)
		}
	}
	return nil
}

func (u *deviceUsecase) DeactivateSubscription(ctx context.Context, subID uint) error {
	return u.setStatusAndPush(ctx, subID, wgDomain.WGPeerStatusDisabled)
}

func (u *deviceUsecase) ActivateSubscription(ctx context.Context, subID uint) error {
	return u.setStatusAndPush(ctx, subID, wgDomain.WGPeerStatusActive)
}
