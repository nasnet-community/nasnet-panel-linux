package usecase

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

// The nft sets forwarded LAN traffic is classified against.
const (
	// Destination-address sets, the floor: they catch traffic dnsmasq never
	// resolved — hardcoded IPs, DoH clients, cached answers. Refreshed from
	// upstream, falling back to the embedded database.
	DomesticSetV4 = "ir_v4"
	DomesticSetV6 = "ir_v6"
	// Domain-resolved sets, populated at runtime by dnsmasq's --nftset. The
	// enrichment: what makes domain rules reach LAN clients at all.
	DomainSetV4 = "ir_dom_v4"
	DomainSetV6 = "ir_dom_v6"
	// DomainSetTimeout bounds the domain sets so a long-running box does not
	// accumulate every address it has ever resolved.
	DomainSetTimeout = "24h"

	DomesticCountry = "IR"
	// DomesticSuffix is the DNS suffix routed to the domestic resolver and fed
	// into the domain sets.
	DomesticSuffix = "ir"
)

// IPv6Enabled is the one switch: router mode is IPv4-only and the kernel has
// IPv6 off, so a v6 set could never match. Read it, never decide locally.
const IPv6Enabled = false

// dnsmasq hands LAN clients the same split the uplinks give the box itself.
const (
	DefaultDomesticDNS = system.DefaultDomesticDNS
	DefaultForeignDNS  = system.DefaultForeignDNS
)

// BuildDomesticSets compiles the embedded geoip.dat and declares the
// dnsmasq-populated sets. Without --nftset the geoip layer runs alone.
func BuildDomesticSets(nftSetSupported bool) ([]nft.Set, error) {
	return BuildDomesticSetsFrom(nftSetSupported, nil)
}

// BuildDomesticSetsFrom takes a refreshed v4 list. Empty falls back to the
// embedded database, so a box that never reaches upstream still classifies.
func BuildDomesticSetsFrom(nftSetSupported bool, fetchedV4 []string) ([]nft.Set, error) {
	cs, err := geoip.EmbeddedCIDRSet(DomesticCountry)
	if err != nil {
		return nil, fmt.Errorf("compile domestic ranges: %w", err)
	}
	v4 := cs.V4
	if len(fetchedV4) > 0 {
		v4 = fetchedV4
	}

	sets := []nft.Set{
		{Name: DomesticSetV4, Family: "ipv4_addr", Interval: true, Elements: v4},
	}
	if IPv6Enabled && len(cs.V6) > 0 {
		sets = append(sets, nft.Set{
			Name: DomesticSetV6, Family: "ipv6_addr", Interval: true, Elements: cs.V6,
		})
	}

	if nftSetSupported {
		// Plain with a timeout: intervals cannot carry per-element timeouts.
		sets = append(sets,
			nft.Set{Name: DomainSetV4, Family: "ipv4_addr", Timeout: DomainSetTimeout})
		if IPv6Enabled {
			sets = append(sets,
				nft.Set{Name: DomainSetV6, Family: "ipv6_addr", Timeout: DomainSetTimeout})
		}
	}
	return sets, nil
}

// lanEgressNames is where LAN hosts may leave by, and what gets masqueraded.
// Never the secondary uplink: LAN goes through the pool or nowhere. The tunnels
// need NAT too, or the far end drops our 10.77.0.x source.
func lanEgressNames(uplinks []Uplink, vpn VPNRouteState) []string {
	out := make([]string, 0, len(uplinks)+len(vpn.IfNames))
	for _, u := range uplinks {
		if u.Slot == domain.SlotSecondary {
			continue
		}
		out = append(out, u.IfName)
	}
	return append(out, vpn.IfNames...)
}

