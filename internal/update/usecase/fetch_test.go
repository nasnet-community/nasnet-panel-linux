package usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dead = "http://127.0.0.1:1/release.json"

func TestGetFallsBackToTheMirror(t *testing.T) {
	var asked string
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte("ok"))
	}))
	defer mirror.Close()

	f := NewRouteFetcher(nil, func() string { return mirror.URL + "/" })
	body, route, err := f.Get(context.Background(), dead, 10)
	if err != nil || string(body) != "ok" || route != RouteMirror {
		t.Fatalf("got %q via %s: %v", body, route, err)
	}
	if asked != "/"+dead {
		t.Fatalf("mirror was asked for %s", asked)
	}
}

func TestAMissingFileDoesNotTryOtherRoutes(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := NewRouteFetcher(nil, func() string { return srv.URL })
	if _, _, err := f.Get(context.Background(), srv.URL+"/release.json", 10); !errors.Is(err, errMissing) {
		t.Fatalf("err = %v", err)
	}
	if hits != 1 {
		t.Fatalf("asked %d times", hits)
	}
}

func TestGetRefusesAnOversizedFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), 11))
	}))
	defer srv.Close()
	if _, _, err := NewRouteFetcher(nil, nil).Get(context.Background(), srv.URL, 10); err == nil {
		t.Fatal("an oversized file was read")
	}
}

func rangeServer(t *testing.T, data []byte, served *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*served = append(*served, r.Header.Get("Range"))
		var from int
		if rg := r.Header.Get("Range"); rg != "" {
			fmt.Sscanf(rg, "bytes=%d-", &from)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(data[from:])
	}))
	t.Cleanup(srv.Close)
	return srv
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestDownloadResumesAPartFile(t *testing.T) {
	data := bytes.Repeat([]byte("nasnet"), 1000)
	var served []string
	srv := rangeServer(t, data, &served)
	dest := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if err := os.WriteFile(dest+".part", data[:2500], 0o600); err != nil {
		t.Fatal(err)
	}

	var last int64
	route, err := NewRouteFetcher(nil, nil).Download(context.Background(), srv.URL, dest, int64(len(data)), sum(data), RouteDirect,
		func(done int64, _ string) { last = done })
	if err != nil || route != RouteDirect {
		t.Fatalf("%s: %v", route, err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) || last != int64(len(data)) {
		t.Fatalf("got %d bytes, progress %d", len(got), last)
	}
	if len(served) != 1 || served[0] != "bytes=2500-" {
		t.Fatalf("ranges asked: %v", served)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("the .part file stayed")
	}
}

func TestDownloadRefusesABundleThatDoesNotMatch(t *testing.T) {
	data := []byte(strings.Repeat("a", 100))
	var served []string
	srv := rangeServer(t, data, &served)
	dest := filepath.Join(t.TempDir(), "bundle.tar.gz")

	_, err := NewRouteFetcher(nil, nil).Download(context.Background(), srv.URL, dest, 100, sum([]byte("other")), RouteDirect, func(int64, string) {})
	if err == nil {
		t.Fatal("a bundle with the wrong hash was kept")
	}
	for _, p := range []string{dest, dest + ".part"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s stayed", p)
		}
	}

	// more bytes than the release lists
	if _, err := NewRouteFetcher(nil, nil).Download(context.Background(), srv.URL, dest, 50, sum(data[:50]), RouteDirect, func(int64, string) {}); err == nil {
		t.Fatal("an oversized download was accepted")
	}
}
