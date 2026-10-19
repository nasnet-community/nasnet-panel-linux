package usecase

import (
	"context"
	"errors"
	"fmt"
	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrPoolBusy = errors.New("stop the active pool run before editing")
var ErrPoolInvalid = errors.New("invalid pool settings")

type OutboundPoolService struct {
	apply     func(context.Context, *d.Node) error
	db        *gorm.DB
	nodes     *nodeUsecase
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	running   map[uint]context.CancelFunc
	wg        sync.WaitGroup
	nodeSlots sync.Map
}

type PoolInput struct {
	d.OutboundPool
	BalancerTag string `json:"balancer_tag"`
	Strategy    string `json:"strategy"`
	FallbackTag string `json:"fallback_tag"`
}

type PoolView struct {
	d.OutboundPool
	Balancer       *d.BalancingRule   `json:"balancer"`
	LastRun        *d.OutboundPoolRun `json:"last_run,omitempty"`
	CandidateCount int64              `json:"candidate_count"`
	ActiveCount    int64              `json:"active_count"`
}

func NewOutboundPoolService(db *gorm.DB, nodes NodeUsecase) *OutboundPoolService {
	ctx, cancel := context.WithCancel(context.Background())
	s := &OutboundPoolService{db: db, nodes: nodes.(*nodeUsecase), ctx: ctx, cancel: cancel, running: map[uint]context.CancelFunc{}}
	s.apply = s.nodes.pushConfigToAgentUnlocked
	s.nodes.pools = s
	return s
}

func (s *OutboundPoolService) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		s.schedule()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-tick.C:
				s.schedule()
			}
		}
	}()
}
func (s *OutboundPoolService) Stop() { s.mu.Lock(); s.cancel(); s.mu.Unlock(); s.wg.Wait() }

