package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lilendian0x00/xray-knife/v11/pkg/core"
	"github.com/lilendian0x00/xray-knife/v11/pkg/subscription"
	d "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray/sharelink"
	"github.com/xtls/xray-core/infra/conf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"sync"
	"time"
)

func (s *OutboundPoolService) Run(ctx context.Context, nodeID, poolID uint, refresh bool) (*d.OutboundPoolRun, error) {
	run := d.OutboundPoolRun{PoolID: poolID, State: "queued", Refresh: refresh, StartedAt: time.Now(), LeaseUntil: time.Now().Add(2 * time.Minute)}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p d.OutboundPool
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; e != nil {
			return e
		}
		if p.ActiveRunID != 0 {
			return ErrPoolBusy
		}
		run.Revision = p.Revision
		if e := tx.Create(&run).Error; e != nil {
			return e
		}
		updates := map[string]any{"active_run_id": run.ID, "last_run_id": run.ID, "next_scan_at": time.Now().Add(time.Duration(p.RescanSeconds) * time.Second)}
		if refresh {
			updates["next_refresh_at"] = time.Now().Add(time.Duration(p.RefreshSeconds) * time.Second)
		}
		claim := tx.Model(&p).Where("active_run_id = 0").Updates(updates)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrPoolBusy
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	jobRun := run
	jobCtx, cancel := context.WithTimeout(s.ctx, 24*time.Hour)
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		cancel()
		s.db.Model(&d.OutboundPool{}).Where("id = ? AND active_run_id = ?", poolID, run.ID).Update("active_run_id", 0)
		s.db.Model(&d.OutboundPoolRun{}).Where("id = ?", run.ID).Updates(map[string]any{"state": "cancelled", "finished_at": time.Now()})
		return nil, s.ctx.Err()
	}
	s.running[poolID] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		defer cancel()
		defer func() { s.mu.Lock(); delete(s.running, poolID); s.mu.Unlock() }()
		leaseDone := make(chan struct{})
		defer close(leaseDone)
		go func() {
			tick := time.NewTicker(30 * time.Second)
			defer tick.Stop()
			for {
				select {
				case <-leaseDone:
					return
				case <-jobCtx.Done():
					return
				case <-tick.C:
					r := s.db.WithContext(jobCtx).Model(&d.OutboundPoolRun{}).Where("id = ? AND state <> ?", run.ID, "cancelling").Update("lease_until", time.Now().Add(2*time.Minute))
					if r.Error != nil || r.RowsAffected != 1 {
						cancel()
						return
					}
				}
			}
		}()
		e := s.execute(jobCtx, nodeID, &jobRun)
		end := time.Now()
		state := "completed"
		message := ""
		if e != nil {
			state = "failed"
			message = e.Error()
			if errors.Is(e, context.Canceled) {
				state = "cancelled"
			}
			if errors.Is(e, context.DeadlineExceeded) {
				state = "timed_out"
			}
		}
		writeCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		s.db.WithContext(writeCtx).Transaction(func(tx *gorm.DB) error {
			if e := tx.Model(&d.OutboundPoolRun{}).Where("id = ?", run.ID).Updates(map[string]any{"state": state, "error": message, "finished_at": end}).Error; e != nil {
				return e
			}
			return tx.Model(&d.OutboundPool{}).Where("id = ? AND active_run_id = ?", poolID, run.ID).Update("active_run_id", 0).Error
		})
	}()
	return &run, nil
}

