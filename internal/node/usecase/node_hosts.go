package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
)

var (
	ErrHostNotFound          = errors.New("host not found")
	ErrHostMissingTarget     = errors.New("host must have either inbound_id or plan_id")
	ErrHostConflictingTarget = errors.New("host cannot have both inbound_id and plan_id")
)

// AddHost creates a new host. Validates XOR: either InboundID or PlanID must be set, not both.
// Hosts are presentation-only — no Xray config push needed.
func (u *nodeUsecase) AddHost(ctx context.Context, host *domain.Host) error {
	log := logger.GetLogger()

	hasInbound := host.InboundID != nil && *host.InboundID > 0
	hasPlan := host.PlanID != nil && *host.PlanID > 0

	// XOR validation
	if !hasInbound && !hasPlan {
		return ErrHostMissingTarget
	}
	if hasInbound && hasPlan {
		return ErrHostConflictingTarget
	}

	if hasInbound {
		// Validate inbound exists
		_, err := u.nodeRepo.GetInbound(ctx, *host.InboundID)
		if err != nil {
			log.WithError(err).WithField("inbound_id", *host.InboundID).Error("[AddHost] Inbound not found")
			return ErrInboundNotFound
		}
		// Clear plan_id to be safe
		host.PlanID = nil
	} else {
		// Info-only host: clear inbound_id to be safe
		host.InboundID = nil
	}

	if err := u.nodeRepo.CreateHost(ctx, host); err != nil {
		log.WithError(err).Error("[AddHost] Failed to create host")
		return err
	}

	log.WithFields(map[string]interface{}{
		"host_id":    host.ID,
		"inbound_id": host.InboundID,
		"plan_id":    host.PlanID,
		"remark":     host.Remark,
	}).Info("[AddHost] Host created successfully")
	return nil
}

// BulkCreateInfoHosts creates the same info host across multiple plans.
func (u *nodeUsecase) BulkCreateInfoHosts(ctx context.Context, hostTemplate *domain.Host, planIDs []uint) ([]*domain.Host, error) {
	log := logger.GetLogger()

	var created []*domain.Host
	for _, planID := range planIDs {
		clone := *hostTemplate
		clone.ID = 0
		clone.InboundID = nil
		pid := planID
		clone.PlanID = &pid
		clone.CreatedAt = time.Time{}
		clone.UpdatedAt = time.Time{}
		clone.Inbound = nil

		if err := u.nodeRepo.CreateHost(ctx, &clone); err != nil {
			log.WithError(err).WithField("plan_id", planID).Error("[BulkCreateInfoHosts] Failed to create host for plan")
			return created, fmt.Errorf("failed to create host for plan %d: %w", planID, err)
		}
		created = append(created, &clone)
	}

	log.WithField("count", len(created)).Info("[BulkCreateInfoHosts] Bulk info hosts created")
	return created, nil
}

// ListHosts returns all hosts for a given inbound, ordered by priority.
func (u *nodeUsecase) ListHosts(ctx context.Context, inboundID uint) ([]*domain.Host, error) {
	return u.nodeRepo.ListHostsByInbound(ctx, inboundID)
}

// ListHostsByPlan returns all info-only hosts for a given plan, ordered by priority.
func (u *nodeUsecase) ListHostsByPlan(ctx context.Context, planID uint) ([]*domain.Host, error) {
	return u.nodeRepo.ListHostsByPlan(ctx, planID)
}

// GetHost returns a single host by ID.
func (u *nodeUsecase) GetHost(ctx context.Context, id uint) (*domain.Host, error) {
	host, err := u.nodeRepo.GetHost(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostNotFound
		}
		return nil, err
	}
	return host, nil
}

// UpdateHost updates a host with validation. Hosts are presentation-only — no Xray config push needed.
func (u *nodeUsecase) UpdateHost(ctx context.Context, host *domain.Host) error {
	log := logger.GetLogger()

	// Validate XOR: either InboundID or PlanID must be set, not both
	hasInbound := host.InboundID != nil && *host.InboundID > 0
	hasPlan := host.PlanID != nil && *host.PlanID > 0

	if !hasInbound && !hasPlan {
		return ErrHostMissingTarget
	}
	if hasInbound && hasPlan {
		return ErrHostConflictingTarget
	}

	if hasInbound {
		if _, err := u.nodeRepo.GetInbound(ctx, *host.InboundID); err != nil {
			log.WithError(err).WithField("inbound_id", *host.InboundID).Error("[UpdateHost] Inbound not found")
			return ErrInboundNotFound
		}
	}

	if err := u.nodeRepo.UpdateHost(ctx, host); err != nil {
		return err
	}
	return nil
}

// DeleteHost removes a host. Hosts are presentation-only — no Xray config push needed.
func (u *nodeUsecase) DeleteHost(ctx context.Context, id uint) error {
	log := logger.GetLogger()
	log.WithField("host_id", id).Info("[DeleteHost] Deleting host")

	if err := u.nodeRepo.DeleteHost(ctx, id); err != nil {
		log.WithError(err).Error("[DeleteHost] Failed to delete host")
		return err
	}
	return nil
}

// ListAllHosts returns all hosts across all inbounds with filtering and pagination.
func (u *nodeUsecase) ListAllHosts(ctx context.Context, search string, nodeID, inboundID, planID uint, isDisabled *bool, hostType string, tag string, sortBy string, sortOrder string, offset, limit int) ([]*domain.Host, int64, error) {
	return u.nodeRepo.ListAllHosts(ctx, search, nodeID, inboundID, planID, isDisabled, hostType, tag, sortBy, sortOrder, offset, limit)
}

