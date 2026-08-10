package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Serves synthetic prefixes the way the real endpoint does.
func pagedServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		var b strings.Builder
		for i := offset; i < offset+limit && i < total; i++ {
			fmt.Fprintf(&b, "10.%d.%d.0/24\n", i/256, i%256)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
}

func TestFetchCIDRs_PagesUntilAShortPage(t *testing.T) {
	srv := pagedServer(t, 2105)
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 1000, MaxPages: 50})
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	if len(got) != 2105 {
		t.Errorf("got %d prefixes, want 2105", len(got))
	}
}

// A page exactly filling the limit must not be mistaken for the end.
func TestFetchCIDRs_ExactMultipleOfPageSize(t *testing.T) {
	srv := pagedServer(t, 2000)
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 1000, MaxPages: 50})
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	if len(got) != 2000 {
		t.Errorf("got %d prefixes, want 2000", len(got))
	}
}

// A garbled line is dropped, not fed to nft where it aborts the transaction.
func TestFetchCIDRs_SkipsCommentsBlanksAndGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# a comment\n\n2.144.0.0/13\nnot-an-address\n5.22.0.0/17\n" +
			"999.1.1.1/24\n  8.8.8.8  \n2.57.3.0/33\n"))
	}))
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 1000, MaxPages: 2})
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
func TestFetchCIDRs_EmptyFirstPageIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("\n"))
	}))
	defer srv.Close()

	if _, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 1000, MaxPages: 5}); err == nil {
		t.Fatal("an empty first page was accepted")
	}
}

func TestFetchCIDRs_HTTPErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	if _, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 10, MaxPages: 2}); err == nil {
		t.Fatal("a 502 was treated as success")
	}
}

// The runaway guard: a server that never returns a short page must not spin.
func TestFetchCIDRs_StopsAtMaxPages(t *testing.T) {
	srv := pagedServer(t, 1_000_000)
	defer srv.Close()

	got, err := FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 100, MaxPages: 3})
	if err != nil {
		t.Fatalf("FetchCIDRs: %v", err)
	}
	if len(got) != 300 {
		t.Errorf("got %d prefixes, want 300 (3 pages x 100)", len(got))
	}
}

// Passed through when set, omitted when not, per the endpoint's contract.
func TestFetchCIDRs_UserIDIsOptional(t *testing.T) {
	var seen []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query())
		_, _ = w.Write([]byte("2.144.0.0/13\n"))
	}))
	defer srv.Close()

	_, _ = FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, PageSize: 10, MaxPages: 1})
	if _, ok := seen[0]["user_id"]; ok {
		t.Error("user_id was sent when none was configured")
	}
	if got := seen[0].Get("format"); got != "addresses" {
		t.Errorf("format = %q, want addresses", got)
	}

	seen = nil
	_, _ = FetchCIDRs(context.Background(), srv.Client(),
		FetchConfig{BaseURL: srv.URL, UserID: "abc", PageSize: 10, MaxPages: 1})
	if got := seen[0].Get("user_id"); got != "abc" {
		t.Errorf("user_id = %q, want abc", got)
	}
}

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
