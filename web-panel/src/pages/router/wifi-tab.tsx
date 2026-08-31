import { useState } from "react"
import { toast } from "sonner"
import { Radio, RefreshCw, ShieldAlert, TriangleAlert, Wifi, WifiOff } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import {
    beaconableChannels,
    describeRadioTradeoff,
    signalBars,
    verdictsFromError,
} from "@/lib/api/network"
import {
    useConnectWifi,
    useDisableWifi,
    useEnableAP,
    useLAN,
    useNetworkInterfaces,
    useRadios,
    useScanWifi,
} from "@/lib/queries/use-network"
import type { RadioView, Verdict, WifiBand, WifiNetwork } from "@/lib/types/network"

interface Props {
    /** A change is already armed, so a second one must wait for it. */
    armed: boolean
    onApplied: () => void
}

interface APDraft {
    ssid: string
    psk: string
    country_code: string
    band: WifiBand
    channel: number
    hidden: boolean
}

const BAND_LABELS: Record<WifiBand, string> = {
    "2g": "2.4 GHz",
    "5g": "5 GHz",
    "6g": "6 GHz",
}

function apDraft(radio: RadioView): APDraft {
    const c = radio.config
    return {
        ssid: c?.ssid ?? "",
        // Never echoed back by the API, so it starts empty and empty means keep
        psk: "",
        country_code: c?.country_code || radio.country_code || "",
        band: c?.band ?? "2g",
        channel: c?.channel ?? 0,
        hidden: c?.hidden ?? false,
    }
}

/** Bands the radio actually has, so the picker cannot offer 6 GHz on a 2.4 card. */
function availableBands(radio: RadioView): WifiBand[] {
    return (["2g", "5g", "6g"] as WifiBand[]).filter((b) => (radio.bands[b]?.length ?? 0) > 0)
}

function SignalBars({ dbm }: { dbm: number }) {
    const bars = signalBars(dbm)
    return (
        <span className="flex items-end gap-0.5" aria-label={`signal ${bars} of 4`}>
            {[1, 2, 3, 4].map((n) => (
                <span
                    key={n}
                    className={
                        n <= bars ? "bg-text-primary w-0.5 rounded-sm" : "bg-border-subtle w-0.5 rounded-sm"
                    }
                    style={{ height: `${n * 3}px` }}
                />
            ))}
        </span>
    )
}

