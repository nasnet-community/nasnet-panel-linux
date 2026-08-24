package system

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
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
	// RouteGetFn answers the tracer's kernel cross-check.
	RouteGetFn func(dst string, mark uint32) (*Route, error)
}

func (f *FakeBackend) RouteGet(_ context.Context, dst string, mark uint32) (*Route, error) {
	f.mu.Lock()
	fn := f.RouteGetFn
	f.mu.Unlock()
	if fn == nil {
		return nil, fmt.Errorf("no RouteGetFn")
	}
	return fn(dst, mark)
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
	// The kernel keys replace on (dst, metric); a different metric is a new route.
	for i := range f.Routes {
		if f.Routes[i].Table == r.Table && f.Routes[i].Dest == r.Dest &&
			f.Routes[i].Metric == r.Metric {
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
	// Kernel-style: no explicit metric only removes that exact metric's alias.
	out := f.Routes[:0]
	for _, ex := range f.Routes {
		if ex.Table == r.Table && ex.Dest == r.Dest && ex.Metric == r.Metric {
			continue
		}
		out = append(out, ex)
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

// FakeWGState is one fake link: absent until Ensure runs, like the real one.
type FakeWGState struct {
	Applied  *WGApplyConfig
	Endpoint netip.AddrPort
	Stat     *WGStatus
}

// FakeWGDevice is an in-memory WGDevice keyed by interface name.
type FakeWGDevice struct {
	mu      sync.Mutex
	Devices map[string]*FakeWGState
	Deleted map[string]int
	ensures int

	EnsureErr   error
	StatusErr   error
	EndpointErr error
	DeleteErr   error
}

// State is a test convenience: the device's fake state, nil when absent.
func (f *FakeWGDevice) State(ifName string) *FakeWGState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Devices[ifName]
}

func (f *FakeWGDevice) Ensure(_ context.Context, ifName string, cfg WGApplyConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EnsureErr != nil {
		return f.EnsureErr
	}
	if f.Devices == nil {
		f.Devices = map[string]*FakeWGState{}
	}
	st, ok := f.Devices[ifName]
	if !ok {
		st = &FakeWGState{}
		f.Devices[ifName] = st
	}
	st.Applied = &cfg
	st.Endpoint = cfg.Endpoint
	if st.Stat == nil {
		st.Stat = &WGStatus{}
	}
	f.ensures++
	return nil
}

// EnsureCalls counts configuration writes, so a test can prove a tunnel was
// left alone rather than needlessly re-handshaked.
func (f *FakeWGDevice) EnsureCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ensures
}

func (f *FakeWGDevice) UpdateEndpoint(_ context.Context, ifName string, ep netip.AddrPort) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.EndpointErr != nil {
		return f.EndpointErr
	}
	st, ok := f.Devices[ifName]
	if !ok {
		return ErrNoWGDevice
	}
	st.Endpoint = ep
	return nil
}

func (f *FakeWGDevice) Status(_ context.Context, ifName string) (*WGStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StatusErr != nil {
		return nil, f.StatusErr
	}
	st, ok := f.Devices[ifName]
	if !ok || st.Applied == nil {
		return nil, ErrNoWGDevice
	}
	return st.Stat, nil
}

func (f *FakeWGDevice) Delete(_ context.Context, ifName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	if f.Deleted == nil {
		f.Deleted = map[string]int{}
	}
	f.Deleted[ifName]++
	delete(f.Devices, ifName)
	return nil
}

func (f *FakeWGDevice) List(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.Devices))
	for name := range f.Devices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// FakeNft is an in-memory NftReader. Membership is exact-match only; tests
// list the concrete IPs they resolve.
type FakeNft struct {
	RulesetText string
	Objects     NftObjects
	Members     map[string][]string
	Err         error
}

func (f *FakeNft) ListRuleset(context.Context) (string, error) { return f.RulesetText, f.Err }

func (f *FakeNft) LiveObjects(context.Context) (*NftObjects, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	o := f.Objects
	if o.Counters == nil {
		o.Counters = map[string]NftCounter{}
	}
	return &o, nil
}

func (f *FakeNft) SetContains(_ context.Context, set, element string) (bool, error) {
	if f.Err != nil {
		return false, f.Err
	}
	for _, e := range f.Members[set] {
		if e == element {
			return true, nil
		}
	}
	return false, nil
}

// FakeFlowSource is an in-memory FlowSource.
type FakeFlowSource struct {
	Flows    []CTFlow
	Stats    map[string]LinkStat
	CTErr    error
	StatsErr error
}

func (f *FakeFlowSource) Conntrack(context.Context) ([]CTFlow, error) { return f.Flows, f.CTErr }

func (f *FakeFlowSource) LinkStats(context.Context) (map[string]LinkStat, error) {
	return f.Stats, f.StatsErr
}
