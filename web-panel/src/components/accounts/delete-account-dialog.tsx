import { useState } from "react"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { revokeSubscription } from "@/lib/admin-api"
import { useDeleteAccountMutation } from "@/lib/queries"
import { useAccountsStore } from "@/store/accounts-store"
import { toast } from "sonner"
import { HiExclamationTriangle } from "react-icons/hi2"

export function DeleteAccountDialog() {
    const { deleteDialog, closeDeleteDialog } = useAccountsStore()
    const { open, account } = deleteDialog
    const deleteMutation = useDeleteAccountMutation()
    const [deleteSub, setDeleteSub] = useState(true)
    const [isRevoking, setIsRevoking] = useState(false)

    if (!account) return null

    const hasSubscription = !!account.subscription
    const isLoading = deleteMutation.isPending || isRevoking

    const handleDelete = async () => {
        if (hasSubscription && deleteSub && account.subscription_id) {
            setIsRevoking(true)
            try {
                await revokeSubscription(account.subscription_id)
                toast.success(`Subscription #${account.subscription_id} revoked`)
            } catch {
                toast.error("Failed to revoke subscription")
            } finally {
                setIsRevoking(false)
            }
        }

        deleteMutation.mutate(account.id, {
            onSuccess: () => closeDeleteDialog(),
        })
    }

    return (
        <Dialog open={open} onOpenChange={(v) => !v && closeDeleteDialog()}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle className="flex items-center gap-2 text-destructive">
                        <HiExclamationTriangle className="w-5 h-5" />
                        Delete Account
                    </DialogTitle>
                    <DialogDescription>
                        Are you sure you want to delete the account <strong>{account.email}</strong>?
                        This action cannot be undone.
                    </DialogDescription>
                </DialogHeader>

                {hasSubscription && (
                    <div className="bg-yellow-50/50 dark:bg-yellow-900/10 border border-yellow-200 dark:border-yellow-900/50 p-4 rounded-md">
                        <div className="flex items-start space-x-3">
                            <Checkbox
                                id="delete_sub"
                                checked={deleteSub}
                                onCheckedChange={(c: boolean | "indeterminate") => setDeleteSub(!!c)}
                            />
                            <div className="grid gap-1.5 leading-none">
                                <Label
                                    htmlFor="delete_sub"
                                    className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                                >
                                    Also revoke Subscription #{account.subscription_id}
                                </Label>
                                <p className="text-sm text-muted-foreground">
                                    This account is part of a subscription. Unchecking this will delete the account record but leave the subscription active (potentially in an inconsistent state).
                                </p>
                            </div>
                        </div>
                    </div>
                )}

                <DialogFooter>
                    <Button variant="outline" onClick={closeDeleteDialog} disabled={isLoading}>
                        Cancel
                    </Button>
                    <Button variant="destructive" onClick={handleDelete} disabled={isLoading}>
                        {isLoading ? "Deleting..." : "Delete Account"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    )
}
