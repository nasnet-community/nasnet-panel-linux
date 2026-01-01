package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"golang.org/x/crypto/bcrypt"
)

// ─── spies: capture writes the shared mock drops on the floor ──────────────

type setPanelCall struct {
	ID   uint
	Hash string
	Mode string
}

type updateUserIDCall struct {
	SubID  uint
	UserID uint
}

type adminSpySubRepo struct {
	*mockSubscriptionRepo
	setPanelCalls     []setPanelCall
	updateUserIDCalls []updateUserIDCall
}

func (s *adminSpySubRepo) SetPanelPassword(_ context.Context, id uint, hash, mode string) error {
	s.setPanelCalls = append(s.setPanelCalls, setPanelCall{ID: id, Hash: hash, Mode: mode})
	return nil
}

func (s *adminSpySubRepo) UpdateUserID(_ context.Context, subID, userID uint) error {
	s.updateUserIDCalls = append(s.updateUserIDCalls, updateUserIDCall{SubID: subID, UserID: userID})
	return nil
}

func newAdminSubRepoSpy() *adminSpySubRepo {
	return &adminSpySubRepo{mockSubscriptionRepo: newMockSubscriptionRepo()}
}

func newAdminTestUsecase(subRepo *adminSpySubRepo) SubscriptionUsecase {
	return NewSubscriptionUsecase(
		subRepo,
		newMockSubUserRepo(),
		&mockNodeRepo{},
		nil, nil, nil, nil,
		newMockAccountMgr(),
		&mockAccountReader{},
		&mockSubTxManager{},
		events.NewEventBus(),
	)
}

// ─── CreateDirect / AssignToUser ───────────────────────────────────────────

// CreateDirect is a thin pass-through for migrations; verify the sub lands
// in the repo with an assigned ID.
func TestCreateDirect_StoresViaRepo(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	uc := newAdminTestUsecase(subRepo)

	sub := &domain.Subscription{ConfigEmail: "imported@x"}
	if err := uc.CreateDirect(context.Background(), sub); err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if sub.ID == 0 {
		t.Fatal("expected ID to be assigned by repo")
	}
	if _, ok := subRepo.subs[sub.ID]; !ok {
		t.Errorf("subscription not stored under id %d", sub.ID)
	}
}

func TestAssignToUser_PropagatesToRepo(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	uc := newAdminTestUsecase(subRepo)

	if err := uc.AssignToUser(context.Background(), 11, 22); err != nil {
		t.Fatalf("AssignToUser: %v", err)
	}
	if len(subRepo.updateUserIDCalls) != 1 || subRepo.updateUserIDCalls[0] != (updateUserIDCall{SubID: 11, UserID: 22}) {
		t.Errorf("UpdateUserID calls = %+v", subRepo.updateUserIDCalls)
	}
}

// ─── GetPanelPasswordHash ──────────────────────────────────────────────────

func TestGetPanelPasswordHash_Found(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, PanelPasswordHash: "$2a$abc", PanelPasswordMode: "custom"})
	uc := newAdminTestUsecase(subRepo)

	hash, mode, err := uc.GetPanelPasswordHash(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPanelPasswordHash: %v", err)
	}
	if hash != "$2a$abc" || mode != "custom" {
		t.Errorf("hash=%q mode=%q", hash, mode)
	}
}

// Sentinel error for missing subs — callers depend on this for routing.
func TestGetPanelPasswordHash_NotFound(t *testing.T) {
	uc := newAdminTestUsecase(newAdminSubRepoSpy())
	if _, _, err := uc.GetPanelPasswordHash(context.Background(), 99); !errors.Is(err, ErrSubscriptionNotFound) {
		t.Errorf("err = %v, want ErrSubscriptionNotFound", err)
	}
}

// ─── SetPanelPassword ──────────────────────────────────────────────────────

func TestSetPanelPassword_DefaultAndDisabled(t *testing.T) {
	for _, mode := range []string{"default", "disabled"} {
		subRepo := newAdminSubRepoSpy()
		uc := newAdminTestUsecase(subRepo)
		if err := uc.SetPanelPassword(context.Background(), 1, mode, ""); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if len(subRepo.setPanelCalls) != 1 {
			t.Fatalf("%s: setPanel called %d times", mode, len(subRepo.setPanelCalls))
		}
		got := subRepo.setPanelCalls[0]
		if got.Mode != mode || got.Hash != "" {
			t.Errorf("%s: got %+v", mode, got)
		}
	}
}

