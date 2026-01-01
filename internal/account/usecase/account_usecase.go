package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/internal/account/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/account/repository"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	nodeRepo "github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/provisioning"
	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

var (
	ErrAccountNotFound = errors.New("account not found")
	ErrInboundNotFound = errors.New("inbound not found")
	ErrEmailExists     = errors.New("account with this email already exists")
	ErrDuplicateUUID   = errors.New("another account on the same inbound already uses this UUID")
)

// NodeSyncTrigger is the narrow slice of the node usecase the account
// usecase needs. Declared here so we don't import the entire node
// usecase package (and risk a cycle or blast-radius churn when node's
// interface changes).
type NodeSyncTrigger interface {
	SyncSingleNodeByID(ctx context.Context, nodeID uint) error
}

type AccountUsecase interface {
	// Lifecycle
	CreateAccount(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, source domain.AccountSource, subID *uint) (*domain.Account, error)
	CreateAccountForSubscription(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
	CreateAccountForSubscriptionNoEnqueue(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error
	CreateAccountManual(ctx context.Context, inboundID uint, email, uuid, flow, encryption string) (uint, string, error) // Returns (accountID, link, error)
	GetAccount(ctx context.Context, id uint) (*domain.Account, error)
	GetAccountByEmail(ctx context.Context, email string) (*domain.Account, error)
	GetLinkByEmail(ctx context.Context, email string) (string, error)

	// Listing
	ListAccountsByInbound(ctx context.Context, inboundID uint) ([]*domain.Account, error)
	ListAccountsByNode(ctx context.Context, nodeID uint) ([]*domain.Account, error)
	ListAccountsByNodePaginated(ctx context.Context, nodeID uint, page, perPage int) ([]*domain.Account, int64, error)
	ListAccountsBySubscription(ctx context.Context, subID uint) ([]*domain.Account, error)
	ListAllAccountsBySubscription(ctx context.Context, subID uint) ([]*domain.Account, error)
	// ListAccountsByEmails resolves a batch of emails back to their owning
	// accounts (with Subscription preloaded). Used for cross-subscription
	// access-history search to map hits to subscriptions.
	ListAccountsByEmails(ctx context.Context, emails []string) ([]*domain.Account, error)
	// ListAccountsBySubscriptionIDs batches the per-sub lookup so global
	// access-history search avoids N+1 round-trips.
	ListAccountsBySubscriptionIDs(ctx context.Context, subIDs []uint) ([]*domain.Account, error)
	ListActiveAccounts(ctx context.Context) ([]*domain.Account, error)
	ListActiveAccountInfos(ctx context.Context) ([]*contract.AccountInfo, error)
	ListAllAccounts(ctx context.Context, filter repository.AccountFilter) ([]*domain.Account, error)
	CountAccounts(ctx context.Context, filter repository.AccountFilter) (int64, error)

	// State management
	DisableAccount(ctx context.Context, id uint) error
	EnableAccount(ctx context.Context, id uint) error
	DeleteAccount(ctx context.Context, id uint) error

	// Stats
	SyncAccountStats(ctx context.Context, id uint) error
	UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error

	// Update
	UpdateAccount(ctx context.Context, id uint, email, uuid, flow string, dataLimit int64, expiresAt *time.Time, enabled bool) error

	// Bulk operations for subscriptions
	DisableAccountsBySubscription(ctx context.Context, subID uint) error
	EnableAccountsBySubscription(ctx context.Context, subID uint) error
	DeleteAccountsBySubscription(ctx context.Context, subID uint) error
	ForceDeleteAccountsBySubscription(ctx context.Context, subID uint) error

	// Link generation
	GenerateAccountLink(ctx context.Context, id uint) (string, error)

	// Migration
	MigrateAccount(ctx context.Context, id uint, targetInboundID uint) error

	// Per-inbound data limit enforcement
	CheckAndDisableExhaustedAccounts(ctx context.Context) (int, error)

	// Clear account-level data limits when subscription override is set
	ClearAccountDataLimitsBySubscription(ctx context.Context, subID uint) error

	// Reset account-level data_used on subscription renewal
	ResetAccountDataUsedBySubscription(ctx context.Context, subID uint) error

	// Bulk-set the UUID for every account under a subscription. Returns the
	// number of accounts successfully updated. If any account update fails the
	// error is non-nil, but updates already applied to earlier accounts remain.
	SetAccountsUUIDBySubscription(ctx context.Context, subID uint, newUUID string) (int, error)
}

type accountUsecase struct {
	accountRepo repository.AccountRepository
	nodeRepo    nodeRepo.NodeRepository
	nodeSync    NodeSyncTrigger
	provService provisioning.ProvisioningService
}

func NewAccountUsecase(
	accountRepo repository.AccountRepository,
	nodeRepo nodeRepo.NodeRepository,
	nodeSync NodeSyncTrigger,
	provService provisioning.ProvisioningService,
) AccountUsecase {
	return &accountUsecase{
		accountRepo: accountRepo,
		nodeRepo:    nodeRepo,
		nodeSync:    nodeSync,
		provService: provService,
	}
}

func (u *accountUsecase) CreateAccount(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, source domain.AccountSource, subID *uint) (*domain.Account, error) {
	log := logger.GetLogger()

	// Get inbound details
	inbound, err := u.nodeRepo.GetInboundWithNode(ctx, inboundID)
	if err != nil {
		return nil, ErrInboundNotFound
	}
	if inbound.Node == nil {
		return nil, errors.New("inbound has no associated node")
	}

	// Check if email already exists on this inbound
	existing, _ := u.accountRepo.FindByEmailAndInbound(ctx, email, inboundID)
	if existing != nil {
		return nil, ErrEmailExists
	}

	// Check if email exists in soft-deleted state and cleanup
	// Clean up soft-deleted record holding the unique constraint on (inbound_id, email)
	zombie, _ := u.accountRepo.FindByEmailAndInboundUnscoped(ctx, email, inboundID)
	if zombie != nil && zombie.DeletedAt.Valid {
		if err := u.accountRepo.ForceDelete(ctx, zombie.ID); err != nil {
			log.WithError(err).Warnf("Failed to force delete zombie account %s", email)
		}
	}

	// Provision on Xray (Async via queue)
	target := fmt.Sprintf("%s:%d", inbound.Node.IP, inbound.Node.APIPort)

	// Save to database first
	account := &domain.Account{
		InboundID:      inboundID,
		Email:          email,
		UUID:           uuid,
		Flow:           flow,
		Encryption:     encryption,
		Source:         source,
		SubscriptionID: subID,
		Status:         domain.AccountStatusActive,
		Inbound:        inbound, // Attach loaded inbound for provisioning
	}

	if err := u.accountRepo.Create(ctx, account); err != nil {
		return nil, err
	}

	// Enqueue provisioning task — failure means the user won't get provisioned
	if err := u.provService.EnqueueAddUser(ctx, account, target); err != nil {
		log.WithError(err).Errorf("Failed to enqueue provisioning task for account %s, marking as pending_provision", email)
		account.Status = domain.AccountStatusPendingProvision
		_ = u.accountRepo.Update(ctx, account)
		return account, fmt.Errorf("account created but provisioning enqueue failed: %w", err)
	}

	log.Infof("Account created and provisioning enqueued: %s on inbound %s (source: %s)", email, inbound.Tag, source)
	return account, nil
}

// CreateAccountForSubscription saves an account record for a subscription
// It does NOT provision on Xray as that's already done by the provider
// dataLimit is the per-inbound data limit in bytes (0 = unlimited)
func (u *accountUsecase) CreateAccountForSubscription(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error {
	log := logger.GetLogger()

	// Check if account already exists on this inbound
	existing, _ := u.accountRepo.FindByEmailAndInbound(ctx, email, inboundID)
	if existing != nil {
		// Account already tracked on this inbound, skip
		return nil
	}

	// Clean up soft-deleted record holding the unique constraint
	zombie, _ := u.accountRepo.FindByEmailAndInboundUnscoped(ctx, email, inboundID)
	if zombie != nil && zombie.DeletedAt.Valid {
		_ = u.accountRepo.ForceDelete(ctx, zombie.ID)
	}

	account := &domain.Account{
		InboundID:      inboundID,
		Email:          email,
		UUID:           uuid,
		Flow:           flow,
		Encryption:     encryption,
		DataLimit:      dataLimit,
		Source:         domain.AccountSourceSubscription,
		SubscriptionID: &subID,
		Status:         domain.AccountStatusActive,
	}

	if err := u.accountRepo.Create(ctx, account); err != nil {
		log.WithError(err).Warnf("Failed to save account record for %s", email)
		return err
	}

	// 2. Enqueue Task
	// Get inbound details for node info (needed for provisioning)
	inbound, err := u.nodeRepo.GetInboundWithNode(ctx, inboundID)
	if err == nil && inbound.Node != nil {
		account.Inbound = inbound // Attach loaded inbound
		target := fmt.Sprintf("%s:%d", inbound.Node.IP, inbound.Node.APIPort)
		if err := u.provService.EnqueueAddUser(ctx, account, target); err != nil {
			log.WithError(err).Warnf("Failed to enqueue provisioning for %s, marking as pending_provision", email)
			account.Status = domain.AccountStatusPendingProvision
			_ = u.accountRepo.Update(ctx, account)
		}
	}

	log.Infof("Account record saved and queued: %s (subscription #%d)", email, subID)
	return nil
}

// CreateAccountForSubscriptionNoEnqueue saves an account record without enqueuing a provisioning task.
// Used when the provider has already successfully provisioned the user on this inbound.
func (u *accountUsecase) CreateAccountForSubscriptionNoEnqueue(ctx context.Context, inboundID uint, email, uuid, flow, encryption string, subID uint, dataLimit int64) error {
	log := logger.GetLogger()

	// Check if account already exists on this inbound
	existing, _ := u.accountRepo.FindByEmailAndInbound(ctx, email, inboundID)
	if existing != nil {
		return nil
	}

	// Clean up soft-deleted record holding the unique constraint
	zombie, _ := u.accountRepo.FindByEmailAndInboundUnscoped(ctx, email, inboundID)
	if zombie != nil && zombie.DeletedAt.Valid {
		_ = u.accountRepo.ForceDelete(ctx, zombie.ID)
	}

	account := &domain.Account{
		InboundID:      inboundID,
		Email:          email,
		UUID:           uuid,
		Flow:           flow,
		Encryption:     encryption,
		DataLimit:      dataLimit,
		Source:         domain.AccountSourceSubscription,
		SubscriptionID: &subID,
		Status:         domain.AccountStatusActive,
	}

	if err := u.accountRepo.Create(ctx, account); err != nil {
		log.WithError(err).Warnf("Failed to save account record (no enqueue) for %s", email)
		return err
	}

	log.Infof("Account record saved (no enqueue): %s (subscription #%d)", email, subID)
	return nil
}

// CreateAccountManual provisions a manual account on Xray and saves it to DB
// Returns (accountID, link, error)
func (u *accountUsecase) CreateAccountManual(ctx context.Context, inboundID uint, email, uuid, flow, encryption string) (uint, string, error) {
	account, err := u.CreateAccount(ctx, inboundID, email, uuid, flow, encryption, domain.AccountSourceManual, nil)
	if err != nil {
		return 0, "", err
	}

	// Generate link
	link, err := u.GenerateAccountLink(ctx, account.ID)
	if err != nil {
		// Account was created but link generation failed - still return success
		return account.ID, "", nil
	}

	return account.ID, link, nil
}

// GetLinkByEmail finds an account by email and generates its link
func (u *accountUsecase) GetLinkByEmail(ctx context.Context, email string) (string, error) {
	account, err := u.accountRepo.FindByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return u.GenerateAccountLink(ctx, account.ID)
}

func (u *accountUsecase) GetAccount(ctx context.Context, id uint) (*domain.Account, error) {
	return u.accountRepo.FindByID(ctx, id)
}

func (u *accountUsecase) GetAccountByEmail(ctx context.Context, email string) (*domain.Account, error) {
	return u.accountRepo.FindByEmail(ctx, email)
}

func (u *accountUsecase) ListAccountsByInbound(ctx context.Context, inboundID uint) ([]*domain.Account, error) {
	return u.accountRepo.ListByInboundID(ctx, inboundID)
}

func (u *accountUsecase) ListAccountsByNode(ctx context.Context, nodeID uint) ([]*domain.Account, error) {
	return u.accountRepo.ListByNodeID(ctx, nodeID)
}

func (u *accountUsecase) ListAccountsByNodePaginated(ctx context.Context, nodeID uint, page, perPage int) ([]*domain.Account, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	offset := (page - 1) * perPage
	return u.accountRepo.ListByNodeIDPaginated(ctx, nodeID, offset, perPage)
}

func (u *accountUsecase) ListAccountsBySubscription(ctx context.Context, subID uint) ([]*domain.Account, error) {
	return u.accountRepo.ListBySubscriptionID(ctx, subID)
}

func (u *accountUsecase) ListAllAccountsBySubscription(ctx context.Context, subID uint) ([]*domain.Account, error) {
	return u.accountRepo.ListAllBySubscriptionID(ctx, subID)
}

func (u *accountUsecase) ListAccountsByEmails(ctx context.Context, emails []string) ([]*domain.Account, error) {
	return u.accountRepo.FindByEmails(ctx, emails)
}

func (u *accountUsecase) ListAccountsBySubscriptionIDs(ctx context.Context, subIDs []uint) ([]*domain.Account, error) {
	return u.accountRepo.FindBySubscriptionIDs(ctx, subIDs)
}

func (u *accountUsecase) ListActiveAccounts(ctx context.Context) ([]*domain.Account, error) {
	return u.accountRepo.ListActive(ctx)
}

func (u *accountUsecase) ListActiveAccountInfos(ctx context.Context) ([]*contract.AccountInfo, error) {
	accounts, err := u.accountRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*contract.AccountInfo, 0, len(accounts))
	for _, acc := range accounts {
		result = append(result, &contract.AccountInfo{
			ID:         acc.ID,
			InboundID:  acc.InboundID,
			Email:      acc.Email,
			UUID:       acc.UUID,
			Flow:       acc.Flow,
			Encryption: acc.Encryption,
		})
	}
	return result, nil
}

func (u *accountUsecase) ListAllAccounts(ctx context.Context, filter repository.AccountFilter) ([]*domain.Account, error) {
	return u.accountRepo.ListAll(ctx, filter)
}

func (u *accountUsecase) CountAccounts(ctx context.Context, filter repository.AccountFilter) (int64, error) {
	return u.accountRepo.Count(ctx, filter)
}

func (u *accountUsecase) DisableAccount(ctx context.Context, id uint) error {
	log := logger.GetLogger()

	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}

	if account.Inbound == nil || account.Inbound.Node == nil {
		return errors.New("account inbound/node not loaded")
	}

	// Remove from Xray (Async via queue)
	target := fmt.Sprintf("%s:%d", account.Inbound.Node.IP, account.Inbound.Node.APIPort)
	if err := u.provService.EnqueueRemoveUser(ctx, account, target); err != nil {
		log.WithError(err).Warnf("Failed to enqueue remove task for account %s", account.Email)
	}

	return u.accountRepo.UpdateStatus(ctx, id, domain.AccountStatusDisabled)
}

func (u *accountUsecase) EnableAccount(ctx context.Context, id uint) error {
	log := logger.GetLogger()

	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}

	if account.Inbound == nil || account.Inbound.Node == nil {
		return errors.New("account inbound/node not loaded")
	}

	// Add to Xray (Async via queue)
	target := fmt.Sprintf("%s:%d", account.Inbound.Node.IP, account.Inbound.Node.APIPort)
	if err := u.provService.EnqueueAddUser(ctx, account, target); err != nil {
		log.WithError(err).Warnf("Failed to enqueue add task for account %s", account.Email)
	}

	return u.accountRepo.UpdateStatus(ctx, id, domain.AccountStatusActive)
}

