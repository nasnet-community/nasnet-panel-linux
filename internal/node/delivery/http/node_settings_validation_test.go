package http

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type settingsUpdateUsecase struct {
	usecase.NodeUsecase
	saved *domain.Node
	left  bool
}

func (u *settingsUpdateUsecase) UsersLeftOn(uint) bool { return u.left }

func (u *settingsUpdateUsecase) GetNode(context.Context, uint) (*domain.Node, error) {
	return &domain.Node{ID: 6, Name: "Existing", IP: "192.0.2.6", AgentPort: 8080, APIPort: 10085}, nil
}
func (u *settingsUpdateUsecase) UpdateNode(_ context.Context, n *domain.Node) error {
	u.saved = n
	return nil
}

func TestNodeSettingsValidationAtHTTPBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name, body string
		valid      bool
	}{
		{"blank name", `{"name":"  "}`, false},
		{"bad country", `{"country_code":"123"}`, false},
		{"bad level", `{"log_level":"verbose"}`, false},
		{"bad interface", `{"bandwidth_settings":{"enabled":true,"interface":"eth0;bad","total_bw":1000}}`, false},
		{"derived router interface", `{"bandwidth_settings":{"enabled":true,"total_bw":1000}}`, true},
		{"explicit router interface override", `{"bandwidth_settings":{"enabled":true,"interface_override":"wan0","total_bw":1000}}`, true},
		{"bad router interface override", `{"bandwidth_settings":{"enabled":true,"interface_override":"wan0;bad","total_bw":1000}}`, false},
		{"bad bandwidth", `{"bandwidth_settings":{"enabled":true,"interface":"eth0","total_bw":0}}`, false},
		{"dish without port", `{"starlink_settings":{"enabled":true,"dish_address":"192.168.100.1"}}`, false},
		{"dish bad port", `{"starlink_settings":{"enabled":true,"dish_address":"dish.local:99999"}}`, false},
		{"disabled draft", `{"starlink_settings":{"enabled":false,"dish_address":""}}`, true},
		{"ipv6 dish", `{"starlink_settings":{"enabled":true,"dish_address":"[2001:db8::1]:9200"}}`, true},
		{"normalize and IPv6", `{"name":" New name ","country_code":"de","ip":"2001:db8::1"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &settingsUpdateUsecase{}
			handler := &Handler{nodeUsecase: uc}
			router := gin.New()
			router.PUT("/nodes/:id", handler.UpdateNode)
			req := httptest.NewRequest(http.MethodPut, "/nodes/6", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if tc.valid {
				if res.Code != http.StatusOK || uc.saved == nil {
					t.Fatalf("wanted saved settings, got %d %s", res.Code, res.Body.String())
				}
			} else if res.Code != http.StatusBadRequest || uc.saved != nil {
				t.Fatalf("invalid settings were not rejected: %d %s", res.Code, res.Body.String())
			}
			if tc.name == "explicit router interface override" && (uc.saved.BandwidthSettings.Interface != "" || uc.saved.BandwidthSettings.InterfaceOverride != "wan0") {
				t.Fatalf("router interface choice lost: %+v", uc.saved.BandwidthSettings)
			}
			if tc.name == "normalize and IPv6" && (uc.saved.Name != "New name" || uc.saved.CountryCode != "DE" || uc.saved.IP != "2001:db8::1") {
				t.Fatalf("normalization failed: %+v", uc.saved)
			}
		})
	}
}

func TestTakingANodeOutOfServiceWarnsWhenUsersAreLeft(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, left := range []bool{false, true} {
		uc := &settingsUpdateUsecase{left: left}
		router := gin.New()
		router.PUT("/nodes/:id", (&Handler{nodeUsecase: uc}).UpdateNode)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/nodes/6", strings.NewReader(`{"is_active":false}`)))
		if rec.Code != http.StatusOK || uc.saved == nil || uc.saved.IsActive {
			t.Fatalf("left=%v: code %d saved %+v", left, rec.Code, uc.saved)
		}
		if got := strings.Contains(rec.Body.String(), `"warning"`); got != left {
			t.Fatalf("left=%v: warning in body = %v: %s", left, got, rec.Body.String())
		}
	}
}

