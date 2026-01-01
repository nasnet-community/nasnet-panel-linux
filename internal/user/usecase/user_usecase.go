package usecase

import (
	"context"
	"errors"
	"fmt"

	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/user/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/user/repository"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound = errors.New("user not found")
	ErrUserExists   = errors.New("user already exists")
)

type UserUsecase interface {
	Register(ctx context.Context, telegramID int64, username, firstName, lastName string) (*domain.User, error)
	GetByID(ctx context.Context, id uint) (*domain.User, error)
	GetByTelegramID(ctx context.Context, telegramID int64) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	GetOrCreate(ctx context.Context, telegramID int64, username, firstName, lastName string) (*domain.User, error)
	UpdateLanguage(ctx context.Context, userID uint, language string) error
	List(ctx context.Context, offset, limit int) ([]*domain.User, error)
	ListAdmins(ctx context.Context) ([]*domain.User, error)
}

type userUsecase struct {
	userRepo repository.UserRepository
	eventBus *events.EventBus
}

func NewUserUsecase(userRepo repository.UserRepository, eventBus *events.EventBus) UserUsecase {
	return &userUsecase{userRepo: userRepo, eventBus: eventBus}
}

func (u *userUsecase) Register(ctx context.Context, telegramID int64, username, firstName, lastName string) (*domain.User, error) {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"telegram_id": telegramID,
		"username":    username,
	}).Info("[Register] Registering new user")

	// Check if user already exists
	existingUser, err := u.userRepo.FindByTelegramID(ctx, telegramID)
	if err == nil && existingUser != nil {
		log.WithField("telegram_id", telegramID).Debug("[Register] User already exists")
		return nil, ErrUserExists
	}

	if username == "" {
		username = fmt.Sprintf("user_%d", telegramID)
	}

	user := &domain.User{
		TelegramID: telegramID,
		Username:   username,
		FirstName:  firstName,
		LastName:   lastName,
	}

	if err := u.userRepo.Create(ctx, user); err != nil {
		log.WithError(err).Error("[Register] Failed to create user")
		return nil, err
	}

	log.WithFields(map[string]interface{}{
		"user_id":     user.ID,
		"telegram_id": telegramID,
		"username":    username,
	}).Info("[Register] User registered successfully")

	// Publish user registered event
	if u.eventBus != nil {
		u.eventBus.Publish(events.Event{
			Type:      events.EventUserRegistered,
			Timestamp: time.Now(),
			Payload: events.UserRegisteredPayload{
				UserID:     user.ID,
				TelegramID: telegramID,
				Username:   username,
			},
		})
	}

	return user, nil
}

func (u *userUsecase) GetByID(ctx context.Context, id uint) (*domain.User, error) {
	user, err := u.userRepo.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

func (u *userUsecase) GetByTelegramID(ctx context.Context, telegramID int64) (*domain.User, error) {
	user, err := u.userRepo.FindByTelegramID(ctx, telegramID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

func (u *userUsecase) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	user, err := u.userRepo.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return user, nil
}

func (u *userUsecase) GetOrCreate(ctx context.Context, telegramID int64, username, firstName, lastName string) (*domain.User, error) {
	if username == "" {
		username = fmt.Sprintf("user_%d", telegramID)
	}

	user, err := u.userRepo.FindByTelegramID(ctx, telegramID)
	if err == nil {
		// Sync profile fields if they changed
		if user.Username != username || user.FirstName != firstName || user.LastName != lastName {
			user.Username = username
			user.FirstName = firstName
			user.LastName = lastName
			if updateErr := u.userRepo.Update(ctx, user); updateErr != nil {
				logger.GetLogger().WithError(updateErr).Warn("[GetOrCreate] Failed to sync user profile fields")
			}
		}
		return user, nil
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return u.Register(ctx, telegramID, username, firstName, lastName)
	}

	return nil, err
}

func (u *userUsecase) UpdateLanguage(ctx context.Context, userID uint, language string) error {
	log := logger.GetLogger()
	log.WithFields(map[string]interface{}{
		"user_id":  userID,
		"language": language,
	}).Info("[UpdateLanguage] Updating user language")
	return u.userRepo.UpdateLanguage(ctx, userID, language)
}

func (u *userUsecase) List(ctx context.Context, offset, limit int) ([]*domain.User, error) {
	return u.userRepo.List(ctx, offset, limit)
}

func (u *userUsecase) ListAdmins(ctx context.Context) ([]*domain.User, error) {
	return u.userRepo.ListAdmins(ctx)
}
