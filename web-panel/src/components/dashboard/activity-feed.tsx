import { useState, useCallback, useRef } from "react"
import { WidgetWrapper } from "./widget-wrapper"
import { cn } from "@/lib/utils"
import { Rss } from "lucide-react"
import { useEventListener } from "@/components/providers/events-provider"
import type { EventType, ServerEvent } from "@/lib/hooks/use-events"

interface FeedEvent {
    id: string
    type: string
    message: string
    timestamp: Date
}

const EVENT_COLORS: Record<string, string> = {
    "node.online": "bg-emerald-500",
    "node.offline": "bg-red-500",
    "payment.created": "bg-amber-500",
    "payment.completed": "bg-emerald-500",
    "payment.failed": "bg-red-500",
    "subscription.created": "bg-blue-500",
    "subscription.expired": "bg-amber-500",
    "system.alert": "bg-red-500",
}

const EVENT_LABELS: Record<string, string> = {
    "node.online": "NODE",
    "node.offline": "NODE",
    "payment.created": "PAY",
    "payment.completed": "PAY",
    "payment.failed": "PAY",
    "subscription.created": "SUB",
    "subscription.expired": "SUB",
    "subscription.expiring": "SUB",
    "system.alert": "SYS",
}

function formatTime(date: Date): string {
    return date.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })
}

/** Build a human-readable message from an SSE event. Returns "" for events we skip. */
function eventToMessage(eventType: EventType, event: ServerEvent): string {
    const payload = event.payload as any
    switch (eventType) {
        case "node.online":
            return `Node "${payload.node_name}" came online`
        case "node.offline":
            return `Node "${payload.node_name}" went offline`
        case "node.stats_updated":
            return "" // Too frequent — skip
        case "payment.created":
            return `New payment $${payload.amount} from ${payload.username || "user"}`
        case "payment.completed":
            return `Payment $${payload.amount} completed`
        case "payment.failed":
            return `Payment $${payload.amount} failed`
        case "subscription.created":
            return `New subscription: ${payload.plan_name || "plan"}`
        case "subscription.expiring":
            return `Subscription expiring: ${payload.plan_name || "plan"}`
        case "subscription.expired":
            return `Subscription expired: ${payload.plan_name || "plan"}`
        case "system.alert":
            return payload.title || payload.message || "System alert"
        default:
            return eventType
    }
}

interface ActivityFeedProps {
    isEditMode?: boolean
}

export function ActivityFeed({ isEditMode }: ActivityFeedProps) {
    const [events, setEvents] = useState<FeedEvent[]>([])
    const scrollRef = useRef<HTMLDivElement>(null)

    const addEvent = useCallback((type: string, message: string) => {
        setEvents((prev) => {
            const newEvent: FeedEvent = {
                id: `${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
                type,
                message,
                timestamp: new Date(),
            }
            const updated = [newEvent, ...prev]
            return updated.slice(0, 50) // Keep last 50 events
        })
    }, [])

    // Subscribe to the shared SSE connection from EventsProvider
    useEventListener(useCallback((eventType: EventType, event: ServerEvent) => {
        const message = eventToMessage(eventType, event)
        if (message) addEvent(eventType, message)
    }, [addEvent]))

    return (
        <WidgetWrapper
            title="Activity Feed"
            icon={<Rss className="w-4 h-4 text-emerald-500" />}
            isEditMode={isEditMode}
            headerRight={
                <span className="text-[10px] text-muted-foreground">{events.length} events</span>
            }
            noPadding
        >
            <div ref={scrollRef} className="h-full overflow-auto">
                {events.length === 0 ? (
                    <div className="flex items-center justify-center h-full min-h-[200px] text-sm text-muted-foreground">
                        <div className="text-center">
                            <Rss className="w-8 h-8 mx-auto mb-2 opacity-30" />
                            <p>Waiting for events...</p>
                            <p className="text-[10px] mt-1">Events will appear here in real-time</p>
                        </div>
                    </div>
                ) : (
                    <div className="divide-y divide-border/50">
                        {events.map((event) => (
                            <div
                                key={event.id}
                                className="flex items-start gap-2.5 px-4 py-2.5 hover:bg-muted/30 transition-colors animate-in fade-in slide-in-from-top-1 duration-300"
                            >
                                <div className={cn(
                                    "w-1.5 h-1.5 rounded-full mt-1.5 shrink-0",
                                    EVENT_COLORS[event.type] || "bg-muted-foreground"
                                )} />
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2">
                                        <span className="text-[9px] font-bold uppercase tracking-wider text-muted-foreground">
                                            {EVENT_LABELS[event.type] || "EVT"}
                                        </span>
                                        <span className="text-[10px] text-muted-foreground/70 font-mono">
                                            {formatTime(event.timestamp)}
                                        </span>
                                    </div>
                                    <p className="text-xs text-foreground/90 truncate">{event.message}</p>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>
        </WidgetWrapper>
    )
}
