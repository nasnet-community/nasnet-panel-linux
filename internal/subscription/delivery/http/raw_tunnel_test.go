package http

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
)

// The real usecase behind the real handler, with only the edges faked.

type rawSubRepo struct {
	repository.SubscriptionRepository
	sub *domain.Subscription
}

func (r *rawSubRepo) FindByLinkKey(_ context.Context, key string) (*domain.Subscription, error) {
	if r.sub != nil && r.sub.LinkKey == key {
		return r.sub, nil
	}
	return nil, fmt.Errorf("subscription not found")
}

type rawAccounts struct {
	contract.AccountManager
	accounts []*accountDomain.Account
}

func (a *rawAccounts) ListAccountsBySubscription(context.Context, uint) ([]*accountDomain.Account, error) {
	return a.accounts, nil
}

type rawTunnels struct {
	usecase.CoreAssigner
	arts []*coreUC.ClientArtifact
}

func (c *rawTunnels) GenerateSubscriptionArtifacts(context.Context, uint) ([]*coreUC.ClientArtifact, error) {
	return c.arts, nil
}

type rawProvider struct{ product.Provider }

func (rawProvider) GetType() product.ProductType { return product.ProductTypeXray }
func (rawProvider) GenerateClientConfig(_ context.Context, sub *product.SubscriptionInfo) (string, error) {
	var links []string
	for _, in := range sub.Inbounds {
		links = append(links, "vless://"+in.Tag)
	}
	return strings.Join(links, "\n"), nil
}

var rawTunnelArts = []*coreUC.ClientArtifact{
	{Protocol: "mtproto", Kind: "link", Content: "tg://proxy?server=a&port=443&secret=s1"},
	{Protocol: "ssh", Kind: "link", Content: "ssh://u:p@b:22"},
}

func rawAccount() *accountDomain.Account {
	node := &nodeDomain.Node{ID: 1, Name: "n1", IP: "203.0.113.1", IsActive: true}
	return &accountDomain.Account{InboundID: 10, UUID: "u", Email: "e", Inbound: &nodeDomain.Inbound{ID: 10, NodeID: 1, Node: node, Tag: "in-a", Protocol: "vless", Port: 443}}
}

func getRaw(t *testing.T, accounts []*accountDomain.Account, arts []*coreUC.ClientArtifact) *httptest.ResponseRecorder {
	t.Helper()
	end := time.Unix(1900000000, 0)
	sub := &domain.Subscription{ID: 3, LinkKey: "key-1", Label: "mtproto", ProductType: "xray", Status: domain.SubscriptionStatusActive, DataUsed: 5, DataLimit: 1 << 30, EndDate: &end}
	factory := product.NewProviderFactory()
	factory.Register(rawProvider{})
	uc := usecase.NewSubscriptionUsecase(&rawSubRepo{sub: sub}, nil, nil, factory, nil, nil, nil, &rawAccounts{accounts: accounts}, nil, nil, nil)
	uc.SetCoreAssigner(&rawTunnels{arts: arts})

	router := gin.New()
	router.GET("/sub/:uuid", (&Handler{subUsecase: uc}).GetConfigRaw)
	req := httptest.NewRequest("GET", "/sub/key-1", nil)
	req.Header.Set("User-Agent", "v2rayNG/1.8.5")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(rec.Body.String())
	if err != nil {
		t.Fatalf("body %q is not base64: %v", rec.Body.String(), err)
	}
	return string(raw)
}

func TestGetConfigRaw_TunnelOnly(t *testing.T) {
	rec := getRaw(t, nil, rawTunnelArts)
	if rec.Code != 200 {
		t.Fatalf("code = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := decodeBody(t, rec); got != "tg://proxy?server=a&port=443&secret=s1\nssh://u:p@b:22" {
		t.Errorf("links = %q", got)
	}
	if got := rec.Header().Get("subscription-userinfo"); got != fmt.Sprintf("upload=0; download=5; total=%d; expire=1900000000", 1<<30) {
		t.Errorf("subscription-userinfo = %q", got)
	}
	if got := rec.Header().Get("profile-title"); got != "base64:"+base64.StdEncoding.EncodeToString([]byte("mtproto")) {
		t.Errorf("profile-title = %q", got)
	}
}

// Xray-only and mixed subs answer exactly the Xray links, no tunnel schemes.
func TestGetConfigRaw_XrayBodyUnchanged(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("vless://in-a"))
	for name, arts := range map[string][]*coreUC.ClientArtifact{"xray only": nil, "mixed": rawTunnelArts} {
		rec := getRaw(t, []*accountDomain.Account{rawAccount()}, arts)
		if rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s: code=%d body=%q, want %q", name, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestGetConfigRaw_NeitherIs404(t *testing.T) {
	rec := getRaw(t, nil, nil)
	if rec.Code != 404 || rec.Body.String() != "Subscription not found or expired" {
		t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
	}
}
