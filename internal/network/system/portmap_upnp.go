package system

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const (
	ssdpMulticast = "239.255.255.250:1900"
	stSSDPAll     = "ssdp:all"
	// Asked for on its own too: some devices answer ssdp:all with only their
	// first descriptor, which may not be the IGD.
	stIGD = "urn:schemas-upnp-org:device:InternetGatewayDevice:1"
)

// The only services worth having. v2 resolves port conflicts itself with
// AddAnyPortMapping; PPP is the DSL-era fallback.
var upnpPreference = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

func ssdpSearchPacket(st string) []byte {
	return []byte("M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + ssdpMulticast + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 2\r\n" +
		"ST: " + st + "\r\n\r\n")
}

type ssdpResponse struct {
	Location string
	Server   string
	USN      string
	ST       string
	// FromGateway: the answer came from the gateway itself, which outranks
	// whatever a random LAN box claims to be.
	FromGateway bool
}

// LooksLikeIGD keeps the description fetches off the TVs and media servers
// that also answer ssdp:all.
func (r ssdpResponse) LooksLikeIGD() bool {
	hay := r.USN + " " + r.ST + " " + r.Server
	for _, want := range []string{"InternetGatewayDevice", "WANConnectionDevice", "WANIPConnection", "WANPPPConnection"} {
		if strings.Contains(hay, want) {
			return true
		}
	}
	return false
}

func parseSSDPResponse(b []byte) (ssdpResponse, bool) {
	// An HTTP response over UDP. ReadResponse handles the case-mangled headers
	// real devices send.
	rd := bufio.NewReader(bytes.NewReader(b))
	resp, err := http.ReadResponse(rd, nil)
	if err != nil || resp.StatusCode != 200 {
		return ssdpResponse{}, false
	}
	defer resp.Body.Close()
	r := ssdpResponse{
		Location: resp.Header.Get("Location"),
		Server:   resp.Header.Get("Server"),
		USN:      resp.Header.Get("USN"),
		ST:       resp.Header.Get("St"),
	}
	if r.Location == "" {
		return ssdpResponse{}, false
	}
	return r, true
}

type igdXMLDevice struct {
	DeviceType string `xml:"deviceType"`
	Services   []struct {
		ServiceType string `xml:"serviceType"`
		ControlURL  string `xml:"controlURL"`
	} `xml:"serviceList>service"`
	Devices []igdXMLDevice `xml:"deviceList>device"`
}

// parseIGDServices walks the device tree, repointing every control URL at the
// gateway: Locations float on some routers, and the kill switch only exempts
// the gateway address.
func parseIGDServices(body []byte, base *url.URL, gateway netip.Addr) []UPnPServiceInfo {
	var root struct {
		Device igdXMLDevice `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil
	}
	var out []UPnPServiceInfo
	var walk func(d igdXMLDevice)
	walk = func(d igdXMLDevice) {
		for _, s := range d.Services {
			if s.ControlURL == "" {
				continue
			}
			ref, err := url.Parse(s.ControlURL)
			if err != nil {
				continue
			}
			u := base.ResolveReference(ref)
			port := u.Port()
			if port == "" {
				port = "80"
			}
			u.Host = gateway.String() + ":" + port
			out = append(out, UPnPServiceInfo{ControlURL: u.String(), ServiceType: s.ServiceType})
		}
		for _, child := range d.Devices {
			walk(child)
		}
	}
	walk(root.Device)
	return out
}

func selectUPnPService(svcs []UPnPServiceInfo) (UPnPServiceInfo, bool) {
	for _, want := range upnpPreference {
		for _, s := range svcs {
			if s.ServiceType == want {
				return s, true
			}
		}
	}
	return UPnPServiceInfo{}, false
}

func soapEnvelope(serviceType, action string, args [][2]string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?>` + "\n")
	b.WriteString(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&b, `<u:%s xmlns:u="%s">`, action, serviceType)
	for _, kv := range args {
		var esc bytes.Buffer
		_ = xml.EscapeText(&esc, []byte(kv[1]))
		fmt.Fprintf(&b, "<%s>%s</%s>", kv[0], esc.String(), kv[0])
	}
	fmt.Fprintf(&b, "</u:%s></s:Body></s:Envelope>", action)
	return []byte(b.String())
}

func soapActionHeader(serviceType, action string) string {
	return `"` + serviceType + "#" + action + `"`
}

// xmlText returns the text of the first element with that local name. SOAP
// replies vary too much across routers for strict structs to survive.
func xmlText(body []byte, local string) string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	depth := -1
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if depth >= 0 {
				depth++
			} else if t.Name.Local == local {
				depth = 1
			}
		case xml.EndElement:
			if depth >= 0 {
				depth--
				if depth == 0 {
					// The element closed without text: it is empty, and the
					// next element's text is somebody else's answer.
					return ""
				}
			}
		case xml.CharData:
			if depth == 1 {
				if text := strings.TrimSpace(string(t)); text != "" {
					return text
				}
			}
		}
	}
}

func soapFaultCode(body []byte) int {
	if !bytes.Contains(body, []byte("Fault")) {
		return 0
	}
	n, _ := strconv.Atoi(xmlText(body, "errorCode"))
	return n
}
