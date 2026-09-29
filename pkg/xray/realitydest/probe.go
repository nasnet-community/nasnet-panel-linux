// Package realitydest checks whether a site can serve as the dest of a REALITY
// inbound. A REALITY server forwards the client's ClientHello to dest and
// hijacks the handshake only when dest answers with a TLS 1.3 ServerHello that
// carries an X25519 or X25519MLKEM768 key share, followed by a ChangeCipherSpec
// and encrypted records no larger than 8 KB (github.com/xtls/reality tls.go).
// Anything else makes REALITY relay the real handshake, and clients report
// "received real certificate". The probe performs the same handshake a client
// would and applies those rules to the bytes the site sends back.
package realitydest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pires/go-proxyproto"
)

// Request describes one dest to verify, using the fields Xray reads.
type Request struct {
	Dest        string        // "host:port", a bare port (localhost) or a unix socket path
	Type        string        // "tcp" or "unix"; derived from Dest when empty, like Xray does
	Xver        uint64        // PROXY protocol version written before TLS (0, 1 or 2)
	ServerNames []string      // SNIs clients will present; each one is checked
	Timeout     time.Duration // per server name; DefaultTimeout when zero
}

// DefaultTimeout bounds connect plus handshake for one server name.
const DefaultTimeout = 8 * time.Second

// MaxTimeout caps what a caller may ask for.
const MaxTimeout = 20 * time.Second

// maxRecordBytes mirrors reality's `size`: a server record longer than this,
// header included, aborts the hijack.
const maxRecordBytes = 8192

// Reason codes explain why a server name failed. They are stable for the UI.
const (
	ReasonDialFailed      = "dial_failed"
	ReasonHandshakeFailed = "handshake_failed"
	ReasonNoResponse      = "no_response"
	ReasonHelloRetry      = "hello_retry_request"
	ReasonTLSVersion      = "tls_version"
	ReasonCipherSuite     = "cipher_suite"
	ReasonKeyShare        = "key_share"
	ReasonNoCCS           = "no_ccs"
	ReasonRecordVersion   = "record_version"
	ReasonUnexpectedRec   = "unexpected_record"
	ReasonRecordTooLarge  = "record_too_large"
)

// Check is the outcome for one server name.
type Check struct {
	ServerName         string     `json:"server_name"`
	OK                 bool       `json:"ok"`
	Reason             string     `json:"reason,omitempty"`
	Message            string     `json:"message,omitempty"`
	TLSVersion         string     `json:"tls_version,omitempty"`
	KeyShare           string     `json:"key_share,omitempty"`
	CipherSuite        string     `json:"cipher_suite,omitempty"`
	ALPN               string     `json:"alpn,omitempty"`
	HandshakeMs        int64      `json:"handshake_ms"`
	FlightBytes        int        `json:"flight_bytes"`
	LargestRecordBytes int        `json:"largest_record_bytes"`
	CertSubject        string     `json:"cert_subject,omitempty"`
	CertIssuer         string     `json:"cert_issuer,omitempty"`
	CertNotAfter       *time.Time `json:"cert_not_after,omitempty"`
	CertError          string     `json:"cert_error,omitempty"`
	Warnings           []string   `json:"warnings,omitempty"`
}

