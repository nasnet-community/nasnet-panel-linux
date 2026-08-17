package system

import "testing"

const nftJSONFixture = `{"nftables": [
  {"metainfo": {"version": "1.0.9", "json_schema_version": 1}},
  {"table": {"family": "inet", "name": "nasnet", "handle": 5}},
  {"chain": {"family": "inet", "table": "nasnet", "name": "mangle_pre", "handle": 1, "type": "filter", "hook": "prerouting", "prio": -150, "policy": "accept"}},
  {"chain": {"family": "inet", "table": "nasnet", "name": "killswitch_out", "handle": 2, "type": "filter", "hook": "output", "prio": 10, "policy": "accept"}},
  {"set": {"family": "inet", "name": "ir_v4", "table": "nasnet", "type": "ipv4_addr", "handle": 3, "flags": ["interval"]}},
  {"counter": {"family": "inet", "name": "cnt_domestic", "table": "nasnet", "handle": 4, "packets": 120, "bytes": 90210}},
  {"counter": {"family": "inet", "name": "cnt_killswitch", "table": "nasnet", "handle": 6, "packets": 3, "bytes": 180}},
  {"rule": {"family": "inet", "table": "nasnet", "chain": "mangle_pre", "handle": 7, "expr": []}}
]}`

func TestParseNftObjects(t *testing.T) {
	got, err := parseNftObjects([]byte(nftJSONFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chains) != 2 || got.Chains[0] != "mangle_pre" || got.Chains[1] != "killswitch_out" {
		t.Fatalf("chains: %v", got.Chains)
	}
	if len(got.Sets) != 1 || got.Sets[0] != "ir_v4" {
		t.Fatalf("sets: %v", got.Sets)
	}
	if c := got.Counters["cnt_domestic"]; c.Packets != 120 || c.Bytes != 90210 {
		t.Fatalf("counter: %+v", c)
	}
	if len(got.Counters) != 2 {
		t.Fatalf("counters: %v", got.Counters)
	}
}

func TestParseNftObjectsIgnoresOtherTables(t *testing.T) {
	other := `{"nftables": [
	  {"chain": {"family": "ip", "table": "filter", "name": "INPUT", "handle": 1}},
	  {"set": {"family": "inet", "name": "x", "table": "notours", "handle": 2}}
	]}`
	got, err := parseNftObjects([]byte(other))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Chains) != 0 || len(got.Sets) != 0 {
		t.Fatalf("leaked foreign objects: %+v", got)
	}
}

func TestFakeNftSetContains(t *testing.T) {
	f := &FakeNft{Members: map[string][]string{"ir_v4": {"5.144.128.1"}}}
	ok, err := f.SetContains(t.Context(), "ir_v4", "5.144.128.1")
	if err != nil || !ok {
		t.Fatalf("want member, got %v %v", ok, err)
	}
	ok, _ = f.SetContains(t.Context(), "ir_v4", "1.1.1.1")
	if ok {
		t.Fatal("1.1.1.1 must not be a member")
	}
}
