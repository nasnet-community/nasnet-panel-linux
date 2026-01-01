package usecase

import (
	"context"
	"testing"
	"time"

	mntDomain "github.com/nasnet-community/nasnet-panel-linux/internal/maintenance/domain"
)

type fakeSettingGetter struct {
	vals     map[string]string
	setCalls []setCall
}

type setCall struct {
	key, val string
}

func (f *fakeSettingGetter) GetByKey(_ context.Context, key string) (string, error) {
	if v, ok := f.vals[key]; ok {
		return v, nil
	}
	return "", nil
}

func (f *fakeSettingGetter) UpdateMany(_ context.Context, pairs []*SettingPair) error {
	for _, s := range pairs {
		f.vals[s.Key] = s.Value
		f.setCalls = append(f.setCalls, setCall{s.Key, s.Value})
	}
	return nil
}

type fakeNodeGetter struct {
	active map[uint]bool
	msg    map[uint]string
	since  map[uint]*time.Time
}

func newFakeNodeGetter() *fakeNodeGetter {
	return &fakeNodeGetter{
		active: map[uint]bool{},
		msg:    map[uint]string{},
		since:  map[uint]*time.Time{},
	}
}

func (f *fakeNodeGetter) GetNodeMaintenance(_ context.Context, id uint) (bool, string, *time.Time, error) {
	return f.active[id], f.msg[id], f.since[id], nil
}

func (f *fakeNodeGetter) SetNodeMaintenance(_ context.Context, id uint, active bool, message string, since *time.Time) error {
	f.active[id] = active
	f.msg[id] = message
	f.since[id] = since
	return nil
}

type fakeSubGetter struct {
	active map[uint]bool
	msg    map[uint]string
	since  map[uint]*time.Time
	links  map[uint][]uint
}

func newFakeSubGetter() *fakeSubGetter {
	return &fakeSubGetter{
		active: map[uint]bool{},
		msg:    map[uint]string{},
		since:  map[uint]*time.Time{},
		links:  map[uint][]uint{},
	}
}

func (f *fakeSubGetter) GetSubMaintenance(_ context.Context, id uint) (bool, string, *time.Time, error) {
	return f.active[id], f.msg[id], f.since[id], nil
}

func (f *fakeSubGetter) SetSubMaintenance(_ context.Context, id uint, active bool, message string, since *time.Time) error {
	f.active[id] = active
	f.msg[id] = message
	f.since[id] = since
	return nil
}

func (f *fakeSubGetter) GetSubLinkedNodeIDs(_ context.Context, id uint) ([]uint, error) {
	return f.links[id], nil
}

func newUsecaseForTest(g *fakeSettingGetter) *maintenanceUsecase {
	return newMaintenanceUsecase(g, newFakeNodeGetter(), newFakeSubGetter(), nil)
}

func newUsecaseWithDeps(g *fakeSettingGetter, n *fakeNodeGetter, s *fakeSubGetter) *maintenanceUsecase {
	return newMaintenanceUsecase(g, n, s, nil)
}

func TestHydrateFromSettings_SetsAtomicWhenEnabled(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{
		"maintenance_mode_enabled": "true",
		"maintenance_mode_message": "Planned upgrade",
	}}
	uc := newUsecaseForTest(g)

	if err := uc.HydrateFromSettings(context.Background()); err != nil {
		t.Fatalf("HydrateFromSettings: %v", err)
	}

	if !uc.IsGlobalActive() {
		t.Fatal("expected IsGlobalActive()=true after hydration")
	}
	_ = mntDomain.ScopeGlobal // ensure domain import used
}

func TestHydrateFromSettings_InactiveWhenDisabled(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{
		"maintenance_mode_enabled": "false",
	}}
	uc := newUsecaseForTest(g)
	_ = uc.HydrateFromSettings(context.Background())
	if uc.IsGlobalActive() {
		t.Fatal("expected IsGlobalActive()=false when setting is false")
	}
}

func TestHydrateFromSettings_EmptyValueTreatedAsDisabled(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{}}
	uc := newUsecaseForTest(g)
	_ = uc.HydrateFromSettings(context.Background())
	if uc.IsGlobalActive() {
		t.Fatal("expected IsGlobalActive()=false when setting missing")
	}
}

