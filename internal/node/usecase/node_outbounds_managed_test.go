package usecase

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/netmark"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// The panel reporting fewer outbounds than the running config has is what sent
// somebody looking for outbounds that were never rows. These are the two that
// decide which uplink traffic leaves by, so they are the worst ones to hide.
func TestManagedOutbounds_MatchWhatTheBuilderEmits(t *testing.T) {
	got := managedOutbounds(7, nil)

	// Build a real router-mode config and compare tag for tag, mark for mark.
	blob, err := xray.NewFullConfigBuilder(&domain.Node{}).WithRouterMode(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Outbounds []struct {
			Tag            string `json:"tag"`
			Protocol       string `json:"protocol"`
			StreamSettings struct {
				Sockopt struct {
					Mark uint32 `json:"mark"`
				} `json:"sockopt"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal([]byte(blob), &cfg); err != nil {
		t.Fatal(err)
	}

	if len(got) == 0 {
		t.Fatal("no managed outbounds")
	}
	if len(cfg.Outbounds) < len(got) {
		t.Fatalf("the config has %d outbounds, fewer than the %d managed ones", len(cfg.Outbounds), len(got))
	}
	// Order is load-bearing: xray falls back to the first outbound, so these
	// have to lead, and foreign has to lead them.
	for i, want := range got {
		have := cfg.Outbounds[i]
		if have.Tag != want.Tag {
			t.Errorf("position %d: config has %q, the list says %q", i, have.Tag, want.Tag)
		}
		if have.Protocol != want.Protocol {
			t.Errorf("%s: protocol %q vs %q", want.Tag, have.Protocol, want.Protocol)
		}
		if want.SockoptSettings == nil || have.StreamSettings.Sockopt.Mark != want.SockoptSettings.Mark {
			t.Errorf("%s: config marks 0x%08x, the list says %+v",
				want.Tag, have.StreamSettings.Sockopt.Mark, want.SockoptSettings)
		}
	}
	if got[0].SockoptSettings.Mark != netmark.GroupMark(netmark.GroupForeign) {
		t.Errorf("the first managed outbound is not the foreign one: %+v", got[0])
	}
}

// They are not rows, and the UI has to know that or it offers an edit that the
// next config build silently throws away.
func TestManagedOutbounds_AreFlaggedAndHaveNoRow(t *testing.T) {
	for _, o := range managedOutbounds(7, nil) {
		if !o.Managed {
			t.Errorf("%s is not flagged as managed", o.Tag)
		}
		if o.ID != 0 {
			t.Errorf("%s claims row id %d", o.Tag, o.ID)
		}
		if o.NodeID != 7 {
			t.Errorf("%s is on node %d, want 7", o.Tag, o.NodeID)
		}
		if o.Remark == "" {
			t.Errorf("%s has nothing to say for itself in a list", o.Tag)
		}
	}
}

// Off the router they do not exist, so listing them would be a lie.
func TestManagedOutbounds_OnlyInRouterMode(t *testing.T) {
	blob, err := xray.NewFullConfigBuilder(&domain.Node{}).WithRouterMode(false).Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range xray.RouterOutbounds(nil) {
		if strings.Contains(blob, g.Tag) {
			t.Errorf("%s emitted with router mode off", g.Tag)
		}
	}
}

// The list and the config have to agree on the per-WAN outbounds too, or the
// panel hides the very ones that pick the WAN.
func TestManagedOutbounds_ViasMatchTheBuilder(t *testing.T) {
	vias := []xray.RouterWAN{
		{Slot: "secondary", UplinkIndex: 2, Label: "Starlink"},
		{Slot: "secondary2", UplinkIndex: 3, Label: "Secondary 2"},
	}
	got := managedOutbounds(7, vias)

	wantTags := map[string]bool{
		xray.TagDirectForeign:                  true,
		xray.TagDirectDomestic:                 true,
		xray.TagDirectForeignVia("secondary"):  true,
		xray.TagDirectForeignVia("secondary2"): true,
	}
	if len(got) != len(wantTags) {
		t.Fatalf("got %d managed outbounds, want %d", len(got), len(wantTags))
	}
	for _, o := range got {
		if !wantTags[o.Tag] {
			t.Errorf("unexpected managed outbound %q", o.Tag)
		}
		if !o.Managed || o.SockoptSettings == nil || o.SockoptSettings.Mark == 0 {
			t.Errorf("%q is missing its managed flag or its mark: %+v", o.Tag, o)
		}
	}
}
