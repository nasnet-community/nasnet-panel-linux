// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/release"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/signing"
)

var bg = context.Background()

func fetchAgent(f *fixture, arch string) (*Agent, error) {
	return FetchAgent(bg, f.srv.Client(), f.source(), arch)
}

func TestFetchAgentGivesTheSignedAgent(t *testing.T) {
	f := newFixture(t)
	pub := f.publish("v1.3.0", "amd64", "arm64")

	a, err := fetchAgent(f, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	want := pub["arm64"]
	if a.Version != "v1.3.0" || a.Arch != "arm64" {
		t.Errorf("version %q, arch %q", a.Version, a.Arch)
	}
	if !bytes.Equal(a.Gz, want.gz) || a.GzSHA256 != sum256(want.gz) {
		t.Error("the gzipped agent is not the published one")
	}
	if a.PlainSize != int64(len(want.plain)) || a.PlainSHA256 != sum256(want.plain) {
		t.Errorf("plain size %d and sum %s", a.PlainSize, a.PlainSHA256)
	}
	if !bytes.Equal(gunzip(t, a.Gz), want.plain) {
		t.Error("the gzipped bytes don't unpack to the plain agent")
	}
	// Sig is the raw signature the node's SelfUpdate checks the unpacked bytes with
	if len(a.Sig) != 64 || !signing.Verify(want.plain, a.Sig, f.pub) {
		t.Errorf("signature %x does not sign the plain agent", a.Sig)
	}
	// three requests and no more: the manifest, its signature, the one file
	if got, want := f.requested(), []string{"/release.json", "/release.json.sig", "/agent-linux-arm64.gz"}; !slices.Equal(got, want) {
		t.Errorf("requests %v, want %v", got, want)
	}

	b, err := fetchAgent(f, "amd64")
	if err != nil || b.Arch != "amd64" || !bytes.Equal(b.Gz, pub["amd64"].gz) {
		t.Fatalf("amd64: %v", err)
	}
}

func TestOnlyReleasesFromMinAgentVersionOnAreTaken(t *testing.T) {
	if _, err := release.Parse("v" + MinAgentVersion); err != nil || MinAgentVersion != "1.3.0" {
		t.Fatalf("MinAgentVersion %q: %v", MinAgentVersion, err)
	}
	for _, c := range []struct {
		version string
		ok      bool
	}{
		{"v1.3.0", true}, {"v1.3.1", true}, {"v1.4.0", true}, {"v1.10.0", true}, {"v2.0.0", true}, {"v1.3.0-1.2", false},
		{"v1.2.9", false}, {"v1.2.0", false}, {"v1.0.0", false}, {"v0.9.0", false},
		// a prerelease of it sorts below it
		{"v1.3.0-rc.1", false}, {"v1.3.0-beta", false},
	} {
		f := newFixture(t)
		f.publish(c.version, "arm64")
		_, err := fetchAgent(f, "arm64")
		switch {
		case c.ok && err != nil:
			t.Errorf("%s: %v", c.version, err)
		case !c.ok && !errors.Is(err, ErrAgentTooOld):
			t.Errorf("%s: %v, want ErrAgentTooOld", c.version, err)
		case !c.ok && f.count("/agent-linux-arm64.gz") != 0:
			t.Errorf("%s: the agent was downloaded from a release that is too old", c.version)
		}
	}
}

func TestAReleaseWithoutGzipAgentsIsRefused(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0") // no agents_gz at all
	_, err := fetchAgent(f, "arm64")
	if !errors.Is(err, ErrNoGzAgents) || errors.Is(err, ErrAgentTooOld) {
		t.Fatalf("a release with none: %v", err)
	}

	f = newFixture(t)
	f.publish("v1.3.0", "amd64")
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrNoGzAgents) {
		t.Fatalf("an architecture it has none for: %v", err)
	}
	if _, err := fetchAgent(f, "amd64"); err != nil {
		t.Fatalf("the one it has: %v", err)
	}

	// an old release is too old, which is the answer that tells what to do
	f = newFixture(t)
	f.publish("v1.2.0")
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrAgentTooOld) || errors.Is(err, ErrNoGzAgents) {
		t.Fatalf("v1.2.0 without gzipped agents: %v", err)
	}
}

