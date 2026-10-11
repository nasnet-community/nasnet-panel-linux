package cmd

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nasnet-community/mtproto-remote-plugin/link"
	"github.com/nasnet-community/mtproto-remote-plugin/tgnet"
	"github.com/nasnet-community/nasnet-panel-linux/internal/mtprotonode"
)

// SSH to a VPS must leave the way the MTProto node's link does: a domestic line in router mode.
func TestMTProtoSSHLeavesThroughTheLinkDialer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var (
		mu      sync.Mutex
		dialled []string
	)
	viaLink := func(_ context.Context, network, addr string) (net.Conn, error) {
		mu.Lock()
		dialled = append(dialled, network+" "+addr)
		mu.Unlock()
		return nil, errors.New("the test's dialer goes nowhere")
	}
	m := mtprotonode.NewManager(mtprotonode.Config{Dialers: mtprotonode.Dialers{Link: func(*mtprotonode.WAN) link.DialFunc { return viaLink }}})
	h := newMTProtoAPI(mtprotoDeps{}, nil, m, &tgnet.Refresher{List: tgnet.New(nil)})
	e := gin.New()
	h.RegisterRoutes(e.Group("/api/v1"))

	req := httptest.NewRequest("POST", "/api/v1/mtproto/probe", strings.NewReader(`{"host":"vps.example.com","ssh_port":2222}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("probe: %d %s", w.Code, w.Body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialled) != 1 || dialled[0] != "tcp vps.example.com:2222" {
		t.Fatalf("SSH dialled %q, want it through the link dialer", dialled)
	}
}
