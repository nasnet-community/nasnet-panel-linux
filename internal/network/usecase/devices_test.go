package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/system"
)

// stubLANRepo is the smallest LAN row a device list needs.
type stubLANRepo struct{ cfg *domain.LANConfig }

func (s *stubLANRepo) Get(context.Context) (*domain.LANConfig, error) { return s.cfg, nil }
func (s *stubLANRepo) Save(_ context.Context, c *domain.LANConfig) error {
	s.cfg = c
	return nil
}
func (s *stubLANRepo) DisarmInputFirewall(context.Context) error { return nil }

type stubLabels struct {
	rows map[string]string
	set  [2]string
	err  error
}

func (s *stubLabels) ByMAC(context.Context) (map[string]string, error) { return s.rows, s.err }
func (s *stubLabels) Set(_ context.Context, mac, label string) error {
	s.set = [2]string{mac, label}
	return s.err
}

func deviceUsecase(src *system.FakeDeviceSource, labels *stubLabels) *networkUsecase {
	u := &networkUsecase{}
	u.LANRepo = &stubLANRepo{cfg: &domain.LANConfig{
		BridgeName: "lan0", CIDR: "10.77.0.1/24", Enabled: true,
	}}
	u.Devices = src
	if labels != nil {
		u.DeviceLabels = labels
	}
	return u
}

func TestListDevices_MergesTheThreeSources(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing:  300,
		FDBRows: []system.FDBEntry{{MAC: "b8:27:eb:aa:bb:01", Port: "enp4s0", Updated: 5}},
		LeaseRows: []system.Lease{{
			MAC: "b8:27:eb:aa:bb:01", IP: "10.77.0.141", Hostname: "my-nas",
			Expiry: time.Unix(1786480400, 0).UTC(),
		}},
		NeighRows: []system.Neighbour{{MAC: "b8:27:eb:aa:bb:01", IP: "10.77.0.141"}},
	}
	got, err := deviceUsecase(src, &stubLabels{rows: map[string]string{
		"b8:27:eb:aa:bb:01": "the NAS",
	}}).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 1 {
		t.Fatalf("got %d devices: %+v", len(got.Devices), got.Devices)
	}
	d := got.Devices[0]
	switch {
	case !d.Online:
		t.Error("a 5s-old FDB entry against 300s ageing must be online")
	case d.Hostname != "my-nas":
		t.Errorf("hostname = %q", d.Hostname)
	case d.Label != "the NAS":
		t.Errorf("label = %q", d.Label)
	case d.Port != "enp4s0":
		t.Errorf("port = %q", d.Port)
	case d.Vendor != "Raspberry Pi Foundation":
		t.Errorf("vendor = %q", d.Vendor)
	case len(d.IPs) != 1 || d.IPs[0] != "10.77.0.141":
		t.Errorf("IPs = %v, want the address once", d.IPs)
	case d.LeaseExpiry == nil:
		t.Error("no lease expiry")
	}
}

// The bridge decides presence, so losing it means the answer is unknown.
func TestListDevices_FDBFailureIsAnError(t *testing.T) {
	src := &system.FakeDeviceSource{Ageing: 300, FDBErr: errors.New("no such device")}
	if _, err := deviceUsecase(src, nil).ListDevices(context.Background()); err == nil {
		t.Fatal("a bridge read failure was reported as an empty list")
	}
}

// Losing leases costs hostnames, not the list.
func TestListDevices_DegradesPerSource(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing:    300,
		FDBRows:   []system.FDBEntry{{MAC: "b8:27:eb:aa:bb:01", Port: "enp4s0", Updated: 1}},
		LeaseErr:  errors.New("no lease file"),
		NeighRows: []system.Neighbour{{MAC: "b8:27:eb:aa:bb:01", IP: "10.77.0.9"}},
	}
	got, err := deviceUsecase(src, nil).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.LeasesOK {
		t.Error("LeasesOK is true after the lease read failed")
	}
	if !got.NeighboursOK {
		t.Error("NeighboursOK is false though neighbours answered")
	}
	if len(got.Devices) != 1 || got.Devices[0].Hostname != "" {
		t.Errorf("got %+v", got.Devices)
	}
	if len(got.Devices[0].IPs) != 1 {
		t.Errorf("the static address was lost: %+v", got.Devices[0])
	}
}

// A lease is not presence: it outlives the device by up to its whole term.
func TestListDevices_LeaseWithoutAnFDBEntryIsOffline(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing: 300,
		LeaseRows: []system.Lease{{
			MAC: "b8:27:eb:aa:bb:01", IP: "10.77.0.141", Hostname: "gone",
			Expiry: time.Unix(1786480400, 0).UTC(),
		}},
	}
	got, err := deviceUsecase(src, nil).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 1 || got.Devices[0].Online {
		t.Errorf("a lease alone reported the device as present: %+v", got.Devices)
	}
}

