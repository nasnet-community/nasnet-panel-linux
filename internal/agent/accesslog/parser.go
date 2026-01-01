package accesslog

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Entry represents a parsed xray access log entry.
type Entry struct {
	Timestamp   time.Time
	SourceIP    string // client IP (without port)
	Status      string // "accepted" or "rejected"
	Network     string // "tcp", "udp"
	Domain      string // destination domain or IP
	Port        int    // destination port
	InboundTag  string
	OutboundTag string
	Email       string // subscription ConfigEmail
}

// Parser parses xray-core access log lines.
type Parser struct {
	re *regexp.Regexp
}

// NewParser creates a new access log parser.
func NewParser() *Parser {
	// xray access log format:
	//   2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main
	// Timestamp may have fractional seconds (.123456)
	// Arrow can be -> or >>
	// Source can be IPv4 (1.2.3.4:port) or IPv6 ([::1]:port)
	// Destination format: network:host:port
	re := regexp.MustCompile(
		`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})[\d.]*\s+` + // timestamp (ignore fractional seconds)
			`from\s+([\d.:\[\]a-fA-F]+?):\d+\s+` + // source IP (strip port)
			`(accepted|rejected)\s+` + // status
			`(\w+):` + // network (tcp/udp)
			`(.+):(\d+)\s+` + // domain:port
			`\[(?:(.+?)\s*(?:==>|>>|->)\s*)?(.+?)\]\s+` + // [inbound >> outbound] or [outbound]
			`email:\s*(.+)$`, // email
	)
	return &Parser{re: re}
}

// Parse attempts to parse a single access log line. Returns nil if the line
// doesn't match the expected access log format.
func (p *Parser) Parse(line string) *Entry {
	line = strings.TrimRight(line, "\r\n")

	// Quick pre-filter: skip non-access lines and internal API calls
	if !strings.Contains(line, " email: ") {
		return nil
	}

	matches := p.re.FindStringSubmatch(line)
	if matches == nil {
		return nil
	}

	ts, err := time.ParseInLocation("2006/01/02 15:04:05", matches[1], time.Local)
	if err != nil {
		ts = time.Now()
	}

	port, _ := strconv.Atoi(matches[6])

	outboundTag := strings.TrimSpace(matches[8])
	status := matches[3]

	// Xray logs "accepted" for connections routed to any outbound, including
	// blackhole/blocked. Treat blocked outbound as rejected for accurate stats.
	if status == "accepted" && outboundTag == "blocked" {
		status = "rejected"
	}

	return &Entry{
		Timestamp:   ts,
		SourceIP:    matches[2],
		Status:      status,
		Network:     matches[4],
		Domain:      matches[5],
		Port:        port,
		InboundTag:  strings.TrimSpace(matches[7]),
		OutboundTag: outboundTag,
		Email:       strings.TrimSpace(matches[9]),
	}
}
