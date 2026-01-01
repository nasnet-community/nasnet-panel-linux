import { useMemo } from "react"
import { cn } from "@/lib/utils"
import type { AgentCertificate } from "@/lib/types"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { Badge } from "@/components/ui/badge"
import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
} from "@/components/ui/tooltip"

interface CertTimelineProps {
    certificates: AgentCertificate[]
    onCertClick?: (cert: AgentCertificate) => void
}

function getUrgencyColor(days: number) {
    if (days < 0) return "bg-muted-foreground/50"
    if (days <= 7) return "bg-red-500"
    if (days <= 30) return "bg-amber-500"
    return "bg-emerald-500"
}

function getUrgencyRing(days: number) {
    if (days < 0) return "ring-muted-foreground/30"
    if (days <= 7) return "ring-red-500/30"
    if (days <= 30) return "ring-amber-500/30"
    return "ring-emerald-500/30"
}

export function CertTimeline({ certificates, onCertClick }: CertTimelineProps) {
    const isMobile = useIsMobile()

    // Get next 12 months of data
    const { months, certPositions, todayPosition } = useMemo(() => {
        const now = new Date()
        const months: { date: Date; label: string }[] = []

        for (let i = 0; i < 12; i++) {
            const d = new Date(now.getFullYear(), now.getMonth() + i, 1)
            months.push({
                date: d,
                label: d.toLocaleDateString(undefined, { month: "short", year: i === 0 || d.getMonth() === 0 ? "2-digit" : undefined }),
            })
        }

        const startTime = months[0].date.getTime()
        const endTime = new Date(now.getFullYear(), now.getMonth() + 12, 1).getTime()
        const totalRange = endTime - startTime

        const todayPosition = ((now.getTime() - startTime) / totalRange) * 100

        // Group certs that are within range
        const certPositions = certificates
            .filter(c => !c.is_revoked)
            .map(cert => {
                const expiryTime = new Date(cert.not_after).getTime()
                const position = ((expiryTime - startTime) / totalRange) * 100
                return { cert, position: Math.max(0, Math.min(100, position)), inRange: position >= 0 && position <= 100 }
            })
            .filter(c => c.inRange)
            .sort((a, b) => a.position - b.position)

        return { months, certPositions, todayPosition }
    }, [certificates])

    if (certPositions.length === 0) {
        return (
            <div className="text-center py-8 text-muted-foreground text-sm">
                No certificate expirations in the next 12 months
            </div>
        )
    }

    if (isMobile) {
        // Vertical timeline for mobile
        return (
            <div className="relative pl-6 space-y-0">
                {/* Vertical line */}
                <div className="absolute left-[11px] top-0 bottom-0 w-0.5 bg-border" />

                {/* Today marker */}
                <div className="relative flex items-center gap-3 py-2">
                    <div className="absolute left-[-13px] w-3 h-3 rounded-full bg-primary ring-2 ring-primary/30 z-10" />
                    <span className="text-xs font-semibold text-primary pl-1">Today</span>
                </div>

                {certPositions.map(({ cert }, idx) => (
                    <div
                        key={cert.id}
                        className="relative flex items-start gap-3 py-2 cursor-pointer hover:bg-muted/30 rounded-r-md pr-2"
                        onClick={() => onCertClick?.(cert)}
                    >
                        <div className={cn(
                            "absolute left-[-11px] w-2.5 h-2.5 rounded-full ring-2 z-10 mt-1.5",
                            getUrgencyColor(cert.days_until_expiry),
                            getUrgencyRing(cert.days_until_expiry),
                        )} />
                        <div className="flex-1 min-w-0 pl-1">
                            <div className="flex items-center gap-2">
                                <span className="text-sm font-medium truncate">{cert.common_name}</span>
                                <Badge variant="outline" className="capitalize text-[9px] px-1 py-0 font-normal shrink-0">
                                    {cert.type}
                                </Badge>
                            </div>
                            <span className={cn(
                                "text-xs font-medium",
                                cert.days_until_expiry <= 7 ? "text-red-500" :
                                    cert.days_until_expiry <= 30 ? "text-amber-500" : "text-muted-foreground"
                            )}>
                                {cert.days_until_expiry > 0
                                    ? `Expires in ${cert.days_until_expiry} days`
                                    : "Expired"
                                }
                                {" · "}
                                {new Date(cert.not_after).toLocaleDateString(undefined, { month: "short", day: "numeric" })}
                            </span>
                        </div>
                    </div>
                ))}
            </div>
        )
    }

    // Horizontal timeline for desktop
    return (
        <TooltipProvider>
            <div className="space-y-2">
                {/* Month labels */}
                <div className="relative h-6 flex">
                    {months.map((month, i) => (
                        <div
                            key={i}
                            className="flex-1 text-[10px] text-muted-foreground font-medium border-l border-border/50 pl-1"
                        >
                            {month.label}
                        </div>
                    ))}
                </div>

                {/* Timeline bar */}
                <div className="relative h-12 bg-muted/30 rounded-md border border-border/50">
                    {/* Today line */}
                    <div
                        className="absolute top-0 bottom-0 w-0.5 bg-primary z-10"
                        style={{ left: `${todayPosition}%` }}
                    >
                        <div className="absolute -top-5 left-1/2 -translate-x-1/2 text-[9px] font-semibold text-primary whitespace-nowrap">
                            Today
                        </div>
                    </div>

                    {/* Certificate dots */}
                    {certPositions.map(({ cert, position }) => (
                        <Tooltip key={cert.id}>
                            <TooltipTrigger asChild>
                                <button
                                    className={cn(
                                        "absolute top-1/2 -translate-y-1/2 -translate-x-1/2 w-3 h-3 rounded-full ring-2 transition-transform hover:scale-150 z-20",
                                        getUrgencyColor(cert.days_until_expiry),
                                        getUrgencyRing(cert.days_until_expiry),
                                    )}
                                    style={{ left: `${position}%` }}
                                    onClick={() => onCertClick?.(cert)}
                                />
                            </TooltipTrigger>
                            <TooltipContent side="top" className="max-w-[220px]">
                                <div className="space-y-1">
                                    <p className="text-xs font-medium truncate">{cert.common_name}</p>
                                    <p className="text-[10px] text-muted-foreground capitalize">{cert.type} certificate</p>
                                    <p className={cn(
                                        "text-[10px] font-medium",
                                        cert.days_until_expiry <= 7 ? "text-red-500" :
                                            cert.days_until_expiry <= 30 ? "text-amber-500" : "text-emerald-500"
                                    )}>
                                        {cert.days_until_expiry > 0
                                            ? `${cert.days_until_expiry} days remaining`
                                            : "Expired"
                                        }
                                    </p>
                                    <p className="text-[10px] text-muted-foreground">
                                        {new Date(cert.not_after).toLocaleDateString()}
                                    </p>
                                </div>
                            </TooltipContent>
                        </Tooltip>
                    ))}
                </div>
            </div>
        </TooltipProvider>
    )
}
