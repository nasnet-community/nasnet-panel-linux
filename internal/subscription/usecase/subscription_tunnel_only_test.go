package usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
)

// seedSub stores an active manual subscription; tunnel-only unless accounts are added.
func (f *manualFixture) seedSub(t *testing.T, over func(s *domain.Subscription)) *domain.Subscription {
	t.Helper()
	now := time.Now()
	sub := &domain.Subscription{
		IsManual: true, ProductType: "xray", Status: domain.SubscriptionStatusActive,
		Label: "mtproto friend", StartDate: &now, DataUsed: 5, DataLimit: 100,
		ConfigID: "old-uuid", ConfigEmail: "manual_0_old", LinkKey: "key-1",
	}
	if over != nil {
		over(sub)
	}
	f.subRepo.seedSubscription(sub)
	return sub
}

func (f *manualFixture) addXrayAccount(sub *domain.Subscription) {
	in := f.nodeRepo.inbounds[10]
	subID := sub.ID
	f.accounts.accounts[sub.ID] = append(f.accounts.accounts[sub.ID], &accountDomain.Account{
		InboundID: in.ID, Inbound: in, Email: sub.ConfigEmail, UUID: sub.ConfigID, SubscriptionID: &subID,
	})
}

var mtprotoArtifacts = []*coreUC.ClientArtifact{
	{Protocol: "mtproto", Kind: "link", Content: "tg://proxy?server=a&port=443&secret=s1\n tg://proxy?server=a&port=443&secret=s2 \n"},
	{Protocol: "ssh", Kind: "link", Content: "ssh://u:p@b:22"},
}

// ─── raw /sub config ───────────────────────────────────────────────────────

func TestGetSubscriptionConfig_TunnelOnlyServesTunnelLinks(t *testing.T) {
	f := newManualFixture(t)
	end := time.Now().Add(48 * time.Hour)
	f.seedSub(t, func(s *domain.Subscription) { s.EndDate = &end })
	f.core.artifacts = mtprotoArtifacts

	res, err := f.uc.GetSubscriptionConfig(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetSubscriptionConfig: %v", err)
	}
	want := "tg://proxy?server=a&port=443&secret=s1\ntg://proxy?server=a&port=443&secret=s2\nssh://u:p@b:22"
	if res.Config != want {
		t.Errorf("config = %q, want %q", res.Config, want)
	}
	if res.DataUsed != 5 || res.DataLimit != 100 || res.PlanName != "mtproto friend" || res.ExpiresAt == nil || !res.ExpiresAt.Equal(end) {
		t.Errorf("result = %+v", res)
	}
}

// Xray-only and mixed subs keep their Xray links and nothing else.
func TestGetSubscriptionConfig_XrayLinksUntouched(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprintf("mixed=%v", mixed), func(t *testing.T) {
			f := newManualFixture(t)
			sub := f.seedSub(t, nil)
			f.addXrayAccount(sub)
			if mixed {
				f.core.artifacts = mtprotoArtifacts
			}
			res, err := f.uc.GetSubscriptionConfig(context.Background(), "key-1")
			if err != nil {
				t.Fatalf("GetSubscriptionConfig: %v", err)
			}
			if res.Config != "vless://vless-in" {
				t.Errorf("config = %q", res.Config)
			}
			if f.core.artifactCalls != 0 {
				t.Errorf("tunnel links looked up %d times", f.core.artifactCalls)
			}
		})
	}
}

func TestGetSubscriptionConfig_NothingToServe(t *testing.T) {
	cases := map[string]func(t *testing.T, f *manualFixture){
		"no xray, no tunnels": func(t *testing.T, f *manualFixture) { f.seedSub(t, nil) },
		// Same rule as the sub page: no tunnel links once access has run out.
		"tunnels but expired": func(t *testing.T, f *manualFixture) {
			past := time.Now().Add(-time.Hour)
			f.seedSub(t, func(s *domain.Subscription) { s.EndDate = &past })
			f.core.artifacts = mtprotoArtifacts
		},
		"tunnels but quota used": func(t *testing.T, f *manualFixture) {
			f.seedSub(t, func(s *domain.Subscription) { s.DataUsed = 100 })
			f.core.artifacts = mtprotoArtifacts
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newManualFixture(t)
			setup(t, f)
			if _, err := f.uc.GetSubscriptionConfig(context.Background(), "key-1"); !errors.Is(err, ErrNoNodesAvailable) {
				t.Fatalf("err = %v, want ErrNoNodesAvailable", err)
			}
		})
	}
}

// ─── RegenerateUUID ────────────────────────────────────────────────────────

