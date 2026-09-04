package usecase

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// InboundSpec is one xray inbound as the firewall sees it, supplied by the node
// usecase so an inbound cannot exist without its accept.
type InboundSpec struct {
	Tag     string
	Proto   string
	Port    int
	Enabled bool
	// PortRange is xray's hop range. filter_in ignores it and the mapper cannot
	// map it; it is here so the UI can say "range not mapped".
	PortRange string
	// NoAutoMap marks an inbound that must never be asked for upstream: a
	// local proxy behind NAT is unreachable by accident, not by design.
	NoAutoMap bool
}

// InboundSpecsFor maps a row onto the accepts it needs. Either-transport
// protocols get both: an extra accept is recoverable, a missing one is not.
func InboundSpecsFor(tag, protocol string, port int, portRange string, enabled bool) []InboundSpec {
	if port <= 0 {
		return nil
	}
	var protos []string
	switch strings.ToLower(protocol) {
	case "wireguard", "hysteria", "hysteria2", "tuic":
		protos = []string{"udp"}
	case "shadowsocks", "dokodemo-door", "socks":
		protos = []string{"tcp", "udp"}
	default:
		protos = []string{"tcp"}
	}

	out := make([]InboundSpec, 0, len(protos))
	for _, p := range protos {
		out = append(out, InboundSpec{Tag: tag, Proto: p, Port: port, Enabled: enabled,
			PortRange: portRange, NoAutoMap: localOnlyInbound(protocol)})
	}
	return out
}

// localOnlyInbound names the protocols that serve this box or its LAN. They
// listen without the auth a public endpoint needs, so the upstream mapper
// leaves them alone unless the operator writes a rule by hand.
func localOnlyInbound(protocol string) bool {
	switch strings.ToLower(protocol) {
	case "socks", "http", "dokodemo-door":
		return true
	}
	return false
}

// InboundSource is the seam onto the node usecase's inbound rows.
type InboundSource interface {
	EnabledInbounds(ctx context.Context) ([]InboundSpec, error)
}

// FilterInputSpec is everything the input chain is derived from.
type FilterInputSpec struct {
	Uplinks      []Uplink
	LocalIfNames []string
	PanelPort    int
	// AdvertisedIfName is where clients connect; the panel is open there only.
	AdvertisedIfName string
	Inbounds         []InboundSpec
	PortForwards     []domain.PortForward
	PortMapRules     []domain.PortMapRule
	// PortMapUplinks are the uplinks the mapper is allowed to ask on. Empty
	// while the feature is off.
	PortMapUplinks []string
	// TwoWANNoLAN has no local management path, so the panel opens on every uplink.
	TwoWANNoLAN bool
}

// OnInboundsChanged re-derives filter_in after any inbound edit. Cheap, and the
// alternative is a chain that drifts from what it protects.
func (u *networkUsecase) OnInboundsChanged(ctx context.Context) error {
	if !u.RouterMode {
		return nil
	}
	// The mapper's desired set just moved too.
	u.kickPortMap()
	return u.reapplyFilterInput(ctx)
}

// reapplyFilterInput re-derives the input chain. Off unless the operator armed
// it: the one change in router mode that can lock them out.
func (u *networkUsecase) reapplyFilterInput(ctx context.Context) error {
	lan := u.lanConfig(ctx)
	if lan == nil || !lan.InputFirewall || u.Inbounds == nil {
		// No inbound rows means no way to know what to keep open.
		return u.Nft.Update(ctx, func(rs *nft.Ruleset) { rs.FilterInput = nil })
	}

	uplinks, err := u.uplinks(ctx)
	if err != nil {
		return err
	}
	inbounds, err := u.Inbounds.EnabledInbounds(ctx)
	if err != nil {
		return fmt.Errorf("read inbound rows: %w", err)
	}
	var pfs []domain.PortForward
	if u.PFRepo != nil {
		if pfs, err = u.PFRepo.List(ctx); err != nil {
			return err
		}
	}
	// Only a live mapping needs an accept. With the feature off nothing
	// invites that traffic, so nothing should be listening for it either.
	var pmRules []domain.PortMapRule
	if u.PortMapRepo != nil && u.healthConfigSnapshot().PortMapEnabled {
		if pmRules, err = u.PortMapRepo.List(ctx); err != nil {
			return err
		}
	}

	var pmUplinks []string
	if u.healthConfigSnapshot().PortMapEnabled {
		for _, up := range uplinks {
			pmUplinks = append(pmUplinks, up.IfName)
		}
	}

	local, twoWANNoLAN := u.localIfNames(ctx, lan)
	fi := DeriveFilterInput(FilterInputSpec{
		Uplinks:          uplinks,
		LocalIfNames:     local,
		PanelPort:        u.PanelPort,
		AdvertisedIfName: u.IngressUplinkIfName(),
		Inbounds:         inbounds,
		PortForwards:     pfs,
		PortMapRules:     pmRules,
		PortMapUplinks:   pmUplinks,
		TwoWANNoLAN:      twoWANNoLAN,
	})
	return u.Nft.Update(ctx, func(rs *nft.Ruleset) { rs.FilterInput = fi })
}

