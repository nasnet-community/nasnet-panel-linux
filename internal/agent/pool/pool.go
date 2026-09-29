package pool

import (
	"context"
	"fmt"
	"github.com/lilendian0x00/xray-knife/v11/pkg/subscription"
	"github.com/nasnet-community/nasnet-panel-linux/internal/agent/probe"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/sharelink"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func Fetch(ctx context.Context, req *pb.PoolSourceRequest) (*pb.PoolSourceResponse, error) {
	if req.MaxBytes < 1 || req.MaxBytes > 64<<20 || req.MaxLinks < 1 || req.MaxLinks > 100000 {
		return nil, status.Error(codes.InvalidArgument, "source limits must be within 64 MiB and 100,000 links")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = sourceDialer(req.AllowPrivate)
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
			return fmt.Errorf("unsupported redirect scheme")
		}
		return nil
	}}
	// subscription.Fetch decodes with the strict v11.2 decoder, so fetch here
	// and decode with sharelink.DecodeSubscription, which skips "#" metadata lines.
	u, err := url.Parse(req.Url)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, status.Error(codes.Unavailable, "subscription URL must be HTTP or HTTPS with a host")
	}
	ctx, cancel := context.WithTimeout(ctx, subscription.DefaultTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "invalid subscription request")
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		// net/http errors carry the URL, which may hold a subscription token.
		return nil, status.Error(codes.Unavailable, "subscription request failed")
	}
	defer resp.Body.Close()
	result := &pb.PoolSourceResponse{Etag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}
	if resp.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, status.Errorf(codes.Unavailable, "subscription server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > req.MaxBytes {
		return nil, status.Error(codes.Unavailable, subscription.ErrTooLarge.Error())
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, req.MaxBytes+1))
	if err != nil {
		return nil, status.Error(codes.Unavailable, "subscription request failed")
	}
	if result.Links, err = sharelink.DecodeSubscription(body, subscription.DecodeOptions{MaxBytes: req.MaxBytes, MaxLinks: int(req.MaxLinks)}); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return result, nil
}

func sourceDialer(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("source DNS failed")
		}
		for _, a := range ips {
			if !allowPrivate && (!a.IP.IsGlobalUnicast() || a.IP.IsPrivate() || a.IP.IsLoopback() || a.IP.IsLinkLocalUnicast()) {
				return nil, fmt.Errorf("private source address requires explicit access")
			}
		}
		dialer := net.Dialer{Timeout: 10 * time.Second}
		for _, a := range ips {
			conn, e := dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("source connection failed")
	}
}

// DefaultScanThreads is a remote agent's node-wide candidate-test budget,
// shared across pools and concurrent hub requests. The built-in node runs
// inside the router's panel and is configured with a smaller one (D7).
const DefaultScanThreads = 2000

var testSlots = make(chan struct{}, DefaultScanThreads)

// Configure sets the node-wide scan budget. Call once, before serving.
func Configure(maxThreads int) {
	if maxThreads > 0 {
		testSlots = make(chan struct{}, maxThreads)
	}
}

// Test scans one candidate: a temporary core instance for the link, then
// every probe URL through it within the delay budget. One attempt plus the
// requested retries; an attempt passes only when every URL answers. The
// panel links neither xray-knife's pkg/http nor its database (its sqlite
// driver collides with the panel's), so this uses the shared probe.
func Test(ctx context.Context, req *pb.PoolCandidateRequest) (*pb.PoolCandidateResponse, error) {
	if req.MaxDelayMs < 100 || req.MaxDelayMs > 30000 || req.Retries < 0 || req.Retries > 3 || len(req.TestUrls) < 1 || len(req.TestUrls) > 3 || len(req.Link) > 1<<20 {
		return nil, status.Error(codes.InvalidArgument, "invalid pool scan settings")
	}
	for _, raw := range req.TestUrls {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" {
			return nil, status.Error(codes.InvalidArgument, "probe URLs must use HTTPS")
		}
	}
	select {
	case testSlots <- struct{}{}:
		defer func() { <-testSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	outbound, _, _, _, err := sharelink.ManagedOutbound(req.Link)
	if err != nil {
		return &pb.PoolCandidateResponse{Status: "unsupported", Error: err.Error()}, nil
	}

	maxDelay := time.Duration(req.MaxDelayMs) * time.Millisecond
	client, instance, err := probe.Tunnel(ctx, req.Link, maxDelay, false)
	if err != nil {
		return &pb.PoolCandidateResponse{Status: "broken", Error: err.Error()}, nil
	}
	defer instance.Close()

	var failed *pb.OutboundTestResponse
	var latency int64
	for attempt := int32(0); attempt <= req.Retries; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		failed, latency = nil, 0
		for _, u := range req.TestUrls {
			out := probe.Check(ctx, client, u, maxDelay)
			if !out.Success {
				failed = out
				break
			}
			// The slowest required URL is the candidate's delay.
			if out.LatencyMs > latency {
				latency = out.LatencyMs
			}
		}
		if failed == nil {
			break
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if failed != nil {
		return &pb.PoolCandidateResponse{Status: failed.Status, Error: "Required HTTPS probe failed: " + failed.Error}, nil
	}
	b, err := sharelink.EncodeManaged(outbound)
	if err != nil {
		return nil, status.Error(codes.Internal, "cannot encode outbound: "+err.Error())
	}
	return &pb.PoolCandidateResponse{Healthy: true, Status: "passed", LatencyMs: latency, OutboundJson: string(b)}, nil
}
