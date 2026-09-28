package repository

import (
	"context"
	"fmt"

	adminDomain "github.com/nasnet-community/nasnet-panel-linux/internal/admin/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/database"
	"gorm.io/gorm"
)

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	FindByID(ctx context.Context, id uint) (*domain.User, error)
	FindByTelegramID(ctx context.Context, telegramID int64) (*domain.User, error)
	FindByUsername(ctx context.Context, username string) (*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id uint) error
	List(ctx context.Context, offset, limit int) ([]*domain.User, error)
	UpdateLanguage(ctx context.Context, id uint, language string) error
	// Admin methods
	ListAll(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*domain.User, int64, error)
	ListForAdmin(ctx context.Context, q adminDomain.UserListQuery) (*adminDomain.UserListPage, error)
	UpdateBanStatus(ctx context.Context, id uint, banned bool) error
	UpdateAdminStatus(ctx context.Context, id uint, isAdmin bool) error
	CountAll(ctx context.Context) (int64, error)
	CountActive(ctx context.Context) (int64, error)
	CountBanned(ctx context.Context) (int64, error)
	CountAdmins(ctx context.Context) (int64, error)
	ListActiveSubscribers(ctx context.Context) ([]*domain.User, error)

	// Admin notes
	UpdateAdminNotes(ctx context.Context, id uint, notes string) error

	// Admin listing
	ListAdmins(ctx context.Context) ([]*domain.User, error)
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) FindByID(ctx context.Context, id uint) (*domain.User, error) {
	var user domain.User
	if err := database.GetExecutor(r.db, ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByTelegramID(ctx context.Context, telegramID int64) (*domain.User, error) {
	var user domain.User
	if err := r.db.WithContext(ctx).Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	if err := r.db.WithContext(ctx).Where(database.ILike("username", "?"), username).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&domain.User{}, id).Error
}

func (r *userRepository) List(ctx context.Context, offset, limit int) ([]*domain.User, error) {
	var users []*domain.User
	if err := r.db.WithContext(ctx).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) UpdateLanguage(ctx context.Context, id uint, language string) error {
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).
		Update("language", language).Error
}

func (r *userRepository) ListAll(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*domain.User, int64, error) {
	var users []*domain.User
	var total int64

	query := r.db.WithContext(ctx).Model(&domain.User{})

	if search != "" {
		searchPattern := "%" + search + "%"
		clause := fmt.Sprintf("%s OR %s OR %s OR CAST(telegram_id AS TEXT) LIKE ?",
			database.ILike("username", "?"),
			database.ILike("first_name", "?"),
			database.ILike("last_name", "?"),
		)
		query = query.Where(clause, searchPattern, searchPattern, searchPattern, searchPattern)
	}

	// Filter
	switch filter {
	case "active":
		query = query.Where("is_banned = ? AND is_admin = ?", false, false)
	case "banned":
		query = query.Where("is_banned = ?", true)
	case "admin":
		query = query.Where("is_admin = ?", true)
	}

	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// Sorting
	sortColumn := "id"
	switch sort {
	case "username":
		sortColumn = "username"
	case "created_at":
		sortColumn = "created_at"
	}

	sortOrder := "DESC"
	if order == "asc" {
		sortOrder = "ASC"
	}

	if err := query.Session(&gorm.Session{}).Order(fmt.Sprintf("%s %s", sortColumn, sortOrder)).Offset(offset).Limit(limit).Find(&users).Error; err != nil {
		return nil, 0, err
	}

	return users, total, nil
}

// UpdateBanStatus updates user's banned status
func (r *userRepository) UpdateBanStatus(ctx context.Context, id uint, banned bool) error {
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).
		Update("is_banned", banned).Error
}

// UpdateAdminStatus updates user's admin status
func (r *userRepository) UpdateAdminStatus(ctx context.Context, id uint, isAdmin bool) error {
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).
		Update("is_admin", isAdmin).Error
}

// CountAll counts all users
func (r *userRepository) CountAll(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.User{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountActive counts users with at least one active subscription
func (r *userRepository) CountActive(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.User{}).
		Where("is_banned = ?", false).
		Where("id IN (SELECT DISTINCT user_id FROM subscriptions WHERE deleted_at IS NULL AND status = 'active' AND user_id IS NOT NULL)").
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountBanned counts banned users
func (r *userRepository) CountBanned(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.User{}).
		Where("is_banned = ?", true).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountAdmins counts admin users
func (r *userRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.User{}).
		Where("is_admin = ?", true).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *userRepository) ListActiveSubscribers(ctx context.Context) ([]*domain.User, error) {
	var users []*domain.User
	if err := r.db.WithContext(ctx).
		Joins("JOIN subscriptions ON subscriptions.user_id = users.id").
		Where("subscriptions.status = ?", "active").
		Where("users.is_banned = ?", false).
		Distinct().
		Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// UpdateAdminNotes updates admin notes for a user
func (r *userRepository) UpdateAdminNotes(ctx context.Context, id uint, notes string) error {
	return r.db.WithContext(ctx).Model(&domain.User{}).Where("id = ?", id).
		Update("admin_notes", notes).Error
}

// ListAdmins returns all users with is_admin=true
func (r *userRepository) ListAdmins(ctx context.Context) ([]*domain.User, error) {
	var users []*domain.User
	err := database.GetExecutor(r.db, ctx).Where("is_admin = ?", true).Find(&users).Error
	return users, err
}
