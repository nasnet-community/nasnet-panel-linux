//go:build linux

package system

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The netns drill. Two halves of one binary: the gateway half runs inside a
// namespace and plays the ISP router, the client half runs outside and is the
// real PortMapper — real sockets, real SO_BINDTODEVICE, real SO_MARK. Both
// stay skipped unless the drill script asks for them, so `go test ./...` on a
// laptop never notices.
//
//	PORTMAP_DRILL_GW=<listen-ip>   run the fake gateway until killed
//	PORTMAP_DRILL=<gateway-ip>     run the client against it
//	PORTMAP_DRILL_IF=<ifname>      bind the client's sockets to this uplink
//	PORTMAP_DRILL_SELF=<ip>        the client's own address
//	PORTMAP_DRILL_NO_PMP=1         gateway answers UPnP only, to drill fallback
const drillExternalIP = "203.0.113.7"

func TestDrillGateway(t *testing.T) {
	listen := os.Getenv("PORTMAP_DRILL_GW")
	if listen == "" {
		t.Skip("not the drill gateway")
	}
	gw := &drillGateway{ip: listen, noPMP: os.Getenv("PORTMAP_DRILL_NO_PMP") == "1"}
	if err := gw.start(); err != nil {
		t.Fatal(err)
	}
	fmt.Println("drill gateway up on", listen)
	select {} // the script kills us
}

func TestDrillClient(t *testing.T) {
	gwIP := os.Getenv("PORTMAP_DRILL")
	if gwIP == "" {
		t.Skip("not the drill client")
	}
	wan := PortMapWAN{
		IfName:  os.Getenv("PORTMAP_DRILL_IF"),
		Gateway: netip.MustParseAddr(gwIP),
		SelfIP:  netip.MustParseAddr(os.Getenv("PORTMAP_DRILL_SELF")),
	}
	pm := NewPortMapper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	probe, err := pm.Probe(ctx, wan)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe: pmp=%v pcp=%v upnp=%v external=%s", probe.PMP, probe.PCP, probe.UPnP, probe.ExternalIP)
	if !probe.Any() {
		t.Fatal("nothing answered over the wire")
	}
	if probe.ExternalIP.String() != drillExternalIP {
		t.Fatalf("external address: %s", probe.ExternalIP)
	}
	if !probe.UPnP {
		t.Fatal("SSDP discovery found no IGD across a real link")
	}

	lease, err := pm.Map(ctx, wan, probe, PortMapRequest{
		Proto: "udp", InternalPort: 51820, Lifetime: 2 * time.Hour, Description: "nasnet:drill"})
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	t.Logf("lease: %s/%d -> %s via %s", lease.Proto, lease.InternalPort, lease.External, lease.Method)
	if lease.External.Addr().String() != drillExternalIP {
		t.Fatalf("lease external: %s", lease.External)
	}
	if !lease.Epoch.Known() {
		t.Fatal("no epoch captured, so a reboot would go unnoticed")
	}

	// A renewal is the interesting one: same port, and for PCP the same nonce.
	renewed, err := pm.Map(ctx, wan, probe, PortMapRequest{
		Proto: "udp", InternalPort: 51820, ExternalHint: lease.External.Port(),
		Lifetime: 2 * time.Hour, Renewal: true, Nonce: lease.Nonce,
		Description: "nasnet:drill"})
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed.External.Port() != lease.External.Port() {
		t.Fatalf("renewal moved the port: %s -> %s", lease.External, renewed.External)
	}

	if err := pm.Unmap(ctx, wan, renewed); err != nil {
		t.Fatalf("unmap: %v", err)
	}
	t.Log("mapped, renewed and released over a real uplink")
}

// drillGateway speaks NAT-PMP and a minimal UPnP IGD on one address.
type drillGateway struct {
	ip    string
	noPMP bool

	mu      sync.Mutex
	mapped  map[string]bool
	started time.Time
}

