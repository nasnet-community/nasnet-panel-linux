import { useRef, useState } from "react"
import { useNavigate } from "react-router"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { NukeDialog } from "@/components/node/nuke-dialog"
import { WipeConfirmBody } from "@/components/node/wipe-confirm-body"
import { wipeNode } from "@/lib/api/nuke"
import {
  AlertTriangleIcon,
  ZapIcon,
  Trash2Icon,
  ShieldOffIcon,
  MinusCircleIcon,
} from "lucide-react"

// ─────────────────────────────────────────────────────────────────────────────

const PLANNED_PHASES = [
  "stop_xray",
  "wipe_xray",
  "wipe_wireguard",
  "wipe_xui",
  "flush_iptables",
  "clear_bash_history",
  "shred_ssh_host_keys",
  "wipe_known_hosts_and_authkeys",
  "wipe_auth_logs",
  "wipe_journals_and_var_log",
  "wipe_tmp",
  "disable_audit",
  "disable_coredumps",
  "scrub_swap",
  "drop_caches",
  "shred_root",
  "shred_tls_certs",
  "wipe_agent_state",
] as const

// ─────────────────────────────────────────────────────────────────────────────
// Props
// ─────────────────────────────────────────────────────────────────────────────

export interface DangerZoneProps {
  nodeId: number
  nodeName: string
  onDelete?: () => void | Promise<void>
  deleteLabel?: string
}

// ─────────────────────────────────────────────────────────────────────────────
// DangerZone
// ─────────────────────────────────────────────────────────────────────────────