func (u *accountUsecase) DeleteAccount(ctx context.Context, id uint) error {
	log := logger.GetLogger()

	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}

	if account.Inbound != nil && account.Inbound.Node != nil {
		target := fmt.Sprintf("%s:%d", account.Inbound.Node.IP, account.Inbound.Node.APIPort)
		if err := u.provService.EnqueueRemoveUser(ctx, account, target); err != nil {
			log.WithError(err).Warnf("Failed to enqueue remove task for account %s during delete", account.Email)
		}
	}

	return u.accountRepo.Delete(ctx, id)
}

func (u *accountUsecase) SyncAccountStats(ctx context.Context, id uint) error {
	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}
	if account.Inbound == nil || account.Inbound.Node == nil {
		return errors.New("account inbound/node not loaded")
	}
	if u.nodeSync == nil {
		return errors.New("node sync trigger not wired")
	}
	return u.nodeSync.SyncSingleNodeByID(ctx, account.Inbound.Node.ID)
}

func (u *accountUsecase) UpdateDataUsed(ctx context.Context, id uint, dataUsed int64) error {
	return u.accountRepo.UpdateDataUsed(ctx, id, dataUsed)
}

func (u *accountUsecase) DisableAccountsBySubscription(ctx context.Context, subID uint) error {
	accounts, err := u.accountRepo.ListBySubscriptionID(ctx, subID)
	if err != nil {
		return err
	}
	var errs []error
	for _, acc := range accounts {
		if err := u.DisableAccount(ctx, acc.ID); err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to disable account %d", acc.ID)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to disable %d/%d accounts", len(errs), len(accounts))
	}
	return nil
}

