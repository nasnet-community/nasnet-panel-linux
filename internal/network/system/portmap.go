package system

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// PortMapLocalPort is the UDP port the mapper's own sockets bind to, so an
// armed input firewall can name it. RFC 6886 gives clients this port for the
// gateway's unsolicited announcements.
const PortMapLocalPort = 5350

// PortMapWAN is one uplink as the mapper sees it: where to bind, whom to ask.
type PortMapWAN struct {
	IfName  string
	Gateway netip.Addr
	SelfIP  netip.Addr
}

type PortMapRequest struct {
	Proto        string // "tcp" | "udp"
	InternalPort uint16
	// ExternalHint asks for this external port; zero asks for InternalPort.
	ExternalHint uint16
	Lifetime     time.Duration
	Description  string
	// Renewal says this request extends a mapping we already hold. PCP must
	// present the original nonce (RFC 6887 §11.3) or the server answers
	// NOT_AUTHORIZED; UPnP must clear its own row before re-adding it.
	Renewal bool
	Nonce   [12]byte
}

// PortMapLease is one granted mapping. Immutable once returned.
type PortMapLease struct {
	Proto        string
	InternalPort uint16
	External     netip.AddrPort
	Method       string // "natpmp" | "pcp" | "upnp"
	GoodUntil    time.Time
	RenewAfter   time.Time
	// Permanent means the router only grants infinite leases. Still re-verified
	// on our own clock, or a reboot forgets it silently.
	Permanent bool
	Epoch     PortMapEpoch
	// Nonce is the create's nonce. A PCP renewal or delete must present it.
	Nonce [12]byte
	// UPnP only: where it was created, so a delete needs no rediscovery.
	ControlURL  string
	ServiceType string
}

// UPnPServiceInfo outlives a probe so a renew can skip rediscovery.
type UPnPServiceInfo struct {
	ControlURL  string
	ServiceType string
}

// PortMapEpoch is a mapping server's uptime counter, read straight off the
// wire. Both NAT-PMP and PCP send one, and both define a reboot the same way.
type PortMapEpoch struct {
	Secs uint32
	// At is our own clock when Secs arrived, so the expected growth is known.
	At time.Time
}

func (e PortMapEpoch) Known() bool { return !e.At.IsZero() }

// Rebooted reports whether next is too small to be the same boot as e.
// RFC 6886 §3.6: the server rebooted if the new epoch is more than a small
// slack below the value the elapsed wall time predicts.
func (e PortMapEpoch) Rebooted(next PortMapEpoch) bool {
	if !e.Known() || !next.Known() || next.At.Before(e.At) {
		return false
	}
	elapsed := next.At.Sub(e.At).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	// The RFC's own inequality, in seconds: 7/8 of the elapsed time minus two.
	expected := float64(e.Secs) + elapsed*7/8 - 2
	return float64(next.Secs) < expected
}

// PortMapProbe is what one pass over the gateway found. The caller caches it;
// the client is stateless.
type PortMapProbe struct {
	PMP, PCP, UPnP bool
	// Denied: something answered, but mapping is turned off there.
	Denied bool
	// ExternalIP is what the gateway says the internet sees. Read once here,
	// so a Map never has to ask again — and never creates a mapping it then
	// cannot describe.
	ExternalIP  netip.Addr
	Epoch       PortMapEpoch
	UPnPService UPnPServiceInfo
	SeenAt      time.Time
}

func (p PortMapProbe) Any() bool { return p.PMP || p.PCP || p.UPnP }

var (
	ErrPortMapNoService = errors.New("no port mapping service on the gateway")
	ErrPortMapDenied    = errors.New("the gateway refuses port mappings")
	// The gateway is itself behind NAT (PCP code 12), so its forward cannot
	// reach the internet.
	ErrPortMapNestedNAT = errors.New("the gateway is itself behind another NAT")
)

// PortMapper is behind an interface because CI has no router to ask.
type PortMapper interface {
	Probe(ctx context.Context, wan PortMapWAN) (PortMapProbe, error)
	Map(ctx context.Context, wan PortMapWAN, probe PortMapProbe, req PortMapRequest) (PortMapLease, error)
	Unmap(ctx context.Context, wan PortMapWAN, lease PortMapLease) error
}

