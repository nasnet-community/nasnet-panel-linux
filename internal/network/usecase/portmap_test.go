package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/nft"
)

func pmTestUsecase(fake *system.FakePortMapper) *networkUsecase {
	return NewNetworkUsecase(Deps{
		RouterMode: true, PortMap: fake, Nft: nft.NewManager(&nft.FakeApplier{}),
	}).(*networkUsecase)
}

func pmTestWAN() system.PortMapWAN {
	return system.PortMapWAN{
		IfName:  "eth0",
		Gateway: netip.AddrFrom4([4]byte{192, 168, 1, 1}),
		SelfIP:  netip.AddrFrom4([4]byte{192, 168, 1, 34}),
	}
}

func okProbe() system.PortMapProbe {
	return system.PortMapProbe{PMP: true, ExternalIP: netip.AddrFrom4([4]byte{203, 0, 113, 7}),
		Epoch: system.PortMapEpoch{Secs: 100, At: time.Now()}, SeenAt: time.Now()}
}

func TestDesiredPortMaps(t *testing.T) {
	inbounds := []InboundSpec{
		{Tag: "wg", Proto: "udp", Port: 51820, Enabled: true},
		{Tag: "off", Proto: "tcp", Port: 8443, Enabled: false},
		{Tag: "dup", Proto: "udp", Port: 51820, Enabled: true},
	}
	rules := []domain.PortMapRule{
		{Proto: "tcp", Port: 2222, ExternalHint: 22022, Enabled: true, Comment: "ssh"},
		{Proto: "tcp", Port: 9999, Enabled: false},
		{Proto: "udp", Port: 4444, Enabled: true, UplinkKey: "mac:other"},
		{Proto: "udp", Port: 51820, Enabled: true}, // shadowed by the inbound
	}
	got := desiredPortMaps(inbounds, rules, "mac:this")
	want := map[pmLeaseKey]string{
		{Proto: "udp", Port: 51820}: "wg",
		{Proto: "tcp", Port: 2222}:  "ssh",
	}
	if len(got) != len(want) {
		t.Fatalf("desired: %+v", got)
	}
	for _, d := range got {
		if want[d.key] != d.source {
			t.Fatalf("desired %+v, want sources %v", got, want)
		}
		if d.key.Port == 2222 && d.hint != 22022 {
			t.Fatalf("hint lost: %+v", d)
		}
	}
}

func TestReconcileAcquiresAndReleases(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	in := pmInputs{wan: wan, key: "mac:aa", enabled: true,
		desired: []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}}

	u.reconcilePortMapWAN(context.Background(), in)
	if _, held := fake.Held(wan, "udp", 51820); !held {
		t.Fatal("lease not acquired")
	}
	st := u.portMapStateFor("mac:aa")
	if st.verdict != PortMapVerdictOK {
		t.Fatalf("verdict = %s (%s)", st.verdict, st.lastErr)
	}

	before := fake.Probes()
	u.reconcilePortMapWAN(context.Background(), in)
	if fake.Probes() != before {
		t.Fatal("probe not cached")
	}

	in.desired = nil
	u.reconcilePortMapWAN(context.Background(), in)
	if _, held := fake.Held(wan, "udp", 51820); held {
		t.Fatal("stale lease kept")
	}
	if fake.Unmaps() == 0 {
		t.Fatal("release never sent")
	}
}

