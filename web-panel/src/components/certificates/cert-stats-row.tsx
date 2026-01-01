import { cn } from "@/lib/utils"
import { useCertificatesStore, type CertFilter } from "@/store/certificates-store"
import type { AgentCertificate } from "@/lib/types"

interface CertStatsRowProps {
    certificates: AgentCertificate[]
}

export function CertStatsRow({ certificates }: CertStatsRowProps) {
    const { activeFilter, setActiveFilter, activeTab } = useCertificatesStore()

    // Filter certs based on active tab
    const tabCerts = certificates.filter(c =>
        activeTab === "internal"
            ? (c.type === "ca" || c.type === "master" || c.type === "agent")
            : c.type === "public"
    )

    const counts = {
        all: tabCerts.length,
        valid: tabCerts.filter(c => c.is_valid && !c.is_revoked && c.days_until_expiry >= 0).length,
        expiring: tabCerts.filter(c => !c.is_revoked && c.days_until_expiry > 0 && c.days_until_expiry <= 30).length,
        expired: tabCerts.filter(c => c.days_until_expiry < 0 && !c.is_revoked).length,
        revoked: tabCerts.filter(c => c.is_revoked).length,
    }

    const chips: { key: CertFilter; label: string; count: number; color: string; activeColor: string }[] = [
        { key: "all", label: "All", count: counts.all, color: "text-foreground", activeColor: "bg-foreground text-background" },
        { key: "valid", label: "Valid", count: counts.valid, color: "text-emerald-600 dark:text-emerald-400", activeColor: "bg-emerald-600 text-white dark:bg-emerald-500" },
        { key: "expiring", label: "Expiring", count: counts.expiring, color: "text-amber-600 dark:text-amber-400", activeColor: "bg-amber-600 text-white dark:bg-amber-500" },
        { key: "expired", label: "Expired", count: counts.expired, color: "text-red-600 dark:text-red-400", activeColor: "bg-red-600 text-white dark:bg-red-500" },
        { key: "revoked", label: "Revoked", count: counts.revoked, color: "text-red-600 dark:text-red-400", activeColor: "bg-red-600 text-white dark:bg-red-500" },
    ]

    return (
        <div className="flex gap-2 overflow-x-auto pb-1 scrollbar-none -mx-1 px-1">
            {chips.map(chip => {
                const isActive = activeFilter === chip.key
                return (
                    <button
                        key={chip.key}
                        onClick={() => setActiveFilter(chip.key)}
                        className={cn(
                            "inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full text-xs font-medium whitespace-nowrap transition-all duration-150 shrink-0",
                            "border",
                            isActive
                                ? cn(chip.activeColor, "border-transparent shadow-sm")
                                : cn("bg-background border-border/60 hover:border-border", chip.color)
                        )}
                    >
                        {chip.label}
                        <span className={cn(
                            "inline-flex items-center justify-center min-w-[18px] h-[18px] rounded-full text-[10px] font-semibold px-1",
                            isActive
                                ? "bg-white/20"
                                : "bg-muted"
                        )}>
                            {chip.count}
                        </span>
                    </button>
                )
            })}
        </div>
    )
}
