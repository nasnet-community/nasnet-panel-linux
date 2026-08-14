package system

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
)

// FakeBackend is an in-memory Backend for unprivileged tests
type FakeBackend struct {
	mu        sync.Mutex
	Rules     []Rule
	Routes    []Route
	Sysctls   map[string]string
	LinkNames []string
	AddrList  []Addr
	Err       error
}

func NewFakeBackend() *FakeBackend {
	return &FakeBackend{Sysctls: map[string]string{}}
}

func (f *FakeBackend) Links(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.LinkNames...), f.Err
}

func (f *FakeBackend) Addrs(context.Context) ([]Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Addr(nil), f.AddrList...), f.Err
}

func (f *FakeBackend) RuleList(context.Context) ([]Rule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Rule(nil), f.Rules...), f.Err
}

func (f *FakeBackend) RuleAdd(_ context.Context, r Rule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for _, ex := range f.Rules {
		if ex.Equal(r) {
			return nil
		}
	}
	f.Rules = append(f.Rules, r)
	return nil
}

func (f *FakeBackend) RuleDel(_ context.Context, r Rule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	out := f.Rules[:0]
	for _, ex := range f.Rules {
		if !ex.Equal(r) {
			out = append(out, ex)
		}
	}
	f.Rules = append([]Rule(nil), out...)
	return nil
}

func (f *FakeBackend) RouteList(_ context.Context, table int) ([]Route, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Route
	for _, r := range f.Routes {
		if r.Table == table {
			out = append(out, r)
		}
	}
	return out, f.Err
}

func (f *FakeBackend) RouteReplace(_ context.Context, r Route) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	for i := range f.Routes {
		if f.Routes[i].Table == r.Table && f.Routes[i].Dest == r.Dest {
			f.Routes[i] = r
			return nil
		}
	}
	f.Routes = append(f.Routes, r)
	return nil
}

func (f *FakeBackend) RouteDel(_ context.Context, r Route) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	out := f.Routes[:0]
	for _, ex := range f.Routes {
		if !(ex.Table == r.Table && ex.Dest == r.Dest) {
			out = append(out, ex)
		}
	}
	f.Routes = append([]Route(nil), out...)
	return nil
}

func (f *FakeBackend) SysctlSet(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Sysctls[key] = value
	return nil
}

func (f *FakeBackend) SysctlGet(_ context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.Sysctls[key]
	if !ok {
		return "", fmt.Errorf("sysctl %s not set", key)
	}
	return v, nil
}

// FakeDeviceSource is an in-memory DeviceSource for tests. A nil error field
// means that source answers; set one to exercise the degraded paths.
type FakeDeviceSource struct {
	LeaseRows []Lease
	NeighRows []Neighbour
	FDBRows   []FDBEntry
	Ageing    int
	LeaseErr  error
	NeighErr  error
	FDBErr    error
	AgeingErr error
}

func (f *FakeDeviceSource) Leases(context.Context) ([]Lease, error) {
	return f.LeaseRows, f.LeaseErr
}

func (f *FakeDeviceSource) Neighbours(context.Context, string) ([]Neighbour, error) {
	return f.NeighRows, f.NeighErr
}

func (f *FakeDeviceSource) FDB(context.Context, string) ([]FDBEntry, error) {
	return f.FDBRows, f.FDBErr
}

func (f *FakeDeviceSource) AgeingSeconds(context.Context, string) (int, error) {
	return f.Ageing, f.AgeingErr
}

// FakeWGDevice is an in-memory WGDevice. Absent until Ensure runs, like the
// real one with no profile.
type FakeWGDevice struct {
	mu       sync.Mutex
	Applied  *WGApplyConfig
	Endpoint netip.AddrPort
	Stat     *WGStatus
	Deleted  int

	EnsureErr   error
	StatusErr   error
	EndpointErr error
	DeleteErr   error
}

func (f *FakeWGDevice) Ensure(_ context.Context, cfg WGApplyConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	f.Applied = &cfg
	f.Endpoint = cfg.Endpoint
	if f.Stat == nil {
		f.Stat = &WGStatus{}
	}
	return nil
}

func (f *FakeWGDevice) UpdateEndpoint(_ context.Context, ep netip.AddrPort) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EndpointErr != nil {
		return f.EndpointErr
	}
	f.Endpoint = ep
	return nil
}

func (f *FakeWGDevice) Status(context.Context) (*WGStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatusErr != nil {
		return nil, f.StatusErr
	}
	if f.Applied == nil {
		return nil, ErrNoWGDevice
	}
	return f.Stat, nil
}

func (f *FakeWGDevice) Delete(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	f.Deleted++
	f.Applied = nil
	f.Stat = nil
	return nil
}