// ApplyLANNftState turns the LAN plane on or off. A nil lan disables it but
// leaves connmark and the pins: those serve shaping and DNAT with no LAN.
func ApplyLANNftState(ctx context.Context, m *nft.Manager, lan *domain.LANConfig,
	uplinks []Uplink, sets []nft.Set, vpn VPNRouteState) error {

	return m.Update(ctx, func(rs *nft.Ruleset) {
		if lan == nil || !lan.Enabled {
			rs.Sets = nil
			rs.LANClassify = nil
			rs.Masquerade = nil
			rs.FilterForward = nil
			return
		}

		bridge := lan.BridgeName
		if bridge == "" {
			bridge = system.LANBridgeName
		}
		names := lanEgressNames(uplinks, vpn)

		// The mark is worthless without mangle_post writing it back to conntrack.
		rs.Connmark = true
		rs.Sets = sets
		// Reference only the sets actually declared: a rule naming a missing set
		// aborts the whole nft transaction, taking the working rules with it.
		rs.LANClassify = &nft.LANClassify{BridgeName: bridge}
		for _, s := range sets {
			switch s.Name {
			case DomesticSetV4:
				rs.LANClassify.DomesticV4Set = DomesticSetV4
			case DomesticSetV6:
				rs.LANClassify.DomesticV6Set = DomesticSetV6
			case DomainSetV4:
				rs.LANClassify.DomainV4Set = DomainSetV4
			case DomainSetV6:
				rs.LANClassify.DomainV6Set = DomainSetV6
			}
		}
		// Uplink egress only: a forwarded host must see the real client address.
		rs.Masquerade = names
		rs.FilterForward = &nft.FilterForward{BridgeName: bridge, UplinkNames: names}
	})
}

// poolForeignDNS gives every member its own resolver line. Rendered on
// enable/disable only — a health flap must not restart dnsmasq.
func poolForeignDNS(pool vpnPool) []system.ForeignServer {
	out := make([]system.ForeignServer, 0, len(pool.Tunnels))
	for _, t := range pool.Tunnels {
		server := DefaultForeignDNS
		// The provider put their own resolver in the config because it is the
		// one guaranteed reachable and unfiltered inside their tunnel.
		if t.Config.DNS != "" {
			server = t.Config.DNS
		}
		out = append(out, system.ForeignServer{Server: server, IfName: t.IfName})
	}
	return out
}

// LANDNSConfig maps the uplinks onto dnsmasq's split-DNS servers. nftSetSupported
// must match what BuildDomesticSets got, or dnsmasq fills an undeclared set.
func LANDNSConfig(lan domain.LANConfig, uplinks []Uplink,
	domesticServer string, foreign []system.ForeignServer, domesticSuffix string,
	nftSetSupported bool) system.DNSMasqConfig {

	bridge := lan.BridgeName
	if bridge == "" {
		bridge = system.LANBridgeName
	}
	listen := lan.CIDR
	if ip, _, err := net.ParseCIDR(lan.CIDR); err == nil {
		listen = ip.String()
	}

	c := system.DNSMasqConfig{
		BridgeName: bridge, ListenAddr: listen,
		RangeLow: lan.DHCPRangeLow, RangeHigh: lan.DHCPRangeHigh,
		LeaseHours:     lan.LeaseHours,
		DomesticServer: domesticServer, DomesticSuffix: domesticSuffix,
		Foreign:         foreign,
		NftSetSupported: nftSetSupported,
	}
	if nftSetSupported {
		ds := system.DomainSet{Suffix: domesticSuffix, V4Set: DomainSetV4}
		if IPv6Enabled {
			ds.V6Set = DomainSetV6
		}
		c.DomainSets = []system.DomainSet{ds}
	}
	// Only the domestic side comes from an uplink; the caller decides the rest.
	for _, u := range uplinks {
		if u.Slot == domain.SlotDomestic {
			c.DomesticIfName = u.IfName
		}
	}
	if c.DomesticIfName == "" {
		// No domestic uplink: drop it rather than get the wrong CDN edge back.
		c.DomesticServer, c.DomesticSuffix = "", ""
	}
	return c
}

// LANView is the stored config plus which classification layers are actually
// live, so the UI states the capability instead of claiming it.
type LANView struct {
	domain.LANConfig
	// GeoIPPrefixes is the floor layer's size; DomainLayer reports whether this
	// dnsmasq has --nftset, which is what makes domain rules reach LAN clients.
	GeoIPPrefixes int  `json:"geoip_prefixes"`
	DomainLayer   bool `json:"domain_layer"`
	// ResolverReady is false with no dnsmasq service, so the operator sees it
	// before the apply rather than as a failed one. ResolverRunning separates
	// "install the package" from "it crashed".
	ResolverReady   bool `json:"resolver_ready"`
	ResolverRunning bool `json:"resolver_running"`
	// RangesFetchedAt is nil while the list is still the build's own.
	RangesFetchedAt *time.Time `json:"ranges_fetched_at,omitempty"`
}