func (g *drillGateway) start() error {
	g.mapped = map[string]bool{}
	g.started = time.Now()

	ln, err := net.Listen("tcp", g.ip+":0")
	if err != nil {
		return err
	}
	httpPort := ln.Addr().(*net.TCPAddr).Port
	go http.Serve(ln, g.httpHandler(httpPort)) //nolint:errcheck // drill only

	if !g.noPMP {
		pxp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(g.ip), Port: 5351})
		if err != nil {
			return err
		}
		go g.servePXP(pxp)
	}
	ssdp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(g.ip), Port: 1900})
	if err != nil {
		return err
	}
	go g.serveSSDP(ssdp, httpPort)
	return nil
}

func (g *drillGateway) epoch() uint32 { return uint32(time.Since(g.started).Seconds()) }

func (g *drillGateway) servePXP(conn *net.UDPConn) {
	ext := net.ParseIP(drillExternalIP).To4()
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt := buf[:n]
		if n < 2 || pkt[0] != 0 {
			continue // PCP is not part of this drill
		}
		switch pkt[1] {
		case 0:
			out := make([]byte, 12)
			out[1] = 0x80
			binary.BigEndian.PutUint32(out[4:], g.epoch())
			copy(out[8:], ext)
			_, _ = conn.WriteToUDP(out, from)
		case 1, 2:
			if n < 12 {
				continue
			}
			internal := binary.BigEndian.Uint16(pkt[4:6])
			external := binary.BigEndian.Uint16(pkt[6:8])
			lifetime := binary.BigEndian.Uint32(pkt[8:12])
			if external == 0 {
				external = internal
			}
			key := fmt.Sprintf("%d:%d", pkt[1], internal)
			g.mu.Lock()
			if lifetime == 0 {
				delete(g.mapped, key)
			} else {
				g.mapped[key] = true
			}
			g.mu.Unlock()
			out := make([]byte, 16)
			out[1] = 0x80 | pkt[1]
			binary.BigEndian.PutUint32(out[4:], g.epoch())
			binary.BigEndian.PutUint16(out[8:], internal)
			binary.BigEndian.PutUint16(out[10:], external)
			binary.BigEndian.PutUint32(out[12:], lifetime)
			_, _ = conn.WriteToUDP(out, from)
		}
	}
}

func (g *drillGateway) serveSSDP(conn *net.UDPConn, httpPort int) {
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !strings.HasPrefix(string(buf[:n]), "M-SEARCH") {
			continue
		}
		resp := "HTTP/1.1 200 OK\r\nEXT:\r\n" +
			fmt.Sprintf("LOCATION: http://%s:%d/rootDesc.xml\r\n", g.ip, httpPort) +
			"SERVER: drill/1.0 UPnP/1.1\r\n" +
			"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n" +
			"USN: uuid:drill::urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
		_, _ = conn.WriteToUDP([]byte(resp), from)
	}
}

func (g *drillGateway) httpHandler(port int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rootDesc.xml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0"><device>
<deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType><deviceList><device>
<deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType><serviceList><service>
<serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
<controlURL>http://%s:%d/ctl</controlURL></service></serviceList>
</device></deviceList></device></root>`, g.ip, port)
	})
	mux.HandleFunc("/ctl", func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("SOAPAction")
		const svc = "urn:schemas-upnp-org:service:WANIPConnection:1"
		reply := func(name, inner string) {
			fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">`+
				`<s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body></s:Envelope>`,
				name, svc, inner, name)
		}
		switch {
		case strings.Contains(action, "GetExternalIPAddress"):
			reply("GetExternalIPAddress", "<NewExternalIPAddress>"+drillExternalIP+"</NewExternalIPAddress>")
		case strings.Contains(action, "GetStatusInfo"):
			reply("GetStatusInfo", fmt.Sprintf(
				"<NewConnectionStatus>Connected</NewConnectionStatus><NewUptime>%d</NewUptime>", g.epoch()))
		case strings.Contains(action, "AddPortMapping"):
			reply("AddPortMapping", "")
		case strings.Contains(action, "DeletePortMapping"):
			reply("DeletePortMapping", "")
		default:
			w.WriteHeader(500)
		}
	})
	return mux
}