func (u *accountUsecase) EnableAccountsBySubscription(ctx context.Context, subID uint) error {
	accounts, err := u.accountRepo.ListBySubscriptionID(ctx, subID)
	if err != nil {
		return err
	}
	var errs []error
	for _, acc := range accounts {
		if err := u.EnableAccount(ctx, acc.ID); err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to enable account %d", acc.ID)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to enable %d/%d accounts", len(errs), len(accounts))
	}
	return nil
}

func (u *accountUsecase) DeleteAccountsBySubscription(ctx context.Context, subID uint) error {
	accounts, err := u.accountRepo.ListAllBySubscriptionID(ctx, subID)
	if err != nil {
		return err
	}
	var errs []error
	for _, acc := range accounts {
		if err := u.DeleteAccount(ctx, acc.ID); err != nil {
			logger.GetLogger().WithError(err).Warnf("Failed to delete account %d", acc.ID)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to delete %d/%d accounts", len(errs), len(accounts))
	}
	return nil
}

func (u *accountUsecase) ForceDeleteAccountsBySubscription(ctx context.Context, subID uint) error {
	log := logger.GetLogger()

	// Enqueue Xray removal for non-soft-deleted accounts (soft-deleted ones are already removed from Xray)
	accounts, _ := u.accountRepo.ListAllBySubscriptionID(ctx, subID)
	for _, acc := range accounts {
		if acc.Inbound != nil && acc.Inbound.Node != nil {
			target := fmt.Sprintf("%s:%d", acc.Inbound.Node.IP, acc.Inbound.Node.APIPort)
			if err := u.provService.EnqueueRemoveUser(ctx, acc, target); err != nil {
				log.WithError(err).Warnf("Failed to enqueue remove task for account %s during force delete", acc.Email)
			}
		}
	}

	// Hard-delete ALL account rows (including soft-deleted) in a single unscoped query
	return u.accountRepo.ForceDeleteBySubscriptionID(ctx, subID)
}

func (u *accountUsecase) GenerateAccountLink(ctx context.Context, id uint) (string, error) {
	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return "", ErrAccountNotFound
	}

	if account.Inbound == nil || account.Inbound.Node == nil {
		return "", errors.New("account inbound/node not loaded")
	}

	inbound := account.Inbound

	// Use inbound's custom Address if set, otherwise fall back to Node IP
	nodeIP := inbound.Node.IP
	if inbound.Address != "" {
		nodeIP = inbound.Address
	}

	// Build InboundInfo for link generation
	inboundInfo := &xray.InboundInfo{
		Tag:      inbound.Tag,
		Protocol: inbound.Protocol,
		Port:     uint32(inbound.Port),
		Network:  inbound.Network,
		Security: inbound.Security,
	}

	// Populate TLS settings
	if tlsSettings := inbound.GetTLSSettingsOrDefault(); tlsSettings != nil && tlsSettings.ServerName != "" {
		inboundInfo.TLSConfig = &xray.TLSInfoConfig{
			SNI:         tlsSettings.ServerName,
			ALPN:        tlsSettings.ALPN,
			Fingerprint: tlsSettings.Fingerprint,
		}
	}

	// Populate Reality settings
	if realitySettings := inbound.GetRealitySettingsOrDefault(); realitySettings != nil && realitySettings.PublicKey != "" {
		sni := ""
		if len(realitySettings.ServerNames) > 0 {
			sni = realitySettings.ServerNames[0]
		}
		inboundInfo.RealityConfig = &xray.RealityInfoConfig{
			PublicKey:   realitySettings.PublicKey,
			ShortID:     realitySettings.ShortID,
			ServerName:  sni,
			Fingerprint: realitySettings.Fingerprint,
			SpiderX:     realitySettings.SpiderX,
		}
	}

	// Populate Transport settings
	if transportSettings := inbound.GetTransportSettingsOrDefault(); transportSettings != nil {
		switch inbound.Network {
		case "ws":
			inboundInfo.WSPath = transportSettings.Path
			inboundInfo.WSHost = transportSettings.Host
		case "grpc":
			inboundInfo.GRPCServiceName = transportSettings.ServiceName
		case "xhttp":
			inboundInfo.XHTTPPath = transportSettings.Path
			inboundInfo.XHTTPHost = transportSettings.Host
			inboundInfo.XHTTPMode = transportSettings.Mode
		case "tcp":
			inboundInfo.HeaderType = transportSettings.HeaderType
		}
	}

	// Generate remark
	remark := fmt.Sprintf("%s - %s", inbound.Node.Name, inbound.Remark)

	return xray.GenerateConfigLink(inboundInfo, account.UUID, nodeIP, remark)
}