func TestADownloadThatIsNotWhatTheReleaseSaysIsRefused(t *testing.T) {
	f := newFixture(t)
	pub := f.publish("v1.3.0", "arm64")
	tampered := bytes.Clone(pub["arm64"].gz)
	tampered[len(tampered)/2] ^= 0x01
	f.put("/agent-linux-arm64.gz", tampered)

	a, err := fetchAgent(f, "arm64")
	if !errors.Is(err, ErrBadHash) || a != nil {
		t.Fatalf("one flipped bit: %v", err)
	}
	if !strings.Contains(err.Error(), pub["arm64"].entry.SHA256) {
		t.Errorf("the message should say what was expected: %v", err)
	}
}

func TestAFileLargerThanTheReleaseSaysIsRefused(t *testing.T) {
	// the release names a size past the cap, so nothing is downloaded
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	m := f.manifest()
	e := m.AgentsGz["arm64"]
	e.Size = MaxDownload + 1
	m.AgentsGz["arm64"] = e
	f.setManifest(m)
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a size past the cap: %v", err)
	}
	if f.count("/agent-linux-arm64.gz") != 0 {
		t.Error("the file was requested although the release says it is too large")
	}

	// a cap that is not the default, so a small body can be over it
	small := defaults()
	small.limit = 1000
	f = newFixture(t)
	f.publish("v1.3.0", "arm64") // its gz is about 40 KB
	if _, err := small.agent(bg, f.srv.Client(), f.source(), "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a release over a lowered cap: %v", err)
	}

	// the server has more than the release says, as a length or as a stream
	f = newFixture(t)
	pub := f.publish("v1.3.0", "arm64")
	f.put("/agent-linux-arm64.gz", append(bytes.Clone(pub["arm64"].gz), "tail"...))
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a longer Content-Length: %v", err)
	}
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(pub["arm64"].gz)
		w.(http.Flusher).Flush() // no Content-Length from here on: chunked
		w.Write([]byte("tail"))
	})
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a longer stream: %v", err)
	}

	// and one that never stops is left behind after a byte over, not read to the end
	stopped := make(chan struct{})
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		defer close(stopped)
		chunk := make([]byte, 32<<10)
		for i := 0; i < 4096; i++ { // 128 MiB, past any cap, if nobody stops it
			if _, err := w.Write(chunk); err != nil || r.Context().Err() != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
		t.Error("the client read a whole 128 MiB")
	})
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("an endless stream: %v", err)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the client left the endless stream running")
	}
}

func TestAFileCutShortIsNotAHashError(t *testing.T) {
	f := newFixture(t)
	pub := f.publish("v1.3.0", "arm64")
	full := pub["arm64"].gz
	for name, hook := range map[string]http.HandlerFunc{
		"a length it doesn't keep to": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			w.Write(full[:len(full)/2])
		},
		"a stream that ends early": func(w http.ResponseWriter, r *http.Request) {
			w.Write(full[:len(full)/2])
			w.(http.Flusher).Flush()
		},
		"nothing at all": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "0")
		},
	} {
		f.hook("/agent-linux-arm64.gz", hook)
		_, err := fetchAgent(f, "arm64")
		if !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ErrBadHash) {
			t.Errorf("%s: %v, want an unexpected EOF", name, err)
		}
	}
}

func TestAManifestTheVerifierRejects(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")

	check := func(name string, src Source, want ...error) {
		t.Helper()
		_, err := FetchAgent(bg, f.srv.Client(), src, "arm64")
		if !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v, want ErrManifest", name, err)
		}
		for _, w := range want {
			if !errors.Is(err, w) {
				t.Errorf("%s: %v, want it to wrap %v", name, err, w)
			}
		}
		if f.count("/agent-linux-arm64.gz") != 0 {
			t.Errorf("%s: the agent was downloaded anyway", name)
		}
	}

	stranger := newFixture(t) // a key the release wasn't signed with
	src := f.source()
	src.Verify = testVerify(stranger.pub)
	check("another key", src, release.ErrSignature)

	boom := errors.New("boom")
	src.Verify = func(_, _ []byte) (*release.Manifest, error) { return nil, boom }
	check("the verifier's own error", src, boom)

	src.Verify = func(_, _ []byte) (*release.Manifest, error) { return nil, nil }
	check("a verifier that says yes to nothing", src)

	// the real check, with the keys this build trusts, doesn't know the fixture's
	check("the real keys", Source{Base: f.srv.URL}, release.ErrSignature)

	// a manifest changed after it was signed
	data := bytes.Clone(f.manifestBytes())
	f.put("/release.json", bytes.Replace(data, []byte("v1.3.0"), []byte("v9.9.9"), 1))
	check("a changed manifest", f.source(), release.ErrSignature)
	f.put("/release.json", data)

	for name, sig := range map[string]string{"text": "not a signature", "empty": "", "too short": "c2hvcnQ="} {
		f.put("/release.json.sig", []byte(sig))
		check("a signature file that is "+name, f.source())
	}
}

