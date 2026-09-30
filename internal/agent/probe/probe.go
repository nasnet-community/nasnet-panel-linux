// Package probe measures an outbound from the node: through a temporary core
// instance built from a share link, or straight out of the node's own route.
// The outbound test and the pool scanner share it.
package probe

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lilendian0x00/xray-knife/v11/pkg/core"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

// DefaultTestURL is xray-knife's own default: a Cloudflare trace endpoint,
// which doubles as the exit IP / geo lookup so no extra request is needed.
const DefaultTestURL = "https://cloudflare.com/cdn-cgi/trace"

// MaxTestDelayMs is the ceiling a single attempt may wait for a response. It
// mirrors the uint16 xray-knife expresses the same value in, so a hub tuned
// against an upstream node behaves identically here.
const MaxTestDelayMs = 65535

// MaxTestRetries bounds the retry count the tester accepts.
const MaxTestRetries = 10

// MaxSpeedtestKb bounds the payload one direction may move. The hub clamps this
// too, but the agent is what actually asks Cloudflare for the bytes.
const MaxSpeedtestKb = 100000

// speedtestDirectionTimeout matches xray-knife's per-direction budget.
const speedtestDirectionTimeout = 30 * time.Second

// Cloudflare's speed test endpoints, the same ones xray-knife measures against.
const (
	speedtestDownloadURL = "https://speed.cloudflare.com/__down?bytes=%d"
	speedtestUploadURL   = "https://speed.cloudflare.com/__up"
)

// Tunnel starts a temporary core instance for a share link and returns an
// HTTP client that goes through it, each request bounded by timeout. The
// caller closes the instance. The error text is ready for the operator.
func Tunnel(ctx context.Context, configLink string, timeout time.Duration, insecure bool) (*http.Client, io.Closer, error) {
	// Automatic core: xray for everything except hysteria2, which sing-box takes.
	knifeCore := core.NewAutomaticCore(false, insecure)
	proto, err := knifeCore.CreateProtocol(configLink)
	if err != nil {
		return nil, nil, errors.New("failed to parse config: " + err.Error())
	}
	// CreateProtocol only identifies the protocol type; Parse() actually parses
	// the URL into struct fields (address, transport, TLS, etc.)
	if err := proto.Parse(); err != nil {
		return nil, nil, errors.New("failed to parse protocol link: " + err.Error())
	}
	httpClient, instance, err := knifeCore.MakeHttpClient(ctx, proto, timeout)
	if err != nil {
		return nil, nil, errors.New("failed to create proxy client: " + err.Error())
	}
	return httpClient, instance, nil
}

// Proxied runs the test through a temporary core instance built from the
// config link, re-attempting up to the requested count until one passes.
func Proxied(ctx context.Context, configLink, testURL string, maxDelayMs, retries int32, insecure, speedtest bool, speedtestKb int32) *pb.OutboundTestResponse {
	timeout := time.Duration(maxDelayMs) * time.Millisecond
	// The instance has to outlive a single attempt: retries and the speedtest
	// reuse it, and only the per-request client timeout bounds each call.
	httpClient, instance, err := Tunnel(ctx, configLink, timeout, insecure)
	if err != nil {
		return &pb.OutboundTestResponse{Success: false, Status: "broken", Error: err.Error()}
	}
	defer instance.Close()

	// One attempt plus the requested retries, matching how xray-knife counts
	// them. Attempts stop at the first pass; a failure is reported as the last
	// one saw it, so the operator gets the reason the test gave up on.
	var out *pb.OutboundTestResponse
	for attempt := int32(0); attempt <= retries; attempt++ {
		if ctx.Err() != nil {
			break
		}
		out = Through(ctx, httpClient, testURL, timeout)
		if out.Success {
			break
		}
	}
	if out == nil {
		return &pb.OutboundTestResponse{Success: false, Status: "timeout", Error: "test cancelled before any attempt completed"}
	}

	if speedtest && out.Success {
		runSpeedtest(ctx, httpClient, uint64(speedtestKb)*1000, out)
	}
	return out
}

// Through issues one timed GET over the given client and grades it the way
// the panel expects: 2xx passes, a timeout is told apart from a hard failure,
// and a Cloudflare trace body yields the exit IP and country for free.
func Through(ctx context.Context, client *http.Client, testURL string, timeout time.Duration) *pb.OutboundTestResponse {
	return through(ctx, client, testURL, timeout, true)
}

