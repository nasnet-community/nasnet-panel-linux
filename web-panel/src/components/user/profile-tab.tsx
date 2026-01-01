import { useState } from "react"
import { Card, CardContent } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import {
    HiOutlineShieldCheck,
    HiOutlineDuplicate,
    HiOutlineCheck,
    HiOutlineX,
    HiOutlineRefresh,
    // HiOutlineChat,
    HiOutlineExternalLink,
} from "react-icons/hi"
import type { UserDetails } from "@/lib/types"
import { copyToClipboard } from "@/lib/utils"
import { toast } from "sonner"
import { useUpdateTelegramID } from "@/lib/queries"
import { AdminNotesPanel } from "./admin-notes-panel"
import { NodeAccessList } from "./node-access-list"
import { CollapsiblePanel } from "./collapsible-panel"
import { useIsMobile } from "@/hooks/use-is-mobile"

interface ProfileTabProps {
    user: UserDetails
    onToggleAdmin: () => void
    actionLoading: boolean
}

function CopyableField({ label, value }: { label: string; value: string }) {
    return (
        <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                {label}
            </span>
            <button
                onClick={async () => {
                    await copyToClipboard(value)
                    toast.success("Copied to clipboard")
                }}
                className="flex items-center gap-2 group hover:bg-muted/50 p-1 -ml-1 rounded transition-colors text-left"
            >
                <span className="font-mono text-sm text-foreground/90">{value}</span>
                <HiOutlineDuplicate className="w-3.5 h-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
            </button>
        </div>
    )
}

function getTelegramLink(user: UserDetails): string | null {
    if (user.username) return `tg://resolve?domain=${user.username}`
    if (user.telegram_id > 0) return `tg://user?id=${user.telegram_id}`
    return null
}

