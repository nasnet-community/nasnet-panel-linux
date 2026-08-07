package httpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"golang.org/x/net/proxy"
)

// Config is the snapshot passed to Factory.Update when settings change.
type Config struct {
	ProxyURL string
	Enabled  map[Feature]bool
}

// state is the immutable snapshot the Factory swaps atomically. Readers
// fetch the pointer once via Load and use whatever they got — no locks on
// the hot path.
type state struct {
	proxyURL   *url.URL // nil if empty/invalid
	transport  http.RoundTripper
	enabled    map[Feature]bool
	routerMode bool
	advertised EgressGroup
}

// Factory hands out *http.Client values configured by the current settings.
// Safe for concurrent use; Update swaps state atomically.
type Factory struct {
	s atomic.Pointer[state]
}

// NewFactory returns a Factory with no proxy and all features disabled.
func NewFactory() *Factory {
	f := &Factory{}
	f.Update(Config{})
	return f
}

// Update rebuilds the internal state from cfg. Invalid URLs are logged and
// treated as "no proxy" — callers with toggle on will get a direct client
// (with a warning at request time).
func (f *Factory) Update(cfg Config) {
	st := &state{enabled: cfg.Enabled}
	if old := f.s.Load(); old != nil {
		st.routerMode, st.advertised = old.routerMode, old.advertised
	}
	if cfg.ProxyURL != "" {
		u, err := parseProxyURL(cfg.ProxyURL)
		if err != nil {
			logger.GetLogger().WithError(err).
				WithField("url", redactURL(cfg.ProxyURL)).
				Warn("[httpclient] outbound_proxy_url is invalid; features routed via proxy will fall back to direct")
		} else {
			st.proxyURL = u
			st.transport = buildSOCKS5Transport(u, proxy.Direct)
		}
	}
	f.s.Store(st)
}

// ClientFor returns an *http.Client for the given feature.
// If the feature toggle is off, or the proxy URL is unset/invalid, a direct
// client is returned. timeout is applied to the returned client.
func (f *Factory) ClientFor(feat Feature, group EgressGroup, timeout time.Duration) *http.Client {
	st := f.s.Load()
	useProxy := st.enabled[feat] && st.transport != nil

	if st.enabled[feat] && st.transport == nil {
		logger.GetLogger().WithField("feature", string(feat)).
			Warn("[httpclient] feature toggled to use proxy, but outbound_proxy_url is unset/invalid; using direct connection")
	}

	if useProxy {
		// Reaching the proxy is itself an egress, so mark that dial too.
		return &http.Client{Transport: f.proxyTransport(st, group), Timeout: timeout}
	}

	if dial := f.markingDialer(group); dial != nil {
		return &http.Client{Timeout: timeout, Transport: &http.Transport{
			DialContext:           dial,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}}
	}
	return &http.Client{Transport: http.DefaultTransport, Timeout: timeout}
}

// IsProxyConfigured returns true if a valid proxy URL is currently set.
func (f *Factory) IsProxyConfigured() bool {
	return f.s.Load().transport != nil
}

// LiveClient: transport defers to the factory per-request so setting
// reloads take effect. Use for consumers that snapshot at construction
// (telebot, acme.Client); ClientFor returns a snapshot.
func (f *Factory) LiveClient(feat Feature, group EgressGroup, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &liveTransport{f: f, feat: feat, group: group, timeout: timeout},
		Timeout:   timeout,
	}
}

type liveTransport struct {
	f       *Factory
	feat    Feature
	group   EgressGroup
	timeout time.Duration
}

func (lt *liveTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c := lt.f.ClientFor(lt.feat, lt.group, lt.timeout)
	return c.Transport.RoundTrip(req)
}

// parseProxyURL validates that s is a socks5:// or socks5h:// URL with a
// host:port authority.
func parseProxyURL(s string) (*url.URL, error) {
	if s == "" {
		return nil, fmt.Errorf("empty url")
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	switch u.Scheme {
	case "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported scheme %q (want socks5 or socks5h)", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || port == "" || host == "" {
		return nil, fmt.Errorf("missing host:port in %q", u.Host)
	}
	return u, nil
}

// buildSOCKS5Transport returns an *http.Transport whose DialContext goes
// through the given SOCKS5 proxy. The dialer is wrapped to honor caller
// cancellation via context.
func buildSOCKS5Transport(u *url.URL, base proxy.Dialer) http.RoundTripper {
	var auth *proxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &proxy.Auth{User: u.User.Username(), Password: pass}
	}
	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, base)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[httpclient] SOCKS5 dialer construction failed; using direct")
		return http.DefaultTransport
	}

	dialCtx := func(ctx context.Context, network, addr string) (net.Conn, error) {
		type result struct {
			c   net.Conn
			err error
		}
		ch := make(chan result, 1)
		go func() {
			c, err := dialer.Dial(network, addr)
			ch <- result{c, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-ch:
			return r.c, r.err
		}
	}

	return &http.Transport{
		DialContext:           dialCtx,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// redactURL strips userinfo password so proxy URLs can be safely logged.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	if u.User != nil {
		u.User = url.User(u.User.Username())
	}
	return u.String()
}
