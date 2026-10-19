package http

import (
	"crypto/tls"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// sniffTimeout bounds how long a new connection may stay silent before its
// first byte says whether it speaks TLS.
const sniffTimeout = 10 * time.Second

// tlsAutoListener serves TLS and plain HTTP on one port. A connection whose
// first byte opens a TLS handshake is handed over as a *tls.Conn; anything
// else goes to the HTTP server as plain HTTP, where redirectPlainHTTP decides
// what it may reach. The first byte is read per connection, so a client that
// connects and sends nothing holds up only itself.
type tlsAutoListener struct {
	net.Listener
	tlsConfig *tls.Config
	accepted  chan acceptResult
	done      chan struct{}
	closeOnce sync.Once
}

type acceptResult struct {
	conn net.Conn
	err  error
}

func newTLSAutoListener(ln net.Listener, tlsCfg *tls.Config) *tlsAutoListener {
	l := &tlsAutoListener{
		Listener:  ln,
		tlsConfig: tlsCfg,
		accepted:  make(chan acceptResult),
		done:      make(chan struct{}),
	}
	go l.acceptLoop()
	return l
}

func (l *tlsAutoListener) acceptLoop() {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			// http.Server backs off on temporary errors before it asks again,
			// and stops serving on the rest.
			select {
			case l.accepted <- acceptResult{err: err}:
			case <-l.done:
				return
			}
			continue
		}
		go l.sniff(conn)
	}
}

func (l *tlsAutoListener) sniff(conn net.Conn) {
	first := make([]byte, 1)
	_ = conn.SetReadDeadline(time.Now().Add(sniffTimeout))
	n, err := conn.Read(first)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || n == 0 {
		conn.Close()
		return
	}
	var c net.Conn = &prefixConn{Conn: conn, prefix: first}
	if first[0] == 0x16 { // TLS handshake record
		c = tls.Server(c, l.tlsConfig)
	}
	select {
	case l.accepted <- acceptResult{conn: c}:
	case <-l.done:
		conn.Close()
	}
}

func (l *tlsAutoListener) Accept() (net.Conn, error) {
	select {
	case r := <-l.accepted:
		return r.conn, r.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *tlsAutoListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// redirectPlainHTTP sends plain-HTTP requests to HTTPS on the same host and
// port, except those plainOK lets through. 307, not 301: browsers keep a 301
// for good and would go on jumping to https after HTTPS is turned off.
func redirectPlainHTTP(next http.Handler, plainOK func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil || (plainOK != nil && plainOK(r)) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Host == "" {
			http.Error(w, "Missing Host header", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, "https://"+r.Host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})
}

// SelfSignedPlainHTTP is what a panel on a self-signed certificate still
// serves over plain HTTP: what VPN apps, the sub page, the built-in node's
// downloads and monitoring fetch, none of which can get past a certificate
// nobody vouches for. The panel, its login and its API move to HTTPS.
func (s *Server) SelfSignedPlainHTTP(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/health", strings.HasPrefix(p, "/health/"),
		strings.HasPrefix(p, "/sub/"),
		strings.HasPrefix(p, "/api/v1/public/"),
		s.metricsPath != "" && p == s.metricsPath:
		return true
	}
	rest := p
	if s.basePath != "" {
		var ok bool
		if rest, ok = strings.CutPrefix(p, s.basePath); !ok || !strings.HasPrefix(rest, "/") {
			return false
		}
	}
	switch {
	case strings.HasPrefix(rest, "/sub/"),
		strings.HasPrefix(rest, "/assets/"),
		// the built-in node's Xray downloads; the rest of /deploy is admin-only
		strings.HasPrefix(rest, "/api/v1/deploy/xray/"):
		return true
	}
	// files at the top of the build (logo, icons), which the sub page loads too
	name := strings.TrimPrefix(rest, "/")
	if s.distFS == nil || name == "" || strings.Contains(name, "/") || path.Ext(name) == "" || name == "index.html" {
		return false
	}
	_, err := fs.Stat(s.distFS, name)
	return err == nil
}

// prefixConn is a net.Conn that prepends already-read bytes before the real connection.
type prefixConn struct {
	net.Conn
	prefix []byte
	offset int
}

// CloseWrite lets http.Server half-close a plain connection, so a redirect
// answering a request whose body it didn't read isn't lost to a reset.
func (c *prefixConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if c.offset < len(c.prefix) {
		n := copy(b, c.prefix[c.offset:])
		c.offset += n
		if n < len(b) {
			m, err := c.Conn.Read(b[n:])
			return n + m, err
		}
		return n, nil
	}
	return c.Conn.Read(b)
}