func (s *OutboundPoolService) List(ctx context.Context, nodeID uint) ([]PoolView, error) {
	var pools []d.OutboundPool
	if err := s.db.WithContext(ctx).Where("node_id = ?", nodeID).Preload("Sources", func(tx *gorm.DB) *gorm.DB { return tx.Omit("snapshot").Order("id") }).Order("id").Find(&pools).Error; err != nil {
		return nil, err
	}
	views := make([]PoolView, 0, len(pools))
	for _, p := range pools {
		v := PoolView{OutboundPool: p}
		bal, err := s.nodes.nodeRepo.GetBalancingRule(ctx, p.BalancerID)
		if err != nil {
			return nil, err
		}
		v.Balancer = bal
		if p.LastRunID != 0 {
			var r d.OutboundPoolRun
			if err := s.db.WithContext(ctx).First(&r, p.LastRunID).Error; err == nil {
				v.LastRun = &r
			}
		}
		if err := s.db.WithContext(ctx).Model(&d.OutboundPoolCandidate{}).Where("pool_id = ? AND present = ?", p.ID, true).Count(&v.CandidateCount).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&d.Outbound{}).Where("managed_pool_id = ? AND is_disabled = ?", p.ID, false).Count(&v.ActiveCount).Error; err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *OutboundPoolService) Save(ctx context.Context, nodeID, id uint, input PoolInput) (*d.OutboundPool, error) {
	p := input.OutboundPool
	p.ID = id
	p.NodeID = nodeID
	p.Name = strings.TrimSpace(p.Name)
	node, err := s.nodes.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if p.Concurrency == 0 && node.IsLocal() {
		p.Concurrency = d.LocalPoolDefaultConcurrency
	}
	if err := validatePool(&p); err != nil {
		return nil, err
	}
	if limit := d.PoolScanThreads(node); p.Concurrency > limit {
		return nil, fmt.Errorf("%w: concurrency on this node is 1–%d", ErrPoolInvalid, limit)
	}
	lock := s.nodes.getOrCreatePushState(nodeID)
	lock.applyMu.Lock()
	defer lock.applyMu.Unlock()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p.LastRunID = 0
		p.AppliedRevision = 0
		p.CreatedAt = time.Time{}
		var old d.OutboundPool
		if id != 0 {
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&old, "id = ? AND node_id = ?", id, nodeID).Error; e != nil {
				return e
			}
			if old.ActiveRunID != 0 {
				return ErrPoolBusy
			}
			p.BalancerID = old.BalancerID
			p.CreatedAt = old.CreatedAt
			p.LastRunID = old.LastRunID
			p.AppliedRevision = old.AppliedRevision
		}
		var bal d.BalancingRule
		if p.BalancerID != 0 {
			if e := tx.First(&bal, "id = ? AND node_id = ?", p.BalancerID, nodeID).Error; e != nil {
				return fmt.Errorf("%w: choose a balancer on this node", ErrPoolInvalid)
			}
			if !bal.Enabled {
				return fmt.Errorf("%w: balancer must be enabled", ErrPoolInvalid)
			}
		} else {
			tag := strings.TrimSpace(input.BalancerTag)
			if tag == "" || len(tag) > 100 || !d.ValidBalancingStrategies[input.Strategy] {
				return fmt.Errorf("%w: valid balancer tag and strategy are required", ErrPoolInvalid)
			}
			var n int64
			if e := tx.Model(&d.BalancingRule{}).Where("node_id = ? AND tag = ?", nodeID, tag).Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				return fmt.Errorf("%w: balancer tag already exists", ErrPoolInvalid)
			}
			bal = d.BalancingRule{NodeID: nodeID, Tag: tag, Strategy: input.Strategy, Enabled: true}
		}
		var owners int64
		if e := tx.Model(&d.OutboundPool{}).Where("balancer_id = ? AND id <> ?", bal.ID, id).Count(&owners).Error; e != nil {
			return e
		}

		fallback := input.FallbackTag
		if fallback == "" {
			fallback = bal.FallbackTag
		}
		if e := poolFallback(tx, nodeID, fallback); e != nil {
			return e
		}
		if (id != 0 || owners > 0) && fallback != bal.FallbackTag {
			return fmt.Errorf("%w: change the fallback in the balancer settings", ErrPoolInvalid)
		}
		bal.FallbackTag = fallback
		if bal.ID == 0 {
			if e := tx.Create(&bal).Error; e != nil {
				return e
			}
		} else if id == 0 {
			if e := tx.Model(&bal).Update("fallback_tag", fallback).Error; e != nil {
				return e
			}
		}
		p.BalancerID = bal.ID
		p.Revision = old.Revision + 1
		p.ActiveRunID = 0
		p.NextRefreshAt = time.Now()
		p.NextScanAt = time.Now()
		sources := p.Sources
		p.Sources = nil
		if e := tx.Omit("Sources").Save(&p).Error; e != nil {
			return e
		}

		prefix := poolPrefix(p.ID)
		var allBals []d.BalancingRule
		if e := tx.Where("node_id = ?", nodeID).Find(&allBals).Error; e != nil {
			return e
		}
		for _, other := range allBals {
			for _, selector := range other.OutboundSelectors {
				if strings.HasPrefix(prefix, selector) && !(other.ID == bal.ID && selector == prefix) {
					return fmt.Errorf("%w: balancer selector %q overlaps the pool", ErrPoolInvalid, selector)
				}
			}
		}
		if !bal.OutboundSelectors.Contains(prefix) {
			bal.OutboundSelectors = append(bal.OutboundSelectors, prefix)
			if e := tx.Model(&bal).Update("outbound_selectors", bal.OutboundSelectors).Error; e != nil {
				return e
			}
		}
		var previous []d.OutboundPoolSource
		if e := tx.Where("pool_id = ?", p.ID).Find(&previous).Error; e != nil {
			return e
		}
		previousByID := map[uint]d.OutboundPoolSource{}
		for _, x := range previous {
			previousByID[x.ID] = x
		}
		keep := []uint{}
		for i := range sources {
			x := &sources[i]
			oldSource, exists := previousByID[x.ID]
			if x.ID != 0 && !exists {
				return fmt.Errorf("%w: source does not belong to this pool", ErrPoolInvalid)
			}
			x.PoolID = p.ID
			if exists && oldSource.Kind == x.Kind && oldSource.URL == x.URL && oldSource.Links == x.Links && oldSource.UserAgent == x.UserAgent && oldSource.AllowPrivate == x.AllowPrivate {
				x.Snapshot = oldSource.Snapshot
				x.FetchedAt = oldSource.FetchedAt
				x.LastError = oldSource.LastError
				x.ETag = oldSource.ETag
				x.LastModified = oldSource.LastModified
				x.LinkCount = oldSource.LinkCount
			} else {
				x.Snapshot = nil
				x.FetchedAt = nil
				x.ETag = ""
				x.LastModified = ""
				x.LastError = ""
				x.LinkCount = 0
			}
			if e := tx.Save(x).Error; e != nil {
				return e
			}
			keep = append(keep, x.ID)
		}
		if e := tx.Where("pool_id = ? AND id NOT IN ?", p.ID, keep).Delete(&d.OutboundPoolSource{}).Error; e != nil {
			return e
		}
		p.Sources = sources
		return nil
	})
	return &p, err
}

