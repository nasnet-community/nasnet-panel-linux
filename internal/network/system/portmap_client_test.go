package system

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePXP answers NAT-PMP and PCP on one loopback socket.
type fakePXP struct {
	mu        sync.Mutex
	lastNonce [12]byte
	conn      *net.UDPConn
	pmpResult uint16
	pcpResult uint8
	epoch     uint32
	extIP     [4]byte
	grant     func(internal uint16) uint16 // nil grants the suggestion
}

func startFakePXP(t *testing.T) *fakePXP {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakePXP{conn: conn, epoch: 100, extIP: [4]byte{203, 0, 113, 7}}
	t.Cleanup(func() { conn.Close() })
	go f.serve()
	return f
}

func (f *fakePXP) port() int { return f.conn.LocalAddr().(*net.UDPAddr).Port }

func (f *fakePXP) nonce() [12]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastNonce
}

func (f *fakePXP) serve() {
	buf := make([]byte, 1500)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := buf[:n]
		switch pkt[0] {
		case 0: // NAT-PMP
			switch pkt[1] {
			case 0:
				out := make([]byte, 12)
				out[1] = 0x80
				binary.BigEndian.PutUint16(out[2:], f.pmpResult)
				binary.BigEndian.PutUint32(out[4:], f.epoch)
				copy(out[8:], f.extIP[:])
				f.conn.WriteToUDP(out, from)
			case 1, 2:
				out := make([]byte, 16)
				out[1] = 0x80 | pkt[1]
				binary.BigEndian.PutUint16(out[2:], f.pmpResult)
				binary.BigEndian.PutUint32(out[4:], f.epoch)
				copy(out[8:10], pkt[4:6])
				ext := binary.BigEndian.Uint16(pkt[6:8])
				if f.grant != nil {
					ext = f.grant(binary.BigEndian.Uint16(pkt[4:6]))
				}
				binary.BigEndian.PutUint16(out[10:], ext)
				copy(out[12:16], pkt[8:12])
				f.conn.WriteToUDP(out, from)
			}
		case 2: // PCP
			if pkt[1] == 1 && n >= 36 {
				f.mu.Lock()
				copy(f.lastNonce[:], pkt[24:36])
				f.mu.Unlock()
			}
			out := make([]byte, 60)
			out[0], out[1] = 2, pkt[1]|0x80
			out[3] = f.pcpResult
			copy(out[4:8], pkt[4:8])
			binary.BigEndian.PutUint32(out[8:], f.epoch)
			if pkt[1] == 1 && n >= 60 {
				copy(out[24:44], pkt[24:44]) // nonce, proto, ports
				copy(out[44:60], []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 203, 0, 113, 7})
			}
			f.conn.WriteToUDP(out[:n], from)
		}
	}
}

// startFakeSSDP answers any M-SEARCH with one Location.
func startFakeSSDP(t *testing.T, location string) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !strings.HasPrefix(string(buf[:n]), "M-SEARCH") {
				continue
			}
			resp := "HTTP/1.1 200 OK\r\nEXT:\r\n" +
				"LOCATION: " + location + "\r\n" +
				"SERVER: fake/1.0 UPnP/1.1\r\n" +
				"USN: uuid:t::urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
			conn.WriteToUDP([]byte(resp), from)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func testWAN() PortMapWAN {
	return PortMapWAN{
		IfName:  "",
		Gateway: netip.AddrFrom4([4]byte{127, 0, 0, 1}),
		SelfIP:  netip.AddrFrom4([4]byte{127, 0, 0, 1}),
	}
}

func testClient(pxpPort, ssdpPort int) *portMapClient {
	c := newPortMapClient(nil)
	c.pxpPort, c.ssdpPort, c.localPort = pxpPort, ssdpPort, 0
	c.probeWindow = 500 * time.Millisecond
	c.opTimeout = time.Second
	return c
}

func TestProbeAllProtocols(t *testing.T) {
	pxp := startFakePXP(t)
	desc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, igdXML)
	}))
	t.Cleanup(desc.Close)
	ssdpPort := startFakeSSDP(t, desc.URL+"/rootDesc.xml")

	c := testClient(pxp.port(), ssdpPort)
	probe, err := c.Probe(context.Background(), testWAN())
	if err != nil {
		t.Fatal(err)
	}
	if !probe.PMP || !probe.PCP || !probe.UPnP {
		t.Fatalf("probe missed a protocol: %+v", probe)
	}
	if probe.ExternalIP != netip.AddrFrom4([4]byte{203, 0, 113, 7}) || probe.Epoch.Secs != 100 {
		t.Fatalf("external/epoch: %+v", probe)
	}
	if !strings.Contains(probe.UPnPService.ServiceType, "WANIPConnection:1") {
		t.Fatalf("upnp service: %+v", probe.UPnPService)
	}
	if !strings.HasPrefix(probe.UPnPService.ControlURL, "http://127.0.0.1:") {
		t.Fatalf("control url: %s", probe.UPnPService.ControlURL)
	}
}