// The threshold is the bridge's own ageing time, not a constant.
func TestListDevices_OnlineIsMeasuredAgainstTheBridgeAgeing(t *testing.T) {
	for _, tc := range []struct {
		ageing, age int
		online      bool
	}{
		{300, 299, true},
		{300, 301, false},
		{10, 11, false}, // a shortened ageing must tighten the window
		{10, 5, true},
	} {
		src := &system.FakeDeviceSource{
			Ageing:  tc.ageing,
			FDBRows: []system.FDBEntry{{MAC: "b8:27:eb:aa:bb:01", Updated: tc.age}},
		}
		got, err := deviceUsecase(src, nil).ListDevices(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got.Devices[0].Online != tc.online {
			t.Errorf("ageing=%d age=%d online=%v, want %v",
				tc.ageing, tc.age, got.Devices[0].Online, tc.online)
		}
		if got.OfflineAfterSeconds != tc.ageing {
			t.Errorf("OfflineAfterSeconds = %d, want %d", got.OfflineAfterSeconds, tc.ageing)
		}
	}
}

// One MAC really can hold a lease and a static address at once.
func TestListDevices_KeepsBothAddressesWithTheLeasedOneFirst(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing:    300,
		FDBRows:   []system.FDBEntry{{MAC: "b2:27:eb:aa:bb:02", Updated: 1}},
		LeaseRows: []system.Lease{{MAC: "b2:27:eb:aa:bb:02", IP: "10.77.0.163"}},
		NeighRows: []system.Neighbour{
			{MAC: "b2:27:eb:aa:bb:02", IP: "10.77.0.55"},
			{MAC: "b2:27:eb:aa:bb:02", IP: "10.77.0.163"},
		},
	}
	got, err := deviceUsecase(src, nil).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ips := got.Devices[0].IPs
	if len(ips) != 2 || ips[0] != "10.77.0.163" || ips[1] != "10.77.0.55" {
		t.Errorf("IPs = %v, want the leased address first and no duplicate", ips)
	}
}

func TestListDevices_FlagsRandomizedAndWithholdsAVendor(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing:  300,
		FDBRows: []system.FDBEntry{{MAC: "b2:27:eb:aa:bb:02", Updated: 1}},
	}
	got, err := deviceUsecase(src, nil).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := got.Devices[0]
	if !d.Randomized {
		t.Error("a locally-administered MAC was not flagged")
	}
	if d.Vendor != "" {
		t.Errorf("vendor = %q; a randomized prefix match is coincidence", d.Vendor)
	}
}

func TestListDevices_DisabledLANListsNothing(t *testing.T) {
	u := deviceUsecase(&system.FakeDeviceSource{Ageing: 300}, nil)
	u.LANRepo = &stubLANRepo{cfg: &domain.LANConfig{BridgeName: "lan0", Enabled: false}}
	got, err := u.ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || len(got.Devices) != 0 {
		t.Errorf("got %+v", got)
	}
}

// Online first, so the list does not reshuffle between polls.
func TestListDevices_SortsPresentFirstThenByName(t *testing.T) {
	src := &system.FakeDeviceSource{
		Ageing: 300,
		FDBRows: []system.FDBEntry{
			{MAC: "b8:27:eb:aa:bb:03", Updated: 900},
			{MAC: "b8:27:eb:aa:bb:01", Updated: 1},
		},
		LeaseRows: []system.Lease{
			{MAC: "b8:27:eb:aa:bb:03", Hostname: "aaa"},
			{MAC: "b8:27:eb:aa:bb:01", Hostname: "zzz"},
		},
	}
	got, err := deviceUsecase(src, nil).ListDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Devices[0].Hostname != "zzz" {
		t.Errorf("order = %+v; the present device must come first", got.Devices)
	}
}

func TestSetDeviceLabel_RefusesARandomizedMAC(t *testing.T) {
	labels := &stubLabels{}
	err := deviceUsecase(&system.FakeDeviceSource{}, labels).
		SetDeviceLabel(context.Background(), "b2:27:eb:aa:bb:02", "phone")
	if !errors.Is(err, domain.ErrRandomizedMAC) {
		t.Fatalf("err = %v, want ErrRandomizedMAC", err)
	}
	if labels.set[0] != "" {
		t.Error("the label was written despite the refusal")
	}
}

func TestSetDeviceLabel_NormalizesBeforeStoring(t *testing.T) {
	labels := &stubLabels{}
	if err := deviceUsecase(&system.FakeDeviceSource{}, labels).
		SetDeviceLabel(context.Background(), "B8:27:EB:AA:BB:01", "  the NAS  "); err != nil {
		t.Fatal(err)
	}
	if labels.set != [2]string{"b8:27:eb:aa:bb:01", "the NAS"} {
		t.Errorf("stored %+v", labels.set)
	}
}
