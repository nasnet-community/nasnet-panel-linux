import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Loader2, AlertTriangle, Info, CheckCircle2, Trash2 } from "lucide-react"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { useDatabaseCleanup, useCreateBackup } from "@/lib/queries"
import type { CleanupResult } from "@/lib/admin-api"

export function DangerZone({ disabled, asMenuItem = false }: { disabled?: boolean; asMenuItem?: boolean }) {
    const [confirmOpen, setConfirmOpen] = useState(false)
    const [resultOpen, setResultOpen] = useState(false)
    const [confirmText, setConfirmText] = useState("")
    const [cleanupResult, setCleanupResult] = useState<CleanupResult | null>(null)
    const [isRunning, setIsRunning] = useState(false)

    const cleanup = useDatabaseCleanup()
    const backup = useCreateBackup()

    const canConfirm = confirmText === "CLEANUP"

    const handleConfirm = async () => {
        setIsRunning(true)
        try {
            // Safety backup first
            await backup.mutateAsync()

            // Then cleanup
            const result = await cleanup.mutateAsync()
            setCleanupResult(result)
            setConfirmOpen(false)
            setConfirmText("")
            setResultOpen(true)
        } catch {
            // Errors handled by mutation onError callbacks
        } finally {
            setIsRunning(false)
        }
    }

    const resultRows: { label: string; key: keyof CleanupResult }[] = [
        { label: "Accounts removed from nodes", key: "accounts_removed" },
        { label: "Subscriptions deleted", key: "subscriptions_deleted" },
        { label: "Users deleted", key: "users_deleted" },
        { label: "Audit logs deleted", key: "audit_logs_deleted" },
        { label: "Notification logs deleted", key: "notification_logs_deleted" },
        { label: "Provisioning tasks deleted", key: "provisioning_tasks_deleted" },
        { label: "Daily usage records deleted", key: "user_daily_usage_deleted" },
        { label: "Node stats deleted", key: "node_stats_deleted" },
        { label: "Deployment tokens deleted", key: "used_tokens_deleted" },
        { label: "Conversation sessions deleted", key: "conversations_deleted" },
        { label: "Admin accounts reset", key: "admins_reset" },
    ]

    return (
        <>
            {asMenuItem ? (
                <DropdownMenuItem
                    onSelect={(e) => {
                        e.preventDefault()
                        setConfirmOpen(true)
                    }}
                    disabled={disabled}
                    className="text-destructive focus:text-destructive focus:bg-destructive/10"
                >
                    <Trash2 className="w-4 h-4 mr-2" />
                    Clean Database
                </DropdownMenuItem>
            ) : (
                <Button
                    variant="destructive"
                    onClick={() => setConfirmOpen(true)}
                    disabled={disabled}
                >
                    <Trash2 className="w-4 h-4 mr-2" />
                    Clean Database
                </Button>
            )}

            {/* Confirmation Dialog */}
            <Dialog open={confirmOpen} onOpenChange={(open) => {
                if (!isRunning) {
                    setConfirmOpen(open)
                    if (!open) setConfirmText("")
                }
            }}>
                <DialogContent className="sm:max-w-lg">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <AlertTriangle className="h-5 w-5 text-destructive" />
                            Clean Database
                        </DialogTitle>
                        <DialogDescription>
                            This action cannot be undone. Please read carefully before proceeding.
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-4">
                        <Alert variant="destructive">
                            <AlertTriangle className="h-4 w-4" />
                            <AlertTitle>This will permanently delete:</AlertTitle>
                            <AlertDescription>
                                All users (non-admin), subscriptions, accounts, payments, coupons, audit logs, notification logs, usage data, import batches, and node statistics.
                            </AlertDescription>
                        </Alert>

                        <Alert>
                            <Info className="h-4 w-4" />
                            <AlertTitle>Safety backup</AlertTitle>
                            <AlertDescription>
                                A safety backup will be created automatically before cleanup begins.
                            </AlertDescription>
                        </Alert>

                        <div className="rounded-md bg-muted p-3 text-sm text-muted-foreground">
                            <p className="font-medium mb-1">The following will be preserved:</p>
                            <ul className="list-disc list-inside space-y-0.5">
                                <li>Nodes, inbounds, and outbounds</li>
                                <li>Plans (inbound links will be cleared)</li>
                                <li>Settings and configuration</li>
                                <li>Admin accounts (balance reset to 0)</li>
                                <li>Agent certificates and SNIs</li>
                            </ul>
                        </div>

                        <div>
                            <label className="text-sm font-medium">
                                Type <span className="font-mono font-bold">CLEANUP</span> to confirm
                            </label>
                            <Input
                                value={confirmText}
                                onChange={(e) => setConfirmText(e.target.value)}
                                placeholder="CLEANUP"
                                className="mt-1"
                                disabled={isRunning}
                            />
                        </div>
                    </div>

                    <DialogFooter>
                        <Button
                            variant="outline"
                            onClick={() => { setConfirmOpen(false); setConfirmText("") }}
                            disabled={isRunning}
                        >
                            Cancel
                        </Button>
                        <Button
                            variant="destructive"
                            onClick={handleConfirm}
                            disabled={!canConfirm || isRunning}
                        >
                            {isRunning ? (
                                <>
                                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                                    Cleaning...
                                </>
                            ) : (
                                "Clean Database"
                            )}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            {/* Results Dialog */}
            <Dialog open={resultOpen} onOpenChange={setResultOpen}>
                <DialogContent className="sm:max-w-md">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <CheckCircle2 className="h-5 w-5 text-green-500" />
                            Cleanup Complete
                        </DialogTitle>
                        <DialogDescription>
                            The database has been cleaned successfully.
                        </DialogDescription>
                    </DialogHeader>

                    {cleanupResult && (
                        <div className="rounded-md border p-3 max-h-80 overflow-y-auto">
                            <table className="w-full text-sm">
                                <tbody>
                                    {resultRows.map(({ label, key }) => (
                                        <tr key={key} className="border-b last:border-0">
                                            <td className="py-1.5 text-muted-foreground">{label}</td>
                                            <td className="py-1.5 text-right font-mono font-medium">
                                                {cleanupResult[key]}
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )}

                    <DialogFooter>
                        <Button onClick={() => setResultOpen(false)}>
                            Done
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </>
    )
}
