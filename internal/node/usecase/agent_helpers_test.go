package usecase

import (
	"context"
	"net/netip"
	"slices"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

func TestAgentMarks(t *testing.T) {
	domestic := httpclient.EgressDomestic.Mark()
	foreign := httpclient.EgressForeign.Mark()
	inIran := func(_ context.Context, ip netip.Addr) bool { return ip.String() == "5.160.0.1" }

	cases := []struct {
		name   string
		source func(context.Context, netip.Addr) bool
		ip     string
		want   []uint32
	}{
		{"domestic node tries the domestic lines, then the tunnel", inIran, "5.160.0.1", []uint32{domestic, foreign}},
		{"foreign node never touches a domestic line", inIran, "203.0.113.5", []uint32{foreign}},
		{"unwired source keeps the tunnel", nil, "5.160.0.1", []uint32{foreign}},
	}
	for _, c := range cases {
		u := &nodeUsecase{domesticAddr: c.source}
		if got := u.agentMarks(context.Background(), netip.MustParseAddr(c.ip)); !slices.Equal(got, c.want) {
			t.Errorf("%s: marks = %#x, want %#x", c.name, got, c.want)
		}
	}
}

func TestResolveAgentHost_LiteralSkipsDNS(t *testing.T) {
	ips, err := resolveAgentHost(context.Background(), "2001:db8::1")
	if err != nil || len(ips) != 1 || ips[0] != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("resolveAgentHost = %v, %v", ips, err)
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"amd64", "amd64"},
		{"AMD64", "amd64"},
		{"x86_64", "amd64"},
		{"X86_64", "amd64"},
		{"arm64", "arm64"},
		{"ARM64", "arm64"},
		{"aarch64", "arm64"},
		{"AArch64", "arm64"},
		{"", "amd64"},
		{"unknown", "amd64"},
		{"  amd64  ", "amd64"},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := normalizeArch(c.in)
			if got != c.want {
				t.Fatalf("normalizeArch(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
