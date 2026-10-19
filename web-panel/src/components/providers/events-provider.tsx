import { createContext, useContext, useCallback, useState, useEffect, useLayoutEffect, useRef } from 'react'
import { toast } from "@/lib/toast"
import { getApiBaseUrl } from '@/lib/config'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useNavigate, type NavigateFunction } from 'react-router'
import { subscriptionCreatedHereRecently } from '@/components/toast/handled-here'
import type {
    EventType,
    ServerEvent,
    NodeStatusPayload,
    PortMapPayload,
    NodeStatsPayload,
    SubscriptionEventPayload,
    SystemAlertPayload,
} from '@/lib/hooks/use-events'
import { queryKeys } from '@/lib/queries/keys'
import type { NodeStats } from '@/lib/types'
import type { NodeStatsBulkMap } from '@/lib/api/nodes'

// Connection status type
export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'error'

// Listener for raw events — components subscribe via useEventListener()
type RawEventListener = (eventType: EventType, event: ServerEvent) => void

interface EventsContextValue {
    status: ConnectionStatus
    /** Subscribe to raw SSE events. Returns an unsubscribe function. */
    subscribe: (listener: RawEventListener) => () => void
}

const EventsContext = createContext<EventsContextValue>({
    status: 'disconnected',
    subscribe: () => () => {},
})

export function useEventsStatus() {
    return useContext(EventsContext).status
}

/** Subscribe to raw SSE events; auto-cleanup on unmount. */
export function useEventListener(listener: RawEventListener) {
    const { subscribe } = useContext(EventsContext)
    const listenerRef = useRef(listener)
    listenerRef.current = listener

    useEffect(() => {
        // Wrap in stable ref so re-renders don't cause unsub/resub
        const stable: RawEventListener = (t, e) => listenerRef.current(t, e)
        return subscribe(stable)
    }, [subscribe])
}

interface EventsProviderProps {
    children: React.ReactNode
}

// ─── Coalesced invalidation ─────────────────────────────────────────────────
// Coalesces rapid-fire invalidation requests (e.g. node.stats_updated arriving
// from many nodes within seconds) into a single invalidateQueries call per
// query-key prefix. Later events share the first timer so a steady stream
// cannot indefinitely postpone the refresh.

type CoalescedInvalidator = {
    timers: Map<string, ReturnType<typeof setTimeout>>
    schedule: (queryClient: QueryClient, queryKey: readonly unknown[], delayMs: number) => void
    cleanup: () => void
}

function createCoalescedInvalidator(): CoalescedInvalidator {
    const timers = new Map<string, ReturnType<typeof setTimeout>>()

    return {
        timers,
        schedule(queryClient, queryKey, delayMs) {
            const key = JSON.stringify(queryKey)
            if (timers.has(key)) return
            timers.set(key, setTimeout(() => {
                timers.delete(key)
                queryClient.invalidateQueries({ queryKey })
            }, delayMs))
        },
        cleanup() {
            timers.forEach((t) => clearTimeout(t))
            timers.clear()
        },
    }
}

/**
 * Global provider for handling real-time server events.
 * Displays toast notifications and invalidates relevant queries.
 */
