import { AlertTriangle, Info, XCircle } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { Issue, IssueAction, IssueLevel } from "./mtproto-health"

const LEVEL: Record<IssueLevel, { icon: typeof Info; tone: string; box: string }> = {
    error: { icon: XCircle, tone: "text-red-500", box: "border-red-500/30 bg-red-500/[0.06]" },
    warn: { icon: AlertTriangle, tone: "text-amber-500", box: "border-amber-500/30 bg-amber-500/[0.06]" },
    info: { icon: Info, tone: "text-sky-400", box: "border-sky-500/25 bg-sky-500/[0.05]" },
}

const ACTION_LABEL: Record<IssueAction, string> = {
    repair: "Repair…",
    restart: "Restart proxy",
    "start-proxy": "Start proxy",
    "update-agent": "Update agent",
    "update-proxy": "Update proxy",
    "refresh-list": "Refresh list",
    tunnels: "Open tunnel",
    settings: "Settings",
    logs: "View log",
    vpn: "Open VPN",
}

/** The checks that failed, worst first, each with the one thing to do about it. */
export function Attention({ issues, onAction, busy }: { issues: Issue[]; onAction: (a: IssueAction) => void; busy?: IssueAction | null }) {
    if (!issues.length) return null
    const worst = issues[0].level
    return (
        <section aria-label="Needs attention" className={cn("rounded-2xl border p-1.5", LEVEL[worst].box)}>
            <ul className="divide-y divide-border/50">
                {issues.map((i) => {
                    const { icon: Icon, tone } = LEVEL[i.level]
                    return (
                        <li key={i.id} className="flex flex-col gap-2 px-2.5 py-2.5 sm:flex-row sm:items-center sm:gap-3">
                            <div className="flex min-w-0 flex-1 items-start gap-2.5">
                                <Icon className={cn("mt-0.5 size-4 shrink-0", tone)} aria-hidden />
                                <div className="min-w-0">
                                    <p className="text-sm font-medium leading-5">{i.title}</p>
                                    {i.detail && <p className="break-words text-xs leading-4 text-muted-foreground">{i.detail}</p>}
                                </div>
                            </div>
                            {i.action && (
                                <Button type="button" size="sm" variant="outline" className="h-8 shrink-0 self-start sm:self-auto" disabled={busy === i.action} onClick={() => onAction(i.action!)}>
                                    {ACTION_LABEL[i.action]}
                                </Button>
                            )}
                        </li>
                    )
                })}
            </ul>
        </section>
    )
}
