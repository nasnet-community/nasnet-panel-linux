package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Serves a list the way the release asset does: one prefix per line, no paging.
func listServer(t *testing.T, count int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var b strings.Builder
		for i := 0; i < count; i++ {
			fmt.Fprintf(&b, "10.%d.%d.0/24\n", i/256, i%256)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
}

func TestFetchCIDRs_ReadsTheWholeList(t *testing.T) {
	srv := listServer(t, 2105)
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	if len(got) != 2105 {
		t.Errorf("got %d prefixes, want 2105", len(got))
	}
}

// The asset URL is a redirect, which is the only way /latest/download resolves.
func TestFetchCIDRs_FollowsTheRedirect(t *testing.T) {
	asset := listServer(t, 1200)
	defer asset.Close()
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, asset.URL, http.StatusFound)
	}))
	defer front.Close()

	got, err := FetchCIDRs(context.Background(), front.Client(), front.URL)
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	if len(got) != 1200 {
		t.Errorf("got %d prefixes, want 1200", len(got))
	}
}

// A garbled line is dropped, not fed to nft where it aborts the transaction.
func TestFetchCIDRs_SkipsCommentsBlanksAndGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# a comment\n\n2.144.0.0/13\nnot-an-address\n5.22.0.0/17\n" +
			"999.1.1.1/24\n  8.8.8.8  \n2.57.3.0/33\n2001:db8::/32\n"))
	}))
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	// A bare address is a legitimate single host; a bad mask or a bad octet is not.
	want := []string{"2.144.0.0/13", "5.22.0.0/17", "8.8.8.8/32"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// An answer with nothing in it is not "the country has no addresses".
func TestFetchCIDRs_EmptyBodyIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("\n"))
	}))
	defer srv.Close()

	if _, err := FetchCIDRs(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("an empty body was accepted")
	}
}

// A 404 is what a renamed asset looks like, and it must not read as an empty list.
func TestFetchCIDRs_HTTPErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := FetchCIDRs(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("a 404 was treated as success")
	}
}

// The runaway guard: a wrong URL streaming forever must not eat the box's memory.
func TestFetchCIDRs_StopsAtTheByteCap(t *testing.T) {
	line := "10.0.0.0/24\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for written := 0; written < maxRangesBytes+len(line); written += len(line) {
			if _, err := w.Write([]byte(line)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	// The cap can slice a line in half, so the count lands within one of it.
	if cap := maxRangesBytes / len(line); len(got) < cap || len(got) > cap+1 {
		t.Errorf("got %d prefixes, want about the capped %d", len(got), cap)
	}
}

// An empty URL is the "not configured" case, which must reach upstream's default.
func TestFetchCIDRs_EmptyURLUsesTheDefault(t *testing.T) {
	var seen string
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		seen = r.URL.String()
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}}, nil
	})}

	_, _ = FetchCIDRs(context.Background(), client, "")
	if seen != DefaultRangesURL {
		t.Errorf("fetched %q, want %q", seen, DefaultRangesURL)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The whole safety story: a truncated response must not replace the list.
func TestAcceptRefresh(t *testing.T) {
	cases := []struct {
		name           string
		fresh, current int
		ok             bool
	}{
		{"first ever fetch, plausible", 2105, 0, true},
		{"first ever fetch, too small to trust", 500, 0, false},
		{"steady state", 2105, 2100, true},
		{"small shrink is fine", 1600, 2100, true},
		{"shrank by more than 30%", 1400, 2100, false},
		{"grew a lot", 9000, 2100, true},
		{"collapsed to nothing", 0, 2100, false},
		{"tiny even with a tiny current", 50, 60, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := AcceptRefresh(c.fresh, c.current)
			if c.ok && err != nil {
				t.Errorf("rejected a good refresh: %v", err)
			}
			if !c.ok && err == nil {
				t.Error("accepted a refresh that should have been kept back")
			}
		})
	}
}
