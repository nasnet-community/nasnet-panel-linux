// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/xray/pin"
)

type zipEntry struct {
	name  string
	data  []byte
	mode  os.FileMode // 0 for a plain file
	store bool        // kept as it is, not deflated
	flags uint16
}

func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Flags: e.flags}
		if e.store {
			h.Method = zip.Store
		}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// release is what an Xray release zip holds, with the binary among the rest.
func releaseZip(t *testing.T, bin []byte, extra ...zipEntry) []byte {
	t.Helper()
	entries := []zipEntry{
		{name: "LICENSE", data: []byte("MPL 2.0")},
		{name: "README.md", data: []byte("# Xray")},
		{name: "geoip.dat", data: bytes.Repeat([]byte("geo"), 50_000)},
		{name: "xray", data: bin, mode: 0o755},
		{name: "geosite.dat", data: bytes.Repeat([]byte("site"), 50_000)},
	}
	return makeZip(t, append(entries, extra...)...)
}

func fakeBinary() []byte { return append([]byte("\x7fELF"), noise(300_000)...) }

const testXray = "26.99.1" // not the real pin, so nothing here can use it by accident

// xrayRig serves a zip per architecture under XTLS's layout and pins each of
// them. A test can change the pin or the files afterwards.
func xrayRig(t *testing.T, zips, bins map[string][]byte) (*fixture, fetcher) {
	t.Helper()
	f := newFixture(t)
	fx := defaults()
	fx.xrayBase = f.srv.URL + "/dl"
	fx.pin = xrayPin{version: testXray, zip: map[string]string{}, bin: map[string]string{}}
	for arch, z := range zips {
		f.put("/dl/v"+testXray+"/Xray-linux-"+xraySuffix[arch]+".zip", z)
		fx.pin.zip[arch], fx.pin.bin[arch] = sum256(z), sum256(bins[arch])
	}
	return f, fx
}

func oneArch(t *testing.T, zipped func(bin []byte) []byte) (*fixture, fetcher, []byte) {
	t.Helper()
	bin := fakeBinary()
	f, fx := xrayRig(t, map[string][]byte{"amd64": zipped(bin)}, map[string][]byte{"amd64": bin})
	return f, fx, bin
}

func goodZip(t *testing.T) func([]byte) []byte {
	return func(bin []byte) []byte { return releaseZip(t, bin) }
}

func TestFetchXrayGivesThePinnedBinaryGzipped(t *testing.T) {
	bins := map[string][]byte{"amd64": fakeBinary(), "arm64": fakeBinary()}
	zips := map[string][]byte{"amd64": releaseZip(t, bins["amd64"]), "arm64": releaseZip(t, bins["arm64"])}
	f, fx := xrayRig(t, zips, bins)

	for arch, name := range map[string]string{"amd64": "Xray-linux-64.zip", "arm64": "Xray-linux-arm64-v8a.zip"} {
		x, err := fx.xray(bg, f.srv.Client(), arch)
		if err != nil {
			t.Fatalf("%s: %v", arch, err)
		}
		if x.Version != testXray || x.Arch != arch {
			t.Errorf("%s: version %q, arch %q", arch, x.Version, x.Arch)
		}
		if x.PlainSize != int64(len(bins[arch])) || x.PlainSHA256 != sum256(bins[arch]) {
			t.Errorf("%s: plain size %d, sum %s", arch, x.PlainSize, x.PlainSHA256)
		}
		if !bytes.Equal(gunzip(t, x.Gz), bins[arch]) {
			t.Errorf("%s: the gzipped bytes don't unpack to the binary", arch)
		}
		// gzip -9: the header says it was made for the best compression
		if x.Gz[8] != 2 {
			t.Errorf("%s: gzip header says level byte %d, want 2 (best compression)", arch, x.Gz[8])
		}
		if f.count("/dl/v"+testXray+"/"+name) != 1 {
			t.Errorf("%s: %s was asked for %d times", arch, name, f.count("/dl/v"+testXray+"/"+name))
		}
	}
	if got := len(f.requested()); got != 2 {
		t.Errorf("requests %v, want the two zips and nothing else", f.requested())
	}
}

