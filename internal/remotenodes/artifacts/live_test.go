// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build live

package artifacts

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

// These go to the real GitHub, with the real keys and the real pin, so they
// are left out of a normal run. Use:
//
//	go test -tags=live -run Live -v ./internal/remotenodes/artifacts

// Until v1.3.0 is out, the latest release is too old to use. Getting that
// answer still means release.json was fetched and its signature checked
// against the keys in pkg/signing, and when v1.3.0 is out this fetches the
// agent for real.
func TestLiveLatestRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 3*time.Minute)
	defer cancel()
	for _, arch := range []string{"amd64", "arm64"} {
		a, err := FetchAgent(ctx, NewClient(netip.Addr{}), Source{}, arch)
		if errors.Is(err, ErrAgentTooOld) {
			t.Logf("%s: release.json verified; %v", arch, err)
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		t.Logf("%s: %s, %d bytes gzipped, %d unpacked", arch, a.Version, len(a.Gz), a.PlainSize)
		if sum256(a.Gz) != a.GzSHA256 || len(a.Sig) != 64 {
			t.Errorf("%s: %+v", arch, a)
		}
	}
}

func TestLiveXray(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Minute)
	defer cancel()
	for _, arch := range []string{"amd64", "arm64"} {
		started := time.Now()
		x, err := FetchXray(ctx, NewClient(netip.Addr{}), arch)
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if x.Version != pin.Version || x.PlainSHA256 != pin.BinarySHA256[arch] || sum256(gunzip(t, x.Gz)) != pin.BinarySHA256[arch] {
			t.Errorf("%s: %s %s", arch, x.Version, x.PlainSHA256)
		}
		t.Logf("%s: Xray %s, %d bytes unpacked, %d gzipped, in %v", arch, x.Version, x.PlainSize, len(x.Gz), time.Since(started).Round(time.Millisecond))
	}
}
