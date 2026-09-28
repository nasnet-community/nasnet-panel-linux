import { AlertTriangle, CheckCircle2, Info, ShieldAlert } from "lucide-react"
import { Button } from "@/components/ui/button"
import type { AttentionItem, AttentionKind } from "@/lib/types"
import { cn } from "@/lib/utils"
import { toneText, type Tone } from "./cert-helpers"
import { CertCard } from "./cert-ui"

const ORDER: AttentionKind[] = ["strict_blocked", "mismatch", "cannot_rotate", "expiring", "orphans", "legacy"]
const TONE: Record<AttentionKind, Tone> = {
    strict_blocked: "danger",
    mismatch: "warn",
    cannot_rotate: "danger",
    expiring: "warn",
    orphans: "warn",
    legacy: "muted",
}

interface CertAttentionCardProps {
    items: AttentionItem[]
    onRevokeOrphans: () => void
    onRotateExpiring: () => void
    onShowLegacy: () => void
}

export function CertAttentionCard({ items, onRevokeOrphans, onRotateExpiring, onShowLegacy }: CertAttentionCardProps) {
    const sorted = [...items].sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind))
    const action = (kind: AttentionKind) => {
        switch (kind) {
            case "orphans": return { label: "Revoke orphans", run: onRevokeOrphans }
            case "expiring": return { label: "Rotate now", run: onRotateExpiring }
            case "legacy": return { label: "Show", run: onShowLegacy }
            default: return null
        }
    }

    return (
        <CertCard title="Needs attention">
            {sorted.length === 0 ? (
                <div className="flex items-center gap-2 text-sm text-emerald-500">
                    <CheckCircle2 className="h-4 w-4" />All clear
                </div>
            ) : (
                <ul className="flex flex-col divide-y divide-border/60">
                    {sorted.map(item => {
                        const tone = TONE[item.kind]
                        const Icon = tone === "danger" ? ShieldAlert : tone === "warn" ? AlertTriangle : Info
                        const act = action(item.kind)
                        return (
                            <li key={item.kind} className="flex flex-col gap-2 py-2.5 first:pt-0 last:pb-0 sm:flex-row sm:items-center">
                                <div className="flex min-w-0 flex-1 items-start gap-2">
                                    <Icon className={cn("mt-0.5 h-4 w-4 shrink-0", toneText[tone])} />
                                    <span className="text-sm">{item.message}</span>
                                </div>
                                {act && (
                                    <Button variant="outline" size="sm" className="h-11 shrink-0 sm:h-8" onClick={act.run}>{act.label}</Button>
                                )}
                            </li>
                        )
                    })}
                </ul>
            )}
        </CertCard>
    )
}
