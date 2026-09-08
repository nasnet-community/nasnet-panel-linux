package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
)

type settingsRepo struct {
	repository.NodeRepository
	inbounds  []*d.Inbound
	outbounds []*d.Outbound
	rules     []*d.RoutingRule
	reverse   []*d.ReverseProxy
	refs      []*d.ReverseProxy
	balancers []*d.BalancingRule
	lookupErr error
	saved     bool
}

func (r *settingsRepo) ListInboundsByNode(context.Context, uint) ([]*d.Inbound, error) {
	return r.inbounds, nil
}
func (r *settingsRepo) ListOutboundsByNode(context.Context, uint) ([]*d.Outbound, error) {
	return r.outbounds, nil
}
func (r *settingsRepo) ListRoutingRulesByNode(context.Context, uint) ([]*d.RoutingRule, error) {
	return r.rules, nil
}
func (r *settingsRepo) ListReverseProxiesByNode(context.Context, uint) ([]*d.ReverseProxy, error) {
	return r.reverse, nil
}
func (r *settingsRepo) ListReverseProxiesByReferencedTag(context.Context, uint, string) ([]*d.ReverseProxy, error) {
	return r.refs, r.lookupErr
}
func (r *settingsRepo) ListBalancingRulesByNode(context.Context, uint) ([]*d.BalancingRule, error) {
	return r.balancers, nil
}
func (r *settingsRepo) GetInbound(context.Context, uint) (*d.Inbound, error) {
	return &d.Inbound{ID: 1, NodeID: 10, Tag: "old"}, nil
}
func (r *settingsRepo) GetOutbound(context.Context, uint) (*d.Outbound, error) {
	return &d.Outbound{ID: 1, NodeID: 10, Tag: "old"}, nil
}
func (r *settingsRepo) GetInboundWithNode(ctx context.Context, id uint) (*d.Inbound, error) {
	return r.GetInbound(ctx, id)
}
func (r *settingsRepo) GetOutboundWithNode(ctx context.Context, id uint) (*d.Outbound, error) {
	return r.GetOutbound(ctx, id)
}
func (r *settingsRepo) UpdateInbound(context.Context, *d.Inbound) error           { r.saved = true; return nil }
func (r *settingsRepo) UpdateOutbound(context.Context, *d.Outbound) error         { r.saved = true; return nil }
func (r *settingsRepo) UpdateReverseProxy(context.Context, *d.ReverseProxy) error { return nil }
func (r *settingsRepo) CreateRoutingRule(_ context.Context, rule *d.RoutingRule) error {
	rule.ID = uint(len(r.rules) + 1)
	r.rules = append(r.rules, rule)
	return nil
}
func (r *settingsRepo) ReorderRoutingRules(_ context.Context, _ uint, ids []uint) error {
	for priority, id := range ids {
		for _, rule := range r.rules {
			if rule.ID == id {
				rule.Priority = priority
			}
		}
	}
	sort.SliceStable(r.rules, func(i, j int) bool { return r.rules[i].Priority < r.rules[j].Priority })
	return nil
}

