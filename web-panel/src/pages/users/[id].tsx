import { useEffect, useRef, useState, type ReactNode } from "react"
import { useLocation, useNavigate, useParams, useSearchParams } from "react-router"
import { useQueryClient } from "@tanstack/react-query"
import { Plus } from "lucide-react"
import { toast } from "@/lib/toast"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { TooltipProvider } from "@/components/ui/tooltip"
import { useConfirm } from "@/components/ui/confirm-dialog"
import { SubscriptionDetailsSheet } from "@/components/subscription/subscription-details-sheet"
import { CreateManualSubscriptionDialog } from "@/components/subscription/create-manual-subscription-dialog"
import { DataLimitDialog } from "@/components/subscription/data-limit-dialog"
import { useSubscriptionsStore } from "@/store/subscriptions-store"
import { useIsMobile } from "@/hooks/use-is-mobile"
import { usePageTitle } from "@/hooks/use-page-title"
import type { Subscription } from "@/lib/types"
import { copyToClipboard, cn } from "@/lib/utils"
import { queryKeys } from "@/lib/queries/keys"
import {
    useUserDetails, useUserSubscriptions, useUserOverview,
    useBanUser, useToggleAdmin, useUpdateTelegramID,
    useExtendSubscription, usePauseSubscription, useResumeSubscription, useRevokeSubscription,
    useResetData, useAddData, useSetMaxDevices, useRegenerateSubscriptionKey,
} from "@/lib/queries"
import { UserHeader, BannedStrip } from "@/components/user/user-header"
import { FigureStrip, FigureTicker, buildTiles, type UserTab } from "@/components/user/figure-strip"
import { SubscriptionPanel, EndedRow, type SubscriptionActions } from "@/components/user/subscription-panel"
import { NoSubscriptionPanel } from "@/components/user/no-subscription"
import { Timeline } from "@/components/user/timeline"
import { IdentityCard, NotesCard } from "@/components/user/profile-cards"
import { AddDataDialog, NumberDialog } from "@/components/user/user-dialogs"
import { displayName, isEnded, plural, subscriptionName } from "@/components/user/format"

const TABS: UserTab[] = ["subscriptions", "activity", "profile"]

function tabFrom(v: string | null): UserTab {
    if (v === "analytics" || v === "payments") return "subscriptions"
    return TABS.includes(v as UserTab) ? (v as UserTab) : "subscriptions"
}

/** Most live first: exhausted, active, paused; newest inside each. */
function liveOrder(a: Subscription, b: Subscription) {
    const rank = (s: Subscription) => (s.status === "traffic_exhausted" ? 0 : s.status === "active" ? 1 : 2)
    return rank(a) - rank(b) || new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
}

