import { useState } from "react"
import { Check, Dice5, MonitorSmartphone, Pencil, TriangleAlert, X } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { EmptyState } from "@/components/ui/empty-state"
import { InfoPopover } from "@/components/ui/info-popover"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useLANDevices, useSetDeviceLabel } from "@/lib/queries/use-network"
import { deviceName, lastSeenLabel, NAME_PROVENANCE_NOTES } from "@/lib/network-labels"
import { cn } from "@/lib/utils"
import type { LANDevice } from "@/lib/types/network"
import { toast } from "sonner"

interface Props {
    /** The LAN is off, so there is no bridge to read. */
    lanEnabled: boolean
}

function NameCell({ device }: { device: LANDevice }) {
    const save = useSetDeviceLabel()
    const [editing, setEditing] = useState(false)
    const [draft, setDraft] = useState("")
    const { name, from } = deviceName(device)

    async function commit() {
        try {
            await save.mutateAsync({ mac: device.mac, label: draft.trim() })
            setEditing(false)
            toast.success(draft.trim() ? "Name saved" : "Name removed")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Failed to save the name")
        }
    }

    if (editing) {
        return (
            <div className="flex items-center gap-1">
                <Input
                    autoFocus
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === "Enter") void commit()
                        if (e.key === "Escape") setEditing(false)
                    }}
                    placeholder="Name this device"
                    aria-label={`Name for ${device.mac}`}
                    className="h-7 max-w-[15rem] text-sm"
                    maxLength={63}
                />
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    aria-label="Save the name"
                    onClick={() => void commit()}
                >
                    <Check className="h-3.5 w-3.5" />
                </Button>
                <Button
                    size="icon"
                    variant="ghost"
                    className="h-7 w-7"
                    aria-label="Discard"
                    onClick={() => setEditing(false)}
                >
                    <X className="h-3.5 w-3.5" />
                </Button>
            </div>
        )
    }

    return (
        <div className="group/name flex min-w-0 items-center gap-1.5">
            <Tooltip>
                <TooltipTrigger asChild>
                    <span
                        className={cn(
                            "truncate",
                            from === "named" && "font-medium",
                            from === "unknown" && "text-muted-foreground font-mono text-xs",
                            from === "vendor" && "text-muted-foreground",
                        )}
                    >
                        {name}
                    </span>
                </TooltipTrigger>
                <TooltipContent>{NAME_PROVENANCE_NOTES[from]}</TooltipContent>
            </Tooltip>

            {device.randomized ? (
                <Tooltip>
                    <TooltipTrigger asChild>
                        <Dice5 className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-xs">
                        This device uses a randomized MAC address. It gets a new one each time it
                        joins, so a name would not stay attached to it.
                    </TooltipContent>
                </Tooltip>
            ) : (
                <Button
                    size="icon"
                    variant="ghost"
                    aria-label={`Rename ${name}`}
                    className="h-6 w-6 shrink-0 opacity-0 transition-opacity group-hover/name:opacity-100 focus-visible:opacity-100"
                    onClick={() => {
                        setDraft(device.label)
                        setEditing(true)
                    }}
                >
                    <Pencil className="h-3 w-3" />
                </Button>
            )}
        </div>
    )
}

function DeviceRow({ device }: { device: LANDevice }) {
    const { from } = deviceName(device)
    // When the operator has named it, keep the device's own claim visible —
    // it is how you tell two identical boxes apart.
    const secondary = from === "named" && device.hostname ? device.hostname : ""

    return (
        <TableRow className="group">
            <TableCell className="py-2.5">
                <div className="flex items-start gap-2.5">
                    <span
                        aria-hidden
                        className={cn(
                            "mt-[0.45rem] h-1.5 w-1.5 shrink-0 rounded-full",
                            device.online ? "bg-emerald-500" : "bg-muted-foreground/40",
                        )}
                    />
                    <span className="sr-only">
                        {device.online ? "Connected" : "Not connected"}
                    </span>
                    <div className="min-w-0">
                        <NameCell device={device} />
                        {secondary && (
                            <div className="text-muted-foreground truncate text-xs">
                                calls itself {secondary}
                            </div>
                        )}
                    </div>
                </div>
            </TableCell>

            <TableCell className="font-mono text-xs whitespace-nowrap">
                {device.ips.length === 0 ? (
                    <span className="text-muted-foreground font-sans">No address</span>
                ) : (
                    <span>
                        {device.ips[0]}
                        {device.ips.length > 1 && (
                            <Tooltip>
                                <TooltipTrigger asChild>
                                    <span className="text-muted-foreground ml-1.5 cursor-default font-sans">
                                        +{device.ips.length - 1}
                                    </span>
                                </TooltipTrigger>
                                <TooltipContent>
                                    Also reachable at {device.ips.slice(1).join(", ")}
                                </TooltipContent>
                            </Tooltip>
                        )}
                    </span>
                )}
            </TableCell>

            <TableCell className="text-muted-foreground hidden font-mono text-xs whitespace-nowrap lg:table-cell">
                {device.mac}
            </TableCell>

            <TableCell className="text-muted-foreground hidden truncate text-sm md:table-cell">
                {device.vendor || (device.randomized ? "—" : "Unknown")}
            </TableCell>

            <TableCell className="text-muted-foreground hidden text-sm whitespace-nowrap xl:table-cell">
                {device.port || "—"}
            </TableCell>

            <TableCell className="text-muted-foreground text-right text-sm whitespace-nowrap">
                {device.online ? lastSeenLabel(device.last_seen_seconds) : "Not connected"}
            </TableCell>
        </TableRow>
    )
}

