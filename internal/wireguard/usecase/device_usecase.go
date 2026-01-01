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
	"github.com/nasnet-community/nasnet-panel-linux/pkg/wgkey"
)

type NodePusher interface {
	PushFullConfig(ctx context.Context, nodeID uint) error
}
type NodeInboundReader interface {
	GetInboundWithNode(ctx context.Context, inboundID uint) (*nodeDomain.Inbound, error)
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
)

// DeviceConfig is returned on create/rotate — the .conf is delivered ONCE.
type DeviceConfig struct {
	Peer *wgDomain.WGPeer
	Conf string
}

type WGServerOption struct {
	InboundID uint   `json:"inbound_id"`
	NodeName  string `json:"node_name"`
	Country   string `json:"country_code"`
}

type DeviceUsecase interface {
	ListServers(ctx context.Context, subID uint) ([]WGServerOption, error)
	ListDevices(ctx context.Context, subID uint) ([]*wgDomain.WGPeer, error)
	MaxDevices(ctx context.Context, subID uint) (int, error)
	CreateDevice(ctx context.Context, subID, inboundID uint, label string) (*DeviceConfig, error)
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

func clientEndpoint(in *nodeDomain.Inbound) string {
	host := ""
	if in.Address != "" {
		host = in.Address
	} else if in.Node != nil {
		host = in.Node.IP
	}
	return fmt.Sprintf("%s:%d", host, in.Port)
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
		out = append(out, WGServerOption{InboundID: in.ID, NodeName: name, Country: cc})
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

func (u *deviceUsecase) CreateDevice(ctx context.Context, subID, inboundID uint, label string) (*DeviceConfig, error) {
	sub, wgInbounds, err := u.wgInboundsForSub(ctx, subID)
	if err != nil {
		return nil, err
	}

	// Pick the requested server, or the first one when none specified.
	var target *nodeDomain.Inbound
	if inboundID == 0 && len(wgInbounds) > 0 {
		target = wgInbounds[0]
	} else {
		for _, in := range wgInbounds {
			if in.ID == inboundID {
				target = in
				break
			}
		}
	}
	if target == nil {
		return nil, ErrNoWGServer
	}

	count, err := u.peers.CountActiveBySubscription(ctx, subID)
	if err != nil {
		return nil, err
	}
	// 0 = unlimited
	if limit := u.resolveMaxDevices(sub); limit > 0 && int(count) >= limit {
		return nil, ErrDeviceCapReached
	}

	full, err := u.nodes.GetInboundWithNode(ctx, target.ID)
	if err != nil {
		return nil, err
	}
	wg := full.GetWireGuardSettingsOrDefault()
	if wg.SecretKey == "" || wg.PeerPoolCIDR == "" {
		return nil, fmt.Errorf("wireguard inbound %s missing secretKey or peerPoolCidr", full.Tag)
	}
	serverPub, err := wgkey.PublicKey(wg.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("derive server public key: %w", err)
	}

	if label == "" {
		label = fmt.Sprintf("Device %d", count+1)
	}
	peer, priv, err := u.allocateAndCreate(ctx, subID, full.ID, label, wg.PeerPoolCIDR, wg.WGServerIP())
	if err != nil {
		return nil, err
	}

	if err := u.pusher.PushFullConfig(ctx, full.NodeID); err != nil {
		_ = u.peers.Delete(ctx, peer.ID) // don't leak an IP for a peer that isn't live
		return nil, fmt.Errorf("apply config to node: %w", err)
	}

	conf := buildClientConf(clientConfParams{
		PrivateKey: priv, Address: peer.AssignedIP, DNS: wg.ClientDNS, MTU: wg.MTU,
		ServerPublicKey: serverPub, PresharedKey: peer.PresharedKey, Endpoint: clientEndpoint(full),
	})
	return &DeviceConfig{Peer: peer, Conf: conf}, nil
}

// allocateAndCreate generates a keypair + IP and persists the peer, retrying on
// the unique-IP constraint (concurrent allocation race).
func (u *deviceUsecase) allocateAndCreate(ctx context.Context, subID, inboundID uint, label, pool, serverIP string) (*wgDomain.WGPeer, string, error) {
	const maxAttempts = 8
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		usedSlice, err := u.peers.ListUsedIPs(ctx, inboundID)
		if err != nil {
			return nil, "", err
		}
		used := make(map[string]bool, len(usedSlice))
		for _, ip := range usedSlice {
			used[ip] = true
		}
		ip, err := nextFreeIP(pool, serverIP, used)
		if err != nil {
			return nil, "", err
		}
		priv, err := wgkey.GeneratePrivateKey()
		if err != nil {
			return nil, "", err
		}
		pub, err := wgkey.PublicKey(priv)
		if err != nil {
			return nil, "", err
		}
		psk, err := wgkey.GeneratePresharedKey()
		if err != nil {
			return nil, "", err
		}
		peer := &wgDomain.WGPeer{
			SubscriptionID: subID, InboundID: inboundID, Label: label,
			PublicKey: pub, PresharedKey: psk, PrivateKey: priv, AssignedIP: ip,
			Status: wgDomain.WGPeerStatusActive,
		}
		if err := u.peers.Create(ctx, peer); err != nil {
			lastErr = err
			if isUniqueViolation(err) {
				continue
			}
			return nil, "", err
		}
		return peer, priv, nil
	}
	return nil, "", fmt.Errorf("allocate peer after %d attempts: %w", maxAttempts, lastErr)
}

func isUniqueViolation(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate") || strings.Contains(s, "constraint")
}

func (u *deviceUsecase) RotateDevice(ctx context.Context, subID, deviceID uint) (*DeviceConfig, error) {
	peer, err := u.peers.FindByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if peer.SubscriptionID != subID {
		return nil, ErrNoWGServer
	}
	full, err := u.nodes.GetInboundWithNode(ctx, peer.InboundID)
	if err != nil {
		return nil, err
	}
	wg := full.GetWireGuardSettingsOrDefault()
	serverPub, err := wgkey.PublicKey(wg.SecretKey)
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
	conf := buildClientConf(clientConfParams{
		PrivateKey: priv, Address: peer.AssignedIP, DNS: wg.ClientDNS, MTU: wg.MTU,
		ServerPublicKey: serverPub, PresharedKey: peer.PresharedKey, Endpoint: clientEndpoint(full),
	})
	return &DeviceConfig{Peer: peer, Conf: conf}, nil
}

func (u *deviceUsecase) RemoveDevice(ctx context.Context, subID, deviceID uint) error {
	peer, err := u.peers.FindByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if peer.SubscriptionID != subID {
		return ErrNoWGServer
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