export function EventsProvider({ children }: EventsProviderProps) {
    const queryClient = useQueryClient()
    const [status, setStatus] = useState<ConnectionStatus>('disconnected')
    const eventSourceRef = useRef<EventSource | null>(null)
    const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
    const isConnectingRef = useRef(false)

    // Use ref for queryClient to avoid dependency changes
    const queryClientRef = useRef(queryClient)
    queryClientRef.current = queryClient

    // Toast actions navigate; the ref keeps connect() stable across renders.
    const navigate = useNavigate()
    const navigateRef = useRef<NavigateFunction>(navigate)
    useLayoutEffect(() => { navigateRef.current = navigate }, [navigate])

    // Debounced invalidator for high-frequency events (node.stats_updated)
    const invalidatorRef = useRef<CoalescedInvalidator>(createCoalescedInvalidator())

    // Raw event listeners — components subscribe via useEventListener()
    const listenersRef = useRef<Set<RawEventListener>>(new Set())
    const subscribe = useCallback((listener: RawEventListener) => {
        listenersRef.current.add(listener)
        return () => { listenersRef.current.delete(listener) }
    }, [])

    // Connect to event stream - stable function, no dependencies that change
    const connect = useCallback(() => {
        // Prevent multiple simultaneous connection attempts
        if (isConnectingRef.current || eventSourceRef.current?.readyState === EventSource.OPEN) {
            return
        }

        // Cleanup existing connection
        if (eventSourceRef.current) {
            eventSourceRef.current.close()
            eventSourceRef.current = null
        }
        if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current)
            reconnectTimeoutRef.current = null
        }

        isConnectingRef.current = true
        setStatus('connecting')

        const url = `${getApiBaseUrl()}/api/v1/events/stream`
        const es = new EventSource(url, { withCredentials: true })

        es.onopen = () => {
            isConnectingRef.current = false
            setStatus('connected')
            // Reconnect catch-up: list + bulk-stats state may have shifted
            // during the disconnect window. Refetch both so the UI reflects
            // any node online/offline flips we missed before live SSE
            // patches resume.
            queryClientRef.current.invalidateQueries({ queryKey: queryKeys.nodeList() })
            queryClientRef.current.invalidateQueries({ queryKey: queryKeys.nodeStatsBulkAll(), exact: false })
            queryClientRef.current.invalidateQueries({ queryKey: queryKeys.dashboard })
            queryClientRef.current.invalidateQueries({ queryKey: queryKeys.subscriptions })
            queryClientRef.current.invalidateQueries({ queryKey: [...queryKeys.users, 'list'] })
        }

        // Handle all event types with inline handlers to avoid dependency issues
        const eventTypes: EventType[] = [
            'node.online', 'node.offline', 'node.stats_updated', 'node.updated',
            'subscription.created', 'subscription.expiring', 'subscription.expired',
            'system.alert',
            'user.registered', 'subscription.renewed', 'subscription.cancelled', 'account.data_exhausted',
            'reverse_tunnel.down', 'reverse_tunnel.up',
            // Named events only arrive if we ask for them by name.
            'interface.added', 'interface.removed', 'interface.link_changed',
            'wan.up', 'wan.down', 'wan.failover', 'wan.failover_lost',
            'wan.failover_restored', 'wan.degraded', 'wan.force_state',
            'wan.applied', 'wan.apply_rolled_back', 'wan.lease_warning',
            'vpn.up', 'vpn.down', 'vpn.degraded', 'vpn.pool_changed', 'vpn.tunnel_rehomed',
            'wan.gateway_changed',
            'portmap.acquired', 'portmap.lost', 'portmap.denied',
        ]

        eventTypes.forEach((eventType) => {
            es.addEventListener(eventType, (e) => {
                try {
                    const event: ServerEvent = JSON.parse(e.data)
                    handleEvent(eventType, event, queryClientRef.current, navigateRef.current, invalidatorRef.current)
                    // Notify raw listeners (ActivityFeed, etc.)
                    listenersRef.current.forEach((fn) => fn(eventType, event))
                } catch (error) {
                    console.error(`[Events] Failed to parse ${eventType} event:`, error)
                }
            })
        })

        es.onerror = () => {
            isConnectingRef.current = false
            setStatus('error')
            es.close()
            eventSourceRef.current = null

            // Auto-reconnect after delay
            reconnectTimeoutRef.current = setTimeout(() => {
                connect()
            }, 5000) // 5 second delay
        }

        eventSourceRef.current = es
    }, []) // No dependencies - uses refs

    // Connect on mount only
    useEffect(() => {
        connect()

        return () => {
            invalidatorRef.current.cleanup()
            if (reconnectTimeoutRef.current) {
                clearTimeout(reconnectTimeoutRef.current)
            }
            if (eventSourceRef.current) {
                eventSourceRef.current.close()
            }
        }
    }, [connect])

    return (
        <EventsContext.Provider value={{ status, subscribe }}>
            {children}
        </EventsContext.Provider>
    )
}

// handleEvent: toast + cache invalidation cascade. See call sites for the
// per-event invalidation lists.

const liveDashboardKeys = [queryKeys.dashboardStats(), queryKeys.onlineUsers(), queryKeys.onlineUsersWithIPs()]