func validatePool(p *d.OutboundPool) error {
	invalid := func(msg string) error { return fmt.Errorf("%w: %s", ErrPoolInvalid, msg) }
	if p.Name == "" || len(p.Name) > 100 || strings.ContainsRune(p.Name, 0) {
		return invalid("name is required (max 100 characters)")
	}
	if p.MaxActive < 1 || p.MaxActive > 1000 || p.Concurrency < 1 || p.Concurrency > d.MaxPoolScanThreads {
		return invalid("active limit must be 1–1000 and concurrency 1–2000")
	}
	if p.RefreshSeconds < 60 || p.RescanSeconds < 60 || p.RefreshSeconds > 604800 || p.RescanSeconds > 604800 {
		return invalid("intervals must be between one minute and seven days")
	}
	if p.MaxDelayMs < 100 || p.MaxDelayMs > 30000 || p.Retries < 0 || p.Retries > 3 || p.FailureThreshold < 1 || p.FailureThreshold > 10 {
		return invalid("invalid probe limits")
	}
	if p.MaxSourceLinks < 1 || p.MaxSourceLinks > 100000 || p.MaxCandidates < p.MaxActive || p.MaxCandidates > 100000 || p.MaxSourceBytes < 1024 || p.MaxSourceBytes > 64<<20 {
		return invalid("source and candidate limits exceed supported bounds")
	}
	if len(p.TestURLs) < 1 || len(p.TestURLs) > 3 {
		return invalid("choose one to three HTTPS probe URLs")
	}
	for _, raw := range p.TestURLs {
		u, e := url.Parse(raw)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" {
			return invalid("probe URLs must use HTTPS")
		}
	}
	if len(p.Sources) < 1 || len(p.Sources) > 20 {
		return invalid("choose one to twenty sources")
	}
	total := 0
	ids := map[uint]bool{}
	for _, x := range p.Sources {
		if x.ID != 0 {
			if ids[x.ID] {
				return invalid("source IDs must be unique")
			}
			ids[x.ID] = true
		}
		total += len(x.Links)
		if len(x.Name) > 100 || len(x.UserAgent) > 512 || strings.ContainsRune(x.Name, 0) || strings.ContainsAny(x.UserAgent, "\x00\r\n") {
			return invalid("source name or User-Agent is too long")
		}
		if x.Kind == "url" {
			u, e := url.Parse(x.URL)
			if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || len(x.URL) > 8192 {
				return invalid("source URL must use HTTP or HTTPS")
			}
		} else if x.Kind != "links" || strings.TrimSpace(x.Links) == "" {
			return invalid("source must be a URL or a pasted link list")
		}
	}
	if total > 64<<20 {
		return invalid("pasted input exceeds 64 MiB")
	}
	return nil
}

