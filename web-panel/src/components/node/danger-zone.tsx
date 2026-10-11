import { useRef, useState } from "react"
import { useNavigate } from "react-router"
import { toast } from "@/lib/toast"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { NukeDialog } from "@/components/node/nuke-dialog"
import { WipeConfirmBody } from "@/components/node/wipe-confirm-body"
import { wipeNode } from "@/lib/api/nuke"
import { Eraser, MinusCircle, Zap } from "lucide-react"
import { DangerCard, DangerRow } from "@/components/settings-page/danger-card"

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
  /** False for the built-in node: its record can't be removed. */
  canRemoveRecord?: boolean
  /** wipe and nuke run through the agent */
  online?: boolean
  /** the card's subtitle */
  note?: string
}

// ─────────────────────────────────────────────────────────────────────────────
// DangerZone
// ─────────────────────────────────────────────────────────────────────────────

export function DangerZone({ nodeId, nodeName, onDelete, deleteLabel, canRemoveRecord = true, online = true, note }: DangerZoneProps) {
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
          canRemoveRecord={canRemoveRecord}
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
          toast.success(`${nodeName} wiped and removed from the panel`)
          navigate("/nodes")
        } else {
          toast.success(`${nodeName} wiped successfully`)
        }
      },
    })

    // User cancelled — nothing to do
    void confirmed
  }

  const local = !canRemoveRecord
  const offline = online ? undefined : "Needs the node online"

  return (
    <>
      <DangerCard note={note}>
          {onDelete && (
            <DangerRow
              icon={MinusCircle} tone="muted" title={deleteLabel || "Delete node"}
              text="Removes it from the panel and uninstalls the agent and Xray. Force deletion also removes linked accounts and configuration."
              button={`${deleteLabel?.startsWith("Force") ? "Force delete" : "Delete"}…`} onClick={() => void onDelete()}
            />
          )}
          <DangerRow
            icon={Eraser} tone="warn" title="Wipe node" why={offline} disabled={!online}
            text={local
              ? "Flushes this router's firewall and removes WireGuard, x-ui and Xray, then the panel itself."
              : `Removes proxy configs, keys, logs and temporary files. The system and the agent stay.${canRemoveRecord ? " You can also remove it from the panel." : ""}`}
            button="Wipe…" onClick={() => void handleWipe()}
          />
          <DangerRow
            icon={Zap} tone="danger" title="Nuke node" why={offline} disabled={!online}
            text={local
              ? `Deep-wipes this router in ${PLANNED_PHASES.length} steps, including SSH keys, logs and swap, then removes the panel itself. It can't be undone.`
              : `Deep-wipes the server in ${PLANNED_PHASES.length} steps: SSH keys, swap, certificates, journals and the agent. It can't be undone.`}
            button="Nuke…" onClick={() => setNukeOpen(true)}
          />
      </DangerCard>

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

