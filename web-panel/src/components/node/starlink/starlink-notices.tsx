import { useEffect, useState } from "react"
import { ArrowUpCircle, Gauge, Loader2, PlugZap } from "lucide-react"
import type { StarlinkAction, StarlinkStatus } from "@/lib/types"
import { rateLimitLabel } from "./starlink-helpers"

interface StarlinkNoticesProps {
    status: StarlinkStatus
    agentOutdated: boolean
    waiting?: { action: StarlinkAction; since: number } | null
}

// One line per condition, only when it applies. The outage banner stays in
// the status header, where it has always been.
export function StarlinkNotices({ status, agentOutdated, waiting }: StarlinkNoticesProps) {
    const dl = rateLimitLabel(status.dl_bandwidth_restricted_reason)
    const ul = rateLimitLabel(status.ul_bandwidth_restricted_reason)
    const updatePending = status.update_requires_reboot || status.swupdate_reboot_ready

    return (
        <>
            {waiting && <WaitingNotice since={waiting.since} action={waiting.action} />}
            {agentOutdated && (
                <Notice tone="amber" icon={PlugZap}>
                    This node's agent predates dish passthrough — live 1 s data, power, device info and dish actions need an agent update (Update Agent, top right).
                </Notice>
            )}
            {updatePending && (
                <Notice tone="blue" icon={ArrowUpCircle}>
                    A dish firmware update is installed and waits for a reboot
                    {status.swupdate_reboot_ready ? " — the dish can reboot now." : "."}
                </Notice>
            )}
            {(dl || ul) && (
                <Notice tone="amber" icon={Gauge}>
                    Bandwidth capped by Starlink: {dl && `download (${dl})`}{dl && ul && ", "}{ul && `upload (${ul})`}.
                </Notice>
            )}
        </>
    )
}

function WaitingNotice({ since, action }: { since: number; action: StarlinkAction }) {
    const [now, setNow] = useState(() => Date.now())
    useEffect(() => {
        const id = setInterval(() => setNow(Date.now()), 1000)
        return () => clearInterval(id)
    }, [])
    const secs = Math.max(0, Math.floor((now - since) / 1000))
    const clock = `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, "0")}`
    return (
        <Notice tone="blue" icon={Loader2} spin>
            {action === "factory-reset" ? "Dish is resetting" : "Dish is rebooting"} — waiting for it to come back ({clock}).
        </Notice>
    )
}

const TONES = {
    amber: "border-amber-500/25 bg-amber-500/[0.07] text-amber-200",
    blue: "border-blue-500/25 bg-blue-500/[0.07] text-blue-200",
} as const

function Notice({ tone, icon: Icon, spin, children }: {
    tone: keyof typeof TONES
    icon: React.ComponentType<{ className?: string }>
    spin?: boolean
    children: React.ReactNode
}) {
    return (
        <div className={`flex items-start gap-2.5 rounded-xl border px-3.5 py-2.5 text-xs leading-4 ${TONES[tone]}`}>
            <Icon className={`mt-px h-4 w-4 shrink-0 ${spin ? "animate-spin" : ""}`} />
            <p>{children}</p>
        </div>
    )
}
