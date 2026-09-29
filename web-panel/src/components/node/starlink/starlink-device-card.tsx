import { Lock } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import type { StarlinkDevice, StarlinkStatus } from "@/lib/types"
import { enumLabel, formatUptime, rateLimitLabel } from "./starlink-helpers"
import { KeyValue, StarlinkCard, T } from "./starlink-ui"

interface StarlinkDeviceCardProps {
    status: StarlinkStatus
    /** Undefined for legacy agents or while loading — the card falls back to status fields. */
    device?: StarlinkDevice
}

// cady is left out: dishes that do not report it send false, which read as a fault.
const READY_ORDER = ["scp", "l1l2", "xphy", "aap", "rf"] as const

function minutesToClock(m: number): string {
    return `${String(Math.floor(m / 60) % 24).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`
}

export function StarlinkDeviceCard({ status, device }: StarlinkDeviceCardProps) {
    const cfg = device?.config
    const dl = rateLimitLabel(status.dl_bandwidth_restricted_reason)
    const ul = rateLimitLabel(status.ul_bandwidth_restricted_reason)
    const ready = status.ready_states

    return (
        <StarlinkCard title="Device">
            <div className="flex flex-col gap-2">
                <KeyValue label="Hardware">{status.hardware_version || "—"}</KeyValue>
                <KeyValue label="Firmware"><span className="font-mono text-[12px]">{status.software_version || "—"}</span></KeyValue>
                {device?.id && <KeyValue label="Dish ID"><span className="font-mono text-[12px]">{device.id}</span></KeyValue>}
                <KeyValue label="Uptime">{formatUptime(status.uptime_s)} · {status.boot_count} boots</KeyValue>
                {status.country_code && <KeyValue label="Country">{status.country_code}</KeyValue>}
                {device && (
                    <KeyValue label="Self-test">
                        <Badge variant="outline" className={`px-2 py-0 text-[12px] ${
                            device.hardware_self_test === "PASSED" ? "border-emerald-400/30 text-emerald-400"
                                : device.hardware_self_test === "FAILED" ? "border-red-400/30 text-red-400"
                                    : "text-muted-foreground"}`}>
                            {enumLabel(device.hardware_self_test)}
                        </Badge>
                    </KeyValue>
                )}
            </div>

            <div className="flex flex-col gap-2 border-t border-border pt-3">
                {ready && (
                    <div className="flex items-center justify-between gap-3 text-xs leading-4">
                        <span className="text-muted-foreground">Ready</span>
                        <span className="flex flex-wrap justify-end gap-1">
                            {READY_ORDER.filter((k) => k in ready).map((k) => (
                                <span key={k} className={`rounded px-1.5 py-0.5 font-mono text-[11px] uppercase ${ready[k] ? "bg-emerald-500/10 text-emerald-300" : "bg-red-500/10 text-red-300"}`}>{k}</span>
                            ))}
                        </span>
                    </div>
                )}
                <KeyValue label="GPS">
                    {status.gps_inhibited
                        ? <span className="text-amber-400">Inhibited</span>
                        : status.gps_valid ? `${status.gps_sats} sats` : <span className="text-red-400">No fix</span>}
                </KeyValue>
                <KeyValue label="Ethernet">
                    {status.eth_speed_mbps >= 1000 ? `${(status.eth_speed_mbps / 1000).toFixed(0)} Gbps` : `${status.eth_speed_mbps} Mbps`}
                </KeyValue>
                <KeyValue label="Service">{enumLabel(status.mobility_class)} · {enumLabel(status.class_of_service)}</KeyValue>
                <KeyValue label="Bandwidth">
                    {dl || ul ? <span className="text-amber-400">Capped{dl ? ` ↓ ${dl}` : ""}{ul ? ` ↑ ${ul}` : ""}</span> : "No cap"}
                </KeyValue>
                {status.disablement_code && status.disablement_code !== "OKAY" && (
                    <KeyValue label="Disablement"><span className="text-red-400">{enumLabel(status.disablement_code)}</span></KeyValue>
                )}
                <KeyValue label="Cell / sat / GW">
                    {status.context_locked ? <Locked /> : status.cell_id ? `${status.cell_id} / ${status.satellite_id} / ${status.gateway_id}` : "—"}
                </KeyValue>
                <KeyValue label="Location">
                    {status.location_locked ? <Locked /> : status.latitude || status.longitude
                        ? `${status.latitude.toFixed(4)}, ${status.longitude.toFixed(4)}` : "—"}
                </KeyValue>
                {(status.context_locked || status.location_locked) && (
                    <p className={`${T.meta} text-right`}>Enable local network access in the Starlink app to unlock.</p>
                )}
            </div>

            {cfg && (
                <div className="flex flex-col gap-2 border-t border-border pt-3">
                    <p className={T.eyebrow}>Config <span className="normal-case tracking-normal">· set in the Starlink app</span></p>
                    <KeyValue label="Power save">
                        {cfg.power_save_mode
                            ? `${minutesToClock(cfg.power_save_start_minutes)} for ${Math.round(cfg.power_save_duration_minutes / 60 * 10) / 10}h`
                            : "Off"}
                    </KeyValue>
                    <KeyValue label="Snow melt">{enumLabel(cfg.snow_melt_mode)}</KeyValue>
                    <KeyValue label="Updates">
                        Reboot at {String(cfg.swupdate_reboot_hour).padStart(2, "0")}:00{cfg.swupdate_three_day_deferral ? " · 3-day deferral" : ""}
                    </KeyValue>
                    <KeyValue label="Level mode">{enumLabel(cfg.level_dish_mode)}</KeyValue>
                    <KeyValue label="Local location">{cfg.location_request_mode === "LOCAL" ? "Allowed" : "Off"}</KeyValue>
                </div>
            )}
        </StarlinkCard>
    )
}

function Locked() {
    return (
        <span className="inline-flex items-center gap-1 text-muted-foreground">
            <Lock className="h-3 w-3" aria-hidden /> Locked by dish
        </span>
    )
}