func TestReconcileVerdicts(t *testing.T) {
	wan := pmTestWAN()
	des := []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}

	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: false, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictDisabled {
		t.Fatalf("verdict = %s", v)
	}

	u = pmTestUsecase(&system.FakePortMapper{})
	pub := wan
	pub.SelfIP = netip.AddrFrom4([4]byte{203, 0, 113, 50})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: pub, key: "k", enabled: true, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictPublicDirect {
		t.Fatalf("verdict = %s", v)
	}

	u = pmTestUsecase(&system.FakePortMapper{ProbeRes: system.PortMapProbe{SeenAt: time.Now()}})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictNoService {
		t.Fatalf("verdict = %s", v)
	}

	u = pmTestUsecase(&system.FakePortMapper{ProbeRes: system.PortMapProbe{Denied: true, SeenAt: time.Now()}})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictDenied {
		t.Fatalf("verdict = %s", v)
	}

	u = pmTestUsecase(&system.FakePortMapper{ProbeRes: okProbe(), MapErr: system.ErrPortMapDenied})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictDenied {
		t.Fatalf("verdict = %s", v)
	}

	u = pmTestUsecase(&system.FakePortMapper{ProbeRes: okProbe(),
		GrantIP: netip.AddrFrom4([4]byte{100, 64, 7, 7})})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})
	if v := u.portMapStateFor("k").verdict; v != PortMapVerdictNestedNAT {
		t.Fatalf("verdict = %s", v)
	}
}

func TestReconcileGatewayChangeDropsLeases(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	des := []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})

	moved := wan
	moved.Gateway = netip.AddrFrom4([4]byte{192, 168, 99, 1})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: moved, key: "k", enabled: true, desired: des})
	st := u.portMapStateFor("k")
	if st.gateway != "192.168.99.1" {
		t.Fatalf("gateway not adopted: %s", st.gateway)
	}
	if _, held := fake.Held(moved, "udp", 51820); !held {
		t.Fatal("lease not re-acquired after gateway move")
	}
}

func TestReleaseAllPortMaps(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true,
		desired: []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}})
	u.releaseAllPortMaps(context.Background())
	if len(fake.Mapped) != 0 {
		t.Fatalf("leases survive release-all: %+v", fake.Mapped)
	}
}

func TestPublicV4(t *testing.T) {
	for addr, want := range map[string]bool{
		"203.0.113.7": true, "192.168.1.5": false, "10.0.0.1": false,
		"100.64.0.9": false, "100.127.255.1": false, "127.0.0.1": false,
		"169.254.1.1": false, "0.0.0.0": false,
	} {
		if got := publicV4(netip.MustParseAddr(addr)); got != want {
			t.Fatalf("publicV4(%s) = %v", addr, got)
		}
	}
	if publicV4(netip.MustParseAddr("2001:db8::1")) {
		t.Fatal("v6 counted as public v4")
	}
}

func TestMapErrorKeepsUnexpiredLease(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	des := []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})

	st := u.portMapStateFor("k")
	h := st.held[pmLeaseKey{Proto: "udp", Port: 51820}]
	h.lease.RenewAfter = time.Now().Add(-time.Minute)
	st.held[pmLeaseKey{Proto: "udp", Port: 51820}] = h
	fake.MapErr = errors.New("transient")
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})
	if _, ok := u.portMapStateFor("k").held[pmLeaseKey{Proto: "udp", Port: 51820}]; !ok {
		t.Fatal("unexpired lease dropped on a transient renew failure")
	}
}

func TestKickOnPortMapToggle(t *testing.T) {
	u := pmTestUsecase(&system.FakePortMapper{})
	select {
	case <-u.portMapKickCh():
	default:
	}
	cfg := DefaultHealthConfig()
	cfg.PortMapEnabled = true
	u.SetHealthConfig(cfg)
	select {
	case <-u.portMapKickCh():
	case <-time.After(time.Second):
		t.Fatal("toggle did not kick the loop")
	}
	// Same value again: no kick.
	u.SetHealthConfig(cfg)
	select {
	case <-u.portMapKickCh():
		t.Fatal("no-op config change kicked the loop")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestStartPortMapLoopReleasesOnShutdown(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true,
		desired: []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}})

	ctx, cancel := context.WithCancel(context.Background())
	u.StartPortMapLoop(ctx, time.Hour)
	cancel()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fake.Unmaps() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("shutdown did not release the leases")
}

