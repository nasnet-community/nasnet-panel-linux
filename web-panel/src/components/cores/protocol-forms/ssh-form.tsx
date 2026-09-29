import { RefreshCw, KeyRound } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useConfirm } from "@/components/ui/confirm-dialog"
import type { SshSettings } from "@/lib/types/cores"
import { SelectSetting, SettingRow } from "./settings-fields"

export function SshForm({
    value,
    onChange,
}: {
    value: SshSettings
    onChange: (v: SshSettings) => void
}) {
    const confirm = useConfirm()
    const set = (patch: Partial<SshSettings>) => onChange({ ...value, ...patch })

    const regenerate = async () => {
        const ok = await confirm({
            title: "Regenerate host key",
            description:
                "This replaces the server's SSH host key. Clients that pinned the old fingerprint will refuse to connect until they re-trust it. The new key is generated when you save.",
            confirmLabel: "Regenerate",
            variant: "destructive",
        })
        if (ok) set({ host_key: "", host_key_fingerprint: "" })
    }

    const fingerprint = value.host_key_fingerprint || ""
    const willRegenerate = !value.host_key && !fingerprint

    return (
        <div className="space-y-4">
            <SelectSetting
                label="Device-limit strategy"
                value={value.strategy || "accept"}
                onChange={(v) => set({ strategy: v as SshSettings["strategy"] })}
                options={[
                    { value: "accept", label: "Accept — evict the oldest device" },
                    { value: "reject", label: "Reject — deny the new device" },
                ]}
                description="What happens when a user exceeds their device limit."
            />

            <SettingRow
                label="Host key fingerprint"
                description="Identifies this server to clients. Generated automatically on first save."
            >
                <div className="flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
                    <KeyRound className="size-4 shrink-0 text-muted-foreground" />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">
                        {willRegenerate ? (
                            <span className="text-amber-600 dark:text-amber-400">
                                Will be generated on save
                            </span>
                        ) : (
                            fingerprint || <span className="text-muted-foreground">—</span>
                        )}
                    </span>
                    {!willRegenerate && (
                        <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            className="h-7 shrink-0 text-muted-foreground hover:text-destructive"
                            onClick={regenerate}
                        >
                            <RefreshCw className="size-3.5" />
                            Regenerate
                        </Button>
                    )}
                </div>
            </SettingRow>
        </div>
    )
}