func (s *OutboundPoolService) Cancel(ctx context.Context, nodeID, poolID uint) error {
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).First(&p, "id = ? AND node_id = ?", poolID, nodeID).Error; err != nil {
		return err
	}
	if p.ActiveRunID == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Model(&d.OutboundPoolRun{}).Where("id = ?", p.ActiveRunID).Update("state", "cancelling").Error; err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.running[poolID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *OutboundPoolService) recoverRuns() error {
	var runs []d.OutboundPoolRun
	if err := s.db.WithContext(s.ctx).Where("finished_at IS NULL AND lease_until < ?", time.Now()).Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		var p d.OutboundPool
		if err := s.db.WithContext(s.ctx).First(&p, run.PoolID).Error; err != nil {
			continue
		}
		if run.State == "applying" {
			node, e := s.nodes.nodeRepo.GetNode(s.ctx, p.NodeID)
			if e != nil || node.XrayStopped {
				continue
			}
			ctx, cancel := context.WithTimeout(s.ctx, 45*time.Second)
			e = s.nodes.pushConfigToAgent(ctx, node)
			cancel()
			if e != nil {
				continue
			}
		}
		if err := s.db.WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
			claim := tx.Model(&d.OutboundPoolRun{}).Where("id = ? AND lease_until < ?", run.ID, time.Now()).Updates(map[string]any{"state": "interrupted", "error": "Run interrupted; saved scan results are retained", "finished_at": time.Now()})
			if claim.Error != nil {
				return claim.Error
			}
			if claim.RowsAffected == 0 {
				return nil
			}
			return tx.Model(&d.OutboundPool{}).Where("id = ? AND active_run_id = ?", run.PoolID, run.ID).Update("active_run_id", 0).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *OutboundPoolService) execute(ctx context.Context, nodeID uint, run *d.OutboundPoolRun) error {
	var p d.OutboundPool
	if err := s.db.WithContext(ctx).Preload("Sources").First(&p, run.PoolID).Error; err != nil {
		return err
	}
	c, pc, err := s.client(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("node unavailable: %w", err)
	}
	defer closeAgentClient(c)
	if err = s.runState(ctx, run, "fetching"); err != nil {
		return err
	}
	for i := range p.Sources {
		source := &p.Sources[i]
		if source.FetchedAt != nil && !run.Refresh {
			continue
		}
		var links []string
		var fetchErr error
		if source.Kind == "links" {
			links, fetchErr = sharelink.DecodeSubscription([]byte(source.Links), subscription.DecodeOptions{MaxBytes: p.MaxSourceBytes, MaxLinks: p.MaxSourceLinks})
		} else {
			headers := map[string]string{}
			if source.UserAgent != "" {
				headers["User-Agent"] = source.UserAgent
			}
			if source.ETag != "" {
				headers["If-None-Match"] = source.ETag
			}
			if source.LastModified != "" {
				headers["If-Modified-Since"] = source.LastModified
			}
			reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			result, e := pc.FetchPoolSource(reqCtx, &pb.PoolSourceRequest{Url: source.URL, Headers: headers, MaxBytes: p.MaxSourceBytes, MaxLinks: int32(p.MaxSourceLinks), AllowPrivate: source.AllowPrivate})
			cancel()
			fetchErr = e
			if e == nil {
				if result.NotModified {
					links = []string(source.Snapshot)
				} else {
					links = result.Links
				}
				source.ETag = result.Etag
				source.LastModified = result.LastModified
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if fetchErr != nil {
			source.LastError = poolRPCError(fetchErr)
		} else {
			now := time.Now()
			source.Snapshot = links
			source.LinkCount = len(links)
			source.FetchedAt = &now
			source.LastError = ""
		}
		if err = s.db.WithContext(ctx).Save(source).Error; err != nil {
			return err
		}
	}
	if run.Refresh {
		if err = s.db.WithContext(ctx).Model(&p).Update("next_refresh_at", time.Now().Add(time.Duration(p.RefreshSeconds)*time.Second)).Error; err != nil {
			return err
		}
	}
	candidates, err := s.reconcileCandidates(ctx, &p, run)
	if err != nil {
		return err
	}
	if err = s.runState(ctx, run, "scanning"); err != nil {
		return err
	}
	if err = s.scan(ctx, pc, &p, run, candidates); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.publish(ctx, &p, run)
}
func poolRPCError(err error) string {
	if status.Code(err) == codes.Unimplemented || strings.Contains(err.Error(), "Unimplemented") {
		return "Update the node agent to enable pool fetching and scanning"
	}
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return "Node request timed out"
	}
	if parsed, ok := status.FromError(err); ok {
		return parsed.Message()
	}
	return "Node request failed: " + status.Code(err).String()
}
func (s *OutboundPoolService) runState(ctx context.Context, r *d.OutboundPoolRun, state string) error {
	r.State = state
	return s.db.WithContext(ctx).Model(&d.OutboundPoolRun{}).Where("id = ?", r.ID).Updates(map[string]any{"state": state, "raw_count": r.RawCount, "unique_count": r.UniqueCount, "duplicate_count": r.DuplicateCount, "tested": r.Tested, "healthy": r.Healthy, "active": r.Active}).Error
}

func (s *OutboundPoolService) reconcileCandidates(ctx context.Context, p *d.OutboundPool, r *d.OutboundPoolRun) ([]d.OutboundPoolCandidate, error) {
	c := core.NewAutomaticCore(false, false)
	unique := map[string]*d.OutboundPoolCandidate{}
	order := []string{}
	successful := 0
	for _, source := range p.Sources {
		if source.FetchedAt == nil {
			continue
		}
		successful++
		for _, raw := range source.Snapshot {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			r.RawCount++
			fingerprint, err := core.ConnectionFingerprint(c, raw)
			candidateStatus := "untested"
			problem := ""
			protocolName := ""
			address := ""
			remark := ""
			if err != nil {
				sum := sha256.Sum256([]byte(raw))
				fingerprint = "raw:" + hex.EncodeToString(sum[:])
				candidateStatus = "unsupported"
				problem = "Unsupported or malformed share link"
			} else {
				proto, e := c.CreateProtocol(raw)
				if e == nil && proto.Parse() == nil {
					info := proto.ConvertToGeneralConfig()
					protocolName = info.Protocol
					address = strings.ReplaceAll(info.Address, "\x00", "")
					remark = strings.ReplaceAll(info.Remark, "\x00", "")
				}
			}
			if existing := unique[fingerprint]; existing != nil {
				r.DuplicateCount++
				found := false
				for _, id := range existing.SourceIDs {
					if id == source.ID {
						found = true
					}
				}
				if !found {
					existing.SourceIDs = append(existing.SourceIDs, source.ID)
				}
				continue
			}
			if len(unique) >= p.MaxCandidates {
				return nil, fmt.Errorf("unique candidate count exceeds %d; raise the pool limit", p.MaxCandidates)
			}
			unique[fingerprint] = &d.OutboundPoolCandidate{PoolID: p.ID, Fingerprint: fingerprint, Link: raw, Remark: remark, Protocol: protocolName, Address: address, SourceIDs: []uint{source.ID}, Present: true, SeenAt: time.Now(), Status: candidateStatus, LastError: problem}
			order = append(order, fingerprint)
		}
	}
	if successful == 0 {
		return nil, fmt.Errorf("no successful source snapshot; existing members are preserved")
	}
	r.UniqueCount = len(unique)
	candidates := make([]d.OutboundPoolCandidate, 0, len(unique))
	for _, key := range order {
		candidates = append(candidates, *unique[key])
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&d.OutboundPoolCandidate{}).Where("pool_id = ?", p.ID).Update("present", false).Error; e != nil {
			return e
		}
		if len(candidates) == 0 {
			return nil
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "pool_id"}, {Name: "fingerprint"}}, DoUpdates: clause.AssignmentColumns([]string{"link", "remark", "protocol", "address", "source_ids", "present", "seen_at"})}).CreateInBatches(&candidates, 200).Error
	})
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Where("pool_id = ? AND present = ? AND excluded = ? AND status <> ?", p.ID, true, false, "unsupported").Find(&candidates).Error
	return candidates, err
}

