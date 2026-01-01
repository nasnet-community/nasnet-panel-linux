import { useState } from "react"
import { FaTelegram } from "react-icons/fa"
import {
    Globe,
    Plus,
    Copy,
    Check,
    Loader2,
    X,
    UserPlus,
    Wifi,
} from "lucide-react"
import { format } from "date-fns"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { cn, copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import type { Subscription, SubscriptionIP } from "@/lib/types"
import type { Account } from "@/lib/api/accounts"
import { getAccountLink } from "@/lib/api/accounts"
import { bulkManageInbounds } from "@/lib/api/subscriptions"
import { useQueryClient } from "@tanstack/react-query"
import { SectionHeader } from "./section-header"

interface AccessSectionProps {
    subscription: Subscription
    accounts: Account[]
    accountsLoading: boolean
    ips: SubscriptionIP[]
    ipsLoading: boolean
    onAssignInbound: () => void
    onAssignUser: () => void
    onUnlinkUser: () => void
}

export function AccessSection({
    subscription,
    accounts,
    accountsLoading,
    ips,
    ipsLoading,
    onAssignInbound,
    onAssignUser,
    onUnlinkUser,
}: AccessSectionProps) {
    const queryClient = useQueryClient()
    const [copyingLinkForAccount, setCopyingLinkForAccount] = useState<number | null>(null)
    const [deletingAccountId, setDeletingAccountId] = useState<number | null>(null)
    const [copiedLinkAccount, setCopiedLinkAccount] = useState<number | null>(null)

    const handleCopyLink = async (accountId: number) => {
        setCopyingLinkForAccount(accountId)
        try {
            const res = await getAccountLink(accountId)
            if (res.success && res.data?.link) {
                await copyToClipboard(res.data.link)
                setCopiedLinkAccount(accountId)
                toast.success("Account link copied")
                setTimeout(() => setCopiedLinkAccount(null), 1500)
            } else {
                toast.error("No link available")
            }
        } catch {
            toast.error("Failed to get link")
        } finally {
            setCopyingLinkForAccount(null)
        }
    }

    const handleRemoveInbound = async (account: Account) => {
        if (!subscription.id || !account.inbound_id) return
        setDeletingAccountId(account.id)
        try {
            const res = await bulkManageInbounds({
                subscription_ids: [subscription.id],
                add_inbound_ids: [],
                remove_inbound_ids: [account.inbound_id],
            })
            if (res.success) {
                toast.success(`Removed ${account.inbound?.tag || "inbound"}`)
                queryClient.setQueryData(
                    ["accounts", "subscription", subscription.id],
                    (old: Account[] | undefined) => old?.filter((a) => a.id !== account.id) ?? [],
                )
                queryClient.invalidateQueries({ queryKey: ["accounts", "subscription", subscription.id] })
            } else {
                toast.error(res.error || "Failed to remove inbound")
            }
        } catch {
            toast.error("Failed to remove inbound")
        } finally {
            setDeletingAccountId(null)
        }
    }

    return (
        <div className="space-y-4">
            {/* Accounts */}
            <div className="space-y-2">
                <SectionHeader
                    tone="default"
                    right={
                        <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 text-xs"
                            onClick={onAssignInbound}
                        >
                            <Plus className="w-3 h-3 mr-1" />
                            Assign inbound
                        </Button>
                    }
                >
                    Accounts {!accountsLoading && `(${accounts.length})`}
                </SectionHeader>

                {accountsLoading ? (
                    <div className="space-y-2">
                        {[1, 2].map((i) => (
                            <div key={i} className="h-10 bg-muted/20 animate-pulse rounded-md" />
                        ))}
                    </div>
                ) : accounts.length === 0 ? (
                    <p className="text-sm text-muted-foreground py-2">No accounts</p>
                ) : (
                    <div className="space-y-1">
                        {accounts.map((account) => (
                            <div
                                key={account.id}
                                className="flex items-center justify-between py-1.5 px-2 rounded-md hover:bg-muted/50 group"
                            >
                                <div className="flex items-center gap-2 min-w-0">
                                    <Globe className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                                    <span className="text-sm font-medium truncate">{account.email}</span>
                                    <Badge variant="outline" className="h-4 px-1 text-[10px] shrink-0">
                                        {account.inbound?.tag || "?"}
                                    </Badge>
                                </div>
                                <div className="flex items-center gap-0.5 sm:opacity-0 sm:group-hover:opacity-100 focus-within:opacity-100 transition-opacity shrink-0">
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className="h-6 text-xs shrink-0"
                                        disabled={copyingLinkForAccount === account.id}
                                        onClick={() => handleCopyLink(account.id)}
                                        aria-label={`Copy link for ${account.email}`}
                                    >
                                        {copyingLinkForAccount === account.id ? (
                                            <><Loader2 className="w-3 h-3 mr-1 animate-spin" /> Loading</>
                                        ) : copiedLinkAccount === account.id ? (
                                            <><Check className="w-3 h-3 mr-1 text-emerald-500" /> Copied</>
                                        ) : (
                                            <><Copy className="w-3 h-3 mr-1" /> Link</>
                                        )}
                                    </Button>
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className="h-6 w-6 p-0 text-muted-foreground hover:text-destructive shrink-0"
                                        disabled={deletingAccountId === account.id}
                                        onClick={() => handleRemoveInbound(account)}
                                        aria-label={`Remove ${account.inbound?.tag || "inbound"}`}
                                    >
                                        {deletingAccountId === account.id ? (
                                            <Loader2 className="w-3 h-3 animate-spin" />
                                        ) : (
                                            <X className="w-3 h-3" />
                                        )}
                                    </Button>
                                </div>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            {/* Connected IPs */}
            {(ipsLoading || ips.length > 0) && (
                <>
                    <Separator />
                    <div className="space-y-2">
                        <SectionHeader
                            right={
                                ips.length > 1 && (
                                    <Button
                                        variant="ghost"
                                        size="sm"
                                        className="h-6 px-2 text-[10px] text-muted-foreground hover:text-foreground"
                                        onClick={async () => {
                                            const all = ips.map((ip) => ip.ip).join("\n")
                                            await copyToClipboard(all)
                                            toast.success(`Copied ${ips.length} IPs`)
                                        }}
                                    >
                                        <Copy className="w-3 h-3 mr-1" />
                                        Copy all
                                    </Button>
                                )
                            }
                        >
                            Connected IPs {!ipsLoading && `(${ips.length})`}
                        </SectionHeader>

                        {ipsLoading ? (
                            <div className="space-y-1.5">
                                {[...Array(3)].map((_, i) => (
                                    <div key={i} className="h-8 bg-muted/20 animate-pulse rounded-md" />
                                ))}
                            </div>
                        ) : (
                            <div className="space-y-0.5 max-h-48 overflow-y-auto">
                                {ips.map((ip) => {
                                    const isActive = new Date(ip.last_seen).getTime() > Date.now() - 60_000
                                    return (
                                        <button
                                            key={ip.id}
                                            type="button"
                                            className="w-full flex items-center justify-between py-1.5 px-2 rounded-md hover:bg-muted/50 active:bg-muted/70 transition-colors cursor-pointer text-left group"
                                            onClick={async () => {
                                                await copyToClipboard(ip.ip)
                                                toast.success(`Copied ${ip.ip}`)
                                            }}
                                            aria-label={`Copy IP ${ip.ip}`}
                                        >
                                            <div className="flex items-center gap-2 min-w-0">
                                                <Wifi
                                                    className={cn(
                                                        "w-3.5 h-3.5 shrink-0",
                                                        isActive ? "text-emerald-500" : "text-muted-foreground/50",
                                                    )}
                                                />
                                                <span className="text-sm font-mono truncate">{ip.ip}</span>
                                                {isActive && (
                                                    <Badge
                                                        variant="outline"
                                                        className="h-4 px-1 text-[10px] text-emerald-600 border-emerald-600/30 shrink-0"
                                                    >
                                                        active
                                                    </Badge>
                                                )}
                                            </div>
                                            <div className="flex items-center gap-1.5 shrink-0 ml-2">
                                                <span className="text-[11px] text-muted-foreground">
                                                    {format(new Date(ip.last_seen), "MMM d, HH:mm")}
                                                </span>
                                                <Copy className="w-3 h-3 text-muted-foreground/0 group-hover:text-muted-foreground transition-colors" />
                                            </div>
                                        </button>
                                    )
                                })}
                            </div>
                        )}
                    </div>
                </>
            )}

            <Separator />

            {/* User Assignment */}
            <div className="space-y-2">
                <SectionHeader tone="default">User Assignment</SectionHeader>
                {subscription.user_id && subscription.user_id > 0 ? (
                    <div className="flex items-center justify-between">
                        <div className="flex items-center gap-2 text-sm">
                            <span className="font-medium">
                                {subscription.user?.username || `User #${subscription.user_id}`}
                            </span>
                            {subscription.user?.telegram_id && subscription.user.telegram_id > 0 && (
                                <span className="text-xs text-muted-foreground font-mono flex items-center gap-1">
                                    <FaTelegram className="w-3 h-3 text-[#229ED9]" />
                                    {subscription.user.telegram_id}
                                </span>
                            )}
                        </div>
                        <div className="flex gap-1">
                            <Button variant="ghost" size="sm" className="h-7 text-xs" onClick={onAssignUser}>
                                Change
                            </Button>
                            <Button
                                variant="ghost"
                                size="sm"
                                className="h-7 text-xs text-red-600 hover:text-red-700"
                                onClick={onUnlinkUser}
                            >
                                Unlink
                            </Button>
                        </div>
                    </div>
                ) : (
                    <div className="flex items-center justify-between">
                        <span className="text-sm text-muted-foreground">No user linked</span>
                        <Button variant="outline" size="sm" className="h-7 text-xs" onClick={onAssignUser}>
                            <UserPlus className="w-3 h-3 mr-1" />
                            Assign
                        </Button>
                    </div>
                )}
            </div>
        </div>
    )
}
