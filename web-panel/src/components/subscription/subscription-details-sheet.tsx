import { useState, useCallback } from "react"
import { FaTelegram } from "react-icons/fa"
import { Skeleton } from "@/components/ui/skeleton"
import { Separator } from "@/components/ui/separator"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import { UserSearchSelect } from "@/components/ui/user-search-select"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { BulkManageInboundsDialog } from "@/components/subscription/bulk-manage-inbounds-dialog"
import { Loader2 } from "lucide-react"
import {
    useExtendSubscription,
    usePauseSubscription,
    useResumeSubscription,
    useRevokeSubscription,
    useSetDataLimit,
    useSetBandwidthLimit,
    useSetMaxDevices,
    useSetExpiry,
    useResetData,
    useRenameSubscription,
    useRegenerateSubscriptionKey,
    useSubscription,
    useAssignSubscriptionUser,
    useDeleteSubscription,
    useSetPanelPassword,
    useSubscriptionIPs,
    useSetSubscriptionUUID,
} from "@/lib/queries"
import { useAccountsBySubscription } from "@/lib/queries/use-accounts"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useSubscriptionDerived } from "@/lib/subscription-derived"
import { SubscriptionStatusStrip, OverviewSection } from "./sections/overview-section"
import { IdentitySection } from "./sections/identity-section"
import { LimitsSection } from "./sections/limits-section"
import { AccessSection } from "./sections/access-section"
import { DangerSection } from "./sections/danger-section"
import { formatBytes } from "@/lib/utils"
import { EntityMaintenanceCard } from "@/components/maintenance/entity-maintenance-card"
import { AccessHistoryDialog } from "./access-history/access-history-dialog"
import { Button } from "@/components/ui/button"
import { History } from "lucide-react"

