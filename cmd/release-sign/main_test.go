package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
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