func (u *networkUsecase) GetLAN(ctx context.Context) (*LANView, error) {
	if u.LANRepo == nil {
		return nil, fmt.Errorf("no LAN storage configured")
	}
	cfg, err := u.LANRepo.Get(ctx)
	if err != nil {
		return nil, err
	}
	st := u.dnsmasq.Status(ctx)
	v := &LANView{LANConfig: *cfg,
		ResolverReady: st.Installed, ResolverRunning: st.Running}
	if c, err := geoip.LoadCachedRanges(u.rangesCachePath()); err == nil && c != nil {
		v.RangesFetchedAt = &c.FetchedAt
	}
	if sets, nftSetOK, err := u.domesticSets(ctx); err == nil {
		v.DomainLayer = nftSetOK
		for _, s := range sets {
			if s.Name == DomesticSetV4 || s.Name == DomesticSetV6 {
				v.GeoIPPrefixes += len(s.Elements)
			}
		}
	}
	return v, nil
}

// UpdateLAN goes through the two-phase apply: it writes .network files and
// restarts dnsmasq, and no network change happens outside the dead-man.
func (u *networkUsecase) UpdateLAN(ctx context.Context, cfg domain.LANConfig) ([]domain.Verdict, *ApplyView, error) {
	if u.LANRepo == nil {
		return nil, nil, fmt.Errorf("no LAN storage configured")
	}
	stored, err := u.LANRepo.Get(ctx)
	if err != nil {
		return nil, nil, err
	}
	// The operator edits the settings, not the row identity.
	cfg.ID, cfg.NodeID = stored.ID, stored.NodeID
	if cfg.BridgeName == "" {
		cfg.BridgeName = system.LANBridgeName
	}

	rows, err := u.IfRepo.List(ctx)
	if err != nil {
		return nil, nil, err
	}
	verdicts := domain.ValidateLANConfig(cfg, rows, defaultMgmtCIDR)
	if verdicts == nil {
		verdicts = []domain.Verdict{}
	}
	if domain.Rejected(verdicts) {
		return verdicts, nil, nil
	}

	var plan system.Plan
	if !system.TakeoverDone(u.Paths) {
		plan.Ops = append(plan.Ops, system.TakeoverOps(u.Paths)...)
	}
	plan.Ops = append(plan.Ops,
		system.Op{
			Desc: lanOpDesc(cfg),
			Do:   func(ctx context.Context) error { return u.LANRepo.Save(ctx, &cfg) },
		},
		system.Op{
			Desc: "render the bridge, its members, rt_tables and the sysctl drop-in",
			Do:   func(ctx context.Context) error { return u.renderAll(ctx) },
		},
		system.Op{
			Desc: "install routing policy rules, LAN classification, NAT and sysctls",
			Do:   func(ctx context.Context) error { return u.Reconcile(ctx) },
		},
		system.Op{
			Desc: "derive the input firewall from the inbound rows",
			Do:   func(ctx context.Context) error { return u.reapplyFilterInput(ctx) },
		},
	)

	prev := *stored
	rec, err := u.applier.Apply(ctx, plan, !system.TakeoverDone(u.Paths))
	if err != nil {
		// The snapshot does not cover the database, so the row would keep claiming
		// a LAN whose files are gone and every reconcile after it would fail.
		if saveErr := u.LANRepo.Save(ctx, &prev); saveErr != nil {
			return verdicts, nil, fmt.Errorf("%w (and the LAN row still says %v: %v)",
				err, cfg.Enabled, saveErr)
		}
		return verdicts, nil, err
	}
	ops := rec.Ops
	if ops == nil {
		ops = []string{}
	}
	view := &ApplyView{PlanID: rec.ID, Ops: ops}
	if rec.Deadline != nil {
		view.ConfirmDeadlineUnix = rec.Deadline.Unix()
	}
	return verdicts, view, nil
}

func lanOpDesc(cfg domain.LANConfig) string {
	if !cfg.Enabled {
		return "disable the LAN bridge, DHCP and split DNS"
	}
	return fmt.Sprintf("enable the LAN bridge on %s, DHCP and split DNS", cfg.CIDR)
}

func uplinkNames(uplinks []Uplink) []string {
	out := make([]string, 0, len(uplinks))
	for _, u := range uplinks {
		out = append(out, u.IfName)
	}
	return out
}
