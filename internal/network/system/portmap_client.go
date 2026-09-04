package system

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

type dialControl func(network, address string, c syscall.RawConn) error

// portMapClient is stateless about mappings: the caller keeps the probe and the
// leases. One instance serves every WAN — the WAN arrives as an argument.
type portMapClient struct {
	// control binds a socket to the WAN and stamps the kill-switch mark; nil
	// leaves sockets unbound (tests, non-Linux).
	control func(ifName string, mark uint32) dialControl

	pxpPort  int
	ssdpPort int
	// localPort is where answers land. RFC 6886's own client port, so a
	// buggy router replying from an ephemeral source still reaches us through
	// an armed input firewall. Zero, or a taken port, falls back to ephemeral.
	localPort   int
	probeWindow time.Duration
	opTimeout   time.Duration

	// One HTTP client per uplink: a Transport is expensive to build and safe
	// to share, and the binding only ever varies by interface.
	mu      sync.Mutex
	clients map[string]*http.Client
}

func newPortMapClient(control func(string, uint32) dialControl) *portMapClient {
	return &portMapClient{
		control:     control,
		pxpPort:     5351,
		ssdpPort:    1900,
		localPort:   PortMapLocalPort,
		probeWindow: time.Second,
		opTimeout:   3 * time.Second,
		clients:     map[string]*http.Client{},
	}
}

func (c *portMapClient) listen(ctx context.Context, wan PortMapWAN) (net.PacketConn, error) {
	var lc net.ListenConfig
	if c.control != nil {
		lc.Control = c.control(wan.IfName, netmark.PinMark(netmark.PinPortmap))
	}
	if c.localPort > 0 {
		conn, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", c.localPort))
		if err == nil {
			return conn, nil
		}
		// Something else holds it. An ephemeral port still works for every
		// router that answers from the port it was asked on.
	}
	return lc.ListenPacket(ctx, "udp4", ":0")
}

// httpClient dials description and SOAP fetches through the same binding as
// the UDP side, and never follows a redirect off the gateway.
func (c *portMapClient) httpClient(wan PortMapWAN) *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cl, ok := c.clients[wan.IfName]; ok {
		return cl
	}
	d := &net.Dialer{Timeout: 2 * time.Second}
	if c.control != nil {
		d.Control = c.control(wan.IfName, netmark.PinMark(netmark.PinPortmap))
	}
	cl := &http.Client{
		Timeout: c.opTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{DialContext: d.DialContext, DisableKeepAlives: true},
	}
	c.clients[wan.IfName] = cl
	return cl
}

