import { useState } from "react"
import { ChevronDown, TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { CopyableText } from "@/components/ui/copyable-text"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import { attachmentLabel, groupAddresses, linkLabel, linkTone } from "@/lib/network-labels"
import type {
    AssignRoleRequest,
    InterfaceRole,
    NetworkInterfaceView,
    UplinkSlot,
} from "@/lib/types/network"

/** Role + slot as one picker: stage 1 groups have one member each, so slots are
 *  what the operator actually thinks in. */
export interface RoleChoice {
    value: string
    label: string
    role: InterfaceRole
    slot: UplinkSlot
}

export const ROLE_CHOICES: RoleChoice[] = [
    { value: "unassigned", label: "Unassigned", role: "unassigned", slot: "" },
    { value: "wan:domestic", label: "Domestic ISP", role: "wan", slot: "domestic" },
    { value: "wan:secondary", label: "Secondary uplink (Starlink)", role: "wan", slot: "secondary" },
    { value: "lan", label: "LAN", role: "lan", slot: "" },
    { value: "lan_member", label: "LAN member", role: "lan_member", slot: "" },
    { value: "mgmt", label: "Management", role: "mgmt", slot: "" },
]

function choiceValue(iface: NetworkInterfaceView): string {
    if (iface.role === "wan" && iface.slot) return `wan:${iface.slot}`
    return iface.role
}

/** One interface per slot. Showing who holds it beats a server-side reject. */
export function slotHolder(
    interfaces: NetworkInterfaceView[],
    slot: UplinkSlot,
    exceptKey: string,
): string | null {
    const held = interfaces.find((i) => i.role === "wan" && i.slot === slot && i.key !== exceptKey)
    return held ? held.label || held.if_name : null
}

/** The request a role pick turns into. Neither id is inferred server side:
 *  V8 wants the evictee named, V10 the bridge the member joins. */
export function buildAssignRequest(
    interfaces: NetworkInterfaceView[],
    iface: NetworkInterfaceView,
    choice: RoleChoice,
): AssignRoleRequest {
    const req: AssignRoleRequest = {
        interface_id: iface.id,
        role: choice.role,
        slot: choice.slot,
    }
    const holder = interfaces.find(
        (i) =>
            i.id !== iface.id &&
            i.role === choice.role &&
            (choice.slot === "" || i.slot === choice.slot),
    )
    if (holder) req.evict_id = holder.id
    if (choice.role === "lan_member") {
        const lan = interfaces.find((i) => i.role === "lan")
        if (lan) req.master_id = lan.id
    }
    return req
}

/** Everything about a port that changes how you'd assign it, in one place —
 *  these used to be scattered across two columns. */
export function caveats(iface: NetworkInterfaceView): string[] {
    const out: string[] = []
    if (iface.key_kind !== "permaddr") {
        out.push("No permanent MAC — the role follows this port, not the device in it.")
    }
    if (iface.confidence > 0 && iface.confidence < 100) {
        out.push(`${iface.confidence}% confident this is a real port — check before assigning.`)
    }
    if (iface.usb_speed_mbit === 480) {
        out.push("USB 2.0 bus — expect a ceiling near 280 Mbit/s.")
    }
    return out
}

const DOT: Record<string, string> = {
    up: "bg-status-success",
    down: "bg-status-danger",
    absent: "bg-status-neutral",
}

/** Caveats inline made a three-note row three times taller than its neighbours,
 *  so they live behind one marker instead. */
function Caveats({ notes }: { notes: string[] }) {
    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    className="text-status-warning hover:bg-status-warning/10 focus-visible:ring-ring -mx-1 flex items-center gap-1 rounded px-1 text-xs transition-colors focus-visible:ring-2 focus-visible:outline-none"
                >
                    <TriangleAlert className="h-3.5 w-3.5" />
                    {notes.length === 1 ? "1 caveat" : `${notes.length} caveats`}
                </button>
            </PopoverTrigger>
            <PopoverContent side="right" align="start" className="max-w-xs">
                <ul className="space-y-2 text-xs leading-relaxed">
                    {notes.map((n) => (
                        <li key={n} className="flex gap-2">
                            <span className="bg-status-warning mt-1.5 h-1 w-1 shrink-0 rounded-full" />
                            {n}
                        </li>
                    ))}
                </ul>
            </PopoverContent>
        </Popover>
    )
}

function Identity({ iface }: { iface: NetworkInterfaceView }) {
    const notes = caveats(iface)
    return (
        <div className="space-y-1">
            <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-sm font-medium">{iface.if_name}</span>
                {!iface.present && (
                    <Badge variant="warning" className="px-1.5 py-0 text-[10px]">
                        absent
                    </Badge>
                )}
                {notes.length > 0 && <Caveats notes={notes} />}
            </div>
            {iface.label && <p className="text-text-secondary text-xs">{iface.label}</p>}
            {!iface.present && (
                <p className="text-text-tertiary text-xs">Role is kept for when it returns.</p>
            )}
        </div>
    )
}

function Attachment({ iface }: { iface: NetworkInterfaceView }) {
    return (
        <div className="space-y-0.5">
            <Tooltip>
                <TooltipTrigger asChild>
                    <span className="text-text-secondary cursor-help text-sm">
                        {attachmentLabel(iface.source)}
                    </span>
                </TooltipTrigger>
                <TooltipContent side="top" className="font-mono text-xs">
                    {iface.source}
                </TooltipContent>
            </Tooltip>
            {iface.driver && (
                <p className="text-text-tertiary font-mono text-[11px]">{iface.driver}</p>
            )}
        </div>
    )
}

