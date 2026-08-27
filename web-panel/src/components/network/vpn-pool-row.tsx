import { ArrowDown, ArrowUp, ChevronDown, ChevronUp, GripVertical } from "lucide-react"
import { useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Select, SelectContent, SelectItem, SelectTrigger } from "@/components/ui/select"
import { RttSparkline } from "@/components/router/rtt-sparkline"
import { cn } from "@/lib/utils"
import { formatBytes, handshakeShort, poolRowCondition, POOL_STATE_LABEL } from "@/lib/vpn-labels"
import type { PoolRowState } from "@/lib/vpn-labels"
import type { TunnelStatus, TunnelVia, VPNProfile, VPNUplink } from "@/lib/types/network"
import type { TunnelHealth } from "@/lib/types/health"

export interface PoolRowProps {
    profile: VPNProfile
    status: TunnelStatus | undefined
    health: TunnelHealth | undefined
    uplinks: VPNUplink[]
    /** The failover chain is the one strategy the order belongs to. */
    chain: boolean
    position: number
    first: boolean
    last: boolean
    nextUp: boolean
    open: boolean
    /** A change is armed, so the pool must hold still until it is kept. */
    armed: boolean
    busy: boolean
    pinBusy: boolean
    orderBusy: boolean
    onOpenChange: (open: boolean) => void
    onMove: (direction: "up" | "down") => void
    onToggle: (on: boolean) => void
    onPin: (uplinkKey: string) => void
    onEdit: () => void
    onDelete: () => void
}

const STATE_CHIP: Record<PoolRowState, string> = {
    "carrying": "bg-status-success-soft text-status-success font-medium",
    "next-up": "bg-surface-3 text-text-secondary",
    "standby": "text-text-tertiary",
    "not-answering": "bg-status-danger-soft text-status-danger font-medium",
    "checking": "text-text-tertiary",
    "off": "text-text-tertiary",
}

const STATE_DOT: Record<PoolRowState, string> = {
    "carrying": "bg-status-success",
    "next-up": "bg-text-tertiary",
    "standby": "bg-text-tertiary",
    "not-answering": "bg-status-danger",
    "checking": "bg-text-disabled",
    "off": "bg-text-disabled opacity-50",
}

/** Drag with a mouse, the arrows with anything else. */
function Position({
    owner,
    position,
    first,
    last,
    disabled,
    handle,
    onMove,
}: {
    owner: string
    position: number
    first: boolean
    last: boolean
    disabled: boolean
    handle?: React.HTMLAttributes<HTMLElement>
    onMove: (direction: "up" | "down") => void
}) {
    return (
        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <span
                {...handle}
                aria-hidden
                className={cn(
                    "text-text-disabled touch-none",
                    disabled ? "opacity-40" : "cursor-grab active:cursor-grabbing",
                )}
            >
                <GripVertical className="h-4 w-4" />
            </span>
            <span className="text-text-tertiary w-3 text-right text-sm tabular-nums">
                {position + 1}
            </span>
            <div className="flex flex-col">
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-4 w-5"
                    aria-label={`Move ${owner} up`}
                    disabled={disabled || first}
                    onClick={() => onMove("up")}
                >
                    <ChevronUp className="h-3 w-3" />
                </Button>
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-4 w-5"
                    aria-label={`Move ${owner} down`}
                    disabled={disabled || last}
                    onClick={() => onMove("down")}
                >
                    <ChevronDown className="h-3 w-3" />
                </Button>
            </div>
        </div>
    )
}

