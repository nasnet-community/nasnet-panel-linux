package repository

import (
	"context"

	"github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type settingRepository struct {
	db *gorm.DB
}

func NewSettingRepository(db *gorm.DB) domain.SettingRepository {
	return &settingRepository{db: db}
}

func (r *settingRepository) GetAll(ctx context.Context) ([]*domain.Setting, error) {
	var settings []*domain.Setting
	err := r.db.WithContext(ctx).Find(&settings).Error
	return settings, err
}

func (r *settingRepository) GetByKey(ctx context.Context, key string) (*domain.Setting, error) {
	var setting domain.Setting
	err := r.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if err != nil {
		return nil, err
	}
	return &setting, nil
}

func (r *settingRepository) Update(ctx context.Context, setting *domain.Setting) error {
	return r.db.WithContext(ctx).Save(setting).Error
}

func (r *settingRepository) UpdateMany(ctx context.Context, settings []*domain.Setting) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, s := range settings {
			// partial update allows updating only value if needed, but here we likely want full update
			// using Save ensures if it doesn't exist it's created, but checking key existence is good.
			// Using OnConflict to update value
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value"}),
			}).Create(s).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
