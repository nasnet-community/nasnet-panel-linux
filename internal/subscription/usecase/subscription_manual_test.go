package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"gorm.io/gorm"
)

// ─── fakes for CreateManual ────────────────────────────────────────────────

type manualNodeRepo struct {
	mockNodeRepo
	nodes    map[uint]*nodeDomain.Node
	inbounds map[uint]*nodeDomain.Inbound
	findErr  error
}

func (r *manualNodeRepo) GetNode(_ context.Context, id uint) (*nodeDomain.Node, error) {
	if n, ok := r.nodes[id]; ok {
		return n, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (r *manualNodeRepo) FindInboundsByIDs(_ context.Context, ids []uint) ([]*nodeDomain.Inbound, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	var out []*nodeDomain.Inbound
	for _, id := range ids {
		if in, ok := r.inbounds[id]; ok {
			out = append(out, in)
		}
	}
	return out, nil
}

type manualAccountMgr struct {
	*mockAccountMgr
	created []uint // inbound ids
}

func (m *manualAccountMgr) CreateAccountForSubscription(_ context.Context, inboundID uint, _, _, _, _ string, _ uint, _ int64) error {
	m.created = append(m.created, inboundID)
	return nil
}

// manualSubRepo adds the link-key lookup the shared mock lacks.
type manualSubRepo struct {
	*mockSubscriptionRepo
}

func (r *manualSubRepo) FindByLinkKey(_ context.Context, key string) (*domain.Subscription, error) {
	for _, s := range r.subs {
		if s.LinkKey == key {
			return s, nil
		}
	}
	return nil, fmt.Errorf("subscription not found")
}

type manualProvider struct {
	mockProvider
	generated [][]product.InboundDetail
	activated int
}

// Each call mints the next credentials: uuid-1, uuid-2, ...
func (p *manualProvider) GenerateConfig(_ context.Context, sub *product.SubscriptionInfo, _ string) (*product.ConfigResult, error) {
	p.generated = append(p.generated, sub.Inbounds)
	n := len(p.generated)
	return &product.ConfigResult{ConfigID: fmt.Sprintf("uuid-%d", n), ConfigEmail: fmt.Sprintf("manual_0_uuid-%d", n), ConfigData: "x"}, nil
}

func (p *manualProvider) GenerateClientConfig(_ context.Context, sub *product.SubscriptionInfo) (string, error) {
	var links []string
	for _, in := range sub.Inbounds {
		links = append(links, "vless://"+in.Tag)
	}
	return strings.Join(links, "\n"), nil
}

func (p *manualProvider) ActivateUser(_ context.Context, _ *product.SubscriptionInfo) error {
	p.activated++
	return nil
}

type coreAssignCall struct {
	SubID, InboundID uint
	Email            string
	KLimit           int
}

type fakeCoreAssigner struct {
	inbounds  map[uint]*coreDomain.CoreInbound
	assignErr map[uint]error
	assigned  []coreAssignCall

	artifacts     []*coreUC.ClientArtifact
	artifactCalls int
	rebinds       []string // emails
	enabled       int
	disabled      int
	reset         int
	deleted       int
	kLimits       []int
}

func (f *fakeCoreAssigner) GetInbound(_ context.Context, id uint) (*coreDomain.CoreInbound, error) {
	if in, ok := f.inbounds[id]; ok {
		return in, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeCoreAssigner) AssignSubscriptionToCoreInbound(_ context.Context, subID uint, email string, kLimit int, coreInboundID uint) error {
	if err := f.assignErr[coreInboundID]; err != nil {
		return err
	}
	f.assigned = append(f.assigned, coreAssignCall{SubID: subID, InboundID: coreInboundID, Email: email, KLimit: kLimit})
	return nil
}
func (f *fakeCoreAssigner) RemoveSubscriptionFromCoreInbound(context.Context, uint, uint) error {
	return nil
}
func (f *fakeCoreAssigner) ListSubscriptionCoreAccounts(context.Context, uint) ([]coreUC.SubscriptionCoreAccount, error) {
	return nil, nil
}
func (f *fakeCoreAssigner) GenerateSubscriptionArtifacts(context.Context, uint) ([]*coreUC.ClientArtifact, error) {
	f.artifactCalls++
	return f.artifacts, nil
}
func (f *fakeCoreAssigner) ResetData(context.Context, uint) error { f.reset++; return nil }
func (f *fakeCoreAssigner) EnableUsersBySubscription(context.Context, uint) error {
	f.enabled++
	return nil
}
func (f *fakeCoreAssigner) DisableUsersBySubscription(context.Context, uint) error {
	f.disabled++
	return nil
}
func (f *fakeCoreAssigner) DeleteUsersBySubscription(context.Context, uint) error {
	f.deleted++
	return nil
}
func (f *fakeCoreAssigner) RebindSubscription(_ context.Context, _ uint, email string) error {
	f.rebinds = append(f.rebinds, email)
	return nil
}
func (f *fakeCoreAssigner) SetKLimitBySubscription(_ context.Context, _ uint, k int) error {
	f.kLimits = append(f.kLimits, k)
	return nil
}

var _ CoreAssigner = (*fakeCoreAssigner)(nil)

type manualFixture struct {
	uc       SubscriptionUsecase
	subRepo  *manualSubRepo
	nodeRepo *manualNodeRepo
	accounts *manualAccountMgr
	provider *manualProvider
	core     *fakeCoreAssigner
}

// Node 1 has an Xray inbound (10); node 2 is an MTProto node with one tunnel (20);
// node 3 is inactive and carries tunnel 30.
func newManualFixture(t *testing.T) *manualFixture {
	t.Helper()
	nodes := map[uint]*nodeDomain.Node{
		1: {ID: 1, Name: "Frankfurt", IP: "203.0.113.1", IsActive: true},
		2: {ID: 2, Name: "VPS 1", IP: "203.0.113.2", IsActive: true},
		3: {ID: 3, Name: "Old", IP: "203.0.113.3", IsActive: false},
	}
	nodeRepo := &manualNodeRepo{
		nodes: nodes,
		inbounds: map[uint]*nodeDomain.Inbound{
			10: {ID: 10, NodeID: 1, Node: nodes[1], Tag: "vless-in", Protocol: "vless", Port: 443},
		},
	}
	core := &fakeCoreAssigner{
		inbounds: map[uint]*coreDomain.CoreInbound{
			20: {ID: 20, NodeID: 2, Protocol: coreDomain.CoreProtoMTProto, Port: 443},
			21: {ID: 21, NodeID: 1, Protocol: coreDomain.CoreProtoSSH, Port: 2222},
			30: {ID: 30, NodeID: 3, Protocol: coreDomain.CoreProtoMTProto, Port: 8443},
			31: {ID: 31, NodeID: 2, Protocol: coreDomain.CoreProtoSSH, Port: 22, IsDisabled: true},
		},
		assignErr: map[uint]error{},
	}
	provider := &manualProvider{}
	factory := product.NewProviderFactory()
	factory.Register(provider)
	subRepo := &manualSubRepo{mockSubscriptionRepo: newMockSubscriptionRepo()}
	accounts := &manualAccountMgr{mockAccountMgr: newMockAccountMgr()}
	uc := NewSubscriptionUsecase(
		subRepo, newMockSubUserRepo(), nodeRepo, factory, nil, nil, &mockNodeSyncer{},
		accounts, &mockAccountReader{}, &mockSubTxManager{}, events.NewEventBus(),
	)
	uc.SetCoreAssigner(core)
	return &manualFixture{uc: uc, subRepo: subRepo, nodeRepo: nodeRepo, accounts: accounts, provider: provider, core: core}
}

func (f *manualFixture) assertNothingCreated(t *testing.T) {
	t.Helper()
	if len(f.subRepo.subs) != 0 || len(f.provider.generated) != 0 || len(f.accounts.created) != 0 || len(f.core.assigned) != 0 {
		t.Fatalf("something was created: subs=%d generated=%d accounts=%v tunnels=%v",
			len(f.subRepo.subs), len(f.provider.generated), f.accounts.created, f.core.assigned)
	}
}

// ─── CreateManual ──────────────────────────────────────────────────────────

func TestCreateManual_TunnelOnly(t *testing.T) {
	f := newManualFixture(t)
	sub, warnings, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		Label:          "mtproto friend",
		MaxDevices:     2,
		CoreInboundIDs: []uint{20, 21},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if sub == nil || sub.ID == 0 || sub.ConfigEmail != "manual_0_uuid-1" || !sub.IsManual {
		t.Fatalf("sub = %+v", sub)
	}
	if len(f.accounts.created) != 0 {
		t.Errorf("no Xray accounts expected, got %v", f.accounts.created)
	}
	want := []coreAssignCall{
		{SubID: sub.ID, InboundID: 20, Email: "manual_0_uuid-1", KLimit: 2},
		{SubID: sub.ID, InboundID: 21, Email: "manual_0_uuid-1", KLimit: 2},
	}
	if fmt.Sprint(f.core.assigned) != fmt.Sprint(want) {
		t.Errorf("tunnel users = %+v, want %+v", f.core.assigned, want)
	}
}

func TestCreateManual_XrayOnlyUnchanged(t *testing.T) {
	f := newManualFixture(t)
	sub, warnings, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		Label:      "plain",
		InboundIDs: []uint{10},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
	if sub.ProductType != "xray" || sub.ConfigID != "uuid-1" || sub.Label != "plain" {
		t.Errorf("sub = %+v", sub)
	}
	if len(f.provider.generated) != 1 || len(f.provider.generated[0]) != 1 || f.provider.generated[0][0].Tag != "vless-in" {
		t.Errorf("provisioned = %+v", f.provider.generated)
	}
	if fmt.Sprint(f.accounts.created) != "[10]" {
		t.Errorf("accounts = %v", f.accounts.created)
	}
	if len(f.core.assigned) != 0 {
		t.Errorf("tunnels = %v", f.core.assigned)
	}
}

func TestCreateManual_XrayAndTunnel(t *testing.T) {
	f := newManualFixture(t)
	sub, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		InboundIDs:     []uint{10},
		CoreInboundIDs: []uint{20, 20},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if fmt.Sprint(f.accounts.created) != "[10]" {
		t.Errorf("accounts = %v", f.accounts.created)
	}
	if len(f.core.assigned) != 1 || f.core.assigned[0].InboundID != 20 || f.core.assigned[0].SubID != sub.ID {
		t.Errorf("tunnels = %+v (duplicates must collapse)", f.core.assigned)
	}
}

func TestCreateManual_NothingSelected(t *testing.T) {
	f := newManualFixture(t)
	_, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{})
	if !errors.Is(err, ErrNoManualAccess) {
		t.Fatalf("err = %v, want ErrNoManualAccess", err)
	}
	f.assertNothingCreated(t)
}

func TestCreateManual_BadTunnelCreatesNothing(t *testing.T) {
	for name, ids := range map[string][]uint{
		"unknown":       {20, 99},
		"inactive node": {30},
		"disabled":      {31},
	} {
		t.Run(name, func(t *testing.T) {
			f := newManualFixture(t)
			_, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
				InboundIDs:     []uint{10},
				CoreInboundIDs: ids,
			})
			if !errors.Is(err, ErrInvalidTunnel) {
				t.Fatalf("err = %v, want ErrInvalidTunnel", err)
			}
			f.assertNothingCreated(t)
		})
	}
}