function Addresses({ iface }: { iface: NetworkInterfaceView }) {
    const [open, setOpen] = useState(false)
    const { primary, extra } = groupAddresses(iface.addrs)

    if (!primary) {
        return <span className="text-text-tertiary text-xs">no address</span>
    }

    return (
        <div className="space-y-1">
            <CopyableText text={primary} className="font-mono text-xs" />
            {open &&
                extra.map((a) => (
                    <CopyableText
                        key={a}
                        text={a}
                        className="text-text-tertiary font-mono text-xs"
                    />
                ))}
            {extra.length > 0 && (
                <button
                    type="button"
                    onClick={() => setOpen((v) => !v)}
                    aria-expanded={open}
                    className="text-text-tertiary hover:text-foreground focus-visible:ring-ring flex items-center gap-1 rounded text-[11px] transition-colors focus-visible:ring-2 focus-visible:outline-none"
                >
                    <ChevronDown
                        className={cn(
                            "h-3 w-3 transition-transform duration-150",
                            open && "rotate-180",
                        )}
                    />
                    {open ? "hide" : `+${extra.length} more`}
                </button>
            )}
        </div>
    )
}

function LinkState({ iface }: { iface: NetworkInterfaceView }) {
    const tone = linkTone(iface)
    return (
        <div className="space-y-0.5">
            <div className="flex items-center gap-2">
                <span
                    aria-hidden
                    className={cn("inline-block h-2 w-2 shrink-0 rounded-full", DOT[tone])}
                />
                <span className="text-sm">{linkLabel(iface)}</span>
            </div>
            {iface.speed_mbit > 0 && (
                <p className="text-text-tertiary font-mono text-[11px]">
                    {iface.speed_mbit} Mbit/s
                </p>
            )}
        </div>
    )
}

interface RoleSelectProps {
    iface: NetworkInterfaceView
    interfaces: NetworkInterfaceView[]
    onAssign: (iface: NetworkInterfaceView, choice: RoleChoice) => void
    disabled?: boolean
}

function RoleSelect({ iface, interfaces, onAssign, disabled }: RoleSelectProps) {
    const assigned = iface.role !== "unassigned"
    return (
        <Select
            value={choiceValue(iface)}
            disabled={disabled}
            onValueChange={(v) => {
                const choice = ROLE_CHOICES.find((c) => c.value === v)
                if (choice) onAssign(iface, choice)
            }}
        >
            <SelectTrigger
                aria-label={`Role for ${iface.if_name}`}
                className={cn(
                    "w-full sm:w-[230px]",
                    assigned ? "font-medium" : "text-text-tertiary",
                )}
            >
                <SelectValue />
            </SelectTrigger>
            <SelectContent>
                {ROLE_CHOICES.map((c) => {
                    const holder = c.slot ? slotHolder(interfaces, c.slot, iface.key) : null
                    return (
                        <SelectItem key={c.value} value={c.value} disabled={!!holder}>
                            {holder ? `${c.label} — held by ${holder}` : c.label}
                        </SelectItem>
                    )
                })}
            </SelectContent>
        </Select>
    )
}

interface Props {
    interfaces: NetworkInterfaceView[]
    onAssign: (iface: NetworkInterfaceView, choice: RoleChoice) => void
    disabled?: boolean
}

export function InterfaceTable({ interfaces, onAssign, disabled }: Props) {
    const rows = interfaces.filter((i) => i.assignable)

    if (rows.length === 0) {
        return (
            <p className="text-text-secondary py-8 text-center text-sm">
                No assignable ports detected. Plug in an adapter and refresh.
            </p>
        )
    }

    return (
        <TooltipProvider delayDuration={200}>
            {/* Table below md becomes unreadable at five columns, so it stacks. */}
            <div className="hidden md:block">
                <Table>
                    <TableHeader>
                        <TableRow className="hover:bg-transparent">
                            <TableHead className="w-[26%]">Interface</TableHead>
                            <TableHead className="w-[16%]">Attachment</TableHead>
                            <TableHead className="w-[22%]">Addresses</TableHead>
                            <TableHead className="w-[12%]">Link</TableHead>
                            <TableHead className="text-right">Role</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {rows.map((iface) => (
                            <TableRow
                                key={iface.key}
                                className={cn("align-top", !iface.present && "opacity-60")}
                            >
                                <TableCell>
                                    <Identity iface={iface} />
                                </TableCell>
                                <TableCell>
                                    <Attachment iface={iface} />
                                </TableCell>
                                <TableCell>
                                    <Addresses iface={iface} />
                                </TableCell>
                                <TableCell>
                                    <LinkState iface={iface} />
                                </TableCell>
                                <TableCell className="text-right">
                                    <div className="flex justify-end">
                                        <RoleSelect
                                            iface={iface}
                                            interfaces={interfaces}
                                            onAssign={onAssign}
                                            disabled={disabled}
                                        />
                                    </div>
                                </TableCell>
                            </TableRow>
                        ))}
                    </TableBody>
                </Table>
            </div>

            <div className="space-y-3 md:hidden">
                {rows.map((iface) => (
                    <div
                        key={iface.key}
                        className={cn(
                            "border-border-subtle space-y-3 rounded-lg border p-3",
                            !iface.present && "opacity-60",
                        )}
                    >
                        <div className="flex items-start justify-between gap-3">
                            <Identity iface={iface} />
                            <LinkState iface={iface} />
                        </div>
                        <div className="flex items-start justify-between gap-3">
                            <Addresses iface={iface} />
                            <Attachment iface={iface} />
                        </div>
                        <RoleSelect
                            iface={iface}
                            interfaces={interfaces}
                            onAssign={onAssign}
                            disabled={disabled}
                        />
                    </div>
                ))}
            </div>
        </TooltipProvider>
    )
}
