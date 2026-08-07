package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

// No zero value means "unset", so forgetting to classify a feature is a compile
// error, not a silent egress via the domestic ISP.
func TestEgressGroup_HasNoUnsetZeroValue(t *testing.T) {
	if uint32(EgressForeign) == 0 {
		t.Error("EgressForeign is the zero value; an unset group would silently mean foreign")
	}
	if uint32(EgressDomestic) == 0 {
		t.Error("EgressDomestic is the zero value")
	}
	if EgressForeign == EgressDomestic {
		t.Fatal("the two groups are indistinguishable")
	}
}

func TestEgressGroup_MarksMatchTheRoutingPolicy(t *testing.T) {
	if got, want := EgressForeign.Mark(), netmark.GroupMark(netmark.GroupForeign); got != want {
		t.Errorf("EgressForeign.Mark() = 0x%08x, want 0x%08x", got, want)
	}
	if got, want := EgressDomestic.Mark(), netmark.GroupMark(netmark.GroupDomestic); got != want {
		t.Errorf("EgressDomestic.Mark() = 0x%08x, want 0x%08x", got, want)
	}
}

func TestDefaultGroupFor(t *testing.T) {
	// The control plane must not disclose the operator's domestic address.
	for _, feat := range []Feature{FeatureGeofiles, FeatureXrayBinary, FeatureGitHubAPI, FeatureTelegram} {
		if got := DefaultGroupFor(feat); got != EgressForeign {
			t.Errorf("DefaultGroupFor(%q) = %v, want foreign", feat, got)
		}
	}
	// ACME HTTP-01 must leave by the advertised address or validation fails.
	if got := DefaultGroupFor(FeatureACME); got != EgressAdvertised {
		t.Errorf("DefaultGroupFor(ACME) = %v, want advertised", got)
	}
}

// Docker has one veth, so a mark would route nothing.
func TestClientFor_NoControlHookWhenRouterModeIsOff(t *testing.T) {
	f := NewFactory()
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	if c == nil {
		t.Fatal("nil client")
	}
	if f.markFor(EgressForeign) != 0 {
		t.Error("a mark was computed with router mode off")
	}
}

func TestSetRouterMode_ResolvesTheAdvertisedGroup(t *testing.T) {
	f := NewFactory()
	f.SetRouterMode(true, EgressDomestic)
	if got, want := f.markFor(EgressAdvertised), EgressDomestic.Mark(); got != want {
		t.Errorf("advertised mark = 0x%08x, want the domestic mark 0x%08x", got, want)
	}
	if got, want := f.markFor(EgressForeign), EgressForeign.Mark(); got != want {
		t.Errorf("foreign mark = 0x%08x, want 0x%08x", got, want)
	}
}

// The SOCKS5 path is not the only one that needs a route.
func TestClientFor_RouterModeSetsControlOnTheDirectTransport(t *testing.T) {
	f := NewFactory()
	f.SetRouterMode(true, EgressDomestic)
	c := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)

	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", c.Transport)
	}
	if tr.DialContext == nil {
		t.Error("router mode did not install a marking dialer")
	}
}

// Reaching the proxy is itself an egress, so that dial must carry the mark too.
func TestClientFor_ProxyPathIsAlsoMarked(t *testing.T) {
	f := NewFactory()
	f.Update(Config{
		ProxyURL: "socks5://127.0.0.1:1080",
		Enabled:  map[Feature]bool{FeatureGeofiles: true},
	})

	plain := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)
	f.SetRouterMode(true, EgressDomestic)
	marked := f.ClientFor(FeatureGeofiles, EgressForeign, 5*time.Second)

	if plain.Transport == marked.Transport {
		t.Error("router mode reused the unmarked proxy transport; the dial to the " +
			"proxy would leave unmarked and find no default route")
	}
}
