package scheduler

import (
	"context"
	"fmt"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/i18n"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gopkg.in/telebot.v3"
)

// sendExpirationNotifications sends alerts to users with expiring subscriptions
func (s *Scheduler) sendExpirationNotifications(ctx context.Context) {
	if s.notifRepo == nil || s.userRepo == nil || s.bot == nil {
		return
	}
	log := logger.GetLogger()

	activeSubs, err := s.subUsecase.ListAllSubscriptions(ctx, SubStatusActive, 0, 10000)
	if err != nil {
		return
	}

	for _, sub := range activeSubs {
		if sub.EndDate == nil {
			continue
		}

		daysLeft := sub.DaysRemaining()

		// Determine notification type based on days left
		var notifType NotificationType
		var i18nKey string
		var urgency string

		switch {
		case daysLeft <= 0:
			notifType = NotificationTypeExpired
			i18nKey = "NotifExpired"
			urgency = "⚠️ Critical"
		case daysLeft == 1:
			notifType = NotificationTypeExpiry1Day
			i18nKey = "NotifExpiry1Day"
			urgency = "🔴 Urgent"
		case daysLeft <= 3:
			notifType = NotificationTypeExpiry3Days
			i18nKey = "NotifExpiry3Days"
			urgency = "🟠 Soon"
		case daysLeft <= 7:
			notifType = NotificationTypeExpiry7Days
			i18nKey = "NotifExpiry7Days"
			urgency = "🟡 Notice"
		default:
			continue // No notification needed
		}

		// Check if already sent (deduplication)
		alreadySent, err := s.notifRepo.HasSentNotification(ctx, sub.ID, notifType)
		if err != nil || alreadySent {
			continue
		}

		// Determine notification recipient
		var recipientTelegramID int64
		lang := "en"

		if sub.TelegramChatID != 0 {
			recipientTelegramID = sub.TelegramChatID
		}

		user, err := s.userRepo.FindByID(ctx, sub.GetUserID())
		if err == nil && user != nil {
			lang = user.Language
			if recipientTelegramID == 0 {
				recipientTelegramID = user.TelegramID
			}
		}

		if recipientTelegramID == 0 {
			continue
		}

		// Build message
		subName := sub.Label
		if subName == "" {
			subName = "Subscription"
		}
		var msg string
		if daysLeft <= 0 {
			msg = i18n.Get(lang, i18nKey, subName)
		} else if daysLeft == 1 {
			msg = i18n.Get(lang, i18nKey, subName, sub.EndDate.Format("Jan 02, 2006"))
		} else {
			msg = i18n.Get(lang, i18nKey, subName, daysLeft, sub.EndDate.Format("Jan 02, 2006"))
		}

		// Create keyboard
		kb := &telebot.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data(i18n.Get(lang, "BtnViewDetailsNotif"), "sub_select", fmt.Sprintf("%d", sub.ID))),
		)

		// Send notification
		recipient := &telebot.User{ID: recipientTelegramID}
		_, err = s.bot.Send(recipient, msg, telebot.ModeMarkdown, kb)
		if err != nil {
			log.WithError(err).WithField("telegram_id", recipientTelegramID).Warn("Failed to send expiry notification")
			continue
		}

		// Record notification
		s.notifRepo.Create(ctx, &NotificationLog{
			UserID:         sub.GetUserID(),
			SubscriptionID: sub.ID,
			Type:           notifType,
		})

		log.WithFields(map[string]interface{}{
			"sub_id":  sub.ID,
			"user_id": sub.GetUserID(),
			"type":    notifType,
			"urgency": urgency,
			"days":    daysLeft,
		}).Info("Expiration notification sent")
	}
}