// FakePortMapper is an in-memory PortMapper for unprivileged tests.
type FakePortMapper struct {
	mu       sync.Mutex
	ProbeRes PortMapProbe
	ProbeErr error
	MapErr   error
	UnmapErr error
	// GrantIP and GrantPort shape the lease; zero grants what was asked.
	GrantIP   netip.Addr
	GrantPort uint16
	// GrantEpoch rides on every granted lease, so a test can stage a reboot.
	GrantEpoch PortMapEpoch
	Mapped     map[string]PortMapLease
	// Requests and Released are the call log a test asserts against.
	Requests []PortMapRequest
	Released []PortMapLease
	// ReleasedTo records which gateway each release was addressed to.
	ReleasedTo []string
	probes     int
	unmaps     int
}

func fakeLeaseKey(wan PortMapWAN, proto string, port uint16) string {
	return fmt.Sprintf("%s/%s/%d", wan.IfName, proto, port)
}

func (f *FakePortMapper) Probe(_ context.Context, _ PortMapWAN) (PortMapProbe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	if f.ProbeErr != nil {
		return PortMapProbe{}, f.ProbeErr
	}
	res := f.ProbeRes
	if res.SeenAt.IsZero() {
		res.SeenAt = time.Now()
	}
	return res, nil
}

func (f *FakePortMapper) Map(_ context.Context, wan PortMapWAN, _ PortMapProbe, req PortMapRequest) (PortMapLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)
	if f.MapErr != nil {
		return PortMapLease{}, f.MapErr
	}
	ext := req.ExternalHint
	if ext == 0 {
		ext = req.InternalPort
	}
	if f.GrantPort != 0 {
		ext = f.GrantPort
	}
	ip := f.GrantIP
	if !ip.IsValid() {
		ip = netip.AddrFrom4([4]byte{203, 0, 113, 7})
	}
	now := time.Now()
	// A renewal keeps the nonce it was granted with; a fresh mapping invents
	// one, exactly as the PCP client does.
	nonce := req.Nonce
	if !req.Renewal {
		nonce = [12]byte{1, 2, 3}
	}
	epoch := f.GrantEpoch
	if !epoch.Known() {
		epoch = PortMapEpoch{Secs: 100, At: now}
	}
	lease := PortMapLease{
		Proto: req.Proto, InternalPort: req.InternalPort,
		External: netip.AddrPortFrom(ip, ext), Method: "natpmp",
		GoodUntil: now.Add(req.Lifetime), RenewAfter: now.Add(req.Lifetime / 2),
		Epoch: epoch, Nonce: nonce,
	}
	if f.Mapped == nil {
		f.Mapped = map[string]PortMapLease{}
	}
	f.Mapped[fakeLeaseKey(wan, req.Proto, req.InternalPort)] = lease
	return lease, nil
}

func (f *FakePortMapper) Unmap(_ context.Context, wan PortMapWAN, lease PortMapLease) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unmaps++
	f.Released = append(f.Released, lease)
	f.ReleasedTo = append(f.ReleasedTo, wan.Gateway.String())
	if f.UnmapErr != nil {
		return f.UnmapErr
	}
	delete(f.Mapped, fakeLeaseKey(wan, lease.Proto, lease.InternalPort))
	return nil
}

// Probes and Unmaps count calls, so a test can prove a probe was cached or a
// lease was really released.
func (f *FakePortMapper) Probes() int { f.mu.Lock(); defer f.mu.Unlock(); return f.probes }
func (f *FakePortMapper) Unmaps() int { f.mu.Lock(); defer f.mu.Unlock(); return f.unmaps }

// Held is a test convenience: the lease for that key, zero when absent.
func (f *FakePortMapper) Held(wan PortMapWAN, proto string, port uint16) (PortMapLease, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.Mapped[fakeLeaseKey(wan, proto, port)]
	return l, ok
}