func TestProbeDenied(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.pmpResult = 2 // not authorized
	pxp.pcpResult = 2
	c := testClient(pxp.port(), 1) // nothing answers on port 1
	probe, err := c.Probe(context.Background(), testWAN())
	if err != nil {
		t.Fatal(err)
	}
	if probe.Any() || !probe.Denied {
		t.Fatalf("want denied-only probe, got %+v", probe)
	}
}

func TestProbeNothingThere(t *testing.T) {
	c := testClient(1, 1)
	c.probeWindow = 200 * time.Millisecond
	probe, err := c.Probe(context.Background(), testWAN())
	if err != nil {
		t.Fatal(err)
	}
	if probe.Any() || probe.Denied {
		t.Fatalf("silent gateway should probe empty, got %+v", probe)
	}
}

// fakeIGDSoap implements just enough WANIPConnection over httptest.
type fakeIGDSoap struct {
	serviceVersion int // 1 or 2
	faultOnAdd     int // SOAP fault code for AddPortMapping, 0 = succeed
	faultOnce      bool
	reserved       uint16 // AddAnyPortMapping's answer
	extIP          string
	deleted        []string
	added          []string
}

func (f *fakeIGDSoap) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		action := r.Header.Get("SOAPAction")
		svc := fmt.Sprintf("urn:schemas-upnp-org:service:WANIPConnection:%d", f.serviceVersion)
		switch {
		case strings.Contains(action, "GetExternalIPAddress"):
			fmt.Fprintf(w, soapOK(svc, "GetExternalIPAddress", "<NewExternalIPAddress>%s</NewExternalIPAddress>"), f.extIP)
		case strings.Contains(action, "GetStatusInfo"):
			fmt.Fprint(w, soapOK(svc, "GetStatusInfo",
				"<NewConnectionStatus>Connected</NewConnectionStatus><NewUptime>4242</NewUptime>"))
		case strings.Contains(action, "AddAnyPortMapping"):
			f.added = append(f.added, xmlText(body, "NewProtocol")+":"+xmlText(body, "NewExternalPort"))
			fmt.Fprintf(w, soapOK(svc, "AddAnyPortMapping", "<NewReservedPort>%d</NewReservedPort>"), f.reserved)
		case strings.Contains(action, "AddPortMapping"):
			if f.faultOnAdd != 0 {
				code := f.faultOnAdd
				if f.faultOnce {
					f.faultOnAdd = 0
				}
				w.WriteHeader(500)
				fmt.Fprintf(w, soapFault, code)
				return
			}
			f.added = append(f.added, xmlText(body, "NewProtocol")+":"+xmlText(body, "NewExternalPort")+
				":lease="+xmlText(body, "NewLeaseDuration"))
			fmt.Fprint(w, soapOK(svc, "AddPortMapping", ""))
		case strings.Contains(action, "DeletePortMapping"):
			f.deleted = append(f.deleted, xmlText(body, "NewProtocol")+":"+xmlText(body, "NewExternalPort"))
			fmt.Fprint(w, soapOK(svc, "DeletePortMapping", ""))
		default:
			w.WriteHeader(500)
			fmt.Fprintf(w, soapFault, 401)
		}
	}
}