function invalidateLiveDashboard(queryClient: QueryClient) {
    for (const queryKey of liveDashboardKeys) queryClient.invalidateQueries({ queryKey })
}

// Availability is explicit: zero is a legitimate reading after recovery.
function mergeNodeStats(old: NodeStats, payload: NodeStatsPayload): NodeStats {
    // A sweep that started before a live read can land after it; keep the newer.
    if (payload.sampled_at && old.sampled_at && Date.parse(payload.sampled_at) < Date.parse(old.sampled_at)) {
        return old
    }
    const next = {
        ...old,
        stats_unavailable: payload.stats_unavailable === true,
        online_users: payload.online_users,
        total_uplink: payload.total_uplink ?? old.total_uplink,
        total_downlink: payload.total_downlink ?? old.total_downlink,
        total_accounts: payload.total_accounts,
        active_accounts: payload.active_accounts,
        agent_version: payload.agent_version || old.agent_version,
        xray_version: payload.xray_version || old.xray_version,
        ...(payload.xray_status ? {
            xray_status: payload.xray_status,
            xray_pid: payload.xray_pid,
            process_uptime: payload.uptime,
        } : {}),
    }
    if (payload.stats_unavailable) return next
    return {
        ...next,
        sampled_at: payload.sampled_at ?? old.sampled_at,
        received_at: Date.now(),
        cpu_percent: payload.cpu_percent,
        memory_percent: payload.memory_percent,
        disk_percent: payload.disk_percent,
        up_speed: payload.up_speed,
        down_speed: payload.down_speed,
        tcp_count: payload.tcp_count,
        udp_count: payload.udp_count,
        fd_count: payload.fd_count,
        memory_used_mb: payload.memory_used_mb,
        memory_total_mb: payload.memory_total_mb,
        disk_used_gb: payload.disk_used_gb,
        disk_total_gb: payload.disk_total_gb,
        system_uptime: payload.system_uptime,
        load_avg_1: payload.load_avg_1,
        load_avg_5: payload.load_avg_5,
        load_avg_15: payload.load_avg_15,
    }
}

// Events that change who belongs in which users-list group.
const USERS_LIST_EVENTS = new Set<EventType>([
    'user.registered', 'subscription.created', 'subscription.expired',
    'subscription.renewed', 'subscription.cancelled', 'account.data_exhausted',
])

