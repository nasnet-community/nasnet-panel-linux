package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/httpclient"
)

// Routes, in the order they're tried.
const (
	RouteDirect = "direct"
	RouteProxy  = "proxy"
	RouteMirror = "mirror"
)

// errMissing means a route answered and the file isn't there; trying
// another route won't change that.
var errMissing = errors.New("not found")

type route struct {
	name   string
	client *http.Client
	prefix string
}

// Transport fetches release files and bundles; RouteFetcher in production.
type Transport interface {
	Fetcher
	Download(ctx context.Context, url, dest string, size int64, sum, first string, progress func(done int64, route string)) (string, error)
	RouteLabel(route string) string
}

// RouteFetcher tries github.com directly, then the outbound proxy when its
// panel_update toggle is on, then the mirror prefix when one is set.
type RouteFetcher struct {
	factory *httpclient.Factory // nil: plain direct client
	mirror  func() string
}

func NewRouteFetcher(f *httpclient.Factory, mirror func() string) *RouteFetcher {
	if mirror == nil {
		mirror = func() string { return "" }
	}
	return &RouteFetcher{factory: f, mirror: mirror}
}

func (f *RouteFetcher) routes(timeout time.Duration) []route {
	direct := &http.Client{Timeout: timeout}
	var proxied *http.Client
	if f.factory != nil {
		direct = f.factory.DirectClient(httpclient.EgressForeign, timeout)
		proxied = f.factory.ProxyClient(httpclient.FeaturePanelUpdate, httpclient.EgressForeign, timeout)
	}
	rs := []route{{name: RouteDirect, client: direct}}
	if proxied != nil {
		rs = append(rs, route{name: RouteProxy, client: proxied})
	}
	if m := strings.TrimRight(strings.TrimSpace(f.mirror()), "/"); m != "" {
		rs = append(rs, route{name: RouteMirror, client: direct, prefix: m + "/"})
	}
	return rs
}

// RouteLabel is how a route reads in the job steps.
func (f *RouteFetcher) RouteLabel(name string) string {
	switch name {
	case RouteProxy:
		return "outbound proxy"
	case RouteMirror:
		if u, err := url.Parse(strings.TrimSpace(f.mirror())); err == nil && u.Host != "" {
			return u.Host
		}
		return "mirror"
	}
	return "github.com"
}

// Get fetches a small file, up to limit bytes.
func (f *RouteFetcher) Get(ctx context.Context, url string, limit int64) ([]byte, string, error) {
	var errs []string
	for _, r := range f.routes(30 * time.Second) {
		body, err := getBody(ctx, r.client, r.prefix+url, limit)
		if err == nil {
			return body, r.name, nil
		}
		if errors.Is(err, errMissing) {
			return nil, r.name, fmt.Errorf("%s: %w", url, err)
		}
		errs = append(errs, r.name+": "+err.Error())
	}
	return nil, "", fmt.Errorf("%s is unreachable (%s)", url, strings.Join(errs, "; "))
}

func getBody(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errMissing
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return body, nil
}

// Download saves url to dest, resuming dest.part, and checks its size and
// hash. It starts with the route that worked for the check.
func (f *RouteFetcher) Download(ctx context.Context, url, dest string, size int64, sum, first string, progress func(done int64, route string)) (string, error) {
	rs := f.routes(0)
	for i, r := range rs {
		if r.name == first && i > 0 {
			rs[0], rs[i] = rs[i], rs[0]
		}
	}
	var errs []string
	for _, r := range rs {
		err := download(ctx, r.client, r.prefix+url, dest+".part", size, func(n int64) { progress(n, r.name) })
		if err == nil {
			if err := checkFile(dest+".part", size, sum); err != nil {
				_ = os.Remove(dest + ".part")
				return r.name, err
			}
			return r.name, os.Rename(dest+".part", dest)
		}
		if ctx.Err() != nil {
			return r.name, ctx.Err()
		}
		errs = append(errs, r.name+": "+err.Error())
	}
	return "", fmt.Errorf("the download failed (%s)", strings.Join(errs, "; "))
}

func download(ctx context.Context, c *http.Client, url, part string, size int64, progress func(int64)) error {
	have := int64(0)
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	if have > size {
		_ = os.Remove(part)
		have = 0
	}
	if have == size {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusOK: // no range support: start over
		flags |= os.O_TRUNC
		have = 0
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	buf := make([]byte, 256<<10)
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if have+int64(n) > size {
				return fmt.Errorf("more than the %d bytes the release lists", size)
			}
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			have += int64(n)
			if time.Since(last) > 500*time.Millisecond || have == size {
				progress(have)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if have != size {
		return fmt.Errorf("stopped at %d of %d bytes", have, size)
	}
	return out.Sync()
}

func checkFile(path string, size int64, sum string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if n != size || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), sum) {
		return errors.New("the download doesn't match the signed release")
	}
	return nil
}
