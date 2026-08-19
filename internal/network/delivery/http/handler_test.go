package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/network/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
)

type stubUsecase struct {
	planCalled  bool
	applyCalled bool
	confirmedID uint
	planView    *usecase.PlanView
	applyErr    error

	portForwardVerdicts []domain.Verdict
	lanVerdicts         []domain.Verdict
	deletedPFID         uint

	devices     *usecase.LANDeviceList
	devicesErr  error
	labelledMAC string
	labelledAs  string
	setLabelErr error

	vpnProfiles   []usecase.VPNProfileView
	vpnCreated    *usecase.CreateVPNProfileRequest
	vpnCreateErr  error
	vpnDeletedID  uint
	vpnDeleteErr  error
	vpnParsed     string
	vpnParseErr   error
	vpnVerdicts   []domain.Verdict
	vpnActivateID uint
	vpnApplyView  *usecase.ApplyView
	vpnApplyErr   error
	vpnDeactived  bool
	vpnStatus     *usecase.VPNStatusView

	flowView   *usecase.FlowView
	traceView  *usecase.TraceView
	traceErr   error
	connsView  *usecase.FlowConnsView
	flowEvents []events.Event
}

func (s *stubUsecase) GetLAN(context.Context) (*usecase.LANView, error) {
	return &usecase.LANView{
		LANConfig:     domain.LANConfig{BridgeName: "lan0", CIDR: "10.77.0.1/24"},
		GeoIPPrefixes: 2027, DomainLayer: true,
	}, nil
}

func (s *stubUsecase) UpdateLAN(_ context.Context, _ domain.LANConfig) ([]domain.Verdict, *usecase.ApplyView, error) {
	if domain.Rejected(s.lanVerdicts) {
		return s.lanVerdicts, nil, nil
	}
	return s.lanVerdicts, &usecase.ApplyView{PlanID: 9, ConfirmDeadlineUnix: 1_800_000_090}, nil
}

func (s *stubUsecase) ListPortForwards(context.Context) ([]domain.PortForward, error) {
	return []domain.PortForward{}, nil
}

func (s *stubUsecase) ListDevices(context.Context) (*usecase.LANDeviceList, error) {
	return s.devices, s.devicesErr
}

func (s *stubUsecase) SetDeviceLabel(_ context.Context, mac, label string) error {
	s.labelledMAC, s.labelledAs = mac, label
	return s.setLabelErr
}

func (s *stubUsecase) ListVPNProfiles(context.Context) ([]usecase.VPNProfileView, error) {
	return s.vpnProfiles, nil
}

func (s *stubUsecase) CreateVPNProfile(_ context.Context, req usecase.CreateVPNProfileRequest) (*usecase.VPNProfileView, error) {
	s.vpnCreated = &req
	if s.vpnCreateErr != nil {
		return nil, s.vpnCreateErr
	}
	return &usecase.VPNProfileView{ID: 1, Name: req.Name}, nil
}

func (s *stubUsecase) UpdateVPNProfile(_ context.Context, id uint, req usecase.CreateVPNProfileRequest) (*usecase.VPNProfileView, error) {
	s.vpnCreated = &req
	if s.vpnCreateErr != nil {
		return nil, s.vpnCreateErr
	}
	return &usecase.VPNProfileView{ID: id, Name: req.Name}, nil
}

func (s *stubUsecase) DeleteVPNProfile(_ context.Context, id uint) error {
	s.vpnDeletedID = id
	return s.vpnDeleteErr
}

func (s *stubUsecase) ParseVPNInput(_ context.Context, raw string) (*domain.WireGuardConfig, []domain.Verdict, error) {
	s.vpnParsed = raw
	if s.vpnParseErr != nil {
		return nil, nil, s.vpnParseErr
	}
	return &domain.WireGuardConfig{Address: "10.66.0.2/32"}, s.vpnVerdicts, nil
}

func (s *stubUsecase) GenerateVPNKeypair() (string, string, error) {
	return "private", "public", nil
}

func (s *stubUsecase) ActivateVPN(_ context.Context, id uint) ([]domain.Verdict, *usecase.ApplyView, error) {
	s.vpnActivateID = id
	return s.vpnVerdicts, s.vpnApplyView, s.vpnApplyErr
}

