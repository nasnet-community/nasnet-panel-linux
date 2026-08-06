package xray

import (
	"testing"

	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
)

func routerBuilder(t *testing.T, routerMode bool) *FullConfigBuilder {
	t.Helper()
	return NewFullConfigBuilder(&nodeDomain.Node{}).WithRouterMode(routerMode)
}

func outboundTags(outs []map[string]interface{}) []string {
	tags := make([]string, 0, len(outs))
	for _, o := range outs {
		t, _ := o["tag"].(string)
		tags = append(tags, t)
	}
	return tags
}

func findOutbound(outs []map[string]interface{}, tag string) map[string]interface{} {
	for _, o := range outs {
		if t, _ := o["tag"].(string); t == tag {
			return o
		}
	}
	return nil
}

func sockoptMark(t *testing.T, out map[string]interface{}) uint32 {
	t.Helper()
	ss, ok := out["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("outbound has no streamSettings: %+v", out)
	}
	so, ok := ss["sockopt"].(map[string]interface{})
	if !ok {
		t.Fatalf("outbound has no sockopt: %+v", ss)
	}
	m, ok := so["mark"].(uint32)
	if !ok {
		t.Fatalf("sockopt.mark is %T, want uint32", so["mark"])
	}
	return m
}

// The first outbound becomes the implicit default, so ordering is load-bearing.
func TestBuildOutbounds_RouterModeEmitsForeignFirst(t *testing.T) {
	outs := routerBuilder(t, true).buildOutbounds()

	tags := outboundTags(outs)
	fi, di := -1, -1
	for i, tag := range tags {
		switch tag {
		case TagDirectForeign:
			fi = i
		case TagDirectDomestic:
			di = i
		}
	}
	if fi < 0 || di < 0 {
		t.Fatalf("router-mode outbounds missing; tags = %v", tags)
	}
	if fi > di {
		t.Errorf("direct-domestic (%d) precedes direct-foreign (%d); the first outbound "+
			"becomes the default fallback, so a rule-less request would leave via the "+
			"domestic ISP and disclose the operator's real address", di, fi)
	}
	if fi != 0 {
		t.Errorf("direct-foreign is at index %d, want 0; tags = %v", fi, tags)
	}
}

func TestBuildOutbounds_RouterModeMarksMatchTheGroupSelectors(t *testing.T) {
	outs := routerBuilder(t, true).buildOutbounds()

	if got, want := sockoptMark(t, findOutbound(outs, TagDirectForeign)),
		netmark.GroupMark(netmark.GroupForeign); got != want {
		t.Errorf("direct-foreign mark = 0x%08x, want 0x%08x", got, want)
	}
	if got, want := sockoptMark(t, findOutbound(outs, TagDirectDomestic)),
		netmark.GroupMark(netmark.GroupDomestic); got != want {
		t.Errorf("direct-domestic mark = 0x%08x, want 0x%08x", got, want)
	}
}

// A mark naming an uplink would make failover rewrite this config, restarting xray.
func TestBuildOutbounds_RouterModeMarksCarryNoPinField(t *testing.T) {
	outs := routerBuilder(t, true).buildOutbounds()
	for _, tag := range []string{TagDirectForeign, TagDirectDomestic} {
		m := sockoptMark(t, findOutbound(outs, tag))
		if netmark.Pin(m) != 0 {
			t.Errorf("%s carries an ingress pin: 0x%08x", tag, m)
		}
		if netmark.Tier(m) != 0 {
			t.Errorf("%s carries a tier: 0x%08x", tag, m)
		}
	}
}

// Docker never sets the flag, so off must change nothing.
func TestBuildOutbounds_RouterModeOffIsUnchanged(t *testing.T) {
	outs := routerBuilder(t, false).buildOutbounds()
	for _, tag := range []string{TagDirectForeign, TagDirectDomestic} {
		if findOutbound(outs, tag) != nil {
			t.Errorf("%s emitted with router mode off", tag)
		}
	}
	if findOutbound(outs, "direct") == nil {
		t.Error("the plain direct outbound disappeared")
	}
}

// Tier outbounds share the mark word in a different field, so both must survive.
func TestBuildOutbounds_RouterModeCoexistsWithTierOutbounds(t *testing.T) {
	outs := routerBuilder(t, true).buildOutbounds()
	if findOutbound(outs, "blocked") == nil {
		t.Error("the blackhole outbound disappeared")
	}
	if findOutbound(outs, "direct") == nil {
		t.Error("the plain direct outbound disappeared")
	}
}
