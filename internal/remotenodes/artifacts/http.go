// SPDX-License-Identifier: AGPL-3.0-or-later

package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/netbind"
)

const (
	tlsTimeout    = 15 * time.Second
	headerTimeout = 30 * time.Second
	// A download may take as long as the link needs, but not go quiet. Nothing
	// else ends a transfer that stops with the connection still open, and an
	// install has no cancel button.
	stallTimeout = 60 * time.Second
	maxRedirects = 5
)

// NewClient returns a client whose connections leave from local, so that the
// router can send them where downloads should go (the plugin uses its
// 192.168.50.16, which is routed like the panel's own container). DNS lookups
// are bound to it too, since a lookup from another address would be routed
// somewhere else.
//
// It takes no proxy from the environment, and times out dialling, the TLS
// handshake and a server that doesn't answer. It has no limit on a whole
// transfer: the fetch functions end one that stalls, and the caller's context
// ends the rest. An invalid local address leaves the choice to the system,
// which is what a development machine wants.
func NewClient(local netip.Addr) *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:       nil,
		DialContext: netbind.Dialer(local).DialContext,
		// a TLS config of our own also keeps HTTP/2 off, which one download at a time doesn't need
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   tlsTimeout,
		ResponseHeaderTimeout: headerTimeout,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		DisableCompression:    true,
	}}
}

// checkRedirect follows a few redirects, which is how GitHub hands out
// release files, and never from https to plain http.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("refused a redirect from https to plain http")
	}
	return nil
}

var errStalled = errors.New("the server stopped sending data")

// body is a response body that ends with errStalled if no byte comes for the
// stall time.
type body struct {
	rc     io.ReadCloser
	length int64 // the Content-Length, or -1
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
	stall  time.Duration
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.timer.Reset(b.stall)
	}
	return n, err
}

func (b *body) Close() {
	b.timer.Stop()
	b.rc.Close()
	b.cancel(nil)
}

// fail is what a failed read means: the stall, if that is what cut it off.
func (b *body) fail(what string, err error) error {
	if errors.Is(context.Cause(b.ctx), errStalled) {
		err = errStalled
	}
	return fmt.Errorf("artifacts: reading %s: %w", what, err)
}

// open starts a GET and wants a 200. A redirect is followed under
// checkRedirect, whatever the client says, and the file is asked for as it is,
// not compressed in transit: the checksums are of the file.
func (f fetcher) open(ctx context.Context, c *http.Client, rawURL string) (*body, error) {
	if c == nil {
		return nil, errors.New("artifacts: no HTTP client")
	}
	client := *c
	client.CheckRedirect = checkRedirect
	ctx, cancel := context.WithCancelCause(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		cancel(nil)
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := client.Do(req)
	if err != nil {
		cancel(nil)
		return nil, plain(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel(nil)
		return nil, fmt.Errorf("artifacts: GET %s: %s", rawURL, resp.Status)
	}
	b := &body{rc: resp.Body, length: resp.ContentLength, ctx: ctx, cancel: cancel, stall: f.stall}
	b.timer = time.AfterFunc(f.stall, func() { cancel(errStalled) })
	return b, nil
}

// plain takes the query off the address in a transport error. A redirect from
// GitHub carries a token in it, which has no business in a log or on a page.
func plain(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		if u, perr := url.Parse(ue.URL); perr == nil {
			u.RawQuery, u.User = "", nil
			ue.URL = u.String()
		}
	}
	return err
}

// small reads a small file, up to max bytes.
func (f fetcher) small(ctx context.Context, c *http.Client, rawURL string, max int64) ([]byte, error) {
	b, err := f.open(ctx, c, rawURL)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	data, err := io.ReadAll(io.LimitReader(b, max+1))
	if err != nil {
		return nil, b.fail(rawURL, err)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%w: %s is over %d bytes", ErrTooLarge, rawURL, max)
	}
	return data, nil
}

// exact reads a file that is meant to be size bytes, as release.json says,
// into one buffer of that size. A server that sends more is refused as too
// large, and one that sends less has been cut off.
func (f fetcher) exact(ctx context.Context, c *http.Client, rawURL, name string, size int64) ([]byte, error) {
	b, err := f.open(ctx, c, rawURL)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	if b.length > size {
		return nil, fmt.Errorf("%w: the server has %d bytes of %s, release.json says %d", ErrTooLarge, b.length, name, size)
	}
	buf := make([]byte, size)
	if n, err := io.ReadFull(b, buf); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, fmt.Errorf("artifacts: %s ended after %d of %d bytes: %w", name, n, size, io.ErrUnexpectedEOF)
		}
		return nil, b.fail(name, err)
	}
	var more [1]byte
	switch n, err := io.ReadFull(b, more[:]); {
	case n > 0:
		return nil, fmt.Errorf("%w: %s goes on past the %d bytes release.json says", ErrTooLarge, name, size)
	case !errors.Is(err, io.EOF):
		return nil, b.fail(name, err)
	}
	return buf, nil
}

// capped reads a file of no known size, up to the limit, and says what its
// SHA-256 is.
func (f fetcher) capped(ctx context.Context, c *http.Client, rawURL, name string) ([]byte, string, error) {
	b, err := f.open(ctx, c, rawURL)
	if err != nil {
		return nil, "", err
	}
	defer b.Close()
	if b.length > f.limit {
		return nil, "", fmt.Errorf("%w: the server has %d bytes of %s, the most taken is %d", ErrTooLarge, b.length, name, f.limit)
	}
	var buf bytes.Buffer
	if b.length > 0 {
		buf.Grow(int(b.length))
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(&buf, h), io.LimitReader(b, f.limit+1))
	if err != nil {
		return nil, "", b.fail(name, err)
	}
	if n > f.limit {
		return nil, "", fmt.Errorf("%w: %s goes on past %d bytes", ErrTooLarge, name, f.limit)
	}
	return buf.Bytes(), hex.EncodeToString(h.Sum(nil)), nil
}