func TestDeriveFilterInputPortMapRules(t *testing.T) {
	f := DeriveFilterInput(FilterInputSpec{
		Uplinks: []Uplink{{IfName: "eth0", Key: "mac:aa"}, {IfName: "eth1", Key: "mac:bb"}},
		PortMapRules: []domain.PortMapRule{
			{Proto: "udp", Port: 4444, Enabled: true, UplinkKey: "mac:bb"},
			{Proto: "tcp", Port: 5555, Enabled: false},
		},
	})
	var hit int
	for _, a := range f.Accepts {
		if a.Comment == "upstream port mapping" {
			hit++
			if a.Port != 4444 || len(a.IfNames) != 1 || a.IfNames[0] != "eth1" {
				t.Fatalf("accept: %+v", a)
			}
		}
	}
	if hit != 1 {
		t.Fatalf("accepts: %+v", f.Accepts)
	}
}

func TestOnInboundsChangedKicksPortMap(t *testing.T) {
	u := pmTestUsecase(&system.FakePortMapper{})
	select {
	case <-u.portMapKickCh():
	default:
	}
	_ = u.OnInboundsChanged(context.Background())
	select {
	case <-u.portMapKickCh():
	case <-time.After(time.Second):
		t.Fatal("inbound change did not kick the mapper")
	}
}

// fakePortMapRepo is the minimal in-memory rule store.
type fakePortMapRepo struct {
	rows   []domain.PortMapRule
	nextID uint
}

func (f *fakePortMapRepo) List(context.Context) ([]domain.PortMapRule, error) {
	return append([]domain.PortMapRule(nil), f.rows...), nil
}
func (f *fakePortMapRepo) Create(_ context.Context, r *domain.PortMapRule) error {
	f.nextID++
	r.ID = f.nextID
	f.rows = append(f.rows, *r)
	return nil
}
func (f *fakePortMapRepo) Update(_ context.Context, r *domain.PortMapRule) error {
	for i := range f.rows {
		if f.rows[i].ID == r.ID {
			f.rows[i] = *r
			return nil
		}
	}
	return fmt.Errorf("not found")
}
func (f *fakePortMapRepo) Delete(_ context.Context, id uint) error {
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows = append(f.rows[:i], f.rows[i+1:]...)
			return nil
		}
	}
	return nil
}

func TestPortMapRuleCRUD(t *testing.T) {
	repo := &fakePortMapRepo{}
	u := NewNetworkUsecase(Deps{
		RouterMode: true, PortMap: &system.FakePortMapper{}, PortMapRepo: repo,
		PanelPort: 9761, Nft: nft.NewManager(&nft.FakeApplier{}),
	}).(*networkUsecase)

	select {
	case <-u.portMapKickCh():
	default:
	}
	vs, err := u.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "udp", Port: 4444, Enabled: true}, false)
	if err != nil || len(vs) != 0 {
		t.Fatalf("create: %v %+v", err, vs)
	}
	select {
	case <-u.portMapKickCh():
	case <-time.After(time.Second):
		t.Fatal("create did not kick the mapper")
	}

	// Panel port needs a typed CONFIRM first.
	_, err = u.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "tcp", Port: 9761, Enabled: true}, false)
	if !errors.Is(err, ErrConfirmRequired) {
		t.Fatalf("want ErrConfirmRequired, got %v", err)
	}
	if _, err = u.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "tcp", Port: 9761, Enabled: true}, true); err != nil {
		t.Fatalf("confirmed create: %v", err)
	}

	vs, err = u.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "icmp", Port: 1, Enabled: true}, false)
	if !errors.Is(err, ErrValidationFailed) || len(vs) == 0 {
		t.Fatalf("want validation failure, got %v %+v", err, vs)
	}

	rows, _ := u.ListPortMapRules(context.Background())
	if len(rows) != 2 {
		t.Fatalf("rows: %+v", rows)
	}
	if err := u.DeletePortMapRule(context.Background(), rows[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestPortMapStatusReflectsState(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "mac:aa", enabled: true,
		desired: []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}})

	view, err := u.PortMapStatus(context.Background())
	if err != nil || view == nil {
		t.Fatalf("status: %v", err)
	}

	st := u.portMapStateFor("mac:aa")
	wv := u.portMapWANView("mac:aa", "eth0", "Domestic", st, nil)
	if wv.Verdict != PortMapVerdictOK || len(wv.Leases) != 1 {
		t.Fatalf("view: %+v", wv)
	}
	l := wv.Leases[0]
	if l.Source != "wg" || l.InternalPort != 51820 || l.ExternalIP != "203.0.113.7" {
		t.Fatalf("lease view: %+v", l)
	}
	if l.Warning != "" {
		t.Fatalf("same-port lease must carry no warning: %+v", l)
	}

	// A differing external port warns: links still advertise the old one.
	fake.GrantPort = 60001
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "mac:bb", enabled: true,
		desired: []pmDesired{{key: pmLeaseKey{Proto: "udp", Port: 51820}, source: "wg"}}})
	wv = u.portMapWANView("mac:bb", "eth0", "", u.portMapStateFor("mac:bb"), nil)
	if wv.Leases[0].Warning == "" {
		t.Fatal("port mismatch must warn")
	}
}

