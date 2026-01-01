import { motion, AnimatePresence } from "framer-motion"
import { Shield, ChevronDown, CheckCircle2, AlertTriangle, Plus, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useInitializeCA } from "@/lib/queries"
import { useCertificatesStore } from "@/store/certificates-store"
import type { AgentCertificate } from "@/lib/types"
import { cn } from "@/lib/utils"

interface CAStatusBannerProps {
    certificates: AgentCertificate[]
}

export function CAStatusBanner({ certificates }: CAStatusBannerProps) {
    const initCAMutation = useInitializeCA()
    const { caBannerExpanded, toggleCaBanner } = useCertificatesStore()

    const caCert = certificates.find((c) => c.type === "ca" && !c.is_revoked)
    const hasCA = !!caCert

    return (
        <div className="rounded-lg border border-border/50 overflow-hidden">
            {/* Collapsed banner bar */}
            <button
                onClick={toggleCaBanner}
                className={cn(
                    "w-full flex items-center gap-3 px-4 py-3 transition-colors text-left",
                    hasCA
                        ? "bg-emerald-500/5 hover:bg-emerald-500/10"
                        : "bg-amber-500/5 hover:bg-amber-500/10"
                )}
            >
                <Shield className={cn("h-4 w-4 shrink-0", hasCA ? "text-emerald-500" : "text-amber-500")} />
                <span className="flex-1 text-sm font-medium">Certificate Authority</span>
                <Badge variant={hasCA ? "success" : "warning"} className="text-[10px] px-2 py-0">
                    {hasCA ? "Active" : "Not Initialized"}
                </Badge>
                <ChevronDown className={cn(
                    "h-4 w-4 text-muted-foreground transition-transform duration-200",
                    caBannerExpanded && "rotate-180"
                )} />
            </button>

            {/* Expanded details */}
            <AnimatePresence>
                {caBannerExpanded && (
                    <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: "auto", opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.2, ease: "easeInOut" }}
                        className="overflow-hidden"
                    >
                        <div className="px-4 py-3 border-t border-border/50 bg-card/50 space-y-3">
                            {hasCA && caCert ? (
                                <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                                    <div>
                                        <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-medium">Common Name</p>
                                        <p className="text-sm font-mono mt-0.5 truncate">{caCert.common_name}</p>
                                    </div>
                                    <div>
                                        <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-medium">Serial</p>
                                        <p className="text-sm font-mono mt-0.5 truncate">
                                            {caCert.serial_number.length > 20
                                                ? `${caCert.serial_number.slice(0, 20)}...`
                                                : caCert.serial_number}
                                        </p>
                                    </div>
                                    <div>
                                        <p className="text-[10px] uppercase tracking-wider text-muted-foreground font-medium">Expires</p>
                                        <p className="text-sm mt-0.5">
                                            <span className={cn(
                                                "font-medium",
                                                caCert.days_until_expiry > 30
                                                    ? "text-emerald-500"
                                                    : caCert.days_until_expiry > 7
                                                        ? "text-amber-500"
                                                        : "text-red-500"
                                            )}>
                                                {caCert.days_until_expiry > 0
                                                    ? `${caCert.days_until_expiry} days`
                                                    : "Expired"}
                                            </span>
                                            <span className="text-muted-foreground ml-1.5 text-xs">
                                                ({new Date(caCert.not_after).toLocaleDateString()})
                                            </span>
                                        </p>
                                    </div>
                                </div>
                            ) : (
                                <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                                    <div className="flex items-start gap-2">
                                        <AlertTriangle className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
                                        <p className="text-sm text-muted-foreground">
                                            Initialize the CA to start generating mTLS certificates for node agents.
                                        </p>
                                    </div>
                                    <Button
                                        size="sm"
                                        onClick={(e) => {
                                            e.stopPropagation()
                                            initCAMutation.mutate({})
                                        }}
                                        disabled={initCAMutation.isPending}
                                        className="shrink-0"
                                    >
                                        {initCAMutation.isPending ? (
                                            <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                                        ) : (
                                            <Plus className="h-3.5 w-3.5 mr-1.5" />
                                        )}
                                        {initCAMutation.isPending ? "Initializing..." : "Initialize CA"}
                                    </Button>
                                </div>
                            )}
                        </div>
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    )
}
