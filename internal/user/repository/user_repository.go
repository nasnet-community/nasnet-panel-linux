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
	ListAllEnriched(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*adminDomain.UserListItem, int64, error)
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

func (r *userRepository) ListAllEnriched(ctx context.Context, search, filter, sort, order string, offset, limit int) ([]*adminDomain.UserListItem, int64, error) {
	var total int64

	// Helper: apply search and filter conditions to a query on the users table.
	applyUserFilters := func(q *gorm.DB, tableAlias string) *gorm.DB {
		col := func(name string) string {
			if tableAlias != "" {
				return tableAlias + "." + name
			}
			return name
		}
		if search != "" {
			searchPattern := "%" + search + "%"
			clause := fmt.Sprintf("%s OR %s OR %s OR CAST(%s AS TEXT) LIKE ?",
				database.ILike(col("username"), "?"),
				database.ILike(col("first_name"), "?"),
				database.ILike(col("last_name"), "?"),
				col("telegram_id"),
			)
			q = q.Where(clause, searchPattern, searchPattern, searchPattern, searchPattern)
		}
		switch filter {
		case "active":
			q = q.Where(col("is_banned")+" = ? AND "+col("is_admin")+" = ?", false, false)
		case "banned":
			q = q.Where(col("is_banned")+" = ?", true)
		case "admin":
			q = q.Where(col("is_admin")+" = ?", true)
		case "has_subscription":
			q = q.Where(col("id") + " IN (SELECT user_id FROM subscriptions WHERE deleted_at IS NULL AND status = 'active')")
		case "no_subscription":
			q = q.Where(col("id") + " NOT IN (SELECT user_id FROM subscriptions WHERE deleted_at IS NULL AND status = 'active' AND user_id IS NOT NULL)")
		}
		return q
	}

	// --- Step 1: Get total count (users table only, no expensive JOINs) ---
	countQuery := r.db.WithContext(ctx).Model(&domain.User{})
	countQuery = applyUserFilters(countQuery, "")
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// --- Step 2: Get paginated user IDs ---
	// Determine sort configuration.
	sortOrder := "DESC"
	if order == "asc" {
		sortOrder = "ASC"
	}

	// Sorting by computed columns requires JOINs in the ID query.
	needsJoinForSort := false
	sortColumn := "u.id"
	switch sort {
	case "username":
		sortColumn = "u.username"
	case "created_at":
		sortColumn = "u.created_at"
	case "active_subscriptions":
		sortColumn = "active_subscriptions"
		needsJoinForSort = true
	case "total_subscriptions":
		sortColumn = "total_subscriptions"
		needsJoinForSort = true
	case "last_active_at":
		sortColumn = "a.last_active_at"
		needsJoinForSort = true
	}

	subQueryCountFilter := database.CountFilter("status = 'active'")
	subsJoin := fmt.Sprintf(`LEFT JOIN (
			SELECT user_id,
				COUNT(*) as total_count,
				%s as active_count
			FROM subscriptions
			WHERE deleted_at IS NULL AND user_id IS NOT NULL
			GROUP BY user_id
		) s ON s.user_id = u.id`, subQueryCountFilter)

	activityJoin := `LEFT JOIN (
			SELECT sub.user_id, MAX(acc.last_activity_at) as last_active_at
			FROM accounts acc
			JOIN subscriptions sub ON sub.id = acc.subscription_id AND sub.deleted_at IS NULL
			WHERE acc.deleted_at IS NULL AND sub.user_id IS NOT NULL
			GROUP BY sub.user_id
		) a ON a.user_id = u.id`

	orderExpr := database.NullsLast(fmt.Sprintf("%s %s", sortColumn, sortOrder))

	var userIDs []uint
	if needsJoinForSort {
		// When sorting by a computed column we need JOINs, but only to
		// determine ordering — the heavy GROUP BY still runs against the
		// full subscriptions/accounts tables, however we only pluck IDs.
		idQuery := r.db.WithContext(ctx).
			Table("users u").
			Select("u.id").
			Joins(subsJoin).
			Joins(activityJoin).
			Where("u.deleted_at IS NULL")
		idQuery = applyUserFilters(idQuery, "u")
		if err := idQuery.Order(orderExpr).Offset(offset).Limit(limit).Pluck("u.id", &userIDs).Error; err != nil {
			return nil, 0, err
		}
	} else {
		// Fast path: no JOINs needed — sort/filter only on the users table.
		idQuery := r.db.WithContext(ctx).
			Table("users u").
			Where("u.deleted_at IS NULL")
		idQuery = applyUserFilters(idQuery, "u")
		if err := idQuery.Order(orderExpr).Offset(offset).Limit(limit).Pluck("u.id", &userIDs).Error; err != nil {
			return nil, 0, err
		}
	}

	if len(userIDs) == 0 {
		return []*adminDomain.UserListItem{}, total, nil
	}

	// --- Step 3: Fetch enriched data only for the page of user IDs ---
	selectFields := `u.id, u.telegram_id, u.username, u.first_name, u.last_name,
		u.is_admin, u.is_banned, u.created_at, u.updated_at,
		COALESCE(s.active_count, 0) as active_subscriptions,
		COALESCE(s.total_count, 0) as total_subscriptions,
		a.last_active_at`

	type enrichedRow struct {
		ID                  uint    `gorm:"column:id"`
		TelegramID          int64   `gorm:"column:telegram_id"`
		Username            string  `gorm:"column:username"`
		FirstName           string  `gorm:"column:first_name"`
		LastName            string  `gorm:"column:last_name"`
		IsAdmin             bool    `gorm:"column:is_admin"`
		IsBanned            bool    `gorm:"column:is_banned"`
		ActiveSubscriptions int     `gorm:"column:active_subscriptions"`
		TotalSubscriptions  int     `gorm:"column:total_subscriptions"`
		LastActiveAt        *string `gorm:"column:last_active_at"`
		CreatedAt           string  `gorm:"column:created_at"`
		UpdatedAt           string  `gorm:"column:updated_at"`
	}

	var rows []enrichedRow
	mainQuery := r.db.WithContext(ctx).
		Table("users u").
		Select(selectFields).
		Joins(subsJoin).
		Joins(activityJoin).
		Where("u.id IN ? AND u.deleted_at IS NULL", userIDs).
		Order(orderExpr)

	if err := mainQuery.Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	items := make([]*adminDomain.UserListItem, len(rows))
	for i, row := range rows {
		items[i] = &adminDomain.UserListItem{
			ID:                  row.ID,
			TelegramID:          row.TelegramID,
			Username:            row.Username,
			FirstName:           row.FirstName,
			LastName:            row.LastName,
			IsAdmin:             row.IsAdmin,
			IsBanned:            row.IsBanned,
			ActiveSubscriptions: row.ActiveSubscriptions,
			TotalSubscriptions:  row.TotalSubscriptions,
			LastActiveAt:        row.LastActiveAt,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		}
	}

	return items, total, nil
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