func TestPanelSettingsReferencedRenames(t *testing.T) {
	cases := []struct {
		name, kind string
		repo       settingsRepo
		want       string
	}{
		{name: "inbound rule", kind: "inbound", repo: settingsRepo{rules: []*d.RoutingRule{{RuleTag: "route", InboundTags: []string{"old"}}}}, want: "routing rule"},
		{name: "outbound rule", kind: "outbound", repo: settingsRepo{rules: []*d.RoutingRule{{RuleTag: "route", OutboundTag: "old"}}}, want: "routing rule"},
		{name: "bridge", kind: "outbound", repo: settingsRepo{refs: []*d.ReverseProxy{{Tag: "bridge"}}}, want: "reverse proxy"},
		{name: "portal", kind: "inbound", repo: settingsRepo{refs: []*d.ReverseProxy{{Tag: "portal"}}}, want: "reverse proxy"},
		{name: "chain", kind: "outbound", repo: settingsRepo{outbounds: []*d.Outbound{{Tag: "proxy", ProxySettings: &d.ProxySettings{Tag: "old"}}}}, want: "outbound"},
		{name: "socket dialer", kind: "outbound", repo: settingsRepo{outbounds: []*d.Outbound{{Tag: "proxy", SockoptSettings: &d.SockoptSettings{DialerProxy: "old"}}}}, want: "outbound"},
		{name: "balancer selector", kind: "outbound", repo: settingsRepo{balancers: []*d.BalancingRule{{Tag: "pool", OutboundSelectors: []string{"ol"}}}}, want: "selector"},
		{name: "balancer fallback", kind: "outbound", repo: settingsRepo{balancers: []*d.BalancingRule{{Tag: "pool", FallbackTag: "old"}}}, want: "fallback"},
		{name: "loopback", kind: "inbound", repo: settingsRepo{outbounds: []*d.Outbound{{Tag: "loop", LoopbackSettings: &d.LoopbackSettings{InboundTag: "old"}}}}, want: "loopback"},
		{name: "lookup failure", kind: "outbound", repo: settingsRepo{lookupErr: errors.New("database unavailable")}, want: "failed to check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := &nodeUsecase{nodeRepo: &tc.repo}
			var err error
			if tc.kind == "inbound" {
				err = u.UpdateInbound(context.Background(), &d.Inbound{ID: 1, Tag: "new", Protocol: "socks", Port: 1080})
			} else {
				err = u.UpdateOutbound(context.Background(), &d.Outbound{ID: 1, Tag: "new", Protocol: "freedom"})
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q dependency error, got %v", tc.want, err)
			}
			if tc.repo.saved {
				t.Fatal("saved a dangling reference")
			}
		})
	}
	u := &nodeUsecase{nodeRepo: &settingsRepo{}}
	for _, kind := range []string{"inbound", "outbound"} {
		if err := u.rejectReferencedTagRename(context.Background(), 10, "old", kind, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Reverse's own managed rule IDs are regenerated on edit; manual references aren't.
	id := uint(42)
	rp := &d.ReverseProxy{Rule1ID: &id}
	u.nodeRepo = &settingsRepo{rules: []*d.RoutingRule{{ID: id, OutboundTag: "old"}}}
	if err := u.rejectReferencedTagRename(context.Background(), 10, "old", "outbound", rp); err != nil {
		t.Fatal(err)
	}
}
func TestPanelSettingsReservedAndReverseTagCollisions(t *testing.T) {
	repo := &settingsRepo{inbounds: []*d.Inbound{{Tag: "in"}}}
	u := &nodeUsecase{nodeRepo: repo}
	for _, tag := range []string{"direct", "blocked", "api", "IPv4", "direct-bw10", "__panel_bw/fallback", "direct-foreign", "direct-domestic", "direct-foreign-secondary4"} {
		rp := &d.ReverseProxy{Tag: tag, Type: "portal", Domain: "internal.test", InterconnectionTags: []string{"in"}, InboundTags: []string{"in"}}
		if err := u.validateReverseProxy(context.Background(), rp); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("accepted generated tag %q: %v", tag, err)
		}
	}
	repo.reverse = []*d.ReverseProxy{{Tag: "portal"}}
	if err := u.ensureUniqueTag(context.Background(), &d.Inbound{Tag: "portal"}); err == nil {
		t.Fatal("inbound accepted reverse tag")
	}
	if err := u.ensureUniqueOutboundTag(context.Background(), &d.Outbound{Tag: "portal"}); err == nil {
		t.Fatal("outbound accepted reverse tag")
	}
}