func soapOK(svc, action, inner string) string {
	return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:` + action + `Response xmlns:u="` + svc + `">` + inner + `</u:` + action + `Response>` +
		`</s:Body></s:Envelope>`
}

const soapFault = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
	`<s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>` +
	`<UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode></UPnPError>` +
	`</detail></s:Fault></s:Body></s:Envelope>`

func upnpProbe(svcVersion int, controlURL string) PortMapProbe {
	return PortMapProbe{UPnP: true, SeenAt: time.Now(), UPnPService: UPnPServiceInfo{
		ControlURL:  controlURL,
		ServiceType: fmt.Sprintf("urn:schemas-upnp-org:service:WANIPConnection:%d", svcVersion),
	}}
}

func TestMapPMP(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.grant = func(uint16) uint16 { return 60001 }
	c := testClient(pxp.port(), 1)
	probe := PortMapProbe{PMP: true, SeenAt: time.Now()}
	lease, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Method != "natpmp" || lease.External.Port() != 60001 ||
		lease.External.Addr() != netip.AddrFrom4([4]byte{203, 0, 113, 7}) {
		t.Fatalf("lease: %+v", lease)
	}
	if lease.RenewAfter.After(lease.GoodUntil) || time.Until(lease.GoodUntil) < time.Hour {
		t.Fatalf("lease times: %+v", lease)
	}
	if err := c.Unmap(context.Background(), testWAN(), lease); err != nil {
		t.Fatal(err)
	}
}

func TestMapPMPDenied(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.pmpResult = 2
	c := testClient(pxp.port(), 1)
	_, err := c.Map(context.Background(), testWAN(), PortMapProbe{PMP: true},
		PortMapRequest{Proto: "udp", InternalPort: 51820})
	if !errors.Is(err, ErrPortMapDenied) {
		t.Fatalf("want ErrPortMapDenied, got %v", err)
	}
}

func TestMapPCP(t *testing.T) {
	pxp := startFakePXP(t)
	c := testClient(pxp.port(), 1)
	probe := PortMapProbe{PCP: true, SeenAt: time.Now()}
	lease, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "tcp", InternalPort: 8443, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Method != "pcp" || lease.External.Port() != 8443 || lease.Nonce == ([12]byte{}) {
		t.Fatalf("lease: %+v", lease)
	}
	if err := c.Unmap(context.Background(), testWAN(), lease); err != nil {
		t.Fatal(err)
	}
}

func TestMapPCPNested(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.pcpResult = 12 // ADDRESS_MISMATCH: pcp server behind another NAT
	c := testClient(pxp.port(), 1)
	_, err := c.Map(context.Background(), testWAN(), PortMapProbe{PCP: true},
		PortMapRequest{Proto: "udp", InternalPort: 51820})
	if !errors.Is(err, ErrPortMapNestedNAT) {
		t.Fatalf("want ErrPortMapNestedNAT, got %v", err)
	}
}

func TestMapUPnPv1(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7"}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	lease, err := c.Map(context.Background(), testWAN(), upnpProbe(1, srv.URL+"/ctl"),
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour, Description: "nasnet:wg"})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Method != "upnp" || lease.External.Port() != 51820 || lease.External.Addr().String() != "203.0.113.7" {
		t.Fatalf("lease: %+v", lease)
	}
	if len(igd.added) != 1 || !strings.HasPrefix(igd.added[0], "UDP:51820") {
		t.Fatalf("added: %v", igd.added)
	}
	if err := c.Unmap(context.Background(), testWAN(), lease); err != nil {
		t.Fatal(err)
	}
	if len(igd.deleted) != 1 || igd.deleted[0] != "UDP:51820" {
		t.Fatalf("deleted: %v", igd.deleted)
	}
}

func TestMapUPnPv2AnyPort(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 2, extIP: "203.0.113.7", reserved: 60001}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	lease, err := c.Map(context.Background(), testWAN(), upnpProbe(2, srv.URL+"/ctl"),
		PortMapRequest{Proto: "tcp", InternalPort: 8443, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if lease.External.Port() != 60001 {
		t.Fatalf("reserved port not honoured: %+v", lease)
	}
}

func TestMapUPnPPermanentFallback(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7", faultOnAdd: 725, faultOnce: true}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	lease, err := c.Map(context.Background(), testWAN(), upnpProbe(1, srv.URL+"/ctl"),
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !lease.Permanent {
		t.Fatalf("want permanent lease: %+v", lease)
	}
	if len(igd.added) != 1 || !strings.HasSuffix(igd.added[0], ":lease=0") {
		t.Fatalf("added: %v", igd.added)
	}
	if until := time.Until(lease.GoodUntil); until > 2*time.Hour+time.Minute {
		t.Fatalf("permanent lease must still re-verify: %v", until)
	}
}

func TestMapFallsBackToTheNextProtocol(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.pmpResult = 2 // NAT-PMP is there and says no
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7"}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)

	probe := upnpProbe(1, srv.URL+"/ctl")
	probe.PMP = true
	c := testClient(pxp.port(), 1)
	lease, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatalf("a refusing NAT-PMP must not hide a working IGD: %v", err)
	}
	if lease.Method != "upnp" {
		t.Fatalf("lease: %+v", lease)
	}
}

func TestMapStopsAtNestedNAT(t *testing.T) {
	pxp := startFakePXP(t)
	pxp.pcpResult = 12 // ADDRESS_MISMATCH
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7"}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)

	probe := upnpProbe(1, srv.URL+"/ctl")
	probe.PCP = true
	c := testClient(pxp.port(), 1)
	_, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "udp", InternalPort: 51820})
	if !errors.Is(err, ErrPortMapNestedNAT) {
		t.Fatalf("nested NAT is the gateway's answer, not one protocol's: %v", err)
	}
	if len(igd.added) != 0 {
		t.Fatalf("UPnP was tried behind a dead end: %v", igd.added)
	}
}

func TestPCPRenewalPresentsTheOriginalNonce(t *testing.T) {
	pxp := startFakePXP(t)
	c := testClient(pxp.port(), 1)
	probe := PortMapProbe{PCP: true, SeenAt: time.Now()}
	lease, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	first := pxp.nonce()
	if first != lease.Nonce {
		t.Fatalf("lease kept a different nonce than it sent")
	}

	if _, err := c.Map(context.Background(), testWAN(), probe, PortMapRequest{
		Proto: "udp", InternalPort: 51820, ExternalHint: lease.External.Port(),
		Lifetime: 2 * time.Hour, Renewal: true, Nonce: lease.Nonce,
	}); err != nil {
		t.Fatal(err)
	}
	if pxp.nonce() != first {
		t.Fatal("a renewal with a fresh nonce is NOT_AUTHORIZED on a real PCP server")
	}
}

func TestUPnPRenewalClearsItsOwnConflict(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7", faultOnAdd: 718, faultOnce: true}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	lease, err := c.Map(context.Background(), testWAN(), upnpProbe(1, srv.URL+"/ctl"),
		PortMapRequest{Proto: "udp", InternalPort: 51820, ExternalHint: 51820,
			Lifetime: 2 * time.Hour, Renewal: true})
	if err != nil {
		t.Fatal(err)
	}
	if lease.External.Port() != 51820 {
		t.Fatalf("a renewal must keep the port clients already have: %+v", lease)
	}
	if len(igd.deleted) != 1 || igd.deleted[0] != "UDP:51820" {
		t.Fatalf("the stale row was never cleared: %v", igd.deleted)
	}
}

func TestMapUPnPv2PermanentFallback(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 2, extIP: "203.0.113.7", faultOnAdd: 725, faultOnce: true}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	// AddAnyPortMapping faults the same way on the permanent-lease routers.
	igd.handler()
	c := testClient(1, 1)
	_, err := c.Map(context.Background(), testWAN(), upnpProbe(2, srv.URL+"/ctl"),
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatalf("v2 needs the permanent-lease fallback too: %v", err)
	}
}

func TestUPnPLeasesCarryTheRoutersUptimeAsAnEpoch(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.7"}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)

	probe := upnpProbe(1, srv.URL+"/ctl")
	probe.Epoch = PortMapEpoch{Secs: 4242, At: time.Now()}
	lease, err := c.Map(context.Background(), testWAN(), probe,
		PortMapRequest{Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if !lease.Epoch.Known() || lease.Epoch.Secs != 4242 {
		t.Fatalf("without an epoch a UPnP router's reboot is invisible: %+v", lease.Epoch)
	}
}

func TestProbeReadsTheUPnPExternalAddress(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "203.0.113.9"}
	ctl := httptest.NewServer(igd.handler())
	t.Cleanup(ctl.Close)
	desc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.ReplaceAll(igdXML, "/ctl", ctl.URL+"/ctl"))
	}))
	t.Cleanup(desc.Close)
	ssdpPort := startFakeSSDP(t, desc.URL+"/rootDesc.xml")

	c := testClient(1, ssdpPort)
	probe, err := c.Probe(context.Background(), testWAN())
	if err != nil {
		t.Fatal(err)
	}
	if !probe.UPnP || probe.ExternalIP.String() != "203.0.113.9" {
		t.Fatalf("probe: %+v", probe)
	}
	if !probe.Epoch.Known() || probe.Epoch.Secs != 4242 {
		t.Fatalf("the router's uptime was never read: %+v", probe.Epoch)
	}
}

func TestMapUPnPLeavesNothingBehindWhenTheAddressIsUnusable(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 1, extIP: "0.0.0.0"}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	if _, err := c.Map(context.Background(), testWAN(), upnpProbe(1, srv.URL+"/ctl"),
		PortMapRequest{Proto: "udp", InternalPort: 51820}); err == nil {
		t.Fatal("0.0.0.0 external accepted")
	}
	if len(igd.added) != 0 {
		t.Fatalf("a mapping was created that no lease records: %v", igd.added)
	}
}

func TestUPnPRenewalDropsTheRowItWasMovedOffOf(t *testing.T) {
	igd := &fakeIGDSoap{serviceVersion: 2, extIP: "203.0.113.7", reserved: 60001}
	srv := httptest.NewServer(igd.handler())
	t.Cleanup(srv.Close)
	c := testClient(1, 1)
	lease, err := c.Map(context.Background(), testWAN(), upnpProbe(2, srv.URL+"/ctl"),
		PortMapRequest{Proto: "tcp", InternalPort: 8443, ExternalHint: 8443,
			Lifetime: 2 * time.Hour, Renewal: true})
	if err != nil {
		t.Fatal(err)
	}
	if lease.External.Port() != 60001 {
		t.Fatalf("lease: %+v", lease)
	}
	if len(igd.deleted) != 1 || igd.deleted[0] != "TCP:8443" {
		t.Fatalf("the row we were moved off of was left behind: %v", igd.deleted)
	}
}