func (c *portMapClient) Probe(ctx context.Context, wan PortMapWAN) (PortMapProbe, error) {
	if !wan.Gateway.Is4() {
		return PortMapProbe{}, fmt.Errorf("port mapping is IPv4 only (gateway %q)", wan.Gateway)
	}
	if !wan.SelfIP.Is4() {
		return PortMapProbe{}, fmt.Errorf("uplink %s has no IPv4 address yet", wan.IfName)
	}
	conn, err := c.listen(ctx, wan)
	if err != nil {
		return PortMapProbe{}, fmt.Errorf("portmap probe socket: %w", err)
	}
	defer conn.Close()

	gw := wan.Gateway.As4()
	pxpAddr := &net.UDPAddr{IP: gw[:], Port: c.pxpPort}
	ssdpAddr := &net.UDPAddr{IP: gw[:], Port: c.ssdpPort}

	_, _ = conn.WriteTo(pmpExternalAddrRequest(), pxpAddr)
	_, _ = conn.WriteTo(pcpAnnounceRequest(wan.SelfIP), pxpAddr)
	// Unicast first: it primes stateful host firewalls for the answers.
	for _, st := range []string{stSSDPAll, stIGD} {
		_, _ = conn.WriteTo(ssdpSearchPacket(st), ssdpAddr)
	}
	if !wan.Gateway.IsLoopback() {
		if mc, err := net.ResolveUDPAddr("udp4", ssdpMulticast); err == nil {
			for _, st := range []string{stSSDPAll, stIGD} {
				_, _ = conn.WriteTo(ssdpSearchPacket(st), mc)
			}
		}
	}

	probe := PortMapProbe{SeenAt: time.Now()}
	deadline := time.Now().Add(c.probeWindow)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var ssdps []ssdpResponse
	seenLoc := map[string]bool{}
	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(deadline)
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			break // deadline; a silent gateway is the normal case
		}
		pkt := buf[:n]
		fromPort, fromIP := 0, netip.Addr{}
		if ua, ok := from.(*net.UDPAddr); ok {
			fromPort = ua.Port
			fromIP, _ = netip.AddrFromSlice(ua.IP)
			fromIP = fromIP.Unmap()
		}
		if fromPort == c.pxpPort && n > 0 {
			switch pkt[0] {
			case pmpVersion:
				if r, ok := parsePMPResponse(pkt); ok && r.Op == 0x80|pmpOpExternalAddr {
					switch r.Result {
					case pmpResultOK:
						probe.PMP, probe.ExternalIP = true, r.ExternalIP
						probe.Epoch = PortMapEpoch{Secs: r.Epoch, At: time.Now()}
					case pmpResultNotAuthorized:
						probe.Denied = true
					}
				}
			case pcpVersion:
				if r, ok := parsePCPResponse(pkt); ok && r.Opcode == pcpOpAnnounce {
					switch r.Result {
					case pcpResultOK:
						probe.PCP = true
						if !probe.Epoch.Known() {
							probe.Epoch = PortMapEpoch{Secs: r.Epoch, At: time.Now()}
						}
					case pcpResultNotAuthorized:
						probe.Denied = true
					}
				}
			}
		} else if r, ok := parseSSDPResponse(pkt); ok {
			// Any source port: buggy routers answer from an ephemeral one. But
			// a LAN full of TVs and media servers answers ssdp:all too, so only
			// a gateway or a self-declared IGD is worth a description fetch.
			r.FromGateway = fromIP.IsValid() && fromIP == wan.Gateway
			if (r.FromGateway || r.LooksLikeIGD()) && !seenLoc[r.Location] && len(ssdps) < 10 {
				seenLoc[r.Location] = true
				ssdps = append(ssdps, r)
			}
		}
		if probe.PMP && probe.PCP && len(ssdps) > 0 {
			// Everyone answered; a short grace collects a second IGD, no more.
			grace := time.Now().Add(100 * time.Millisecond)
			if grace.Before(deadline) {
				deadline = grace
			}
		}
	}

	if len(ssdps) > 0 {
		c.resolveUPnP(ctx, wan, ssdps, &probe)
	}
	return probe, nil
}

