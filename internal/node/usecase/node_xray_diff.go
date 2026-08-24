package usecase

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/bandwidth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// XrayConfigDiff: Running = current agent report; Generated = what
// master would push now. Differs is a hash compare for the UI badge.
type XrayConfigDiff struct {
	Running       string `json:"running"`
	Generated     string `json:"generated"`
	RunningHash   string `json:"running_hash"`
	GeneratedHash string `json:"generated_hash"`
	Differs       bool   `json:"differs"`
	// RunningError surfaces when we couldn't fetch from the agent (offline,
	// auth, etc). Generated still usable as a raw preview in that case.
	RunningError string `json:"running_error,omitempty"`
}

// GetNodeXrayConfigDiff: running-vs-would-push diff, pretty-printed JSON
// for Monaco diff editor. Read-only: no agent writes, no cert uploads;
// certs inlined into the JSON so preview is self-contained.
func (u *nodeUsecase) GetNodeXrayConfigDiff(ctx context.Context, nodeID uint) (*XrayConfigDiff, error) {
	node, err := u.nodeRepo.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}

	// Build the generated config (no side effects).
	generated, err := u.buildNodeConfigForDiff(ctx, node)
	if err != nil {
		return nil, fmt.Errorf("build generated config: %w", err)
	}
	genHash := md5.Sum([]byte(generated))
	genHashStr := hex.EncodeToString(genHash[:])

	out := &XrayConfigDiff{
		Generated:     generated,
		GeneratedHash: genHashStr,
	}

	// Fetch running config from agent. Agent may be offline; that's OK —
	// report the error inline and keep the generated side usable.
	client, err := u.getAgentClient(node)
	if err != nil {
		out.RunningError = err.Error()
		out.Differs = true
		return out, nil
	}
	defer closeAgentClient(client)

	running, _, err := client.GetCurrentConfig(ctx)
	if err != nil {
		out.RunningError = err.Error()
		out.Differs = true
		return out, nil
	}
	out.Running = running
	runHash := md5.Sum([]byte(running))
	out.RunningHash = hex.EncodeToString(runHash[:])
	// Compare with cert material blanked out: the running config stores certs as
	// file paths while the generated preview inlines them, so comparing raw would
	// always report drift. Cert propagation is handled by the push path, not this
	// badge — here we only care about non-cert config drift.
	out.Differs = normalizeCertsForCompare(running) != normalizeCertsForCompare(generated)
	return out, nil
}

// normalizeCertsForCompare blanks TLS cert/key material (inline or file path) so
// two configs carrying the same cert in different representations compare equal.
// Falls back to the raw string if the config isn't parseable JSON.
func normalizeCertsForCompare(cfg string) string {
	var root interface{}
	if err := json.Unmarshal([]byte(cfg), &root); err != nil {
		return cfg
	}
	stripCertMaterial(root)
	out, err := json.Marshal(root)
	if err != nil {
		return cfg
	}
	return string(out)
}

func stripCertMaterial(v interface{}) {
	switch node := v.(type) {
	case map[string]interface{}:
		for _, k := range []string{"certificate", "certificateFile", "key", "keyFile"} {
			if _, ok := node[k]; ok {
				node[k] = "***"
			}
		}
		for _, child := range node {
			stripCertMaterial(child)
		}
	case []interface{}:
		for _, child := range node {
			stripCertMaterial(child)
		}
	}
}