// sendDataUsageNotifications checks for high data usage and notifies users
func (s *Scheduler) sendDataUsageNotifications(ctx context.Context) {
	if s.notifRepo == nil || s.userRepo == nil || s.bot == nil {
		return
	}
	log := logger.GetLogger()

	// Get subscriptions that triggered a new warning level
	updatedSubs, err := s.subUsecase.CheckAndSendDataWarnings(ctx)
	if err != nil {
		log.WithError(err).Error("Failed to check data usage warnings")
		return
	}

	for _, sub := range updatedSubs {
		planName := sub.Label
		if planName == "" {
			planName = "Subscription"
		}

		// Determine notification recipient and language
		var recipientTelegramID int64
		lang := "en"

		if sub.TelegramChatID != 0 {
			recipientTelegramID = sub.TelegramChatID
		}

		user, err := s.userRepo.FindByID(ctx, sub.GetUserID())
		if err == nil && user != nil {
			lang = user.Language
			if recipientTelegramID == 0 {
				recipientTelegramID = user.TelegramID
			}
		}

		if recipientTelegramID == 0 {
			continue
		}

		// Map level to i18n key and urgency.
		// Level 4 (100%/exhausted) is handled by sendTrafficExhaustedNotifications
		// to avoid duplicate messages when the sub transitions to traffic_exhausted.
		var i18nKey string
		var urgency string
		switch sub.DataWarningLevel {
		case 4:
			continue
		case 3:
			i18nKey = "NotifData90"
			urgency = "🔴 Urgent"
		case 2:
			i18nKey = "NotifData75"
			urgency = "🟠 Warning"
		case 1:
			i18nKey = "NotifData50"
			urgency = "🟡 Notice"
		default:
			continue
		}

		msg := i18n.Get(lang, i18nKey, planName)

		// Create keyboard
		kb := &telebot.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data(i18n.Get(lang, "BtnViewUsageNotif"), "sub_select", fmt.Sprintf("%d", sub.ID))),
		)

		// Send notification
		recipient := &telebot.User{ID: recipientTelegramID}
		_, err = s.bot.Send(recipient, msg, telebot.ModeMarkdown, kb)
		if err != nil {
			log.WithError(err).WithField("telegram_id", recipientTelegramID).Warn("Failed to send data usage notification")
			continue
		}

		log.WithFields(map[string]interface{}{
			"sub_id":  sub.ID,
			"user_id": sub.GetUserID(),
			"level":   sub.DataWarningLevel,
			"urgency": urgency,
		}).Info("Data usage notification sent")
	}
}

// sendTrafficExhaustedNotifications sends a one-time renewal prompt when a
// subscription transitions to traffic_exhausted status.
func (s *Scheduler) sendTrafficExhaustedNotifications(ctx context.Context) {
	if s.notifRepo == nil || s.userRepo == nil || s.bot == nil {
		return
	}
	log := logger.GetLogger()

	activeSubs, err := s.subUsecase.ListAllSubscriptions(ctx, SubStatusTrafficExhausted, 0, 10000)
	if err != nil {
		return
	}

	for _, sub := range activeSubs {
		// Skip if already sent
		alreadySent, err := s.notifRepo.HasSentNotification(ctx, sub.ID, NotificationTypeDataExhausted)
		if err != nil || alreadySent {
			continue
		}

		var recipientTelegramID int64
		lang := "en"

		if sub.TelegramChatID != 0 {
			recipientTelegramID = sub.TelegramChatID
		}

		user, err := s.userRepo.FindByID(ctx, sub.GetUserID())
		if err == nil && user != nil {
			lang = user.Language
			if recipientTelegramID == 0 {
				recipientTelegramID = user.TelegramID
			}
		}

		if recipientTelegramID == 0 {
			continue
		}

		subName := sub.Label
		if subName == "" {
			subName = "Subscription"
		}
		msg := i18n.Get(lang, "NotifDataExhausted", subName)

		kb := &telebot.ReplyMarkup{}
		kb.Inline(
			kb.Row(kb.Data(i18n.Get(lang, "BtnViewDetailsNotif"), "sub_select", fmt.Sprintf("%d", sub.ID))),
		)

		recipient := &telebot.User{ID: recipientTelegramID}
		_, err = s.bot.Send(recipient, msg, telebot.ModeMarkdown, kb)
		if err != nil {
			log.WithError(err).WithField("telegram_id", recipientTelegramID).Warn("Failed to send traffic exhausted notification")
			continue
		}

		s.notifRepo.Create(ctx, &NotificationLog{
			UserID:         sub.GetUserID(),
			SubscriptionID: sub.ID,
			Type:           NotificationTypeDataExhausted,
		})

		log.WithFields(map[string]interface{}{
			"sub_id":  sub.ID,
			"user_id": sub.GetUserID(),
		}).Info("Traffic exhausted renewal notification sent")
	}
}
