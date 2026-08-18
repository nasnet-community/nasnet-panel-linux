package geoip

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRangesURL is the prefix-fetcher release asset. /latest/download
// redirects to the newest release, so there is no tag to keep bumping.
const DefaultRangesURL = "https://github.com/nasnet-community/prefix-fetcher/" +
	"releases/latest/download/ir_prefixes_v4_collapsed.txt"

const RefreshInterval = 7 * 24 * time.Hour

// maxRangesBytes caps the body. The collapsed list is ~35 KB, so anything near
// this is a wrong URL rather than a bigger country.
const maxRangesBytes = 8 << 20

// Thresholds guarding a refresh. A truncated or censored response must leave the
// working list alone rather than replace it.
const (
	// MinSafeCount is the floor no accepted list may fall under.
	MinSafeCount = 100
	// MinBootstrapCount applies with nothing to compare against.
	MinBootstrapCount = 1000
	// MinRetentionPercent is how much of the previous list must survive.
	MinRetentionPercent = 70
)

// FetchCIDRs downloads the prefix list, keeping only lines that parse: one bad
// element aborts the whole nft transaction.
func FetchCIDRs(ctx context.Context, client *http.Client, rangesURL string) ([]string, error) {
	if rangesURL == "" {
		rangesURL = DefaultRangesURL
	}
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rangesURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch domestic ranges: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("domestic ranges: unexpected status %d", resp.StatusCode)
	}

	out := make([]string, 0, 4096)
	sc := bufio.NewScanner(io.LimitReader(resp.Body, maxRangesBytes))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		out = appendPrefix(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s returned no addresses at all", rangesURL)
	}
	return out, nil
}

// A bare address is a legitimate single host and becomes a /32.
func appendPrefix(out []string, line string) []string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return out
	}
	if p, err := netip.ParsePrefix(line); err == nil && p.Addr().Is4() {
		return append(out, p.String())
	}
	if a, err := netip.ParseAddr(line); err == nil && a.Is4() {
		return append(out, a.String()+"/32")
	}
	return out
}

// AcceptRefresh reports whether a fetched list is safe to install
func AcceptRefresh(fresh, current int) error {
	required := MinSafeCount
	if current == 0 {
		required = MinBootstrapCount
	} else if floor := current * MinRetentionPercent / 100; floor > required {
		required = floor
	}
	if fresh < required {
		return fmt.Errorf("refused a list of %d prefixes: at least %d needed "+
			"(previous list had %d); keeping the current one", fresh, required, current)
	}
	return nil
}

// CachedRanges is the last accepted fetch, kept so a restart need not refetch
type CachedRanges struct {
	FetchedAt time.Time `json:"fetched_at"`
	Source    string    `json:"source"`
	V4        []string  `json:"v4"`
}

func (c *CachedRanges) Age() time.Duration { return time.Since(c.FetchedAt) }

// LoadCachedRanges returns nil with no error when nothing has been fetched yet
func LoadCachedRanges(path string) (*CachedRanges, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c CachedRanges
	if err := json.Unmarshal(b, &c); err != nil {
		// A corrupt cache is not fatal; the embedded list still works.
		return nil, fmt.Errorf("parse cached ranges: %w", err)
	}
	if len(c.V4) == 0 {
		return nil, nil
	}
	return &c, nil
}

// Write then rename
func SaveCachedRanges(path string, c *CachedRanges) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
