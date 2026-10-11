package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/core/repository"
)

// only the one repository method validateForNode reads
type inboundsOnly struct {
	repository.CoreRepository
	byNode map[uint][]*domain.CoreInbound
}

func (r inboundsOnly) ListInboundsByNode(_ context.Context, id uint) ([]*domain.CoreInbound, error) {
	return r.byNode[id], nil
}

func usecaseWith(kind string, existing ...*domain.CoreInbound) *CoreUsecase {
	return NewCoreUsecase(inboundsOnly{byNode: map[uint][]*domain.CoreInbound{1: existing}},
		WithNodeKindResolver(func(context.Context, uint) (string, error) { return kind, nil }))
}

func inbound(egress domain.EgressMode, settings string) *domain.CoreInbound {
	return &domain.CoreInbound{NodeID: 1, Protocol: domain.CoreProtoMTProto, Port: 443, EgressMode: egress, Settings: []byte(settings)}
}

func TestMTProtoNodeRules(t *testing.T) {
	good := `{"tls_domain":"mirror.example.ir"}`
	cases := []struct {
		name string
		u    *CoreUsecase
		in   *domain.CoreInbound
		want error
	}{
		{"an MTProto node tunnel with hub egress and a domain is fine", usecaseWith("lite"), inbound(domain.EgressHub, good), nil},
		{"direct egress on an MTProto node", usecaseWith("lite"), inbound(domain.EgressDirect, good), ErrMTProtoNodeEgress},
		{"xray egress on an MTProto node", usecaseWith("lite"), inbound(domain.EgressXray, good), ErrMTProtoNodeEgress},
		{"ssh on an MTProto node", usecaseWith("lite"), &domain.CoreInbound{NodeID: 1, Protocol: domain.CoreProtoSSH, Port: 22, EgressMode: domain.EgressHub}, ErrMTProtoNodeProtocol},
		{"a second tunnel", usecaseWith("lite", &domain.CoreInbound{ID: 9, NodeID: 1}), inbound(domain.EgressHub, good), ErrMTProtoNodeSingle},
		{"no cover domain", usecaseWith("lite"), inbound(domain.EgressHub, `{}`), ErrMTProtoNodeDomain},
		{"middle proxy", usecaseWith("lite"), inbound(domain.EgressHub, `{"tls_domain":"a.example.ir","middle_proxy":true}`), ErrMTProtoNodeNoMiddle},
		{"ad tag", usecaseWith("lite"), inbound(domain.EgressHub, `{"tls_domain":"a.example.ir","adtag_default":"00"}`), ErrMTProtoNodeNoMiddle},
		{"hub egress on a full node", usecaseWith("full"), inbound(domain.EgressHub, good), ErrHubEgressNotHere},
		{"hub egress with no kind resolver", NewCoreUsecase(inboundsOnly{}), inbound(domain.EgressHub, good), ErrHubEgressNotHere},
		{"direct egress on a full node is untouched", usecaseWith("full"), inbound(domain.EgressDirect, `{}`), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.u.validateForNode(context.Background(), c.in)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
		})
	}
}

// Editing the one tunnel must not trip over itself.
func TestEditingAnMTProtoNodesOnlyTunnelIsAllowed(t *testing.T) {
	existing := &domain.CoreInbound{ID: 5, NodeID: 1}
	in := inbound(domain.EgressHub, `{"tls_domain":"mirror.example.ir"}`)
	in.ID = 5
	if err := usecaseWith("lite", existing).validateForNode(context.Background(), in); err != nil {
		t.Fatal(err)
	}
}

func TestHubEgressIsAKnownMode(t *testing.T) {
	if !domain.EgressHub.IsValid() || domain.EgressMode("nope").IsValid() {
		t.Fatal("egress validity wrong")
	}
}

// A new cover site is checked from the node before the tunnel row is saved.
func TestMTProtoNodeTunnelCoverSiteIsCheckedBeforeSaving(t *testing.T) {
	refused := errors.New("bad.example.ir can't be used as a cover site from this node")
	var asked []string
	check := WithCoverSiteCheck(func(_ context.Context, nodeID uint, domain string) error {
		asked = append(asked, domain)
		if domain == "bad.example.ir" {
			return refused
		}
		return nil
	})
	u := NewCoreUsecase(inboundsOnly{byNode: map[uint][]*domain.CoreInbound{}},
		WithNodeKindResolver(func(context.Context, uint) (string, error) { return "lite", nil }), check)
	if err := u.validateForNode(context.Background(), inbound(domain.EgressHub, `{"tls_domain":" Bad.Example.IR "}`)); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the node's verdict", err)
	}
	if err := u.validateForNode(context.Background(), inbound(domain.EgressHub, `{"tls_domain":"good.example.ir"}`)); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 || asked[0] != "bad.example.ir" {
		t.Fatalf("asked %v", asked)
	}
	// full nodes have no cover site to check
	asked = nil
	full := NewCoreUsecase(inboundsOnly{}, WithNodeKindResolver(func(context.Context, uint) (string, error) { return "full", nil }), check)
	if err := full.validateForNode(context.Background(), inbound(domain.EgressDirect, `{"tls_domain":"bad.example.ir"}`)); err != nil || len(asked) != 0 {
		t.Fatalf("err=%v asked=%v", err, asked)
	}
}

// telemt takes its primary domain from tls_domain, so the extras alone are not enough.
func TestMTProtoNodeTunnelNeedsAPrimaryCoverSite(t *testing.T) {
	err := usecaseWith("lite").validateForNode(context.Background(), inbound(domain.EgressHub, `{"tls_domains_extra":["a.example.ir"]}`))
	if !errors.Is(err, ErrMTProtoNodeDomain) {
		t.Fatalf("err = %v, want ErrMTProtoNodeDomain", err)
	}
}