func pmDesire(proto string, port uint16, source string) []pmDesired {
	return []pmDesired{{key: pmLeaseKey{Proto: proto, Port: port}, source: source}}
}

func TestRenewalCarriesTheNonceAndTheGrantedPort(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe(), GrantPort: 60001}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	in := pmInputs{wan: wan, key: "k", enabled: true, desired: pmDesire("udp", 51820, "wg")}
	u.reconcilePortMapWAN(context.Background(), in)

	key := pmLeaseKey{Proto: "udp", Port: 51820}
	st := u.portMapStateFor("k")
	h := st.held[key]
	if h.lease.Nonce == ([12]byte{}) {
		t.Fatal("the fake granted no nonce to renew with")
	}
	h.lease.RenewAfter = time.Now().Add(-time.Minute)
	st.held[key] = h

	u.reconcilePortMapWAN(context.Background(), in)
	last := fake.Requests[len(fake.Requests)-1]
	if !last.Renewal {
		t.Fatal("a renewal was sent as a fresh mapping")
	}
	if last.Nonce != h.lease.Nonce {
		t.Fatal("PCP answers NOT_AUTHORIZED to a renewal carrying a new nonce")
	}
	if last.ExternalHint != 60001 {
		t.Fatalf("a renewal must ask for the port clients already have, asked %d", last.ExternalHint)
	}
}

func TestRebootedUpstreamRebuildsItsMappings(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	in := pmInputs{wan: wan, key: "k", enabled: true, desired: pmDesire("udp", 51820, "wg")}
	u.reconcilePortMapWAN(context.Background(), in)
	if len(u.portMapStateFor("k").held) != 1 {
		t.Fatal("no lease to lose")
	}

	// An hour later the router answers with a *larger* epoch that is still far
	// short of the hour it should have counted: it rebooted and forgot us.
	st := u.portMapStateFor("k")
	st.epoch = system.PortMapEpoch{Secs: 300, At: time.Now().Add(-time.Hour)}
	st.probed = false
	fake.ProbeRes.Epoch = system.PortMapEpoch{Secs: 600, At: time.Now()}
	fake.GrantEpoch = system.PortMapEpoch{Secs: 600, At: time.Now()}

	u.reconcilePortMapWAN(context.Background(), in)
	if len(u.portMapStateFor("k").held) != 1 {
		t.Fatal("the mapping was not re-created after the reboot")
	}
	// Two creates, because the first lease was dropped rather than renewed.
	creates := 0
	for _, r := range fake.Requests {
		if !r.Renewal {
			creates++
		}
	}
	if creates != 2 {
		t.Fatalf("want a fresh create after the reboot, got %d create(s)", creates)
	}
}

