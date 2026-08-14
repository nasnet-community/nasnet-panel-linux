package system

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

// Liveness has to come from the handshake: the link reports "up" whether or not
// anything is on the other end.
func TestWGStatus_ConnectedIsAHandshakeAge(t *testing.T) {
	for name, tc := range map[string]struct {
		st   *WGStatus
		want bool
	}{
		"never handshook": {&WGStatus{}, false},
		"just now":        {&WGStatus{LastHandshake: time.Now()}, true},
		"a minute ago":    {&WGStatus{LastHandshake: time.Now().Add(-time.Minute)}, true},
		"past the limit":  {&WGStatus{LastHandshake: time.Now().Add(-StaleHandshakeAfter - time.Second)}, false},
		"no device":       {nil, false},
	} {
		if got := tc.st.Connected(); got != tc.want {
			t.Errorf("%s: Connected() = %v", name, got)
		}
	}
}

func TestFakeWGDevice_AbsentUntilEnsured(t *testing.T) {
	ctx := context.Background()
	f := &FakeWGDevice{}

	if _, err := f.Status(ctx); err != ErrNoWGDevice {
		t.Fatalf("err = %v, want ErrNoWGDevice", err)
	}

	cfg := WGApplyConfig{
		PrivateKey:   "priv",
		Endpoint:     netip.MustParseAddrPort("185.65.135.1:51820"),
		Address:      netip.MustParsePrefix("10.66.0.2/32"),
		FirewallMark: 0x02000000,
	}
	if err := f.Ensure(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if f.Applied == nil || f.Applied.FirewallMark != 0x02000000 {
		t.Fatalf("applied = %+v", f.Applied)
	}
	if _, err := f.Status(ctx); err != nil {
		t.Fatalf("status after ensure: %v", err)
	}

	if err := f.UpdateEndpoint(ctx, netip.MustParseAddrPort("1.2.3.4:51820")); err != nil {
		t.Fatal(err)
	}
	if f.Endpoint.String() != "1.2.3.4:51820" {
		t.Errorf("endpoint = %v", f.Endpoint)
	}

	if err := f.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Status(ctx); err != ErrNoWGDevice {
		t.Errorf("the device survived deletion: %v", err)
	}
	// Deleting twice is what a rollback does after a failed apply.
	if err := f.Delete(ctx); err != nil {
		t.Errorf("second delete: %v", err)
	}
}