/** What is on the LAN bridge right now, assembled from the DHCP leases, the
 *  neighbour table and the bridge's forwarding database. */
export function LanDevices({ lanEnabled }: Props) {
    const devices = useLANDevices(lanEnabled)

    if (!lanEnabled) return null
    if (devices.isLoading) return <Skeleton className="h-64 w-full" />

    if (devices.isError) {
        return (
            <Alert variant="warning">
                <TriangleAlert className="h-4 w-4" />
                <AlertDescription>
                    The bridge could not be read, so it is not known what is connected.{" "}
                    {devices.error?.message}
                </AlertDescription>
            </Alert>
        )
    }

    const list = devices.data
    if (!list) return null

    const total = list.devices.length
    const online = list.devices.filter((d) => d.online).length
    const offlineAfter = Math.round(list.offline_after_seconds / 60)

    return (
        // Every Tooltip below needs a provider above it; there is no global one.
        <TooltipProvider delayDuration={200}>
            <Card>
                <CardHeader className="pb-4">
                    <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                        <CardTitle className="flex items-center gap-2">
                            <MonitorSmartphone className="h-4 w-4" />
                            Connected devices
                        </CardTitle>
                        {total > 0 && (
                            <div className="text-muted-foreground flex items-center gap-1.5 text-sm">
                                <span className="text-foreground font-medium tabular-nums">
                                    {online}
                                </span>
                                connected
                                {online !== total && <span>of {total} seen</span>}
                                {/* The lag is surprising enough to explain, but not
                                    important enough to spend three lines on. */}
                                <InfoPopover>
                                    A device shows up as soon as it sends anything. One that leaves
                                    keeps showing as connected for up to {offlineAfter} minute
                                    {offlineAfter === 1 ? "" : "s"}, which is how long this box
                                    remembers it.
                                </InfoPopover>
                            </div>
                        )}
                    </div>
                    <CardDescription>
                        Everything reaching the internet through this box's local network.
                    </CardDescription>
                </CardHeader>

                <CardContent className="space-y-3">
                    {/* Only worth saying when a source is actually missing. */}
                    {!list.leases_ok && (
                        <Alert variant="warning">
                            <TriangleAlert className="h-4 w-4" />
                            <AlertDescription>
                                The DHCP lease file could not be read, so devices show no name and
                                any device that has left is missing entirely.
                            </AlertDescription>
                        </Alert>
                    )}
                    {!list.neighbours_ok && (
                        <Alert variant="warning">
                            <TriangleAlert className="h-4 w-4" />
                            <AlertDescription>
                                The neighbour table could not be read. Devices using a fixed address
                                may show no address.
                            </AlertDescription>
                        </Alert>
                    )}

                    {total === 0 ? (
                        <EmptyState
                            icon={MonitorSmartphone}
                            title="Nothing is connected yet"
                            description="Plug a device into a LAN port, or connect a switch to reach several at once."
                        />
                    ) : (
                        <div className="overflow-x-auto">
                            <Table>
                                <TableHeader>
                                    <TableRow>
                                        <TableHead className="w-[30%]">Device</TableHead>
                                        <TableHead className="w-[14%]">Address</TableHead>
                                        <TableHead className="hidden w-[16%] lg:table-cell">
                                            MAC address
                                        </TableHead>
                                        <TableHead className="hidden w-[20%] md:table-cell">
                                            Made by
                                        </TableHead>
                                        <TableHead className="hidden w-[10%] xl:table-cell">
                                            Plugged into
                                        </TableHead>
                                        <TableHead className="text-right">Last seen</TableHead>
                                    </TableRow>
                                </TableHeader>
                                <TableBody>
                                    {list.devices.map((d) => (
                                        <DeviceRow key={d.mac} device={d} />
                                    ))}
                                </TableBody>
                            </Table>
                        </div>
                    )}
                </CardContent>
            </Card>
        </TooltipProvider>
    )
}