func poolFallback(tx *gorm.DB, nodeID uint, tag string) error {
	// A reverse tunnel portalled here exists only at runtime.
	if d.IsReverseTag(tag) {
		var tunnels int64
		if err := tx.Model(&d.ReverseTunnel{}).Where("portal_node_id = ? AND portal_tag = ?", nodeID, tag).Count(&tunnels).Error; err != nil {
			return err
		}
		if tunnels > 0 {
			return nil
		}
	}
	var outbounds []d.Outbound
	if err := tx.Where("node_id = ?", nodeID).Find(&outbounds).Error; err != nil {
		return err
	}
	byTag := map[string]d.Outbound{}
	for _, out := range outbounds {
		byTag[out.Tag] = out
	}
	visiting := map[string]bool{}
	var visit func(string) error
	visit = func(current string) error {
		out, exists := byTag[current]
		if !exists || out.IsDisabled || out.ManagedPoolID != nil {
			return fmt.Errorf("%w: fallback and its dependencies must be enabled, manually managed outbounds", ErrPoolInvalid)
		}
		if visiting[current] {
			return fmt.Errorf("%w: fallback has a dependency cycle", ErrPoolInvalid)
		}
		visiting[current] = true
		defer delete(visiting, current)
		if next := out.DialerProxyTag(); next != "" {
			if err := visit(next); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(tag)
}

func (s *OutboundPoolService) Candidates(ctx context.Context, nodeID, poolID uint, offset, limit int, filters ...string) ([]d.OutboundPoolCandidate, int64, error) {
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; err != nil {
		return nil, 0, err
	}
	search := ""
	if len(filters) > 0 {
		search = filters[0]
	}
	query := s.candidateQuery(ctx, poolID, search)
	if len(filters) > 1 {
		// Status filters look at configs still in the sources; "missing" is where
		// the rest live, and "excluded" spans both.
		switch filters[1] {
		case "live":
			query = query.Where("present = ?", true)
		case "excluded":
			query = query.Where("excluded = ?", true)
		case "missing":
			query = query.Where("present = ?", false)
		case "failed":
			query = query.Where("present = ? AND status IN ?", true, failedCandidateStatuses)
		case "passed", "untested", "unsupported":
			query = query.Where("present = ? AND status = ?", true, filters[1])
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var candidates []d.OutboundPoolCandidate
	err := query.Order("present DESC").
		Order("CASE WHEN status = 'passed' THEN 0 ELSE 1 END").
		Order("CASE WHEN latency_ms > 0 THEN latency_ms ELSE 2147483647 END").
		Order("id").
		Offset(offset).Limit(limit).Find(&candidates).Error
	return candidates, total, err
}

var failedCandidateStatuses = []string{"failed", "timeout", "broken"}

func (s *OutboundPoolService) candidateQuery(ctx context.Context, poolID uint, search string) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&d.OutboundPoolCandidate{}).Where("pool_id = ?", poolID)
	if q := strings.ToLower(strings.TrimSpace(search)); q != "" {
		q = "%" + q + "%"
		query = query.Where("(LOWER(remark) LIKE ? OR LOWER(address) LIKE ? OR LOWER(protocol) LIKE ?)", q, q, q)
	}
	return query
}

// CandidateCounts sizes each Candidates status filter for the same search, in one scan.
func (s *OutboundPoolService) CandidateCounts(ctx context.Context, nodeID, poolID uint, search string) (map[string]int64, error) {
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; err != nil {
		return nil, err
	}
	var rows []struct {
		Present  bool
		Excluded bool
		Status   string
		N        int64
	}
	err := s.candidateQuery(ctx, poolID, search).
		Select("present, excluded, status, COUNT(*) AS n").
		Group("present, excluded, status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{"live": 0, "passed": 0, "failed": 0, "untested": 0, "unsupported": 0, "excluded": 0, "missing": 0}
	for _, r := range rows {
		if r.Excluded {
			counts["excluded"] += r.N
		}
		if !r.Present {
			counts["missing"] += r.N
			continue
		}
		counts["live"] += r.N
		switch {
		case r.Status == "passed" || r.Status == "untested" || r.Status == "unsupported":
			counts[r.Status] += r.N
		case slices.Contains(failedCandidateStatuses, r.Status):
			counts["failed"] += r.N
		}
	}
	return counts, nil
}
func (s *OutboundPoolService) Runs(ctx context.Context, nodeID, poolID uint) ([]d.OutboundPoolRun, error) {
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; err != nil {
		return nil, err
	}
	var runs []d.OutboundPoolRun
	err := s.db.WithContext(ctx).Where("pool_id = ?", poolID).Order("id DESC").Limit(20).Find(&runs).Error
	return runs, err
}
func (s *OutboundPoolService) Exclude(ctx context.Context, nodeID, poolID, candidateID uint, excluded bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p d.OutboundPool
		if e := tx.First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; e != nil {
			return e
		}
		if p.ActiveRunID != 0 {
			return ErrPoolBusy
		}
		result := tx.Model(&d.OutboundPoolCandidate{}).Where("id = ? AND pool_id = ?", candidateID, poolID).Update("excluded", excluded)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&p).Updates(map[string]any{"revision": gorm.Expr("revision + 1"), "next_scan_at": time.Now()}).Error
	})
}

func (s *OutboundPoolService) client(ctx context.Context, nodeID uint) (agent.NodeClient, agent.PoolClient, error) {
	node, err := s.nodes.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, nil, err
	}
	if node.XrayStopped {
		return nil, nil, fmt.Errorf("Xray is explicitly stopped; resume it before scanning")
	}
	// A scan owns its connection; cache rotation must not interrupt active tests.
	c, err := s.nodes.getAgentClientUnpooled(node)
	if err != nil {
		return nil, nil, err
	}
	pc, ok := c.(agent.PoolClient)
	if !ok {
		closeAgentClient(c)
		return nil, nil, fmt.Errorf("update the node agent to enable pools")
	}
	return c, pc, nil
}

func (s *OutboundPoolService) schedule() {
	if err := s.recoverRuns(); err != nil {
		logger.GetLogger().WithError(err).Warn("Pool recovery failed")
	}
	var pools []d.OutboundPool
	if err := s.db.WithContext(s.ctx).Where("enabled = ? AND active_run_id = 0 AND (next_scan_at <= ? OR next_refresh_at <= ?)", true, time.Now(), time.Now()).Find(&pools).Error; err != nil {
		return
	}
	for _, p := range pools {
		_, _ = s.Run(s.ctx, p.NodeID, p.ID, !p.NextRefreshAt.After(time.Now()))
	}
}