function handleEvent(
    eventType: EventType,
    event: ServerEvent,
    queryClient: QueryClient,
    navigate: NavigateFunction,
    invalidator: CoalescedInvalidator,
) {
    if (USERS_LIST_EVENTS.has(eventType)) {
        invalidator.schedule(queryClient, [...queryKeys.users, 'list'], 1000)
    }
    switch (eventType) {
        case 'node.online': {
            const payload = event.payload as NodeStatusPayload
            toast.success(`Node "${payload.node_name}" is back online`, {
                id: `node-${payload.node_id}`,
                description: `IP: ${payload.ip}`,
                action: { label: 'View node', onClick: () => navigate(`/nodes/${payload.node_id}`) },
            })
            // Node status changed → refresh node lists, detail, and stats
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            // Dashboard aggregates depend on which nodes are online
            invalidateLiveDashboard(queryClient)
            // Account online indicators may change
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            break
        }
        case 'node.offline': {
            const payload = event.payload as NodeStatusPayload
            toast.error(`Node "${payload.node_name}" went offline`, {
                id: `node-${payload.node_id}`,
                description: payload.message || `IP: ${payload.ip}`,
                action: { label: 'View node', onClick: () => navigate(`/nodes/${payload.node_id}`) },
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            invalidateLiveDashboard(queryClient)
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            break
        }
        case 'reverse_tunnel.down':
        case 'reverse_tunnel.up': {
            // A portal saw its bridge drop or connect. Alerts come through
            // system.alert after the grace period; this only refreshes status.
            queryClient.invalidateQueries({ queryKey: queryKeys.reverseTunnels() })
            break
        }
        case 'node.updated': {
            // An edit landed from another tab or admin: refetch, no toast (the
            // editor already saw their own save).
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            break
        }
        case 'node.stats_updated': {
            const payload = event.payload as NodeStatsPayload
            // Optimistically update individual node stats cache (no network round-trip).
            // Also patches every matching stats_bulk cache entry (keyed by sorted id
            // lists) so the Nodes-page grid + dashboard aggregate — both of which
            // read from the bulk cache — go live without a refetch.
            queryClient.setQueryData<NodeStats>(queryKeys.nodeStats(payload.node_id), (old) => {
                return old ? mergeNodeStats(old, payload) : undefined
            })

            // Patch every bulk-stats cache variant that contains this node.
            queryClient.setQueriesData<NodeStatsBulkMap>(
                { queryKey: queryKeys.nodeStatsBulkAll(), exact: false },
                (old) => {
                    if (!old) return old
                    const key = String(payload.node_id)
                    const entry = old[key]
                    if (!entry?.stats) return old
                    return {
                        ...old,
                        [key]: {
                            ...entry,
                            stats: mergeNodeStats(entry.stats, payload),
                            error: payload.stats_unavailable ? entry.error : undefined,
                        },
                    }
                },
            )

            // Dashboard widgets that are *not* derived from bulk cache still
            // need a poke (e.g. dashboard/* aggregates that read other keys).
            // Debounced to coalesce N-nodes-in-burst.
            for (const queryKey of liveDashboardKeys) invalidator.schedule(queryClient, queryKey, 3000)
            break
        }
        case 'subscription.created': {
            const payload = event.payload as SubscriptionEventPayload
            // A subscription this panel just created already got its own toast.
            if (!subscriptionCreatedHereRecently()) toast.success('New subscription created!', {
                description: `${payload.plan_name}`,
            })
            // New sub → sub lists, counts, dashboard active-subs count,
            // accounts (provisioned), user detail (active_subscriptions count)
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            queryClient.invalidateQueries({ queryKey: queryKeys.dashboardStats() })
            if (payload.user_id) {
                queryClient.invalidateQueries({ queryKey: queryKeys.userDetails(payload.user_id) })
                queryClient.invalidateQueries({ queryKey: queryKeys.userSubscriptions(payload.user_id) })
            }
            break
        }
        case 'subscription.expiring': {
            const payload = event.payload as SubscriptionEventPayload
            toast.warning('Subscription expiring soon', {
                description: `${payload.plan_name} expires at ${payload.expires_at}`,
                duration: 8000,
            })
            break
        }
        case 'subscription.expired': {
            const payload = event.payload as SubscriptionEventPayload
            // Expired sub → status changed, accounts may be disabled,
            // dashboard active-subs count changed
            queryClient.invalidateQueries({ queryKey: queryKeys.subscriptions })
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            queryClient.invalidateQueries({ queryKey: queryKeys.dashboardStats() })
            if (payload.user_id) {
                queryClient.invalidateQueries({ queryKey: queryKeys.userDetails(payload.user_id) })
                queryClient.invalidateQueries({ queryKey: queryKeys.userSubscriptions(payload.user_id) })
            }
            break
        }
        case 'portmap.acquired': {
            const p = event.payload as PortMapPayload
            toast.success(`${p.proto}/${p.port} open from outside`, {
                id: `portmap-${p.proto}-${p.port}`,
                description: p.external ? `Reachable at ${p.external}` : undefined,
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.networkPortMapStatus() })
            break
        }

        case 'portmap.lost': {
            const p = event.payload as PortMapPayload
            toast.warning(
                p.count ? `${p.count} upstream mappings dropped` : `${p.proto}/${p.port} closed upstream`,
                { id: p.count ? 'portmap-lost' : `portmap-${p.proto}-${p.port}`, description: p.reason || 'Retrying on the next pass.' },
            )
            queryClient.invalidateQueries({ queryKey: queryKeys.networkPortMapStatus() })
            break
        }

        case 'portmap.denied': {
            toast.error('The upstream router refuses port mapping', {
                id: 'portmap-denied',
                description: 'Turn UPnP on there, or forward the ports by hand.',
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.networkPortMapStatus() })
            break
        }

        case 'system.alert': {
            const payload = event.payload as SystemAlertPayload
            const toastFn = payload.level === 'error' ? toast.error : payload.level === 'warning' ? toast.warning : toast.info
            toastFn(payload.title, { description: payload.message, duration: 10000 })
            break
        }
    }
}
