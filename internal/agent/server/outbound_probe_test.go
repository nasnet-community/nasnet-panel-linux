package server

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
)

func TestOutboundProbeRequiresExplicitInsecureTLS(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	upstream.Config.ErrorLog = log.New(io.Discard, "", 0)
	upstream.StartTLS()
	defer upstream.Close()

	server := &Server{}
	request := &pb.OutboundTestRequest{TestUrl: upstream.URL, DirectProbe: true}
	response, err := server.TestOutbound(context.Background(), request)
	if err != nil || response.Success || response.Error == "" {
		t.Fatalf("probe accepted an untrusted certificate without opt-in: %+v, %v", response, err)
	}
	request.InsecureTls = true
	response, err = server.TestOutbound(context.Background(), request)
	if err != nil || !response.Success {
		t.Fatalf("explicit insecure probe failed: %+v, %v", response, err)
	}
}