// Custom mode without a password is rejected before any repo write.
func TestSetPanelPassword_CustomRequiresPassword(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	uc := newAdminTestUsecase(subRepo)
	err := uc.SetPanelPassword(context.Background(), 1, "custom", "")
	if err == nil {
		t.Fatal("empty password should be rejected for custom mode")
	}
	if len(subRepo.setPanelCalls) != 0 {
		t.Errorf("repo must not be called when validation fails")
	}
}

// The custom password is bcrypt-hashed before being stored.
func TestSetPanelPassword_CustomHashesPassword(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	uc := newAdminTestUsecase(subRepo)
	const password = "hunter2"
	if err := uc.SetPanelPassword(context.Background(), 1, "custom", password); err != nil {
		t.Fatalf("SetPanelPassword: %v", err)
	}
	if len(subRepo.setPanelCalls) != 1 {
		t.Fatalf("setPanel called %d times", len(subRepo.setPanelCalls))
	}
	call := subRepo.setPanelCalls[0]
	if call.Mode != "custom" || call.Hash == "" {
		t.Fatalf("call = %+v", call)
	}
	if call.Hash == password {
		t.Error("password stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(call.Hash), []byte(password)); err != nil {
		t.Errorf("stored hash doesn't verify against password: %v", err)
	}
}

func TestSetPanelPassword_InvalidMode(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	uc := newAdminTestUsecase(subRepo)
	if err := uc.SetPanelPassword(context.Background(), 1, "totally-bogus", ""); err == nil {
		t.Fatal("unknown mode should error")
	}
	if len(subRepo.setPanelCalls) != 0 {
		t.Errorf("repo must not be called for invalid mode")
	}
}

// ─── RegenerateSubscriptionKey ─────────────────────────────────────────────

// Empty customKey → generates a fresh UUID-shaped link key.
func TestRegenerateSubscriptionKey_GeneratesUUID(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	subRepo.seedSubscription(&domain.Subscription{ID: 1, LinkKey: "old-key"})
	uc := newAdminTestUsecase(subRepo)

	got, err := uc.RegenerateSubscriptionKey(context.Background(), 1, "")
	if err != nil {
		t.Fatalf("RegenerateSubscriptionKey: %v", err)
	}
	if got.LinkKey == "" || got.LinkKey == "old-key" {
		t.Errorf("LinkKey not regenerated, got %q", got.LinkKey)
	}
	// UUID v4 string is 36 chars with 4 dashes.
	if len(got.LinkKey) != 36 || strings.Count(got.LinkKey, "-") != 4 {
		t.Errorf("LinkKey %q not UUID-shaped", got.LinkKey)
	}
}

// Non-empty customKey is used verbatim.
func TestRegenerateSubscriptionKey_CustomKey(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	subRepo.seedSubscription(&domain.Subscription{ID: 1})
	uc := newAdminTestUsecase(subRepo)

	got, err := uc.RegenerateSubscriptionKey(context.Background(), 1, "my-key")
	if err != nil {
		t.Fatalf("RegenerateSubscriptionKey: %v", err)
	}
	if got.LinkKey != "my-key" {
		t.Errorf("LinkKey = %q, want my-key", got.LinkKey)
	}
}

func TestRegenerateSubscriptionKey_NotFound(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	subRepo.findByIDErr = errors.New("not found")
	uc := newAdminTestUsecase(subRepo)

	if _, err := uc.RegenerateSubscriptionKey(context.Background(), 1, ""); err == nil {
		t.Fatal("expected error from FindByID failure")
	}
}

func TestRegenerateSubscriptionKey_UpdateError(t *testing.T) {
	subRepo := newAdminSubRepoSpy()
	subRepo.seedSubscription(&domain.Subscription{ID: 1})
	subRepo.updateErr = errors.New("db down")
	uc := newAdminTestUsecase(subRepo)

	_, err := uc.RegenerateSubscriptionKey(context.Background(), 1, "k")
	if err == nil {
		t.Fatal("expected error wrap from Update failure")
	}
	if !strings.Contains(err.Error(), "failed to update subscription key") {
		t.Errorf("err = %v, want wrap containing 'failed to update subscription key'", err)
	}
}