func TestSetGlobal_EnablesAndPersists(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{}}
	var broadcasts []string
	bcast := func(_ context.Context, msg string) error {
		broadcasts = append(broadcasts, msg)
		return nil
	}
	uc := newMaintenanceUsecase(g, newFakeNodeGetter(), newFakeSubGetter(), bcast)

	if err := uc.SetGlobal(context.Background(), true, "Upgrading", true); err != nil {
		t.Fatalf("SetGlobal: %v", err)
	}

	if !uc.IsGlobalActive() {
		t.Fatal("expected atomic active=true after SetGlobal")
	}
	if g.vals["maintenance_mode_enabled"] != "true" {
		t.Fatalf("expected setting enabled=true, got %q", g.vals["maintenance_mode_enabled"])
	}
	if g.vals["maintenance_mode_message"] != "Upgrading" {
		t.Fatalf("expected message persisted, got %q", g.vals["maintenance_mode_message"])
	}
	if g.vals["maintenance_mode_since"] == "" {
		t.Fatal("expected since to be stamped")
	}
	if len(broadcasts) != 1 || broadcasts[0] != "Upgrading" {
		t.Fatalf("expected one broadcast with custom message, got %v", broadcasts)
	}
}

func TestSetGlobal_DisableSkipsBroadcast(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{
		"maintenance_mode_enabled": "true",
	}}
	var broadcasts []string
	bcast := func(_ context.Context, msg string) error {
		broadcasts = append(broadcasts, msg)
		return nil
	}
	uc := newMaintenanceUsecase(g, newFakeNodeGetter(), newFakeSubGetter(), bcast)
	_ = uc.HydrateFromSettings(context.Background())

	if err := uc.SetGlobal(context.Background(), false, "", true); err != nil {
		t.Fatalf("SetGlobal disable: %v", err)
	}
	if uc.IsGlobalActive() {
		t.Fatal("expected atomic inactive after disable")
	}
	if len(broadcasts) != 0 {
		t.Fatalf("expected no broadcast on disable, got %v", broadcasts)
	}
}

func TestSetGlobal_NotifyFalseSkipsBroadcast(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{}}
	var broadcasts []string
	bcast := func(_ context.Context, msg string) error {
		broadcasts = append(broadcasts, msg)
		return nil
	}
	uc := newMaintenanceUsecase(g, newFakeNodeGetter(), newFakeSubGetter(), bcast)

	_ = uc.SetGlobal(context.Background(), true, "No push", false)
	if len(broadcasts) != 0 {
		t.Fatalf("expected no broadcast when notify=false, got %v", broadcasts)
	}
}

func TestSetGlobal_EmptyMessageUsesDefaultForBroadcast(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{}}
	var broadcasts []string
	bcast := func(_ context.Context, msg string) error {
		broadcasts = append(broadcasts, msg)
		return nil
	}
	uc := newMaintenanceUsecase(g, newFakeNodeGetter(), newFakeSubGetter(), bcast)

	_ = uc.SetGlobal(context.Background(), true, "", true)
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if broadcasts[0] == "" {
		t.Fatal("expected non-empty default broadcast message when custom is empty")
	}
}

func TestSetNode_StampsActiveSinceMessage(t *testing.T) {
	n := newFakeNodeGetter()
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, n, newFakeSubGetter())

	if err := uc.SetNode(context.Background(), 42, true, "Patching kernel"); err != nil {
		t.Fatalf("SetNode: %v", err)
	}
	if !n.active[42] {
		t.Fatalf("expected node 42 active, got %+v", n.active)
	}
	if n.msg[42] != "Patching kernel" {
		t.Fatalf("expected message, got %q", n.msg[42])
	}
	if n.since[42] == nil {
		t.Fatal("expected since to be set")
	}
}

func TestSetNode_DisableClearsSince(t *testing.T) {
	n := newFakeNodeGetter()
	n.active[7] = true
	t0 := time.Now().Add(-time.Hour)
	n.since[7] = &t0
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, n, newFakeSubGetter())

	if err := uc.SetNode(context.Background(), 7, false, ""); err != nil {
		t.Fatalf("SetNode disable: %v", err)
	}
	if n.active[7] {
		t.Fatal("expected node 7 active=false")
	}
	if n.since[7] != nil {
		t.Fatalf("expected since cleared, got %v", n.since[7])
	}
}

