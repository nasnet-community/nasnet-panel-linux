package release

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

func TestCompare(t *testing.T) {
	order := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2",
		"v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.2.0-rc.1", "v1.2.0-rc.2", "v1.2.0", "v1.10.0", "v2.0.0"}
	for i := 0; i+1 < len(order); i++ {
		a, b := MustParse(order[i]), MustParse(order[i+1])
		if !b.Newer(a) || a.Newer(b) || a.Compare(a) != 0 {
			t.Fatalf("%s should sort before %s", a, b)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{"dev", "1.0.0", "v1.0", "v01.0.0", "v1.0.0-14-g3fa9c1e", "v1.0.0+meta", ""} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%q parsed", s)
		}
	}
	if v := MustParse("v1.2.0-rc.2"); v.String() != "v1.2.0-rc.2" || v.Stable() {
		t.Fatalf("round trip: %s", v)
	}
}

func TestPick(t *testing.T) {
	tags := []string{"v1.2.0-rc.2", "v1.1.0", "v1.2.0-rc.1", "v1.0.0", "junk"}
	cases := []struct {
		running string
		beta    bool
		want    string
	}{
		{"v1.0.0", false, "v1.1.0"},
		{"v1.0.0", true, "v1.2.0-rc.2"},
		{"v1.2.0-rc.1", true, "v1.2.0-rc.2"},
		{"v1.2.0-rc.2", false, ""}, // beta off on a beta waits for stable
		{"v1.1.0", false, ""},
	}
	for _, c := range cases {
		got, ok := Pick(tags, MustParse(c.running), c.beta)
		if (c.want == "") != !ok || (ok && got.String() != c.want) {
			t.Errorf("Pick(%s, beta=%v) = %s %v, want %q", c.running, c.beta, got, ok, c.want)
		}
	}
}

func signedKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := signing.PublicKeys
	signing.PublicKeys = []ed25519.PublicKey{pub}
	t.Cleanup(func() { signing.PublicKeys = old })
	return priv
}

func TestParseManifest(t *testing.T) {
	priv := signedKey(t)
	m := Manifest{Schema: Schema, Version: "v1.1.0", Xray: "26.10.10", Notes: "- signed", Assets: map[string]Asset{"a": {SHA256: "00", Size: 1}}}
	data, _ := json.Marshal(m)
	sig := []byte(signing.EncodeSig(signing.Sign(data, priv)))

	got, err := ParseManifest(data, sig)
	if err != nil || got.Version != "v1.1.0" {
		t.Fatalf("good manifest: %v %v", got, err)
	}
	if _, err := ParseManifest(append(data, ' '), sig); !errors.Is(err, ErrSignature) {
		t.Fatalf("tampered: %v", err)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := ParseManifest(data, []byte(signing.EncodeSig(signing.Sign(data, other)))); !errors.Is(err, ErrSignature) {
		t.Fatalf("untrusted key: %v", err)
	}
	m.Schema = 2
	data2, _ := json.Marshal(m)
	if _, err := ParseManifest(data2, []byte(base64.StdEncoding.EncodeToString(signing.Sign(data2, priv)))); err == nil {
		t.Fatal("future schema accepted")
	}
}

func TestParseBundleNeedsFiles(t *testing.T) {
	priv := signedKey(t)
	b := Bundle{Schema: Schema, Version: "v1.1.0", Arch: "amd64"}
	data, _ := json.Marshal(b)
	if _, err := ParseBundle(data, []byte(signing.EncodeSig(signing.Sign(data, priv)))); err == nil {
		t.Fatal("bundle without files accepted")
	}
}

func TestAllowsFrom(t *testing.T) {
	if !AllowsFrom("", MustParse("v1.0.0")) || AllowsFrom("v1.2.0", MustParse("v1.1.9")) || !AllowsFrom("v1.2.0", MustParse("v1.2.0")) {
		t.Fatal("min_from")
	}
}

func TestSplitNotes(t *testing.T) {
	notes, min := SplitNotes("<!-- min_from: v1.2.0 -->\n# v1.3.0\n\n- one\n")
	if min != "v1.2.0" || notes != "# v1.3.0\n\n- one" {
		t.Fatalf("got %q %q", notes, min)
	}
	if _, min := SplitNotes("- plain"); min != "" {
		t.Fatalf("min %q", min)
	}
}

func TestAtomTags(t *testing.T) {
	feed := []byte(`<feed><entry><link rel="alternate" type="text/html" href="https://github.com/o/r/releases/tag/v1.2.0-rc.1"/></entry>
<entry><link href="https://github.com/o/r/releases/tag/v1.1.0"/><id>tag:github.com,2008:Repository/1/v1.1.0</id></entry></feed>`)
	got := AtomTags(feed)
	if len(got) != 2 || got[0] != "v1.2.0-rc.1" || got[1] != "v1.1.0" {
		t.Fatalf("got %v", got)
	}
}
