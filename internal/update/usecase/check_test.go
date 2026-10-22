package usecase

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

// testKey makes the panel trust a fresh key for one test.
func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := signing.PublicKeys
	signing.PublicKeys = []ed25519.PublicKey{pub}
	t.Cleanup(func() { signing.PublicKeys = old })
	return priv
}

type fakeFetcher struct {
	files map[string][]byte
	down  bool
}

func (f *fakeFetcher) Get(_ context.Context, url string, limit int64) ([]byte, string, error) {
	if f.down {
		return nil, "", errors.New("unreachable")
	}
	b, ok := f.files[url]
	if !ok {
		return nil, RouteDirect, errMissing
	}
	return b, RouteDirect, nil
}

// publish puts a signed release.json for version at tag ("" = latest).
func (f *fakeFetcher) publish(t *testing.T, key ed25519.PrivateKey, tag string, m release.Manifest) {
	t.Helper()
	if m.Schema == 0 {
		m.Schema = release.Schema
	}
	if m.Assets == nil {
		m.Assets = map[string]release.Asset{release.BundleAsset("amd64"): {SHA256: strings.Repeat("a", 64), Size: 100}}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	f.files[ReleaseURL(tag, release.ManifestName)] = data
	f.files[ReleaseURL(tag, release.ManifestName+release.SigSuffix)] = []byte(signing.EncodeSig(ed25519.Sign(key, data)))
}

func (f *fakeFetcher) feed(tags ...string) {
	var b strings.Builder
	b.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom">`)
	for _, tag := range tags {
		b.WriteString(`<entry><link rel="alternate" href="https://github.com/` + Repo + `/releases/tag/` + tag + `"/><title>` + tag + `</title></entry>`)
	}
	b.WriteString(`</feed>`)
	f.files[feedURL] = []byte(b.String())
}

func TestCheckOffersANewerStableRelease(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0", Notes: "Fixes.", Xray: "26.10.10"})

	res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelStable)
	if !res.Available || res.Latest.Version != "v1.1.0" || res.Latest.Size != 100 || res.Route != RouteDirect {
		t.Fatalf("check = %+v", res)
	}
}

func TestCheckOffersNothingAtOrAboveTheLatest(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0"})
	c := NewChecker(f, "amd64")

	for _, running := range []string{"v1.1.0", "v1.2.0-beta.1"} {
		res := c.Check(context.Background(), release.MustParse(running), ChannelStable)
		if res.Available || res.Error != "" {
			t.Fatalf("%s: %+v", running, res)
		}
	}
}

func TestBetaPicksTheNewestTagAboveStable(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0"})
	f.publish(t, key, "v1.2.0-beta.2", release.Manifest{Version: "v1.2.0-beta.2", Prerelease: true})
	f.feed("v1.2.0-beta.2", "v1.2.0-beta.1", "v1.1.0", "v1.0.0")

	res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelBeta)
	if !res.Available || res.Latest.Version != "v1.2.0-beta.2" || !res.Latest.Prerelease {
		t.Fatalf("check = %+v", res)
	}
}

func TestBetaFallsBackToStableWithoutTheFeed(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0"})

	res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelBeta)
	if !res.Available || res.Latest.Version != "v1.1.0" || res.FeedError == "" {
		t.Fatalf("check = %+v", res)
	}
}

func TestBetaWorksBeforeTheFirstStableRelease(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "v1.0.0-beta.1", release.Manifest{Version: "v1.0.0-beta.1", Prerelease: true})
	f.feed("v1.0.0-beta.1")

	res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v0.9.0"), ChannelBeta)
	if !res.Available || res.Latest.Version != "v1.0.0-beta.1" {
		t.Fatalf("check = %+v", res)
	}
}

func TestCheckRefusesAnUnsignedOrMislabelledRelease(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v1.1.0"})
	f.files[ReleaseURL("", release.ManifestName)] = []byte(`{"schema":1,"version":"v9.0.0"}`)
	if res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelStable); res.Available || res.Error == "" {
		t.Fatalf("a manifest that doesn't match its signature: %+v", res)
	}

	// a tag serving another release's (validly signed) manifest
	f = &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "v1.2.0-beta.1", release.Manifest{Version: "v1.0.1", Prerelease: true})
	f.feed("v1.2.0-beta.1")
	if res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelBeta); res.Available {
		t.Fatalf("a mislabelled tag was offered: %+v", res)
	}
}

func TestCheckExplainsWhyANewerReleaseIsBlocked(t *testing.T) {
	key := testKey(t)
	f := &fakeFetcher{files: map[string][]byte{}}
	f.publish(t, key, "", release.Manifest{Version: "v2.0.0", MinFrom: "v1.5.0"})
	res := NewChecker(f, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelStable)
	if res.Available || !strings.Contains(res.Blocked, "v1.5.0") {
		t.Fatalf("check = %+v", res)
	}

	res = NewChecker(f, "riscv64").Check(context.Background(), release.MustParse("v1.6.0"), ChannelStable)
	if res.Available || !strings.Contains(res.Blocked, "riscv64") {
		t.Fatalf("check = %+v", res)
	}
}

func TestAFailedCheckSaysWhy(t *testing.T) {
	res := NewChecker(&fakeFetcher{down: true}, "amd64").Check(context.Background(), release.MustParse("v1.0.0"), ChannelStable)
	if res.Available || res.Error == "" || res.CheckedAt.IsZero() {
		t.Fatalf("check = %+v", res)
	}
}