// The real URL, which no test can fetch: XTLS's layout, as the panel's own
// updater and the release workflow name it.
func TestTheRealXrayAddress(t *testing.T) {
	var asked []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		asked = append(asked, r.URL.String())
		return nil, errors.New("no network in tests")
	})
	for _, arch := range []string{"amd64", "arm64"} {
		FetchXray(bg, &http.Client{Transport: rt}, arch)
	}
	want := []string{
		"https://github.com/XTLS/Xray-core/releases/download/v" + pin.Version + "/Xray-linux-64.zip",
		"https://github.com/XTLS/Xray-core/releases/download/v" + pin.Version + "/Xray-linux-arm64-v8a.zip",
	}
	if strings.Join(asked, "\n") != strings.Join(want, "\n") {
		t.Fatalf("asked for\n%s\nwant\n%s", strings.Join(asked, "\n"), strings.Join(want, "\n"))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAZipThatIsNotThePinnedOneIsRefusedBeforeItIsRead(t *testing.T) {
	f, fx, _ := oneArch(t, goodZip(t))
	fx.pin.zip["amd64"] = strings.Repeat("0", 64)
	if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadHash) {
		t.Fatalf("a zip with another hash: %v", err)
	}
	// and a body that isn't a zip at all fails on the hash, not on being unreadable
	f.put("/dl/v"+testXray+"/Xray-linux-64.zip", []byte("<html>rate limited</html>"))
	if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadHash) || errors.Is(err, ErrBadArchive) {
		t.Fatalf("a web page for the zip: %v", err)
	}
}

func TestABinaryThatIsNotThePinnedOneIsRefused(t *testing.T) {
	f, fx, _ := oneArch(t, goodZip(t))
	fx.pin.bin["amd64"] = sum256([]byte("another build"))
	x, err := fx.xray(bg, f.srv.Client(), "amd64")
	if !errors.Is(err, ErrBadHash) || x != nil {
		t.Fatalf("a binary with another hash: %v", err)
	}
	if !strings.Contains(err.Error(), "the pin says") {
		t.Errorf("the message should say which hash failed: %v", err)
	}
}

func TestAZipBombIsRefused(t *testing.T) {
	t.Run("a binary that unpacks past the limit", func(t *testing.T) {
		zeros := make([]byte, 3<<20) // 3 MiB that deflate makes a few KB of
		f, fx, _ := oneArch(t, func([]byte) []byte { return makeZip(t, zipEntry{name: "xray", data: zeros, mode: 0o755}) })
		fx.limit = 1 << 20
		if z := f.file("/dl/v" + testXray + "/Xray-linux-64.zip"); len(z) > 100<<10 {
			t.Fatalf("the test zip is %d bytes, not a bomb", len(z))
		}
		x, err := fx.xray(bg, f.srv.Client(), "amd64")
		if !errors.Is(err, ErrTooLarge) || x != nil {
			t.Fatalf("%v", err)
		}
	})

	t.Run("a size one past the real limit, never unpacked", func(t *testing.T) {
		// the data is small; the header says one byte more than the limit
		f, fx, _ := oneArch(t, func([]byte) []byte {
			z := makeZip(t, zipEntry{name: "xray", data: []byte("small"), mode: 0o755})
			return setUncompressedSize(t, z, "xray", MaxDownload+1)
		})
		x, err := fx.xray(bg, f.srv.Client(), "amd64")
		if !errors.Is(err, ErrTooLarge) || x != nil {
			t.Fatalf("%v", err)
		}
	})

	t.Run("a header that lies about the size", func(t *testing.T) {
		zeros := make([]byte, 3<<20)
		f, fx, _ := oneArch(t, func([]byte) []byte {
			z := makeZip(t, zipEntry{name: "xray", data: zeros, mode: 0o755})
			return setUncompressedSize(t, z, "xray", 100) // says 100 bytes, inflates to 3 MiB
		})
		fx.limit = 1 << 20
		x, err := fx.xray(bg, f.srv.Client(), "amd64")
		if !errors.Is(err, ErrBadArchive) || x != nil {
			t.Fatalf("%v", err)
		}
	})

	t.Run("a great many entries", func(t *testing.T) {
		var many []zipEntry
		for i := 0; i < maxZipEntries+1; i++ {
			many = append(many, zipEntry{name: "f" + strconv.Itoa(i)})
		}
		f, fx, _ := oneArch(t, func(bin []byte) []byte { return releaseZip(t, bin, many...) })
		if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadArchive) {
			t.Fatalf("%v", err)
		}
	})
}