export function DangerZone({ nodeId, nodeName, onDelete, deleteLabel }: DangerZoneProps) {
  const navigate = useNavigate()
  const confirm = useConfirm()

  const [nukeOpen, setNukeOpen] = useState(false)

  // The ref is read by the onConfirm closure to get the latest checkbox value.
  // WipeConfirmBody owns the visual state; it writes back here via onAlsoRemoveChange.
  const alsoRemoveHubRecordRef = useRef(false)

  const updateAlsoRemoveHubRecord = (v: boolean) => {
    alsoRemoveHubRecordRef.current = v
  }

  // ── Wipe handler ────────────────────────────────────────────────────────────
  async function handleWipe() {
    // Reset ref before opening (WipeConfirmBody resets its own visual state on mount)
    alsoRemoveHubRecordRef.current = false

    const confirmed = await confirm({
      variant: "warning",
      title: "Wipe node data",
      typeToConfirm: nodeName,
      confirmLabel: "Wipe node",
      cancelLabel: "Cancel",
      description: (
        <WipeConfirmBody
          nodeName={nodeName}
          onAlsoRemoveChange={updateAlsoRemoveHubRecord}
        />
      ),
      onConfirm: async () => {
        const alsoRemove = alsoRemoveHubRecordRef.current
        const result = await wipeNode(nodeId, {
          alsoRemoveHubRecord: alsoRemove,
        })
        if (result.error) {
          toast.error(`Wipe error: ${result.error}`, {
            duration: 8000,
            action: {
              label: "Retry",
              onClick: () => handleWipe(),
            },
          })
          throw new Error(result.error)
        }
        if (alsoRemove) {
          toast.success(`${nodeName} wiped and removed from hub`)
          navigate("/nodes")
        } else {
          toast.success(`${nodeName} wiped successfully`)
        }
      },
    })

    // User cancelled — nothing to do
    void confirmed
  }

  // ── Render ──────────────────────────────────────────────────────────────────
  return (
    <>
      {/* ── Card shell ──────────────────────────────────────────────────── */}
      <div className="rounded-lg border border-red-500/25 bg-card overflow-hidden">
        {/* Header */}
        <div className="flex items-center gap-2.5 border-b border-red-500/15 bg-red-500/4 px-5 py-3.5">
          <AlertTriangleIcon className="size-4 text-red-500/80 shrink-0" />
          <span className="font-mono text-sm font-semibold tracking-tight text-red-500/90">
            Danger Zone
          </span>
          <span className="ml-auto font-mono text-[10px] uppercase tracking-widest text-red-500/40">
            Irreversible operations
          </span>
        </div>

        <div className="divide-y divide-border/40">
          {/* ── Row 0: Delete from hub (least destructive) ──────────────── */}
          {onDelete && (
            <div className="flex items-center justify-between gap-6 px-5 py-4">
              <div className="flex items-start gap-3 min-w-0">
                <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md border border-muted-foreground/25 bg-muted/40">
                  <MinusCircleIcon className="size-4 text-muted-foreground" />
                </div>
                <div className="space-y-0.5 min-w-0">
                  <p className="text-sm font-semibold leading-none text-foreground">
                    {deleteLabel || "Delete from hub"}
                  </p>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    Removes the hub record for this node. The node OS, agent
                    and any running services are not touched. Re-add the node
                    later to restore management.
                  </p>
                  <p className="font-mono text-[10px] uppercase tracking-wide text-muted-foreground/60">
                    Reversible · hub-only · no node side effects
                  </p>
                </div>
              </div>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void onDelete()}
                className="shrink-0 border-muted-foreground/30 bg-muted/30 font-mono text-xs text-muted-foreground hover:border-muted-foreground/50 hover:bg-muted/60 hover:text-foreground"
              >
                <Trash2Icon className="mr-1.5 size-3" />
                {deleteLabel || "Delete"}
              </Button>
            </div>
          )}

          {/* ── Row 1: Wipe ─────────────────────────────────────────────── */}
          <div className="flex items-center justify-between gap-6 px-5 py-4">
            <div className="flex items-start gap-3 min-w-0">
              <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md border border-amber-500/30 bg-amber-500/8">
                <ShieldOffIcon className="size-4 text-amber-500" />
              </div>
              <div className="space-y-0.5 min-w-0">
                <p className="text-sm font-semibold leading-none text-foreground">
                  Wipe node
                </p>
                <p className="text-xs text-muted-foreground leading-relaxed">
                  Remove proxy configs, keys, logs and temporary data. The
                  node OS and agent stay intact. Optionally removes the hub
                  record.
                </p>
                <p className="font-mono text-[10px] uppercase tracking-wide text-amber-500/70">
                  Partially reversible · configs are lost
                </p>
              </div>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={handleWipe}
              className="shrink-0 border-amber-500/40 bg-amber-500/6 font-mono text-xs text-amber-500 hover:border-amber-500/60 hover:bg-amber-500/15 hover:text-amber-400"
            >
              <Trash2Icon className="mr-1.5 size-3" />
              Wipe…
            </Button>
          </div>

          {/* ── Row 2: Nuke ─────────────────────────────────────────────── */}
          <div className="flex items-center justify-between gap-6 bg-red-500/2 px-5 py-4">
            <div className="flex items-start gap-3 min-w-0">
              <div className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md border border-red-500/30 bg-red-500/8">
                <ZapIcon className="size-4 text-red-500" />
              </div>
              <div className="space-y-0.5 min-w-0">
                <p className="text-sm font-semibold leading-none text-foreground">
                  Nuke node
                </p>
                <p className="text-xs text-muted-foreground leading-relaxed">
                  Full {PLANNED_PHASES.length}-phase deep wipe: shreds SSH
                  keys, swap, TLS certs, journals and agent state. Streams
                  live progress. Cannot be undone.
                </p>
                <p className="font-mono text-[10px] uppercase tracking-wide text-red-500/70">
                  Catastrophic · permanent · no recovery
                </p>
              </div>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={() => setNukeOpen(true)}
              className="shrink-0 border-red-500/40 bg-red-500/6 font-mono text-xs text-red-500 hover:border-red-500/60 hover:bg-red-500/15 hover:text-red-400"
            >
              <ZapIcon className="mr-1.5 size-3" />
              Nuke…
            </Button>
          </div>
        </div>
      </div>

      {/* ── NukeDialog (mounted outside card to avoid z-index clipping) ── */}
      <NukeDialog
        open={nukeOpen}
        onOpenChange={setNukeOpen}
        nodeId={nodeId}
        nodeName={nodeName}
        plannedPhases={PLANNED_PHASES as unknown as string[]}
      />
    </>
  )
}