// localIfNames lists the always-accepted interfaces, and whether there are none.
func (u *networkUsecase) localIfNames(ctx context.Context, lan *domain.LANConfig) ([]string, bool) {
	names := []string{"lo"}
	if lan != nil && lan.Enabled {
		bridge := lan.BridgeName
		if bridge == "" {
			bridge = system.LANBridgeName
		}
		names = append(names, bridge)
	}
	rows, err := u.IfRepo.GetByRole(ctx, domain.RoleMgmt)
	if err == nil {
		for _, r := range rows {
			if r.Present {
				names = append(names, r.IfName)
			}
		}
	}
	return names, len(names) == 1
}

// DeriveFilterInput builds the input chain from the rows that define the box's
// surface. Nil with no uplink: a drop policy filtering nothing is an outage.
func DeriveFilterInput(spec FilterInputSpec) *nft.FilterInput {
	if len(spec.Uplinks) == 0 {
		return nil
	}
	allUplinks := uplinkNames(spec.Uplinks)
	sort.Strings(allUplinks)

	f := &nft.FilterInput{LocalIfNames: append([]string(nil), spec.LocalIfNames...)}

	if spec.PanelPort > 0 {
		switch {
		case spec.AdvertisedIfName == "":
			// Too widely reachable is recoverable; unreachable is not.
			f.Accepts = append(f.Accepts, nft.InputAccept{
				IfNames: allUplinks, Proto: "tcp", Port: spec.PanelPort,
				Comment: "panel (no advertised uplink recorded — accepted on all)",
			})
		case spec.TwoWANNoLAN:
			// Losing the advertised uplink here leaves no way back in.
			f.Accepts = append(f.Accepts, nft.InputAccept{
				IfNames: allUplinks, Proto: "tcp", Port: spec.PanelPort,
				Comment: "panel (no local network — accepted on every uplink)",
			})
		default:
			f.Accepts = append(f.Accepts, nft.InputAccept{
				IfNames: []string{spec.AdvertisedIfName}, Proto: "tcp", Port: spec.PanelPort,
				Comment: "panel, advertised uplink only",
			})
		}
	}

	seen := map[string]bool{}
	key := func(proto string, port int) string { return proto + ":" + strconv.Itoa(port) }
	for _, ib := range spec.Inbounds {
		if !ib.Enabled || ib.Port <= 0 || seen[key(ib.Proto, ib.Port)] {
			continue
		}
		seen[key(ib.Proto, ib.Port)] = true
		f.Accepts = append(f.Accepts, nft.InputAccept{
			IfNames: allUplinks, Proto: ib.Proto, Port: ib.Port,
			Comment: "xray inbound " + ib.Tag,
		})
	}

	// Box-terminated forwards need an accept; LAN ones go through filter_fwd.
	for _, pf := range spec.PortForwards {
		if !pf.Enabled {
			continue
		}
		ip := net.ParseIP(pf.ToAddr)
		if ip == nil || !ip.IsLoopback() || seen[key(pf.Proto, pf.DPort)] {
			continue
		}
		seen[key(pf.Proto, pf.DPort)] = true
		names := allUplinks
		if pf.UplinkKey != "" {
			for _, u := range spec.Uplinks {
				if u.Key == pf.UplinkKey {
					names = []string{u.IfName}
				}
			}
		}
		f.Accepts = append(f.Accepts, nft.InputAccept{
			IfNames: names, Proto: pf.Proto, Port: pf.DPort,
			Comment: "box-terminated port forward",
		})
	}

	// The mapper's own replies. PortMapRules is empty while the feature is
	// off, which is exactly when this must not be open.
	if len(spec.PortMapUplinks) > 0 {
		f.PortMapReplyIfNames = spec.PortMapUplinks
		f.PortMapReplyPort = system.PortMapLocalPort
	}

	// Manual upstream mappings terminate on the box. Without an accept an armed
	// input firewall drops exactly the traffic the mapping invited.
	for _, r := range spec.PortMapRules {
		if !r.Enabled || seen[key(r.Proto, r.Port)] {
			continue
		}
		names := allUplinks
		if r.UplinkKey != "" {
			names = nil
			for _, u := range spec.Uplinks {
				if u.Key == r.UplinkKey {
					names = []string{u.IfName}
				}
			}
			if len(names) == 0 {
				// Unassigned since this row was written. Opening the port on
				// every uplink is not a safe reading of "only that one".
				continue
			}
		}
		seen[key(r.Proto, r.Port)] = true
		f.Accepts = append(f.Accepts, nft.InputAccept{
			IfNames: names, Proto: r.Proto, Port: r.Port,
			Comment: "upstream port mapping",
		})
	}

	return f
}
