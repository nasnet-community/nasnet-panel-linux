package usecase

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/subscription/repository"
)

func (u *subscriptionUsecase) GetByID(ctx context.Context, id uint) (*domain.Subscription, error) {
	return u.subRepo.FindByID(ctx, id)
}

func (u *subscriptionUsecase) GetByConfigID(ctx context.Context, configID string) (*domain.Subscription, error) {
	return u.subRepo.FindByConfigID(ctx, configID)
}

func (u *subscriptionUsecase) ListByUserID(ctx context.Context, userID uint, offset, limit int) ([]*domain.Subscription, error) {
	return u.subRepo.ListByUserID(ctx, userID, offset, limit)
}

func (u *subscriptionUsecase) GetActiveByUserID(ctx context.Context, userID uint) ([]*domain.Subscription, error) {
	return u.subRepo.FindActiveByUserID(ctx, userID)
}

func (u *subscriptionUsecase) ListAllSubscriptions(ctx context.Context, status string, offset, limit int) ([]*domain.Subscription, error) {
	return u.subRepo.ListAll(ctx, status, offset, limit)
}

func (u *subscriptionUsecase) ListAllFilteredSubscriptions(ctx context.Context, filter repository.SubscriptionFilter) ([]*domain.Subscription, int64, error) {
	return u.subRepo.ListAllFiltered(ctx, filter)
}

func (u *subscriptionUsecase) GetSubscriptionLink(ctx context.Context, id uint) (string, error) {
	sub, err := u.subRepo.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	return sub.SubLink, nil
}

// GetByConfigEmail finds a subscription by config email (for migration duplicate check)
func (u *subscriptionUsecase) GetByConfigEmail(ctx context.Context, email string) (*domain.Subscription, error) {
	return u.subRepo.FindByConfigEmail(ctx, email)
}
