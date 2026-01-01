// Query Key Factory
export { queryKeys } from "./keys"

// Dashboard Queries
export {
    useDashboardStats,
    useOnlineUsers,
    useOnlineUsersWithIPs,
    useNodesSummary,
} from "./use-dashboard"

// Node Queries & Mutations
export {
    useNodes,
    useNode,
    useNodeStats,
    useNodesStatsBulk,
    useNodeStatsHistory,
    useNodeSparkline,
    useNodesStatsHistoryBulk,
    sparklineFromBulk,
    useNodeHostInfo,
    useNodeInbounds,
    useNodeOutbounds,
    useNodeRouting,
    useCreateNode,
    useUpdateNode,
    useDeleteNode,
    useCheckNodeHealth,
    useBulkRestartNodes,
    useBulkPushNodeConfig,
    useBulkCheckNodeHealth,
    useBulkUpdateXrayVersion,
    useNodeXrayConfigDiff,
    useDiscoverInbounds,
    useSyncInbounds,
    useAddInbound,
    useUpdateInbound,
    useDeleteInbound,
    useAccessLogs,
} from "./use-nodes"

// User Queries & Mutations
export {
    useUsers,
    useUserDetails,
    useUserSubscriptions,
    useBanUser,
    useToggleAdmin,
    useBulkBan,
    useBulkUnban,
    useUpdateTelegramID,
    useCreateUser,
    useUserUsageHistory,
    useUserActivity,
    useUserActivityInfinite,
    useUserAccounts,
    useUpdateUserNotes,
} from "./use-users"
export type { UseUsersParams } from "./use-users"

// Subscription Queries & Mutations
export {
    useSubscriptions,
    useSubscription,
    useExtendSubscription,
    usePauseSubscription,
    useResumeSubscription,
    useRevokeSubscription,
    useSetDataLimit,
    useSetBandwidthLimit,
    useSetMaxDevices,
    useSetExpiry,
    useAddData,
    useResetData,
    useRenameSubscription,
    useRegenerateSubscriptionKey,
    useRegenerateUUID,
    useCreateManualSubscription,
    useAssignSubscriptionUser,
    useDeleteSubscription,
    useBulkSubscriptionAction,
    useBulkSetBandwidthLimit,
    useSubscriptionCounts,
    useSetPanelPassword,
    useSubscriptionIPs,
    useSubscriptionUsageHistory,
    useSetSubscriptionUUID,
} from "./use-subscriptions"
export type { UseSubscriptionsParams } from "./use-subscriptions"

// Settings Queries & Mutations
export {
    useSettings,
    useUpdateSettings,
    useExportSettings,
    useImportSettings,
    useChangePassword,
} from "./use-settings"

// Certificate Queries & Mutations
export {
    useCA,
    useHasCA,
    useCertificates,
    useMasterCert,
    useAgentCert,
    useCertificateDetails,
    useCertBundle,
    useExpiringSoonCerts,
    useInitializeCA,
    useGenerateMasterCert,
    useGenerateAgentCert,
    useRegenerateAgentCert,
    useRevokeCertificate,
    useRenewCertificate,
    useDeleteCertificate,
    useIssuePublicCertificate,
    useStartDNSChallenge,
    useCompleteDNSChallenge,
    useToggleAutoRenew,
} from "./use-certificates"

// Host Queries & Mutations
export {
    useHostList,
    useHostTags,
    useHostTemplates,
    useCreateHostMutation,
    useUpdateHostMutation,
    useDeleteHostMutation,
    useDuplicateHostMutation,
    useToggleHostMutation,
    useBulkUpdateHostsMutation,
    useCreateHostTemplateMutation,
    useUpdateHostTemplateMutation,
    useDeleteHostTemplateMutation,
    useApplyHostTemplateMutation,
} from "./use-hosts"
export type { UseHostListParams } from "./use-hosts"

// Account List Queries & Mutations
export {
    useAccountList,
    useAccountCounts,
    useDeleteAccountMutation,
    useDisableAccountMutation,
    useEnableAccountMutation,
    useSyncAccountMutation,
    useCopyAccountLink,
    useUpdateAccountMutation,
    useBulkAccountAction,
} from "./use-account-list"
export type { UseAccountListParams, BulkAccountAction } from "./use-account-list"

// Access Logs (aggregated)
export { useAggregatedAccessLogs, useAccessLogAnalytics, useAccessLogTopDomains } from "./use-access-logs"
export type { UseAccessLogsParams } from "./use-access-logs"

// Per-subscription access history (date-ranged)
export {
    useSubscriptionAccessHistory,
    useSubscriptionAccessSearch,
    useGlobalAccessHistorySearch,
} from "./use-access-history"

// Audit Queries
export { useAuditLogs } from "./use-audit"

// Backup Queries & Mutations
export {
    useBackups,
    useCreateBackup,
    useDeleteBackup,
    useRestoreBackup,
    useRestoreFromExisting,
} from "./use-backup"

// Dashboard Widget Queries (new)
export {
    useNodeAggregateStats,
    useUserActivityHeatmap,
} from "./use-dashboard-widgets"

// Database Cleanup
export { useDatabaseCleanup } from "./use-cleanup"

// SNI (Domain TLS Certificates) Queries & Mutations
export {
    useSNIs,
    useSNI,
    useCreateSNI,
    useCreateSNIWithPaths,
    useUpdateSNI,
    useDeleteSNI,
    useValidateSNICert,
    useRenewSNICert,
    useSNIUsage,
    useIssueSNIHTTP01,
    useStartSNIDNS01,
    useCompleteSNIDNS01,
} from "./use-sni"

// Alerting Queries & Mutations
export {
    useAlertRules,
    useAlertEvents,
    useSetAlertRuleEnabled,
    useSetAlertRuleThreshold,
    useTestAlertRule,
} from "./use-alerts"
