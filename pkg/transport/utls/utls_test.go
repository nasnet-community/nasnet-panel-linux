package utls

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestGetHelloID_Known(t *testing.T) {
	cases := map[Fingerprint]utls.ClientHelloID{
		FingerprintChrome:     utls.HelloChrome_Auto,
		FingerprintFirefox:    utls.HelloFirefox_Auto,
		FingerprintSafari:     utls.HelloSafari_Auto,
		FingerprintEdge:       utls.HelloEdge_Auto,
		FingerprintIOS:        utls.HelloIOS_Auto,
		FingerprintAndroid:    utls.HelloAndroid_11_OkHttp,
		FingerprintRandomized: utls.HelloRandomized,
	}
	for fp, want := range cases {
		if got := GetHelloID(fp); got != want {
			t.Errorf("GetHelloID(%q) = %+v, want %+v", fp, got, want)
		}
	}
}

// Unknown fingerprints fall back to Chrome — keeps gRPC working even when
// the operator sets a typo in node config.
func TestGetHelloID_UnknownFallsBackToChrome(t *testing.T) {
	if got := GetHelloID("totally-bogus"); got != utls.HelloChrome_Auto {
		t.Errorf("unknown fp fallback = %+v, want Chrome_Auto", got)
	}
}

// "random" picks from the common set (Chrome/Firefox/Safari/Edge). 50 picks
// should always be one of those four IDs.
func TestGetHelloID_RandomReturnsCommonOne(t *testing.T) {
	wantSet := map[utls.ClientHelloID]bool{
		utls.HelloChrome_Auto:  true,
		utls.HelloFirefox_Auto: true,
		utls.HelloSafari_Auto:  true,
		utls.HelloEdge_Auto:    true,
	}
	for i := 0; i < 50; i++ {
		got := GetHelloID(FingerprintRandom)
		if !wantSet[got] {
			t.Fatalf("iter %d: got %+v, not in common set", i, got)
		}
	}
}

func TestIsValidFingerprint(t *testing.T) {
	valid := []string{"chrome", "firefox", "safari", "edge", "ios", "android", "randomized", "random", ""}
	for _, fp := range valid {
		if !IsValidFingerprint(fp) {
			t.Errorf("%q should be valid", fp)
		}
	}
	for _, fp := range []string{"opera", "ie", "ie11", "garbage"} {
		if IsValidFingerprint(fp) {
			t.Errorf("%q should NOT be valid", fp)
		}
	}
}