export default function UserDetailPage() {
    const { id } = useParams()
    const userId = Number(id)
    const navigate = useNavigate()
    const { pathname } = useLocation()
    const [searchParams] = useSearchParams()
    const tab = tabFrom(searchParams.get("tab"))
    const isMobile = useIsMobile()
    const confirm = useConfirm()
    const qc = useQueryClient()
    const { openDetailsSheet, openCreateManualDialog } = useSubscriptionsStore()
    const detailsOpen = useSubscriptionsStore(s => s.detailsSheet.open)

    const { data: user, isLoading: userLoading, isRefetching, dataUpdatedAt } = useUserDetails(userId)
    const { data: subscriptions = [], isLoading: subsLoading } = useUserSubscriptions(userId)
    const { data: overview } = useUserOverview(userId)
    usePageTitle(user ? displayName(user) : "User")

    const ban = useBanUser()
    const toggleAdmin = useToggleAdmin()
    const updateTelegramId = useUpdateTelegramID()
    const extend = useExtendSubscription()
    const pause = usePauseSubscription()
    const resume = useResumeSubscription()
    const revoke = useRevokeSubscription()
    const resetData = useResetData()
    const addData = useAddData()
    const setDevices = useSetMaxDevices()
    const regenerate = useRegenerateSubscriptionKey()

    const subBusy = extend.isPending || pause.isPending || resume.isPending || revoke.isPending || resetData.isPending || addData.isPending || setDevices.isPending || regenerate.isPending
    const userBusy = ban.isPending || toggleAdmin.isPending || updateTelegramId.isPending

    const [extendSub, setExtendSub] = useState<Subscription | null>(null)
    const [addDataSub, setAddDataSub] = useState<Subscription | null>(null)
    const [devicesSub, setDevicesSub] = useState<Subscription | null>(null)
    const [limitSub, setLimitSub] = useState<Subscription | null>(null)
    const [editTelegram, setEditTelegram] = useState(false)
    const [scrollTo, setScrollTo] = useState<number | null>(null)

    // Phone: once the tabs scroll above the viewport, pin the figures (as one
    // line) and the tabs to the top. The layout's <main> doesn't scroll on
    // phones, so CSS sticky can't do this.
    const stripRef = useRef<HTMLDivElement>(null)
    const tabsRef = useRef<HTMLDivElement>(null)
    const [pinned, setPinned] = useState(false)
    useEffect(() => {
        const el = tabsRef.current
        if (!el || !isMobile) return
        const obs = new IntersectionObserver(([e]) => setPinned(!e.isIntersecting && e.boundingClientRect.top < 0), { threshold: 0 })
        obs.observe(el)
        return () => obs.disconnect()
    }, [isMobile, user])

    useEffect(() => {
        if (scrollTo == null || tab !== "subscriptions") return
        const t = setTimeout(() => {
            document.getElementById(`sub-${scrollTo}`)?.scrollIntoView({ behavior: "smooth", block: "start" })
            setScrollTo(null)
        }, 50)
        return () => clearTimeout(t)
    }, [scrollTo, tab])

    const setTab = (t: UserTab) => {
        const next = new URLSearchParams(searchParams)
        next.set("tab", t)
        navigate(`${pathname}?${next.toString()}`, { replace: true })
        // From the pinned bar, start the new tab at its top.
        if (pinned) tabsRef.current?.scrollIntoView({ block: "start" })
    }
    const openSubscription = (subId: number) => {
        setScrollTo(subId)
        setTab("subscriptions")
    }

    // Subscription mutations invalidate the subscriptions keys; the user
    // page reads its own copies under users.
    const refreshUser = () => {
        qc.invalidateQueries({ queryKey: queryKeys.userSubscriptions(userId) })
        qc.invalidateQueries({ queryKey: queryKeys.userOverview(userId) })
        qc.invalidateQueries({ queryKey: queryKeys.userDetails(userId) })
    }
    const after = { onSuccess: refreshUser }

    // Changes made in the subscription details sheet show up once it closes.
    const wasOpen = useRef(false)
    useEffect(() => {
        if (wasOpen.current && !detailsOpen) refreshUser()
        wasOpen.current = detailsOpen
    }, [detailsOpen])

    const actions: SubscriptionActions = {
        onToggle: (s, on) => (on ? resume.mutate(s.id, after) : pause.mutate(s.id, after)),
        onCopyLink: async (s) => {
            const link = s.subscription_url || s.sub_link
            if (!link) return toast.error("This subscription has no link")
            await copyToClipboard(link)
            toast.success("Subscription link copied")
        },
        onExtend: (s) => setExtendSub(s),
        onAddData: (s) => setAddDataSub(s),
        onDataLimit: (s) => setLimitSub(s),
        onResetUsage: async (s) => {
            const ok = await confirm({
                title: `Reset usage of #${s.id}?`,
                description: "Data used goes back to 0 for this cycle. Lifetime traffic is kept.",
                confirmLabel: "Reset usage",
                variant: "destructive",
            })
            if (ok) resetData.mutate(s.id, after)
        },
        onDeviceLimit: (s) => setDevicesSub(s),
        onRegenerateLink: async (s) => {
            const ok = await confirm({
                title: `Regenerate the link of #${s.id}?`,
                description: "The current subscription link stops working. The customer needs the new one.",
                confirmLabel: "Regenerate link",
                variant: "warning",
            })
            if (ok) regenerate.mutate({ id: s.id }, after)
        },
        onOpenDetails: (s) => openDetailsSheet(s),
        onPause: (s) => pause.mutate(s.id, after),
        onResume: (s) => resume.mutate(s.id, after),
        onRevoke: async (s) => {
            const ok = await confirm({
                title: `Revoke #${s.id}?`,
                description: "The customer loses access immediately. This can't be undone.",
                confirmLabel: "Revoke",
                variant: "destructive",
            })
            if (ok) revoke.mutate(s.id, after)
        },
    }

    const onBan = async () => {
        if (!user) return
        const toPause = subscriptions.filter(s => s.status === "active")
        const effects: ReactNode = (
            <div className="space-y-3 text-left">
                <ul className="list-disc space-y-1 pl-5">
                    <li>The bot ignores their messages.</li>
                    <li>Expiry notices stop.</li>
                    <li>Sub-page chat is closed.</li>
                </ul>
                {toPause.length > 0 && (
                    <p>Pauses now: {toPause.map(s => `#${s.id} ${subscriptionName(s)}`).join(", ")}. Unbanning resumes {toPause.length === 1 ? "it" : "them"}; expiry is not extended.</p>
                )}
            </div>
        )
        const ok = await confirm({
            title: `Ban ${displayName(user)}?`,
            description: effects,
            confirmLabel: toPause.length ? `Ban and pause ${toPause.length}` : "Ban user",
            variant: "destructive",
        })
        if (ok) ban.mutate({ userId, isBanned: false }, after)
    }
    const onUnban = async () => {
        if (!user) return
        const toResume = subscriptions.filter(s => s.status === "paused" && s.pause_reason === "ban")
        const ok = await confirm({
            title: `Unban ${displayName(user)}?`,
            description: toResume.length
                ? `Resumes ${toResume.map(s => `#${s.id}`).join(", ")}, the ${plural(toResume.length, "subscription")} the ban paused. Any that expired meanwhile end as usual.`
                : "The bot and chat work for them again.",
            confirmLabel: "Unban",
        })
        if (ok) ban.mutate({ userId, isBanned: true }, after)
    }
    const onToggleAdmin = async () => {
        if (!user) return
        const ok = await confirm({
            title: user.is_admin ? `Remove bot admin from ${displayName(user)}?` : `Make ${displayName(user)} a bot admin?`,
            description: user.is_admin
                ? "Takes the bot's admin commands away from this Telegram account. Panel access is unchanged."
                : "Gives this Telegram account the bot's admin commands. Panel access is unchanged.",
            confirmLabel: user.is_admin ? "Remove bot admin" : "Make bot admin",
            variant: user.is_admin ? "default" : "warning",
        })
        if (ok) toggleAdmin.mutate({ userId, isAdmin: user.is_admin })
    }

    if (userLoading || subsLoading) {
        return (
            <div className="space-y-4 p-4 sm:p-6">
                <Skeleton className="h-14 w-full rounded-xl" />
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">{[0, 1, 2, 3].map(i => <Skeleton key={i} className="h-[92px] rounded-xl" />)}</div>
                <Skeleton className="h-10 w-full" />
                <Skeleton className="h-[480px] rounded-xl" />
            </div>
        )
    }
    if (!user) return null

    const live = subscriptions.filter(s => !isEnded(s)).sort(liveOrder)
    const ended = subscriptions.filter(isEnded)
    const overviewFor = (subId: number) => overview?.subscriptions.find(o => o.subscription_id === subId)
    const tiles = buildTiles(user, subscriptions, overview)
    const banPaused = subscriptions.filter(s => s.status === "paused" && s.pause_reason === "ban").map(s => s.id)

    const tabList = (
        <div role="tablist" aria-label="User sections" className="flex border-b border-border px-1.5 sm:px-0">
            {TABS.map(t => {
                const label = t === "subscriptions" ? (isMobile ? "Subs" : "Subscriptions") : t.charAt(0).toUpperCase() + t.slice(1)
                const count = t === "subscriptions" ? subscriptions.length : null
                return (
                    <button
                        key={t}
                        type="button"
                        role="tab"
                        aria-selected={tab === t}
                        onClick={() => setTab(t)}
                        className={cn(
                            "-mb-px inline-flex h-11 items-center gap-1.5 border-b-2 px-2.5 text-sm transition-colors sm:gap-2 sm:px-4",
                            tab === t ? "border-foreground font-medium text-foreground" : "border-transparent text-foreground/70 hover:text-foreground",
                        )}
                    >
                        {label}
                        {count != null && <span className="font-mono text-xs text-muted-foreground">{count}</span>}
                    </button>
                )
            })}
        </div>
    )

    return (
        <TooltipProvider delayDuration={300}>
            <div className="min-h-screen bg-background pb-20 text-foreground">
                <UserHeader
                    user={user}
                    isRefetching={isRefetching}
                    dataUpdatedAt={dataUpdatedAt}
                    busy={userBusy}
                    actions={{
                        onEditTelegramId: () => setEditTelegram(true),
                        onToggleAdmin,
                        onBan,
                        onUnban,
                    }}
                />
                {user.is_banned && <BannedStrip ban={overview?.ban} pausedIds={banPaused} onUnban={onUnban} busy={userBusy} />}

                <main className="mx-auto flex max-w-7xl flex-col gap-4 p-4 sm:px-6 sm:py-6">
                    <div ref={stripRef}>
                        <FigureStrip tiles={tiles} onNavigate={setTab} />
                    </div>

                    <div ref={tabsRef}>{tabList}</div>
                    {isMobile && pinned && (
                        <div className="fixed inset-x-0 top-0 z-30 bg-background shadow-md">
                            <FigureTicker tiles={tiles} onExpand={() => stripRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })} />
                            {tabList}
                        </div>
                    )}

                    {tab === "subscriptions" && (
                        <div className="flex flex-col gap-4">
                            {subscriptions.length > 0 ? (
                                <>
                                    <div className="flex items-center justify-between">
                                        <span className="text-[13px] text-muted-foreground">{plural(subscriptions.length, "subscription")}</span>
                                        <Button size="sm" className="h-9 gap-1.5 sm:h-8" onClick={() => openCreateManualDialog(userId, user)}>
                                            <Plus className="size-4" /><span className="sm:hidden">Create</span><span className="hidden sm:inline">Create subscription</span>
                                        </Button>
                                    </div>
                                    {live.map(s => <SubscriptionPanel key={s.id} sub={s} overview={overviewFor(s.id)} busy={subBusy} actions={actions} userBanned={user.is_banned} banAt={overview?.ban?.at} />)}
                                    {ended.length > 0 && (
                                        <div className="flex flex-col gap-2">
                                            <div className="flex items-center gap-2">
                                                <h3 className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">Ended</h3>
                                                <span className="rounded bg-muted px-1.5 font-mono text-[12px] font-semibold">{ended.length}</span>
                                            </div>
                                            {ended.map(s => <EndedRow key={s.id} sub={s} overview={overviewFor(s.id)} busy={subBusy} actions={actions} />)}
                                        </div>
                                    )}
                                </>
                            ) : (
                                <NoSubscriptionPanel user={user} onCreate={() => openCreateManualDialog(userId, user)} />
                            )}
                        </div>
                    )}

                    {tab === "activity" && (
                        <Timeline userId={userId} onOpenSubscription={openSubscription} />
                    )}

                    {tab === "profile" && (
                        <div className="grid gap-4 lg:grid-cols-2">
                            <IdentityCard user={user} overview={overview} onEditTelegramId={() => setEditTelegram(true)} />
                            <NotesCard userId={userId} initial={user.admin_notes || ""} />
                        </div>
                    )}
                </main>

                <NumberDialog
                    open={extendSub !== null}
                    onOpenChange={(open) => { if (!open) setExtendSub(null) }}
                    title={`Extend #${extendSub?.id ?? ""}`}
                    description="Adds days to the end date. An expired subscription comes back."
                    label="Days"
                    initial="30"
                    suffix="days"
                    min={1}
                    pending={extend.isPending}
                    confirmLabel={(n) => (n > 0 ? `Extend by ${plural(n, "day")}` : "Extend")}
                    onSubmit={(days) => extendSub && extend.mutate({ id: extendSub.id, days }, { onSuccess: () => { refreshUser(); setExtendSub(null) } })}
                />
                <NumberDialog
                    open={devicesSub !== null}
                    onOpenChange={(open) => { if (!open) setDevicesSub(null) }}
                    title={`Device limit of #${devicesSub?.id ?? ""}`}
                    description="How many devices can connect at once. 0 means no limit."
                    label="Devices"
                    initial={String(devicesSub?.max_devices ?? 0)}
                    min={0}
                    pending={setDevices.isPending}
                    confirmLabel={(n) => (n > 0 ? `Allow ${plural(n, "device")}` : "Remove the limit")}
                    onSubmit={(maxDevices) => devicesSub && setDevices.mutate({ id: devicesSub.id, maxDevices }, { onSuccess: () => { refreshUser(); setDevicesSub(null) } })}
                />
                <NumberDialog
                    open={editTelegram}
                    onOpenChange={setEditTelegram}
                    title="Edit Telegram ID"
                    description="The chat ID the bot uses to reach this customer."
                    label="Telegram ID"
                    initial={user.telegram_id > 0 ? String(user.telegram_id) : ""}
                    min={1}
                    pending={updateTelegramId.isPending}
                    confirmLabel={() => "Save"}
                    onSubmit={(telegramId) => updateTelegramId.mutate({ userId, telegramId }, { onSuccess: () => setEditTelegram(false) })}
                />
                <AddDataDialog
                    subscription={addDataSub}
                    open={addDataSub !== null}
                    onOpenChange={(open) => { if (!open) setAddDataSub(null) }}
                    pending={addData.isPending}
                    onSubmit={(amountGb) => addDataSub && addData.mutate({ id: addDataSub.id, amountGb }, { onSuccess: () => { refreshUser(); setAddDataSub(null) } })}
                />
                <DataLimitDialog open={limitSub !== null} onOpenChange={(open) => { if (!open) { setLimitSub(null); refreshUser() } }} subscription={limitSub} />

                <SubscriptionDetailsSheet />
                <CreateManualSubscriptionDialog />
            </div>
        </TooltipProvider>
    )
}
