package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	subdomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	tg "github.com/nasnet-community/nasnet-panel-linux/pkg/telegram"
)

type fakeSubLinker struct {
	subs  map[uint]*subdomain.Subscription
	bound map[uint]int64
}

func newFakeSubLinker(subs ...*subdomain.Subscription) *fakeSubLinker {
	f := &fakeSubLinker{subs: map[uint]*subdomain.Subscription{}, bound: map[uint]int64{}}
	for _, s := range subs {
		f.subs[s.ID] = s
	}
	return f
}

func (f *fakeSubLinker) GetByID(_ context.Context, id uint) (*subdomain.Subscription, error) {
	if s, ok := f.subs[id]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeSubLinker) UpdateTelegramChatIDByConfigID(_ context.Context, configID string, chatID int64) error {
	for _, s := range f.subs {
		if s.LinkKey == configID || (s.LinkKey == "" && s.ConfigID == configID) {
			f.bound[s.ID] = chatID
			return nil
		}
	}
	return errors.New("subscription not found")
}

func TestHandleLinkPayload_BindsSubWithRotatableLinkKey(t *testing.T) {
	const secret = "test-secret"
	subID := uint(90)
	var chatID int64 = 123456789

	sub := &subdomain.Subscription{ID: subID, ConfigID: "v2ray-config-uuid", LinkKey: "public-link-key"}
	fake := newFakeSubLinker(sub)
	h := &Handler{subLinker: fake, linkSecret: secret}

	token := tg.SignLinkToken(uint64(subID), secret, 15*time.Minute)

	msg := h.handleLinkPayload(context.Background(), token, chatID, "en")

	if want := i18n.Get("en", "TgLinkSuccess"); msg != want {
		t.Fatalf("handleLinkPayload = %q, want success %q", msg, want)
	}
	if fake.bound[subID] != chatID {
		t.Fatalf("chat id not bound: got %d, want %d", fake.bound[subID], chatID)
	}
}