// Check is Through without the exit IP lookup: one request, graded the same
// way. The pool scanner runs it per candidate and has no use for the geo.
func Check(ctx context.Context, client *http.Client, testURL string, timeout time.Duration) *pb.OutboundTestResponse {
	return through(ctx, client, testURL, timeout, false)
}

func through(ctx context.Context, client *http.Client, testURL string, timeout time.Duration, lookupExit bool) *pb.OutboundTestResponse {
	// Dual-stack dialing races two connections, so both callbacks can fire on
	// their own goroutines while this one reads the values back.
	var traceMu sync.Mutex
	var connectMs, ttfbMs int64
	var start time.Time
	trace := &httptrace.ClientTrace{
		ConnectDone: func(_, _ string, err error) {
			if err != nil {
				return // a losing or failed dial should not overwrite a good timing
			}
			traceMu.Lock()
			defer traceMu.Unlock()
			if connectMs == 0 {
				connectMs = time.Since(start).Milliseconds()
			}
		},
		GotFirstResponseByte: func() {
			traceMu.Lock()
			defer traceMu.Unlock()
			ttfbMs = time.Since(start).Milliseconds()
		},
	}

	reqCtx, cancel := context.WithTimeout(httptrace.WithClientTrace(ctx, trace), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, testURL, nil)
	if err != nil {
		return &pb.OutboundTestResponse{Success: false, Status: "broken", Error: err.Error()}
	}

	start = time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	traceMu.Lock()
	connect, ttfb := connectMs, ttfbMs
	traceMu.Unlock()
	if err != nil {
		// Client.Timeout expiry reports "context deadline exceeded (Client.Timeout
		// ...)" with a nil ctx.Err(), so match on the timeout behaviour instead of
		// the message text.
		status := "failed"
		var netErr net.Error
		if reqCtx.Err() != nil || (errors.As(err, &netErr) && netErr.Timeout()) {
			status = "timeout"
		}
		return &pb.OutboundTestResponse{Success: false, Status: status, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))

	// A generate_204 endpoint answers 204 and nothing else; a 200 there is a
	// captive portal or a hijacked response, not a working exit.
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if strings.Contains(req.URL.Path, "generate_204") {
		ok = resp.StatusCode == http.StatusNoContent
	}
	out := &pb.OutboundTestResponse{
		Success:       ok,
		Status:        "passed",
		LatencyMs:     latency,
		TtfbMs:        ttfb,
		ConnectTimeMs: connect,
		StatusCode:    int32(resp.StatusCode),
		Message:       fmt.Sprintf("Connected (%dms) - HTTP %d", latency, resp.StatusCode),
	}
	if !out.Success {
		out.Status = "failed"
		out.Error = fmt.Sprintf("unexpected HTTP status %d", resp.StatusCode)
		out.Message = ""
		return out
	}

	// Exit IP/geo comes free when the test URL is a Cloudflare trace endpoint;
	// otherwise spend one extra request on the default one.
	ip, loc := parseCFTrace(string(body))
	if ip == "" && lookupExit {
		traceCtx, traceCancel := context.WithTimeout(ctx, timeout)
		defer traceCancel()
		if traceReq, err := http.NewRequestWithContext(traceCtx, http.MethodGet, DefaultTestURL, nil); err == nil {
			if traceResp, err := client.Do(traceReq); err == nil {
				traceBody, _ := io.ReadAll(io.LimitReader(traceResp.Body, 8192))
				traceResp.Body.Close()
				ip, loc = parseCFTrace(string(traceBody))
			}
		}
	}
	out.Ip, out.Country = ip, loc
	return out
}

// runSpeedtest measures throughput in both directions through the tunnel and
// records it on the response. A failed direction downgrades the verdict to
// semi-passed rather than discarding a connection that demonstrably works.
func runSpeedtest(ctx context.Context, client *http.Client, amount uint64, out *pb.OutboundTestResponse) {
	// A dedicated client: the probe's own timeout is far too short to move
	// megabytes, but the transport (and so the tunnel) is the same.
	stClient := &http.Client{Transport: client.Transport, Timeout: speedtestDirectionTimeout}

	var reasons []string
	if mbps, err := measureDownload(ctx, stClient, amount); err != nil {
		reasons = append(reasons, "download failed: "+err.Error())
	} else {
		out.DownloadMbps = mbps
	}
	if mbps, err := measureUpload(ctx, stClient, amount); err != nil {
		reasons = append(reasons, "upload failed: "+err.Error())
	} else {
		out.UploadMbps = mbps
	}
	if len(reasons) > 0 {
		out.Status = "semi-passed"
		out.Message = fmt.Sprintf("%s - speedtest %s", out.Message, strings.Join(reasons, "; "))
	}
}

