package domain

// Outbound test statuses. The first five mirror xray-knife's own verdicts;
// not_applicable is ours, for outbounds with nothing to probe (blackhole, dns,
// loopback).
const (
	OutboundTestPassed        = "passed"
	OutboundTestSemiPassed    = "semi-passed"
	OutboundTestFailed        = "failed"
	OutboundTestTimeout       = "timeout"
	OutboundTestBroken        = "broken"
	OutboundTestNotApplicable = "not_applicable"
)

// Default outbound test parameters. The test URL and speedtest amount match
// xray-knife's own defaults so an unconfigured node behaves like the tool does.
const (
	DefaultOutboundTestConcurrency = 4
	DefaultOutboundTestMaxDelayMs  = 5000
	DefaultOutboundTestRetries     = 1
	DefaultOutboundTestURL         = "https://cloudflare.com/cdn-cgi/trace"
	DefaultOutboundSpeedtestKb     = 10000
)

// Ceilings for the same settings. These are not cosmetic: the tester takes the
// delay as a uint16 and the retry count as a uint8, and the hub multiplies
// delay by attempts to size an RPC deadline — an unbounded value read from the
// settings endpoint would truncate on the wire or overflow the duration.
// They are also deliberately tight enough that the worst legal combination
// still fits inside the hub's RPC budget ceiling, so a slow-but-valid test is
// never cut short: 15s x (1+3) attempts + slack stays under that ceiling.
const (
	MaxOutboundTestConcurrency = 32
	MaxOutboundTestMaxDelayMs  = 15000
	MaxOutboundTestRetries     = 3
	MaxOutboundSpeedtestKb     = 100000
)

// OutboundTestResult is the outcome of the last connectivity test for an
// outbound. Stored as JSONB on the Outbound and overwritten on every test.
// Mirrors pkg/agent.OutboundTestResult, duplicated so the domain layer keeps
// no dependency on the transport package.
type OutboundTestResult struct {
	Success      bool    `json:"success"`
	Status       string  `json:"status,omitempty"`
	LatencyMs    int64   `json:"latency_ms"`
	TTFBMs       int64   `json:"ttfb_ms,omitempty"`
	ConnectMs    int64   `json:"connect_time_ms,omitempty"`
	StatusCode   int32   `json:"status_code,omitempty"`
	IP           string  `json:"ip,omitempty"`
	Country      string  `json:"country,omitempty"`
	DownloadMbps float64 `json:"download_mbps,omitempty"`
	UploadMbps   float64 `json:"upload_mbps,omitempty"`
	Speedtest    bool    `json:"speedtest,omitempty"`
	Error        string  `json:"error,omitempty"`
	Message      string  `json:"message,omitempty"`
}

// OutboundTestSettings holds per-node outbound test tuning, stored as JSONB on
// the Node. Zero values mean "use the default", so an empty struct is valid.
type OutboundTestSettings struct {
	Concurrency int    `json:"concurrency,omitempty"`  // parallel tests during Test All
	MaxDelayMs  int    `json:"max_delay_ms,omitempty"` // max acceptable delay / HTTP timeout
	Retries     int    `json:"retries,omitempty"`      // attempts per test, best result wins
	TestURL     string `json:"test_url,omitempty"`     // URL fetched through the outbound
	SpeedtestKb int    `json:"speedtest_kb,omitempty"` // speedtest payload per direction
	InsecureTLS *bool  `json:"insecure_tls,omitempty"` // nil means true (prior behavior)
}

// GetOutboundTestSettingsOrDefault returns the node's test settings with every
// default filled in, so callers never have to check for zero values. The legacy
// RoutingSettings.OutboundTestURL is honored when no test URL is set here.
func (n *Node) GetOutboundTestSettingsOrDefault() *OutboundTestSettings {
	s := &OutboundTestSettings{}
	if n.OutboundTestSettings != nil {
		*s = *n.OutboundTestSettings
	}
	if s.Concurrency <= 0 {
		s.Concurrency = DefaultOutboundTestConcurrency
	} else if s.Concurrency > MaxOutboundTestConcurrency {
		s.Concurrency = MaxOutboundTestConcurrency
	}
	if s.MaxDelayMs <= 0 {
		s.MaxDelayMs = DefaultOutboundTestMaxDelayMs
	} else if s.MaxDelayMs > MaxOutboundTestMaxDelayMs {
		s.MaxDelayMs = MaxOutboundTestMaxDelayMs
	}
	if s.Retries <= 0 {
		s.Retries = DefaultOutboundTestRetries
	} else if s.Retries > MaxOutboundTestRetries {
		s.Retries = MaxOutboundTestRetries
	}
	if s.TestURL == "" {
		if rs := n.GetRoutingSettingsOrDefault(); rs.OutboundTestURL != "" {
			s.TestURL = rs.OutboundTestURL
		} else {
			s.TestURL = DefaultOutboundTestURL
		}
	}
	if s.SpeedtestKb <= 0 {
		s.SpeedtestKb = DefaultOutboundSpeedtestKb
	} else if s.SpeedtestKb > MaxOutboundSpeedtestKb {
		s.SpeedtestKb = MaxOutboundSpeedtestKb
	}
	if s.InsecureTLS == nil {
		insecure := true
		s.InsecureTLS = &insecure
	}
	return s
}

// IsTestable reports whether an outbound can be probed at all. Blackhole
// discards traffic by design; dns and loopback are internal routing targets
// with no upstream to reach; an http proxy outbound has no share-link scheme
// the tester's core accepts, so it would always come back broken.
func (o *Outbound) IsTestable() bool {
	switch o.Protocol {
	case "blackhole", "dns", "loopback", "http":
		return false
	}
	return true
}
