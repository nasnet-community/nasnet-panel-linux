package product

import (
	"reflect"
	"testing"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
)

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func TestApplyHostOverrides_NilSafe(t *testing.T) {
	// Both arguments nil: must not panic.
	ApplyHostOverrides(nil, nil)
	ApplyHostOverrides(&InboundDetail{}, nil)
	ApplyHostOverrides(nil, &nodeDomain.Host{})
}

func TestApplyHostOverrides_BasicFields(t *testing.T) {
	d := InboundDetail{
		Network:    "tcp",
		Security:   "tls",
		NodeIP:     "1.1.1.1",
		PublicPort: 443,
	}
	port := 8443
	host := &nodeDomain.Host{
		ID:       7,
		Priority: 5,
		Remark:   "custom",
		Address:  "edge.example.com",
		Port:     &port,
	}
	ApplyHostOverrides(&d, host)
	if d.HostID != 7 || d.Priority != 5 {
		t.Fatalf("expected host id/priority propagated; got %+v", d)
	}
	if d.Remark != "custom" || !d.RemarkIsTemplate {
		t.Fatalf("expected custom remark with template flag; got %+v", d)
	}
	if d.NodeIP != "edge.example.com" || d.PublicPort != 8443 {
		t.Fatalf("expected address/port overridden; got %+v", d)
	}
}

func TestApplyHostOverrides_DefaultRemarkWhenEmpty(t *testing.T) {
	d := InboundDetail{}
	ApplyHostOverrides(&d, &nodeDomain.Host{})
	if d.Remark != DefaultRemarkTemplate {
		t.Fatalf("expected DefaultRemarkTemplate, got %q", d.Remark)
	}
	if !d.RemarkIsTemplate {
		t.Fatal("expected RemarkIsTemplate=true")
	}
}

func TestApplyHostOverrides_TLSSecurityRouting(t *testing.T) {
	d := InboundDetail{Security: "tls"}
	ApplyHostOverrides(&d, &nodeDomain.Host{
		SNI:         "tls.example.com",
		Fingerprint: "chrome",
		ALPN:        "h2,http/1.1",
	})
	if d.TLSSni != "tls.example.com" {
		t.Fatalf("TLSSni = %q", d.TLSSni)
	}
	if d.TLSFingerprint != "chrome" {
		t.Fatalf("TLSFingerprint = %q", d.TLSFingerprint)
	}
	if !reflect.DeepEqual(d.TLSALPN, []string{"h2", "http/1.1"}) {
		t.Fatalf("TLSALPN = %v", d.TLSALPN)
	}
	if d.RealitySNI != "" || d.RealityFingerprint != "" {
		t.Fatalf("expected reality fields untouched; got sni=%q fp=%q", d.RealitySNI, d.RealityFingerprint)
	}
}

func TestApplyHostOverrides_RealityRoutingViaHostSecurity(t *testing.T) {
	// Base inbound has security="tls" but host overrides to "reality".
	d := InboundDetail{Security: "tls"}
	ApplyHostOverrides(&d, &nodeDomain.Host{
		Security:    "reality",
		SNI:         "www.cloudflare.com",
		Fingerprint: "firefox",
	})
	if d.Security != "reality" {
		t.Fatalf("Security = %q", d.Security)
	}
	if d.RealitySNI != "www.cloudflare.com" || d.RealityFingerprint != "firefox" {
		t.Fatalf("expected reality fields populated; got %+v", d)
	}
	if d.TLSSni != "" || d.TLSFingerprint != "" {
		t.Fatalf("expected TLS fields untouched; got %+v", d)
	}
}

func TestApplyHostOverrides_AllowInsecure(t *testing.T) {
	d := InboundDetail{}
	ApplyHostOverrides(&d, &nodeDomain.Host{AllowInsecure: boolPtr(true)})
	if d.AllowInsecure == nil || !*d.AllowInsecure {
		t.Fatalf("AllowInsecure not propagated")
	}
}

func TestApplyHostOverrides_Fragment(t *testing.T) {
	d := InboundDetail{}
	ApplyHostOverrides(&d, &nodeDomain.Host{
		FragmentSettings: &nodeDomain.HostFragmentSettings{
			Packets:  "tlshello",
			Length:   "100-200",
			Interval: "10-20",
		},
	})
	if d.Fragment == nil {
		t.Fatal("Fragment not propagated")
	}
	if d.Fragment.Packets != "tlshello" || d.Fragment.Length != "100-200" || d.Fragment.Interval != "10-20" {
		t.Fatalf("Fragment fields wrong: %+v", d.Fragment)
	}
}

func TestApplyHostOverrides_GRPCPathAliasesServiceName(t *testing.T) {
	d := InboundDetail{Network: "grpc"}
	ApplyHostOverrides(&d, &nodeDomain.Host{Path: "MyGrpcSvc"})
	if d.TransportPath != "MyGrpcSvc" {
		t.Fatalf("TransportPath = %q", d.TransportPath)
	}
	if d.TransportServiceName != "MyGrpcSvc" {
		t.Fatalf("expected gRPC alias to TransportServiceName; got %q", d.TransportServiceName)
	}
}

func TestApplyHostOverrides_NonGRPCPathDoesNotTouchServiceName(t *testing.T) {
	d := InboundDetail{Network: "ws", TransportServiceName: "untouched"}
	ApplyHostOverrides(&d, &nodeDomain.Host{Path: "/edge"})
	if d.TransportPath != "/edge" {
		t.Fatalf("TransportPath = %q", d.TransportPath)
	}
	if d.TransportServiceName != "untouched" {
		t.Fatalf("expected TransportServiceName untouched for non-grpc; got %q", d.TransportServiceName)
	}
}

func TestApplyHostOverrides_ModeAndHeaderType(t *testing.T) {
	d := InboundDetail{Network: "xhttp"}
	ApplyHostOverrides(&d, &nodeDomain.Host{Mode: "stream-up", HeaderType: "http"})
	if d.TransportMode != "stream-up" {
		t.Fatalf("TransportMode = %q", d.TransportMode)
	}
	if d.TransportHeaderType != "http" {
		t.Fatalf("TransportHeaderType = %q", d.TransportHeaderType)
	}
}

func TestApplyHostOverrides_HostHostHeader(t *testing.T) {
	d := InboundDetail{}
	ApplyHostOverrides(&d, &nodeDomain.Host{Host: "cdn.example.com"})
	if d.TransportHost != "cdn.example.com" {
		t.Fatalf("TransportHost = %q", d.TransportHost)
	}
}
