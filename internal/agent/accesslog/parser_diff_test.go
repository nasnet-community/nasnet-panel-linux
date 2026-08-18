package accesslog

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// regexReference is a verbatim copy of the previous regexp-based parser. It is
// the behavioural oracle: the hand-written Parser must produce byte-identical
// Entry values (and identical nil decisions) for every realistic access log
// line. If you change Parser's field semantics on purpose, this reference — and
// the corpus below — must change with it.
var referenceRE = regexp.MustCompile(
	`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})[\d.]*\s+` +
		`from\s+([\d.:\[\]a-fA-F]+?):\d+\s+` +
		`(accepted|rejected)\s+` +
		`(\w+):` +
		`(.+):(\d+)\s+` +
		`\[(?:(.+?)\s*(?:==>|>>|->)\s*)?(.+?)\]\s+` +
		`email:\s*(.+)$`,
)

func regexReference(line string) *Entry {
	line = strings.TrimRight(line, "\r\n")
	if !strings.Contains(line, " email: ") {
		return nil
	}
	matches := referenceRE.FindStringSubmatch(line)
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

// diffCorpus is the set of lines the fast parser must handle exactly like the
// old regexp. It covers every shape xray-core actually emits.
var diffCorpus = []string{
	// --- realistic accepted/rejected lines ---
	"2024/01/15 10:30:45 from 192.168.1.100:54321 accepted tcp:www.google.com:443 [vless-in >> direct] email: user_123_a1b2c3d4",
	"2024/06/20 08:15:00 from 10.0.0.5:12345 rejected tcp:blocked.example.com:80 [vmess-in >> blackhole] email: user_456_deadbeef",
	"2024/03/10 12:00:00 from 172.16.0.1:9999 accepted udp:dns.google:53 [trojan-in >> freedom-0] email: manual_789_cafe1234",
	// --- IP (no domain) destination ---
	"2024/02/01 00:00:00 from 1.2.3.4:1000 accepted tcp:93.184.216.34:443 [vless-in >> direct] email: user_1_abcd1234",
	// --- fractional seconds + different arrows ---
	"2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main",
	"2026/03/07 17:05:46.572365 from 5.126.80.233:0 accepted tcp:web.whatsapp.com:443 [vless -> wg-out] email: RubikDentistry",
	"2026/03/07 12:00:00.000000 from 10.0.0.1:12345 accepted tcp:example.com:443 [vless ==> direct] email: user1",
	// --- no inbound tag (single outbound) ---
	"2026/03/07 12:00:00.000000 from 10.0.0.1:12345 accepted tcp:example.com:80 [direct] email: user2",
	// --- accepted -> blocked flips to rejected ---
	"2026/03/08 12:00:00.123456 from 5.126.80.233:0 accepted tcp:20.33.92.5:443 [vless -> blocked] email: RubikDentistry",
	// --- trailing newline (from bufio ReadString) ---
	"2026/03/07 17:05:46.035211 from 5.250.104.59:0 accepted udp:8.8.4.4:53 [vless -> wg-out] email: main\n",
	// --- >> arrow with @-style email ---
	"2024/03/15 14:30:45 from 1.2.3.4:5678 accepted tcp:example.com:443 [in >> out] email: test@user",
	// --- IPv6 source (bracketed) ---
	"2024/01/01 00:00:00 from [2001:db8::1]:443 accepted tcp:example.com:443 [vless-in >> direct] email: v6src",
	// --- IPv6 destination (bracketed) ---
	"2024/01/01 00:00:00 from 1.2.3.4:5 accepted tcp:[2001:db8::2]:443 [vless-in >> direct] email: v6dst",
	// --- high port + hyphenated tags ---
	"2024/01/01 00:00:00 from 1.2.3.4:5 accepted tcp:cdn.example.co.uk:8443 [ws-in >> proxy-out-2] email: u_hyphen",
	// --- lines that must parse to nil in both ---
	"2024/01/15 10:30:45 [Info] [DNS] google.com got answer: 142.250.80.46",
	"2024/01/15 10:30:45 [Warning] failed to handler mux client connection",
	"",
	// passes the " email: " pre-filter but is not an access line -> nil in both
	"2024/01/01 10:00:00 garbage email: x",
	"random text with email: value but no structure",
}

func TestParse_MatchesRegexReference(t *testing.T) {
	p := NewParser()
	for _, line := range diffCorpus {
		got := p.Parse(line)
		want := regexReference(line)
		assertEntriesEqual(t, line, got, want)
	}
}

func assertEntriesEqual(t *testing.T, line string, got, want *Entry) {
	t.Helper()
	if want == nil || got == nil {
		if got != want { // one nil, one not
			t.Errorf("nil mismatch for %q:\n  manual = %+v\n  regex  = %+v", line, got, want)
		}
		return
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp mismatch for %q: manual=%v regex=%v", line, got.Timestamp, want.Timestamp)
	}
	if got.SourceIP != want.SourceIP {
		t.Errorf("SourceIP mismatch for %q: manual=%q regex=%q", line, got.SourceIP, want.SourceIP)
	}
	if got.Status != want.Status {
		t.Errorf("Status mismatch for %q: manual=%q regex=%q", line, got.Status, want.Status)
	}
	if got.Network != want.Network {
		t.Errorf("Network mismatch for %q: manual=%q regex=%q", line, got.Network, want.Network)
	}
	if got.Domain != want.Domain {
		t.Errorf("Domain mismatch for %q: manual=%q regex=%q", line, got.Domain, want.Domain)
	}
	if got.Port != want.Port {
		t.Errorf("Port mismatch for %q: manual=%d regex=%d", line, got.Port, want.Port)
	}
	if got.InboundTag != want.InboundTag {
		t.Errorf("InboundTag mismatch for %q: manual=%q regex=%q", line, got.InboundTag, want.InboundTag)
	}
	if got.OutboundTag != want.OutboundTag {
		t.Errorf("OutboundTag mismatch for %q: manual=%q regex=%q", line, got.OutboundTag, want.OutboundTag)
	}
	if got.Email != want.Email {
		t.Errorf("Email mismatch for %q: manual=%q regex=%q", line, got.Email, want.Email)
	}
}

const benchLine = "2026/03/07 17:05:46.035211 from 5.250.104.59:54321 accepted tcp:web.whatsapp.com:443 [vless-in >> wg-out] email: RubikDentistry"

func BenchmarkParseManual(b *testing.B) {
	p := NewParser()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if p.Parse(benchLine) == nil {
			b.Fatal("unexpected nil")
		}
	}
}

func BenchmarkParseRegex(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if regexReference(benchLine) == nil {
			b.Fatal("unexpected nil")
		}
	}
}
