package accesslog

import (
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
//
// The access log is the agent's hottest CPU path: Parse runs once per proxied
// connection, so on a busy node it dominates agent CPU. It is a hand-written
// landmark tokenizer rather than a regexp — the line layout is fixed, so we
// split on known delimiters instead of paying regexp backtracking and the
// per-line submatch allocation. Field semantics are identical to the previous
// regexp (see parser_diff_test.go, which pins them against the old pattern).
type Parser struct{}

// NewParser creates a new access log parser.
func NewParser() *Parser {
	return &Parser{}
}

// Parse attempts to parse a single access log line. Returns nil if the line
// doesn't match the expected access log format.
//
// Layout (fixed by xray-core's access logger):
//
//	DATE TIME[.frac] from SRC:PORT (accepted|rejected) NET:HOST:PORT [route] email: X
//
// e.g. 2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main
func (p *Parser) Parse(line string) *Entry {
	line = strings.TrimRight(line, "\r\n")

	// Quick pre-filter: skip non-access lines and internal API calls. Kept
	// byte-identical to the previous implementation so any line that never
	// contained " email: " is rejected exactly as before.
	if !strings.Contains(line, " email: ") {
		return nil
	}

	// Timestamp: first two whitespace-delimited tokens (date, then time).
	date, i := nextField(line, 0)
	timeTok, i := nextField(line, i)
	if date == "" || timeTok == "" {
		return nil
	}
	// Drop fractional seconds: keep HH:MM:SS, discard the ".frac" tail, exactly
	// as the old pattern's `[\d.]*` did.
	if dot := strings.IndexByte(timeTok, '.'); dot >= 0 {
		timeTok = timeTok[:dot]
	}
	ts, err := time.ParseInLocation("2006/01/02 15:04:05", date+" "+timeTok, time.Local)
	if err != nil {
		ts = time.Now()
	}

	// "from" literal.
	kw, i := nextField(line, i)
	if kw != "from" {
		return nil
	}

	// Source: SRC:PORT — drop the trailing ":port".
	srcTok, i := nextField(line, i)
	srcIP := stripPort(srcTok)
	if srcIP == "" {
		return nil
	}

	// Status.
	status, i := nextField(line, i)
	if status != "accepted" && status != "rejected" {
		return nil
	}

	// Destination: NET:HOST:PORT.
	destTok, i := nextField(line, i)
	network, domain, port, ok := splitDest(destTok)
	if !ok {
		return nil
	}

	// Routing segment: [inbound >> outbound] or [outbound].
	inbound, outbound, i, ok := parseBracket(line, i)
	if !ok {
		return nil
	}

	// Email: the "email:" keyword followed by the rest of the line.
	rest := strings.TrimLeft(line[i:], " \t")
	if !strings.HasPrefix(rest, "email:") {
		return nil
	}
	email := strings.TrimSpace(rest[len("email:"):])

	// Xray logs "accepted" for connections routed to any outbound, including
	// blackhole/blocked. Treat blocked outbound as rejected for accurate stats.
	if status == "accepted" && outbound == "blocked" {
		status = "rejected"
	}

	return &Entry{
		Timestamp:   ts,
		SourceIP:    srcIP,
		Status:      status,
		Network:     network,
		Domain:      domain,
		Port:        port,
		InboundTag:  inbound,
		OutboundTag: outbound,
		Email:       email,
	}
}

// nextField returns the next whitespace-delimited token at or after index i and
// the index just past it. Leading spaces/tabs are skipped, mirroring the old
// pattern's `\s+` separators. Returns "" at end of line.
func nextField(s string, i int) (string, int) {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	start := i
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return s[start:i], i
}

// stripPort removes the trailing ":port" from a "host:port" token. The port is
// after the last colon, so bracketed IPv6 ("[::1]:443") keeps its inner colons
// — matching the old pattern's greedy `(...):\d+` tail. Returns "" when the
// token has no colon.
func stripPort(tok string) string {
	c := strings.LastIndexByte(tok, ':')
	if c < 0 {
		return ""
	}
	return tok[:c]
}

// splitDest parses xray's "network:host:port" destination token. network is up
// to the first colon; port is the digits after the last colon; host is
// everything between (may itself contain colons for IPv6). Mirrors the old
// pattern's `(\w+):(.+):(\d+)` with host greedy up to the final ":port".
func splitDest(tok string) (network, host string, port int, ok bool) {
	network, rest, found := strings.Cut(tok, ":")
	if !found {
		return "", "", 0, false
	}
	last := strings.LastIndexByte(rest, ':')
	if last < 0 {
		return "", "", 0, false
	}
	host = rest[:last]
	if host == "" {
		return "", "", 0, false
	}
	port, err := strconv.Atoi(rest[last+1:])
	if err != nil {
		return "", "", 0, false
	}
	return network, host, port, true
}

// parseBracket parses the "[inbound >> outbound]" (or "[outbound]") routing
// segment at or after index i. It returns the trimmed inbound and outbound tags
// and the index just past the closing "]". inbound is "" when the segment
// carries only an outbound tag. Recognises xray's ">>", "->" and "==>"
// separators.
func parseBracket(s string, i int) (inbound, outbound string, next int, ok bool) {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] != '[' {
		return "", "", i, false
	}
	rel := strings.IndexByte(s[i:], ']')
	if rel < 0 {
		return "", "", i, false
	}
	content := s[i+1 : i+rel]
	next = i + rel + 1

	if a, alen := findArrow(content); a >= 0 {
		if in := strings.TrimSpace(content[:a]); in != "" {
			return in, strings.TrimSpace(content[a+alen:]), next, true
		}
	}
	return "", strings.TrimSpace(content), next, true
}

// findArrow returns the byte offset and length of the first xray routing
// separator ("==>", ">>" or "->") in s, or (-1, 0) if none is present.
func findArrow(s string) (offset, length int) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '=':
			if i+2 < len(s) && s[i+1] == '=' && s[i+2] == '>' {
				return i, 3
			}
		case '>':
			if i+1 < len(s) && s[i+1] == '>' {
				return i, 2
			}
		case '-':
			if i+1 < len(s) && s[i+1] == '>' {
				return i, 2
			}
		}
	}
	return -1, 0
}
