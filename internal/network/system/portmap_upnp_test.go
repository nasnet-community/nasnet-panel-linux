package system

import (
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSSDP(t *testing.T) {
	pkt := string(ssdpSearchPacket(stIGD))
	for _, want := range []string{
		"M-SEARCH * HTTP/1.1\r\n",
		"HOST: 239.255.255.250:1900\r\n",
		"MAN: \"ssdp:discover\"\r\n",
		"ST: " + stIGD + "\r\n",
	} {
		if !strings.Contains(pkt, want) {
			t.Fatalf("packet missing %q:\n%s", want, pkt)
		}
	}
	if !strings.HasSuffix(pkt, "\r\n\r\n") {
		t.Fatal("packet must end with a blank line")
	}

	resp := "HTTP/1.1 200 OK\r\n" +
		"CACHE-CONTROL: max-age=120\r\n" +
		"location: http://192.168.1.1:5000/rootDesc.xml\r\n" +
		"SERVER: FakeOS UPnP/1.1 MiniUPnPd/2.3\r\n" +
		"USN: uuid:x::urn:schemas-upnp-org:device:InternetGatewayDevice:2\r\n\r\n"
	r, ok := parseSSDPResponse([]byte(resp))
	if !ok || r.Location != "http://192.168.1.1:5000/rootDesc.xml" || !strings.Contains(r.USN, "InternetGatewayDevice:2") {
		t.Fatalf("parse: %+v ok=%v", r, ok)
	}
	if _, ok := parseSSDPResponse([]byte("NOTIFY * HTTP/1.1\r\n\r\n")); ok {
		t.Fatal("non-200 parsed")
	}
}

const igdXML = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
 <device>
  <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
  <deviceList><device>
   <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
   <deviceList><device>
    <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
    <serviceList>
     <service>
      <serviceType>urn:schemas-upnp-org:service:WANPPPConnection:1</serviceType>
      <controlURL>/ctl/PPP</controlURL>
     </service>
     <service>
      <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
      <controlURL>/ctl/IPConn</controlURL>
     </service>
    </serviceList>
   </device></deviceList>
  </device></deviceList>
 </device>
</root>`

func TestIGDServices(t *testing.T) {
	// Location claimed a host that is not the gateway (floating Location);
	// control URLs must come out repointed at the gateway.
	base, _ := url.Parse("http://10.99.99.99:5000/rootDesc.xml")
	gw := netip.AddrFrom4([4]byte{192, 168, 1, 1})
	svcs := parseIGDServices([]byte(igdXML), base, gw)
	if len(svcs) != 2 {
		t.Fatalf("services: %+v", svcs)
	}
	best, ok := selectUPnPService(svcs)
	if !ok || !strings.Contains(best.ServiceType, "WANIPConnection:1") {
		t.Fatalf("preference picked %+v", best)
	}
	if best.ControlURL != "http://192.168.1.1:5000/ctl/IPConn" {
		t.Fatalf("control url not repointed: %s", best.ControlURL)
	}
	if _, ok := selectUPnPService(nil); ok {
		t.Fatal("empty selection succeeded")
	}
}

func TestSOAP(t *testing.T) {
	env := string(soapEnvelope("urn:schemas-upnp-org:service:WANIPConnection:1", "AddPortMapping",
		[][2]string{{"NewRemoteHost", ""}, {"NewExternalPort", "51820"}, {"NewProtocol", "UDP"}}))
	for _, want := range []string{
		`<u:AddPortMapping xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`,
		"<NewExternalPort>51820</NewExternalPort>",
		"<NewProtocol>UDP</NewProtocol>",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("envelope missing %q:\n%s", want, env)
		}
	}
	if got := soapActionHeader("urn:x:service:WANIPConnection:1", "AddPortMapping"); got != `"urn:x:service:WANIPConnection:1#AddPortMapping"` {
		t.Fatalf("soapaction: %s", got)
	}

	fault := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring>
<detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">
<errorCode>725</errorCode><errorDescription>OnlyPermanentLeasesSupported</errorDescription>
</UPnPError></detail></s:Fault></s:Body></s:Envelope>`
	if got := soapFaultCode([]byte(fault)); got != 725 {
		t.Fatalf("fault code = %d", got)
	}

	okBody := `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:GetExternalIPAddressResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">
<NewExternalIPAddress>203.0.113.7</NewExternalIPAddress>
</u:GetExternalIPAddressResponse></s:Body></s:Envelope>`
	if got := xmlText([]byte(okBody), "NewExternalIPAddress"); got != "203.0.113.7" {
		t.Fatalf("xmlText = %q", got)
	}
	if soapFaultCode([]byte(okBody)) != 0 {
		t.Fatal("fault code on a non-fault body")
	}
}

