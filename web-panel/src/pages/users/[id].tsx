import { useState } from "react"
import { useParams, useNavigate, useSearchParams, useLocation } from "react-router"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    TooltipProvider,
} from "@/components/ui/tooltip"
import {
    HiOutlineArrowLeft,
} from "react-icons/hi"
import type { Subscription } from "@/lib/types"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { AutoRefreshControl } from "@/components/ui/auto-refresh-control"
import { SubscriptionDetailsSheet } from "@/components/subscription/subscription-details-sheet"
import { DataLimitDialog } from "@/components/subscription/data-limit-dialog"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useQueryClient } from "@tanstack/react-query"
import { useIsMobile } from "@/hooks/use-is-mobile"
import {
    useUserDetails,
    useUserSubscriptions,
    useBanUser,
    useToggleAdmin,
    useExtendSubscription,
    usePauseSubscription,
    useResumeSubscription,
    useRevokeSubscription,
    useResetData,
} from "@/lib/queries"

import { Badge } from "@/components/ui/badge"
import { SummaryStrip, SummaryStripSkeleton } from "@/components/user/summary-strip"
import { AlertBanner, useUserAlerts } from "@/components/user/alert-banner"
import { ProfileTab } from "@/components/user/profile-tab"
import { CollapsibleSummary } from "@/components/user/collapsible-summary"
import { UserSubscriptionsTab } from "@/components/user/user-subscriptions-tab"
import { UsageChart } from "@/components/user/usage-chart"
import { UsagePatternHeatmap } from "@/components/user/usage-pattern-heatmap"
import { ActivityFeed } from "@/components/user/activity-feed"
import { PerSubscriptionUsage } from "@/components/user/per-subscription-usage"