// resolveUPnP fetches descriptions until one yields a usable service, IGDv2
// first — its AddAnyPortMapping resolves port conflicts for us. The external
// address is read here too: a Map must never create a mapping it cannot then
// describe.
func (c *portMapClient) resolveUPnP(ctx context.Context, wan PortMapWAN, ssdps []ssdpResponse, probe *PortMapProbe) {
	rank := func(r ssdpResponse) int {
		switch {
		case strings.Contains(r.USN, "InternetGatewayDevice:2"):
			return 0
		case r.FromGateway:
			return 1
		default:
			return 2
		}
	}
	ordered := make([]ssdpResponse, 0, len(ssdps))
	for want := 0; want <= 2; want++ {
		for _, r := range ssdps {
			if rank(r) == want {
				ordered = append(ordered, r)
			}
		}
	}
	client := c.httpClient(wan)
	for _, r := range ordered {
		loc := repointToGateway(r.Location, wan.Gateway)
		base, err := url.Parse(loc)
		if err != nil {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
		if err != nil {
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			continue
		}
		svc, ok := selectUPnPService(parseIGDServices(body, base, wan.Gateway))
		if !ok {
			continue
		}
		probe.UPnP = true
		probe.UPnPService = svc
		if !probe.ExternalIP.IsValid() {
			if ip, err := c.upnpExternalIP(ctx, wan, svc); err == nil {
				probe.ExternalIP = ip
			}
		}
		if !probe.Epoch.Known() {
			// UPnP has no epoch field. Its uptime counter says the same thing:
			// a router that restarted forgot every mapping it granted us.
			if up, err := c.upnpUptime(ctx, wan, svc); err == nil {
				probe.Epoch = PortMapEpoch{Secs: up, At: time.Now()}
			}
		}
		return
	}
}

// repointToGateway forces a URL's host to the gateway, keeping its port.
// Locations float on some routers, and the kill switch only exempts the
// gateway address.
func repointToGateway(raw string, gateway netip.Addr) string {
	u, err := url.Parse(raw)
	if err != nil || !gateway.IsValid() {
		return raw
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	u.Host = net.JoinHostPort(gateway.String(), port)
	return u.String()
}

const defaultLeaseSecs = 7200 // RFC 6886's recommendation; PCP and UPnP reuse it

func wantExternal(req PortMapRequest) uint16 {
	if req.ExternalHint != 0 {
		return req.ExternalHint
	}
	return req.InternalPort
}

func leaseSecs(req PortMapRequest) uint32 {
	if req.Lifetime <= 0 {
		return defaultLeaseSecs
	}
	return uint32(req.Lifetime / time.Second)
}

func leaseTimes(grantedSecs uint32) (goodUntil, renewAfter time.Time) {
	now := time.Now()
	d := time.Duration(grantedSecs) * time.Second
	return now.Add(d), now.Add(d / 2)
}

// Map asks for one mapping, trying each protocol the probe found in the
// preference order. A gateway that answers NAT-PMP but refuses to map may
// still run a working UPnP IGD, so one protocol's no is not the answer.
func (c *portMapClient) Map(ctx context.Context, wan PortMapWAN, probe PortMapProbe, req PortMapRequest) (PortMapLease, error) {
	attempts := []struct {
		ok bool
		do func() (PortMapLease, error)
	}{
		{probe.PMP, func() (PortMapLease, error) { return c.pmpMap(ctx, wan, probe, req) }},
		{probe.PCP, func() (PortMapLease, error) { return c.pcpMap(ctx, wan, req) }},
		{probe.UPnP, func() (PortMapLease, error) { return c.upnpMap(ctx, wan, probe, req) }},
	}
	var firstErr error
	for _, a := range attempts {
		if !a.ok {
			continue
		}
		lease, err := a.do()
		if err == nil {
			return lease, nil
		}
		// Nested NAT is the gateway's whole answer, not this protocol's: every
		// other protocol on the same box would forward into the same dead end.
		if errors.Is(err, ErrPortMapNestedNAT) {
			return PortMapLease{}, err
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			return PortMapLease{}, ctx.Err()
		}
	}
	if firstErr != nil {
		return PortMapLease{}, firstErr
	}
	return PortMapLease{}, ErrPortMapNoService
}

func (c *portMapClient) Unmap(ctx context.Context, wan PortMapWAN, lease PortMapLease) error {
	switch lease.Method {
	case "natpmp":
		// Lifetime 0 with external 0 destroys the mapping.
		_, err := c.sendPXP(ctx, wan, pmpMapRequest(lease.Proto, lease.InternalPort, 0, 0), 1)
		return err
	case "pcp":
		_, err := c.sendPXP(ctx, wan,
			pcpMapRequest(wan.SelfIP, lease.Nonce, lease.Proto, lease.InternalPort, 0, 0), 1)
		return err
	case "upnp":
		return c.upnpDelete(ctx, wan, lease.ControlURL, lease.ServiceType, lease.Proto, lease.External.Port())
	}
	return nil
}

func (c *portMapClient) upnpDelete(ctx context.Context, wan PortMapWAN, controlURL, serviceType, proto string, external uint16) error {
	if controlURL == "" {
		return nil
	}
	_, err := c.soapCall(ctx, wan, controlURL, serviceType, "DeletePortMapping", [][2]string{
		{"NewRemoteHost", ""},
		{"NewExternalPort", strconv.Itoa(int(external))},
		{"NewProtocol", strings.ToUpper(proto)},
	})
	return err
}

// sendPXP fires payload at gateway:5351 up to tries times and returns the
// first parseable answer from the gateway itself.
func (c *portMapClient) sendPXP(ctx context.Context, wan PortMapWAN, payload []byte, tries int) ([]byte, error) {
	conn, err := c.listen(ctx, wan)
	if err != nil {
		return nil, fmt.Errorf("portmap socket: %w", err)
	}
	defer conn.Close()
	gw := wan.Gateway.As4()
	dst := &net.UDPAddr{IP: gw[:], Port: c.pxpPort}
	buf := make([]byte, 1500)
	for i := 0; i < tries; i++ {
		if _, err := conn.WriteTo(payload, dst); err != nil {
			return nil, fmt.Errorf("portmap send: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(700 * time.Millisecond))
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				break // retry
			}
			// Only the gateway's own 5351 may answer a mapping request.
			if ua, ok := from.(*net.UDPAddr); !ok || ua.Port != c.pxpPort {
				continue
			}
			out := make([]byte, n)
			copy(out, buf[:n])
			return out, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, ErrPortMapNoService
}

// pmpExternal reads the gateway's external address, preferring the one the
// probe already has: it is the same answer, one round trip cheaper.
func (c *portMapClient) pmpExternal(ctx context.Context, wan PortMapWAN, probe PortMapProbe) (netip.Addr, error) {
	if probe.ExternalIP.IsValid() && !probe.ExternalIP.IsUnspecified() {
		return probe.ExternalIP, nil
	}
	raw, err := c.sendPXP(ctx, wan, pmpExternalAddrRequest(), 2)
	if err != nil {
		return netip.Addr{}, err
	}
	addr, ok := parsePMPResponse(raw)
	if !ok || addr.Op != 0x80|pmpOpExternalAddr {
		return netip.Addr{}, ErrPortMapNoService
	}
	if addr.Result == pmpResultNotAuthorized {
		return netip.Addr{}, fmt.Errorf("nat-pmp external address: %w", ErrPortMapDenied)
	}
	if addr.Result != pmpResultOK {
		return netip.Addr{}, fmt.Errorf("nat-pmp external address: result %d", addr.Result)
	}
	return addr.ExternalIP, nil
}

func (c *portMapClient) pmpMap(ctx context.Context, wan PortMapWAN, probe PortMapProbe, req PortMapRequest) (PortMapLease, error) {
	extIP, err := c.pmpExternal(ctx, wan, probe)
	if err != nil {
		return PortMapLease{}, err
	}

	raw, err := c.sendPXP(ctx, wan,
		pmpMapRequest(req.Proto, req.InternalPort, wantExternal(req), leaseSecs(req)), 2)
	if err != nil {
		return PortMapLease{}, err
	}
	m, ok := parsePMPResponse(raw)
	if !ok || m.Op != 0x80|pmpProtoOp(req.Proto) {
		return PortMapLease{}, ErrPortMapNoService
	}
	switch m.Result {
	case pmpResultOK:
	case pmpResultNotAuthorized:
		return PortMapLease{}, fmt.Errorf("nat-pmp map: %w", ErrPortMapDenied)
	default:
		return PortMapLease{}, fmt.Errorf("nat-pmp map: result %d", m.Result)
	}
	good, renew := leaseTimes(m.Lifetime)
	return PortMapLease{
		Proto: req.Proto, InternalPort: req.InternalPort,
		External: netip.AddrPortFrom(extIP, m.ExternalPort),
		Method:   "natpmp", GoodUntil: good, RenewAfter: renew,
		Epoch: PortMapEpoch{Secs: m.Epoch, At: time.Now()},
	}, nil
}

func (c *portMapClient) pcpMap(ctx context.Context, wan PortMapWAN, req PortMapRequest) (PortMapLease, error) {
	// RFC 6887 §11.3: a renewal is the same request, nonce included. A fresh
	// nonce for a mapping the server already holds earns NOT_AUTHORIZED.
	nonce := req.Nonce
	if !req.Renewal || nonce == ([12]byte{}) {
		if _, err := rand.Read(nonce[:]); err != nil {
			return PortMapLease{}, fmt.Errorf("pcp nonce: %w", err)
		}
	}
	raw, err := c.sendPXP(ctx, wan,
		pcpMapRequest(wan.SelfIP, nonce, req.Proto, req.InternalPort, wantExternal(req), leaseSecs(req)), 2)
	if err != nil {
		return PortMapLease{}, err
	}
	r, ok := parsePCPResponse(raw)
	// A reply carrying someone else's nonce is not ours.
	if !ok || r.Opcode != pcpOpMap || r.Nonce != nonce {
		return PortMapLease{}, ErrPortMapNoService
	}
	switch r.Result {
	case pcpResultOK:
	case pcpResultNotAuthorized:
		return PortMapLease{}, fmt.Errorf("pcp map: %w", ErrPortMapDenied)
	case pcpResultAddressMismatch:
		return PortMapLease{}, fmt.Errorf("pcp map: %w", ErrPortMapNestedNAT)
	default:
		return PortMapLease{}, fmt.Errorf("pcp map: result %d", r.Result)
	}
	good, renew := leaseTimes(r.Lifetime)
	return PortMapLease{
		Proto: req.Proto, InternalPort: req.InternalPort,
		External: netip.AddrPortFrom(r.ExternalIP, r.ExternalPort),
		Method:   "pcp", GoodUntil: good, RenewAfter: renew,
		Epoch: PortMapEpoch{Secs: r.Epoch, At: time.Now()}, Nonce: nonce,
	}, nil
}

type soapError struct{ Code int }

func (e soapError) Error() string { return fmt.Sprintf("upnp fault %d", e.Code) }

// SOAP faults the mapping path has an answer for.
const (
	soapConflict      = 718 // ConflictInMappingEntry: someone else holds that port
	soapUnsupported   = 402 // Invalid Args, which is how some routers say 725
	soapOnlyPermLease = 725 // OnlyPermanentLeasesSupported
)

func (c *portMapClient) soapCall(ctx context.Context, wan PortMapWAN, controlURL, serviceType, action string, args [][2]string) ([]byte, error) {
	body := soapEnvelope(serviceType, action, args)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	httpReq.Header.Set("SOAPAction", soapActionHeader(serviceType, action))
	resp, err := c.httpClient(wan).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upnp %s: %w", action, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, fmt.Errorf("upnp %s: %w", action, err)
	}
	if code := soapFaultCode(out); code != 0 {
		return out, soapError{Code: code}
	}
	if resp.StatusCode != 200 {
		return out, fmt.Errorf("upnp %s: http %d", action, resp.StatusCode)
	}
	return out, nil
}

func (c *portMapClient) upnpExternalIP(ctx context.Context, wan PortMapWAN, svc UPnPServiceInfo) (netip.Addr, error) {
	out, err := c.soapCall(ctx, wan, svc.ControlURL, svc.ServiceType, "GetExternalIPAddress", nil)
	if err != nil {
		return netip.Addr{}, err
	}
	text := xmlText(out, "NewExternalIPAddress")
	ip, perr := netip.ParseAddr(text)
	if perr != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsLoopback() {
		return netip.Addr{}, fmt.Errorf("upnp external address unusable: %q", text)
	}
	return ip, nil
}

// upnpUptime reads the connection's age off GetStatusInfo. Best effort: a
// router that will not say is simply a router whose reboots we cannot see.
func (c *portMapClient) upnpUptime(ctx context.Context, wan PortMapWAN, svc UPnPServiceInfo) (uint32, error) {
	out, err := c.soapCall(ctx, wan, svc.ControlURL, svc.ServiceType, "GetStatusInfo", nil)
	if err != nil {
		return 0, err
	}
	secs, perr := strconv.ParseUint(xmlText(out, "NewUptime"), 10, 32)
	if perr != nil {
		return 0, fmt.Errorf("upnp uptime unreadable: %w", perr)
	}
	return uint32(secs), nil
}

func (c *portMapClient) upnpMap(ctx context.Context, wan PortMapWAN, probe PortMapProbe, req PortMapRequest) (PortMapLease, error) {
	svc := probe.UPnPService
	if svc.ControlURL == "" {
		return PortMapLease{}, ErrPortMapNoService
	}
	// The external address first, always. A mapping we cannot describe is a
	// mapping we would leave behind on the router forever.
	extIP := probe.ExternalIP
	if !extIP.IsValid() || extIP.IsUnspecified() {
		var err error
		if extIP, err = c.upnpExternalIP(ctx, wan, svc); err != nil {
			return PortMapLease{}, err
		}
	}

	proto := strings.ToUpper(req.Proto) // some routers reject lowercase
	secs := leaseSecs(req)
	desc := req.Description
	if desc == "" {
		desc = "nasnet"
	}
	mapArgs := func(ext uint16, lease uint32) [][2]string {
		return [][2]string{
			{"NewRemoteHost", ""},
			{"NewExternalPort", strconv.Itoa(int(ext))},
			{"NewProtocol", proto},
			{"NewInternalPort", strconv.Itoa(int(req.InternalPort))},
			{"NewInternalClient", wan.SelfIP.String()},
			{"NewEnabled", "1"},
			{"NewPortMappingDescription", desc},
			{"NewLeaseDuration", strconv.FormatUint(uint64(lease), 10)},
		}
	}
	// The permanent-lease routers, and the ones that misreport 725 as 402.
	// Ask forever instead, and re-verify on our own clock.
	permanentRetry := func(ext uint16, action string) error {
		_, err := c.soapCall(ctx, wan, svc.ControlURL, svc.ServiceType, action, mapArgs(ext, 0))
		return err
	}

	ext := wantExternal(req)
	granted := ext
	permanent := false
	v2 := strings.HasSuffix(svc.ServiceType, "WANIPConnection:2")

	if v2 {
		out, err := c.soapCall(ctx, wan, svc.ControlURL, svc.ServiceType, "AddAnyPortMapping", mapArgs(ext, secs))
		var se soapError
		if errors.As(err, &se) && (se.Code == soapOnlyPermLease || se.Code == soapUnsupported) {
			if err = permanentRetry(ext, "AddAnyPortMapping"); err == nil {
				permanent = true
			}
		}
		if err != nil {
			return PortMapLease{}, err
		}
		if p, perr := strconv.ParseUint(xmlText(out, "NewReservedPort"), 10, 16); perr == nil && p != 0 {
			granted = uint16(p)
		}
	} else {
		var lastErr error
		conflictRetried := false
		done := false
		for attempt := 0; attempt < 3 && !done; attempt++ {
			_, err := c.soapCall(ctx, wan, svc.ControlURL, svc.ServiceType, "AddPortMapping", mapArgs(ext, secs))
			var se soapError
			switch {
			case err == nil:
				granted, done = ext, true
			case errors.As(err, &se) && (se.Code == soapOnlyPermLease || se.Code == soapUnsupported):
				if err := permanentRetry(ext, "AddPortMapping"); err != nil {
					return PortMapLease{}, err
				}
				granted, permanent, done = ext, true, true
			case errors.As(err, &se) && se.Code == soapConflict && req.Renewal && !conflictRetried:
				// Our own row from last time. Some routers refuse to overwrite
				// it; clearing it first keeps the port the clients know.
				conflictRetried = true
				lastErr = err
				_ = c.upnpDelete(ctx, wan, svc.ControlURL, svc.ServiceType, req.Proto, ext)
			case errors.As(err, &se) && se.Code == soapConflict:
				// Someone else holds it. Never retry with 0 — that is the
				// spec's wildcard for every port.
				ext = uint16(1024 + mrand.IntN(65536-1024))
				lastErr = err
			default:
				return PortMapLease{}, err
			}
		}
		if !done {
			return PortMapLease{}, fmt.Errorf("upnp map: %w", lastErr)
		}
	}

	// The router moved us to a different external port. The row we held is
	// still there, still pointed here, and nothing will ever release it.
	if req.Renewal && granted != wantExternal(req) {
		_ = c.upnpDelete(ctx, wan, svc.ControlURL, svc.ServiceType, req.Proto, wantExternal(req))
	}

	good, renew := leaseTimes(secs)
	if permanent {
		good, renew = leaseTimes(defaultLeaseSecs)
	}
	return PortMapLease{
		Proto: req.Proto, InternalPort: req.InternalPort,
		External: netip.AddrPortFrom(extIP, granted),
		Method:   "upnp", GoodUntil: good, RenewAfter: renew, Permanent: permanent,
		Epoch:      probe.Epoch,
		ControlURL: svc.ControlURL, ServiceType: svc.ServiceType,
	}, nil
}

var _ PortMapper = (*portMapClient)(nil)
