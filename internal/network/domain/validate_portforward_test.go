package domain

import (
	"strings"
	"testing"
)

func pfInput(newPF PortForward) PortForwardInput {
	return PortForwardInput{
		New:          newPF,
		LANCIDR:      "10.77.0.1/24",
		PanelPort:    9761,
		SSHPort:      22,
		InboundPorts: map[string][]int{"tcp": {443}, "udp": {51820}},
		UplinkKeys:   []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"},
	}
}

func TestValidatePortForward_AcceptsAPlainLANTarget(t *testing.T) {
	vs := ValidatePortForward(pfInput(PortForward{
		UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "10.77.0.5", ToPort: 80, Enabled: true,
	}))
	if Rejected(vs) {
		t.Fatalf("a plain forward was rejected: %+v", vs)
	}
}

// V26 — the target must be inside the LAN, or local for a box-terminated forward.
func TestValidatePortForward_V26TargetOutsideTheLAN(t *testing.T) {
	vs := ValidatePortForward(pfInput(PortForward{
		UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "192.168.50.5", ToPort: 80, Enabled: true,
	}))
	if !Rejected(vs) {
		t.Fatalf("a forward to a non-LAN address was accepted: %+v", vs)
	}
	if firstReject(vs).Rule != "V26" {
		t.Errorf("first reject = %s, want V26", firstReject(vs).Rule)
	}
}

// A forward that terminates on the box is legitimate and is not a LAN address.
func TestValidatePortForward_V26AllowsALoopbackTarget(t *testing.T) {
	vs := ValidatePortForward(pfInput(PortForward{
		UplinkKey: "aa:bb:cc:dd:ee:01", Proto: "tcp", DPort: 8080,
		ToAddr: "127.0.0.1", ToPort: 8080, Enabled: true,
	}))
	if Rejected(vs) {
		t.Fatalf("a box-terminated forward was rejected: %+v", vs)
	}
}

// V27 — collisions with an xray inbound or another forward.
func TestValidatePortForward_V27Collisions(t *testing.T) {
	cases := []struct {
		name  string
		input PortForwardInput
	}{
		{
			name: "collides with an xray inbound on the same proto",
			input: pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
				Proto: "tcp", DPort: 443, ToAddr: "10.77.0.5", ToPort: 443, Enabled: true}),
		},
		{
			name: "collides with a udp inbound",
			input: pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
				Proto: "udp", DPort: 51820, ToAddr: "10.77.0.5", ToPort: 51820, Enabled: true}),
		},
		{
			name: "collides with another forward on the same uplink",
			input: func() PortForwardInput {
				in := pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
					Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.6", ToPort: 80, Enabled: true})
				in.Existing = []PortForward{{ID: 1, UplinkKey: "aa:bb:cc:dd:ee:01",
					Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}}
				return in
			}(),
		},
		{
			name: "an any-uplink forward collides with a per-uplink one",
			input: func() PortForwardInput {
				in := pfInput(PortForward{UplinkKey: "",
					Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.6", ToPort: 80, Enabled: true})
				in.Existing = []PortForward{{ID: 1, UplinkKey: "aa:bb:cc:dd:ee:02",
					Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}}
				return in
			}(),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			vs := ValidatePortForward(c.input)
			r := firstReject(vs)
			if r == nil {
				t.Fatalf("collision accepted: %+v", vs)
			}
			if r.Rule != "V27" {
				t.Errorf("first reject = %s (%s), want V27", r.Rule, r.Message)
			}
		})
	}
}

// A disabled forward cannot collide with anything.
func TestValidatePortForward_V27IgnoresDisabledRows(t *testing.T) {
	in := pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
		Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.6", ToPort: 80, Enabled: true})
	in.Existing = []PortForward{{ID: 1, UplinkKey: "aa:bb:cc:dd:ee:01",
		Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.5", ToPort: 80, Enabled: false}}
	if Rejected(ValidatePortForward(in)) {
		t.Errorf("a disabled row blocked a new forward: %+v", ValidatePortForward(in))
	}
}

// Editing a row must not collide with itself.
func TestValidatePortForward_V27IgnoresTheRowBeingEdited(t *testing.T) {
	pf := PortForward{ID: 1, UplinkKey: "aa:bb:cc:dd:ee:01",
		Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.9", ToPort: 80, Enabled: true}
	in := pfInput(pf)
	in.Existing = []PortForward{{ID: 1, UplinkKey: "aa:bb:cc:dd:ee:01",
		Proto: "tcp", DPort: 8080, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}}
	if Rejected(ValidatePortForward(in)) {
		t.Errorf("a row collided with itself: %+v", ValidatePortForward(in))
	}
}

// V28 — exposing the panel port or :22 to an uplink needs a typed CONFIRM.
func TestValidatePortForward_V28ExposingThePanelOrSSHNeedsConfirm(t *testing.T) {
	for _, port := range []int{9761, 22} {
		in := pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
			Proto: "tcp", DPort: port, ToAddr: "10.77.0.5", ToPort: port, Enabled: true})

		vs := ValidatePortForward(in)
		var sawConfirm bool
		for _, v := range vs {
			if v.Rule == "V28" && v.Level == LevelConfirm {
				sawConfirm = true
				if !strings.Contains(strings.ToUpper(v.Message), "CONFIRM") {
					t.Errorf("V28 must ask for a typed CONFIRM: %q", v.Message)
				}
			}
		}
		if !sawConfirm {
			t.Errorf("port %d produced no V28 confirm: %+v", port, vs)
		}
	}
}

// Taking the panel's own port makes it unreachable there, so: confirm.
func TestValidatePortForward_V28TakingOverThePanelPort(t *testing.T) {
	vs := ValidatePortForward(pfInput(PortForward{UplinkKey: "aa:bb:cc:dd:ee:01",
		Proto: "tcp", DPort: 9761, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}))
	if Rejected(vs) {
		t.Fatalf("should be a confirm, not a reject: %+v", vs)
	}
	found := false
	for _, v := range vs {
		if v.Rule == "V28" && v.Level == LevelConfirm && strings.Contains(v.Message, "panel") {
			found = true
		}
	}
	if !found {
		t.Errorf("no V28 confirm for taking over the panel port: %+v", vs)
	}
}

func TestValidatePortForward_BasicFieldChecks(t *testing.T) {
	cases := []struct {
		name string
		pf   PortForward
	}{
		{"bad proto", PortForward{Proto: "sctp", DPort: 80, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}},
		{"dport zero", PortForward{Proto: "tcp", DPort: 0, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}},
		{"dport too big", PortForward{Proto: "tcp", DPort: 70000, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}},
		{"bad target", PortForward{Proto: "tcp", DPort: 80, ToAddr: "not-an-ip", ToPort: 80, Enabled: true}},
		{"unknown uplink", PortForward{UplinkKey: "zz:zz:zz:zz:zz:zz", Proto: "tcp",
			DPort: 80, ToAddr: "10.77.0.5", ToPort: 80, Enabled: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !Rejected(ValidatePortForward(pfInput(c.pf))) {
				t.Errorf("%s was accepted", c.name)
			}
		})
	}
}