func (s *OutboundPoolService) scan(ctx context.Context, client agent.PoolClient, p *d.OutboundPool, r *d.OutboundPoolRun, candidates []d.OutboundPoolCandidate) error {
	var active []d.Outbound
	if err := s.db.WithContext(ctx).Where("managed_pool_id = ? AND is_disabled = ?", p.ID, false).Find(&active).Error; err != nil {
		return err
	}
	activeTags := map[string]bool{}
	for _, x := range active {
		activeTags[x.Tag] = true
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ai, aj := activeTags[poolTag(p.ID, candidates[i].Fingerprint)], activeTags[poolTag(p.ID, candidates[j].Fingerprint)]
		if ai != aj {
			return ai
		}
		a, b := candidates[i].TestedAt, candidates[j].TestedAt
		if a == nil {
			return b != nil
		}
		if b == nil {
			return false
		}
		return a.Before(*b)
	})
	type outcome struct {
		candidate d.OutboundPoolCandidate
		response  *pb.PoolCandidateResponse
		err       error
	}
	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	work := make(chan d.OutboundPoolCandidate)
	results := make(chan outcome, p.Concurrency)
	threads := d.MaxPoolScanThreads
	if p.NodeID == s.nodes.LocalNodeID() {
		threads = d.LocalPoolScanThreads
	}
	slotsAny, _ := s.nodeSlots.LoadOrStore(p.NodeID, make(chan struct{}, threads))
	slots := slotsAny.(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < p.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range work {
				select {
				case slots <- struct{}{}:
				case <-scanCtx.Done():
					return
				}
				reqCtx, stop := context.WithTimeout(scanCtx, 120*time.Second)
				response, err := client.TestPoolCandidate(reqCtx, &pb.PoolCandidateRequest{Link: candidate.Link, TestUrls: []string(p.TestURLs), MaxDelayMs: int32(p.MaxDelayMs), Retries: int32(p.Retries)})
				stop()
				<-slots
				select {
				case results <- outcome{candidate, response, err}:
				case <-scanCtx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(work)
		for _, candidate := range candidates {
			select {
			case work <- candidate:
			case <-scanCtx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	retained := 0
	relevantActive := 0
	for _, candidate := range candidates {
		if activeTags[poolTag(p.ID, candidate.Fingerprint)] {
			relevantActive++
		}
	}
	activeTested := 0
	for result := range results {
		if result.err != nil {
			cancel()
			for range results {
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("scan infrastructure failed: %s; existing members preserved", poolRPCError(result.err))
		}
		x := result.candidate
		response := result.response
		// Older agents pass members the node's Xray refuses to load, and one
		// such member fails the whole apply.
		if response.Healthy {
			var out conf.OutboundDetourConfig
			if e := json.Unmarshal([]byte(response.OutboundJson), &out); e != nil {
				response = &pb.PoolCandidateResponse{Status: "failed", Error: "Agent returned an invalid outbound"}
			} else if e = sharelink.RequireTransportSecurity(&out); e != nil {
				response = &pb.PoolCandidateResponse{Status: "unsupported", Error: e.Error()}
			}
		}
		now := time.Now()
		x.TestedAt = &now
		x.LatencyMs = response.LatencyMs
		x.Status = response.Status
		x.LastError = response.Error
		if response.Healthy {
			x.Failures = 0
			x.OutboundJSON = response.OutboundJson
			r.Healthy++
		} else {
			x.Failures++
		}
		if err := s.db.WithContext(ctx).Model(&d.OutboundPoolCandidate{}).Where("id = ?", x.ID).Updates(map[string]any{"tested_at": now, "latency_ms": x.LatencyMs, "status": x.Status, "last_error": x.LastError, "failures": x.Failures, "outbound_json": x.OutboundJSON}).Error; err != nil {
			cancel()
			for range results {
			}
			return err
		}
		r.Tested++
		isActive := activeTags[poolTag(p.ID, x.Fingerprint)]
		if isActive {
			activeTested++
		}
		if response.Healthy || (isActive && x.Failures < p.FailureThreshold && x.OutboundJSON != "") {
			retained++
		}
		if err := s.runState(ctx, r, "scanning"); err != nil {
			cancel()
			for range results {
			}
			return err
		}
		if p.StopWhenFull && retained >= p.MaxActive && activeTested >= relevantActive {
			cancel()
			for range results {
			}
			break
		}
	}
	return ctx.Err()
}
