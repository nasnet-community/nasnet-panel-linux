package mtprotonode

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func payload(users ...*pb.CoreUser) *pb.CoreStatePayload {
	return &pb.CoreStatePayload{
		CoreType: "mtproto", InboundId: "7", Enabled: true, Listen: "0.0.0.0", Port: 443,
		EgressMode: "hub", Settings: []byte(`{"tls_domain":" Mirror.Example.ir ","log_level":"debug"}`), Users: users,
	}
}

func u(email, username string, enabled bool) *pb.CoreUser {
	return &pb.CoreUser{Email: email, Username: username, Secret: strings.Repeat("ab", 16), Enabled: enabled, KLimit: 2}
}

var extras = Extras{Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}, MaxStreams: 2500}

func TestStateCarriesPolicyAndNormalisesTheDisguise(t *testing.T) {
	user := u("alice@example.com", "alice", true)
	user.ExpiresAtUnix = time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC).Unix()
	user.RateLimitUpBps, user.RateLimitDownBps = 1000, 2000
	st, err := StateFromPayload(payload(user), extras)
	if err != nil {
		t.Fatal(err)
	}
	if st.Disguise != "mirror.example.ir" || st.Port != 443 || st.MaxStreams != 2500 || len(st.DCNets) != 1 || st.DCNets[0] != "203.0.113.0/24" {
		t.Fatalf("state = %+v", st)
	}
	got := st.Users[0]
	if got.Name != "alice@example.com" || !got.Enabled || got.MaxIPs != 2 || got.RateUpBps != 1000 || got.RateDownBps != 2000 ||
		!got.ExpiresAt.Equal(time.Unix(user.ExpiresAtUnix, 0)) {
		t.Fatalf("user = %+v", got)
	}
	// telemt settings travel as an object the agent parses itself
	if m, ok := st.Telemt.(map[string]any); !ok || m["log_level"] != "debug" {
		t.Fatalf("telemt settings = %#v", st.Telemt)
	}
}

func TestUserIdentityFallsBackToUsername(t *testing.T) {
	st, _ := StateFromPayload(payload(u("", "bob", true)), extras)
	if st.Users[0].Name != "bob" {
		t.Fatalf("name = %q", st.Users[0].Name)
	}
}

func TestRevIsStableAndOrderIndependent(t *testing.T) {
	a, _ := StateFromPayload(payload(u("a", "a", true), u("b", "b", true)), extras)
	b, _ := StateFromPayload(payload(u("b", "b", true), u("a", "a", true)), extras)
	if a.Rev == "" || a.Rev != b.Rev {
		t.Fatalf("revs %q vs %q", a.Rev, b.Rev)
	}
	c, _ := StateFromPayload(payload(u("a", "a", true), u("b", "b", false)), extras)
	if c.Rev == a.Rev {
		t.Fatal("a disabled user did not change the revision")
	}
	d := extras
	d.Prefixes = append([]netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}, extras.Prefixes...)
	e, _ := StateFromPayload(payload(u("a", "a", true), u("b", "b", true)), d)
	if e.Rev == a.Rev {
		t.Fatal("a changed allowlist did not change the revision")
	}
}

func TestDisabledInboundKeepsNoUsers(t *testing.T) {
	p := payload(u("a", "a", true))
	p.Enabled = false
	st, err := StateFromPayload(p, extras)
	if err != nil || len(st.Users) != 0 {
		t.Fatalf("state=%+v err=%v", st, err)
	}
}

func TestStateRejectsWhatAnMTProtoNodeCannotRun(t *testing.T) {
	p := payload()
	p.CoreType = "ssh"
	if _, err := StateFromPayload(p, extras); err == nil || !strings.Contains(err.Error(), "only run MTProto") {
		t.Errorf("ssh core: %v", err)
	}
	p = payload()
	p.Settings = []byte(`{"tls_domain":""}`)
	if _, err := StateFromPayload(p, extras); err == nil || !strings.Contains(err.Error(), "tls_domain") {
		t.Errorf("no domain: %v", err)
	}
	p = payload()
	p.Settings = []byte(`{not json`)
	if _, err := StateFromPayload(p, extras); err == nil {
		t.Error("garbage settings accepted")
	}
}

func TestPortDefaultsTo443(t *testing.T) {
	p := payload()
	p.Port = 0
	st, _ := StateFromPayload(p, extras)
	if st.Port != 443 {
		t.Fatalf("port = %d", st.Port)
	}
}
