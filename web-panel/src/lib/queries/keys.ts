// Query Key Factory for TanStack Query
// Centralized query keys for consistent cache invalidation

export const queryKeys = {
    // Dashboard
    dashboard: ['dashboard'] as const,
    dashboardStats: () => [...queryKeys.dashboard, 'stats'] as const,
    xrayStats: () => [...queryKeys.dashboard, 'xray'] as const,
    onlineUsers: () => [...queryKeys.dashboard, 'online'] as const,
    onlineUsersWithIPs: () => [...queryKeys.dashboard, 'online-ips'] as const,
    onlineUsersHistory: (minutes: number) => [...queryKeys.dashboard, 'online-users-history', minutes] as const,

    // Users
    users: ['users'] as const,
    userList: (filters: object) => [...queryKeys.users, 'list', filters] as const,
    userDetails: (id: number) => [...queryKeys.users, 'details', id] as const,
    userSubscriptions: (id: number) => [...queryKeys.users, 'subscriptions', id] as const,

    // Nodes
    nodes: ['nodes'] as const,
    nodeList: () => [...queryKeys.nodes, 'list'] as const,
    nodeDetails: (id: number) => [...queryKeys.nodes, 'details', id] as const,
    nodeStats: (id: number) => [...queryKeys.nodes, 'stats', id] as const,
    nodeStatsBulk: (ids?: number[]) => [...queryKeys.nodes, 'stats_bulk', ids ?? 'all'] as const,
    // Prefix matcher for all bulk-stats variants (different sorted id lists).
    nodeStatsBulkAll: () => [...queryKeys.nodes, 'stats_bulk'] as const,
    nodeStatsHistory: (id: number) => [...queryKeys.nodes, 'stats_history', id] as const,
    nodeStatsHistoryBulk: (ids?: number[], limit?: number) =>
        [...queryKeys.nodes, 'stats_history_bulk', ids ?? 'all', limit ?? 'default'] as const,
    nodeStatsHistoryBulkAll: () => [...queryKeys.nodes, 'stats_history_bulk'] as const,
    nodeInbounds: (id: number) => [...queryKeys.nodes, 'inbounds', id] as const,
    nodeOutbounds: (id: number) => [...queryKeys.nodes, 'outbounds', id] as const,
    nodeRouting: (id: number) => [...queryKeys.nodes, 'routing', id] as const,
    nodeReverseProxies: (id: number) => [...queryKeys.nodes, 'reverse-proxies', id] as const,
    nodeBalancingRules: (id: number) => [...queryKeys.nodes, 'balancing-rules', id] as const,
    nodeXrayConfigDiff: (id: number) => [...queryKeys.nodes, 'xray-config-diff', id] as const,
    nodeAccessLogs: (id: number, email?: string) => [...queryKeys.nodes, 'access-logs', id, email] as const,
    starlinkStatus: (id: number) => [...queryKeys.nodes, 'starlink-status', id] as const,
    starlinkMap: (id: number) => [...queryKeys.nodes, 'starlink-map', id] as const,
    starlinkHistory: (id: number, timeRange: string) => [...queryKeys.nodes, 'starlink-history', id, timeRange] as const,

    // Hosts
    hosts: ['hosts'] as const,
    hostList: (filters: object) => [...queryKeys.hosts, 'list', filters] as const,
    hostTags: () => [...queryKeys.hosts, 'tags'] as const,
    hostTemplates: ['hostTemplates'] as const,
    hostTemplateList: () => [...queryKeys.hostTemplates, 'list'] as const,

    // Accounts
    accounts: ['accounts'] as const,
    accountList: (filters: object) => [...queryKeys.accounts, 'list', filters] as const,
    accountDetails: (id: number) => [...queryKeys.accounts, 'details', id] as const,
    accountCounts: () => [...queryKeys.accounts, 'counts'] as const,

    // Subscriptions
    subscriptions: ['subscriptions'] as const,
    subscriptionList: (filters: object) => [...queryKeys.subscriptions, 'list', filters] as const,
    subscriptionDetails: (id: number) => [...queryKeys.subscriptions, 'details', id] as const,
    subscriptionCounts: () => [...queryKeys.subscriptions, 'counts'] as const,
    subscriptionAccessHistory: (id: number, from: string, to: string, granularity: string, includeIps: boolean) =>
        [...queryKeys.subscriptions, 'access-history', id, from, to, granularity, includeIps] as const,
    subscriptionAccessSearch: (id: number, from: string, to: string, q: string, kinds: string[], includeIps: boolean) =>
        [...queryKeys.subscriptions, 'access-history-search', id, from, to, q, kinds.slice().sort().join(','), includeIps] as const,
    accessHistoryGlobalSearch: (
        from: string,
        to: string,
        q: string,
        kinds: string[],
        nodeIds: number[],
        subscriptionIds: number[],
        emails: string[],
        includeIps: boolean,
        limit: number,
    ) =>
        [
            'access-history',
            'global-search',
            from,
            to,
            q,
            kinds.slice().sort().join(','),
            nodeIds.slice().sort().join(','),
            subscriptionIds.slice().sort().join(','),
            emails.slice().sort().join(','),
            includeIps,
            limit,
        ] as const,

    // Settings
    settings: ['settings'] as const,
    retentionStats: () => ['settings', 'retention-stats'] as const,

    // Certificates
    certificates: ['certificates'] as const,
    ca: () => [...queryKeys.certificates, 'ca'] as const,
    masterCert: () => [...queryKeys.certificates, 'master'] as const,
    agentCert: (nodeId: number) => [...queryKeys.certificates, 'agent', nodeId] as const,
    certBundle: (nodeId: number) => [...queryKeys.certificates, 'bundle', nodeId] as const,

    // SNI (Domain TLS Certificates)
    sni: ['sni'] as const,
    sniList: () => [...queryKeys.sni, 'list'] as const,
    sniDetails: (id: number) => [...queryKeys.sni, 'details', id] as const,

    // Analytics
    analytics: ['analytics'] as const,
    peakHours: (days: number, nodeIds?: number[]) => [...queryKeys.analytics, 'peak-hours', days, nodeIds] as const,
    blockedDomains: (params: object) => [...queryKeys.analytics, 'blocked-domains', params] as const,
    userUsagePattern: (userId: number, days: number) => [...queryKeys.users, 'usage-pattern', userId, days] as const,
    exhaustionPrediction: (subId: number) => [...queryKeys.subscriptions, 'exhaustion', subId] as const,

    // Access Logs (aggregated)
    accessLogs: ['accessLogs'] as const,
    accessLogList: (params: object) => [...queryKeys.accessLogs, 'list', params] as const,

    // Audit Logs
    auditLogs: (params: object) => ['auditLogs', params] as const,

    // Backups
    backups: ['backups'] as const,
    backupList: () => [...queryKeys.backups, 'list'] as const,

    // Chats
    chats: ['chats'] as const,
    chatList: (filters: object) => [...queryKeys.chats, 'list', filters] as const,
    chatMessages: (subscriptionId: number) => [...queryKeys.chats, 'messages', subscriptionId] as const,
    chatSearch: (subscriptionId: number, q: string) => [...queryKeys.chats, 'search', subscriptionId, q] as const,
    chatUnreadCount: () => [...queryKeys.chats, 'unread-count'] as const,
    chatPinned: (subscriptionId: number) => [...queryKeys.chats, 'pinned', subscriptionId] as const,
    chatReactions: (messageId: number) => [...queryKeys.chats, 'reactions', messageId] as const,

    // Sub panel chat
    subChat: (uuid: string) => ['sub-chat', uuid] as const,

    // Network / router mode
    network: ['network'] as const,
    networkInterfaces: () => [...queryKeys.network, 'interfaces'] as const,
    networkState: () => [...queryKeys.network, 'state'] as const,
    networkLAN: () => [...queryKeys.network, 'lan'] as const,
    networkLANDevices: () => [...queryKeys.network, 'lan', 'devices'] as const,
    networkPortForwards: () => [...queryKeys.network, 'port-forwards'] as const,
    networkVPNProfiles: () => [...queryKeys.network, 'vpn', 'profiles'] as const,
    networkVPNStatus: () => [...queryKeys.network, 'vpn', 'status'] as const,
    networkFlow: () => [...queryKeys.network, 'flow'] as const,
    networkFlowConns: () => [...queryKeys.network, 'flow', 'conns'] as const,
    networkFlowEvents: () => [...queryKeys.network, 'flow', 'events'] as const,
}
