package http

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

func TestFlowEndpointWrapsEnvelope(t *testing.T) {
	uc := &stubUsecase{flowView: &usecase.FlowView{
		Nodes:    []usecase.FlowNode{{ID: "src-lan", Label: "LAN clients", Status: "ok"}},
		Edges:    []usecase.FlowEdge{{ID: "e-lan-for", From: "src-lan", To: "mark-foreign"}},
		Counters: map[string]usecase.FlowCounter{"if:eth0": {RxBytes: 10}},
	}}
	w := do(t, newRouter(t, uc, true), "GET", "/api/v1/network/flow", "")
	if w.Code != 200 {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
	// The frontend unwraps {success, data} and treats a bare body as a failure.
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Nodes    []usecase.FlowNode             `json:"nodes"`
			Counters map[string]usecase.FlowCounter `json:"counters"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Success || len(body.Data.Nodes) != 1 || body.Data.Nodes[0].ID != "src-lan" {
		t.Fatalf("%s", w.Body.String())
	}
	if body.Data.Counters["if:eth0"].RxBytes != 10 {
		t.Fatalf("counters lost: %s", w.Body.String())
	}
}

func TestTracePassesTheRequestThrough(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/flow/trace",
		`{"dest":"1.1.1.1","source":"lan"}`)
	if w.Code != 200 {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
	var body struct {
		Data usecase.TraceView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Dest != "1.1.1.1" || body.Data.Source != "lan" {
		t.Fatalf("%+v", body.Data)
	}
}

func TestTraceBadInputIs400(t *testing.T) {
	uc := &stubUsecase{traceErr: fmt.Errorf("%w: bad", usecase.ErrBadTraceInput)}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/flow/trace",
		`{"dest":"","source":"lan"}`)
	if w.Code != 400 {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
}

func TestFlowEventsAlwaysRendersAnArray(t *testing.T) {
	// nil would serialize as null, and the page maps over it.
	w := do(t, newRouter(t, &stubUsecase{}, true), "GET", "/api/v1/network/flow/events", "")
	var body struct {
		Data []events.Event `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data == nil {
		t.Fatalf("null events: %s", w.Body.String())
	}
}

func TestFlowRoutes404WithoutRouterMode(t *testing.T) {
	r := newRouter(t, &stubUsecase{}, false)
	for _, p := range []string{
		"/api/v1/network/flow", "/api/v1/network/flow/conns", "/api/v1/network/flow/events",
	} {
		if w := do(t, r, "GET", p, ""); w.Code != 404 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := do(t, r, "POST", "/api/v1/network/flow/trace", `{"dest":"1.1.1.1","source":"lan"}`); w.Code != 404 {
		t.Errorf("trace: %d", w.Code)
	}
}