export function WifiTab({ armed, onApplied }: Props) {
    const radios = useRadios()
    const lan = useLAN()
    const interfaces = useNetworkInterfaces()
    const enableAP = useEnableAP()
    const disableWifi = useDisableWifi()
    const connect = useConnectWifi()
    const scan = useScanWifi()

    const [verdicts, setVerdicts] = useState<Verdict[]>([])
    const [drafts, setDrafts] = useState<Record<string, APDraft>>({})
    const [scans, setScans] = useState<Record<string, WifiNetwork[]>>({})
    const [joins, setJoins] = useState<Record<string, { ssid: string; psk: string }>>({})

    // Same wrapper the other tabs use: verdicts to alerts, one toast per action
    async function run(fn: () => Promise<{ verdicts?: Verdict[] } | unknown>, done: string) {
        setVerdicts([])
        try {
            const res = await fn()
            const ok = res as { verdicts?: Verdict[] } | undefined
            setVerdicts(ok?.verdicts ?? [])
            toast.success(done)
            onApplied()
        } catch (err) {
            const vs = verdictsFromError(err)
            setVerdicts(vs)
            if (vs.length === 0) {
                toast.error(err instanceof Error ? err.message : "The change did not apply")
            }
        }
    }

    function draftFor(radio: RadioView): APDraft {
        return drafts[radio.phy] ?? apDraft(radio)
    }

    function setDraft(radio: RadioView, patch: Partial<APDraft>) {
        setDrafts((prev) => ({ ...prev, [radio.phy]: { ...draftFor(radio), ...patch } }))
    }

    async function doScan(radio: RadioView) {
        try {
            const nets = await scan.mutateAsync(radio.key)
            setScans((prev) => ({ ...prev, [radio.phy]: nets }))
            if (nets.length === 0) {
                toast.success("Scan finished, nothing in range")
            }
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "The scan failed")
        }
    }

    const lanOn = lan.data?.enabled ?? false
    const rows = interfaces.data ?? []
    const assignable = rows.filter((r) => r.assignable).length
    const list = radios.data ?? []

    if (radios.isLoading) {
        return <Skeleton className="h-48 w-full" />
    }

    return (
        <div className="space-y-4">
            {radios.isError && (
                <Alert variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        The radios could not be read. {radios.error?.message}
                    </AlertDescription>
                </Alert>
            )}

            {verdicts.map((v) => (
                <Alert key={v.rule + v.message} variant="warning">
                    <TriangleAlert className="h-4 w-4" />
                    <AlertDescription>
                        <span className="text-text-tertiary font-mono text-xs">{v.rule}</span>{" "}
                        {v.message}
                    </AlertDescription>
                </Alert>
            ))}

            {list.length === 0 && !radios.isError && (
                <Card>
                    <CardHeader>
                        <CardTitle className="flex items-center gap-2">
                            <WifiOff className="h-4 w-4" />
                            No radios on this box
                        </CardTitle>
                        <CardDescription>
                            A USB Wi-Fi adapter adds one. It shows up here once plugged in.
                        </CardDescription>
                    </CardHeader>
                </Card>
            )}

            {list.map((radio) => {
                const draft = draftFor(radio)
                const tradeoff = describeRadioTradeoff(radio, assignable, list.length)
                const channels = beaconableChannels(radio, draft.band)
                const allOnBand = radio.bands[draft.band]?.length ?? 0
                const hidden = allOnBand - channels.length
                const isAP = radio.mode === "ap"
                const isStation = radio.mode === "station"
                const on = radio.config?.enabled ?? false
                const join = joins[radio.phy] ?? { ssid: "", psk: "" }
                const found = scans[radio.phy] ?? []
                const busy = enableAP.isPending || disableWifi.isPending || connect.isPending

                return (
                    <Card key={radio.phy}>
                        <CardHeader>
                            <CardTitle className="flex items-center gap-2">
                                <Radio className="h-4 w-4" />
                                {radio.if_name || radio.phy}
                                <span className="text-text-tertiary font-mono text-xs">
                                    {radio.phy}
                                </span>
                                {on && (
                                    <Badge variant="secondary">
                                        {isAP ? "Access point" : "Uplink"}
                                    </Badge>
                                )}
                            </CardTitle>
                            <CardDescription>
                                {radio.supports_ap && radio.supports_sta
                                    ? "Can serve an access point or join a network — one at a time."
                                    : radio.supports_ap
                                      ? "Can serve an access point."
                                      : "Can join a network, but cannot serve an access point."}
                                {radio.sae_supported && radio.supports_ap
                                    ? " Security is WPA3 transition mode, so WPA2 clients still join."
                                    : radio.supports_ap
                                      ? " Security is WPA2 — this hostapd build has no WPA3."
                                      : ""}
                            </CardDescription>
                        </CardHeader>

                        <CardContent className="space-y-4">
                            {tradeoff && (
                                <Alert variant="warning">
                                    <ShieldAlert className="h-4 w-4" />
                                    <AlertDescription>{tradeoff}</AlertDescription>
                                </Alert>
                            )}

                            {!isAP && !isStation && (
                                <p className="text-text-secondary text-sm">
                                    This radio holds no role yet. Give it the local-network role on
                                    the Ports tab to serve an access point, or an uplink slot to
                                    join an existing network.
                                </p>
                            )}

                            {isAP && !lanOn && (
                                <Alert variant="warning">
                                    <TriangleAlert className="h-4 w-4" />
                                    <AlertDescription>
                                        An access point needs a local network to bridge into. Turn
                                        the local network on first — the AP then shares its
                                        addresses, DNS and routing.
                                    </AlertDescription>
                                </Alert>
                            )}

                            {isAP && (
                                <fieldset
                                    disabled={armed || busy || !lanOn}
                                    className="space-y-4 disabled:opacity-50"
                                >
                                    <div className="grid gap-4 sm:grid-cols-2">
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`ssid-${radio.phy}`}>Network name</Label>
                                            <Input
                                                id={`ssid-${radio.phy}`}
                                                value={draft.ssid}
                                                onChange={(e) => setDraft(radio, { ssid: e.target.value })}
                                                placeholder="nasnet"
                                            />
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`psk-${radio.phy}`}>Passphrase</Label>
                                            <Input
                                                id={`psk-${radio.phy}`}
                                                type="password"
                                                value={draft.psk}
                                                onChange={(e) => setDraft(radio, { psk: e.target.value })}
                                                placeholder={on ? "unchanged" : "at least 8 characters"}
                                            />
                                            <p className="text-text-tertiary text-xs">
                                                Eight characters minimum — Wi-Fi itself refuses
                                                anything shorter.
                                            </p>
                                        </div>
                                    </div>

                                    <div className="space-y-1.5">
                                        <Label htmlFor={`country-${radio.phy}`}>Country</Label>
                                        <Input
                                            id={`country-${radio.phy}`}
                                            className="w-24 font-mono uppercase"
                                            maxLength={2}
                                            value={draft.country_code}
                                            onChange={(e) =>
                                                setDraft(radio, {
                                                    country_code: e.target.value.toUpperCase(),
                                                })
                                            }
                                            placeholder="IR"
                                        />
                                        {!radio.country_code_set && (
                                            <p className="text-status-warning text-xs">
                                                Without a country code the regulatory domain forbids
                                                transmitting on almost every channel, and the access
                                                point will not start.
                                            </p>
                                        )}
                                    </div>

                                    <div className="grid gap-4 sm:grid-cols-2">
                                        <div className="space-y-1.5">
                                            <Label>Band</Label>
                                            <Select
                                                value={draft.band}
                                                onValueChange={(v) =>
                                                    setDraft(radio, { band: v as WifiBand, channel: 0 })
                                                }
                                            >
                                                <SelectTrigger>
                                                    <SelectValue />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    {availableBands(radio).map((b) => (
                                                        <SelectItem key={b} value={b}>
                                                            {BAND_LABELS[b]}
                                                        </SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label>Channel</Label>
                                            <Select
                                                value={String(draft.channel)}
                                                onValueChange={(v) =>
                                                    setDraft(radio, { channel: Number(v) })
                                                }
                                            >
                                                <SelectTrigger>
                                                    <SelectValue />
                                                </SelectTrigger>
                                                <SelectContent>
                                                    <SelectItem value="0">Automatic</SelectItem>
                                                    {channels.map((c) => (
                                                        <SelectItem
                                                            key={c.number}
                                                            value={String(c.number)}
                                                        >
                                                            {c.number} ({c.freq_mhz} MHz)
                                                        </SelectItem>
                                                    ))}
                                                </SelectContent>
                                            </Select>
                                            {hidden > 0 && (
                                                <p className="text-text-tertiary text-xs">
                                                    {hidden} channel{hidden === 1 ? "" : "s"} hidden:
                                                    radar-only or not permitted to transmit in{" "}
                                                    {draft.country_code || "this country"}.
                                                </p>
                                            )}
                                        </div>
                                    </div>

                                    <div className="border-border-subtle flex items-center justify-between rounded-lg border p-3">
                                        <div className="pr-4">
                                            <Label
                                                htmlFor={`hidden-${radio.phy}`}
                                                className="text-sm font-medium"
                                            >
                                                Hide the network name
                                            </Label>
                                            <p className="text-text-secondary mt-0.5 text-xs">
                                                Clients have to type it in. Not a security measure.
                                            </p>
                                        </div>
                                        <Switch
                                            id={`hidden-${radio.phy}`}
                                            checked={draft.hidden}
                                            onCheckedChange={(v) => setDraft(radio, { hidden: v })}
                                        />
                                    </div>

                                    <div className="flex gap-2">
                                        <Button
                                            onClick={() =>
                                                void run(
                                                    () =>
                                                        enableAP.mutateAsync({
                                                            interface_id: radio.interface_id,
                                                            ...draft,
                                                        }),
                                                    on
                                                        ? "Access point settings applied"
                                                        : "Access point turned on",
                                                )
                                            }
                                        >
                                            <Wifi className="h-4 w-4" />
                                            {on ? "Apply changes" : "Turn on"}
                                        </Button>
                                        {on && (
                                            <Button
                                                variant="outline"
                                                onClick={() =>
                                                    void run(
                                                        () => disableWifi.mutateAsync(radio.key),
                                                        "Access point turned off",
                                                    )
                                                }
                                            >
                                                Turn off
                                            </Button>
                                        )}
                                    </div>
                                </fieldset>
                            )}

                            {isStation && (
                                <div className="space-y-4">
                                    <p className="text-text-secondary text-sm">
                                        The box joins this network as an uplink; addressing comes
                                        from that network's DHCP. Its health shows in the strip
                                        above, like any other uplink.
                                    </p>

                                    {radio.config?.ssid && (
                                        <p className="text-sm">
                                            Joined <span className="font-mono">{radio.config.ssid}</span>
                                        </p>
                                    )}

                                    <div className="flex gap-2">
                                        <Button
                                            variant="outline"
                                            disabled={scan.isPending}
                                            onClick={() => void doScan(radio)}
                                        >
                                            <RefreshCw className="h-4 w-4" />
                                            {scan.isPending ? "Scanning…" : "Scan"}
                                        </Button>
                                        {on && (
                                            <Button
                                                variant="outline"
                                                disabled={armed || busy}
                                                onClick={() =>
                                                    void run(
                                                        () => disableWifi.mutateAsync(radio.key),
                                                        "Uplink left the network",
                                                    )
                                                }
                                            >
                                                Leave
                                            </Button>
                                        )}
                                    </div>

                                    {found.length > 0 && (
                                        <div className="border-border-subtle divide-border-subtle divide-y rounded-lg border">
                                            {found.map((n) => (
                                                <button
                                                    key={n.ssid}
                                                    type="button"
                                                    className="hover:bg-surface-hover flex w-full items-center justify-between p-3 text-left"
                                                    onClick={() =>
                                                        setJoins((prev) => ({
                                                            ...prev,
                                                            [radio.phy]: { ssid: n.ssid, psk: "" },
                                                        }))
                                                    }
                                                >
                                                    <span className="flex items-center gap-2">
                                                        <SignalBars dbm={n.signal_dbm} />
                                                        <span className="text-sm">{n.ssid}</span>
                                                        {n.connected && (
                                                            <Badge variant="secondary">joined</Badge>
                                                        )}
                                                    </span>
                                                    <span className="text-text-tertiary text-xs">
                                                        {n.security}
                                                    </span>
                                                </button>
                                            ))}
                                        </div>
                                    )}

                                    <fieldset
                                        disabled={armed || busy}
                                        className="grid gap-4 disabled:opacity-50 sm:grid-cols-2"
                                    >
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`join-ssid-${radio.phy}`}>
                                                Network name
                                            </Label>
                                            <Input
                                                id={`join-ssid-${radio.phy}`}
                                                value={join.ssid}
                                                onChange={(e) =>
                                                    setJoins((prev) => ({
                                                        ...prev,
                                                        [radio.phy]: { ...join, ssid: e.target.value },
                                                    }))
                                                }
                                                placeholder="pick one above, or type it"
                                            />
                                        </div>
                                        <div className="space-y-1.5">
                                            <Label htmlFor={`join-psk-${radio.phy}`}>Passphrase</Label>
                                            <Input
                                                id={`join-psk-${radio.phy}`}
                                                type="password"
                                                value={join.psk}
                                                onChange={(e) =>
                                                    setJoins((prev) => ({
                                                        ...prev,
                                                        [radio.phy]: { ...join, psk: e.target.value },
                                                    }))
                                                }
                                                placeholder="leave empty for an open network"
                                            />
                                        </div>
                                        <div>
                                            <Button
                                                disabled={!join.ssid}
                                                onClick={() =>
                                                    void run(
                                                        () =>
                                                            connect.mutateAsync({
                                                                key: radio.key,
                                                                ssid: join.ssid,
                                                                psk: join.psk,
                                                            }),
                                                        `Joining ${join.ssid}`,
                                                    )
                                                }
                                            >
                                                <Wifi className="h-4 w-4" />
                                                Join
                                            </Button>
                                        </div>
                                    </fieldset>
                                </div>
                            )}
                        </CardContent>
                    </Card>
                )
            })}
        </div>
    )
}