func TestWhatTheVerifierLetsThroughIsStillChecked(t *testing.T) {
	for name, mutate := range map[string]func(*release.Manifest){
		"another schema": func(m *release.Manifest) { m.Schema = 2 },
		"no schema":      func(m *release.Manifest) { m.Schema = 0 },
		"no version":     func(m *release.Manifest) { m.Version = "" },
		"a dev build":    func(m *release.Manifest) { m.Version = "dev" },
		"a git build":    func(m *release.Manifest) { m.Version = "v1.3.0-14-g3fa9c1e" },
	} {
		f := newFixture(t)
		f.publish("v1.3.0", "arm64")
		m := f.manifest()
		mutate(&m)
		f.setManifest(m)
		if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v, want ErrManifest", name, err)
		}
	}
}

// The release tool writes these entries, so a signed one that is odd is a
// release gone wrong. None of it is used as it comes: the name goes into a
// URL and the sizes decide how much memory is taken.
func TestAManifestEntryIsHeldToWhatTheReleaseToolWrites(t *testing.T) {
	good := func() (*fixture, release.Manifest, release.AgentGz) {
		f := newFixture(t)
		f.publish("v1.3.0", "arm64")
		m := f.manifest()
		return f, m, m.AgentsGz["arm64"]
	}
	for name, mutate := range map[string]func(*release.AgentGz){
		"a path in the name":        func(e *release.AgentGz) { e.Name = "../agent-linux-arm64.gz" },
		"another architecture":      func(e *release.AgentGz) { e.Name = "agent-linux-amd64.gz" },
		"another file":              func(e *release.AgentGz) { e.Name = "release.json" },
		"a query in the name":       func(e *release.AgentGz) { e.Name += "?x=1" },
		"no name":                   func(e *release.AgentGz) { e.Name = "" },
		"a checksum that isn't hex": func(e *release.AgentGz) { e.SHA256 = strings.Repeat("z", 64) },
		"a short checksum":          func(e *release.AgentGz) { e.SHA256 = e.SHA256[:40] },
		"no checksum":               func(e *release.AgentGz) { e.SHA256 = "" },
		"a bad plain checksum":      func(e *release.AgentGz) { e.PlainSHA256 = "abc" },
		"no size":                   func(e *release.AgentGz) { e.Size = 0 },
		"a negative size":           func(e *release.AgentGz) { e.Size = -5 },
		"no plain size":             func(e *release.AgentGz) { e.PlainSize = 0 },
		"no signature":              func(e *release.AgentGz) { e.Sig = "" },
		"a signature of text":       func(e *release.AgentGz) { e.Sig = "this is not base64!" },
		"a signature too short":     func(e *release.AgentGz) { e.Sig = "c2hvcnQ=" },
	} {
		f, m, e := good()
		mutate(&e)
		m.AgentsGz["arm64"] = e
		f.setManifest(m)
		if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v, want ErrManifest", name, err)
		}
		if len(f.requested()) != 2 {
			t.Errorf("%s: requests %v, want only the manifest and its signature", name, f.requested())
		}
	}

	// upper case hex is the same checksum
	f, m, e := good()
	e.SHA256, e.PlainSHA256 = strings.ToUpper(e.SHA256), strings.ToUpper(e.PlainSHA256)
	m.AgentsGz["arm64"] = e
	f.setManifest(m)
	a, err := fetchAgent(f, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if a.GzSHA256 != strings.ToLower(e.SHA256) || a.PlainSHA256 != strings.ToLower(e.PlainSHA256) {
		t.Errorf("the sums come back as %s and %s, want lower case", a.GzSHA256, a.PlainSHA256)
	}
}

