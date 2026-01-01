import { useState } from "react"
import { Checkbox } from "@/components/ui/checkbox"

// WipeConfirmBody owns its own `checked` state so the checkbox re-renders
// even though ConfirmDialog holds a frozen description snapshot.

export interface WipeConfirmBodyProps {
  nodeName: string
  onAlsoRemoveChange: (v: boolean) => void
}

export function WipeConfirmBody({ nodeName, onAlsoRemoveChange }: WipeConfirmBodyProps) {
  const [checked, setChecked] = useState(false)

  const handleChange = (v: boolean) => {
    setChecked(v)          // local re-render — checkbox visually flips
    onAlsoRemoveChange(v)  // parent writes to its ref for onConfirm closure
  }

  return (
    <div className="space-y-4">
      {/* Risk banner */}
      <div className="rounded-md border border-amber-500/30 bg-amber-500/6 px-4 py-3">
        <p className="font-mono text-[11px] font-semibold uppercase tracking-widest text-amber-400">
          Partially reversible
        </p>
        <p className="mt-1 text-xs text-muted-foreground">
          Wipe removes xray configs, VPN keys, iptables rules, logs and
          temporary files from{" "}
          <span className="font-semibold text-foreground">{nodeName}</span>.
          The node record in the hub is preserved unless you opt-in below.
        </p>
      </div>

      {/* What gets wiped */}
      <div>
        <p className="mb-2 font-mono text-[10px] uppercase tracking-widest text-muted-foreground/60">
          Affected data
        </p>
        <ul className="grid grid-cols-2 gap-x-4 gap-y-1">
          {[
            "xray configs",
            "wireguard keys",
            "iptables rules",
            "bash history",
            "SSH host keys",
            "auth logs",
            "journals / var/log",
            "tmp files",
          ].map((item) => (
            <li
              key={item}
              className="flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground"
            >
              <span className="size-1 rounded-full bg-amber-500/70 shrink-0" />
              {item}
            </li>
          ))}
        </ul>
      </div>

      {/* Hub record checkbox */}
      <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border/50 bg-muted/30 px-3 py-2.5">
        <Checkbox
          checked={checked}
          onCheckedChange={(v) => handleChange(!!v)}
          className="mt-0.5"
        />
        <div className="space-y-0.5">
          <p className="text-sm font-medium leading-none">
            Also remove node from hub
          </p>
          <p className="text-xs text-muted-foreground">
            Deletes the hub database record — you will be redirected to the
            nodes list after wipe completes.
          </p>
        </div>
      </label>
    </div>
  )
}
