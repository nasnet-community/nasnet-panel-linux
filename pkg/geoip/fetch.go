package geoip

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultRangesURL = "https://s4i.co/irip"

const RefreshInterval = 7 * 24 * time.Hour

const (
	DefaultPageSize = 1000
	DefaultMaxPages = 50
)

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

type FetchConfig struct {
	BaseURL string
	// UserID is upstream's optional tracking parameter; omitted when empty.
	UserID   string
	PageSize int
	MaxPages int
}

func (c FetchConfig) withDefaults() FetchConfig {
	if c.BaseURL == "" {
		c.BaseURL = DefaultRangesURL
	}
	if c.PageSize <= 0 {
		c.PageSize = DefaultPageSize
	}
	if c.MaxPages <= 0 {
		c.MaxPages = DefaultMaxPages
	}
	return c
}

// FetchCIDRs pages through the list, keeping only lines that parse: one bad
// element aborts the whole nft transaction. A short page ends the walk.
func FetchCIDRs(ctx context.Context, client *http.Client, cfg FetchConfig) ([]string, error) {
	cfg = cfg.withDefaults()
	if client == nil {
		client = http.DefaultClient
	}

	var out []string
	for page := 0; page < cfg.MaxPages; page++ {
		lines, err := fetchPage(ctx, client, cfg, page*cfg.PageSize)
		if err != nil {
			return nil, err
		}
		if page == 0 && len(lines) == 0 {
			return nil, fmt.Errorf("%s returned no addresses at all", cfg.BaseURL)
		}
		out = append(out, parsePrefixes(lines)...)
		if len(lines) < cfg.PageSize {
			break
		}
	}
	return out, nil
}

func fetchPage(ctx context.Context, client *http.Client, cfg FetchConfig, offset int) ([]string, error) {
	q := url.Values{
		"format": {"addresses"},
		"limit":  {strconv.Itoa(cfg.PageSize)},
		"offset": {strconv.Itoa(offset)},
	}
	if cfg.UserID != "" {
		q.Set("user_id", cfg.UserID)
	}

	sep := "?"
	if strings.Contains(cfg.BaseURL, "?") {
		sep = "&"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+sep+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch domestic ranges at offset %d: %w", offset, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("domestic ranges at offset %d: unexpected status %d",
			offset, resp.StatusCode)
	}

	var lines []string
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

// A bare address is a legitimate single host and becomes a /32.
func parsePrefixes(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if p, err := netip.ParsePrefix(line); err == nil && p.Addr().Is4() {
			out = append(out, p.String())
			continue
		}
		if a, err := netip.ParseAddr(line); err == nil && a.Is4() {
			out = append(out, a.String()+"/32")
		}
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
