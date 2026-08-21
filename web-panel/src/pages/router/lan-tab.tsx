import { useState } from "react"
import { toast } from "sonner"
import { Settings2, ShieldAlert, TriangleAlert } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
} from "@/components/ui/sheet"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { useConfirm } from "@/components/ui/confirm-dialog"
import {
    ClassificationLadder,
    ClassificationStrip,
} from "@/components/network/classification-ladder"
import { LanDevices } from "@/components/network/lan-devices"
import { useLAN, useUpdateLAN } from "@/lib/queries/use-network"
import { verdictsFromError } from "@/lib/api/network"
import type { LANConfig, NetworkState, Verdict } from "@/lib/types/network"

interface Props {
    state: NetworkState | undefined
    /** A change is already armed, so a second one must wait for it. */
    armed: boolean
    onApplied: () => void
}

type Draft = Pick<
    LANConfig,
    "enabled" | "cidr" | "dhcp_range_low" | "dhcp_range_high" | "lease_hours" | "input_firewall"
>

function uplinkLabel(state: NetworkState | undefined, slot: "domestic" | "secondary"): string {
    const u = state?.uplinks?.find((x) => x.slot === slot)
    if (!u) return slot === "domestic" ? "the domestic uplink" : "the secondary uplink"
    return u.label || u.if_name
}