func (s *stubUsecase) DeactivateVPN(context.Context) ([]domain.Verdict, *usecase.ApplyView, error) {
	s.vpnDeactived = true
	return s.vpnVerdicts, s.vpnApplyView, s.vpnApplyErr
}

func (s *stubUsecase) VPNStatus(context.Context) (*usecase.VPNStatusView, error) {
	if s.vpnStatus == nil {
		return &usecase.VPNStatusView{KillSwitch: true}, nil
	}
	return s.vpnStatus, nil
}

func (s *stubUsecase) FlowGraph(context.Context) (*usecase.FlowView, error) {
	if s.flowView != nil {
		return s.flowView, nil
	}
	return &usecase.FlowView{
		Nodes: []usecase.FlowNode{}, Edges: []usecase.FlowEdge{},
		Mismatches: []usecase.FlowMismatch{},
		Counters:   map[string]usecase.FlowCounter{},
	}, nil
}

func (s *stubUsecase) TraceFlow(_ context.Context, req usecase.TraceRequest) (*usecase.TraceView, error) {
	if s.traceErr != nil {
		return nil, s.traceErr
	}
	if s.traceView != nil {
		return s.traceView, nil
	}
	return &usecase.TraceView{Dest: req.Dest, Source: req.Source}, nil
}

func (s *stubUsecase) FlowConns(context.Context) (*usecase.FlowConnsView, error) {
	if s.connsView != nil {
		return s.connsView, nil
	}
	return &usecase.FlowConnsView{Flows: []usecase.FlowConn{}}, nil
}

func (s *stubUsecase) RecentNetworkEvents(context.Context) ([]events.Event, error) {
	return s.flowEvents, nil
}

func (s *stubUsecase) CreatePortForward(_ context.Context, _ domain.PortForward, confirmed bool) ([]domain.Verdict, error) {
	return s.portForwardVerdicts, stubVerdictErr(s.portForwardVerdicts, confirmed)
}

func (s *stubUsecase) UpdatePortForward(_ context.Context, _ domain.PortForward, confirmed bool) ([]domain.Verdict, error) {
	return s.portForwardVerdicts, stubVerdictErr(s.portForwardVerdicts, confirmed)
}

func (s *stubUsecase) DeletePortForward(_ context.Context, id uint) error {
	s.deletedPFID = id
	return nil
}

func (s *stubUsecase) OnInboundsChanged(context.Context) error               { return nil }
func (s *stubUsecase) RefreshDomesticRanges(context.Context) error           { return nil }
func (s *stubUsecase) StartRangesRefreshLoop(context.Context, time.Duration) {}

func stubVerdictErr(vs []domain.Verdict, confirmed bool) error {
	if domain.Rejected(vs) {
		return usecase.ErrValidationFailed
	}
	for _, v := range vs {
		if v.Level == domain.LevelConfirm && !confirmed {
			return usecase.ErrConfirmRequired
		}
	}
	return nil
}

func (s *stubUsecase) Enumerate(context.Context) ([]usecase.InterfaceView, error) {
	return []usecase.InterfaceView{{Role: "wan", Slot: "domestic"}}, nil
}

func (s *stubUsecase) State(context.Context) (*usecase.StateView, error) {
	return &usecase.StateView{RouterMode: true, TakeoverDone: false,
		Warnings: []string{"network not managed by nasnet yet — assign roles to finish setup"}}, nil
}

func (s *stubUsecase) Plan(_ context.Context, _ domain.ChangeRequest) (*usecase.PlanView, error) {
	s.planCalled = true
	if s.planView != nil {
		return s.planView, nil
	}
	return &usecase.PlanView{Ops: []string{"move netplan configuration aside"}}, nil
}

func (s *stubUsecase) Apply(_ context.Context, _ domain.ChangeRequest) (*usecase.ApplyView, error) {
	s.applyCalled = true
	if s.applyErr != nil {
		return nil, s.applyErr
	}
	return &usecase.ApplyView{PlanID: 7, ConfirmDeadlineUnix: 1_800_000_090}, nil
}

