package domain

import (
	"strings"
	"testing"
)

func rule(mut func(*PortMapRule)) PortMapRule {
	r := PortMapRule{Proto: "udp", Port: 51820, Enabled: true}
	if mut != nil {
		mut(&r)
	}
	return r
}

func TestValidatePortMapRule(t *testing.T) {
	base := PortMapRuleInput{
		New: rule(nil), PanelPort: 9761, SSHPort: 22,
		UplinkKeys: []string{"mac:aa"},
	}
	if vs := ValidatePortMapRule(base); len(vs) != 0 {
		t.Fatalf("clean rule got verdicts: %+v", vs)
	}

	cases := []struct {
		name string
		in   PortMapRuleInput
		rule string
		lvl  Level
	}{
		{"bad proto", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.Proto = "icmp" })}, "V43", LevelReject},
		{"port zero", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.Port = 0 })}, "V43", LevelReject},
		{"hint out of range", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.ExternalHint = 70000 })}, "V43", LevelReject},
		{"unknown uplink", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.UplinkKey = "mac:zz" }), UplinkKeys: []string{"mac:aa"}}, "V43", LevelReject},
		{"collides with inbound", PortMapRuleInput{New: rule(nil), InboundPorts: map[string][]int{"udp": {51820}}}, "V44", LevelReject},
		{"collides with rule", PortMapRuleInput{New: rule(nil), Existing: []PortMapRule{{ID: 9, Proto: "udp", Port: 51820, Enabled: true}}}, "V44", LevelReject},
		{"panel port", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.Proto = "tcp"; r.Port = 9761 }), PanelPort: 9761}, "V45", LevelConfirm},
		{"ssh port", PortMapRuleInput{New: rule(func(r *PortMapRule) { r.Proto = "tcp"; r.Port = 22 }), SSHPort: 22}, "V45", LevelConfirm},
	}
	for _, tc := range cases {
		vs := ValidatePortMapRule(tc.in)
		if len(vs) == 0 {
			t.Fatalf("%s: expected %s, got none", tc.name, tc.rule)
		}
		if vs[0].Rule != tc.rule || vs[0].Level != tc.lvl {
			t.Fatalf("%s: got %+v", tc.name, vs[0])
		}
	}

	// Same (proto, port) on two different named uplinks does not collide.
	ok := PortMapRuleInput{
		New:        rule(func(r *PortMapRule) { r.UplinkKey = "mac:aa" }),
		Existing:   []PortMapRule{{ID: 9, Proto: "udp", Port: 51820, UplinkKey: "mac:bb", Enabled: true}},
		UplinkKeys: []string{"mac:aa", "mac:bb"},
	}
	if vs := ValidatePortMapRule(ok); len(vs) != 0 {
		t.Fatalf("distinct uplinks should not collide: %+v", vs)
	}

	// Editing a row must not collide with itself.
	self := PortMapRuleInput{
		New:      rule(func(r *PortMapRule) { r.ID = 9 }),
		Existing: []PortMapRule{{ID: 9, Proto: "udp", Port: 51820, Enabled: true}},
	}
	if vs := ValidatePortMapRule(self); len(vs) != 0 {
		t.Fatalf("self-collision: %+v", vs)
	}
}

// Turning a rule off is how an operator undoes a mistake. It can never need a
// confirmation, and it cannot collide with anything: it asks for nothing.
func TestValidatePortMapRuleDisabledRowIsAlwaysAllowed(t *testing.T) {
	cases := []PortMapRuleInput{
		{New: rule(func(r *PortMapRule) { r.Proto = "tcp"; r.Port = 9761; r.Enabled = false }), PanelPort: 9761},
		{New: rule(func(r *PortMapRule) { r.Proto = "tcp"; r.Port = 22; r.Enabled = false }), SSHPort: 22},
		{New: rule(func(r *PortMapRule) { r.Enabled = false }), InboundPorts: map[string][]int{"udp": {51820}}},
	}
	for i, in := range cases {
		if vs := ValidatePortMapRule(in); len(vs) != 0 {
			t.Fatalf("case %d: switching a rule off asked for a verdict: %+v", i, vs)
		}
	}
}

// A forwarded port does not terminate here, so the mapping opens the LAN
// device behind it. Legal behind two NATs, never a surprise.
func TestValidatePortMapRuleWarnsWhenThePortIsForwardedOnward(t *testing.T) {
	in := PortMapRuleInput{
		New: rule(func(r *PortMapRule) { r.Proto = "tcp"; r.Port = 8080 }),
		Forwards: []PortForward{
			{Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true},
		},
	}
	vs := ValidatePortMapRule(in)
	if len(vs) != 1 || vs[0].Rule != "V46" || vs[0].Level != LevelConfirm {
		t.Fatalf("verdicts: %+v", vs)
	}
	if !strings.Contains(vs[0].Message, "10.77.0.5") {
		t.Fatalf("the operator is not told where it lands: %q", vs[0].Message)
	}

	// A disabled forward forwards nothing.
	in.Forwards[0].Enabled = false
	if vs := ValidatePortMapRule(in); len(vs) != 0 {
		t.Fatalf("disabled forward warned anyway: %+v", vs)
	}
}