export function ProfileTab({ user, onToggleAdmin, actionLoading }: ProfileTabProps) {
    const isMobile = useIsMobile()
    const [editingTelegramId, setEditingTelegramId] = useState(false)
    const [telegramIdValue, setTelegramIdValue] = useState("")
    const updateTelegramMutation = useUpdateTelegramID()
    const telegramLink = getTelegramLink(user)

    const hasName = (user.first_name && user.first_name !== "-") || (user.last_name && user.last_name !== "-")
    const nameDisplay = hasName
        ? [user.first_name !== "-" ? user.first_name : "", user.last_name !== "-" ? user.last_name : ""].filter(Boolean).join(" ")
        : null

    return (
        <div className={isMobile ? "space-y-4" : "grid grid-cols-3 gap-4"}>
            {/* Section 1: User Info */}
            <Card className="border-0 shadow-sm bg-card/50">
                <CardContent className="p-5 space-y-4">
                    <h3 className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                        User Info
                    </h3>

                    <CopyableField label="Telegram Username" value={user.username ? `@${user.username}` : "—"} />

                    {/* Chat ID (editable) */}
                    <div className="space-y-1">
                        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                            Chat ID
                        </span>
                        {editingTelegramId ? (
                            <div className="flex items-center gap-2">
                                <Input
                                    type="number"
                                    value={telegramIdValue}
                                    onChange={(e) => setTelegramIdValue(e.target.value)}
                                    className="h-8 font-mono text-sm flex-1"
                                    placeholder="Telegram Chat ID"
                                    autoFocus
                                />
                                <Button
                                    variant="ghost" size="icon" className="h-8 w-8 text-emerald-600"
                                    disabled={updateTelegramMutation.isPending}
                                    onClick={() => {
                                        const newId = parseInt(telegramIdValue)
                                        if (isNaN(newId)) { toast.error("Invalid Telegram ID"); return }
                                        updateTelegramMutation.mutate(
                                            { userId: user.id, telegramId: newId },
                                            { onSuccess: () => setEditingTelegramId(false) }
                                        )
                                    }}
                                >
                                    {updateTelegramMutation.isPending
                                        ? <HiOutlineRefresh className="w-4 h-4 animate-spin" />
                                        : <HiOutlineCheck className="w-4 h-4" />}
                                </Button>
                                <Button variant="ghost" size="icon" className="h-8 w-8" onClick={() => setEditingTelegramId(false)}>
                                    <HiOutlineX className="w-4 h-4" />
                                </Button>
                            </div>
                        ) : (
                            <div className="group flex items-center gap-2">
                                <button
                                    onClick={async () => {
                                        await copyToClipboard(user.telegram_id.toString())
                                        toast.success("Copied to clipboard")
                                    }}
                                    className="flex items-center gap-2 hover:bg-muted/50 p-1 -ml-1 rounded transition-colors text-left"
                                >
                                    <span className="font-mono text-sm text-foreground/90">
                                        {user.telegram_id > 0 ? user.telegram_id : "Not set"}
                                    </span>
                                    <HiOutlineDuplicate className="w-3.5 h-3.5 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                                </button>
                                <button
                                    onClick={() => {
                                        setTelegramIdValue(user.telegram_id > 0 ? user.telegram_id.toString() : "")
                                        setEditingTelegramId(true)
                                    }}
                                    className="text-muted-foreground hover:text-primary opacity-0 group-hover:opacity-100 transition-all"
                                    title="Edit Chat ID"
                                >
                                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5">
                                        <path d="M2.695 14.763l-1.262 3.154a.5.5 0 00.65.65l3.155-1.262a4 4 0 001.343-.885L17.5 5.5a2.121 2.121 0 00-3-3L3.58 13.42a4 4 0 00-.885 1.343z" />
                                    </svg>
                                </button>
                            </div>
                        )}
                    </div>

                    {nameDisplay && <CopyableField label="Name" value={nameDisplay} />}

                    <div className="grid grid-cols-2 gap-4">
                        <div className="space-y-1">
                            <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">Language</span>
                            <div className="text-sm">{user.language?.toUpperCase() || "—"}</div>
                        </div>
                    </div>

                    <div className="space-y-1">
                        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">Joined</span>
                        <div className="text-sm">{new Date(user.created_at).toLocaleDateString(undefined, { dateStyle: "long" })}</div>
                    </div>
                </CardContent>
            </Card>

            {/* Section 2: Chat & Communication */}
            <Card className="border-0 shadow-sm bg-card/50">
                <CardContent className="p-5 space-y-4">
                    <h3 className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                        Chat & Communication
                    </h3>

                    {/*
                    <div className="bg-muted/30 rounded-lg p-4">
                        <div className="flex justify-between items-center mb-1">
                            <span className="text-2xl font-bold">
                                {(user as any).chat_message_count ?? "—"}
                            </span>
                            <span className="text-xs text-muted-foreground">messages</span>
                        </div>
                        <div className="text-xs text-muted-foreground">
                            {(user as any).chat_last_message_at
                                ? `Last: ${new Date((user as any).chat_last_message_at).toLocaleDateString()}`
                                : "Chat data unavailable"}
                        </div>
                    </div>
                    */}

                    <div className="flex flex-col gap-2">
                        {/*
                        <Button variant="default" size="sm" className="w-full" asChild>
                            <a href={`/chats?user=${user.id}`}>
                                <HiOutlineChat className="w-4 h-4 mr-2" />
                                Open Chat History
                            </a>
                        </Button>
                        */}
                        {telegramLink && (
                            <Button variant="outline" size="sm" className="w-full" asChild>
                                <a href={telegramLink} target="_blank" rel="noopener noreferrer">
                                    <HiOutlineExternalLink className="w-4 h-4 mr-2" />
                                    Message on Telegram
                                </a>
                            </Button>
                        )}
                    </div>
                </CardContent>
            </Card>

            {/* Section 3: Admin Tools */}
            <Card className="border-0 shadow-sm bg-card/50">
                <CardContent className="p-5 space-y-4">
                    <h3 className="text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                        Admin Tools
                    </h3>

                    <AdminNotesPanel userId={user.id} initialNotes={user.admin_notes || ""} />

                    <Separator />

                    <CollapsiblePanel title="Node Access" defaultOpen={false}>
                        <NodeAccessList userId={user.id} />
                    </CollapsiblePanel>

                    <Separator />

                    <Button variant="outline" className="w-full justify-start" onClick={onToggleAdmin} disabled={actionLoading}>
                        <HiOutlineShieldCheck className="w-4 h-4 mr-2" />
                        {user.is_admin ? "Revoke Admin Access" : "Grant Admin Access"}
                    </Button>
                </CardContent>
            </Card>
        </div>
    )
}
