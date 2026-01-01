import { useState, useEffect, useMemo } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogTitle,
} from "@/components/ui/dialog"
import {
    Globe,
    RefreshCw,
    Copy,
    Trash2,
    Server,
    User,
    Check,
    Link2,
    Loader2,
    Power,
    PowerOff,
} from "lucide-react"
import {
    useSyncAccountMutation,
    useUpdateAccountMutation,
    useDisableAccountMutation,
    useEnableAccountMutation,
} from "@/lib/queries"
import { getAccountLink } from "@/lib/api/accounts"
import { useAccountsStore } from "@/store/accounts-store"
import { cn, formatBytes, formatDate, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { Link } from "react-router"

function getDaysRemaining(endDate?: string): number {
    if (!endDate) return 0
    const end = new Date(endDate)
    const now = new Date()
    const diff = end.getTime() - now.getTime()
    return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
}

function SectionHeader({ children }: { children: React.ReactNode }) {
    return (
        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            {children}
        </h3>
    )
}

function StatRow({ label, value, extra }: { label: string; value: React.ReactNode; extra?: React.ReactNode }) {
    return (
        <div className="flex items-center justify-between py-1">
            <span className="text-sm text-muted-foreground">{label}</span>
            <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{value}</span>
                {extra}
            </div>
        </div>
    )
}

export function AccountDetailsSheet() {
    const { detailsSheet, closeDetailsSheet, openDeleteDialog } = useAccountsStore()
    const { account, open } = detailsSheet

    const syncMutation = useSyncAccountMutation()
    const updateMutation = useUpdateAccountMutation()
    const [gettingLink, setGettingLink] = useState(false)
    const disableMutation = useDisableAccountMutation()
    const enableMutation = useEnableAccountMutation()

    const [uuid, setUuid] = useState("")
    const [copied, setCopied] = useState("")
    const [linkText, setLinkText] = useState("")

    useEffect(() => {
        if (open && account) {
            setUuid(account.uuid)
            setCopied("")
            setLinkText("")
        }
    }, [open, account])

    const stats = useMemo(() => {
        if (!account) return null
        const isShared = !!account.subscription
        const totalLimit = isShared
            ? (account.subscription?.custom_data_limit ?? account.subscription?.data_limit ?? 0)
            : account.data_limit
        const totalUsed = isShared
            ? (account.subscription?.data_used ?? 0)
            : account.data_used
        const usagePercent = totalLimit > 0 ? Math.min((totalUsed / totalLimit) * 100, 100) : 0
        const expiryDate = account.subscription?.custom_end_date ?? account.subscription?.end_date ?? account.expires_at
        const daysRemaining = getDaysRemaining(expiryDate)

        return { isShared, totalLimit, totalUsed, usagePercent, expiryDate, daysRemaining }
    }, [account])

    if (!account || !stats) return null

    const handleCopy = async (text: string, item: string) => {
        await copyToClipboard(text)
        setCopied(item)
        toast.success(`${item} copied`)
        setTimeout(() => setCopied(""), 2000)
    }

    const handleGetLink = async () => {
        setGettingLink(true)
        try {
            const res = await getAccountLink(account.id)
            if (!res.success || !res.data?.link) throw new Error("Failed to get link")
            setLinkText(res.data.link)
            await copyToClipboard(res.data.link)
            toast.success("Subscription link copied")
        } catch {
            toast.error("Failed to get link")
        } finally {
            setGettingLink(false)
        }
    }

    const handleSaveUuid = () => {
        const trimmed = uuid.trim()
        if (!trimmed || trimmed === account.uuid) return
        updateMutation.mutate({
            id: account.id,
            data: {
                email: account.email,
                uuid: trimmed,
                flow: account.flow,
                data_limit: account.data_limit,
                expires_at: account.expires_at || null,
                enabled: account.status === "active",
            },
        })
    }

    const handleToggleStatus = () => {
        if (account.status === "active") {
            disableMutation.mutate(account.id, { onSuccess: () => closeDetailsSheet() })
        } else if (account.status === "disabled") {
            enableMutation.mutate(account.id, { onSuccess: () => closeDetailsSheet() })
        }
    }

    const isOnline = account.last_activity_at &&
        (new Date().getTime() - new Date(account.last_activity_at).getTime()) < 10 * 1000

    const statusBadgeColor = {
        active: "success",
        disabled: "danger",
        expired: "secondary",
    } as const

    const usageBarColor =
        stats.usagePercent > 90 ? "bg-red-500" :
        stats.usagePercent > 70 ? "bg-amber-500" : "bg-emerald-500"

    const daysColor =
        stats.daysRemaining <= 3 ? "text-red-500" :
        stats.daysRemaining <= 7 ? "text-amber-500" : "text-foreground"

    const source = account.subscription ? "subscription" : "manual"

    return (
        <Dialog open={open} onOpenChange={(v) => !v && closeDetailsSheet()}>
            <DialogContent
                className="max-w-xl p-0 gap-0 max-h-[95vh] overflow-hidden flex flex-col"
                onInteractOutside={(e) => e.preventDefault()}
            >
                {/* Sticky header */}
                <div className="px-5 pt-5 pb-3 border-b bg-background shrink-0 space-y-1.5">
                    <DialogTitle className="sr-only">Account #{account.id}</DialogTitle>
                    <DialogDescription className="sr-only">Details for account #{account.id}</DialogDescription>
                    <div className="flex items-center gap-2 flex-wrap pr-10">
                        <Badge
                            variant={statusBadgeColor[account.status] || "secondary"}
                            className="text-xs px-2 py-0.5 uppercase"
                        >
                            {account.status}
                        </Badge>
                        <Badge variant="outline" className="text-xs px-2 py-0.5 uppercase">
                            {source}
                        </Badge>
                        {/* Online indicator */}
                        <span className="relative flex h-2.5 w-2.5">
                            {isOnline && <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-400 opacity-75" />}
                            <span className={cn(
                                "relative inline-flex rounded-full h-2.5 w-2.5",
                                isOnline ? "bg-green-500" : "bg-slate-400/50"
                            )} />
                        </span>
                        <span className="text-xs font-mono text-muted-foreground ml-auto">#{account.id}</span>
                    </div>
                    <div className="flex items-center gap-1.5 text-sm text-muted-foreground flex-wrap">
                        <span className="font-medium text-foreground">{account.email}</span>
                        {account.inbound?.tag && (
                            <>
                                <span>·</span>
                                <Badge variant="outline" className="text-[10px] h-4 px-1">{account.inbound.tag}</Badge>
                            </>
                        )}
                    </div>
                </div>

                {/* Scrollable content */}
                <div className="flex-1 min-h-0 overflow-y-auto">
                <div className="px-5 py-4 space-y-5">

                    {/* === Summary === */}
                    <div className="rounded-lg border p-4 space-y-3">
                        {/* Usage bar */}
                        <div className="space-y-1.5">
                            <div className="flex items-center justify-between text-xs text-muted-foreground">
                                <span>{formatBytes(account.data_used)}{stats.isShared ? " (individual)" : ""}</span>
                                <span>{stats.totalLimit > 0 ? formatBytes(stats.totalLimit) : "Unlimited"}</span>
                            </div>
                            <div className="h-2 bg-muted rounded-full overflow-hidden">
                                <div
                                    className={cn("h-full rounded-full transition-all", usageBarColor)}
                                    style={{
                                        width: stats.totalLimit === 0
                                            ? "3%"
                                            : `${Math.max(stats.usagePercent, 1)}%`
                                    }}
                                />
                            </div>
                            {stats.totalLimit > 0 && (
                                <p className="text-[10px] text-muted-foreground">
                                    {stats.usagePercent.toFixed(1)}% used{stats.isShared ? " (pool)" : ""}
                                </p>
                            )}
                        </div>

                        <Separator />

                        <StatRow
                            label="Days Left"
                            value={
                                stats.expiryDate
                                    ? <span className={daysColor}>{stats.daysRemaining}</span>
                                    : <span className="text-muted-foreground">Unlimited</span>
                            }
                            extra={
                                stats.expiryDate ? (
                                    <span className="text-xs text-muted-foreground">
                                        (expires {formatDate(stats.expiryDate, "long")})
                                    </span>
                                ) : null
                            }
                        />
                        {stats.isShared && (
                            <StatRow
                                label="Pool Usage"
                                value={`${formatBytes(stats.totalUsed)} / ${stats.totalLimit > 0 ? formatBytes(stats.totalLimit) : "∞"}`}
                            />
                        )}
                        <StatRow label="Individual Usage" value={formatBytes(account.data_used)} />
                        <StatRow label="Created" value={formatDate(account.created_at, "long")} />
                    </div>

                    <Separator />

                    {/* === Connection Info === */}
                    <div className="space-y-2">
                        <SectionHeader>Connection Info</SectionHeader>
                        <div className="rounded-lg border p-3 space-y-2">
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-2 text-sm">
                                    <Server className="w-3.5 h-3.5 text-muted-foreground" />
                                    <span className="text-muted-foreground">Node</span>
                                </div>
                                {account.inbound?.node ? (
                                    <Link
                                        href={`/nodes/${account.inbound.node.id}`}
                                        className="text-sm font-medium hover:underline text-primary"
                                    >
                                        {account.inbound.node.name}
                                    </Link>
                                ) : (
                                    <span className="text-sm text-muted-foreground">-</span>
                                )}
                            </div>
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-2 text-sm">
                                    <Globe className="w-3.5 h-3.5 text-muted-foreground" />
                                    <span className="text-muted-foreground">Inbound</span>
                                </div>
                                <span className="text-sm font-medium">
                                    {account.inbound?.tag || "-"} / {account.inbound?.port || "-"}
                                </span>
                            </div>
                            <div className="flex items-center justify-between">
                                <span className="text-sm text-muted-foreground">Protocol</span>
                                <Badge variant="outline" className="uppercase font-mono text-xs">
                                    {account.inbound?.protocol || "-"}
                                </Badge>
                            </div>
                            {account.flow && (
                                <div className="flex items-center justify-between">
                                    <span className="text-sm text-muted-foreground">Flow</span>
                                    <span className="text-sm font-mono">{account.flow}</span>
                                </div>
                            )}
                        </div>
                    </div>

                    <Separator />

                    {/* === Ownership === */}
                    <div className="space-y-2">
                        <SectionHeader>Ownership</SectionHeader>
                        <div className="rounded-lg border p-3 space-y-2">
                            <div className="flex items-center justify-between">
                                <div className="flex items-center gap-2 text-sm">
                                    <User className="w-3.5 h-3.5 text-muted-foreground" />
                                    <span className="text-muted-foreground">User</span>
                                </div>
                                {account.subscription?.user ? (
                                    <Link
                                        href={`/users/${account.subscription.user.id}`}
                                        className="text-sm font-medium hover:underline text-primary"
                                    >
                                        {account.subscription.user.username}
                                    </Link>
                                ) : (
                                    <span className="text-sm text-muted-foreground">-</span>
                                )}
                            </div>
                            <div className="flex items-center justify-between">
                                <span className="text-sm text-muted-foreground">Subscription</span>
                                {account.subscription ? (
                                    <span className="text-sm font-medium">#{account.subscription.id}</span>
                                ) : (
                                    <span className="text-sm text-muted-foreground">Manual Account</span>
                                )}
                            </div>
                        </div>
                    </div>

                    <Separator />

                    {/* === Subscription Link === */}
                    <div className="space-y-2">
                        <SectionHeader>Subscription Link</SectionHeader>
                        {linkText ? (
                            <>
                                <div className="bg-muted/50 px-3 py-2 rounded-md text-xs font-mono break-all border">
                                    {linkText}
                                </div>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="w-full"
                                    onClick={() => handleCopy(linkText, "Link")}
                                >
                                    {copied === "Link" ? (
                                        <><Check className="w-3.5 h-3.5 mr-1.5 text-emerald-500" /> Copied</>
                                    ) : (
                                        <><Copy className="w-3.5 h-3.5 mr-1.5" /> Copy Link</>
                                    )}
                                </Button>
                            </>
                        ) : (
                            <Button
                                variant="outline"
                                size="sm"
                                className="w-full"
                                onClick={handleGetLink}
                                disabled={gettingLink}
                            >
                                {gettingLink ? (
                                    <><Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> Getting link...</>
                                ) : (
                                    <><Link2 className="w-3.5 h-3.5 mr-1.5" /> Get Link</>
                                )}
                            </Button>
                        )}
                    </div>

                    <Separator />

                    {/* === UUID === */}
                    <div className="space-y-2">
                        <SectionHeader>UUID</SectionHeader>
                        <div className="flex gap-2">
                            <Input
                                value={uuid}
                                onChange={(e) => setUuid(e.target.value)}
                                className="font-mono text-xs h-8 flex-1"
                                placeholder="Account UUID"
                            />
                            <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handleCopy(uuid, "UUID")}
                            >
                                {copied === "UUID" ? <Check className="w-3.5 h-3.5 text-emerald-500" /> : <Copy className="w-3.5 h-3.5" />}
                            </Button>
                            <Button
                                size="sm"
                                onClick={handleSaveUuid}
                                disabled={updateMutation.isPending || !uuid.trim() || uuid.trim() === account.uuid}
                            >
                                {updateMutation.isPending ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Save"}
                            </Button>
                        </div>
                    </div>

                    <Separator />

                    {/* === Sync Stats === */}
                    <div className="flex items-center justify-between">
                        <div>
                            <SectionHeader>Sync Stats</SectionHeader>
                            <p className="text-xs text-muted-foreground mt-0.5">
                                Force sync data from the node
                            </p>
                        </div>
                        <Button
                            variant="outline"
                            size="sm"
                            onClick={() => syncMutation.mutate(account.id)}
                            disabled={syncMutation.isPending}
                        >
                            {syncMutation.isPending ? (
                                <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                            ) : (
                                <RefreshCw className="w-3.5 h-3.5 mr-1.5" />
                            )}
                            Sync Now
                        </Button>
                    </div>

                    <Separator />

                    {/* === Danger Zone === */}
                    <div className="space-y-2">
                        <SectionHeader>Danger Zone</SectionHeader>
                        <div className="flex gap-2">
                            {account.status === "active" ? (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="text-amber-600 border-amber-200 hover:bg-amber-50 dark:border-amber-900 dark:hover:bg-amber-950 flex-1"
                                    onClick={handleToggleStatus}
                                    disabled={disableMutation.isPending}
                                >
                                    <PowerOff className="w-3.5 h-3.5 mr-1.5" />
                                    {disableMutation.isPending ? "Disabling..." : "Disable"}
                                </Button>
                            ) : account.status === "disabled" ? (
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="text-emerald-600 border-emerald-200 hover:bg-emerald-50 dark:border-emerald-900 dark:hover:bg-emerald-950 flex-1"
                                    onClick={handleToggleStatus}
                                    disabled={enableMutation.isPending}
                                >
                                    <Power className="w-3.5 h-3.5 mr-1.5" />
                                    {enableMutation.isPending ? "Enabling..." : "Enable"}
                                </Button>
                            ) : null}
                            <Button
                                variant="outline"
                                size="sm"
                                className="text-red-700 border-red-200 hover:bg-red-50 dark:border-red-900 dark:hover:bg-red-950 flex-1"
                                onClick={() => {
                                    closeDetailsSheet()
                                    setTimeout(() => openDeleteDialog(account), 150)
                                }}
                            >
                                <Trash2 className="w-3.5 h-3.5 mr-1.5" />
                                Delete
                            </Button>
                        </div>
                        <p className="text-[10px] text-muted-foreground">
                            Disable temporarily blocks access. Delete removes the record permanently.
                        </p>
                    </div>
                </div>
                </div>
            </DialogContent>
        </Dialog>
    )
}