// setUncompressedSize patches the size that the central directory and the
// local header give an entry, as a hostile zip would.
func setUncompressedSize(t *testing.T, z []byte, name string, size uint32) []byte {
	t.Helper()
	z = bytes.Clone(z)
	patched := 0
	for i := 0; i+46 <= len(z); i++ {
		switch {
		case bytes.Equal(z[i:i+4], []byte("PK\x01\x02")): // central directory header
			n := int(binary.LittleEndian.Uint16(z[i+28:]))
			if i+46+n <= len(z) && string(z[i+46:i+46+n]) == name {
				binary.LittleEndian.PutUint32(z[i+24:], size)
				patched++
			}
		case bytes.Equal(z[i:i+4], []byte("PK\x03\x04")): // local header
			n := int(binary.LittleEndian.Uint16(z[i+26:]))
			if i+30+n <= len(z) && string(z[i+30:i+30+n]) == name {
				binary.LittleEndian.PutUint32(z[i+22:], size)
				patched++
			}
		}
	}
	if patched != 2 {
		t.Fatalf("patched %d headers of %s, want 2", patched, name)
	}
	return z
}

func TestNamesThatClimbOutOfAFolderAreRefused(t *testing.T) {
	for _, bad := range []string{
		"../xray", "../../etc/passwd", "/xray", "/etc/cron.d/x", "a/../xray", "a/../../x", "..\\xray", "dir\\file", "./xray", "a//b", "..", ".",
		"x\x00y", "C:/xray", "", "dir/./file",
	} {
		// next to a good xray, which doesn't make it any better
		f, fx, _ := oneArch(t, func(bin []byte) []byte { return releaseZip(t, bin, zipEntry{name: bad, data: []byte("x")}) })
		x, err := fx.xray(bg, f.srv.Client(), "amd64")
		if !errors.Is(err, ErrBadArchive) || x != nil {
			t.Errorf("an entry named %q: %v", bad, err)
		}
	}
	// and alone, it is not an xray
	for _, only := range []string{"../xray", "/xray", "dir/xray", "./xray", "XRAY", "xray.exe", "xray/", "xray/xray"} {
		bin, data := fakeBinary(), fakeBinary()
		if strings.HasSuffix(only, "/") {
			data = nil // a folder has no contents
		}
		f, fx := xrayRig(t, map[string][]byte{"amd64": makeZip(t, zipEntry{name: only, data: data})}, map[string][]byte{"amd64": bin})
		if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadArchive) {
			t.Errorf("only %q: %v", only, err)
		}
	}
	// folders and files in them are not a problem
	f, fx, bin := oneArch(t, func(bin []byte) []byte {
		return releaseZip(t, bin, zipEntry{name: "docs/"}, zipEntry{name: "docs/README.txt", data: []byte("hi")})
	})
	x, err := fx.xray(bg, f.srv.Client(), "amd64")
	if err != nil || x.PlainSHA256 != sum256(bin) {
		t.Fatalf("a zip with a folder: %v", err)
	}
}

