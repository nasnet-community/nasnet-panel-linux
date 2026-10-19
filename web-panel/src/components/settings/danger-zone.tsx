import { useState } from "react"
import { Eraser, Loader2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useDatabaseCleanup, useCreateBackup } from "@/lib/queries"
import type { CleanupResult } from "@/lib/api/backup"
import { DangerCard, DangerRow } from "@/components/settings-page/danger-card"

const RESULT_ROWS: { label: string; key: keyof CleanupResult }[] = [
    { label: "Accounts removed from nodes", key: "accounts_removed" },
    { label: "Subscriptions", key: "subscriptions_deleted" },
    { label: "Users", key: "users_deleted" },
    { label: "Audit log entries", key: "audit_logs_deleted" },
    { label: "Notification log entries", key: "notification_logs_deleted" },
    { label: "Provisioning tasks", key: "provisioning_tasks_deleted" },
    { label: "Daily usage records", key: "user_daily_usage_deleted" },
    { label: "Node stats", key: "node_stats_deleted" },
    { label: "Deployment tokens", key: "used_tokens_deleted" },
    { label: "Bot conversations", key: "conversations_deleted" },
    { label: "Admins reset", key: "admins_reset" },
]

const WORD = "ERASE"

/** Erasing customer data takes a backup first. */
export function DangerZone() {
    const [open, setOpen] = useState(false)
    const [typed, setTyped] = useState("")
    const [running, setRunning] = useState(false)
    const [result, setResult] = useState<CleanupResult | null>(null)
    const backup = useCreateBackup()
    const cleanup = useDatabaseCleanup()

    const erase = async () => {
        setRunning(true)
        try {
            await backup.mutateAsync()
            setResult(await cleanup.mutateAsync())
            setOpen(false)
            setTyped("")
        } catch {
            // both mutations toast their own errors
        } finally {
            setRunning(false)
        }
    }

    return (
        <>
            <DangerCard note="These act on the panel's data and can't be taken back.">
                <DangerRow
                    icon={Eraser}
                    tone="danger"
                    title="Erase customer data"
                    text="Deletes every non-admin user, their subscriptions with their accounts and tunnel users on nodes, usage history and the audit log. Nodes, inbounds, settings and certificates stay. A backup is taken first."
                    button="Erase…"
                    onClick={() => setOpen(true)}
                />
            </DangerCard>

            <Dialog open={open} onOpenChange={o => { if (!running) { setOpen(o); if (!o) setTyped("") } }}>
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader>
                        <DialogTitle className="text-red-500">Erase customer data?</DialogTitle>
                        <DialogDescription>This can't be undone, except by restoring the backup taken first.</DialogDescription>
                    </DialogHeader>
                    <div className="grid gap-4 text-sm sm:grid-cols-2">
                        <div>
                            <p className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Deleted</p>
                            <ul className="list-disc space-y-0.5 pl-4 text-muted-foreground">
                                <li>Users, except admins</li>
                                <li>Subscriptions, their accounts and tunnel users on nodes</li>
                                <li>Usage history, node stats</li>
                                <li>Audit and notification logs</li>
                            </ul>
                        </div>
                        <div>
                            <p className="mb-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">Kept</p>
                            <ul className="list-disc space-y-0.5 pl-4 text-muted-foreground">
                                <li>Nodes, inbounds and outbounds</li>
                                <li>Settings and certificates</li>
                                <li>Admins</li>
                            </ul>
                        </div>
                    </div>
                    <div className="space-y-1.5">
                        <Label htmlFor="erase-confirm">Type <span className="font-mono font-semibold">{WORD}</span> to confirm</Label>
                        <Input id="erase-confirm" autoComplete="off" value={typed} onChange={e => setTyped(e.target.value)} placeholder={WORD} className="font-mono" disabled={running} />
                    </div>
                    <DialogFooter>
                        <Button variant="ghost" onClick={() => { setOpen(false); setTyped("") }} disabled={running}>Cancel</Button>
                        <Button variant="destructive" onClick={erase} disabled={typed !== WORD || running}>
                            {running && <Loader2 className="mr-1.5 size-4 animate-spin" />}
                            {running ? (backup.isPending ? "Backing up…" : "Erasing…") : "Back up and erase"}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={!!result} onOpenChange={o => { if (!o) setResult(null) }}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle>Customer data erased</DialogTitle>
                        <DialogDescription>The backup taken first is on the Backups page.</DialogDescription>
                    </DialogHeader>
                    {result && (
                        <dl className="divide-y divide-border/50 text-sm">
                            {RESULT_ROWS.filter(r => (result[r.key] as number) > 0).map(r => (
                                <div key={r.key} className="flex justify-between py-1.5">
                                    <dt className="text-muted-foreground">{r.label}</dt>
                                    <dd className="font-mono">{(result[r.key] as number).toLocaleString()}</dd>
                                </div>
                            ))}
                        </dl>
                    )}
                    <DialogFooter><Button onClick={() => setResult(null)}>Done</Button></DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}
