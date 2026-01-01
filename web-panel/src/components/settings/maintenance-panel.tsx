import { useState, useEffect } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { setGlobalMaintenance } from "@/lib/api/maintenance"
import type { Setting } from "@/lib/domain/setting"
import { HiOutlineExclamationCircle } from "react-icons/hi"

interface MaintenancePanelProps {
  settings: Setting[]
}

// Looks up a setting value by key.
function findValue(settings: Setting[], key: string, fallback = ""): string {
  const s = settings.find((x) => x.key === key)
  return s?.value ?? fallback
}

export function MaintenancePanel({ settings }: MaintenancePanelProps) {
  const qc = useQueryClient()
  const initialEnabled = findValue(settings, "maintenance_mode_enabled") === "true"
  const initialMessage = findValue(settings, "maintenance_mode_message")
  const initialSince = findValue(settings, "maintenance_mode_since")

  const [enabled, setEnabled] = useState(initialEnabled)
  const [message, setMessage] = useState(initialMessage)
  const [notify, setNotify] = useState(true)

  // Sync local state if upstream settings change
  useEffect(() => {
    setEnabled(initialEnabled)
    setMessage(initialMessage)
  }, [initialEnabled, initialMessage])

  const mut = useMutation({
    mutationFn: () => setGlobalMaintenance({ enabled, message, notify }),
    onSuccess: (r) => {
      if (!r.success) {
        toast.error(r.error || "Failed to update maintenance mode")
        return
      }
      toast.success(enabled ? "Maintenance mode enabled" : "Maintenance mode disabled")
      qc.invalidateQueries({ queryKey: ["settings"] })
      qc.invalidateQueries({ queryKey: ["maintenance"] })
    },
    onError: (e) => toast.error(`Failed: ${(e as Error).message}`),
  })

  return (
    <div className="space-y-6">
      <div className="flex items-start gap-3 rounded-lg border border-border/50 p-4">
        <HiOutlineExclamationCircle className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
        <div className="flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium">Global maintenance mode</p>
            {initialEnabled && <Badge variant="warning">Active</Badge>}
          </div>
          <p className="text-sm text-muted-foreground">
            When enabled, all non-admin users see a maintenance notice. Purchase, renewal, and config-regeneration actions are blocked on both the Telegram bot and the web subscription panel. Admin access is unaffected.
          </p>
          {initialSince && initialEnabled && (
            <p className="text-xs text-muted-foreground/80">
              Active since {new Date(initialSince).toLocaleString()}
            </p>
          )}
        </div>
      </div>

      <div className="space-y-4">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            className="h-4 w-4"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          <span className="text-sm font-medium">Enable maintenance mode</span>
        </label>

        <div className="space-y-2">
          <label className="block text-sm font-medium">Custom message (optional)</label>
          <textarea
            dir="auto"
            className="w-full rounded-md border border-border/50 bg-background px-3 py-2 text-sm"
            rows={3}
            placeholder="Leave empty to use the default translated notice."
            value={message}
            onChange={(e) => setMessage(e.target.value)}
          />
        </div>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            className="h-4 w-4"
            checked={notify}
            onChange={(e) => setNotify(e.target.checked)}
          />
          <span className="text-sm">
            Broadcast notice to active users via Telegram on enable
          </span>
        </label>
      </div>

      <Button onClick={() => mut.mutate()} disabled={mut.isPending}>
        {mut.isPending ? "Saving\u2026" : "Save"}
      </Button>
    </div>
  )
}
