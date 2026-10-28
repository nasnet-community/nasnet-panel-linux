package usecase

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/geoip"
)

func cacheRanges(t *testing.T, u *networkUsecase, v4 ...string) {
	t.Helper()
	if err := geoip.SaveCachedRanges(u.rangesCachePath(), &geoip.CachedRanges{
		FetchedAt: time.Now(), Source: "test", V4: v4,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestIsDomesticAddr_UsesTheLiveList(t *testing.T) {
	u := newRangesUsecase(t, nil)
	cacheRanges(t, u, "198.51.100.0/24", "203.0.113.0/26")
	_, _, err := u.domesticSets(context.Background())
	skipWithoutGeoIP(t, err)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		ip   string
		want bool
	}{
		{"198.51.100.7", true},
		{"::ffff:198.51.100.7", true}, // a mapped address is the same host
		{"203.0.113.10", true},
		{"203.0.113.70", false},
		{"8.8.8.8", false},
	} {
		if got := u.IsDomesticAddr(context.Background(), netip.MustParseAddr(c.ip)); got != c.want {
			t.Errorf("IsDomesticAddr(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}

// A refresh drops the compiled sets; the parsed prefixes must go with them.
func TestIsDomesticAddr_FollowsARefresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		for i := 0; i < 2105; i++ {
			fmt.Fprintf(&b, "10.%d.%d.0/24\n", i/256, i%256)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	u := newRangesUsecase(t, srv)
	cacheRanges(t, u, "198.51.100.0/24")
	_, _, err := u.domesticSets(context.Background())
	skipWithoutGeoIP(t, err)
	if !u.IsDomesticAddr(context.Background(), netip.MustParseAddr("198.51.100.7")) {
		t.Fatal("the cached list was not used")
	}

	if err := u.RefreshDomesticRanges(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if u.IsDomesticAddr(context.Background(), netip.MustParseAddr("198.51.100.7")) {
		t.Error("an address only the old list had is still domestic")
	}
	if !u.IsDomesticAddr(context.Background(), netip.MustParseAddr("10.0.5.9")) {
		t.Error("an address in the refreshed list is not domestic")
	}
}