// buildNodeConfigForDiff: pushConfigToAgent's DB-driven build, side-effect-free.
// Certs inlined for stable preview across stealth/non-stealth nodes.
func (u *nodeUsecase) buildNodeConfigForDiff(ctx context.Context, node *domain.Node) (string, error) {
	var (
		inbounds       []*domain.Inbound
		outbounds      []*domain.Outbound
		routingRules   []*domain.RoutingRule
		reverseProxies []*domain.ReverseProxy
		balancingRules []*domain.BalancingRule
		dbErrors       [5]error
	)
	var dbWg sync.WaitGroup
	dbWg.Add(5)
	go func() {
		defer dbWg.Done()
		inbounds, dbErrors[0] = u.nodeRepo.ListInboundsByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		outbounds, dbErrors[1] = u.nodeRepo.ListOutboundsByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		routingRules, dbErrors[2] = u.nodeRepo.ListRoutingRulesByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		reverseProxies, dbErrors[3] = u.nodeRepo.ListReverseProxiesByNode(ctx, node.ID)
	}()
	go func() {
		defer dbWg.Done()
		balancingRules, dbErrors[4] = u.nodeRepo.ListBalancingRulesByNode(ctx, node.ID)
	}()
	dbWg.Wait()
	for _, e := range dbErrors {
		if e != nil {
			return "", e
		}
	}

	// Filter disabled inbounds/outbounds — diff should reflect what we'd
	// actually push, which excludes them.
	enabledInbounds := make([]*domain.Inbound, 0, len(inbounds))
	for _, in := range inbounds {
		if !in.IsDisabled {
			enabledInbounds = append(enabledInbounds, in)
		}
	}
	inbounds = enabledInbounds

	enabledOutbounds := make([]*domain.Outbound, 0, len(outbounds))
	for _, out := range outbounds {
		if !out.IsDisabled {
			enabledOutbounds = append(enabledOutbounds, out)
		}
	}
	outbounds = enabledOutbounds

	// Populate users the same way the real push does — from active
	// accounts, deduplicated by UUID within an inbound.
	usersMap := make(map[string][]*xray.User)
	addedUUIDs := make(map[string]map[string]bool)
	if u.accountRepo != nil {
		accounts, err := u.accountRepo.ListByNodeID(ctx, node.ID)
		if err != nil {
			return "", fmt.Errorf("list accounts: %w", err)
		}
		for _, acc := range accounts {
			if acc.Status != "active" || acc.Inbound == nil || acc.Inbound.IsDisabled {
				continue
			}
			tag := acc.Inbound.Tag
			if addedUUIDs[tag] == nil {
				addedUUIDs[tag] = make(map[string]bool)
			}
			if addedUUIDs[tag][acc.UUID] {
				continue
			}
			var lvl uint32
			if acc.Subscription != nil {
				lvl = bandwidth.GetTier(acc.Subscription.GetEffectiveBandwidthLimit()).Level
			}
			usersMap[tag] = append(usersMap[tag], &xray.User{
				Email:      acc.Email,
				UUID:       acc.UUID,
				Level:      lvl,
				Protocol:   xray.Protocol(acc.Inbound.Protocol),
				Flow:       acc.Flow,
				Encryption: acc.Encryption,
				AlterId:    0,
			})
			addedUUIDs[tag][acc.UUID] = true
		}
	}

	// Inline managed/SNI cert contents. No file writes, no abs-path
	// lookups — the preview is self-contained.
	for i := range inbounds {
		in := inbounds[i]
		if in.Security != "tls" {
			continue
		}
		tlsSettings := in.GetTLSSettingsOrDefault()
		for j := range tlsSettings.Certificates {
			cert := &tlsSettings.Certificates[j]
			if cert.ID > 0 && u.certUC != nil {
				fetched, err := u.certUC.GetCertificate(ctx, cert.ID)
				if err == nil && fetched != nil {
					cert.CertificateFile = string(fetched.Certificate)
					cert.KeyFile = string(fetched.PrivateKey)
				}
			} else if cert.SNIId > 0 {
				certPEM, keyPEM, err := u.resolveSNICertContent(ctx, cert.SNIId)
				if err == nil {
					cert.CertificateFile = string(certPEM)
					cert.KeyFile = string(keyPEM)
				}
			}
		}
	}

	apiPort := node.APIPort
	if apiPort == 0 {
		apiPort = 10085
	}

	configJSON, err := xray.NewFullConfigBuilder(node).
		WithRouterMode(u.routerMode).
		WithRouterWANs(u.currentRouterWANs(ctx)).
		WithInbounds(inbounds).
		WithOutbounds(outbounds).
		WithRoutingRules(routingRules).
		WithBalancingRules(balancingRules).
		WithReverseProxies(reverseProxies).
		WithUsers(usersMap).
		WithAPI(true, apiPort).
		Build()
	if err != nil {
		return "", err
	}
	return configJSON, nil
}
