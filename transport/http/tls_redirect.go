package http

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// tlsAutoListener wraps a net.Listener to auto-detect TLS vs plain HTTP
// on the same port. TLS connections are passed through as tls.Conn.
// Plain HTTP connections receive a 301 redirect to HTTPS and are closed.
type tlsAutoListener struct {
	net.Listener
	tlsConfig    *tls.Config
	fallbackHost string // used in redirect if request has no Host header
}

// newTLSAutoListener creates a listener that serves TLS and redirects plain HTTP.
func newTLSAutoListener(ln net.Listener, tlsCfg *tls.Config, fallbackHost string) net.Listener {
	return &tlsAutoListener{
		Listener:     ln,
		tlsConfig:    tlsCfg,
		fallbackHost: fallbackHost,
	}
}

func (l *tlsAutoListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		// Peek at first byte with a short timeout
		peeked := make([]byte, 1)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, err := conn.Read(peeked)
		conn.SetReadDeadline(time.Time{})
		if err != nil || n == 0 {
			conn.Close()
			continue
		}

		if peeked[0] == 0x16 {
			// TLS ClientHello — wrap as TLS connection
			tlsConn := tls.Server(&prefixConn{Conn: conn, prefix: peeked[:n]}, l.tlsConfig)
			return tlsConn, nil
		}

		// Plain HTTP — redirect to HTTPS in a goroutine and accept next connection
		go l.redirectHTTP(conn, peeked[:n])
	}
}

// redirectHTTP reads the HTTP request path and sends a 301 redirect to HTTPS.
func (l *tlsAutoListener) redirectHTTP(conn net.Conn, firstByte []byte) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	reader := bufio.NewReader(&prefixConn{Conn: conn, prefix: firstByte})

	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	// Use the Host header from the request (preserves original host:port)
	host := req.Host
	if host == "" {
		host = l.fallbackHost
	}
	target := "https://" + host + req.URL.RequestURI()
	body := fmt.Sprintf("<html><body>Redirecting to <a href=%q>%s</a></body></html>\n", target, target)

	resp := fmt.Sprintf("HTTP/1.1 301 Moved Permanently\r\n"+
		"Location: %s\r\n"+
		"Content-Type: text/html\r\n"+
		"Content-Length: %d\r\n"+
		"Connection: close\r\n"+
		"\r\n%s", target, len(body), body)

	conn.Write([]byte(resp))

	log := logger.GetLogger()
	log.WithField("target", target).Debug("Redirected HTTP to HTTPS")
}

// prefixConn is a net.Conn that prepends already-read bytes before the real connection.
type prefixConn struct {
	net.Conn
	prefix []byte
	offset int
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
