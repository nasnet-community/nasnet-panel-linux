package usecase

import (
	"context"
	"fmt"
	"time"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	subDomain "github.com/nasnet-community/nasnet-panel-linux/internal/subscription/domain"
	userDomain "github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
)

func (u *adminUsecase) ListUsers(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*adminDomain.UserListItem, int64, error) {
	return u.userRepo.ListAllEnriched(ctx, search, filter, sort, order, offset, limit)
}

func (u *adminUsecase) GetUserDetails(ctx context.Context, id uint) (*adminDomain.UserDetails, error) {
	user, err := u.userRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	subs, _ := u.subRepo.ListByUserID(ctx, id, 0, 1000)
	activeSubs := 0
	var totalDataUsed int64
	var totalDataUpload int64
	var totalDataDownload int64
	for _, sub := range subs {
		if sub.Status == subDomain.SubscriptionStatusActive {
			activeSubs++
		}
		totalDataUsed += sub.LifetimeDataUsed
		totalDataUpload += sub.LifetimeDataUpload
		totalDataDownload += sub.LifetimeDataDownload
	}

	// Compute LastActiveAt from accounts
	var lastActiveAt *string
	if u.accountRepo != nil {
		accounts, _ := u.accountRepo.ListByUserID(ctx, id)
		var latest time.Time
		for _, acc := range accounts {
			if acc.LastActivityAt != nil && acc.LastActivityAt.After(latest) {
				latest = *acc.LastActivityAt
			}
		}
		if !latest.IsZero() {
			s := latest.Format(time.RFC3339)
			lastActiveAt = &s
		}
	}

	return &adminDomain.UserDetails{
		ID:                  user.ID,
		TelegramID:          user.TelegramID,
		Username:            user.Username,
		FirstName:           user.FirstName,
		LastName:            user.LastName,
		IsAdmin:             user.IsAdmin,
		IsBanned:            user.IsBanned,
		Language:            user.Language,
		AdminNotes:          user.AdminNotes,
		TotalSubscriptions:  len(subs),
		ActiveSubscriptions: activeSubs,
		TotalDataUsed:       totalDataUsed,
		TotalDataUpload:     totalDataUpload,
		TotalDataDownload:   totalDataDownload,
		LastActiveAt:        lastActiveAt,
		CreatedAt:           user.CreatedAt.Format(time.RFC3339),
		UpdatedAt:           user.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (u *adminUsecase) BanUser(ctx context.Context, id uint) error {
	return u.userRepo.UpdateBanStatus(ctx, id, true)
}
func (u *adminUsecase) UnbanUser(ctx context.Context, id uint) error {
	return u.userRepo.UpdateBanStatus(ctx, id, false)
}
func (u *adminUsecase) SetAdmin(ctx context.Context, id uint, isAdmin bool) error {
	return u.userRepo.UpdateAdminStatus(ctx, id, isAdmin)
}

// UpdateUserTelegramID updates the telegram_id for a user
func (u *adminUsecase) UpdateUserTelegramID(ctx context.Context, userID uint, telegramID int64) error {
	user, err := u.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("user not found: %w", err)
	}
	user.TelegramID = telegramID
	return u.userRepo.Update(ctx, user)
}

// CreateUser creates a new user account
func (u *adminUsecase) CreateUser(ctx context.Context, username, firstName, lastName string, telegramID int64) (*userDomain.User, error) {
	if telegramID == 0 {
		// Use negative timestamp as placeholder for unique constraint
		telegramID = -time.Now().UnixNano()
	}
	user := &userDomain.User{
		TelegramID: telegramID,
		Username:   username,
		FirstName:  firstName,
		LastName:   lastName,
	}
	if err := u.userRepo.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return user, nil
}

// UpdateUserNotes updates admin notes for a user
func (u *adminUsecase) UpdateUserNotes(ctx context.Context, userID uint, notes string) error {
	return u.userRepo.UpdateAdminNotes(ctx, userID, notes)
}

// GetUserUsageHistory returns daily usage data points for a user
func (u *adminUsecase) GetUserUsageHistory(ctx context.Context, userID uint, days int) ([]adminDomain.UserDailyUsagePoint, error) {
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	if u.usageRepo == nil {
		return []adminDomain.UserDailyUsagePoint{}, nil
	}

	to := time.Now().Truncate(24 * time.Hour)
	from := to.AddDate(0, 0, -days)

	records, err := u.usageRepo.ListByUserID(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}

	// Convert cumulative snapshots to daily deltas
	points := make([]adminDomain.UserDailyUsagePoint, 0, len(records))
	var prevDataUsed int64
	for i, r := range records {
		delta := r.DataUsed - prevDataUsed
		if i == 0 || delta < 0 {
			delta = 0
		}
		points = append(points, adminDomain.UserDailyUsagePoint{
			Date:     r.Date.Format("2006-01-02"),
			DataUsed: delta,
		})
		prevDataUsed = r.DataUsed
	}

	return points, nil
}

// GetUserAccounts returns all accounts belonging to a user, grouped by node
func (u *adminUsecase) GetUserAccounts(ctx context.Context, userID uint) ([]adminDomain.UserAccountInfo, error) {
	if u.accountRepo == nil {
		return []adminDomain.UserAccountInfo{}, nil
	}

	accounts, err := u.accountRepo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	result := make([]adminDomain.UserAccountInfo, 0, len(accounts))
	for _, acc := range accounts {
		info := adminDomain.UserAccountInfo{
			AccountID:      acc.ID,
			Email:          acc.Email,
			Status:         string(acc.Status),
			SubscriptionID: acc.SubscriptionID,
			DataUsed:       acc.DataUsed,
		}
		if acc.LastActivityAt != nil {
			info.LastActivityAt = acc.LastActivityAt.Format(time.RFC3339)
		}
		if acc.Inbound != nil {
			info.InboundTag = acc.Inbound.Tag
			info.Protocol = acc.Inbound.Protocol
			if acc.Inbound.Node != nil {
				info.NodeID = acc.Inbound.Node.ID
				info.NodeName = acc.Inbound.Node.Name
				info.NodeCountry = acc.Inbound.Node.CountryCode
			}
		}
		result = append(result, info)
	}

	return result, nil
}