func TestPanelSettingsManagedRouterOutboundTags(t *testing.T) {
	u := &nodeUsecase{nodeRepo: &settingsRepo{}, routerMode: true}
	for _, tag := range []string{"direct-foreign", "direct-domestic", "direct-foreign-secondary4"} {
		if err := u.ensureUniqueOutboundTag(context.Background(), &d.Outbound{Tag: tag}); err == nil || !strings.Contains(err.Error(), "managed") {
			t.Fatalf("accepted managed outbound tag %q: %v", tag, err)
		}
	}
}
func TestPanelSettingsReverseGenerationOrdersAndDeletionErrors(t *testing.T) {
	repo := &settingsRepo{rules: []*d.RoutingRule{{ID: 1, Enabled: true, RuleTag: "catch-all", NetworkRules: []string{"tcp", "udp"}, OutboundTag: "direct"}}}
	u := &nodeUsecase{nodeRepo: repo}
	rp := &d.ReverseProxy{Tag: "bridge", Type: "bridge", Domain: "internal.test", InterconnectionTag: "tunnel", OutboundTag: "direct"}
	if err := u.generateReverseProxyRulesWithRepo(context.Background(), repo, rp); err != nil {
		t.Fatal(err)
	}
	if len(repo.rules) != 2 || rp.Rule1ID != nil || repo.rules[0].ID != *rp.Rule2ID || repo.rules[1].ID != 1 {
		t.Fatalf("unsafe order: %v", repo.rules)
	}
	repo.lookupErr = fmt.Errorf("lookup failed")
	if err := u.DeleteInbound(context.Background(), 1); !errors.Is(err, repo.lookupErr) {
		t.Fatalf("inbound deletion ignored lookup failure: %v", err)
	}
	if err := u.DeleteOutbound(context.Background(), 1); !errors.Is(err, repo.lookupErr) {
		t.Fatalf("outbound deletion ignored lookup failure: %v", err)
	}
}
func TestPanelSettingsCustomOptionValidation(t *testing.T) {
	for _, typ := range []string{"int", "str", ""} {
		option := &d.SockoptSettings{CustomSockopt: []d.CustomSockopt{{Type: typ, OptValue: "1"}}}
		if err := ValidateOutbound(&d.Outbound{Tag: "out", Protocol: "freedom", SockoptSettings: option}); err != nil {
			t.Fatal(err)
		}
	}
	for _, option := range []d.CustomSockopt{{Type: "float", OptValue: "1"}, {Type: "int", OptValue: "abc"}, {Type: "str", OptValue: 1}} {
		sock := &d.SockoptSettings{CustomSockopt: []d.CustomSockopt{option}}
		if err := ValidateOutbound(&d.Outbound{Tag: "out", Protocol: "freedom", SockoptSettings: sock}); err == nil {
			t.Fatal("invalid outbound option accepted")
		}
		if err := validateInbound(&d.Inbound{Tag: "in", Protocol: "socks", Port: 1080, SockoptSettings: sock}); err == nil {
			t.Fatal("invalid inbound option accepted")
		}
	}
}

func TestPanelSettingsReverseEndpointValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*settingsRepo)
		want   string
	}{
		{name: "valid"},
		{name: "legacy bridge", change: func(r *settingsRepo) { r.outbounds[0].Protocol = "socks" }, want: "VLESS"},
		{name: "disabled bridge", change: func(r *settingsRepo) { r.outbounds[0].IsDisabled = true }, want: "enabled VLESS"},
		{name: "legacy portal", change: func(r *settingsRepo) { r.inbounds[0].Protocol = "socks" }, want: "VLESS"},
		{name: "missing allow", change: func(r *settingsRepo) { r.outbounds[1].FreedomSettings = nil }, want: "Allow Final Rule"},
		{name: "generated direct", change: func(r *settingsRepo) { r.outbounds = r.outbounds[:1] }, want: "Allow Final Rule"},
		{name: "managed router exit", change: func(r *settingsRepo) { r.reverse[0].OutboundTag = "direct-foreign" }, want: "managed router outbound"},
		{name: "shared bridge", change: func(r *settingsRepo) {
			copy := *r.reverse[0]
			copy.Tag = "second-bridge"
			r.reverse = append(r.reverse, &copy)
		}, want: "already belongs"},
		{name: "shared portal", change: func(r *settingsRepo) {
			copy := *r.reverse[1]
			copy.Tag = "second-portal"
			r.reverse = append(r.reverse, &copy)
		}, want: "already belongs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &settingsRepo{
				inbounds:  []*d.Inbound{{ID: 1, Tag: "tunnel-in", Protocol: "vless"}},
				outbounds: []*d.Outbound{{ID: 1, Tag: "tunnel", Protocol: "vless"}, {ID: 2, Tag: "direct", Protocol: "freedom", FreedomSettings: &d.FreedomSettings{FinalRules: []d.FreedomFinalRule{{Action: " Allow ", IP: []string{"127.0.0.1/32"}}}}}},
				reverse:   []*d.ReverseProxy{{Tag: "bridge", Type: "bridge", InterconnectionTag: "tunnel", OutboundTag: "direct"}, {Tag: "portal", Type: "portal", InterconnectionTags: []string{"tunnel-in"}, InboundTags: []string{"tunnel-in"}}},
			}
			if tc.change != nil {
				tc.change(r)
			}
			u := &nodeUsecase{nodeRepo: r}
			err := u.ensureNoReverseTagCollision(context.Background(), 1, "tunnel", nil, r.outbounds[0])
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