// DuplicateHost creates a copy of an existing host with " (copy)" appended to the remark.
func (u *nodeUsecase) DuplicateHost(ctx context.Context, id uint) (*domain.Host, error) {
	log := logger.GetLogger()

	original, err := u.nodeRepo.GetHost(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostNotFound
		}
		return nil, err
	}

	// Deep copy to avoid shared pointer mutation
	clone := *original
	clone.ID = 0
	clone.Remark = original.Remark + " (copy)"
	clone.CreatedAt = time.Time{}
	clone.UpdatedAt = time.Time{}
	clone.Inbound = nil

	// Deep copy pointer fields
	if original.InboundID != nil {
		v := *original.InboundID
		clone.InboundID = &v
	}
	if original.PlanID != nil {
		v := *original.PlanID
		clone.PlanID = &v
	}
	if original.Port != nil {
		v := *original.Port
		clone.Port = &v
	}
	if original.AllowInsecure != nil {
		v := *original.AllowInsecure
		clone.AllowInsecure = &v
	}
	if original.FragmentSettings != nil {
		fs := *original.FragmentSettings
		clone.FragmentSettings = &fs
	}

	if err := u.nodeRepo.CreateHost(ctx, &clone); err != nil {
		log.WithError(err).Error("[DuplicateHost] Failed to create duplicate host")
		return nil, err
	}

	log.WithFields(map[string]interface{}{
		"original_id": id,
		"clone_id":    clone.ID,
	}).Info("[DuplicateHost] Host duplicated successfully")

	return &clone, nil
}

// BulkUpdateHosts updates specified fields on multiple hosts.
func (u *nodeUsecase) BulkUpdateHosts(ctx context.Context, ids []uint, fields map[string]any) (int64, error) {
	log := logger.GetLogger()

	allowed := map[string]bool{
		"address": true, "port": true, "sni": true, "host": true,
		"path": true, "alpn": true, "fingerprint": true, "security": true,
		"allow_insecure": true, "priority": true, "is_disabled": true, "tags": true,
	}
	for k := range fields {
		if !allowed[k] {
			return 0, fmt.Errorf("field %q is not allowed for bulk update", k)
		}
	}

	affected, err := u.nodeRepo.BulkUpdateHosts(ctx, ids, fields)
	if err != nil {
		log.WithError(err).Error("[BulkUpdateHosts] Failed")
		return 0, err
	}
	log.WithField("count", affected).Info("[BulkUpdateHosts] Updated hosts")
	return affected, nil
}

// ListHostTags returns all unique tags across hosts.
func (u *nodeUsecase) ListHostTags(ctx context.Context) ([]string, error) {
	return u.nodeRepo.ListHostTags(ctx)
}

// === Host Template Management ===

var ErrHostTemplateNotFound = errors.New("host template not found")

func (u *nodeUsecase) CreateHostTemplate(ctx context.Context, template *domain.HostTemplate) error {
	return u.nodeRepo.CreateHostTemplate(ctx, template)
}

func (u *nodeUsecase) GetHostTemplate(ctx context.Context, id uint) (*domain.HostTemplate, error) {
	t, err := u.nodeRepo.GetHostTemplate(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrHostTemplateNotFound
		}
		return nil, err
	}
	return t, nil
}

func (u *nodeUsecase) UpdateHostTemplate(ctx context.Context, template *domain.HostTemplate) error {
	return u.nodeRepo.UpdateHostTemplate(ctx, template)
}

func (u *nodeUsecase) DeleteHostTemplate(ctx context.Context, id uint) error {
	return u.nodeRepo.DeleteHostTemplate(ctx, id)
}

func (u *nodeUsecase) ListHostTemplates(ctx context.Context) ([]*domain.HostTemplate, error) {
	return u.nodeRepo.ListHostTemplates(ctx)
}

// ApplyHostTemplate applies a template's fields to the specified hosts via bulk update.
func (u *nodeUsecase) ApplyHostTemplate(ctx context.Context, templateID uint, hostIDs []uint) (int64, error) {
	log := logger.GetLogger()

	tmpl, err := u.nodeRepo.GetHostTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrHostTemplateNotFound
		}
		return 0, err
	}

	fields := make(map[string]any)
	if tmpl.Remark != "" {
		fields["remark"] = tmpl.Remark
	}
	if tmpl.Address != "" {
		fields["address"] = tmpl.Address
	}
	if tmpl.Port != nil {
		fields["port"] = *tmpl.Port
	}
	if tmpl.SNI != "" {
		fields["sni"] = tmpl.SNI
	}
	if tmpl.Host != "" {
		fields["host"] = tmpl.Host
	}
	if tmpl.Path != "" {
		fields["path"] = tmpl.Path
	}
	if tmpl.ALPN != "" {
		fields["alpn"] = tmpl.ALPN
	}
	if tmpl.Fingerprint != "" {
		fields["fingerprint"] = tmpl.Fingerprint
	}
	if tmpl.Security != "" {
		fields["security"] = tmpl.Security
	}
	if tmpl.AllowInsecure != nil {
		fields["allow_insecure"] = *tmpl.AllowInsecure
	}
	if tmpl.Priority != nil {
		fields["priority"] = *tmpl.Priority
	}

	if len(fields) == 0 {
		return 0, nil
	}

	affected, err := u.nodeRepo.BulkUpdateHosts(ctx, hostIDs, fields)
	if err != nil {
		log.WithError(err).Error("[ApplyHostTemplate] Failed")
		return 0, err
	}
	log.WithFields(map[string]interface{}{
		"template_id": templateID,
		"count":       affected,
	}).Info("[ApplyHostTemplate] Template applied")
	return affected, nil
}
