package telegram

import (
	"context"
	"fmt"
	"strings"
	"testing"

	accountDomain "github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	coreUC "github.com/nasnet-community/nasnet-panel-linux/internal/core/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/usecase"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	userUC "github.com/nasnet-community/nasnet-panel-linux/internal/user/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/product"
	"gopkg.in/telebot.v3"
)

// The real usecase with only the edges faked: a tunnel-only sub owned by user 7.

type botSubRepo struct {
	repository.SubscriptionRepository
	sub *domain.Subscription
}

func (r *botSubRepo) FindByID(_ context.Context, id uint) (*domain.Subscription, error) {
	if r.sub.ID == id {
		return r.sub, nil
	}
	return nil, fmt.Errorf("subscription not found")
}

func (r *botSubRepo) FindByLinkKey(_ context.Context, key string) (*domain.Subscription, error) {
	if r.sub.LinkKey == key {
		return r.sub, nil
	}
	return nil, fmt.Errorf("subscription not found")
}

type botNoAccounts struct{ contract.AccountManager }

func (botNoAccounts) ListAccountsBySubscription(context.Context, uint) ([]*accountDomain.Account, error) {
	return nil, nil
}

type botTunnels struct {
	usecase.CoreAssigner
	arts []*coreUC.ClientArtifact
}

func (c *botTunnels) GenerateSubscriptionArtifacts(context.Context, uint) ([]*coreUC.ClientArtifact, error) {
	return c.arts, nil
}

type botUsers struct{ userUC.UserUsecase }

func (botUsers) GetByTelegramID(context.Context, int64) (*userDomain.User, error) {
	return &userDomain.User{ID: 7, Language: "en"}, nil
}
func (botUsers) GetOrCreate(context.Context, int64, string, string, string) (*userDomain.User, error) {
	return &userDomain.User{ID: 7, Language: "en"}, nil
}

type botChat struct {
	telebot.Context
	sent []any
}

func (c *botChat) Sender() *telebot.User         { return &telebot.User{ID: 42} }
func (c *botChat) Callback() *telebot.Callback   { return nil }
func (c *botChat) Send(what any, _ ...any) error { c.sent = append(c.sent, what); return nil }

func tunnelOnlyBot(arts ...*coreUC.ClientArtifact) *Handler {
	owner := uint(7)
	sub := &domain.Subscription{ID: 3, UserID: &owner, LinkKey: "key-1", ProductType: "xray", Status: domain.SubscriptionStatusActive}
	uc := usecase.NewSubscriptionUsecase(&botSubRepo{sub: sub}, nil, nil, product.NewProviderFactory(), nil, nil, nil, botNoAccounts{}, nil, nil, nil)
	uc.SetCoreAssigner(&botTunnels{arts: arts})
	return &Handler{subUsecase: uc, userUsecase: botUsers{}}
}

const mtprotoLink = "tg://proxy?server=a&port=443&secret=s1"

func TestBotConfigAll_TunnelOnlyShowsTunnelLinks(t *testing.T) {
	h := tunnelOnlyBot(
		&coreUC.ClientArtifact{Protocol: "mtproto", Kind: "link", Content: mtprotoLink},
		&coreUC.ClientArtifact{Protocol: "ssh", Kind: "link", Content: "ssh://u:p@b:22"},
	)
	chat := &botChat{}
	if err := h.HandleGetConfigAll(chat, 3); err != nil {
		t.Fatalf("HandleGetConfigAll: %v", err)
	}
	if len(chat.sent) != 1 {
		t.Fatalf("sent = %v", chat.sent)
	}
	msg, _ := chat.sent[0].(string)
	if strings.Contains(msg, i18n.Get("en", "ErrGenerateConfig")) || !strings.Contains(msg, "`"+mtprotoLink+"`") || !strings.Contains(msg, "`ssh://u:p@b:22`") {
		t.Errorf("message = %q", msg)
	}
}

func TestBotQR_TunnelOnlySendsQR(t *testing.T) {
	h := tunnelOnlyBot(&coreUC.ClientArtifact{Protocol: "mtproto", Kind: "link", Content: mtprotoLink})
	chat := &botChat{}
	if err := h.HandleGetQR(chat, 3); err != nil {
		t.Fatalf("HandleGetQR: %v", err)
	}
	if len(chat.sent) != 1 {
		t.Fatalf("sent = %v", chat.sent)
	}
	photo, ok := chat.sent[0].(*telebot.Photo)
	if !ok || !strings.Contains(photo.Caption, mtprotoLink) {
		t.Errorf("sent %#v, want a QR photo for the tunnel link", chat.sent[0])
	}
}