export function SubscriptionDetailsSheet() {
    const { detailsSheet, closeDetailsSheet } = useSubscriptionsStore()
    const { subscription: initialSubscription, open } = detailsSheet
    const confirm = useConfirm()

    const { data: fetchedSubscription, isLoading: subscriptionLoading } = useSubscription(
        initialSubscription?.id || 0,
    )
    const subscription = fetchedSubscription || initialSubscription
    const { data: accountsData, isLoading: accountsLoading } = useAccountsBySubscription(subscription?.id)
    const accounts = accountsData || []
    const { data: ipsData = [], isLoading: ipsLoading } = useSubscriptionIPs(subscription?.id)
    const derived = useSubscriptionDerived(subscription)

    const extendMutation = useExtendSubscription()
    const pauseMutation = usePauseSubscription()
    const resumeMutation = useResumeSubscription()
    const revokeMutation = useRevokeSubscription()
    const setLimitMutation = useSetDataLimit()
    const setBandwidthMutation = useSetBandwidthLimit()
    const setMaxDevicesMutation = useSetMaxDevices()
    const setExpiryMutation = useSetExpiry()
    const resetDataMutation = useResetData()
    const renameMutation = useRenameSubscription()
    const regenerateKeyMutation = useRegenerateSubscriptionKey()
    const assignUserMutation = useAssignSubscriptionUser()
    const deleteMutation = useDeleteSubscription()
    const panelPasswordMutation = useSetPanelPassword()
    const setUuidMutation = useSetSubscriptionUUID()

    const [assignUserOpen, setAssignUserOpen] = useState(false)
    const [assignInboundOpen, setAssignInboundOpen] = useState(false)
    const [accessHistoryOpen, setAccessHistoryOpen] = useState(false)

    const runMutationAsPromise = useCallback(
        <TArgs, TData>(
            mutation: {
                mutateAsync: (args: TArgs) => Promise<TData>
            },
            args: TArgs,
        ): Promise<void> =>
            mutation.mutateAsync(args).then(() => undefined),
        [],
    )

    // --- Action handlers (wrapped with confirmations where destructive) ---

    const handleRename = async (label: string) => {
        if (!subscription) return
        await runMutationAsPromise(renameMutation, { id: subscription.id, label })
    }

    const handleRegenerateKey = async (key: string) => {
        if (!subscription) return
        await runMutationAsPromise(regenerateKeyMutation, { id: subscription.id, key })
    }

    const handleSetUUID = async (uuid: string) => {
        if (!subscription) return
        const ok = await confirm({
            title: "Apply UUID to all accounts?",
            description: (
                <>
                    This will overwrite the UUID on every account under this subscription and
                    re-sync with Xray nodes. Users currently connected with the old UUID will
                    lose access until they reload their subscription.
                </>
            ),
            confirmLabel: "Apply",
            variant: "warning",
        })
        if (!ok) return
        await runMutationAsPromise(setUuidMutation, { id: subscription.id, uuid })
    }

    const handleExtend = async (days: number) => {
        if (!subscription) return
        const ok = await confirm({
            title: "Extend duration",
            description: (
                <>
                    Add <strong className="text-foreground">{days} days</strong> to this
                    subscription?
                </>
            ),
            confirmLabel: "Extend",
        })
        if (!ok) return
        extendMutation.mutate({ id: subscription.id, days })
    }

    const handleSetExpiry = (endDateISO: string | null, unlimited?: boolean) => {
        if (!subscription) return
        setExpiryMutation.mutate({ id: subscription.id, expiryDate: endDateISO, unlimited })
    }

    const handleSetDataLimit = (limitGb: number | null) => {
        if (!subscription) return
        setLimitMutation.mutate({ id: subscription.id, limitGb })
    }

    const handleSetBandwidth = (limitMbps: number | null) => {
        if (!subscription) return
        setBandwidthMutation.mutate({ id: subscription.id, limitMbps })
    }

    const handleSetMaxDevices = (maxDevices: number) => {
        if (!subscription) return
        setMaxDevicesMutation.mutate({ id: subscription.id, maxDevices })
    }

    const handlePauseResume = () => {
        if (!subscription || !derived) return
        if (derived.isActive) pauseMutation.mutate(subscription.id)
        else resumeMutation.mutate(subscription.id)
    }

    const handleReset = async () => {
        if (!subscription) return
        const ok = await confirm({
            title: "Reset data usage",
            description: "This resets the usage counter to 0. Limits remain unchanged.",
            confirmLabel: "Reset",
        })
        if (!ok) return
        resetDataMutation.mutate(subscription.id)
    }

    const handleRevoke = async () => {
        if (!subscription) return
        const ok = await confirm({
            title: "Revoke subscription",
            description:
                "The user will lose access immediately. This pauses the subscription and disables all accounts on the Xray nodes.",
            confirmLabel: "Yes, revoke",
            variant: "warning",
        })
        if (!ok) return
        revokeMutation.mutate(subscription.id, {
            onSuccess: () => closeDetailsSheet(),
        })
    }

    const handleDelete = async () => {
        if (!subscription) return
        const confirmWord = subscription.label?.trim() || `sub-${subscription.id}`
        const ok = await confirm({
            title: "Delete permanently",
            description: (
                <>
                    This removes the subscription <strong className="font-mono text-foreground">#{subscription.id}</strong>{" "}
                    and all associated accounts from every Xray node and the database. <strong>Cannot be undone.</strong>
                </>
            ),
            confirmLabel: "Delete permanently",
            variant: "destructive",
            typeToConfirm: confirmWord,
        })
        if (!ok) return
        deleteMutation.mutate(subscription.id, {
            onSuccess: () => closeDetailsSheet(),
        })
    }

    const handleSetPanelPassword = (mode: "default" | "custom" | "disabled", password?: string) => {
        if (!subscription) return
        panelPasswordMutation.mutate({ id: subscription.id, mode, password })
    }

    const handleUnlinkUser = async () => {
        if (!subscription) return
        const ok = await confirm({
            title: "Unlink user",
            description: "The subscription will become a standalone manual subscription.",
            confirmLabel: "Yes, unlink",
            variant: "warning",
        })
        if (!ok) return
        assignUserMutation.mutate({ subId: subscription.id, userId: null })
    }

    const planName = !subscription ? "" : (subscription.label || `Subscription #${subscription.id}`)
    const username = !subscription
        ? ""
        : subscription.user_id === null || subscription.user_id === 0
            ? "Unassigned"
            : subscription.user?.username || `User #${subscription.user_id}`

    const headerTitle = subscription
        ? `${planName}${subscription.label && subscription.label !== planName ? " · " + subscription.label : ""}`
        : "Subscription"

    const showSkeleton = !subscription && subscriptionLoading

    return (
        <>
            <Dialog open={open} onOpenChange={(v) => !v && closeDetailsSheet()}>
                <DialogContent
                    className="max-w-xl p-0 gap-0 max-h-[95vh] overflow-hidden flex flex-col"
                    onInteractOutside={(e) => e.preventDefault()}
                >
                    {/* Header */}
                    {subscription ? (
                        <DialogHeader className="px-4 pt-4 pb-2 border-b bg-background shrink-0 space-y-1">
                            <DialogTitle className="text-sm font-semibold leading-tight truncate pr-10">
                                {headerTitle}
                            </DialogTitle>
                            <DialogDescription className="sr-only">
                                Details and management actions for subscription #{subscription.id}
                            </DialogDescription>
                            <SubscriptionStatusStrip subscription={subscription} />
                            <div className="flex items-center gap-1.5 text-sm text-muted-foreground flex-wrap">
                                <span className="font-medium text-foreground">{username}</span>
                                {subscription.user?.telegram_id && subscription.user.telegram_id > 0 && (
                                    <>
                                        <span>·</span>
                                        <FaTelegram className="w-3 h-3 text-[#229ED9]" />
                                        <span className="font-mono text-xs">{subscription.user.telegram_id}</span>
                                    </>
                                )}
                            </div>
                        </DialogHeader>
                    ) : (
                        <DialogHeader className="px-4 pt-4 pb-2 border-b bg-background shrink-0 space-y-1">
                            <DialogTitle className="sr-only">Loading subscription</DialogTitle>
                            <DialogDescription className="sr-only">
                                Loading subscription details
                            </DialogDescription>
                            <Skeleton className="h-4 w-48" />
                            <Skeleton className="h-4 w-32" />
                        </DialogHeader>
                    )}

                    {/* Body */}
                    <div className="flex-1 min-h-0 overflow-y-auto">
                        <div className="px-4 py-3 space-y-4">
                            {showSkeleton ? (
                                <SheetSkeleton />
                            ) : subscription && derived ? (
                                <>
                                    <OverviewSection subscription={subscription} derived={derived} />
                                    <Separator />
                                    <IdentitySection
                                        subscription={subscription}
                                        accounts={accounts}
                                        accountsLoading={accountsLoading}
                                        onRename={handleRename}
                                        onRegenerateKey={handleRegenerateKey}
                                        onSetUUID={handleSetUUID}
                                    />
                                    <Separator />
                                    <LimitsSection
                                        subscription={subscription}
                                        derived={derived}
                                        onExtend={handleExtend}
                                        onSetExpiry={handleSetExpiry}
                                        onSetDataLimit={handleSetDataLimit}
                                        onSetBandwidth={handleSetBandwidth}
                                        onSetMaxDevices={handleSetMaxDevices}
                                        mutationsPending={{
                                            extend: extendMutation.isPending,
                                            setExpiry: setExpiryMutation.isPending,
                                            setDataLimit: setLimitMutation.isPending,
                                            setBandwidth: setBandwidthMutation.isPending,
                                            setMaxDevices: setMaxDevicesMutation.isPending,
                                        }}
                                    />
                                    <Separator />
                                    <AccessSection
                                        subscription={subscription}
                                        accounts={accounts}
                                        accountsLoading={accountsLoading}
                                        ips={ipsData}
                                        ipsLoading={ipsLoading}
                                        onAssignInbound={() => setAssignInboundOpen(true)}
                                        onAssignUser={() => setAssignUserOpen(true)}
                                        onUnlinkUser={handleUnlinkUser}
                                    />
                                    <div className="pt-1">
                                        <Button
                                            variant="outline"
                                            size="sm"
                                            className="h-8 gap-1.5 text-xs"
                                            onClick={() => setAccessHistoryOpen(true)}
                                        >
                                            <History className="w-3.5 h-3.5" />
                                            View Access History
                                        </Button>
                                    </div>
                                    <Separator />
                                    <EntityMaintenanceCard
                                        type="subscription"
                                        id={subscription.id}
                                        initialEnabled={!!subscription.maintenance_mode}
                                        initialMessage={subscription.maintenance_message || ""}
                                        initialSince={subscription.maintenance_since}
                                        invalidateKey={["subscriptions"] as const}
                                        variant="compact"
                                    />
                                    <Separator />
                                    <DangerSection
                                        subscription={subscription}
                                        derived={derived}
                                        onPauseResume={handlePauseResume}
                                        onReset={handleReset}
                                        onRevoke={handleRevoke}
                                        onDelete={handleDelete}
                                        onSetPanelPassword={handleSetPanelPassword}
                                        mutationsPending={{
                                            pauseResume: pauseMutation.isPending || resumeMutation.isPending,
                                            reset: resetDataMutation.isPending,
                                            revoke: revokeMutation.isPending,
                                            delete: deleteMutation.isPending,
                                            panelPassword: panelPasswordMutation.isPending,
                                        }}
                                    />
                                </>
                            ) : null}
                        </div>
                    </div>
                </DialogContent>
            </Dialog>

            {/* Assign User form dialog (stays inline — it's a form, not a confirm) */}
            <Dialog open={assignUserOpen} onOpenChange={setAssignUserOpen}>
                <DialogContent>
                    <DialogHeader>
                        <DialogTitle>
                            {subscription?.user_id && subscription.user_id > 0 ? "Change user" : "Assign user"}
                        </DialogTitle>
                        <DialogDescription>Search for a user to link to this subscription.</DialogDescription>
                    </DialogHeader>
                    <UserSearchSelect
                        value={null}
                        onChange={(user) => {
                            if (user && subscription) {
                                assignUserMutation.mutate(
                                    { subId: subscription.id, userId: user.id },
                                    { onSuccess: () => setAssignUserOpen(false) },
                                )
                            }
                        }}
                        placeholder="Search users…"
                    />
                    {assignUserMutation.isPending && (
                        <div className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
                            <Loader2 className="w-4 h-4 animate-spin" />
                            Assigning…
                        </div>
                    )}
                </DialogContent>
            </Dialog>

            <BulkManageInboundsDialog
                open={assignInboundOpen}
                onOpenChange={setAssignInboundOpen}
                selectedSubscriptionIds={subscription?.id ? [subscription.id] : []}
            />

            <AccessHistoryDialog
                subId={subscription?.id}
                open={accessHistoryOpen}
                onOpenChange={setAccessHistoryOpen}
            />
        </>
    )
}

function SheetSkeleton() {
    return (
        <div className="space-y-4">
            <div className="rounded-lg border p-3 space-y-3">
                <Skeleton className="h-2 w-full" />
                <div className="space-y-1.5">
                    <Skeleton className="h-4 w-24" />
                    <Skeleton className="h-4 w-32" />
                    <Skeleton className="h-4 w-28" />
                </div>
            </div>
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-24 w-full" />
            <Skeleton className="h-16 w-full" />
        </div>
    )
}