// Xray inbound validation still applies when tunnels are picked too.
func TestCreateManual_BadXrayInboundWithTunnel(t *testing.T) {
	f := newManualFixture(t)
	_, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		InboundIDs:     []uint{404},
		CoreInboundIDs: []uint{20},
	})
	if err == nil || !strings.Contains(err.Error(), "no valid inbounds") {
		t.Fatalf("err = %v, want no valid inbounds", err)
	}
	f.assertNothingCreated(t)
}

// A tunnel that fails after the row exists is reported, not dropped.
func TestCreateManual_TunnelFailureBecomesWarning(t *testing.T) {
	f := newManualFixture(t)
	f.core.assignErr[21] = errors.New("port clash on node")
	sub, warnings, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		CoreInboundIDs: []uint{20, 21},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if sub == nil || len(f.subRepo.subs) != 1 {
		t.Fatalf("subscription must still be returned, sub=%v", sub)
	}
	if len(f.core.assigned) != 1 || f.core.assigned[0].InboundID != 20 {
		t.Errorf("tunnels = %+v", f.core.assigned)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
	for _, want := range []string{"SSH", "2222", "Frankfurt", "port clash on node"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q lacks %q", warnings[0], want)
		}
	}
}

// A node push that is only pending is not a failure.
func TestCreateManual_PendingReconcileIsNotAWarning(t *testing.T) {
	f := newManualFixture(t)
	f.core.assignErr[20] = coreUC.ErrReconcilePending
	_, warnings, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{
		CoreInboundIDs: []uint{20},
	})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
}