func TestOnlyAmd64AndArm64(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "amd64", "arm64")
	h := NewHolder(f.srv.Client(), f.source())
	for _, arch := range []string{"", "386", "arm", "ARM64", "AMD64", "x86_64", "aarch64", "amd64 ", "arm64\n", "../arm64", "riscv64"} {
		if _, err := fetchAgent(f, arch); !errors.Is(err, ErrUnsupportedArch) {
			t.Errorf("FetchAgent(%q): %v", arch, err)
		}
		if _, err := FetchXray(bg, f.srv.Client(), arch); !errors.Is(err, ErrUnsupportedArch) {
			t.Errorf("FetchXray(%q): %v", arch, err)
		}
		if _, err := h.Agent(bg, arch); !errors.Is(err, ErrUnsupportedArch) {
			t.Errorf("Holder.Agent(%q): %v", arch, err)
		}
		if _, err := h.Xray(bg, arch); !errors.Is(err, ErrUnsupportedArch) {
			t.Errorf("Holder.Xray(%q): %v", arch, err)
		}
	}
	if got := f.requested(); len(got) != 0 {
		t.Errorf("an architecture that is refused cost requests: %v", got)
	}
}

func redirect(f *fixture, from, to string) {
	f.hook(from, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, to, http.StatusFound) })
}

func TestRedirects(t *testing.T) {
	// GitHub answers with a redirect to another host, which is followed
	cdn := newFixture(t)
	cdn.publish("v1.3.0", "arm64")
	front := newFixture(t)
	for _, name := range []string{"release.json", "release.json.sig"} {
		front.put("/final/"+name, cdn.file("/"+name))
		redirect(front, "/rel/"+name, "/mid/"+name) // two hops on the one server
		redirect(front, "/mid/"+name, "/final/"+name)
	}
	redirect(front, "/rel/agent-linux-arm64.gz", cdn.srv.URL+"/agent-linux-arm64.gz?token=abc") // one hop to the other
	src := Source{Base: front.srv.URL + "/rel", Verify: testVerify(cdn.pub)}
	a, err := FetchAgent(bg, front.srv.Client(), src, "arm64")
	if err != nil {
		t.Fatalf("through redirects: %v", err)
	}
	if !bytes.Equal(a.Gz, cdn.file("/agent-linux-arm64.gz")) {
		t.Error("the file that came through the redirect is not the published one")
	}

	// a loop, and a chain that goes on too long, both end with an error
	loop := newFixture(t)
	redirect(loop, "/a/release.json", "/b/release.json")
	redirect(loop, "/b/release.json", "/a/release.json")
	_, err = FetchAgent(bg, loop.srv.Client(), Source{Base: loop.srv.URL + "/a", Verify: testVerify(loop.pub)}, "arm64")
	if err == nil || !strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Errorf("a redirect loop: %v", err)
	}
	long := newFixture(t)
	for i := 0; i < 8; i++ {
		redirect(long, "/hop"+strconv.Itoa(i)+"/release.json", "/hop"+strconv.Itoa(i+1)+"/release.json")
	}
	_, err = FetchAgent(bg, long.srv.Client(), Source{Base: long.srv.URL + "/hop0", Verify: testVerify(long.pub)}, "arm64")
	if err == nil || !strings.Contains(err.Error(), "stopped after 5 redirects") {
		t.Errorf("eight redirects: %v", err)
	}
}

func TestARedirectNeverGoesFromHTTPSToHTTP(t *testing.T) {
	plain := newFixture(t)
	plain.publish("v1.3.0", "arm64")
	secure := newTLSFixture(t)
	secure.hook("/release.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.srv.URL+"/release.json", http.StatusFound)
	})
	_, err := FetchAgent(bg, secure.srv.Client(), Source{Base: secure.srv.URL, Verify: testVerify(plain.pub)}, "arm64")
	if err == nil || !strings.Contains(err.Error(), "https to plain http") {
		t.Fatalf("a downgrade: %v", err)
	}
	if plain.count("/release.json") != 0 {
		t.Error("the plain server was asked after the downgrade")
	}

	// the other way is fine: a plain address that sends the client to https
	upgraded := newTLSFixture(t)
	upgraded.publish("v1.3.0", "arm64")
	plain2 := newFixture(t)
	for _, p := range []string{"/release.json", "/release.json.sig", "/agent-linux-arm64.gz"} {
		plain2.hook(p, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, upgraded.srv.URL+r.URL.Path, http.StatusFound)
		})
	}
	if _, err := FetchAgent(bg, upgraded.srv.Client(), Source{Base: plain2.srv.URL, Verify: testVerify(upgraded.pub)}, "arm64"); err != nil {
		t.Fatalf("an upgrade to https: %v", err)
	}
}

func TestATokenInARedirectDoesNotReachTheError(t *testing.T) {
	f := newFixture(t)
	f.hook("/release.json", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/file?sig=SECRET&jwt=SECRET", http.StatusFound) // nothing listens on port 1
	})
	_, err := fetchAgent(f, "arm64")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("a redirect to a dead address: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1/file") {
		t.Errorf("the address should still be named: %v", err)
	}
}

