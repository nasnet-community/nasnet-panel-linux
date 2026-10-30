package main

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

func trust(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	old := signing.PublicKeys
	signing.PublicKeys = []ed25519.PublicKey{pub}
	t.Cleanup(func() { signing.PublicKeys = old })
	return priv
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBundleRoundTrip(t *testing.T) {
	priv := trust(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bin/nasnet-panel"), "panel")
	write(t, filepath.Join(dir, "bin/agent/nasnet-agent-linux-arm64"), "agent")
	write(t, filepath.Join(dir, "checksums.txt"), "skipped")
	m := meta{version: "v1.2.0-rc.1", notes: "- one", minFrom: "v1.1.0", builtAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	if err := writeBundle(dir, "amd64", m, priv); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, release.BundleName))
	sig, _ := os.ReadFile(filepath.Join(dir, release.BundleName+release.SigSuffix))
	b, err := release.ParseBundle(data, sig)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sortedKeys(b.Files), ","); got != "bin/agent/nasnet-agent-linux-arm64,bin/nasnet-panel" {
		t.Fatalf("files %s", got)
	}
	if !b.Prerelease || b.MinFrom != "v1.1.0" || b.Arch != "amd64" || b.Xray == "" {
		t.Fatalf("bundle %+v", b)
	}
}

func TestReleaseSignsAgents(t *testing.T) {
	priv := trust(t)
	dir := t.TempDir()
	agent := filepath.Join(dir, "nasnet-agent-linux-amd64")
	tool := filepath.Join(dir, "nasnet-tool.sh")
	write(t, agent, "agent-binary")
	write(t, tool, "#!/bin/bash")
	m := meta{version: "v1.1.0", notes: "notes", builtAt: time.Now().UTC()}
	if err := writeRelease(dir, []string{agent, tool}, m, priv); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, release.ManifestName))
	sig, _ := os.ReadFile(filepath.Join(dir, release.ManifestName+release.SigSuffix))
	r, err := release.ParseManifest(data, sig)
	if err != nil {
		t.Fatal(err)
	}
	a := r.Assets["nasnet-agent-linux-amd64"]
	asig, err := signing.ParseSig([]byte(a.Sig))
	if err != nil || !signing.VerifyAny([]byte("agent-binary"), asig) {
		t.Fatalf("agent sig: %v", err)
	}
	if r.Assets["nasnet-tool.sh"].Sig != "" || r.Assets["nasnet-tool.sh"].Size != 11 {
		t.Fatalf("tool asset %+v", r.Assets["nasnet-tool.sh"])
	}
	if r.AgentsGz != nil || strings.Contains(string(data), "agents_gz") {
		t.Fatal("agents_gz written for a release with no gzipped agents")
	}
}

func gzipped(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestReleaseListsGzippedAgents(t *testing.T) {
	priv := trust(t)
	dir := t.TempDir()
	plain := map[string][]byte{"amd64": bytes.Repeat([]byte("amd64 agent "), 400), "arm64": bytes.Repeat([]byte("arm64 agent "), 700)}
	packed := map[string][]byte{}
	var files []string
	for arch, body := range plain { // map order: the gz may come before its agent
		packed[arch] = gzipped(t, body)
		agent, gz := filepath.Join(dir, release.AgentAsset(arch)), filepath.Join(dir, release.AgentGzAsset(arch))
		write(t, agent, string(body))
		write(t, gz, string(packed[arch]))
		files = append(files, gz, agent)
	}
	m := meta{version: "v1.3.0", notes: "notes", builtAt: time.Now().UTC()}
	if err := writeRelease(dir, files, m, priv); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, release.ManifestName))
	sig, _ := os.ReadFile(filepath.Join(dir, release.ManifestName+release.SigSuffix))
	r, err := release.ParseManifest(data, sig) // so agents_gz is inside the signed bytes
	if err != nil {
		t.Fatal(err)
	}
	if len(r.AgentsGz) != 2 || len(r.Assets) != 2 {
		t.Fatalf("assets %v, agents_gz %v", sortedKeys(r.Assets), sortedKeys(r.AgentsGz))
	}
	for arch, body := range plain {
		want := release.AgentGz{Name: "agent-linux-" + arch + ".gz", Size: int64(len(packed[arch])), SHA256: hexSum(packed[arch]),
			PlainSize: int64(len(body)), PlainSHA256: hexSum(body), Sig: r.Assets["nasnet-agent-linux-"+arch].Sig}
		if got := r.AgentsGz[arch]; got != want {
			t.Fatalf("%s: got %+v, want %+v", arch, got, want)
		}
		asig, err := signing.ParseSig([]byte(want.Sig))
		if err != nil || !signing.VerifyAny(body, asig) {
			t.Fatalf("%s: the signature doesn't verify the plain agent: %v", arch, err)
		}
	}
}

func TestReleaseRefusesAGzThatIsntTheAgent(t *testing.T) {
	priv := trust(t)
	body := bytes.Repeat([]byte("agent "), 500)
	good := gzipped(t, body)
	cases := []struct {
		name  string
		gz    []byte
		plain bool // the plain agent is listed too
	}{
		{"other bytes", gzipped(t, []byte("not the agent")), true},
		{"truncated", good[:len(good)-12], true},
		{"not gzip", body, true},
		{"no plain agent", good, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			agent, gz := filepath.Join(dir, "nasnet-agent-linux-amd64"), filepath.Join(dir, "agent-linux-amd64.gz")
			write(t, agent, string(body))
			write(t, gz, string(c.gz))
			files := []string{gz}
			if c.plain {
				files = append(files, agent)
			}
			m := meta{version: "v1.3.0", notes: "notes", builtAt: time.Now().UTC()}
			if err := writeRelease(dir, files, m, priv); err == nil {
				t.Fatal("release written")
			}
			if _, err := os.Stat(filepath.Join(dir, release.ManifestName)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("release.json left behind: %v", err)
			}
		})
	}
}

func TestKeygenPrintsAUsableKey(t *testing.T) {
	var out bytes.Buffer
	if err := keygen(&out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out.String(), "\n")
	if _, err := signing.ParsePrivateKey(lines[1]); err != nil {
		t.Fatalf("private key line %q: %v", lines[1], err)
	}
	if !strings.Contains(out.String(), "mustKey(\"") {
		t.Fatal("no PublicKeys line")
	}
}

func TestCheckRefusesUntrustedKey(t *testing.T) {
	trust(t)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	t.Setenv(keyEnv, signingSeed(other))
	if err := check(); err == nil {
		t.Fatal("untrusted key passed check")
	}
}

func signingSeed(k ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(k.Seed())
}

// The bash tool checks a release with OpenSSL before the panel can, so it
// has to trust exactly the keys the panel does.
func TestToolScriptTrustsTheReleaseKeys(t *testing.T) {
	script, err := os.ReadFile("../../nasnet-tool.sh")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)\nRELEASE_KEYS=\(([^)]*)\)`).FindSubmatch(script)
	if m == nil {
		t.Fatal("nasnet-tool.sh has no RELEASE_KEYS")
	}
	var got []string
	for _, f := range strings.Fields(string(m[1])) {
		if !strings.HasPrefix(f, "#") {
			got = append(got, strings.Trim(f, `"'`))
		}
	}
	var want []string
	for _, k := range signing.PublicKeys {
		der, _ := x509.MarshalPKIXPublicKey(k)
		want = append(want, base64.StdEncoding.EncodeToString(der))
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("RELEASE_KEYS in nasnet-tool.sh = %v, pkg/signing trusts %v", got, want)
	}
}
