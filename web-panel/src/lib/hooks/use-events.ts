import { useEffect, useRef, useCallback, useState } from 'react'
import { getApiBaseUrl } from '@/lib/config'

// Event types matching backend definitions
export type EventType =
    | 'node.online'
    | 'node.offline'
    | 'node.stats_updated'
    | 'payment.created'
    | 'payment.completed'
    | 'payment.failed'
    | 'subscription.created'
    | 'subscription.expiring'
    | 'subscription.expired'
    | 'system.alert'
    | 'xray.recovery_command'
    | 'xray.recovery_exhausted'
    // Router mode. The backend has always published these; nothing subscribed.
    | 'interface.added'
    | 'interface.removed'
    | 'interface.link_changed'
    | 'wan.up'
    | 'wan.down'
    | 'wan.failover'
    | 'wan.failover_lost'
    | 'wan.failover_restored'
    | 'wan.degraded'
    | 'wan.force_state'
    | 'wan.applied'
    | 'wan.apply_rolled_back'
    | 'wan.lease_warning'
    | 'vpn.up'
    | 'vpn.down'
    | 'vpn.degraded'
    | 'vpn.pool_changed'

// Event payloads
export interface NodeStatusPayload {
    node_id: number
    node_name: string
    ip: string
    is_online: boolean
    message?: string
}

export interface NodeStatsPayload {
    node_id: number
    online_users: number
    total_uplink: number
    total_downlink: number
    cpu_percent: number
    memory_percent: number
    disk_percent: number
    memory_used_mb: number
    memory_total_mb: number
    disk_used_gb: number
    disk_total_gb: number
    up_speed: number
    down_speed: number
    tcp_count: number
    udp_count: number
    fd_count: number
    uptime: number
    system_uptime: number
    xray_status: string
    xray_pid: number
    load_avg_1: number
    load_avg_5: number
    load_avg_15: number
    agent_version: string
    xray_version: string
    total_accounts: number
    active_accounts: number
}

export interface PaymentEventPayload {
    payment_id: number
    transaction_id: string
    user_id: number
    username?: string
    amount: number
    status: string
    plan_name?: string
    method?: string
    crypto_asset?: string
}

export interface SubscriptionEventPayload {
    subscription_id: number
    user_id: number
    username?: string
    plan_name: string
    expires_at?: string
}

export interface SystemAlertPayload {
    level: 'info' | 'warning' | 'error'
    title: string
    message: string
}

export interface XrayRecoveryPayload {
    node_id: number
    node_name: string
    ip: string
    crash_count: number
    attempt_num: number
    max_attempts: number
    command: string
    exit_code: number
    stdout?: string
    stderr?: string
    success: boolean
    error_message?: string
}

// Generic event structure
export interface ServerEvent<T = unknown> {
    type: EventType
    timestamp: string
    payload: T
}

type EventHandler<T = unknown> = (event: ServerEvent<T>) => void
type EventHandlers = Partial<Record<EventType, EventHandler<any>>>

interface UseEventsOptions {
    /** Whether to auto-connect on mount (default: true) */
    autoConnect?: boolean
    /** Reconnect delay in ms (default: 3000) */
    reconnectDelay?: number
}

interface UseEventsReturn {
    /** Current connection status */
    status: 'connecting' | 'connected' | 'disconnected' | 'error'
    /** Manually connect to the event stream */
    connect: () => void
    /** Manually disconnect from the event stream */
    disconnect: () => void
    /** Whether currently connected */
    isConnected: boolean
}

/** Subscribe to SSE server events. handlers maps event type → callback. */
export function useEvents(
    handlers?: EventHandlers,
    options: UseEventsOptions = {}
): UseEventsReturn {
    const { autoConnect = true, reconnectDelay = 3000 } = options

    const [status, setStatus] = useState<UseEventsReturn['status']>('disconnected')
    const eventSourceRef = useRef<EventSource | null>(null)
    const handlersRef = useRef(handlers)
    const reconnectTimeoutRef = useRef<NodeJS.Timeout | null>(null)

    // Keep handlers ref up to date
    useEffect(() => {
        handlersRef.current = handlers
    }, [handlers])

    const disconnect = useCallback(() => {
        if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current)
            reconnectTimeoutRef.current = null
        }
        if (eventSourceRef.current) {
            eventSourceRef.current.close()
            eventSourceRef.current = null
        }
        setStatus('disconnected')
    }, [])

    const connect = useCallback(() => {
        // Clean up existing connection
        if (eventSourceRef.current) {
            eventSourceRef.current.close()
        }
        if (reconnectTimeoutRef.current) {
            clearTimeout(reconnectTimeoutRef.current)
            reconnectTimeoutRef.current = null
        }

        setStatus('connecting')

        const url = `${getApiBaseUrl()}/api/v1/events/stream`
        const es = new EventSource(url, { withCredentials: true })

        es.onopen = () => {
            setStatus('connected')
        }

        // Handle the "connected" event from server
        es.addEventListener('connected', (e) => {
            try {
                const data = JSON.parse(e.data)
                console.log('[Events] Connected:', data.subscriber_id)
            } catch {
                // Ignore parse errors
            }
        })

        // Handle all event types
        const eventTypes: EventType[] = [
            'node.online',
            'node.offline',
            'node.stats_updated',
            'payment.created',
            'payment.completed',
            'payment.failed',
            'subscription.created',
            'subscription.expiring',
            'subscription.expired',
            'system.alert',
            'xray.recovery_command',
            'xray.recovery_exhausted',
        ]

        eventTypes.forEach((eventType) => {
            es.addEventListener(eventType, (e) => {
                try {
                    const event: ServerEvent = JSON.parse(e.data)
                    handlersRef.current?.[eventType]?.(event)
                } catch (error) {
                    console.error(`[Events] Failed to parse ${eventType} event:`, error)
                }
            })
        })

        es.onerror = () => {
            setStatus('error')
            es.close()
            eventSourceRef.current = null

            // Auto-reconnect
            reconnectTimeoutRef.current = setTimeout(() => {
                console.log('[Events] Attempting to reconnect...')
                connect()
            }, reconnectDelay)
        }

        eventSourceRef.current = es
    }, [reconnectDelay])

    // Auto-connect on mount
    useEffect(() => {
        if (autoConnect) {
            connect()
        }

        return () => {
            disconnect()
        }
    }, [autoConnect, connect, disconnect])

    return {
        status,
        connect,
        disconnect,
        isConnected: status === 'connected',
    }
}
