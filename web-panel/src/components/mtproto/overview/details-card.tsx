import type { ReactNode } from "react"
import { CalendarClock, Cpu, Fingerprint, Globe, KeyRound, Network, Package, Send } from "lucide-react"
import { CopyableText } from "@/components/ui/copyable-text"
import { CountryFlag, countryName } from "@/components/ui/country-flag"
import type { MtprotoNode } from "@/lib/api/mtproto"
import type { Node } from "@/lib/types"
import { cn, formatDate, formatRelativeTime } from "@/lib/utils"
import { OverviewCard } from "@/components/node/overview/overview-card"
import { newAgentVersion, shortBuild } from "./mtproto-health"

function Spec({ label, icon, value, sub }: { label: string; icon: ReactNode; value: ReactNode; sub?: ReactNode }) {
    return (
        <div className="min-w-0">
            <div className="mb-1.5 text-[12px] leading-4 text-muted-foreground/80">{label}</div>
            <div className="flex items-center gap-2.5">
                <span className="flex w-5 shrink-0 justify-center">{icon}</span>
                <div className="min-w-0">
                    <div className="truncate text-[14px] font-semibold leading-[18px]">{value}</div>
                    {sub && <div className="truncate text-[12px] leading-4 text-muted-foreground">{sub}</div>}
                </div>
            </div>
        </div>
    )
}

const icon = "h-5 w-5"

// dev builds share a version, so the build says which one runs
function agentSub(r: MtprotoNode): string | undefined {
    const build = shortBuild(r.agent?.agent_build)
    if (r.updates?.agent) return `${newAgentVersion(r) || "new build"} available`
    if (build) return `build ${build}`
    return r.latest?.agent ? `latest ${r.latest.agent}` : undefined
}

/** The node's fixed facts: the full node's System specifications card, for a VPS we only see through the agent. */
export function DetailsCard({ node, r, className }: { node: Node; r: MtprotoNode; className?: string }) {
    const telemt = r.agent?.telemt_version || r.telemt_version
    const agent = r.agent?.agent_version || r.agent_version
    return (
        <OverviewCard glow="emerald" title="Node details" className={cn("gap-3.5", className)}>
            <div className="grid grid-cols-2 gap-x-3 gap-y-4">
                <Spec label="MTProto agent" icon={<Package className={cn(icon, "text-sky-400")} />}
                    value={<span className="font-mono" title={r.agent?.agent_build ? `Build ${r.agent.agent_build}` : undefined}>{agent || "—"}</span>}
                    sub={agentSub(r)} />
                <Spec label="Proxy (telemt)" icon={<Send className={cn(icon, "text-indigo-400")} />}
                    value={<span className="font-mono">{telemt || "—"}</span>} sub={r.latest?.telemt ? `latest ${r.latest.telemt}` : undefined} />
                <Spec label="Architecture" icon={<Cpu className={cn(icon, "text-blue-400")} />}
                    value={r.arch || "—"} sub="Linux" />
                <Spec label="Installed" icon={<CalendarClock className={cn(icon, "text-emerald-400")} />}
                    value={formatDate(r.installed_at || node.created_at)}
                    sub={formatRelativeTime(r.installed_at || node.created_at)} />
            </div>
            <div className="h-px bg-border/60" />
            <div className="flex flex-col gap-3.5">
                <Spec label="Address" icon={node.country_code ? <CountryFlag code={node.country_code} /> : <Globe className={cn(icon, "text-muted-foreground")} />}
                    value={<CopyableText text={node.ip} className="font-mono text-[14px] font-semibold" />}
                    sub={node.country_code ? countryName(node.country_code) : undefined} />
                <Spec label="SSH" icon={<KeyRound className={cn(icon, "text-amber-400")} />}
                    value={<span className="font-mono">{r.ssh_user}@{node.ip}:{r.ssh_port}</span>}
                    sub="Used only to repair or remove the node" />
                <Spec label="Host key" icon={<Fingerprint className={cn(icon, "text-violet-400")} />}
                    value={r.host_key ? <CopyableText text={r.host_key} className="font-mono text-[12px] font-medium" /> : "—"} />
                <Spec label="Agent ports" icon={<Network className={cn(icon, "text-teal-400")} />}
                    value={<span className="tabular-nums" title="Link: the panel connects to the agent here. Capture: the node hands the proxy's Telegram traffic to the agent here; it can't be reached from outside. Users connect to the proxy's own port.">Link {r.hub_port} · capture {r.capture_port}</span>}
                    sub={`Capture is local only · up to ${r.max_streams.toLocaleString()} streams`} />
            </div>
        </OverviewCard>
    )
}
