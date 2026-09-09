import { useState } from "react"
import { MoreHorizontal, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { startXrayProcess, stopXrayProcess, restartXrayProcess, restartNodeSSH, clearNodeSSHLogs } from "@/lib/api/nodes"
import { toast } from "sonner"
import type { Node } from "@/lib/types"
import type { NodeSettingsForm } from "@/hooks/use-node-settings-form"

export function NodeSettingsActions({ node, settingsForm, onRefresh }: {
    node: Node; settingsForm: NodeSettingsForm; onRefresh: () => void
}) {
    const [busy, setBusy] = useState(false)
    const confirm = useConfirm()
    const disabled = busy || settingsForm.isDirty || settingsForm.isSaving
    const run = async (label: string, action: () => Promise<{ success: boolean; error?: string }>, confirmation?: string, ssh = false) => {
        if (confirmation && !await confirm({ title: `${label}?`, description: confirmation, confirmLabel: label, variant: "warning" })) return
        setBusy(true)
        try {
            const response = await action()
            if (!response.success) throw new Error(response.error || `${label} failed`)
            toast.success(`${label} completed`)
            onRefresh()
            if (ssh) await settingsForm.fetchSSHStatus()
        } catch (error) {
            toast.error(error instanceof Error ? error.message : `${label} failed`)
        } finally { setBusy(false) }
    }
    return <DropdownMenu>
        <DropdownMenuTrigger asChild><Button type="button" variant="outline" size="icon" className="size-8" aria-label="Settings actions" disabled={busy}>{busy ? <Loader2 className="size-4 animate-spin" /> : <MoreHorizontal className="size-4" />}</Button></DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-60">
            <DropdownMenuLabel>Xray service</DropdownMenuLabel>
            <DropdownMenuItem disabled={disabled || !node.is_online} onSelect={() => void run("Start Xray", () => startXrayProcess(node.id))}>Start Xray</DropdownMenuItem>
            <DropdownMenuItem disabled={disabled || !node.is_online} onSelect={() => void run("Restart Xray", () => restartXrayProcess(node.id), "Active connections may briefly reconnect.")}>Restart Xray</DropdownMenuItem>
            <DropdownMenuItem disabled={disabled || !node.is_online} onSelect={() => void run("Stop Xray", () => stopXrayProcess(node.id), "This stops proxy traffic on this server until Xray is started again.")}>Stop Xray</DropdownMenuItem>
            {!node.is_stealth && <>
                <DropdownMenuSeparator /><DropdownMenuLabel>SSH service</DropdownMenuLabel>
                <DropdownMenuItem disabled={disabled || !node.is_online} onSelect={() => void run("Restart SSH", () => restartNodeSSH(node.id), "Active SSH connections may drop.", true)}>Restart SSH</DropdownMenuItem>
                <DropdownMenuItem disabled={disabled || !node.is_online} onSelect={() => void run("Clear SSH logs", () => clearNodeSSHLogs(node.id), "Existing SSH log entries will be permanently removed.")}>Clear SSH logs</DropdownMenuItem>
            </>}
            {settingsForm.isDirty && <p className="px-2 py-2 text-xs text-muted-foreground">Save or discard edits before running service actions.</p>}
        </DropdownMenuContent>
    </DropdownMenu>
}
