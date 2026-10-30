// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

func sum256(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func gz(t *testing.T, b []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// noise is n bytes that don't compress to nothing.
func noise(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// testVerify is the check of a key of the test's own, which the real keys in
// pkg/signing don't know. It verifies the signature and decodes, as
// release.ParseManifest does, and leaves the rest to the package under test.
func testVerify(pub ed25519.PublicKey) VerifyFunc {
	return func(data, sigFile []byte) (*release.Manifest, error) {
		sig, err := signing.ParseSig(sigFile)
		if err != nil {
			return nil, err
		}
		if !signing.Verify(data, sig, pub) {
			return nil, release.ErrSignature
		}
		var m release.Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, err
		}
		return &m, nil
	}
}

// fixture is a release server: files by path, a record of what was asked for,
// and a way to take over any path.
type fixture struct {
	t    *testing.T
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
	srv  *httptest.Server

	mu    sync.Mutex
	files map[string][]byte
	hits  []string
	hooks map[string]http.HandlerFunc
}

func newFixture(t *testing.T) *fixture { return newFixtureOn(t, false) }

func newTLSFixture(t *testing.T) *fixture { return newFixtureOn(t, true) }

func newFixtureOn(t *testing.T, secure bool) *fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, pub: pub, priv: priv, files: map[string][]byte{}, hooks: map[string]http.HandlerFunc{}}
	f.srv = httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	// the tests cut responses short on purpose, which the server would complain about
	f.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	if secure {
		f.srv.StartTLS()
	} else {
		f.srv.Start()
	}
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits = append(f.hits, r.URL.Path)
	hook := f.hooks[r.URL.Path]
	body, ok := f.files[r.URL.Path]
	f.mu.Unlock()
	switch {
	case hook != nil:
		hook(w, r)
	case ok:
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) put(path string, body []byte) {
	f.mu.Lock()
	f.files[path] = body
	f.mu.Unlock()
}

func (f *fixture) file(path string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[path]
}

func (f *fixture) unhook(path string) {
	f.mu.Lock()
	delete(f.hooks, path)
	f.mu.Unlock()
}

func (f *fixture) hook(path string, h http.HandlerFunc) {
	f.mu.Lock()
	f.hooks[path] = h
	f.mu.Unlock()
}

// requested is the paths the server was asked for, in order.
func (f *fixture) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.hits...)
}

func (f *fixture) count(path string) int {
	n := 0
	for _, h := range f.requested() {
		if h == path {
			n++
		}
	}
	return n
}

func (f *fixture) source() Source {
	return Source{Base: f.srv.URL, Verify: testVerify(f.pub)}
}

// published is one architecture's agent as the release holds it.
type published struct {
	plain, gz []byte
	entry     release.AgentGz
}

// publish puts a release with a gzipped agent for each arch on the server,
// signed with the fixture's key. Each agent is signed too, the way the
// release tool does it.
func (f *fixture) publish(version string, archs ...string) map[string]*published {
	f.t.Helper()
	out := map[string]*published{}
	m := release.Manifest{
		Schema: release.Schema, Version: version, Xray: "26.10.21", BuiltAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
		Notes: "test", Assets: map[string]release.Asset{}, AgentsGz: map[string]release.AgentGz{},
	}
	for _, arch := range archs {
		plain := noise(40_000)
		packed := gz(f.t, plain)
		sig := strings.TrimSpace(signing.EncodeSig(signing.Sign(plain, f.priv)))
		e := release.AgentGz{
			Name: release.AgentGzAsset(arch), Size: int64(len(packed)), SHA256: sum256(packed),
			PlainSize: int64(len(plain)), PlainSHA256: sum256(plain), Sig: sig,
		}
		m.Assets[release.AgentAsset(arch)] = release.Asset{SHA256: e.PlainSHA256, Size: e.PlainSize, Sig: sig}
		m.AgentsGz[arch] = e
		f.put("/"+e.Name, packed)
		out[arch] = &published{plain: plain, gz: packed, entry: e}
	}
	if len(archs) == 0 {
		m.AgentsGz = nil
	}
	f.setManifest(m)
	return out
}

// setManifest signs m and puts it where FetchAgent looks.
func (f *fixture) setManifest(m release.Manifest) {
	f.t.Helper()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	f.put("/"+release.ManifestName, data)
	f.put("/"+release.ManifestName+release.SigSuffix, []byte(signing.EncodeSig(signing.Sign(data, f.priv))))
}

func (f *fixture) manifestBytes() []byte { return f.file("/" + release.ManifestName) }

// manifest is what the server serves now, decoded.
func (f *fixture) manifest() release.Manifest {
	f.t.Helper()
	var m release.Manifest
	if err := json.Unmarshal(f.manifestBytes(), &m); err != nil {
		f.t.Fatal(err)
	}
	return m
}
