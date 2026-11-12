package realitydest

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// A caller's own dialer is what reaches dest, for every server name.
func TestProbeDialsWithTheCallersDialer(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13, CurvePreferences: []tls.CurveID{tls.X25519}}
	srv.StartTLS()
	defer srv.Close()
	dest := srv.Listener.Addr().String()

	var mu sync.Mutex
	var dialed []string
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, network+" "+address)
		mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	report, err := Probe(context.Background(), Request{Dest: dest, ServerNames: []string{"a.example", "b.example"}, Dial: dial})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checks) != 2 || !report.OK {
		t.Fatalf("report %+v", report)
	}
	if len(dialed) != 2 || dialed[0] != "tcp "+dest || dialed[1] != "tcp "+dest {
		t.Fatalf("dialed %v, want dest twice through the caller's dialer", dialed)
	}

	// what the caller's dialer can't reach is a failed check, not an error
	refuse := func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("no route from here") }
	report, err = Probe(context.Background(), Request{Dest: dest, ServerNames: []string{"a.example"}, Dial: refuse})
	if err != nil {
		t.Fatal(err)
	}
	if c := report.Checks[0]; c.OK || c.Reason != ReasonDialFailed {
		t.Fatalf("check %+v", c)
	}
}
