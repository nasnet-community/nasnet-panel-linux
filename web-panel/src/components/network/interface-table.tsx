import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import type { InterfaceRole, NetworkInterfaceView, UplinkSlot } from "@/lib/types/network"

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

interface Props {
    interfaces: NetworkInterfaceView[]
    onAssign: (iface: NetworkInterfaceView, choice: RoleChoice) => void
    disabled?: boolean
}

export function InterfaceTable({ interfaces, onAssign, disabled }: Props) {
    const rows = interfaces.filter((i) => i.assignable)

    return (
        <Table>
            <TableHeader>
                <TableRow>
                    <TableHead>Interface</TableHead>
                    <TableHead>Source</TableHead>
                    <TableHead>Addresses</TableHead>
                    <TableHead>Link</TableHead>
                    <TableHead>Role</TableHead>
                </TableRow>
            </TableHeader>
            <TableBody>
                {rows.map((iface) => (
                    <TableRow key={iface.key} className={cn(!iface.present && "opacity-50")}>
                        <TableCell>
                            <div className="font-medium">{iface.label || iface.if_name}</div>
                            {iface.label && (
                                <div className="text-muted-foreground text-xs">{iface.if_name}</div>
                            )}
                            {iface.key_kind !== "permaddr" && (
                                <div className="text-muted-foreground mt-1 text-xs">
                                    No usable permanent MAC — this role is tied to the port, not the
                                    device.
                                </div>
                            )}
                            {!iface.present && (
                                <div className="text-muted-foreground mt-1 text-xs">
                                    Not present — the role is kept for when it returns.
                                </div>
                            )}
                        </TableCell>

                        <TableCell>
                            <Badge variant="outline">{iface.source}</Badge>
                            {iface.confidence > 0 && iface.confidence < 100 && (
                                <div className="text-muted-foreground mt-1 text-xs">
                                    {iface.confidence}% confident — confirm before assigning
                                </div>
                            )}
                            {iface.usb_speed_mbit === 480 && (
                                <div className="text-muted-foreground mt-1 text-xs">
                                    USB 2.0 — expect a ceiling near 280 Mbit/s
                                </div>
                            )}
                        </TableCell>

                        <TableCell>
                            {iface.addrs?.length ? (
                                iface.addrs.map((a) => (
                                    <div key={a} className="font-mono text-xs">
                                        {a}
                                    </div>
                                ))
                            ) : (
                                <span className="text-muted-foreground text-xs">none</span>
                            )}
                        </TableCell>

                        <TableCell>
                            <div className="flex items-center gap-2">
                                <span
                                    aria-hidden
                                    className={cn(
                                        "inline-block h-2 w-2 rounded-full",
                                        iface.carrier ? "bg-emerald-500" : "bg-muted-foreground",
                                    )}
                                />
                                <span className="text-xs">
                                    {iface.carrier ? "up" : iface.oper_state || "down"}
                                </span>
                            </div>
                            {iface.speed_mbit > 0 && (
                                <div className="text-muted-foreground text-xs">
                                    {iface.speed_mbit} Mbit/s
                                </div>
                            )}
                        </TableCell>

                        <TableCell>
                            <Select
                                value={choiceValue(iface)}
                                disabled={disabled}
                                onValueChange={(v) => {
                                    const choice = ROLE_CHOICES.find((c) => c.value === v)
                                    if (choice) onAssign(iface, choice)
                                }}
                            >
                                <SelectTrigger className="w-[220px]">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {ROLE_CHOICES.map((c) => {
                                        const holder = c.slot
                                            ? slotHolder(interfaces, c.slot, iface.key)
                                            : null
                                        return (
                                            <SelectItem
                                                key={c.value}
                                                value={c.value}
                                                disabled={!!holder}
                                            >
                                                {holder ? `${c.label} — held by ${holder}` : c.label}
                                            </SelectItem>
                                        )
                                    })}
                                </SelectContent>
                            </Select>
                        </TableCell>
                    </TableRow>
                ))}
            </TableBody>
        </Table>
    )
}
