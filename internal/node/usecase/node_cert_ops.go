package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	auditDomain "github.com/nasnet-community/nasnet-panel-linux/internal/audit/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	sniDomain "github.com/nasnet-community/nasnet-panel-linux/internal/sni/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/agent/certs"
	pb "github.com/nasnet-community/nasnet-panel-linux/pkg/agent/pb"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	certRPCProbeTTL           = 10 * time.Minute
	domainInspectTTL          = 6 * time.Hour
	certMaintenanceEvery      = 12 * time.Hour
	certMaintenanceStartDelay = 5 * time.Minute
	revokedRefreshEvery       = 5 * time.Minute
	strictModeKey             = "agent_cert_strict_mode"
)

type certRPCProbe struct {
	supported bool
	at        time.Time
}

type domainInspect struct {
	expires time.Time
	node    string
	at      time.Time
	errMsg  string
}

// ---- overview types ----

type CertSummary struct {
	ID         uint       `json:"id"`
	Type       string     `json:"type"`
	CommonName string     `json:"common_name"`
	Serial     string     `json:"serial"`
	NotBefore  time.Time  `json:"not_before"`
	NotAfter   time.Time  `json:"not_after"`
	DaysLeft   int        `json:"days_left"`
	IsRevoked  bool       `json:"is_revoked"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	NodeID     *uint      `json:"node_id,omitempty"`
	NodeName   string     `json:"node_name,omitempty"`
	Orphan     bool       `json:"orphan,omitempty"`
}

type CertCA struct {
	CertSummary
	FingerprintSHA256 string `json:"fingerprint_sha256"`
}

type PresentedCert struct {
	Serial     string    `json:"serial"`
	CommonName string    `json:"common_name"`
	NotAfter   time.Time `json:"not_after"`
	SeenAt     time.Time `json:"seen_at"`
	Mode       string    `json:"mode"`
	Match      string    `json:"match"` // current | older | unknown
	Revoked    bool      `json:"revoked"`
}

type NodeCertRow struct {
	NodeID          uint           `json:"node_id"`
	Name            string         `json:"name"`
	ConnectMode     string         `json:"connect_mode"`
	IsStealth       bool           `json:"is_stealth"`
	Online          bool           `json:"online"`
	RotateSupported *bool          `json:"rotate_supported"`
	Current         *CertSummary   `json:"current"`
	Presented       *PresentedCert `json:"presented"`
	History         []CertSummary  `json:"history"`
}

type DomainCertRow struct {
	ID           uint       `json:"id"`
	Name         string     `json:"name"`
	Domain       string     `json:"domain"`
	Source       string     `json:"source"` // acme-http | acme-dns | file | uploaded
	NodeNames    []string   `json:"node_names"`
	ExpiresAt    *time.Time `json:"expires_at"`
	DaysLeft     *int       `json:"days_left"`
	AutoRenew    bool       `json:"auto_renew"`
	IsAutoIssued bool       `json:"is_auto_issued"`
	IssueError   string     `json:"issue_error"`
	UsageCount   int64      `json:"usage_count"`
	CheckedAt    *time.Time `json:"checked_at"`
	CheckedOn    string     `json:"checked_on,omitempty"`
	Note         string     `json:"note,omitempty"`
}

type StrictModeState struct {
	Enabled  bool            `json:"enabled"`
	Ready    bool            `json:"ready"`
	Blockers []StrictBlocker `json:"blockers"`
}

type AutoRotateState struct {
	Enabled bool `json:"enabled"`
	Days    int  `json:"days"`
}

type AttentionItem struct {
	Kind    string `json:"kind"` // strict_blocked | mismatch | cannot_rotate | expiring | orphans | legacy
	Count   int    `json:"count"`
	CertIDs []uint `json:"cert_ids,omitempty"`
	NodeIDs []uint `json:"node_ids,omitempty"`
	Message string `json:"message"`
}

type CertCounts struct {
	Active      int `json:"active"`
	Expiring30d int `json:"expiring_30d"`
	Revoked     int `json:"revoked"`
	Orphans     int `json:"orphans"`
}

type CertOverview struct {
	CA           *CertCA         `json:"ca"`
	StrictMode   StrictModeState `json:"strict_mode"`
	AutoRotate   AutoRotateState `json:"auto_rotate"`
	Nodes        []NodeCertRow   `json:"nodes"`
	Domains      []DomainCertRow `json:"domains"`
	Legacy       []CertSummary   `json:"legacy"`
	Attention    []AttentionItem `json:"attention"`
	Counts       CertCounts      `json:"counts"`
	Certificates []CertSummary   `json:"certificates"`
}

type RotateResult struct {
	NodeID   uint   `json:"node_id"`
	NodeName string `json:"node_name"`
	OK       bool   `json:"ok"`
	Serial   string `json:"serial,omitempty"`
	Error    string `json:"error,omitempty"`
	Code     string `json:"code,omitempty"`
}

// ---- helpers ----

func FormatFingerprint(sum []byte) string {
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// FingerprintOf returns the SHA-256 of a PEM certificate's DER, or "".
func FingerprintOf(certPEM []byte) string {
	leaf, err := certs.ParseCertificate(certPEM)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(leaf.Raw)
	return FormatFingerprint(sum[:])
}

func summarize(c *domain.AgentCertificate, nodeName string) CertSummary {
	return CertSummary{
		ID:         c.ID,
		Type:       c.Type,
		CommonName: c.CommonName,
		Serial:     c.SerialNumber,
		NotBefore:  c.NotBefore,
		NotAfter:   c.NotAfter,
		DaysLeft:   c.DaysUntilExpiry(),
		IsRevoked:  c.IsRevoked,
		RevokedAt:  c.RevokedAt,
		NodeID:     c.NodeID,
		NodeName:   nodeName,
	}
}

// rpcDesc pulls the status code and message out of an agent error;
// reverse-mode errors arrive as text ("... code = X desc = msg").
func rpcDesc(err error) (codes.Code, string) {
	if st, ok := grpcstatus.FromError(err); ok && st.Code() != codes.Unknown {
		return st.Code(), st.Message()
	}
	msg := err.Error()
	if i := strings.Index(msg, "code = "); i >= 0 {
		rest := msg[i+len("code = "):]
		if j := strings.Index(rest, " desc = "); j >= 0 {
			name, desc := rest[:j], rest[j+len(" desc = "):]
			for c := codes.OK; c <= codes.Unauthenticated; c++ {
				if c.String() == name {
					return c, desc
				}
			}
		}
	}
	return codes.Unknown, msg
}

// expiryBucket returns the tightest threshold (days) the time left falls
// under, or 0 when above all of them. thresholds ascend.
func expiryBucket(left time.Duration, thresholds ...int) int {
	days := int(left.Hours() / 24)
	for _, t := range thresholds {
		if days <= t {
			return t
		}
	}
	return 0
}

func (u *nodeUsecase) autoRotateSettings(ctx context.Context) AutoRotateState {
	st := AutoRotateState{Enabled: true, Days: 30}
	if u.settingUC == nil {
		return st
	}
	if v, err := u.settingUC.GetByKey(ctx, "agent_cert_auto_rotate"); err == nil && v != "" {
		st.Enabled = v == "true"
	}
	if v, err := u.settingUC.GetByKey(ctx, "agent_cert_rotate_days"); err == nil {
		if d, err := strconv.Atoi(v); err == nil {
			st.Days = min(max(d, 7), 90)
		}
	}
	return st
}

// remoteNodes drops the built-in node: it runs inside the panel's process
// and has no agent certificate to show, rotate or gate on.
func remoteNodes(nodes []*domain.Node) []*domain.Node {
	out := make([]*domain.Node, 0, len(nodes))
	for _, n := range nodes {
		if !n.IsLocal() {
			out = append(out, n)
		}
	}
	return out
}

// ---- rotation ----

// certRPCSupported probes (and caches) whether a node's agent has the cert
// RPCs: a supported agent answers an empty InspectCertFile with
// InvalidArgument. nil = unknown (offline or transport error).
func (u *nodeUsecase) certRPCSupported(ctx context.Context, n *domain.Node) *bool {
	yes, no := true, false
	if n.IsStealth {
		return &no
	}
	if v, ok := u.certRPCSupport.Load(n.ID); ok {
		if p := v.(certRPCProbe); time.Since(p.at) < certRPCProbeTTL {
			return &p.supported
		}
	}
	if !n.IsOnline {
		return nil
	}
	client, err := u.getAgentClient(n)
	if err != nil {
		return nil
	}
	defer closeAgentClient(client)
	cc, ok := client.(agent.CertInstallClient)
	if !ok {
		u.certRPCSupport.Store(n.ID, certRPCProbe{supported: false, at: time.Now()})
		return &no
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = cc.InspectCertFile(pctx, &pb.InspectCertFileRequest{})
	var supported bool
	switch {
	case err == nil:
		supported = true
	case isUnimplementedErr(err):
		supported = false
	default:
		if code, _ := rpcDesc(err); code != codes.InvalidArgument {
			return nil
		}
		supported = true
	}
	u.certRPCSupport.Store(n.ID, certRPCProbe{supported: supported, at: time.Now()})
	if supported {
		return &yes
	}
	return &no
}

// RotateAgentCert issues a new node certificate and installs it on the node.
// Rotations of one node run one at a time (manual, bulk and scheduler).
func (u *nodeUsecase) RotateAgentCert(ctx context.Context, nodeID uint) (*domain.AgentCertificate, error) {
	lock, _ := u.certRotateLocks.LoadOrStore(nodeID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()

	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node.IsLocal() {
		return nil, ErrLocalNode // shares the panel's process, no agent cert
	}
	if node.IsStealth {
		return nil, ErrNodeStealth
	}
	client, err := u.getAgentClient(node)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNodeOffline, err)
	}
	defer closeAgentClient(client)
	installer, ok := client.(agent.CertInstallClient)
	if !ok {
		return nil, ErrCertAgentOutdated
	}

	ca, err := u.certUC.GetCA(ctx)
	if err != nil {
		return nil, fmt.Errorf("CA not found: %w", err)
	}
	info, err := certs.GenerateAgentCert(&certs.AgentCertConfig{
		NodeID:      node.ID,
		NodeName:    node.Name,
		IPAddresses: []string{node.IP},
		DNSNames:    []string{node.Name},
		ValidMonths: 12,
	}, ca.Certificate, ca.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("generate certificate: %w", err)
	}

	ictx, cancel := context.WithTimeout(ctx, agent.DeadlineFor("InstallCertificate"))
	defer cancel()
	if _, err := installer.InstallCertificate(ictx, &pb.InstallCertificateRequest{CertPem: info.Certificate, KeyPem: info.PrivateKey}); err != nil {
		if isUnimplementedErr(err) {
			u.certRPCSupport.Store(node.ID, certRPCProbe{supported: false, at: time.Now()})
			return nil, ErrCertAgentOutdated
		}
		if code, desc := rpcDesc(err); code == codes.InvalidArgument || code == codes.FailedPrecondition {
			return nil, &CertInstallRejectedError{Reason: desc}
		}
		return nil, fmt.Errorf("%w: %v", ErrNodeOffline, err)
	}

	rec, err := u.certUC.RecordAgentCert(ctx, node.ID, info)
	if err != nil {
		logger.GetLogger().WithError(err).WithField("node_id", node.ID).Error("[RotateAgentCert] Node runs a certificate the hub failed to record")
		return nil, fmt.Errorf("certificate installed but not recorded: %w", err)
	}
	u.certRPCSupport.Store(node.ID, certRPCProbe{supported: true, at: time.Now()})
	// The node may already have reconnected with the new cert before it was
	// recorded; that sighting's retirement then found nothing to do.
	if u.certWatch != nil {
		if s, ok := u.certWatch.Sighting(node.ID); ok && s.Serial == rec.SerialNumber {
			go u.retireSuperseded(node.ID, rec.SerialNumber)
		}
	}
	if node.ConnectMode != domain.ConnectModeReverse && u.clientPool != nil {
		// Next dial handshakes against the new cert, which triggers retirement.
		u.clientPool.Evict(node.ID)
	}
	logger.GetLogger().WithFields(map[string]interface{}{"node_id": node.ID, "serial": rec.SerialNumber}).Info("[RotateAgentCert] Node certificate rotated")
	return rec, nil
}

// RotateExpiringCerts rotates every non-stealth node whose current cert is
// within the rotate threshold.
func (u *nodeUsecase) RotateExpiringCerts(ctx context.Context) ([]RotateResult, error) {
	days := u.autoRotateSettings(ctx).Days
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	results := []RotateResult{}
	for _, n := range remoteNodes(nodes) {
		if n.IsStealth {
			continue
		}
		cur, err := u.certUC.GetAgentCert(ctx, n.ID)
		if err != nil || cur.DaysUntilExpiry() > days {
			continue
		}
		r := RotateResult{NodeID: n.ID, NodeName: n.Name}
		if rec, err := u.RotateAgentCert(ctx, n.ID); err != nil {
			r.Error, r.Code = err.Error(), CertErrorCode(err)
		} else {
			r.OK, r.Serial = true, rec.SerialNumber
		}
		results = append(results, r)
	}
	return results, nil
}

// ---- overview ----

// CertificateOverview assembles everything the Certificates page shows.
func (u *nodeUsecase) CertificateOverview(ctx context.Context, refreshDomains bool) (*CertOverview, error) {
	all, err := u.certUC.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	nodeByID := make(map[uint]*domain.Node, len(nodes))
	for _, n := range nodes {
		nodeByID[n.ID] = n
	}

	ov := &CertOverview{
		AutoRotate:   u.autoRotateSettings(ctx),
		Nodes:        []NodeCertRow{},
		Domains:      []DomainCertRow{},
		Legacy:       []CertSummary{},
		Attention:    []AttentionItem{},
		Certificates: make([]CertSummary, 0, len(all)),
	}
	ov.StrictMode.Enabled = u.certWatch != nil && u.certWatch.Strict()

	byNode := map[uint][]*domain.AgentCertificate{}
	var orphanIDs, legacyIDs []uint
	for _, c := range all { // newest first
		name := ""
		if c.NodeID != nil {
			if n := nodeByID[*c.NodeID]; n != nil {
				name = n.Name
			}
		}
		s := summarize(c, name)
		switch c.Type {
		case domain.CertTypeCA:
			if ov.CA == nil && !c.IsRevoked {
				ov.CA = &CertCA{CertSummary: s, FingerprintSHA256: FingerprintOf(c.Certificate)}
			}
		case domain.CertTypeAgent:
			if c.NodeID != nil && nodeByID[*c.NodeID] != nil {
				byNode[*c.NodeID] = append(byNode[*c.NodeID], c)
			} else if !c.IsRevoked {
				s.Orphan = true
				orphanIDs = append(orphanIDs, c.ID)
			}
		case domain.CertTypeMaster, domain.CertTypePublic:
			ov.Legacy = append(ov.Legacy, s)
			if !c.IsRevoked {
				legacyIDs = append(legacyIDs, c.ID)
			}
		}
		if c.IsRevoked {
			ov.Counts.Revoked++
		}
		ov.Certificates = append(ov.Certificates, s)
	}
	ov.Counts.Orphans = len(orphanIDs)

	remote := remoteNodes(nodes)
	rows := make([]NodeCertRow, len(remote))
	var wg sync.WaitGroup
	for i, n := range remote {
		rows[i] = u.nodeCertRow(ctx, n, byNode[n.ID])
		wg.Add(1)
		go func(i int, n *domain.Node) {
			defer wg.Done()
			rows[i].RotateSupported = u.certRPCSupported(ctx, n)
		}(i, n)
	}
	wg.Wait()
	ov.Nodes = rows

	ov.StrictMode.Blockers = strictBlockers(rows)
	ov.StrictMode.Ready = len(ov.StrictMode.Blockers) == 0
	ov.Domains = u.domainCertRows(ctx, nodeByID, refreshDomains)

	count := func(expires time.Time) {
		ov.Counts.Active++
		if time.Until(expires) <= 30*24*time.Hour {
			ov.Counts.Expiring30d++
		}
	}
	if ov.CA != nil {
		count(ov.CA.NotAfter)
	}
	for _, r := range rows {
		if r.Current != nil && r.Current.DaysLeft >= 0 {
			count(r.Current.NotAfter)
		}
	}
	for _, d := range ov.Domains {
		if d.ExpiresAt != nil && d.ExpiresAt.After(time.Now()) {
			count(*d.ExpiresAt)
		}
	}

	ov.Attention = certAttention(ov, orphanIDs, legacyIDs)
	return ov, nil
}

func (u *nodeUsecase) nodeCertRow(ctx context.Context, n *domain.Node, certsOfNode []*domain.AgentCertificate) NodeCertRow {
	row := NodeCertRow{
		NodeID:      n.ID,
		Name:        n.Name,
		ConnectMode: n.ConnectMode,
		IsStealth:   n.IsStealth,
		Online:      n.IsOnline,
		History:     []CertSummary{},
	}
	if row.ConnectMode == "" {
		row.ConnectMode = "direct"
	}
	for _, c := range certsOfNode {
		s := summarize(c, n.Name)
		if row.Current == nil && !c.IsRevoked {
			row.Current = &s
			continue
		}
		row.History = append(row.History, s)
	}
	if u.certWatch == nil {
		return row
	}
	sight, ok := u.certWatch.Sighting(n.ID)
	if !ok {
		return row
	}
	p := &PresentedCert{
		Serial:     sight.Serial,
		CommonName: sight.CommonName,
		NotAfter:   sight.NotAfter,
		SeenAt:     sight.SeenAt,
		Mode:       sight.Mode,
		Match:      "unknown",
		Revoked:    u.certWatch.IsRevoked(sight.Serial),
	}
	if row.Current != nil && row.Current.Serial == sight.Serial {
		p.Match = "current"
	} else if list, err := u.certUC.ListNodeCerts(ctx, n.ID); err == nil { // includes soft-deleted
		for _, c := range list {
			if c.SerialNumber == sight.Serial {
				p.Match = "older"
				break
			}
		}
	}
	row.Presented = p
	return row
}

func strictBlockers(rows []NodeCertRow) []StrictBlocker {
	out := []StrictBlocker{}
	for _, r := range rows {
		reason := ""
		switch {
		case r.Presented == nil:
			reason = "not_seen"
		case r.Presented.Revoked:
			reason = "presents_revoked"
		case r.Presented.Match == "unknown":
			reason = "presents_unknown"
		default:
			if id, ok := certs.NodeIDFromCommonName(r.Presented.CommonName); !ok || id != r.NodeID {
				reason = "id_mismatch"
			}
		}
		if reason != "" {
			out = append(out, StrictBlocker{NodeID: r.NodeID, NodeName: r.Name, Reason: reason})
		}
	}
	return out
}

func joinNames(names []string) string {
	if len(names) > 3 {
		return strings.Join(names[:3], ", ") + fmt.Sprintf(" and %d more", len(names)-3)
	}
	return strings.Join(names, ", ")
}

func certAttention(ov *CertOverview, orphanIDs, legacyIDs []uint) []AttentionItem {
	items := []AttentionItem{}
	if ov.StrictMode.Enabled && len(ov.StrictMode.Blockers) > 0 {
		var ids []uint
		var names []string
		for _, b := range ov.StrictMode.Blockers {
			ids, names = append(ids, b.NodeID), append(names, b.NodeName)
		}
		items = append(items, AttentionItem{Kind: "strict_blocked", Count: len(ids), NodeIDs: ids,
			Message: fmt.Sprintf("Strict mode is refusing %s. Rotate or redeploy them.", joinNames(names))})
	}
	var mismatch, cannot, expiring []uint
	var mismatchN, cannotN, expiringN []string
	for _, r := range ov.Nodes {
		if r.Presented != nil && r.Presented.Match != "current" {
			mismatch, mismatchN = append(mismatch, r.NodeID), append(mismatchN, r.Name)
		}
		if r.Current == nil || r.Current.DaysLeft > ov.AutoRotate.Days {
			continue
		}
		if r.IsStealth || !r.Online || (r.RotateSupported != nil && !*r.RotateSupported) {
			cannot, cannotN = append(cannot, r.NodeID), append(cannotN, r.Name)
		} else {
			expiring, expiringN = append(expiring, r.NodeID), append(expiringN, r.Name)
		}
	}
	if len(mismatch) > 0 {
		items = append(items, AttentionItem{Kind: "mismatch", Count: len(mismatch), NodeIDs: mismatch,
			Message: fmt.Sprintf("%s present an older or unknown certificate. Rotate to fix.", joinNames(mismatchN))})
	}
	if len(cannot) > 0 {
		items = append(items, AttentionItem{Kind: "cannot_rotate", Count: len(cannot), NodeIDs: cannot,
			Message: fmt.Sprintf("%s expire soon and can't be rotated from here (offline, stealth or old agent).", joinNames(cannotN))})
	}
	if len(expiring) > 0 {
		items = append(items, AttentionItem{Kind: "expiring", Count: len(expiring), NodeIDs: expiring,
			Message: fmt.Sprintf("%s expire within %d days.", joinNames(expiringN), ov.AutoRotate.Days)})
	}
	if len(orphanIDs) > 0 {
		items = append(items, AttentionItem{Kind: "orphans", Count: len(orphanIDs), CertIDs: orphanIDs,
			Message: fmt.Sprintf("%d valid certificate(s) belong to nodes that no longer exist.", len(orphanIDs))})
	}
	if len(legacyIDs) > 0 {
		items = append(items, AttentionItem{Kind: "legacy", Count: len(legacyIDs), CertIDs: legacyIDs,
			Message: fmt.Sprintf("%d legacy certificate(s) (master or public) the hub no longer uses.", len(legacyIDs))})
	}
	return items
}

// ---- domains ----

func (u *nodeUsecase) domainCertRows(ctx context.Context, nodeByID map[uint]*domain.Node, refresh bool) []DomainCertRow {
	rows := []DomainCertRow{}
	if u.sniUsecase == nil {
		return rows
	}
	snis, err := u.sniUsecase.List(ctx)
	if err != nil {
		logger.GetLogger().WithError(err).Warn("[CertOverview] Failed to list domains")
		return rows
	}
	for _, s := range snis {
		row := DomainCertRow{
			ID:           s.ID,
			Name:         s.Name,
			Domain:       s.Domain,
			AutoRenew:    s.AutoRenew && s.IsAutoIssued,
			IsAutoIssued: s.IsAutoIssued,
			IssueError:   s.IssueError,
			NodeNames:    []string{},
		}
		switch {
		case s.IsAutoIssued && s.ChallengeType == "dns-01":
			row.Source = "acme-dns"
		case s.IsAutoIssued:
			row.Source = "acme-http"
		case s.UsePathMode:
			row.Source = "file"
		default:
			row.Source = "uploaded"
		}
		row.UsageCount, _ = u.sniUsecase.CountInbounds(ctx, s.ID)
		ids, _ := u.sniUsecase.ListNodeIDs(ctx, s.ID)
		var linked []*domain.Node
		for _, id := range ids {
			if n := nodeByID[id]; n != nil {
				linked = append(linked, n)
				row.NodeNames = append(row.NodeNames, n.Name)
			}
		}
		expires := s.ExpiresAt
		if s.UsePathMode {
			insp := u.inspectDomainFile(ctx, s, linked, refresh)
			if !insp.at.IsZero() {
				at := insp.at
				row.CheckedAt = &at
			}
			row.CheckedOn, row.Note = insp.node, insp.errMsg
			if !insp.expires.IsZero() {
				expires = insp.expires
			}
		}
		if !expires.IsZero() {
			e := expires
			d := int(time.Until(e).Hours() / 24)
			row.ExpiresAt, row.DaysLeft = &e, &d
		}
		rows = append(rows, row)
	}
	return rows
}

// inspectDomainFile reads a file-mode domain cert's expiry from the first
// online node that serves it. Results (failures too) are cached.
func (u *nodeUsecase) inspectDomainFile(ctx context.Context, s *sniDomain.SNI, linked []*domain.Node, refresh bool) domainInspect {
	if v, ok := u.domainInspectCache.Load(s.ID); ok && !refresh {
		if e := v.(domainInspect); time.Since(e.at) < domainInspectTTL {
			return e
		}
	}
	if len(linked) == 0 {
		return domainInspect{errMsg: "Not used by any inbound, so there is no node to read the file from."}
	}
	res := domainInspect{errMsg: "No linked node is online to read the file."}
	for _, n := range linked {
		if !n.IsOnline {
			continue
		}
		client, err := u.getAgentClient(n)
		if err != nil {
			continue
		}
		cc, ok := client.(agent.CertInstallClient)
		if !ok {
			closeAgentClient(client)
			res.errMsg = "Update the agent on " + n.Name + " to read file certificates."
			continue
		}
		ictx, cancel := context.WithTimeout(ctx, agent.DeadlineFor("InspectCertFile"))
		out, err := cc.InspectCertFile(ictx, &pb.InspectCertFileRequest{Path: s.CertPath})
		cancel()
		closeAgentClient(client)
		if err != nil {
			if isUnimplementedErr(err) {
				res.errMsg = "Update the agent on " + n.Name + " to read file certificates."
			} else {
				_, desc := rpcDesc(err)
				res.errMsg = fmt.Sprintf("%s could not read %s: %s", n.Name, s.CertPath, desc)
			}
			continue
		}
		res = domainInspect{expires: time.Unix(out.GetNotAfterUnix(), 0), node: n.Name}
		break
	}
	res.at = time.Now()
	u.domainInspectCache.Store(s.ID, res)
	return res
}

// ---- strict mode ----

func (u *nodeUsecase) SetCertStrictMode(ctx context.Context, enabled bool) error {
	if u.certWatch == nil {
		return errors.New("certificate watch is not wired")
	}
	if enabled {
		ov, err := u.CertificateOverview(ctx, false)
		if err != nil {
			return err
		}
		if len(ov.StrictMode.Blockers) > 0 {
			return &StrictModeNotReadyError{Blockers: ov.StrictMode.Blockers}
		}
	}
	err := u.settingUC.UpdateMany(settingDomain.AllowProtectedWrite(ctx), []*settingDomain.Setting{{
		Key:         strictModeKey,
		Value:       strconv.FormatBool(enabled),
		Type:        "bool",
		Category:    "admin",
		Label:       "Certificate Strict Mode",
		Description: "Refuse revoked node certificates and certificates that belong to another node. Changed from the Certificates page only.",
	}})
	if err != nil {
		return err
	}
	u.certWatch.SetStrict(enabled)
	if enabled {
		u.EvictAllAgentClients()
	}
	return nil
}

// ---- maintenance ----

// RunAgentCertMaintenance rotates node certs that are close to expiry and
// alerts when that is not possible. Runs at most every 12 h; the first run
// waits a few minutes after start so agents can reconnect.
func (u *nodeUsecase) RunAgentCertMaintenance(ctx context.Context) error {
	now := time.Now()
	u.certMaintMu.Lock()
	refresh := now.Sub(u.revokedRefreshAt) >= revokedRefreshEvery
	if refresh {
		u.revokedRefreshAt = now
	}
	u.certMaintMu.Unlock()
	// Covers a revoke whose refresh failed or that bypassed the usecase.
	if refresh && u.certWatch != nil {
		if err := u.certWatch.RefreshRevoked(ctx); err != nil {
			logger.GetLogger().WithError(err).Warn("[CertMaintenance] Failed to refresh revoked certificates")
		}
	}

	u.certMaintMu.Lock()
	if u.certMaintNext.IsZero() {
		u.certMaintNext = now.Add(certMaintenanceStartDelay)
	}
	if now.Before(u.certMaintNext) {
		u.certMaintMu.Unlock()
		return nil
	}
	u.certMaintNext = now.Add(certMaintenanceEvery)
	u.certMaintMu.Unlock()

	settings := u.autoRotateSettings(ctx)
	all, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return err
	}
	nodes := remoteNodes(all)
	// Retry retirements that were lost (race with the record, DB error).
	if u.certWatch != nil {
		for _, n := range nodes {
			if s, ok := u.certWatch.Sighting(n.ID); ok {
				u.retireSuperseded(n.ID, s.Serial)
			}
		}
	}
	for _, n := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cur, err := u.certUC.GetAgentCert(ctx, n.ID)
		if err != nil {
			continue
		}
		expires := cur.NotAfter
		if u.certWatch != nil {
			if s, ok := u.certWatch.Sighting(n.ID); ok {
				expires = s.NotAfter // what the node actually uses is what breaks
			}
		}
		left := time.Until(expires)
		if left > time.Duration(settings.Days)*24*time.Hour {
			continue
		}
		var failure error
		switch {
		case n.IsStealth:
			failure = ErrNodeStealth
		case !settings.Enabled:
			failure = errors.New("auto-rotate is turned off")
		default:
			rec, err := u.RotateAgentCert(ctx, n.ID)
			if err == nil {
				u.auditSchedulerRotate(ctx, n, rec)
				continue
			}
			failure = err
		}
		u.alertNodeCertExpiry(ctx, n, cur, left, failure)
	}
	u.alertCAExpiry(ctx)
	return nil
}

func (u *nodeUsecase) alertNodeCertExpiry(ctx context.Context, n *domain.Node, cur *domain.AgentCertificate, left time.Duration, failure error) {
	bucket := expiryBucket(left, 1, 7, 30)
	if bucket == 0 || (cur.ExpiryNotifyLevel != 0 && bucket >= cur.ExpiryNotifyLevel) {
		return
	}
	level := "warning"
	if bucket <= 7 {
		level = "error"
	}
	u.publishSystemAlert(level, "Node certificate expiring", fmt.Sprintf(
		"Node %s: its agent certificate expires in %d day(s) and was not rotated (%v). When it expires the hub loses this node until it is redeployed.",
		n.Name, int(left.Hours()/24), failure))
	if err := u.certUC.SetExpiryNotifyLevel(ctx, cur.ID, bucket); err != nil {
		logger.GetLogger().WithError(err).Warn("[CertMaintenance] Failed to record expiry alert")
	}
}

func (u *nodeUsecase) alertCAExpiry(ctx context.Context) {
	ca, err := u.certUC.GetCA(ctx)
	if err != nil || ca == nil {
		return
	}
	left := time.Until(ca.NotAfter)
	bucket := expiryBucket(left, 30, 180)
	if bucket == 0 || (ca.ExpiryNotifyLevel != 0 && bucket >= ca.ExpiryNotifyLevel) {
		return
	}
	level := "warning"
	if bucket <= 30 {
		level = "error"
	}
	u.publishSystemAlert(level, "Hub CA expiring", fmt.Sprintf(
		"The hub's CA expires in %d day(s). Every node certificate depends on it.", int(left.Hours()/24)))
	if err := u.certUC.SetExpiryNotifyLevel(ctx, ca.ID, bucket); err != nil {
		logger.GetLogger().WithError(err).Warn("[CertMaintenance] Failed to record CA alert")
	}
}

func (u *nodeUsecase) publishSystemAlert(level, title, message string) {
	if u.eventBus == nil {
		return
	}
	u.eventBus.Publish(events.Event{
		Type:    events.EventSystemAlert,
		Payload: events.SystemAlertPayload{Level: level, Title: title, Message: message},
	})
}

func (u *nodeUsecase) auditSchedulerRotate(ctx context.Context, n *domain.Node, rec *domain.AgentCertificate) {
	if u.auditUC == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"node_id": n.ID, "node_name": n.Name, "serial": rec.SerialNumber})
	u.auditUC.Log(ctx, &auditDomain.AuditLog{
		Action:     "cert_rotate",
		ActorName:  "scheduler",
		EntityType: "certificate",
		EntityID:   rec.ID,
		NewValues:  string(payload),
		Source:     "scheduler",
	})
}

// ---- counts (dashboard/sidebar; DB only, no agent calls) ----

func (u *nodeUsecase) CertificateCounts(ctx context.Context) (active, expiring int, err error) {
	all, err := u.certUC.ListCertificates(ctx)
	if err != nil {
		return 0, 0, err
	}
	nodes, err := u.nodeRepo.ListNodes(ctx)
	if err != nil {
		return 0, 0, err
	}
	live := make(map[uint]bool, len(nodes))
	for _, n := range remoteNodes(nodes) {
		live[n.ID] = true
	}
	seenNode := map[uint]bool{}
	count := func(expires time.Time) {
		active++
		if time.Until(expires) <= 30*24*time.Hour {
			expiring++
		}
	}
	for _, c := range all {
		if c.IsRevoked || c.IsExpired() {
			continue
		}
		switch c.Type {
		case domain.CertTypeCA:
			count(c.NotAfter)
		case domain.CertTypeAgent:
			if c.NodeID != nil && live[*c.NodeID] && !seenNode[*c.NodeID] {
				seenNode[*c.NodeID] = true
				count(c.NotAfter)
			}
		}
	}
	if u.sniUsecase != nil {
		if snis, err := u.sniUsecase.List(ctx); err == nil {
			for _, s := range snis {
				if s.ExpiresAt.IsZero() {
					active++ // file-mode cert, expiry only known from the node
				} else if s.ExpiresAt.After(time.Now()) {
					count(s.ExpiresAt)
				}
			}
		}
	}
	return active, expiring, nil
}