/** Dimmed reads "the box chose this"; solid reads "you did". */
function ViaPicker({
    owner,
    via,
    uplinks,
    disabled,
    onPin,
}: {
    owner: string
    via: TunnelVia | undefined
    uplinks: VPNUplink[]
    disabled: boolean
    onPin: (uplinkKey: string) => void
}) {
    const text = via ? (
        <span className={cn("truncate", !via.pinned && "text-text-tertiary")}>
            {via.pinned ? via.label : `auto · ${via.label}`}
        </span>
    ) : (
        <span className="text-text-tertiary">—</span>
    )
    // One uplink is not a choice, so it reads rather than offers.
    if (uplinks.length < 2) {
        return <span className="truncate text-sm">{text}</span>
    }
    return (
        <div onClick={(e) => e.stopPropagation()}>
            <Select
                value={via?.pinned ? via.key : "auto"}
                onValueChange={(v) => onPin(v === "auto" ? "" : v)}
                disabled={disabled}
            >
                <SelectTrigger
                    aria-label={`Change ${owner} uplink`}
                    className={cn(
                        "h-7 w-full justify-start gap-1 rounded border-0 px-1.5 text-sm shadow-none",
                        // shadcn paints the trigger in dark mode; the row is bare.
                        "bg-transparent dark:bg-transparent hover:bg-surface-3 dark:hover:bg-surface-3",
                        "focus-visible:ring-1",
                    )}
                >
                    {text}
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="auto">Automatic</SelectItem>
                    {uplinks.map((u) => (
                        <SelectItem key={u.key} value={u.key}>
                            {u.label}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </div>
    )
}

export function PoolRow(props: PoolRowProps & { handle?: React.HTMLAttributes<HTMLElement> }) {
    const {
        profile: p, status: st, health: h, uplinks, chain, position, first, last, nextUp,
        open, armed, busy, pinBusy, orderBusy, onOpenChange, onMove, onToggle, onPin,
        onEdit, onDelete, handle,
    } = props

    const { state, degraded } = poolRowCondition(p, st, h, nextUp)
    const on = p.enabled
    const chipTone = state === "carrying" && degraded
        ? "bg-status-warning-soft text-status-warning font-medium"
        : STATE_CHIP[state]
    const dotTone = state === "carrying" && degraded ? "bg-status-warning" : STATE_DOT[state]

    return (
        <div
            data-profile={p.id}
            data-state={state}
            className={cn(
                "border-border/60 relative border-b last:border-b-0",
                !on && "opacity-70",
            )}
        >
            {/* The one heavy mark in the card: which VPN has the traffic. */}
            {state === "carrying" && (
                <span
                    aria-hidden
                    className={cn(
                        "absolute inset-y-0 left-0 w-0.5",
                        degraded ? "bg-status-warning" : "bg-status-success",
                    )}
                />
            )}
            <div
                className="hover:bg-surface-3/40 flex cursor-pointer items-center gap-3 py-3 pr-3 pl-4 transition-colors"
                onClick={() => onOpenChange(!open)}
            >
                {chain && on && (
                    <Position
                        owner={p.name}
                        position={position}
                        first={first}
                        last={last}
                        disabled={orderBusy || armed}
                        handle={handle}
                        onMove={onMove}
                    />
                )}

                <div onClick={(e) => e.stopPropagation()}>
                    <Switch
                        checked={on}
                        aria-label={on ? `Turn ${p.name} off` : `Turn ${p.name} on`}
                        disabled={armed || busy || !!p.unreadable}
                        onCheckedChange={onToggle}
                    />
                </div>

                <span aria-hidden className={cn("h-2 w-2 shrink-0 rounded-full", dotTone)} />

                <div className="min-w-0 flex-1">
                    <p className="text-text-primary truncate font-medium">{p.name}</p>
                    {p.unreadable ? (
                        // Listed only so it can be deleted.
                        <p className="text-status-warning truncate text-xs">
                            Stored config cannot be read — {p.unreadable}
                        </p>
                    ) : (
                        <p className="text-text-disabled truncate font-mono text-xs">
                            {p.config.peer.endpoint}
                        </p>
                    )}
                </div>

                {on && (
                    <span
                        data-chip={state}
                        className={cn(
                            "w-24 shrink-0 rounded-full px-2 py-0.5 text-center text-xs whitespace-nowrap",
                            chipTone,
                        )}
                    >
                        {POOL_STATE_LABEL[state]}
                    </span>
                )}

                {on && (
                    <div className="hidden w-36 shrink-0 md:block">
                        <ViaPicker
                            owner={p.name}
                            via={st?.via}
                            uplinks={uplinks}
                            disabled={pinBusy || armed}
                            onPin={onPin}
                        />
                    </div>
                )}

                {on && (
                    <div className="w-20 shrink-0 text-right tabular-nums">
                        <p className="text-sm">{h ? `${h.median_rtt_ms} ms` : "—"}</p>
                        <p className="text-text-tertiary text-xs">{h ? `${h.loss_pct}% loss` : ""}</p>
                    </div>
                )}

                {on && (
                    <RttSparkline
                        history={h?.history}
                        className="hidden h-6 w-20 lg:block"
                        label={`${p.name} round-trip time`}
                    />
                )}

                <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7 shrink-0"
                    aria-label={`Details for ${p.name}`}
                    aria-expanded={open}
                    onClick={(e) => {
                        e.stopPropagation()
                        onOpenChange(!open)
                    }}
                >
                    <ChevronDown
                        className={cn("h-4 w-4 transition-transform", open && "rotate-180")}
                    />
                </Button>
            </div>

            {open && (
                <div className="space-y-4 px-4 pb-4 pl-14">
                    <dl className="grid grid-cols-[9rem_1fr] gap-x-4 gap-y-2 text-sm">
                        {on && (
                            <>
                                <dt className="text-text-tertiary">Last handshake</dt>
                                <dd>{st ? handshakeShort(st.handshake_age_seconds) : "—"}</dd>
                                <dt className="text-text-tertiary">Traffic</dt>
                                <dd className="tabular-nums">
                                    <span className="inline-flex items-center gap-1">
                                        <ArrowDown className="h-3 w-3" aria-hidden />
                                        {formatBytes(st?.rx_bytes ?? 0)}
                                    </span>
                                    <span className="ml-3 inline-flex items-center gap-1">
                                        <ArrowUp className="h-3 w-3" aria-hidden />
                                        {formatBytes(st?.tx_bytes ?? 0)}
                                    </span>
                                </dd>
                                <dt className="text-text-tertiary">Uplink it rides</dt>
                                <dd className="md:hidden">
                                    <ViaPicker
                                        owner={p.name}
                                        via={st?.via}
                                        uplinks={uplinks}
                                        disabled={pinBusy || armed}
                                        onPin={onPin}
                                    />
                                </dd>
                                <dd className="hidden md:block">
                                    {st?.via
                                        ? `${st.via.label} — ${st.via.pinned ? "you chose this" : "chosen for you"}`
                                        : "—"}
                                </dd>
                            </>
                        )}
                        <dt className="text-text-tertiary">Endpoint</dt>
                        <dd className="font-mono text-xs break-all">{p.config.peer.endpoint}</dd>
                        {st?.last_error && (
                            <>
                                <dt className="text-text-tertiary">Problem</dt>
                                <dd className="text-status-warning">{st.last_error}</dd>
                            </>
                        )}
                    </dl>
                    <div className="flex flex-wrap items-center gap-2">
                        <Button
                            size="sm"
                            variant="outline"
                            onClick={onEdit}
                            // Editing a live member is a routing change.
                            disabled={on || !!p.unreadable}
                        >
                            Edit
                        </Button>
                        <Button size="sm" variant="outline" onClick={onDelete} disabled={on}>
                            Delete
                        </Button>
                        {on && (
                            <span className="text-text-tertiary text-xs">
                                Turn it off first to edit or delete it.
                            </span>
                        )}
                    </div>
                </div>
            )}
        </div>
    )
}

/** Only the chain's rows drag. */
export function SortablePoolRow(props: PoolRowProps) {
    const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
        id: props.profile.id,
    })
    return (
        <div
            ref={setNodeRef}
            style={{ transform: CSS.Translate.toString(transform), transition }}
            className={cn(isDragging && "bg-surface-3 opacity-60")}
        >
            <PoolRow {...props} handle={{ ...attributes, ...listeners }} />
        </div>
    )
}