func TestPartialVerdictNamesTheGapInsteadOfHidingIt(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	des := append(pmDesire("udp", 51820, "wg"),
		pmDesired{key: pmLeaseKey{Proto: "tcp", Port: 2222}, source: "ssh"})

	// The first mapping lands, the second is refused.
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true,
		desired: pmDesire("udp", 51820, "wg")})
	fake.MapErr = errors.New("router said no")
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})

	st := u.portMapStateFor("k")
	if st.verdict != PortMapVerdictPartial {
		t.Fatalf("one working lease must not report the other as fine: %s", st.verdict)
	}
	wv := u.portMapWANView("k", "eth0", "", st, nil)
	if len(wv.Failures) != 1 || wv.Failures[0].InternalPort != 2222 {
		t.Fatalf("the failed port is missing from the view: %+v", wv.Failures)
	}
	if wv.Error == "" {
		t.Fatal("a partial result with no message tells the operator nothing")
	}
}

func TestDemotedUplinkGivesItsMappingsBack(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "gone", enabled: true,
		desired: pmDesire("udp", 51820, "wg")})

	// The next pass sees a different uplink set: this one is no longer a WAN.
	other := wan
	other.IfName = "eth1"
	u.prunePortMapStates(context.Background(), []pmInputs{{wan: other, key: "still-here"}})

	if fake.Unmaps() == 0 {
		t.Fatal("a demoted uplink left its forwards open on the upstream router")
	}
	u.pmMu.Lock()
	_, still := u.pmState["gone"]
	u.pmMu.Unlock()
	if still {
		t.Fatal("state for a vanished uplink was kept")
	}
}

func TestGatewayMoveReleasesToTheOldRouter(t *testing.T) {
	fake := &system.FakePortMapper{ProbeRes: okProbe()}
	u := pmTestUsecase(fake)
	wan := pmTestWAN()
	des := pmDesire("udp", 51820, "wg")
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: wan, key: "k", enabled: true, desired: des})

	moved := wan
	moved.Gateway = netip.AddrFrom4([4]byte{192, 168, 99, 1})
	u.reconcilePortMapWAN(context.Background(), pmInputs{wan: moved, key: "k", enabled: true, desired: des})

	if len(fake.ReleasedTo) != 1 || fake.ReleasedTo[0] != "192.168.1.1" {
		t.Fatalf("the release went to the wrong router: %v", fake.ReleasedTo)
	}
}

func TestDesiredPortMapsLeavesLocalProxiesAlone(t *testing.T) {
	inbounds := InboundSpecsFor("socks-lan", "socks", 1080, "", true)
	inbounds = append(inbounds, InboundSpecsFor("wg", "wireguard", 51820, "", true)...)
	got := desiredPortMaps(inbounds, nil, "mac:this")
	for _, d := range got {
		if d.key.Port == 1080 {
			t.Fatal("a local proxy must not be handed to the internet automatically")
		}
	}
	if len(got) != 1 || got[0].key.Port != 51820 {
		t.Fatalf("desired: %+v", got)
	}
}

func TestDeriveFilterInputSkipsARuleWhoseUplinkIsGone(t *testing.T) {
	f := DeriveFilterInput(FilterInputSpec{
		Uplinks:      []Uplink{{IfName: "eth0", Key: "mac:aa"}},
		PortMapRules: []domain.PortMapRule{{Proto: "tcp", Port: 2222, Enabled: true, UplinkKey: "mac:vanished"}},
	})
	for _, a := range f.Accepts {
		if a.Port == 2222 {
			t.Fatalf("a rule pinned to a gone uplink opened on %v instead", a.IfNames)
		}
	}
}