func TestAnEntryThatIsNotAPlainXrayIsRefused(t *testing.T) {
	bin := fakeBinary()
	for name, z := range map[string]func() []byte{
		"two of them": func() []byte {
			return makeZip(t, zipEntry{name: "xray", data: bin}, zipEntry{name: "xray", data: []byte("another")})
		},
		"a link": func() []byte {
			return makeZip(t, zipEntry{name: "xray", data: []byte("/bin/sh"), mode: os.ModeSymlink | 0o777})
		},
		"a folder": func() []byte { return makeZip(t, zipEntry{name: "xray", mode: os.ModeDir | 0o755}) },
		"a locked one": func() []byte {
			return makeZip(t, zipEntry{name: "xray", data: bin, flags: 0x1})
		},
		"empty": func() []byte { return nil },
		"not a zip at all": func() []byte {
			return []byte("this is not a zip")
		},
	} {
		zb := z()
		f, fx := xrayRig(t, map[string][]byte{"amd64": zb}, map[string][]byte{"amd64": bin})
		if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadArchive) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestACorruptedEntryIsCaughtByTheZip(t *testing.T) {
	bin := fakeBinary()
	z := makeZip(t, zipEntry{name: "xray", data: bin, store: true})
	i := bytes.Index(z, bin)
	if i < 0 {
		t.Fatal("the stored binary is not in the zip as it is")
	}
	z[i+1000] ^= 0xff // after the zip was made; the central directory still has the old CRC
	f, fx := xrayRig(t, map[string][]byte{"amd64": z}, map[string][]byte{"amd64": bin})
	// the zip is pinned as it is, so only the entry's own checksum can notice
	if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadArchive) {
		t.Fatalf("%v", err)
	}
}

func TestAXrayDownloadThatStopsShortOrRunsOn(t *testing.T) {
	f, fx, _ := oneArch(t, goodZip(t))
	path := "/dl/v" + testXray + "/Xray-linux-64.zip"
	full := f.file(path)

	// cut off against a length it promised
	f.hook(path, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(full)))
		w.Write(full[:len(full)/2])
	})
	if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("a length it didn't keep to: %v", err)
	}
	// a stream that just ends: nothing says it is short, so the hash does
	f.hook(path, func(w http.ResponseWriter, r *http.Request) {
		w.Write(full[:len(full)/2])
		w.(http.Flusher).Flush()
	})
	if _, err := fx.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrBadHash) {
		t.Errorf("a stream that ended early: %v", err)
	}

	// bigger than the limit, as a length and as a stream
	small := fx
	small.limit = int64(len(full) / 2)
	f.unhook(path)
	if _, err := small.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a zip over the limit: %v", err)
	}
	f.hook(path, func(w http.ResponseWriter, r *http.Request) {
		w.Write(full)
		w.(http.Flusher).Flush()
	})
	if _, err := small.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a stream over the limit: %v", err)
	}
}

func TestXrayErrorsAndCancellation(t *testing.T) {
	f, fx, _ := oneArch(t, goodZip(t))
	path := "/dl/v" + testXray + "/Xray-linux-64.zip"

	// the arm64 zip isn't there
	if _, err := fx.xray(bg, f.srv.Client(), "arm64"); !errors.Is(err, ErrUnsupportedArch) {
		t.Errorf("an architecture the pin has nothing for: %v", err)
	}
	f2 := newFixture(t)
	fx.xrayBase = f2.srv.URL + "/dl"
	if _, err := fx.xray(bg, f2.srv.Client(), "amd64"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a missing zip: %v", err)
	}
	fx.xrayBase = f.srv.URL + "/dl"

	f.hook(path, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(bg, 150*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := fx.xray(ctx, f.srv.Client(), "amd64"); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Errorf("a deadline: %v after %v", err, time.Since(started))
	}

	// a download that goes quiet ends too
	quick := fx
	quick.stall = 300 * time.Millisecond
	f.hook(path, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("PK"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	if _, err := quick.xray(bg, f.srv.Client(), "amd64"); !errors.Is(err, errStalled) {
		t.Errorf("a zip that stopped coming: %v", err)
	}
}
