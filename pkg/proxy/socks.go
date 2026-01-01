package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"
)

// Config holds the proxy configuration settings.
type Config struct {
	Enabled  bool
	Type     string // "socks5" (future: "http")
	Host     string
	Port     int
	Username string
	Password string
}

// NewHTTPClient returns an http.Client routed through SOCKS5, or nil if proxy is disabled.
func NewHTTPClient(cfg Config) (*http.Client, error) {
	if !cfg.Enabled {
		return nil, nil // Return nil to use telebot's default client
	}

	if cfg.Host == "" {
		return nil, fmt.Errorf("proxy host is required when proxy is enabled")
	}

	proxyAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var auth *proxy.Auth
	if cfg.Username != "" {
		auth = &proxy.Auth{
			User:     cfg.Username,
			Password: cfg.Password,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	// Create a DialContext function from the proxy dialer
	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}

	transport := &http.Transport{
		DialContext:           dialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}