func (u *accountUsecase) MigrateAccount(ctx context.Context, id uint, targetInboundID uint) error {
	log := logger.GetLogger()

	// 1. Get Account & Source Info
	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}

	// Ensure source info is loaded
	if account.Inbound == nil {
		// Try to fetch it if missing
		if acctWithInbound, err := u.accountRepo.FindByID(ctx, id); err == nil {
			account = acctWithInbound
		}
	}

	sourceInbound := account.Inbound
	var sourceNode *nodeDomain.Node
	if sourceInbound != nil {
		sourceNode = sourceInbound.Node
	}

	// 2. Get Target Info
	targetInbound, err := u.nodeRepo.GetInboundWithNode(ctx, targetInboundID)
	if err != nil {
		return ErrInboundNotFound
	}
	targetNode := targetInbound.Node

	// 3. Validation
	// Check if moving to same inbound (no-op)
	if sourceInbound != nil && sourceInbound.ID == targetInboundID {
		return nil
	}

	log.WithFields(map[string]interface{}{
		"account_id":     id,
		"source_inbound": sourceInbound.ID,
		"target_inbound": targetInboundID,
	}).Info("[MigrateAccount] Migration started")

	// 4. Update Database
	// We update the InboundID. This is the source of truth.
	if err := u.accountRepo.UpdateInbound(ctx, id, targetInboundID); err != nil {
		log.WithError(err).Error("[MigrateAccount] Failed to update DB")
		return err
	}

	// Treat migration as remove + add (xray AddUser targets an inbound tag,
	// so same-node + different-inbound still needs both).
	if sourceNode != nil {
		targetAddr := fmt.Sprintf("%s:%d", sourceNode.IP, sourceNode.APIPort)
		if err := u.provService.EnqueueRemoveUser(ctx, account, targetAddr); err != nil {
			log.WithError(err).Warnf("[MigrateAccount] Failed to enqueue remove for source node %d", sourceNode.ID)
		}
	}

	account.InboundID = targetInboundID
	account.Inbound = targetInbound

	targetAddr := fmt.Sprintf("%s:%d", targetNode.IP, targetNode.APIPort)
	if err := u.provService.EnqueueAddUser(ctx, account, targetAddr); err != nil {
		// DB already updated; account migrated logically. Admin can retry/sync.
		log.WithError(err).Warnf("[MigrateAccount] Failed to enqueue add for target node %d", targetNode.ID)
	}

	log.Info("[MigrateAccount] Account migrated successfully")
	return nil
}