// Bad Xray inbounds keep their messages and match ErrInvalidInbound.
func TestCreateManual_BadXrayInboundIsInvalidInbound(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(f *manualFixture)
		ids   []uint
		msg   string
	}{
		"none found": {ids: []uint{404}, msg: "no valid inbounds found for the given IDs"},
		"disabled": {
			setup: func(f *manualFixture) { f.nodeRepo.inbounds[10].IsDisabled = true },
			ids:   []uint{10}, msg: "inbound 10 is disabled",
		},
		"inactive node": {
			setup: func(f *manualFixture) { f.nodeRepo.inbounds[10].Node = f.nodeRepo.nodes[3] },
			ids:   []uint{10}, msg: "inbound 10 belongs to an inactive or missing node",
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newManualFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{InboundIDs: tc.ids})
			if !errors.Is(err, ErrInvalidInbound) || err.Error() != tc.msg {
				t.Fatalf("err = %v, want ErrInvalidInbound %q", err, tc.msg)
			}
			f.assertNothingCreated(t)
		})
	}
}

// A database failure is not the caller's fault.
func TestCreateManual_InboundLookupFailureIsInternal(t *testing.T) {
	f := newManualFixture(t)
	f.nodeRepo.findErr = errors.New("db down")
	_, _, err := f.uc.CreateManual(context.Background(), &ManualSubscriptionRequest{InboundIDs: []uint{10}})
	if err == nil || errors.Is(err, ErrInvalidInbound) || errors.Is(err, ErrInvalidTunnel) {
		t.Fatalf("err = %v, want a plain internal error", err)
	}
}