func TestSetSubscription_StampsActiveSinceMessage(t *testing.T) {
	s := newFakeSubGetter()
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, newFakeNodeGetter(), s)

	if err := uc.SetSubscription(context.Background(), 9, true, "Under review"); err != nil {
		t.Fatalf("SetSubscription: %v", err)
	}
	if !s.active[9] || s.msg[9] != "Under review" || s.since[9] == nil {
		t.Fatalf("expected sub 9 flagged, got %+v", s)
	}
}

func TestResolve_InactiveWhenAllOff(t *testing.T) {
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, newFakeNodeGetter(), newFakeSubGetter())
	id := uint(5)
	s := uc.Resolve(context.Background(), 1, &id, "fallback")
	if s.Active {
		t.Fatalf("expected inactive, got %+v", s)
	}
}

func TestResolve_GlobalShortCircuits(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{
		"maintenance_mode_enabled": "true",
		"maintenance_mode_message": "Global down",
	}}
	sub := newFakeSubGetter()
	sub.active[5] = true
	sub.msg[5] = "Sub down"
	uc := newUsecaseWithDeps(g, newFakeNodeGetter(), sub)
	_ = uc.HydrateFromSettings(context.Background())

	id := uint(5)
	s := uc.Resolve(context.Background(), 1, &id, "fallback")
	if !s.Active || s.Scope != mntDomain.ScopeGlobal || s.Message != "Global down" {
		t.Fatalf("expected global scope with Global down, got %+v", s)
	}
}

func TestResolve_SubscriptionWinsOverNode(t *testing.T) {
	sub := newFakeSubGetter()
	sub.active[5] = true
	sub.msg[5] = "Sub msg"
	sub.links[5] = []uint{99}
	n := newFakeNodeGetter()
	n.active[99] = true
	n.msg[99] = "Node msg"
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, n, sub)

	id := uint(5)
	s := uc.Resolve(context.Background(), 1, &id, "fallback")
	if s.Scope != mntDomain.ScopeSubscription || s.Message != "Sub msg" {
		t.Fatalf("expected subscription scope with Sub msg, got %+v", s)
	}
}

func TestResolve_NodeActiveWhenSubOff(t *testing.T) {
	sub := newFakeSubGetter()
	sub.links[5] = []uint{11, 22}
	n := newFakeNodeGetter()
	n.active[22] = true
	n.msg[22] = "Node 22 down"
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, n, sub)

	id := uint(5)
	s := uc.Resolve(context.Background(), 1, &id, "fallback")
	if s.Scope != mntDomain.ScopeNode || s.Message != "Node 22 down" {
		t.Fatalf("expected node scope from node 22, got %+v", s)
	}
}

func TestResolve_MessageFallbackWhenEmpty(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{
		"maintenance_mode_enabled": "true",
		"maintenance_mode_message": "",
	}}
	uc := newUsecaseWithDeps(g, newFakeNodeGetter(), newFakeSubGetter())
	_ = uc.HydrateFromSettings(context.Background())

	s := uc.Resolve(context.Background(), 1, nil, "DEFAULT")
	if s.Message != "DEFAULT" {
		t.Fatalf("expected fallback DEFAULT, got %q", s.Message)
	}
}

func TestResolve_NilSubIDOnlyChecksGlobal(t *testing.T) {
	sub := newFakeSubGetter()
	sub.active[9] = true
	uc := newUsecaseWithDeps(&fakeSettingGetter{vals: map[string]string{}}, newFakeNodeGetter(), sub)

	s := uc.Resolve(context.Background(), 1, nil, "fallback")
	if s.Active {
		t.Fatalf("expected inactive when subID=nil and global off, got %+v", s)
	}
}

func TestResolve_GlobalActiveSinceIsPointer(t *testing.T) {
	g := &fakeSettingGetter{vals: map[string]string{}}
	uc := newUsecaseWithDeps(g, newFakeNodeGetter(), newFakeSubGetter())
	_ = uc.SetGlobal(context.Background(), true, "Hi", false)

	s := uc.Resolve(context.Background(), 1, nil, "fallback")
	if !s.Active || s.Scope != mntDomain.ScopeGlobal {
		t.Fatalf("expected global active, got %+v", s)
	}
	if s.Since == nil {
		t.Fatal("expected non-nil Since after SetGlobal(true)")
	}
}