func (u *accountUsecase) UpdateAccount(ctx context.Context, id uint, email, uuid, flow string, dataLimit int64, expiresAt *time.Time, enabled bool) error {
	log := logger.GetLogger()

	account, err := u.accountRepo.FindByID(ctx, id)
	if err != nil {
		return ErrAccountNotFound
	}

	// 0. Validate UUID uniqueness on the same inbound
	if uuid != account.UUID && account.InboundID != 0 {
		exists, checkErr := u.accountRepo.ExistsByUUIDAndInbound(ctx, uuid, account.InboundID, account.ID)
		if checkErr != nil {
			log.WithError(checkErr).Warn("[UpdateAccount] Failed to check UUID uniqueness")
		} else if exists {
			return ErrDuplicateUUID
		}
	}

	// 1. Detect if Xray update is needed
	// Xray update needed if:
	// - Enabled state changes
	// - Currently enabled AND (Email, UUID, or Flow changes)
	needsReProvision := false
	needsRemove := false
	needsAdd := false

	// Status change
	if account.Status == domain.AccountStatusActive && !enabled {
		needsRemove = true
	} else if account.Status == domain.AccountStatusDisabled && enabled {
		needsAdd = true
	} else if enabled {
		// Enabled -> Enabled, check fields
		if account.Email != email || account.UUID != uuid || account.Flow != flow {
			needsReProvision = true
		}
	}

	// 2. Prepare Xray Target
	var target string
	if account.Inbound != nil && account.Inbound.Node != nil {
		target = fmt.Sprintf("%s:%d", account.Inbound.Node.IP, account.Inbound.Node.APIPort)
	} else {
		// Try to reload to be sure
		if acctWithInbound, err := u.accountRepo.FindByID(ctx, id); err == nil && acctWithInbound.Inbound != nil && acctWithInbound.Inbound.Node != nil {
			account = acctWithInbound
			target = fmt.Sprintf("%s:%d", account.Inbound.Node.IP, account.Inbound.Node.APIPort)
		}
	}

	// Capture old values for removal if needed
	oldAccountConfig := *account

	// 3. Update DB Struct
	account.Email = email
	account.UUID = uuid
	account.Flow = flow
	account.DataLimit = dataLimit
	account.ExpiresAt = expiresAt
	if enabled {
		account.Status = domain.AccountStatusActive
	} else {
		account.Status = domain.AccountStatusDisabled
	}

	if err := u.accountRepo.Update(ctx, account); err != nil {
		return err
	}

	// 4. Execute Xray changes
	if target != "" {
		if needsRemove {
			if err := u.provService.EnqueueRemoveUser(ctx, &oldAccountConfig, target); err != nil {
				log.WithError(err).Warnf("Failed to enqueue remove for account %s", oldAccountConfig.Email)
			}
		}

		if needsReProvision {
			// Remove old first
			if err := u.provService.EnqueueRemoveUser(ctx, &oldAccountConfig, target); err != nil {
				log.WithError(err).Warnf("Failed to enqueue remove (reprovision) for account %s", oldAccountConfig.Email)
			}
			// Add new
			if err := u.provService.EnqueueAddUser(ctx, account, target); err != nil {
				log.WithError(err).Warnf("Failed to enqueue add (reprovision) for account %s", account.Email)
			}
		} else if needsAdd {
			if err := u.provService.EnqueueAddUser(ctx, account, target); err != nil {
				log.WithError(err).Warnf("Failed to enqueue add for account %s", account.Email)
			}
		}
	}

	return nil
}

