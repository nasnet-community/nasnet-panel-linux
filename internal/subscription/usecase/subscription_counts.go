package usecase

import (
	"context"
	"fmt"
	"time"
)

// CountExpiringSubscriptions supplies the admin badge without loading rows or associations.
func (u *subscriptionUsecase) CountExpiringSubscriptions(ctx context.Context, days int) (int64, error) {
	if days < 1 || days > 365 {
		return 0, fmt.Errorf("days must be between 1 and 365")
	}
	now := time.Now().UTC()
	return u.subRepo.CountExpiring(ctx, now, now.Add(time.Duration(days)*24*time.Hour))
}