// Report collects the checks for every server name.
type Report struct {
	OK          bool      `json:"ok"`
	Dest        string    `json:"dest"`
	CheckedFrom string    `json:"checked_from,omitempty"` // "node" or "hub"; filled in by the caller
	Summary     string    `json:"summary"`
	Checks      []Check   `json:"checks"`
	Warnings    []string  `json:"warnings,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// Validate rejects requests Xray itself would refuse.
func (r Request) Validate() error {
	if _, _, err := resolveDest(r.Dest, r.Type); err != nil {
		return err
	}
	if r.Xver > 2 {
		return errors.New(`"xver" only accepts 0, 1 or 2`)
	}
	if len(dedupe(r.ServerNames)) == 0 {
		return errors.New("at least one server name is required")
	}
	if r.Timeout < 0 || r.Timeout > MaxTimeout {
		return fmt.Errorf("timeout must be within %s", MaxTimeout)
	}
	return nil
}

// resolveDest mirrors infra/conf REALITYConfig.Build: a bare port means
// localhost, a leading '@' or '/' means a unix socket, anything else must be
// host:port.
func resolveDest(dest, typ string) (network, address string, err error) {
	s := strings.TrimSpace(dest)
	if s == "" {
		return "", "", errors.New("destination is empty")
	}
	if typ == "" {
		switch s[0] {
		case '@', '/':
			typ = "unix"
		default:
			if _, e := strconv.Atoi(s); e == nil {
				s = "localhost:" + s
			}
			if _, _, e := net.SplitHostPort(s); e == nil {
				typ = "tcp"
			}
		}
	}
	switch typ {
	case "tcp", "unix":
		return typ, s, nil
	}
	return "", "", fmt.Errorf("%q is not host:port, a port or a unix socket path", dest)
}

func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// Probe checks every server name against dest. Network failures are reported
// inside the checks; the error covers invalid requests only.
func Probe(ctx context.Context, req Request) (*Report, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	network, address, _ := resolveDest(req.Dest, req.Type)
	timeout := req.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	names := dedupe(req.ServerNames)
	report := &Report{Dest: req.Dest, Checks: make([]Check, len(names)), CheckedAt: time.Now()}

	// A handful of names at once keeps the check quick without hammering dest.
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				report.Checks[i] = Check{ServerName: name, Reason: ReasonDialFailed, Message: "Check cancelled before it could start."}
				return
			}
			probeCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			report.Checks[i] = probeOne(probeCtx, network, address, req.Xver, name)
		}(i, name)
	}
	wg.Wait()

	report.OK = true
	for _, c := range report.Checks {
		if !c.OK {
			report.OK = false
		}
	}
	report.Summary = summarize(report)
	report.Warnings = destWarnings(names)
	return report, nil
}

func summarize(r *Report) string {
	failed := 0
	var first *Check
	for i := range r.Checks {
		if !r.Checks[i].OK {
			failed++
			if first == nil {
				first = &r.Checks[i]
			}
		}
	}
	switch {
	case failed == 0:
		return "Ready for REALITY"
	case len(r.Checks) == 1:
		return first.Message
	case failed == len(r.Checks):
		return fmt.Sprintf("None of the %d server names can be mirrored", len(r.Checks))
	default:
		return fmt.Sprintf("%d of %d server names cannot be mirrored", failed, len(r.Checks))
	}
}

// destWarnings repeats the caution Xray logs for names that draw attention.
func destWarnings(names []string) []string {
	var flagged []string
	for _, n := range names {
		l := strings.ToLower(n)
		if strings.HasSuffix(l, ".ru") || strings.HasSuffix(l, ".ir") || strings.HasSuffix(l, ".cn") ||
			strings.Contains(l, "apple") || strings.Contains(l, "icloud") || strings.Contains(l, "microsoft") {
			flagged = append(flagged, n)
		}
	}
	if len(flagged) == 0 {
		return nil
	}
	sort.Strings(flagged)
	return []string{fmt.Sprintf("Xray warns that %s raises the chance of the server IP being blocked: .ir, .ru and .cn names and Apple, iCloud or Microsoft properties are watched closely.", strings.Join(flagged, ", "))}
}

// subject names the dest in messages: hostnames stay as typed, the SNI-less
// case reads as prose at either position in a sentence.
type subject struct{ start, mid string }

func displayName(sni string) subject {
	if sni == "" {
		return subject{start: "The destination (no SNI)", mid: "the destination"}
	}
	return subject{start: sni, mid: sni}
}

func probeOne(ctx context.Context, network, address string, xver uint64, sni string) Check {
	check := Check{ServerName: sni}
	who := displayName(sni)

	dialer := &net.Dialer{}
	raw, err := dialer.DialContext(ctx, network, address)
	if err != nil {
		check.Reason, check.Message = ReasonDialFailed, fmt.Sprintf("Could not connect to %s: %s", address, trimErr(err))
		return check
	}
	defer raw.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	if xver == 1 || xver == 2 {
		if _, err = proxyproto.HeaderProxyFromAddrs(byte(xver), raw.LocalAddr(), raw.RemoteAddr()).WriteTo(raw); err != nil {
			check.Reason, check.Message = ReasonDialFailed, fmt.Sprintf("Could not send the PROXY protocol header to %s: %s", address, trimErr(err))
			return check
		}
	}

	rec := &recorder{Conn: raw}
	conn := tls.Client(rec, &tls.Config{
		ServerName: sni,
		// The chain is verified by hand below and reported as a warning: an
		// untrusted dest still works for REALITY, it just looks odd to probers.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		// Chrome's supported_groups. Key shares go out for the first two, so
		// a dest that prefers a NIST curve has to answer with HelloRetryRequest
		// exactly as it would for a real client.
		CurvePreferences: []tls.CurveID{tls.X25519MLKEM768, tls.X25519, tls.CurveP256, tls.CurveP384},
		NextProtos:       []string{"h2", "http/1.1"},
	})
	start := time.Now()
	hsErr := conn.HandshakeContext(ctx)
	check.HandshakeMs = time.Since(start).Milliseconds()

	flight := parseFlight(rec.bytes())
	check.FlightBytes = flight.total
	check.LargestRecordBytes = flight.largest
	if flight.hello != nil {
		check.TLSVersion = flight.hello.versionName()
		check.KeyShare = groupName(flight.hello.group)
		check.CipherSuite = tls.CipherSuiteName(flight.hello.cipherSuite)
	}

	if reason, msg := flight.verdict(who); reason != "" {
		check.Reason, check.Message = reason, msg
		return check
	}
	if hsErr != nil {
		check.Reason, check.Message = ReasonHandshakeFailed, fmt.Sprintf("TLS handshake with %s failed: %s", who.mid, trimErr(hsErr))
		return check
	}

	state := conn.ConnectionState()
	check.ALPN = state.NegotiatedProtocol
	if len(state.PeerCertificates) > 0 {
		leaf := state.PeerCertificates[0]
		check.CertSubject = leaf.Subject.CommonName
		check.CertIssuer = leaf.Issuer.CommonName
		notAfter := leaf.NotAfter
		check.CertNotAfter = &notAfter
		if err := verifyChain(state.PeerCertificates, sni); err != nil {
			check.CertError = trimErr(err)
			check.Warnings = append(check.Warnings, fmt.Sprintf("The certificate %s presents does not verify (%s). Probers relayed to it will notice.", who.mid, check.CertError))
		}
	}
	if check.ALPN != "h2" {
		check.Warnings = append(check.Warnings, fmt.Sprintf("%s does not offer HTTP/2. REALITY guides recommend a dest that speaks TLS 1.3 with h2.", who.start))
	}
	if flight.total > maxRecordBytes {
		check.Warnings = append(check.Warnings, fmt.Sprintf("The whole handshake is %d bytes. REALITY buffers 8 KB, so a slow link can push it into relay mode; a dest with a smaller certificate chain is safer.", flight.total))
	}
	check.OK = true
	return check
}

func verifyChain(chain []*x509.Certificate, sni string) error {
	opts := x509.VerifyOptions{Intermediates: x509.NewCertPool()}
	if net.ParseIP(sni) == nil {
		opts.DNSName = sni
	}
	for _, c := range chain[1:] {
		opts.Intermediates.AddCert(c)
	}
	_, err := chain[0].Verify(opts)
	return err
}

func trimErr(err error) string {
	s := err.Error()
	for _, p := range []string{"tls: ", "remote error: "} {
		s = strings.ReplaceAll(s, p, "")
	}
	return s
}

// recorder keeps a copy of everything the server sent so the record layout can
// be judged after the handshake.
type recorder struct {
	net.Conn
	mu  sync.Mutex
	buf []byte
}

const recorderLimit = 64 << 10

func (r *recorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.mu.Lock()
		if len(r.buf) < recorderLimit {
			r.buf = append(r.buf, p[:n]...)
		}
		r.mu.Unlock()
	}
	return n, err
}

func (r *recorder) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte(nil), r.buf...)
}
