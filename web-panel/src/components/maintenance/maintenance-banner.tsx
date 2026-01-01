import { motion, AnimatePresence } from "framer-motion"
import { Wrench, AlertTriangle } from "lucide-react"
import type { MaintenanceStatus } from "@/lib/api/maintenance"

interface MaintenanceBannerProps {
  status: MaintenanceStatus | null | undefined
}

export function MaintenanceBanner({ status }: MaintenanceBannerProps) {
  const active = !!status?.active
  return (
    <AnimatePresence>
      {active && (
        <motion.div
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: -8 }}
          transition={{ duration: 0.2 }}
          className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-3 flex items-start gap-3"
          role="alert"
        >
          <Wrench className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-amber-500 mb-0.5">
              Maintenance in progress
            </p>
            <p dir="auto" className="text-sm text-muted-foreground whitespace-pre-wrap">
              {status?.message || "Service maintenance is currently underway."}
            </p>
            {status?.since && (
              <p className="text-xs text-muted-foreground/80 mt-1">
                Since {new Date(status.since).toLocaleString()}
              </p>
            )}
          </div>
          <AlertTriangle className="h-4 w-4 text-amber-500/60 shrink-0 mt-0.5" />
        </motion.div>
      )}
    </AnimatePresence>
  )
}