export function LanTab({ state, armed, onApplied }: Props) {
    const lan = useLAN()
    const save = useUpdateLAN()
    const confirm = useConfirm()

    // null means untouched: the server stays the truth until an edit.
    const [edited, setEdited] = useState<Draft | null>(null)
    const [verdicts, setVerdicts] = useState<Verdict[]>([])
    const [settingsOpen, setSettingsOpen] = useState(false)

    if (lan.isLoading || !lan.data) {
        return <Skeleton className="h-96 w-full" />
    }
    if (lan.isError) {
        return (
            <Alert variant="warning">
                <TriangleAlert className="h-4 w-4" />
                <AlertDescription>
                    The LAN settings could not be read. {lan.error?.message}
                </AlertDescription>
            </Alert>
        )
    }

    const stored = lan.data
    const draft: Draft = edited ?? {
        enabled: stored.enabled,
        cidr: stored.cidr,
        dhcp_range_low: stored.dhcp_range_low,
        dhcp_range_high: stored.dhcp_range_high,
        lease_hours: stored.lease_hours,
        input_firewall: stored.input_firewall,
    }
    const dirty =
        draft.enabled !== stored.enabled ||
        draft.cidr !== stored.cidr ||
        draft.dhcp_range_low !== stored.dhcp_range_low ||
        draft.dhcp_range_high !== stored.dhcp_range_high ||
        draft.lease_hours !== stored.lease_hours ||
        draft.input_firewall !== stored.input_firewall

    const set = <K extends keyof Draft>(key: K, value: Draft[K]) =>
        setEdited({ ...draft, [key]: value })

    function discard() {
        setEdited(null)
        setVerdicts([])
    }

    async function apply() {
        setVerdicts([])
        // The one change here that can cut the operator off.
        if (draft.input_firewall && !stored.input_firewall) {
            const ok = await confirm({
                title: "Close the box to unsolicited traffic",
                description:
                    "Only the panel, your VPN inbounds and your port forwards stay reachable from the internet. " +
                    "SSH on an uplink is not opened automatically — add a port forward for it first if that is how you reach this box. " +
                    "You get 90 seconds to keep the change before it reverts itself.",
                confirmLabel: "Arm the firewall",
                variant: "warning",
                typeToConfirm: "CONFIRM",
                icon: <ShieldAlert className="h-5 w-5" />,
            })
            if (!ok) return
        }

        try {
            await save.mutateAsync(draft)
            setEdited(null)
            setSettingsOpen(false)
            toast.success("LAN settings applied")
            onApplied()
        } catch (err) {
            setVerdicts(verdictsFromError(err))
        }
    }

    const domestic = uplinkLabel(state, "domestic")
    const foreign = uplinkLabel(state, "secondary")

    const fields = (
        <>
            {!stored.resolver_ready && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        No dnsmasq service is installed, so the LAN would have no addresses and no
                        name lookups. Install the <code>dnsmasq</code> package on this box first —{" "}
                        <code>dnsmasq-base</code> alone is not enough.
                    </AlertDescription>
                </Alert>
            )}

            {/* Installed but down is a different problem with a different fix, and
                one process serves both, so DHCP is gone too. */}
            {stored.resolver_ready && !stored.resolver_running && draft.enabled && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        dnsmasq is installed but not running, so devices on the LAN are getting
                        neither addresses nor name lookups. Check <code>journalctl -u dnsmasq</code>{" "}
                        on this box.
                    </AlertDescription>
                </Alert>
            )}

            <div className="border-border-subtle flex items-center justify-between rounded-lg border p-3">
                <div className="pr-4">
                    <Label htmlFor="lan-enabled" className="text-sm font-medium">
                        Serve the local network
                    </Label>
                    <p className="text-text-secondary mt-0.5 text-xs">
                        Creates the bridge, hands out addresses, and answers DNS on it.
                    </p>
                </div>
                <Switch
                    id="lan-enabled"
                    checked={draft.enabled}
                    onCheckedChange={(v) => set("enabled", v)}
                />
            </div>

            <fieldset disabled={!draft.enabled} className="space-y-4 disabled:opacity-50">
                <div className="space-y-1.5">
                    <Label htmlFor="lan-cidr">Address and prefix</Label>
                    <Input
                        id="lan-cidr"
                        className="font-mono"
                        value={draft.cidr}
                        onChange={(e) => set("cidr", e.target.value)}
                        placeholder="10.77.0.1/24"
                    />
                    <p className="text-text-tertiary text-xs">
                        This box's own address on the LAN. Avoid 192.168.1.0/24 — most home ISP
                        routers already use it.
                    </p>
                </div>

                <div className="grid gap-4 sm:grid-cols-2">
                    <div className="space-y-1.5">
                        <Label htmlFor="lan-low">First address handed out</Label>
                        <Input
                            id="lan-low"
                            className="font-mono"
                            value={draft.dhcp_range_low}
                            onChange={(e) => set("dhcp_range_low", e.target.value)}
                        />
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="lan-high">Last address handed out</Label>
                        <Input
                            id="lan-high"
                            className="font-mono"
                            value={draft.dhcp_range_high}
                            onChange={(e) => set("dhcp_range_high", e.target.value)}
                        />
                    </div>
                </div>
                <p className="text-text-tertiary -mt-2 text-xs">
                    Addresses below the range stay free for devices you address by hand and point a
                    port forward at.
                </p>

                <div className="space-y-1.5 sm:max-w-[12rem]">
                    <Label htmlFor="lan-lease">Lease length (hours)</Label>
                    <Input
                        id="lan-lease"
                        type="number"
                        min={1}
                        className="font-mono tabular-nums"
                        value={draft.lease_hours}
                        onChange={(e) => set("lease_hours", Number(e.target.value))}
                    />
                </div>
            </fieldset>

            <div className="border-status-warning/40 bg-status-warning/[0.05] rounded-lg border p-3">
                <div className="flex items-center justify-between">
                    <div className="pr-4">
                        <Label htmlFor="lan-fw" className="text-sm font-medium">
                            Close the box to unsolicited traffic
                        </Label>
                        <p className="text-text-secondary mt-0.5 text-xs">
                            Drops anything arriving on an uplink that isn't the panel, a VPN inbound
                            or a port forward. The panel is kept open on {domestic} only.
                        </p>
                    </div>
                    <Switch
                        id="lan-fw"
                        checked={draft.input_firewall}
                        onCheckedChange={(v) => set("input_firewall", v)}
                    />
                </div>
                {draft.input_firewall && !stored.input_firewall && (
                    <p className="text-status-warning mt-2 text-xs">
                        This is the change that can lock you out. You'll be asked to type CONFIRM,
                        then have 90 seconds to keep it.
                    </p>
                )}
            </div>

            {verdicts.map((v) => (
                <Alert key={v.rule + v.message} variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        <span className="text-text-tertiary font-mono text-xs">{v.rule}</span>{" "}
                        {v.message}
                    </AlertDescription>
                </Alert>
            ))}
        </>
    )

    // No gray dead button: the apply control exists only once there is
    // something to apply.
    const applyRow = dirty ? (
        <div className="flex items-center gap-3">
            <Button
                onClick={apply}
                disabled={save.isPending || armed || (draft.enabled && !stored.resolver_ready)}
            >
                {save.isPending ? "Applying…" : "Review and apply"}
            </Button>
            {!armed && (
                <span className="text-text-tertiary text-xs">
                    Reverts itself after 90 seconds unless you keep it.
                </span>
            )}
            {armed && (
                <span className="text-text-tertiary text-xs">
                    Settle the change waiting above first.
                </span>
            )}
        </div>
    ) : (
        <p className="text-text-tertiary text-xs">Nothing to apply yet — change a setting first.</p>
    )

    // First run: the LAN is off, so setup is the page's whole job.
    if (!stored.enabled) {
        return (
            <div className="grid gap-4 lg:grid-cols-5">
                <Card className="lg:col-span-3">
                    <CardHeader>
                        <CardTitle>Local network</CardTitle>
                        <CardDescription>
                            Devices plugged into the LAN ports get an address, a resolver and split
                            routing from this box. Nothing to install on them.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="space-y-5">
                        {fields}
                        {applyRow}
                    </CardContent>
                </Card>

                <Card className="lg:col-span-2">
                    <CardHeader>
                        <CardTitle>How LAN traffic is sorted</CardTitle>
                        <CardDescription>
                            Decided in the kernel, so restarting the panel never interrupts
                            browsing. The address list refreshes itself weekly.
                        </CardDescription>
                    </CardHeader>
                    <CardContent>
                        <ClassificationLadder
                            geoipPrefixes={stored.geoip_prefixes}
                            domainLayer={stored.domain_layer}
                            rangesFetchedAt={stored.ranges_fetched_at ?? null}
                            lanCidr={draft.cidr}
                            domesticLabel={domestic}
                            foreignLabel={foreign}
                        />
                    </CardContent>
                </Card>
            </div>
        )
    }

    // Serving: what's connected is the daily read, so it comes first. The
    // write-once settings live behind the sheet.
    return (
        <div className="space-y-4">
            <LanDevices
                lanEnabled={stored.enabled}
                action={
                    <Button variant="outline" size="sm" onClick={() => setSettingsOpen(true)}>
                        <Settings2 className="mr-1.5 h-3.5 w-3.5" />
                        Settings
                    </Button>
                }
            />

            <ClassificationStrip
                geoipPrefixes={stored.geoip_prefixes}
                domainLayer={stored.domain_layer}
                rangesFetchedAt={stored.ranges_fetched_at ?? null}
                lanCidr={stored.cidr}
                domesticLabel={domestic}
                foreignLabel={foreign}
            />

            <Sheet
                open={settingsOpen}
                onOpenChange={(o) => {
                    setSettingsOpen(o)
                    if (!o) discard()
                }}
            >
                <SheetContent className="overflow-y-auto sm:max-w-lg">
                    <SheetHeader className="pb-0">
                        <SheetTitle>Local network settings</SheetTitle>
                        <SheetDescription>
                            Devices plugged into the LAN ports get an address, a resolver and split
                            routing from this box.
                        </SheetDescription>
                    </SheetHeader>
                    <div className="space-y-5 px-4">{fields}</div>
                    <SheetFooter>{applyRow}</SheetFooter>
                </SheetContent>
            </Sheet>
        </div>
    )
}
