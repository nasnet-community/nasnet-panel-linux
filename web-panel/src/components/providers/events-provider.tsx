import { createContext, useContext, useCallback, useState, useEffect, useRef } from 'react'
import { toast } from 'sonner'
import { getApiBaseUrl } from '@/lib/config'
import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import type {
    EventType,
    ServerEvent,
    NodeStatusPayload,
    NodeStatsPayload,
    SubscriptionEventPayload,
    SystemAlertPayload,
    XrayRecoveryPayload,
} from '@/lib/hooks/use-events'
import { queryKeys } from '@/lib/queries/keys'
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

// ─── Debounced invalidation ─────────────────────────────────────────────────
// Coalesces rapid-fire invalidation requests (e.g. node.stats_updated arriving
// from many nodes within seconds) into a single invalidateQueries call per
// query-key prefix.  This avoids hammering the network with redundant refetches.

type DebouncedInvalidator = {
    timers: Map<string, NodeJS.Timeout>
    schedule: (queryClient: QueryClient, queryKey: readonly unknown[], delayMs: number) => void
    cleanup: () => void
}

function createDebouncedInvalidator(): DebouncedInvalidator {
    const timers = new Map<string, NodeJS.Timeout>()

    return {
        timers,
        schedule(queryClient, queryKey, delayMs) {
            const key = JSON.stringify(queryKey)
            const existing = timers.get(key)
            if (existing) clearTimeout(existing)
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
    const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null)
    const isConnectingRef = useRef(false)

    // Use ref for queryClient to avoid dependency changes
    const queryClientRef = useRef(queryClient)
    queryClientRef.current = queryClient

    // Store active toast IDs to dismiss them when resolved
    const activeToastsRef = useRef<Map<number, string | number>>(new Map())

    // Debounced invalidator for high-frequency events (node.stats_updated)
    const debouncerRef = useRef<DebouncedInvalidator>(createDebouncedInvalidator())

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
        }

        // Handle all event types with inline handlers to avoid dependency issues
        const eventTypes: EventType[] = [
            'node.online', 'node.offline', 'node.stats_updated',
            'payment.created', 'payment.completed', 'payment.failed',
            'subscription.created', 'subscription.expiring', 'subscription.expired',
            'system.alert',
            'xray.recovery_command', 'xray.recovery_exhausted',
            // Named events only arrive if we ask for them by name.
            'interface.added', 'interface.removed', 'interface.link_changed',
            'wan.up', 'wan.down', 'wan.failover', 'wan.failover_lost',
            'wan.failover_restored', 'wan.degraded', 'wan.force_state',
            'wan.applied', 'wan.apply_rolled_back', 'wan.lease_warning',
            'vpn.up', 'vpn.down', 'vpn.degraded', 'vpn.pool_changed', 'vpn.tunnel_rehomed',
        ]

        eventTypes.forEach((eventType) => {
            es.addEventListener(eventType, (e) => {
                try {
                    const event: ServerEvent = JSON.parse(e.data)
                    handleEvent(eventType, event, queryClientRef.current, activeToastsRef.current, debouncerRef.current)
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
            debouncerRef.current.cleanup()
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

function handleEvent(
    eventType: EventType,
    event: ServerEvent,
    queryClient: QueryClient,
    activeToasts: Map<number, string | number>,
    debouncer: DebouncedInvalidator,
) {
    switch (eventType) {
        case 'node.online': {
            const payload = event.payload as NodeStatusPayload
            toast.success(`Node "${payload.node_name}" is back online`, {
                description: `IP: ${payload.ip}`,
                duration: 5000,
            })
            // Node status changed → refresh node lists, detail, and stats
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            // Dashboard aggregates depend on which nodes are online
            queryClient.invalidateQueries({ queryKey: queryKeys.dashboard })
            // Account online indicators may change
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            break
        }
        case 'node.offline': {
            const payload = event.payload as NodeStatusPayload
            toast.error(`Node "${payload.node_name}" went offline`, {
                description: payload.message || `IP: ${payload.ip}`,
                duration: 8000,
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            queryClient.invalidateQueries({ queryKey: queryKeys.dashboard })
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts })
            break
        }
        case 'node.stats_updated': {
            const payload = event.payload as NodeStatsPayload
            // Optimistically update individual node stats cache (no network round-trip).
            // Also patches every matching stats_bulk cache entry (keyed by sorted id
            // lists) so the Nodes-page grid + dashboard aggregate — both of which
            // read from the bulk cache — go live without a refetch.
            queryClient.setQueryData(queryKeys.nodeStats(payload.node_id), (oldData: any) => {
                if (!oldData) return undefined
                return {
                    ...oldData,
                    online_users: payload.online_users,
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
                    xray_status: payload.xray_status,
                    xray_pid: payload.xray_pid,
                    agent_version: payload.agent_version,
                    xray_version: payload.xray_version,
                    process_uptime: payload.uptime > 0 ? payload.uptime : oldData.process_uptime,
                    total_accounts: payload.total_accounts,
                    active_accounts: payload.active_accounts,
                    // Inject traffic totals so dashboard aggregates pick them up
                    total_uplink: payload.total_uplink ?? oldData.total_uplink,
                    total_downlink: payload.total_downlink ?? oldData.total_downlink,
                    load_avg_1: payload.load_avg_1,
                    load_avg_5: payload.load_avg_5,
                    load_avg_15: payload.load_avg_15,
                }
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
                            stats: {
                                ...entry.stats,
                                online_users: payload.online_users,
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
                                xray_status: payload.xray_status,
                                xray_pid: payload.xray_pid,
                                agent_version: payload.agent_version,
                                xray_version: payload.xray_version,
                                total_accounts: payload.total_accounts,
                                active_accounts: payload.active_accounts,
                                total_uplink: payload.total_uplink ?? entry.stats.total_uplink,
                                total_downlink: payload.total_downlink ?? entry.stats.total_downlink,
                                load_avg_1: payload.load_avg_1,
                                load_avg_5: payload.load_avg_5,
                                load_avg_15: payload.load_avg_15,
                            },
                        },
                    }
                },
            )

            // Dashboard widgets that are *not* derived from bulk cache still
            // need a poke (e.g. dashboard/* aggregates that read other keys).
            // Debounced to coalesce N-nodes-in-burst.
            debouncer.schedule(queryClient, queryKeys.dashboard, 3000)
            break
        }
        case 'subscription.created': {
            const payload = event.payload as SubscriptionEventPayload
            toast.success('New subscription created!', {
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
        case 'system.alert': {
            const payload = event.payload as SystemAlertPayload
            const toastFn = payload.level === 'error' ? toast.error : payload.level === 'warning' ? toast.warning : toast.info
            toastFn(payload.title, { description: payload.message, duration: 10000 })
            break
        }
        case 'xray.recovery_command': {
            const payload = event.payload as XrayRecoveryPayload
            const toastFn = payload.success ? toast.success : toast.error
            const maxLabel = payload.max_attempts === 0 ? '\u221e' : String(payload.max_attempts)
            toastFn(`Recovery command ${payload.success ? 'succeeded' : 'failed'} on "${payload.node_name}"`, {
                description: `Exit code: ${payload.exit_code} | Attempt ${payload.attempt_num}/${maxLabel}`,
                duration: 8000,
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            break
        }
        case 'xray.recovery_exhausted': {
            const payload = event.payload as XrayRecoveryPayload
            toast.error(`Recovery exhausted on "${payload.node_name}"`, {
                description: `${payload.attempt_num} attempt(s) used. Manual intervention required.`,
                duration: 10000,
            })
            queryClient.invalidateQueries({ queryKey: queryKeys.nodes })
            break
        }
    }
}
