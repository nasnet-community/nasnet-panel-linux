package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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

// oldManifest is Manifest as panels declared it before agents_gz.
type oldManifest struct {
	Schema     int              `json:"schema"`
	Version    string           `json:"version"`
	Prerelease bool             `json:"prerelease"`
	MinFrom    string           `json:"min_from,omitempty"`
	Xray       string           `json:"xray"`
	BuiltAt    time.Time        `json:"built_at"`
	Notes      string           `json:"notes"`
	Assets     map[string]Asset `json:"assets"`
}

func manifestWithGz() Manifest {
	return Manifest{Schema: Schema, Version: "v1.3.0", MinFrom: "v1.2.0", Xray: "26.10.21",
		BuiltAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), Notes: "- gzipped agents",
		Assets: map[string]Asset{
			AgentAsset("arm64"): {SHA256: "aa", Size: 52, Sig: "c2ln"},
			"nasnet-tool.sh":    {SHA256: "bb", Size: 7},
		},
		AgentsGz: map[string]AgentGz{
			"arm64": {Name: AgentGzAsset("arm64"), Size: 18, SHA256: "cc", PlainSize: 52, PlainSHA256: "aa", Sig: "c2ln"},
		},
	}
}

func TestParseManifestKeepsAgentsGz(t *testing.T) {
	priv := signedKey(t)
	want := manifestWithGz()
	data, _ := json.Marshal(want)
	sig := []byte(signing.EncodeSig(signing.Sign(data, priv)))

	got, err := ParseManifest(data, sig)
	if err != nil || !reflect.DeepEqual(got.AgentsGz, want.AgentsGz) {
		t.Fatalf("agents_gz after a round trip: %+v %v", got, err)
	}
	// the key is inside what release.json's signature covers
	tampered := bytes.Replace(data, []byte(`"plain_sha256":"aa"`), []byte(`"plain_sha256":"ab"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("nothing to tamper with")
	}
	if _, err := ParseManifest(tampered, sig); !errors.Is(err, ErrSignature) {
		t.Fatalf("tampered agents_gz: %v", err)
	}
}

func TestOldPanelsReadAManifestWithAgentsGz(t *testing.T) {
	priv := signedKey(t)
	m := manifestWithGz()
	data, _ := json.Marshal(m)
	sig := []byte(signing.EncodeSig(signing.Sign(data, priv)))

	// what an old panel does: verify the bytes, then a plain Unmarshal
	s, err := signing.ParseSig(sig)
	if err != nil || !signing.VerifyAny(data, s) {
		t.Fatalf("signature: %v", err)
	}
	var old oldManifest
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatalf("old shape refused the new key: %v", err)
	}
	if old.Schema != m.Schema || old.Version != m.Version || old.MinFrom != m.MinFrom || old.Xray != m.Xray ||
		!old.BuiltAt.Equal(m.BuiltAt) || old.Notes != m.Notes || !reflect.DeepEqual(old.Assets, m.Assets) {
		t.Fatalf("old shape read %+v", old)
	}

	// every other key is written as it was, and no key at all without gz files
	var with, without map[string]json.RawMessage
	json.Unmarshal(data, &with)
	m.AgentsGz = nil
	plain, _ := json.Marshal(m)
	json.Unmarshal(plain, &without)
	if _, ok := without["agents_gz"]; ok {
		t.Fatal("agents_gz written with nothing in it")
	}
	delete(with, "agents_gz")
	if !reflect.DeepEqual(with, without) {
		t.Fatal("agents_gz changed another key")
	}
}

// Old updaters pick assets by substring (nasnet-tool.sh greps the asset list),
// so the gzipped agents' names can't sit inside a name a release already has,
// or the other way round. These are v1.2.0's.
func TestAgentGzNamesStayClearOfOtherAssets(t *testing.T) {
	published := []string{"checksums.txt", "release.json", "release.json.sig", "nasnet-tool.sh"}
	for _, arch := range []string{"amd64", "arm64"} {
		published = append(published, "nasnet-panel-linux-"+arch, "nasnet-tool-linux-"+arch,
			"nasnet-agent-linux-"+arch, "nasnet-panel-offline-linux-"+arch+".tar.gz")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		for _, name := range []string{AgentAsset(arch), BundleAsset(arch)} {
			if !slices.Contains(published, name) {
				t.Errorf("%s isn't one of the published names", name)
			}
		}
		gz := AgentGzAsset(arch)
		for _, name := range published {
			if strings.Contains(gz, name) || strings.Contains(name, gz) {
				t.Errorf("%s and %s contain one another", gz, name)
			}
		}
		if got, ok := AgentGzArch(gz); !ok || got != arch {
			t.Errorf("AgentGzArch(%s) = %q, %v", gz, got, ok)
		}
	}
	for _, name := range []string{"nasnet-agent-linux-amd64", "agent-linux-.gz", "agent-linux-amd64", "agent-linux-amd64.gz.sig", "x-agent-linux-amd64.gz"} {
		if arch, ok := AgentGzArch(name); ok {
			t.Errorf("%s read as a gzipped agent for %q", name, arch)
		}
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
