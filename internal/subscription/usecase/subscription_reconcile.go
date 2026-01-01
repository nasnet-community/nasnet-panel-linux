package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/shared/contract"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

// ReconcileUsers enforces account-Xray consistency across all nodes
// Uses accounts table as single source of truth (includes both subscription and manual accounts)
func (u *subscriptionUsecase) ReconcileUsers(ctx context.Context) (*ReconcileStats, error) {
	log := logger.GetLogger()
	stats := &ReconcileStats{}

	nodes, err := u.nodeRepo.ListActiveNodes(ctx)
	if err != nil {
		return stats, err
	}

	// Build inbound ID -> tag lookup
	inboundTagMap := make(map[uint]string)
	for _, node := range nodes {
		for _, inbound := range node.Inbounds {
			inboundTagMap[inbound.ID] = inbound.Tag
		}
	}

	// Get all active accounts from accounts table
	var authMap map[uint]map[string]*contract.AccountInfo // inboundID -> email -> account

	if u.accountReader != nil {
		accounts, err := u.accountReader.ListActiveAccountInfos(ctx)
		if err != nil {
			log.Warnf("ReconcileUsers: Failed to read accounts, falling back to subscription-based: %v", err)
		} else {
			authMap = make(map[uint]map[string]*contract.AccountInfo)
			for _, acc := range accounts {
				if _, ok := authMap[acc.InboundID]; !ok {
					authMap[acc.InboundID] = make(map[string]*contract.AccountInfo)
				}
				authMap[acc.InboundID][acc.Email] = acc
			}
			stats.TotalDBUsers = len(accounts)
		}
	}

	// Without the account reader there's nothing to reconcile against (accounts
	// are the sole source of truth), so skip this pass rather than fall back.
	if authMap == nil {
		log.Warn("ReconcileUsers: AccountReader not available; skipping reconciliation")
		return stats, nil
	}

	// Enforce state on each node using account-based auth map
	for _, node := range nodes {
		for _, inbound := range node.Inbounds {
			// Skip disabled inbounds — they are excluded from the xray config
			if inbound.IsDisabled {
				continue
			}
			// Skip protocols that don't expose UserManager (xray's
			// "proxy is not a UserManager" error). socks/http/dokodemo-door
			// use static admin-defined credentials — there's nothing to
			// reconcile, and probing would log a warning every cycle.
			if !protocolSupportsUserManagement(inbound.Protocol) {
				continue
			}

			var xrayUsers []*xray.User
			var err error

			// Fetch users via Agent
			if u.nodeUC != nil {
				client, errClient := u.nodeUC.GetNodeClient(ctx, node.ID)
				if errClient != nil {
					log.Warnf("Reconcile: Failed to get agent client for %s: %v", node.Name, errClient)
					stats.Errors++
					continue
				}

				// Fetch users
				agentUsers, errList := client.ListUsers(ctx, inbound.Tag)
				client.Close() // Close immediately after use

				if errList != nil {
					err = errList
				} else {
					// Map to xray.User for common diff logic
					xrayUsers = make([]*xray.User, len(agentUsers))
					for i, au := range agentUsers {
						xrayUsers[i] = &xray.User{
							Email:      au.Email,
							UUID:       au.Uuid,
							Level:      uint32(au.Level),
							Flow:       au.Flow,
							Encryption: au.Encryption,
							Protocol:   xray.Protocol(au.Protocol),
						}
					}
				}
			} else {
				log.Warnf("Reconcile: NodeUC not available for node %s", node.Name)
				continue
			}

			if err != nil {
				log.Warnf("Reconcile: Failed to fetch users from %s (Tag: %s): %v", node.Name, inbound.Tag, err)
				stats.Errors++
				continue
			}
			stats.TotalXrayUsers += len(xrayUsers)

			// Get authorized accounts for this inbound
			inboundAccounts := authMap[inbound.ID]

			// A. Remove Ghosts (users in Xray but not in accounts table)
			for _, xu := range xrayUsers {
				// Skip non-managed users
				if !strings.HasPrefix(xu.Email, "user_") && !strings.HasPrefix(xu.Email, "trial_") && !strings.HasPrefix(xu.Email, "manual_") {
					continue
				}

				isAuthorized := false
				if inboundAccounts != nil {
					_, isAuthorized = inboundAccounts[xu.Email]
				}

				if !isAuthorized {
					var err error
					if u.nodeUC != nil {
						err = u.nodeUC.RemoveUserViaAgent(ctx, node, inbound.Tag, xu.Email)
					} else {
						// Fallback if nodeUC unavailable
						target := fmt.Sprintf("%s:%d", node.IP, node.APIPort)
						err = u.grpcClient.RemoveUser(ctx, target, inbound.Tag, xu.Email)
					}

					if err == nil {
						stats.GhostsRemoved++
						log.Debugf("Reconcile: Removed ghost user %s from %s/%s", xu.Email, node.Name, inbound.Tag)
					} else {
						stats.Errors++
						log.WithError(err).WithFields(map[string]interface{}{
							"email":       xu.Email,
							"node":        node.Name,
							"inbound_tag": inbound.Tag,
						}).Warn("[ReconcileUsers] Failed to remove ghost user")
					}
				}
			}

			// B. Add Missing (accounts in DB but not in Xray)
			if inboundAccounts == nil {
				continue
			}

			for email, acc := range inboundAccounts {
				exists := false
				for _, xu := range xrayUsers {
					if xu.Email == email {
						exists = true
						break
					}
				}

				if !exists {
					var pType xray.Protocol
					switch strings.ToLower(inbound.Protocol) {
					case "vmess":
						pType = xray.ProtocolVMess
					case "vless":
						pType = xray.ProtocolVLESS
					case "trojan":
						pType = xray.ProtocolTrojan
					case "shadowsocks":
						pType = xray.ProtocolShadowsocks
					case "hysteria2", "hysteria":
						pType = xray.ProtocolHysteria2
					default:
						continue
					}

					var err error
					if u.nodeUC != nil {
						err = u.nodeUC.AddUserViaAgent(ctx, node, inbound.Tag, acc.Email, acc.UUID, string(pType), acc.Flow, acc.Encryption)
					} else {
						xUser := &xray.User{
							Email:    acc.Email,
							UUID:     acc.UUID,
							Protocol: pType,
							Level:    0,
							Flow:     acc.Flow, // Use flow from account record
						}
						target := fmt.Sprintf("%s:%d", node.IP, node.APIPort)
						err = u.grpcClient.AddUser(ctx, target, inbound.Tag, xUser)
					}

					if err == nil {
						stats.MissingAdded++
						log.Debugf("Reconcile: Added missing user %s to %s/%s", email, node.Name, inbound.Tag)
					} else {
						stats.Errors++
						log.WithError(err).WithFields(map[string]interface{}{
							"email":       email,
							"node":        node.Name,
							"inbound_tag": inbound.Tag,
						}).Warn("[ReconcileUsers] Failed to add missing user")
					}
				}
			}
		}
	}

	log.WithFields(map[string]interface{}{
		"ghosts_removed": stats.GhostsRemoved,
		"missing_added":  stats.MissingAdded,
		"errors":         stats.Errors,
		"total_db_users": stats.TotalDBUsers,
		"total_xray":     stats.TotalXrayUsers,
	}).Info("[ReconcileUsers] Reconciliation completed")

	return stats, nil
}

// protocolSupportsUserManagement reports whether xray's API exposes a
// UserManager for the given inbound protocol. SOCKS, HTTP and
// dokodemo-door use static admin-defined creds in the inbound config —
// xray returns "proxy is not a UserManager" when ListUsers is called
// against them, which spams the log every reconcile pass.
//
// Mirrors the canonical non-user-managed set already used in
// pkg/xray/full_config.go and internal/node/usecase/node_inbounds.go.
func protocolSupportsUserManagement(protocol string) bool {
	switch protocol {
	case "socks", "http", "dokodemo-door", "wireguard":
		return false
	}
	return true
}
