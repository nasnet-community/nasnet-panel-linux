import { useEffect, useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { Wrench, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import { setNodeMaintenance, setSubscriptionMaintenance } from "@/lib/api/maintenance"

type EntityType = "node" | "subscription"

interface EntityMaintenanceCardProps {
  type: EntityType
  id: number
  initialEnabled: boolean
  initialMessage: string
  initialSince?: string | null
  /** Tanstack Query key prefix to invalidate after a successful save. */
  invalidateKey: readonly unknown[]
  /** "card" (default) renders the full card; "compact" renders an inline admin row. */
  variant?: "card" | "compact"
}

export function EntityMaintenanceCard({
  type,
  id,
  initialEnabled,
  initialMessage,
  initialSince,
  invalidateKey,
  variant = "card",
}: EntityMaintenanceCardProps) {
  const qc = useQueryClient()
  const [enabled, setEnabled] = useState(initialEnabled)
  const [message, setMessage] = useState(initialMessage)

  useEffect(() => {
    setEnabled(initialEnabled)
    setMessage(initialMessage)
  }, [initialEnabled, initialMessage])

  const mut = useMutation({
    mutationFn: () => {
      const payload = { enabled, message }
      return type === "node"
        ? setNodeMaintenance(id, payload)
        : setSubscriptionMaintenance(id, payload)
    },
    onSuccess: (r) => {
      if (!r.success) {
        toast.error(r.error || "Failed to update maintenance")
        return
      }
      toast.success(enabled ? "Maintenance enabled" : "Maintenance cleared")
      qc.invalidateQueries({ queryKey: invalidateKey })
      qc.invalidateQueries({ queryKey: ["maintenance"] })
    },
    onError: (e) => toast.error(`Failed: ${(e as Error).message}`),
  })

  const label = type === "node" ? "node" : "subscription"
  const dirty = enabled !== initialEnabled || message !== initialMessage

  if (variant === "compact") {
    return (
      <div className="space-y-1.5">
        <div className="flex items-center justify-between">
          <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Maintenance
          </h3>
          {dirty && (
            <Button
              size="sm"
              className="h-6 text-xs px-2.5"
              onClick={() => mut.mutate()}
              disabled={mut.isPending}
            >
              {mut.isPending ? <Loader2 className="w-3 h-3 animate-spin" /> : "Save"}
            </Button>
          )}
        </div>

        <label
          className={cn(
            "flex items-center justify-between gap-3 rounded-md border px-3 py-2 cursor-pointer transition-colors",
            enabled
              ? "border-amber-500/40 bg-amber-500/5 hover:bg-amber-500/10 dark:border-amber-500/30 dark:bg-amber-950/20 dark:hover:bg-amber-950/30"
              : "border-border/50 hover:border-border",
          )}
        >
          <div className="flex items-center gap-2.5 min-w-0">
            <Wrench
              className={cn(
                "h-4 w-4 shrink-0",
                enabled ? "text-amber-500" : "text-muted-foreground/60",
              )}
            />
            <div className="min-w-0 leading-tight">
              <p className="text-sm">
                {enabled ? `${capitalize(label)} in maintenance` : `Put ${label} in maintenance`}
              </p>
              {initialEnabled && initialSince && (
                <p className="text-[10px] text-muted-foreground mt-0.5">
                  since {new Date(initialSince).toLocaleString()}
                </p>
              )}
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0">
            {initialEnabled && !dirty && (
              <Badge variant="warning" className="text-[10px] px-1.5 py-0">
                Active
              </Badge>
            )}
            <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={`Toggle ${label} maintenance`} />
          </div>
        </label>

        {enabled && (
          <textarea
            dir="auto"
            className="w-full rounded-md border border-border/50 bg-background px-2.5 py-1.5 text-xs placeholder:text-muted-foreground/60 focus:outline-none focus:border-border resize-none"
            rows={2}
            placeholder="Custom message (optional — leave empty for default notice)"
            value={message}
            onChange={(e) => setMessage(e.target.value)}
          />
        )}

        {!dirty && (
          <p className="text-[10px] text-muted-foreground">
            Blocks purchase, renewal and config regeneration for linked users. Others unaffected.
          </p>
        )}
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border/50 p-4 space-y-4">
      <div className="flex items-start gap-3">
        <Wrench className="h-5 w-5 text-amber-500 shrink-0 mt-0.5" />
        <div className="flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <p className="text-sm font-medium">Maintenance mode</p>
            {initialEnabled && <Badge variant="warning">Active</Badge>}
          </div>
          <p className="text-sm text-muted-foreground">
            When enabled, users tied to this {label} see a maintenance notice on their subscription panel and Telegram bot. Purchase/renewal/config-regeneration actions are blocked. Other users are unaffected.
          </p>
          {initialSince && initialEnabled && (
            <p className="text-xs text-muted-foreground/80">
              Active since {new Date(initialSince).toLocaleString()}
            </p>
          )}
        </div>
      </div>

      <label className="flex items-center gap-3 cursor-pointer">
        <input
          type="checkbox"
          className="h-4 w-4"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        <span className="text-sm font-medium">Put this {label} in maintenance</span>
      </label>

      <div className="space-y-2">
        <label className="block text-sm font-medium">Message (optional)</label>
        <textarea
          dir="auto"
          className="w-full rounded-md border border-border/50 bg-background px-3 py-2 text-sm"
          rows={3}
          placeholder="Leave empty to use the default translated notice."
          value={message}
          onChange={(e) => setMessage(e.target.value)}
        />
      </div>

      <Button onClick={() => mut.mutate()} disabled={mut.isPending}>
        {mut.isPending ? "Saving…" : "Save"}
      </Button>
    </div>
  )
}

function capitalize(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
