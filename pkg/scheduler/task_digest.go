package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gopkg.in/telebot.v3"
)

// checkAndSendDailyDigest sends admin digest once per day
func (s *Scheduler) checkAndSendDailyDigest(ctx context.Context) {
	now := time.Now().UTC()

	// Only send at 9 AM UTC and if not sent today
	if now.Hour() != 9 {
		return
	}
	todayStart := now.Truncate(24 * time.Hour)

	s.mu.Lock()
	alreadySent := s.lastDailyDigest.After(todayStart)
	s.mu.Unlock()
	if alreadySent {
		return
	}

	s.sendAdminDailyDigest(ctx)

	s.mu.Lock()
	s.lastDailyDigest = now
	s.mu.Unlock()
}

// sendAdminDailyDigest sends a summary to all admins
func (s *Scheduler) sendAdminDailyDigest(ctx context.Context) {
	if len(s.adminTelegramIDs) == 0 || s.bot == nil {
		return
	}
	log := logger.GetLogger()

	activeSubs, err := s.subUsecase.ListAllSubscriptions(ctx, SubStatusActive, 0, 10000)
	if err != nil {
		return
	}

	var expiring7d, expiring1d, expiredToday []string

	for _, sub := range activeSubs {
		if sub.EndDate == nil {
			continue
		}

		subName := sub.Label
		if subName == "" {
			subName = "Subscription"
		}

		daysLeft := sub.DaysRemaining()

		switch {
		case daysLeft <= 0:
			expiredToday = append(expiredToday, fmt.Sprintf("• Sub #%d - %s", sub.ID, subName))
		case daysLeft <= 1:
			expiring1d = append(expiring1d, fmt.Sprintf("• Sub #%d - %s (expires %s)", sub.ID, subName, sub.EndDate.Format("Jan 02")))
		case daysLeft <= 7:
			expiring7d = append(expiring7d, fmt.Sprintf("• Sub #%d - %s (%d days)", sub.ID, subName, daysLeft))
		}
	}

	if len(expiring7d) == 0 && len(expiring1d) == 0 && len(expiredToday) == 0 {
		return // Nothing to report
	}

	msg := "📊 *Daily Subscription Report*\n━━━━━━━━━━━━━━━━━━━━\n\n"

	if len(expiring7d) > 0 {
		msg += "⏰ *Expiring Soon (7 days):*\n"
		for _, item := range expiring7d {
			msg += item + "\n"
		}
		msg += "\n"
	}

	if len(expiring1d) > 0 {
		msg += "⚠️ *Expiring Tomorrow:*\n"
		for _, item := range expiring1d {
			msg += item + "\n"
		}
		msg += "\n"
	}

	if len(expiredToday) > 0 {
		msg += "🚫 *Expired Today:*\n"
		for _, item := range expiredToday {
			msg += item + "\n"
		}
	}

	for _, adminID := range s.adminTelegramIDs {
		recipient := &telebot.User{ID: adminID}
		_, err := s.bot.Send(recipient, msg, telebot.ModeMarkdown)
		if err != nil {
			log.WithError(err).WithField("admin_id", adminID).Warn("Failed to send admin digest")
		}
	}

	log.Info("Admin daily digest sent")
}
