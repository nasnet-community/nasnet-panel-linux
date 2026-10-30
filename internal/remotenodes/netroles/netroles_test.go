// SPDX-License-Identifier: AGPL-3.0-or-later

package netroles

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func env(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestTheDefaultsAreTheManifestsAddresses(t *testing.T) {
	d := Defaults()
	if d.Downloads.String() != "192.168.50.16" || d.Exit.String() != "192.168.50.17" || d.Nodes.String() != "192.168.50.18" {
		t.Fatalf("defaults = %v %v %v", d.Downloads, d.Exit, d.Nodes)
	}
	got, err := FromEnv(env())
	if err != nil || got != d {
		t.Fatalf("an empty environment = %+v, %v", got, err)
	}
	if got.Downloads.Local || got.Exit.Local || got.Nodes.Local {
		t.Error("FromEnv says an address is held without looking")
	}
}

func TestFromEnv(t *testing.T) {
	got, err := FromEnv(env(EnvDownloads, " 10.9.0.16 ", EnvExit, "10.9.0.17", EnvNodes, "::ffff:10.9.0.18"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Downloads.String() != "10.9.0.16" || got.Exit.String() != "10.9.0.17" || got.Nodes.String() != "10.9.0.18" {
		t.Errorf("addresses = %v %v %v", got.Downloads, got.Exit, got.Nodes)
	}
	// one set, the others default
	got, err = FromEnv(env(EnvNodes, "172.20.0.5"))
	if err != nil || got.Nodes.String() != "172.20.0.5" || got.Exit.String() != "192.168.50.17" {
		t.Errorf("one address set = %+v, %v", got, err)
	}
	// the loopback is allowed to be both, as a dev run has no second address
	if _, err := FromEnv(env(EnvExit, "127.0.0.1", EnvNodes, "127.0.0.1")); err != nil {
		t.Errorf("a dev run's loopback: %v", err)
	}

	for name, c := range map[string]struct {
		e    func(string) string
		want string
	}{
		"a name":                  {env(EnvNodes, "nodes.example"), EnvNodes},
		"empty octet":             {env(EnvExit, "10.0.0."), EnvExit},
		"unspecified":             {env(EnvDownloads, "0.0.0.0"), EnvDownloads},
		"unspecified v6":          {env(EnvDownloads, "::"), EnvDownloads},
		"multicast":               {env(EnvExit, "224.0.0.1"), EnvExit},
		"a zone":                  {env(EnvNodes, "fe80::1%eth0"), EnvNodes},
		"a port":                  {env(EnvNodes, "10.0.0.1:80"), EnvNodes},
		"exit is the nodes' one":  {env(EnvExit, "10.1.1.1", EnvNodes, "10.1.1.1"), "both"},
		"default exit and nodes":  {env(EnvNodes, "192.168.50.17"), "both"},
		"exit set to the default": {env(EnvExit, "192.168.50.18"), "both"},
	} {
		if _, err := FromEnv(c.e); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %q", name, err, c.want)
		}
	}
}

func TestCheckMarksWhatIsHeldAndWarnsOnceForTheRest(t *testing.T) {
	in := Defaults()
	held := func(ip netip.Addr) bool { return ip == in.Exit.IP }
	var warned []string
	out := in.Check(held, func(s string) { warned = append(warned, s) })

	if out.Exit.Local != true || out.Nodes.Local || out.Downloads.Local {
		t.Errorf("marks = %v %v %v", out.Downloads.Local, out.Exit.Local, out.Nodes.Local)
	}
	if in.Exit.Local {
		t.Error("Check changed the set it was called on")
	}
	if len(warned) != 2 {
		t.Fatalf("warnings = %q, want one each for the downloads and the nodes' address", warned)
	}
	for i, want := range []string{EnvDownloads + " 192.168.50.16", EnvNodes + " 192.168.50.18"} {
		if !strings.Contains(warned[i], want) || !strings.Contains(warned[i], "not assigned to any interface") {
			t.Errorf("warning %d = %q", i, warned[i])
		}
	}
	if !strings.Contains(warned[1], "bridge stays stopped") {
		t.Errorf("the nodes' warning doesn't say what happens to the bridge: %q", warned[1])
	}

	// all held: nothing to say, and a nil warn is fine
	all := in.Check(func(netip.Addr) bool { return true }, func(s string) { t.Errorf("warned: %s", s) })
	if !all.Downloads.Local || !all.Exit.Local || !all.Nodes.Local {
		t.Errorf("marks = %+v", all)
	}
	_ = in.Check(func(netip.Addr) bool { return false }, nil)
}

func TestHeldFindsTheLoopbackAndNotADocumentationAddress(t *testing.T) {
	if _, err := net.InterfaceAddrs(); err != nil {
		t.Skipf("no interface list here: %v", err)
	}
	held := Held()
	if !held(netip.MustParseAddr("127.0.0.1")) {
		t.Error("127.0.0.1 is not held")
	}
	if !held(netip.MustParseAddr("::ffff:127.0.0.1")) {
		t.Error("the mapped form of 127.0.0.1 is not held")
	}
	if held(netip.MustParseAddr("192.0.2.77")) {
		t.Error("an address from the documentation range is held")
	}
}

// A role whose address is held dials from it. 192.0.2.1 is nobody's, so
// binding it fails, and that is how a test can tell a bound dial from a plain
// one without a second interface.
func TestDialContextBindsOnlyAHeldAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback here: %v", err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nobody := netip.MustParseAddr("192.0.2.1")

	if _, err := (Addr{IP: nobody, Local: true}).DialContext(ctx, "tcp", l.Addr().String()); err == nil {
		t.Fatal("a dial that claims to bind an address nobody holds worked, so it was not bound")
	}
	c, err := Addr{IP: nobody}.DialContext(ctx, "tcp", l.Addr().String())
	if err != nil {
		t.Fatalf("an unbound dial: %v", err)
	}
	c.Close()

	lo := Addr{IP: netip.MustParseAddr("127.0.0.1"), Local: true}
	c, err = lo.DialContext(ctx, "tcp4", l.Addr().String())
	if err != nil {
		t.Fatalf("a dial bound to the loopback: %v", err)
	}
	if got := c.LocalAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Errorf("the connection came from %s", got)
	}
	c.Close()

	// a datagram network gets a datagram address, which Dial insists on
	u, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp here: %v", err)
	}
	defer u.Close()
	conn, err := lo.DialContext(ctx, "udp", u.LocalAddr().String())
	if err != nil {
		t.Fatalf("a udp dial bound to the loopback: %v", err)
	}
	if got := conn.LocalAddr().(*net.UDPAddr).IP.String(); got != "127.0.0.1" {
		t.Errorf("the datagrams come from %s", got)
	}
	conn.Close()
	if _, err := (Addr{IP: nobody, Local: true}).DialContext(ctx, "udp", u.LocalAddr().String()); err == nil {
		t.Error("a udp dial from an address nobody holds worked")
	}
}

func TestResolverLooksNamesUpFromAHeldAddressOnly(t *testing.T) {
	if r := (Addr{IP: netip.MustParseAddr("192.0.2.1")}).Resolver(); r != net.DefaultResolver {
		t.Error("an unbound role has a resolver of its own")
	}
	r := Addr{IP: netip.MustParseAddr("127.0.0.1"), Local: true}.Resolver()
	if r == net.DefaultResolver || !r.PreferGo || r.Dial == nil {
		t.Errorf("a bound role's resolver = %+v: the system's would not ask the address", r)
	}
}