// CheckAndDisableExhaustedAccounts finds active accounts that have exceeded their per-inbound
// data limit and disables them. Returns the number of accounts disabled.
func (u *accountUsecase) CheckAndDisableExhaustedAccounts(ctx context.Context) (int, error) {
	log := logger.GetLogger()

	exhausted, err := u.accountRepo.ListDataExhausted(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list exhausted accounts: %w", err)
	}

	if len(exhausted) == 0 {
		return 0, nil
	}

	disabled := 0
	for _, acc := range exhausted {
		if err := u.DisableAccount(ctx, acc.ID); err != nil {
			log.WithError(err).Warnf("Failed to disable exhausted account %d (%s)", acc.ID, acc.Email)
			continue
		}
		disabled++
		log.WithFields(map[string]interface{}{
			"account_id": acc.ID,
			"email":      acc.Email,
			"data_used":  acc.DataUsed,
			"data_limit": acc.DataLimit,
			"inbound_id": acc.InboundID,
		}).Info("[CheckAndDisableExhaustedAccounts] Account disabled due to per-inbound data exhaustion")
	}

	return disabled, nil
}

// ClearAccountDataLimitsBySubscription sets data_limit = 0 for all accounts in a subscription.
// This is called when a subscription's custom data limit is set, so the subscription-level
// enforcement becomes the single source of truth and account-level checks don't conflict.
func (u *accountUsecase) ClearAccountDataLimitsBySubscription(ctx context.Context, subID uint) error {
	return u.accountRepo.UpdateDataLimitBySubscriptionID(ctx, subID, 0)
}