func TestACancelledContextStopsTheFetch(t *testing.T) {
	// before the server says anything
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	f.hook("/release.json", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(bg)
	time.AfterFunc(100*time.Millisecond, cancel)
	started := time.Now()
	_, err := FetchAgent(ctx, f.srv.Client(), f.source(), "arm64")
	if !errors.Is(err, context.Canceled) || time.Since(started) > 5*time.Second {
		t.Fatalf("cancelled while waiting: %v after %v", err, time.Since(started))
	}

	// a deadline
	ctx, cancel = context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	if _, err := FetchAgent(ctx, f.srv.Client(), f.source(), "arm64"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a deadline: %v", err)
	}

	// already cancelled: not a request is made
	f2 := newFixture(t)
	f2.publish("v1.3.0", "arm64")
	done, stop := context.WithCancel(bg)
	stop()
	if _, err := FetchAgent(done, f2.srv.Client(), f2.source(), "arm64"); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled: %v", err)
	}
	if got := f2.requested(); len(got) != 0 {
		t.Fatalf("a cancelled context still made requests: %v", got)
	}

	// halfway through the file
	f3 := newFixture(t)
	pub := f3.publish("v1.3.0", "arm64")
	half := make(chan struct{})
	f3.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(pub["arm64"].gz)))
		w.Write(pub["arm64"].gz[:1000])
		w.(http.Flusher).Flush()
		close(half)
		<-r.Context().Done()
	})
	ctx, cancel = context.WithCancel(bg)
	go func() { <-half; cancel() }()
	started = time.Now()
	_, err = FetchAgent(ctx, f3.srv.Client(), f3.source(), "arm64")
	if !errors.Is(err, context.Canceled) || time.Since(started) > 5*time.Second {
		t.Fatalf("cancelled halfway: %v after %v", err, time.Since(started))
	}
}

func TestADownloadThatGoesQuietEnds(t *testing.T) {
	f := newFixture(t)
	pub := f.publish("v1.3.0", "arm64")
	full := pub["arm64"].gz
	quick := defaults()
	quick.stall = 500 * time.Millisecond

	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		w.Write(full[:1000])
		w.(http.Flusher).Flush()
		<-r.Context().Done() // and never another byte
	})
	started := time.Now()
	_, err := quick.agent(bg, f.srv.Client(), f.source(), "arm64")
	if !errors.Is(err, errStalled) {
		t.Fatalf("a server that went quiet: %v", err)
	}
	if took := time.Since(started); took < 250*time.Millisecond || took > 5*time.Second {
		t.Errorf("it ended after %v, want about the stall time", took)
	}

	// a slow download is not a stalled one: each byte that comes starts the wait over
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		const parts = 20
		step := len(full)/parts + 1
		for off := 0; off < len(full); off += step {
			w.Write(full[off:min(off+step, len(full))])
			w.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond) // a second in all, twice the stall time
		}
	})
	a, err := quick.agent(bg, f.srv.Client(), f.source(), "arm64")
	if err != nil || !bytes.Equal(a.Gz, full) {
		t.Fatalf("a slow but steady download: %v", err)
	}
}

func TestAnErrorStatusIsAnError(t *testing.T) {
	f := newFixture(t) // nothing published: 404 for the manifest
	if _, err := fetchAgent(f, "arm64"); err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "/release.json") {
		t.Fatalf("no manifest: %v", err)
	}

	f.publish("v1.3.0", "arm64")
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	if _, err := fetchAgent(f, "arm64"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("a server error on the file: %v", err)
	}

	// a sign-in page or any other 2xx that isn't 200 is not the file
	f.hook("/agent-linux-arm64.gz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if _, err := fetchAgent(f, "arm64"); err == nil || !strings.Contains(err.Error(), "204") {
		t.Fatalf("a 204: %v", err)
	}
}