func TestRegenerateUUID_TunnelOnly(t *testing.T) {
	f := newManualFixture(t)
	sub := f.seedSub(t, nil)

	got, err := f.uc.RegenerateUUID(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("RegenerateUUID: %v", err)
	}
	if got.ConfigID != "uuid-1" || got.ConfigEmail != "manual_0_uuid-1" || f.subRepo.subs[sub.ID].ConfigEmail != "manual_0_uuid-1" {
		t.Errorf("credentials not rotated: %+v", got)
	}
	if len(f.provider.generated) != 1 || len(f.provider.generated[0]) != 0 {
		t.Errorf("generated = %+v", f.provider.generated)
	}
	if f.provider.deactivateCalls != 0 || f.provider.activated != 0 {
		t.Errorf("xray touched: deactivate=%d activate=%d", f.provider.deactivateCalls, f.provider.activated)
	}
	if fmt.Sprint(f.core.rebinds) != "[manual_0_uuid-1]" {
		t.Errorf("tunnel rebinds = %v", f.core.rebinds)
	}
}

func TestRegenerateUUID_XrayUnchanged(t *testing.T) {
	f := newManualFixture(t)
	sub := f.seedSub(t, nil)
	f.addXrayAccount(sub)

	if _, err := f.uc.RegenerateUUID(context.Background(), sub.ID); err != nil {
		t.Fatalf("RegenerateUUID: %v", err)
	}
	if f.provider.deactivateCalls != 1 || f.provider.activated != 1 {
		t.Errorf("deactivate=%d activate=%d, want 1/1", f.provider.deactivateCalls, f.provider.activated)
	}
	if len(f.provider.generated) != 1 || len(f.provider.generated[0]) != 1 {
		t.Errorf("generated = %+v", f.provider.generated)
	}
	if fmt.Sprint(f.accounts.created) != "[10]" || fmt.Sprint(f.core.rebinds) != "[manual_0_uuid-1]" {
		t.Errorf("accounts=%v rebinds=%v", f.accounts.created, f.core.rebinds)
	}
}

// ─── the rest of a tunnel-only sub's lifecycle ─────────────────────────────

// These already reached the tunnels; kept as a guard.
func TestTunnelOnlyLifecycleReachesTunnels(t *testing.T) {
	future := time.Now().Add(240 * time.Hour)
	cases := map[string]struct {
		seed func(s *domain.Subscription)
		act  func(f *manualFixture, id uint) error
		want func(c *fakeCoreAssigner) bool
	}{
		"cancel": {
			act:  func(f *manualFixture, id uint) error { return f.uc.Cancel(context.Background(), id) },
			want: func(c *fakeCoreAssigner) bool { return c.disabled == 1 },
		},
		"expire by date": {
			act: func(f *manualFixture, id uint) error {
				f.subRepo.expiredSubs = []*domain.Subscription{f.subRepo.subs[id]}
				return f.uc.CheckAndExpireSubscriptions(context.Background())
			},
			want: func(c *fakeCoreAssigner) bool { return c.disabled == 1 },
		},
		"pause or revoke": {
			act:  func(f *manualFixture, id uint) error { return f.uc.SetTunnelAccess(context.Background(), id, false) },
			want: func(c *fakeCoreAssigner) bool { return c.disabled == 1 },
		},
		"resume": {
			act:  func(f *manualFixture, id uint) error { return f.uc.SetTunnelAccess(context.Background(), id, true) },
			want: func(c *fakeCoreAssigner) bool { return c.enabled == 1 },
		},
		"renew past expiry": {
			seed: func(s *domain.Subscription) { s.Status = domain.SubscriptionStatusExpired },
			act: func(f *manualFixture, id uint) error {
				_, err := f.uc.SetCustomEndDate(context.Background(), id, &future, true)
				return err
			},
			want: func(c *fakeCoreAssigner) bool { return c.enabled == 1 },
		},
		"add data when used up": {
			seed: func(s *domain.Subscription) { s.Status = domain.SubscriptionStatusTrafficExhausted; s.DataUsed = 100 },
			act:  func(f *manualFixture, id uint) error { return f.uc.AddData(context.Background(), id, 1) },
			want: func(c *fakeCoreAssigner) bool { return c.enabled == 1 },
		},
		"reset data": {
			act:  func(f *manualFixture, id uint) error { return f.uc.ResetDataUsed(context.Background(), id) },
			want: func(c *fakeCoreAssigner) bool { return c.reset == 1 },
		},
		"device limit": {
			act:  func(f *manualFixture, id uint) error { return f.uc.SetMaxDevices(context.Background(), id, 3) },
			want: func(c *fakeCoreAssigner) bool { return fmt.Sprint(c.kLimits) == "[3]" },
		},
		"delete": {
			act:  func(f *manualFixture, id uint) error { return f.uc.DeleteCoreTunnels(context.Background(), id) },
			want: func(c *fakeCoreAssigner) bool { return c.deleted == 1 },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newManualFixture(t)
			sub := f.seedSub(t, tc.seed)
			if err := tc.act(f, sub.ID); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if !tc.want(f.core) {
				t.Errorf("%s: tunnels = %+v", name, f.core)
			}
		})
	}
}
