package cmd

import (
	"context"
	"crypto/x509"
	"errors"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/selfsigned"
	"gorm.io/gorm"
)

// selfSignedStore keeps the panel's self-signed certificate in a hidden,
// protected settings row: out of the settings page and its export, in backups.
type selfSignedStore struct {
	repo settingDomain.SettingRepository
}

func (s selfSignedStore) Load(ctx context.Context) (string, error) {
	row, err := s.repo.GetByKey(ctx, settingDomain.SelfSignedCertKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

func (s selfSignedStore) Save(ctx context.Context, pemBlob string) error {
	return s.repo.Update(ctx, &settingDomain.Setting{
		Key:         settingDomain.SelfSignedCertKey,
		Value:       pemBlob,
		Type:        "string",
		Category:    "admin",
		Description: "The panel's self-signed HTTPS certificate and key (managed by the panel)",
		Label:       "Self-signed certificate",
		Sensitive:   true,
	})
}

// newSelfSignedManager names new certificates after the machine and the
// hosts of the panel's configured URLs, and audits the ones it issues on its
// own.
func newSelfSignedManager(repo settingDomain.SettingRepository, settings settingDomain.SettingUsecase, audit auditDomain.AuditLogUsecase) *selfsigned.Manager {
	names := func() []string {
		var hosts []string
		for _, key := range []string{"app_base_url", "sub_panel_url", "agent_hub_url"} {
			if v, err := settings.GetByKey(context.Background(), key); err == nil {
				if h := hostOf(v); h != "" {
					hosts = append(hosts, h)
				}
			}
		}
		return selfsigned.LocalNames(hosts...)
	}
	onIssue := func(ctx context.Context, reason string, old, cur *x509.Certificate) {
		if audit == nil {
			return
		}
		audit.Log(ctx, &auditDomain.AuditLog{
			Action:     string(auditDomain.AuditPanelCertificate),
			EntityType: "setting",
			OldValues:  selfsigned.AuditJSON(old, ""),
			NewValues:  selfsigned.AuditJSON(cur, reason),
			Source:     "system",
		})
	}
	return selfsigned.NewManager(selfSignedStore{repo}, names, onIssue)
}
