// SPDX-License-Identifier: AGPL-3.0-or-later

package nodecfg_test

import (
	"encoding/json"
	"errors"
	"net/netip"
	"testing"

	"github.com/xtls/xray-core/common"
	xnet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/outbound"
	"github.com/xtls/xray-core/features/routing"
	rs "github.com/xtls/xray-core/features/routing/session"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/nodecfg"
)

// patch builds the sample and lets edit change the generic JSON before the
// result goes back out, for what Input can't say: DNS answers for names that
// don't exist, or another domain strategy.
func patch(t *testing.T, edit func(cfg map[string]any)) []byte {
	t.Helper()
	return patchInput(t, sample(), edit)
}

func patchInput(t *testing.T, in nodecfg.Input, edit func(cfg map[string]any)) []byte {
	t.Helper()
	raw, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	edit(cfg)
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// hosts are the names the routing tests resolve; the node's own resolver is
// never asked.
var hosts = map[string]any{
	"domestic.test":      "5.22.9.9",
	"domestic-v6.test":   "2a01:5ec0::5",
	"foreign.test":       "93.184.216.34",
	"private.test":       "10.9.9.9",
	"loopback.test":      "127.0.0.1",
	"mixed.test":         []string{"93.184.216.34", "5.22.9.9"},
	"mixed-private.test": []string{"93.184.216.34", "10.9.9.9"},
	"nowhere.test":       "#3", // NXDOMAIN
}

func withHosts(cfg map[string]any) { cfg["dns"] = map[string]any{"hosts": hosts} }

// route asks the instance's router where a connection from inbound to target
// would go. "default" means no rule matched and the first outbound takes it.
func route(t *testing.T, inst *core.Instance, inbound, target string) string {
	t.Helper()
	router := inst.GetFeature(routing.RouterType()).(routing.Router)
	picked, err := router.PickRoute(&rs.Context{
		Inbound:  &session.Inbound{Tag: inbound},
		Outbound: &session.Outbound{Target: xnet.TCPDestination(xnet.ParseAddress(target), 443)},
	})
	if errors.Is(err, common.ErrNoClue) {
		return "default"
	}
	if err != nil {
		t.Fatalf("routing %s from %s: %v", target, inbound, err)
	}
	return picked.GetOutboundTag()
}

func TestRoutingDecisions(t *testing.T) {
	inst := load(t, patch(t, withHosts))
	tunnel := sample().Bridge.ReverseTag

	cases := []struct {
		target, want, why string
	}{
		{"5.22.9.9", "direct", "domestic IPv4"},
		{"2.176.9.9", "direct", "domestic IPv4, second range"},
		{"2a01:5ec0:1::1", "direct", "domestic IPv6"},
		{"93.184.216.34", tunnel, "foreign IPv4"},
		{"1.1.1.1", tunnel, "foreign IPv4"},
		{"2606:2800:220:1::1", tunnel, "foreign IPv6"},

		{"10.1.2.3", "blocked", "private 10/8"},
		{"172.16.5.5", "blocked", "private 172.16/12"},
		{"192.168.1.1", "blocked", "private 192.168/16"},
		{"127.0.0.1", "blocked", "loopback, where the Xray API listens"},
		{"169.254.169.254", "blocked", "link-local"},
		{"100.64.1.1", "blocked", "carrier-grade NAT"},
		{"224.0.0.1", "blocked", "multicast"},
		{"0.0.0.0", "blocked", "unspecified"},
		{"::1", "blocked", "IPv6 loopback"},
		{"fe80::1", "blocked", "IPv6 link-local"},
		{"fd12:3456::1", "blocked", "IPv6 unique local"},

		{"domestic.test", "direct", "a name that resolves to a domestic address"},
		{"domestic-v6.test", "direct", "a name that resolves to a domestic IPv6 address"},
		{"foreign.test", tunnel, "a name that resolves abroad"},
		{"private.test", "blocked", "a name that resolves to a private address"},
		{"loopback.test", "blocked", "a name that resolves to loopback"},
		{"mixed.test", "direct", "one domestic address among foreign ones is enough to leave from the node"},
		{"mixed-private.test", "blocked", "one private address among foreign ones blocks the name"},
		{"nowhere.test", tunnel, "a name this node can't resolve; the other end tries"},
	}
	for _, c := range cases {
		if got := route(t, inst, nodecfg.EntryTag, c.target); got != c.want {
			t.Errorf("%s (%s) goes to %q, want %q", c.target, c.why, got, c.want)
		}
	}

	// The API rule is first: nothing that arrives on the api inbound is
	// judged by where it is going.
	for _, target := range []string{"127.0.0.1", "10.1.2.3", "93.184.216.34", "domestic.test"} {
		if got := route(t, inst, "api", target); got != "api" {
			t.Errorf("api traffic to %s goes to %q, want api", target, got)
		}
	}

	// Only the entry is sent into the tunnel. Anything else that turns up, and
	// matches nothing, takes the first outbound.
	if got := route(t, inst, "somewhere-else", "93.184.216.34"); got != "default" {
		t.Errorf("traffic from another inbound goes to %q, want the default outbound", got)
	}
	om := inst.GetFeature(outbound.ManagerType()).(outbound.Manager)
	if h := om.GetDefaultHandler(); h == nil || h.Tag() != "blocked" {
		t.Errorf("default outbound is %v, want blocked", h)
	}
}

// Why the config says IPOnDemand: with IPIfNonMatch the router tries every
// rule on the unresolved name first, and the entry-wide last rule wins, so
// domestic names would ride the tunnel. This pins that behaviour of the Xray
// we build against; if it changes, the domain strategy is worth another look.
func TestIPIfNonMatchWouldSendDomesticNamesIntoTheTunnel(t *testing.T) {
	inst := load(t, patch(t, func(cfg map[string]any) {
		withHosts(cfg)
		cfg["routing"].(map[string]any)["domainStrategy"] = "IPIfNonMatch"
	}))
	tunnel := sample().Bridge.ReverseTag
	if got := route(t, inst, nodecfg.EntryTag, "domestic.test"); got != tunnel {
		t.Fatalf("with IPIfNonMatch domestic.test goes to %q, expected the tunnel %q (the reason for IPOnDemand)", got, tunnel)
	}
	// IP literals don't depend on the strategy
	if got := route(t, inst, nodecfg.EntryTag, "5.22.9.9"); got != "direct" {
		t.Fatalf("with IPIfNonMatch 5.22.9.9 goes to %q, want direct", got)
	}
}

// The embedded list works as a whole, not only the three ranges of the sample.
func TestRoutingWithTheEmbeddedDomesticList(t *testing.T) {
	in := sample()
	in.DomesticCIDRs = nil
	raw, err := nodecfg.Build(in)
	if err != nil {
		t.Fatal(err)
	}
	inst := load(t, raw)

	list := nodecfg.DomesticCIDRs()
	for _, i := range []int{0, len(list) / 2, len(list) - 1} {
		first := firstAddress(t, list[i])
		if got := route(t, inst, nodecfg.EntryTag, first); got != "direct" {
			t.Errorf("%s, the start of %s, goes to %q, want direct", first, list[i], got)
		}
	}
	for _, target := range []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "2001:4860:4860::8888"} {
		if got := route(t, inst, nodecfg.EntryTag, target); got == "direct" {
			t.Errorf("%s leaves from the node", target)
		}
	}
}

func firstAddress(t *testing.T, cidr string) string {
	t.Helper()
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return p.Addr().String()
}