// measureDownload pulls amount bytes through the tunnel and returns Mbps.
func measureDownload(ctx context.Context, client *http.Client, amount uint64) (float64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, speedtestDirectionTimeout)
	defer cancel()

	// Timed from the first response byte, so the tunnel setup and the server's
	// own think time are not counted as transfer time.
	firstByte := time.Now()
	trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte = time.Now() }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(reqCtx, trace), http.MethodGet,
		fmt.Sprintf(speedtestDownloadURL, amount), nil)
	if err != nil {
		return 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	read, copyErr := io.Copy(io.Discard, resp.Body)
	elapsed := time.Since(firstByte)
	if copyErr != nil {
		return 0, fmt.Errorf("read body after %d bytes: %w", read, copyErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return mbps(read, elapsed)
}

// measureUpload pushes amount bytes through the tunnel and returns Mbps.
func measureUpload(ctx context.Context, client *http.Client, amount uint64) (float64, error) {
	reqCtx, cancel := context.WithTimeout(ctx, speedtestDirectionTimeout)
	defer cancel()

	sent := &countingReader{}
	newBody := func() io.ReadCloser {
		sent.r = io.LimitReader(zeroReader{}, int64(amount))
		sent.n.Store(0)
		return io.NopCloser(sent)
	}

	// Timed from the moment the headers are on the wire to the first response
	// byte, which is the window the body actually occupies.
	bodyStart, gotResponse := time.Now(), time.Now()
	trace := &httptrace.ClientTrace{
		WroteHeaders:         func() { bodyStart = time.Now() },
		GotFirstResponseByte: func() { gotResponse = time.Now() },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(reqCtx, trace), http.MethodPost, speedtestUploadURL, nil)
	if err != nil {
		return 0, err
	}
	req.Body = newBody()
	// Without GetBody the transport cannot replay the body on a redirect.
	req.GetBody = func() (io.ReadCloser, error) { return newBody(), nil }
	req.ContentLength = int64(amount)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return mbps(sent.n.Load(), gotResponse.Sub(bodyStart))
}

// mbps converts a transferred byte count and its duration into megabits/sec.
func mbps(bytes int64, d time.Duration) (float64, error) {
	if bytes <= 0 {
		return 0, errors.New("no bytes transferred")
	}
	if d <= 0 {
		return 0, errors.New("transfer too fast to time")
	}
	return float64(bytes) * 8 / d.Seconds() / 1e6, nil
}

// zeroReader is an endless stream of zero bytes for the upload direction.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// countingReader counts what the transport actually managed to send, so a
// truncated upload is measured over the bytes that really went out.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// Direct measures raw node egress for freedom outbounds: no proxy, no
// tunnel, just a timed request straight out of the node's default route.
func Direct(ctx context.Context, testURL string, maxDelayMs int32, insecure, speedtest bool) *pb.OutboundTestResponse {
	if speedtest {
		return &pb.OutboundTestResponse{
			Success: false,
			Status:  "broken",
			Error:   "speedtest is not supported for direct outbounds",
		}
	}

	timeout := time.Duration(maxDelayMs) * time.Millisecond
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
	}
	// This transport is built per probe and keeps its connections alive with no
	// idle timeout, so without this every freedom test would pin a socket (and
	// its two goroutines) until the far end gave up.
	defer client.CloseIdleConnections()
	return Through(ctx, client, testURL, timeout)
}

// parseCFTrace pulls ip= and loc= out of a Cloudflare /cdn-cgi/trace body.
func parseCFTrace(body string) (ip, loc string) {
	for _, line := range strings.Split(body, "\n") {
		if v, ok := strings.CutPrefix(line, "ip="); ok {
			ip = strings.TrimSpace(v)
		} else if v, ok := strings.CutPrefix(line, "loc="); ok {
			loc = strings.TrimSpace(v)
		}
	}
	return ip, loc
}