// A CDN that labels a .gz file Content-Encoding: gzip would make the HTTP
// client hand over the unpacked file, and the checksum of the packed one
// would fail. The files are asked for as they are.
func TestFilesAreAskedForAsTheyAre(t *testing.T) {
	f := newFixture(t)
	pub := f.publish("v1.3.0", "arm64")
	var mu sync.Mutex
	asked := map[string]string{}
	for _, p := range []string{"/release.json", "/release.json.sig", "/agent-linux-arm64.gz"} {
		body := f.file(p)
		f.hook(p, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			asked[r.URL.Path] = r.Header.Get("Accept-Encoding")
			mu.Unlock()
			w.Header().Set("Content-Encoding", "gzip") // as if
			w.Write(body)
		})
	}
	a, err := fetchAgent(f, "arm64")
	if err != nil || !bytes.Equal(a.Gz, pub["arm64"].gz) {
		t.Fatalf("%v", err)
	}
	for p, enc := range asked {
		if enc != "identity" {
			t.Errorf("%s was asked for with Accept-Encoding %q", p, enc)
		}
	}
	if len(asked) != 3 {
		t.Errorf("asked for %v", asked)
	}
}

func TestAnOversizedManifestIsRefused(t *testing.T) {
	f := newFixture(t)
	f.publish("v1.3.0", "arm64")
	f.put("/release.json", bytes.Repeat([]byte(" "), maxManifest+1))
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a manifest over %d bytes: %v", maxManifest, err)
	}
	f.publish("v1.3.0", "arm64")
	f.put("/release.json.sig", bytes.Repeat([]byte("A"), maxSig+1))
	if _, err := fetchAgent(f, "arm64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a signature file over %d bytes: %v", maxSig, err)
	}
}

func TestNoClientIsAnErrorNotAPanic(t *testing.T) {
	if _, err := FetchAgent(bg, nil, Source{Base: "http://127.0.0.1:1"}, "arm64"); err == nil {
		t.Error("FetchAgent with no client")
	}
	if _, err := FetchXray(bg, nil, "arm64"); err == nil {
		t.Error("FetchXray with no client")
	}
}

func TestOverTLS(t *testing.T) {
	f := newTLSFixture(t)
	f.publish("v1.3.0", "arm64")
	if _, err := FetchAgent(bg, f.srv.Client(), f.source(), "arm64"); err != nil {
		t.Fatalf("a client that trusts the server: %v", err)
	}
	// NewClient trusts the system's roots, not the test server's
	_, err := FetchAgent(bg, NewClient(netip.Addr{}), f.source(), "arm64")
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("a certificate nobody trusts: %v", err)
	}
}

func TestSourceFromEnv(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k != "RELEASE_BASE_URL" {
				t.Errorf("asked for %s", k)
			}
			return v
		}
	}
	// unset means the project's releases
	src, err := SourceFromEnv(env(""))
	if err != nil || src.url("release.json") != "https://github.com/nasnet-community/nasnet-panel-linux/releases/latest/download/release.json" {
		t.Fatalf("unset: %+v, %v", src, err)
	}
	if got := (Source{}).url("x"); got != DefaultReleaseBase+"/x" {
		t.Errorf("the zero Source: %s", got)
	}
	for in, want := range map[string]string{
		"http://10.0.2.2:8000":         "http://10.0.2.2:8000/release.json",
		"http://10.0.2.2:8000/":        "http://10.0.2.2:8000/release.json",
		"  https://mirror.test/rel// ": "https://mirror.test/rel/release.json",
		"http://[::1]:8080/a/b":        "http://[::1]:8080/a/b/release.json",
	} {
		src, err := SourceFromEnv(env(in))
		if err != nil || src.url("release.json") != want || src.Verify != nil {
			t.Errorf("%q: %+v, %v, want %s", in, src, err, want)
		}
	}
	for _, bad := range []string{
		"ftp://mirror.test", "mirror.test", "file:///tmp", "://x", "http://", "https:///path",
		"http://user:pass@mirror.test", "http://mirror.test/?x=1", "http://mirror.test/?", "http://mirror.test/#frag", "javascript:alert(1)",
	} {
		_, err := SourceFromEnv(env(bad))
		if err == nil {
			t.Errorf("%q was taken", bad)
		} else if strings.Contains(err.Error(), "pass") || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: the error repeats the value: %v", bad, err)
		}
	}
}

func TestTheDefaultsAreTheRealPin(t *testing.T) {
	d := defaults()
	if d.limit != MaxDownload || MaxDownload != 64<<20 {
		t.Errorf("limit %d", d.limit)
	}
	if d.stall <= 0 {
		t.Errorf("stall %v", d.stall)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if d.pin.zip[arch] == "" || d.pin.bin[arch] == "" || d.pin.version == "" {
			t.Errorf("the pin has nothing for %s", arch)
		}
	}
	if d.xrayBase != "https://github.com/XTLS/Xray-core/releases/download" {
		t.Errorf("xray base %s", d.xrayBase)
	}
}