// ResetAccountDataUsedBySubscription resets data_used to 0 for all accounts in a subscription.
// Called on renewal so account-level usage stays in sync with subscription-level reset.
func (u *accountUsecase) ResetAccountDataUsedBySubscription(ctx context.Context, subID uint) error {
	return u.accountRepo.ResetDataUsedBySubscriptionID(ctx, subID)
}

// SetAccountsUUIDBySubscription updates every account under the sub via
// UpdateAccount (xray sync + uniqueness + provisioning). Partial-failure
// safe: returns count of successes; xray-side ops can't roll back.
func (u *accountUsecase) SetAccountsUUIDBySubscription(ctx context.Context, subID uint, newUUID string) (int, error) {
	log := logger.GetLogger()
	accounts, err := u.ListAccountsBySubscription(ctx, subID)
	if err != nil {
		return 0, fmt.Errorf("list accounts: %w", err)
	}
	if len(accounts) == 0 {
		return 0, nil
	}

	updated := 0
	var firstErr error
	for _, acc := range accounts {
		if acc.UUID == newUUID {
			updated++
			continue
		}
		enabled := acc.Status == domain.AccountStatusActive
		if err := u.UpdateAccount(ctx, acc.ID, acc.Email, newUUID, acc.Flow, acc.DataLimit, acc.ExpiresAt, enabled); err != nil {
			log.WithError(err).WithField("account_id", acc.ID).WithField("subscription_id", subID).
				Warn("[SetAccountsUUIDBySubscription] failed to update account uuid")
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		updated++
	}
	return updated, firstErr
}