func (s *stubUsecase) Confirm(_ context.Context, id uint) error       { s.confirmedID = id; return nil }
func (s *stubUsecase) Rollback(context.Context) error                 { return nil }
func (s *stubUsecase) Reconcile(context.Context) error                { return nil }
func (s *stubUsecase) StartHealthLoop(context.Context, time.Duration) {}

func (s *stubUsecase) SetHealthConfig(usecase.HealthConfig) {}

func (s *stubUsecase) HealthState(context.Context) (*usecase.HealthView, error) {
	return &usecase.HealthView{}, nil
}

func (s *stubUsecase) SetUplinkForce(context.Context, string, string) error { return nil }
func (s *stubUsecase) Groups(context.Context) ([]domain.WANGroup, error) {
	return []domain.WANGroup{{Name: "domestic"}}, nil
}
func (s *stubUsecase) SetLabel(context.Context, string, string) error { return nil }
func (s *stubUsecase) IngressUplinkIfName() string                    { return "enp1s0" }

func newRouter(t *testing.T, uc usecase.NetworkUsecase, routerMode bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(uc, routerMode).RegisterRoutes(r.Group("/api/v1"))
	return r
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// 404, not 500 and not an empty 200 — that's what hides the section in the UI.
func TestRoutes_404WhenRouterModeIsOff(t *testing.T) {
	r := newRouter(t, &stubUsecase{}, false)
	for _, path := range []string{
		"/api/v1/network/interfaces", "/api/v1/network/state", "/api/v1/network/groups",
	} {
		if w := do(t, r, "GET", path, ""); w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}
	if w := do(t, r, "POST", "/api/v1/network/apply", "{}"); w.Code != http.StatusNotFound {
		t.Errorf("POST apply = %d, want 404", w.Code)
	}
}

func TestGetInterfaces(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "GET", "/api/v1/network/interfaces", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool                    `json:"success"`
		Data    []usecase.InterfaceView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Data) != 1 || resp.Data[0].Slot != "domestic" {
		t.Errorf("resp = %+v", resp)
	}
}

// The plan endpoint is a dry run and must never apply.
func TestPostPlan_IsADryRun(t *testing.T) {
	uc := &stubUsecase{}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/plan",
		`{"interface_id":1,"role":"wan","slot":"domestic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !uc.planCalled {
		t.Error("Plan was not called")
	}
	if uc.applyCalled {
		t.Error("the dry-run endpoint applied the change")
	}
}

// A rejected plan is a 400 carrying the verdicts, so the UI names the rule.
func TestPostApply_RejectedPlanReturnsTheVerdicts(t *testing.T) {
	uc := &stubUsecase{planView: &usecase.PlanView{Verdicts: []domain.Verdict{
		{Rule: "V8", Level: domain.LevelReject, Message: "enp1s0 already holds the lan role"},
	}}}
	w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/apply",
		`{"interface_id":2,"role":"lan"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "V8") {
		t.Errorf("response does not name the failing rule: %s", w.Body.String())
	}
	if uc.applyCalled {
		t.Error("a rejected plan was applied anyway")
	}
}

func TestPostApply_ReturnsThePlanIDAndDeadline(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "POST", "/api/v1/network/apply",
		`{"interface_id":1,"role":"wan","slot":"domestic"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "1800000090") {
		t.Errorf("response has no confirm deadline: %s", w.Body.String())
	}
}

// The box may re-address itself mid-apply, so the UI can't always know the id.
func TestPostConfirm_AcceptsAnEmptyBody(t *testing.T) {
	uc := &stubUsecase{}
	if w := do(t, newRouter(t, uc, true), "POST", "/api/v1/network/confirm", ""); w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if uc.confirmedID != 0 {
		t.Errorf("confirmedID = %d, want 0 for an empty body", uc.confirmedID)
	}
}

// State must work pre-takeover so the UI can show the finish-setup banner.
func TestGetState_ReportsPreTakeover(t *testing.T) {
	w := do(t, newRouter(t, &stubUsecase{}, true), "GET", "/api/v1/network/state", "")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "finish setup") {
		t.Errorf("state does not carry the finish-setup warning: %s", w.Body.String())
	}
}