func TestSSDPKeepsIGDsAndDropsTheRest(t *testing.T) {
	igd := "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.1.1:5000/d.xml\r\n" +
		"USN: uuid:x::urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n" +
		"ST: urn:schemas-upnp-org:device:InternetGatewayDevice:1\r\n\r\n"
	tv := "HTTP/1.1 200 OK\r\nLOCATION: http://192.168.1.44:8060/d.xml\r\n" +
		"USN: uuid:y::urn:schemas-upnp-org:device:MediaRenderer:1\r\n" +
		"ST: urn:schemas-upnp-org:device:MediaRenderer:1\r\nSERVER: Roku/9\r\n\r\n"

	r, ok := parseSSDPResponse([]byte(igd))
	if !ok || !r.LooksLikeIGD() {
		t.Fatalf("igd response rejected: %+v", r)
	}
	r, ok = parseSSDPResponse([]byte(tv))
	if !ok {
		t.Fatal("valid response failed to parse")
	}
	if r.LooksLikeIGD() {
		t.Fatal("a media renderer must not cost us a description fetch")
	}
}

func TestRepointToGateway(t *testing.T) {
	gw := netip.MustParseAddr("192.168.1.1")
	for _, tc := range []struct{ in, want string }{
		{"http://192.168.1.254:5000/desc.xml", "http://192.168.1.1:5000/desc.xml"},
		{"http://192.168.1.254/desc.xml", "http://192.168.1.1:80/desc.xml"},
	} {
		if got := repointToGateway(tc.in, gw); got != tc.want {
			t.Fatalf("repoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestXMLTextStopsAtAnEmptyElement(t *testing.T) {
	body := []byte(`<r><NewExternalIPAddress></NewExternalIPAddress><NewOther>203.0.113.7</NewOther></r>`)
	if got := xmlText(body, "NewExternalIPAddress"); got != "" {
		t.Fatalf("an empty element must not borrow the next one's text, got %q", got)
	}
	body = []byte(`<r><NewExternalIPAddress>203.0.113.7</NewExternalIPAddress></r>`)
	if got := xmlText(body, "NewExternalIPAddress"); got != "203.0.113.7" {
		t.Fatalf("text lost: %q", got)
	}
}

func TestEpochRebooted(t *testing.T) {
	base := time.Now()
	prev := PortMapEpoch{Secs: 300, At: base}
	cases := []struct {
		name string
		next PortMapEpoch
		want bool
	}{
		// The plain "smaller than before" test misses this one: the router
		// rebooted, and its fresh uptime is still the larger number.
		{"reboot with a larger epoch", PortMapEpoch{Secs: 600, At: base.Add(time.Hour)}, true},
		{"same boot, clock kept up", PortMapEpoch{Secs: 300 + 3600, At: base.Add(time.Hour)}, false},
		{"reboot, counter restarted", PortMapEpoch{Secs: 5, At: base.Add(time.Minute)}, true},
		{"a second of jitter is not a reboot", PortMapEpoch{Secs: 299, At: base}, false},
		{"nothing known yet", PortMapEpoch{Secs: 10, At: base.Add(time.Minute)}, true},
	}
	for _, tc := range cases {
		if got := prev.Rebooted(tc.next); got != tc.want {
			t.Fatalf("%s: Rebooted = %v, want %v", tc.name, got, tc.want)
		}
	}
	if (PortMapEpoch{}).Rebooted(PortMapEpoch{Secs: 1, At: base}) {
		t.Fatal("an unknown baseline cannot detect a reboot")
	}
}
