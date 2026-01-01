package scheduler

import (
	"context"
	"fmt"
	"time"

	sniDomain "github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gopkg.in/telebot.v3"
)

// checkCertRenewals auto-renews expiring ACME certs and alerts admins about
// manual certs (which can't be auto-renewed) before they lapse. A renewal
// updates the cert on record; the SNI usecase re-pushes it to the nodes.
func (s *Scheduler) checkCertRenewals(ctx context.Context) {
	log := logger.GetLogger()
	certs, err := s.sniUsecase.GetExpiringCertificates(ctx, 30)
	if err != nil {
		log.WithError(err).Error("Scheduler: Failed to check expiring certificates")
		return
	}

	for _, sni := range certs {
		if sni.AutoRenew && sni.IsAutoIssued {
			log.WithField("domain", sni.Domain).Info("Scheduler: Renewing certificate...")
			if err := s.sniUsecase.RenewCertificate(ctx, sni.ID); err != nil {
				log.WithField("domain", sni.Domain).WithError(err).Error("Scheduler: Failed to auto-renew certificate")
			}
			continue
		}

		// Manual cert — can't auto-renew. Alert admins once per threshold.
		bucket := expiryBucket(sni.ExpiresAt)
		if bucket != 0 && (sni.ExpiryNotifyLevel == 0 || bucket < sni.ExpiryNotifyLevel) {
			s.notifyAdminsCertExpiring(sni)
			if err := s.sniUsecase.MarkExpiryNotified(ctx, sni.ID, bucket); err != nil {
				log.WithError(err).Warn("Scheduler: Failed to record cert expiry notification")
			}
		}
	}
}

// expiryBucket maps time-until-expiry to an alert threshold (30/7/1 days); 0 = no alert.
func expiryBucket(expiresAt time.Time) int {
	if expiresAt.IsZero() {
		return 0
	}
	days := int(time.Until(expiresAt).Hours() / 24)
	switch {
	case days <= 1:
		return 1
	case days <= 7:
		return 7
	case days <= 30:
		return 30
	default:
		return 0
	}
}

func (s *Scheduler) notifyAdminsCertExpiring(sni *sniDomain.SNI) {
	if s.bot == nil || len(s.adminTelegramIDs) == 0 {
		return
	}
	daysLeft := int(time.Until(sni.ExpiresAt).Hours() / 24)
	msg := fmt.Sprintf("⚠️ *Certificate expiring*\n\nDomain: `%s`\nExpires: %s (~%d day(s) left)\n\n"+
		"This certificate is manual and won't auto-renew — re-import a fresh one from the Domains page.",
		sni.Domain, sni.ExpiresAt.Format("2006-01-02"), daysLeft)
	for _, adminID := range s.adminTelegramIDs {
		recipient := &telebot.User{ID: adminID}
		if _, err := s.bot.Send(recipient, msg, telebot.ModeMarkdown); err != nil {
			logger.GetLogger().WithError(err).Warn("Scheduler: Failed to send cert expiry alert")
		}
	}
}