export default function UserDetailPage() {
    const params = useParams()
    const navigate = useNavigate()
    const userId = Number(params.id)
    const confirmDialog = useConfirm()
    const queryClient = useQueryClient()
    const { openDetailsSheet, openCreateManualDialog } = useSubscriptionsStore()
    const isMobile = useIsMobile()
    const [searchParams] = useSearchParams()
    const { pathname } = useLocation()
    const activeTab = searchParams.get("tab") || "subscriptions"

    const handleTabChange = (value: string) => {
        const params = new URLSearchParams(searchParams)
        params.set("tab", value)
        navigate(`${pathname}?${params.toString()}`, { replace: true })
    }

    // TanStack Query hooks
    const { data: user, isLoading: userLoading, isRefetching, dataUpdatedAt } = useUserDetails(userId)
    const { data: subscriptions = [], isLoading: subsLoading } = useUserSubscriptions(userId)

    // Mutation hooks
    const banMutation = useBanUser()
    const adminMutation = useToggleAdmin()
    const extendMutation = useExtendSubscription()
    const pauseMutation = usePauseSubscription()
    const resumeMutation = useResumeSubscription()
    const revokeMutation = useRevokeSubscription()
    const resetDataMutation = useResetData()

    const isLoading = userLoading || subsLoading
    const isSubMutationPending = extendMutation.isPending || pauseMutation.isPending ||
        resumeMutation.isPending || revokeMutation.isPending || resetDataMutation.isPending
    const actionLoading = banMutation.isPending || adminMutation.isPending
    const alerts = useUserAlerts(user, subscriptions)

    // Dialog states
    const [extendDialog, setExtendDialog] = useState<{ open: boolean; subscription: Subscription | null; days: string }>({
        open: false,
        subscription: null,
        days: "30",
    })
    const [dataLimitSub, setDataLimitSub] = useState<Subscription | null>(null)

    // Actions
    const handleRefresh = () => {
        queryClient.invalidateQueries({ queryKey: ['users', 'details', userId] })
        queryClient.invalidateQueries({ queryKey: ['users', 'subscriptions', userId] })
    }

    const handleBan = () => {
        if (!user) return
        banMutation.mutate({ userId: user.id, isBanned: user.is_banned })
    }

    const handleToggleAdmin = () => {
        if (!user) return
        adminMutation.mutate({ userId: user.id, isAdmin: user.is_admin })
    }

    const handleExtendSubscription = () => {
        if (!extendDialog.subscription || !extendDialog.days) return
        const days = parseInt(extendDialog.days)
        if (isNaN(days)) return
        extendMutation.mutate(
            { id: extendDialog.subscription.id, days },
            { onSuccess: () => setExtendDialog({ open: false, subscription: null, days: "30" }) }
        )
    }

    const handleToggle = (sub: Subscription, checked: boolean) => {
        if (checked && sub.status === "paused") {
            resumeMutation.mutate(sub.id)
        } else if (!checked && sub.status === "active") {
            pauseMutation.mutate(sub.id)
        }
    }

    const handleRevoke = async (sub: Subscription) => {
        const confirmed = await confirmDialog({
            title: "Revoke Subscription",
            description: "Are you sure you want to revoke this subscription? The user will immediately lose access.",
            confirmLabel: "Revoke",
            variant: "destructive",
        })
        if (!confirmed) return
        revokeMutation.mutate(sub.id)
    }

    const handleResetData = async (sub: Subscription) => {
        const confirmed = await confirmDialog({
            title: "Reset Data Usage",
            description: "Are you sure you want to reset the data usage to 0? This cannot be undone.",
            confirmLabel: "Reset",
            variant: "destructive",
        })
        if (!confirmed) return
        resetDataMutation.mutate(sub.id)
    }

    const handleCopyLink = async (sub: Subscription) => {
        const link = sub.subscription_url || sub.sub_link
        if (link) {
            await copyToClipboard(link)
            toast.success("Subscription link copied")
        } else {
            toast.error("No subscription link available")
        }
    }

    if (isLoading) {
        return (
            <div className="space-y-4 container mx-auto max-w-7xl animate-in fade-in duration-500 p-6">
                <Skeleton className="h-16 w-full rounded-xl" />
                <SummaryStripSkeleton />
                <Skeleton className="h-10 w-full rounded-lg" />
                <Skeleton className="h-[400px] rounded-xl" />
            </div>
        )
    }

    if (!user) return null

    return (
        <TooltipProvider delayDuration={300}>
            <div className="min-h-screen bg-background text-foreground pb-20">
                {/* Header */}
                <header className="md:sticky md:top-0 z-30 w-full border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/60">
                    <div className="container mx-auto max-w-7xl px-4 sm:px-6 h-16 flex items-center justify-between">
                        <div className="flex items-center gap-3">
                            <Button variant="ghost" size="icon" onClick={() => navigate("/users")} className="ml-[-12px]" aria-label="Back to users">
                                <HiOutlineArrowLeft className="w-5 h-5 opacity-70" />
                            </Button>
                            <div className="w-8 h-8 rounded-full bg-gradient-to-br from-primary/10 to-primary/5 flex items-center justify-center ring-1 ring-border shrink-0">
                                <span className="text-sm font-bold text-primary">
                                    {(user.username || user.first_name || "U").charAt(0).toUpperCase()}
                                </span>
                            </div>
                            <div className="flex flex-col min-w-0">
                                <div className="flex items-center gap-2">
                                    <span className="text-sm font-bold truncate max-w-[200px]">{user.username || `User #${user.id}`}</span>
                                    {user.is_banned ? (
                                        <Badge variant="danger" className="text-[9px] px-1.5 py-0">Banned</Badge>
                                    ) : (
                                        <Badge variant="success" className="bg-emerald-500/15 text-emerald-600 border-emerald-200 text-[9px] px-1.5 py-0">Active</Badge>
                                    )}
                                    {user.language && (
                                        <Badge variant="outline" className="text-[9px] px-1.5 py-0 hidden sm:inline-flex">{user.language.toUpperCase()}</Badge>
                                    )}
                                </div>
                                <span className="text-xs text-muted-foreground hidden sm:block truncate">
                                    @{user.username || "\u2014"} &middot; Joined {new Date(user.created_at).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" })} &middot; Last active {user.last_active_at ? new Date(user.last_active_at).toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "never"}
                                </span>
                            </div>
                        </div>

                        <div className="flex items-center gap-2">
                            <AutoRefreshControl
                                onRefresh={handleRefresh}
                                isRefreshing={isRefetching}
                                dataUpdatedAt={dataUpdatedAt}
                            />
                            <Button
                                variant={user.is_banned ? "default" : "secondary"}
                                size="sm"
                                className={cn(user.is_banned ? "bg-red-600 hover:bg-red-700" : "text-red-500 hover:text-red-600 bg-red-500/10 hover:bg-red-500/20")}
                                onClick={handleBan}
                                disabled={actionLoading}
                            >
                                {user.is_banned ? "Unban User" : "Ban User"}
                            </Button>
                        </div>
                    </div>
                </header>

                <main className="container mx-auto max-w-7xl px-4 sm:px-6 py-6 space-y-4 animate-in fade-in duration-500">
                    {/* Summary Strip */}
                    {isMobile ? (
                        <CollapsibleSummary user={user}>
                            <SummaryStrip user={user} subscriptions={subscriptions} onNavigateTab={handleTabChange} />
                        </CollapsibleSummary>
                    ) : (
                        <SummaryStrip user={user} subscriptions={subscriptions} onNavigateTab={handleTabChange} />
                    )}

                    {/* Alert Banner */}
                    <AlertBanner alerts={alerts} />

                    {/* 5-Tab Interface */}
                    <Tabs value={activeTab} onValueChange={handleTabChange} className="w-full">
                        <div className={cn("bg-background", isMobile && "sticky top-0 z-20 pb-2")}>
                            <TabsList className="bg-transparent border-b rounded-none w-full justify-start h-auto p-0 gap-0">
                                <TabsTrigger value="subscriptions" className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2.5 text-sm">
                                    {isMobile ? "Subs" : "Subscriptions"}
                                </TabsTrigger>
                                <TabsTrigger value="analytics" className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2.5 text-sm">
                                    {isMobile ? "Stats" : "Analytics"}
                                </TabsTrigger>
                                <TabsTrigger value="activity" className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2.5 text-sm">
                                    {isMobile ? "Log" : "Activity"}
                                </TabsTrigger>
                                <TabsTrigger value="profile" className="rounded-none border-b-2 border-transparent data-[state=active]:border-primary data-[state=active]:bg-transparent data-[state=active]:shadow-none px-4 py-2.5 text-sm">
                                    {isMobile ? "Info" : "Profile"}
                                </TabsTrigger>
                            </TabsList>
                        </div>

                        <TabsContent value="subscriptions" className="mt-4">
                            <UserSubscriptionsTab
                                subscriptions={subscriptions}
                                isSubMutationPending={isSubMutationPending}
                                onToggle={handleToggle}
                                onCopyLink={handleCopyLink}
                                onSetDataLimit={(sub) => setDataLimitSub(sub)}
                                onExtend={(sub) => setExtendDialog({ open: true, subscription: sub, days: "30" })}
                                onResetData={handleResetData}
                                onPause={(id) => pauseMutation.mutate(id)}
                                onResume={(id) => resumeMutation.mutate(id)}
                                onRevoke={handleRevoke}
                                onOpenDetails={openDetailsSheet}
                                onCreateSubscription={() => openCreateManualDialog(userId)}
                            />
                        </TabsContent>

                        <TabsContent value="analytics" className="mt-4 space-y-6">
                            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                                <Card className="border-0 shadow-sm bg-card/50">
                                    <CardHeader className="pb-2">
                                        <CardTitle className="text-sm font-medium text-muted-foreground">Data Usage</CardTitle>
                                    </CardHeader>
                                    <CardContent>
                                        <UsageChart userId={userId} />
                                    </CardContent>
                                </Card>
                                <div className="space-y-6">
                                    <Card className="border-0 shadow-sm bg-card/50">
                                        <CardHeader className="pb-2">
                                            <CardTitle className="text-sm font-medium text-muted-foreground">Usage Pattern</CardTitle>
                                        </CardHeader>
                                        <CardContent>
                                            <UsagePatternHeatmap userId={userId} />
                                        </CardContent>
                                    </Card>
                                    <Card className="border-0 shadow-sm bg-card/50">
                                        <CardContent className="p-4">
                                            <PerSubscriptionUsage subscriptions={subscriptions} />
                                        </CardContent>
                                    </Card>
                                </div>
                            </div>
                        </TabsContent>

                        <TabsContent value="activity" className="mt-4">
                            <Card className="border-0 shadow-sm bg-card/50">
                                <CardHeader className="pb-2">
                                    <CardTitle className="text-sm font-medium text-muted-foreground">Recent Activity</CardTitle>
                                </CardHeader>
                                <CardContent>
                                    <ActivityFeed userId={userId} />
                                </CardContent>
                            </Card>
                        </TabsContent>

                        <TabsContent value="profile" className="mt-4">
                            <ProfileTab
                                user={user}
                                onToggleAdmin={handleToggleAdmin}
                                actionLoading={actionLoading}
                            />
                        </TabsContent>
                    </Tabs>
                </main>

                {/* Dialogs */}
                <Dialog open={extendDialog.open} onOpenChange={(open) => setExtendDialog(prev => ({ ...prev, open }))}>
                    <DialogContent>
                        <DialogHeader>
                            <DialogTitle>Extend Subscription</DialogTitle>
                            <DialogDescription>
                                Add days to subscription #{extendDialog.subscription?.id}.
                            </DialogDescription>
                        </DialogHeader>
                        <div className="space-y-4 py-4">
                            <div className="space-y-2">
                                <Label htmlFor="days">Extension (Days)</Label>
                                <Input
                                    id="days"
                                    type="number"
                                    placeholder="30"
                                    value={extendDialog.days}
                                    onChange={(e) => setExtendDialog(prev => ({ ...prev, days: e.target.value }))}
                                />
                            </div>
                        </div>
                        <DialogFooter>
                            <Button variant="outline" onClick={() => setExtendDialog({ open: false, subscription: null, days: "30" })}>Cancel</Button>
                            <Button onClick={handleExtendSubscription} disabled={extendMutation.isPending}>
                                {extendMutation.isPending ? "Extending..." : "Confirm Extension"}
                            </Button>
                        </DialogFooter>
                    </DialogContent>
                </Dialog>

                <DataLimitDialog
                    open={dataLimitSub !== null}
                    onOpenChange={(open) => { if (!open) setDataLimitSub(null) }}
                    subscription={dataLimitSub}
                />

                <SubscriptionDetailsSheet />
            </div>
        </TooltipProvider>
    )
}
