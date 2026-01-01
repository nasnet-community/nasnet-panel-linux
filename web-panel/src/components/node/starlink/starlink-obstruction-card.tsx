import { Card } from "@/components/ui/card"
import { Compass } from "lucide-react"
import type { StarlinkStatus } from "@/lib/types"

interface StarlinkObstructionCardProps {
    status: StarlinkStatus
    onClick: () => void
}

export function StarlinkObstructionCard({ status, onClick }: StarlinkObstructionCardProps) {
    const clearPct = ((1 - status.obstruction_fraction) * 100).toFixed(1)
    const obstructedPct = (status.obstruction_fraction * 100).toFixed(1)

    return (
        <Card
            role="button"
            tabIndex={0}
            aria-label={`Obstruction map. ${clearPct}% clear, ${obstructedPct}% obstructed. Open details.`}
            className="h-full relative overflow-hidden group transition-shadow duration-300 hover:shadow-lg hover:shadow-teal-500/10 rounded-2xl p-4 bg-card/50 backdrop-blur-sm border-white/5 cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={onClick}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onClick() } }}
        >
            <div className="flex items-center justify-between mb-3">
                <p className="text-[11px] uppercase font-bold text-muted-foreground/70 tracking-[0.15em]">Obstruction Map</p>
                <span className="text-[10px] text-muted-foreground/40 group-hover:text-muted-foreground/70 transition-colors">details &rarr;</span>
            </div>
            <div className="flex items-center gap-4">
                <div className="w-[60px] h-[60px] rounded-full bg-gradient-to-br from-teal-500/30 to-teal-500/10 border border-white/10 flex items-center justify-center shrink-0">
                    <Compass className="w-5 h-5 text-teal-400/60" />
                </div>
                <div className="flex flex-col gap-1.5">
                    <span className="text-xs text-emerald-400 font-semibold">{clearPct}% Clear</span>
                    <span className="text-xs text-red-400 font-semibold">{obstructedPct}% Obstructed</span>
                    <span className="text-[11px] text-muted-foreground">
                        {status.currently_obstructed ? "Currently obstructed" : "Not currently obstructed"}
                    </span>
                </div>
            </div>
        </Card>
    )
}