func lastRuleset(f *nft.FakeApplier) string {
	if len(f.Applied) == 0 {
		return ""
	}
	return f.Applied[len(f.Applied)-1]
}

type stubInbounds struct{ specs []InboundSpec }

func (s stubInbounds) EnabledInbounds(context.Context) ([]InboundSpec, error) { return s.specs, nil }

// A rule's accept exists to admit traffic the mapper invited. With the mapper
// off nothing is invited, so nothing should be admitted.
func TestFilterInputDropsPortMapAcceptsWhileTheFeatureIsOff(t *testing.T) {
	repo := &fakePortMapRepo{}
	_ = repo.Create(context.Background(), &domain.PortMapRule{Proto: "tcp", Port: 2222, Enabled: true})
	applier := &nft.FakeApplier{}
	u := NewNetworkUsecase(Deps{
		RouterMode: true, PortMap: &system.FakePortMapper{}, PortMapRepo: repo,
		IfRepo: &stubIfRepo{rows: []domain.NetworkInterface{{
			IfName: "eth0", Key: "mac:aa", Role: domain.RoleWAN,
			Slot: domain.SlotDomestic, Present: true,
		}}},
		LANRepo:  &stubLANRepo{cfg: &domain.LANConfig{InputFirewall: true}},
		Inbounds: stubInbounds{specs: []InboundSpec{{Tag: "wg", Proto: "udp", Port: 51820, Enabled: true}}},
		Nft:      nft.NewManager(applier),
	}).(*networkUsecase)

	if err := u.reapplyFilterInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastRuleset(applier), "dport 2222") {
		t.Fatal("the port was opened although upstream mapping is off")
	}
	if strings.Contains(lastRuleset(applier), "dport 5350") {
		t.Fatal("the mapper's reply port is open although it is not asking")
	}

	cfg := DefaultHealthConfig()
	cfg.PortMapEnabled = true
	u.SetHealthConfig(cfg)
	if err := u.reapplyFilterInput(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastRuleset(applier), "dport 2222") {
		t.Fatalf("an invited mapping has no accept, so an armed filter_in drops it:\n%s", lastRuleset(applier))
	}
	// Without this the routers that answer discovery from an ephemeral port
	// look like routers that answer nothing.
	if !strings.Contains(lastRuleset(applier), "dport 5350") {
		t.Fatalf("the mapper's own answers are dropped:\n%s", lastRuleset(applier))
	}
}

// The mapper leaves local proxies alone, so a manual rule is the only way to
// expose one. Rejecting it as "already mapped automatically" would be a lie.
func TestAManualRuleMayStillExposeALocalProxy(t *testing.T) {
	u := NewNetworkUsecase(Deps{
		RouterMode: true, PortMap: &system.FakePortMapper{}, PortMapRepo: &fakePortMapRepo{},
		Inbounds: stubInbounds{specs: InboundSpecsFor("socks-lan", "socks", 1080, "", true)},
		Nft:      nft.NewManager(&nft.FakeApplier{}),
	}).(*networkUsecase)

	if _, err := u.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "tcp", Port: 1080, Enabled: true}, false); err != nil {
		t.Fatalf("a rule for an un-mapped inbound was refused: %v", err)
	}

	// A wireguard inbound *is* mapped automatically, so a rule for it collides.
	u2 := NewNetworkUsecase(Deps{
		RouterMode: true, PortMap: &system.FakePortMapper{}, PortMapRepo: &fakePortMapRepo{},
		Inbounds: stubInbounds{specs: InboundSpecsFor("wg", "wireguard", 51820, "", true)},
		Nft:      nft.NewManager(&nft.FakeApplier{}),
	}).(*networkUsecase)
	if _, err := u2.CreatePortMapRule(context.Background(),
		domain.PortMapRule{Proto: "udp", Port: 51820, Enabled: true}, false); !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("a duplicate of an auto-mapped inbound was allowed: %v", err)
	}
}
