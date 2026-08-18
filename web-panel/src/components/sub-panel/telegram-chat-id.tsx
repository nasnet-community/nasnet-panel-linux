import { useState, useRef } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Card, CardContent } from "@/components/ui/card"
import { BsTelegram } from "react-icons/bs"
import { Check, Pencil, X, Loader2, Trash2, ExternalLink } from "lucide-react"
import { motion } from "framer-motion"
import { toast } from "sonner"
import { getApiBaseUrl } from '@/lib/config'

const API_BASE_URL = getApiBaseUrl()

interface TelegramChatIdProps {
    uuid: string
    currentChatId: number
    // True when notifications reach the subscription via the owner's linked
    // Telegram account, even if no explicit per-sub chat ID is set.
    connectedViaAccount?: boolean
    botUsername?: string
}

export function TelegramChatId({ uuid, currentChatId, connectedViaAccount, botUsername }: TelegramChatIdProps) {
    const [isEditing, setIsEditing] = useState(false)
    const [value, setValue] = useState("")
    const [connecting, setConnecting] = useState(false)
    const [showManual, setShowManual] = useState(false)
    const inputRef = useRef<HTMLInputElement>(null)
    const queryClient = useQueryClient()

    const mutation = useMutation({
        mutationFn: async (chatId: number) => {
            const res = await fetch(
                `${API_BASE_URL}/api/v1/public/sub/${uuid}/telegram-chat-id`,
                {
                    method: "PUT",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ chat_id: chatId }),
                }
            )
            if (!res.ok) {
                const json = await res.json().catch(() => null)
                const raw = json?.error
                let msg: string
                if (typeof raw === "string") {
                    msg = raw
                } else if (raw && typeof raw === "object") {
                    msg = (raw as { message?: string }).message || "Failed to update"
                } else {
                    msg = "Failed to update"
                }
                throw new Error(msg)
            }
            return res.json()
        },
        onSuccess: (_data, chatId) => {
            queryClient.invalidateQueries({ queryKey: ["sub-panel", uuid] })
            setIsEditing(false)
            setShowManual(false)
            toast.success(chatId === 0 ? "Telegram Chat ID cleared" : "Telegram Chat ID updated")
        },
        onError: (err: Error) => {
            toast.error(err.message)
        },
    })

    const handleEdit = () => {
        setValue(currentChatId > 0 ? String(currentChatId) : "")
        setIsEditing(true)
        setTimeout(() => inputRef.current?.focus(), 0)
    }

    const handleCancel = () => {
        setIsEditing(false)
        setShowManual(false)
        setValue("")
    }

    const handleSave = () => {
        const parsed = parseInt(value, 10)
        if (!value || isNaN(parsed) || parsed <= 0) {
            toast.error("Enter a valid positive Chat ID")
            return
        }
        mutation.mutate(parsed)
    }

    const handleClear = () => {
        mutation.mutate(0)
    }

    // Connect: mint a short-lived deep link and open it. The bot binds the real
    // sender's chat ID — no copy-paste, verified.
    const handleConnect = async () => {
        setConnecting(true)
        try {
            const res = await fetch(
                `${API_BASE_URL}/api/v1/public/sub/${uuid}/telegram-link-token`,
                { method: "POST" }
            )
            const json = await res.json().catch(() => null)
            if (!res.ok || !json?.success) {
                const raw = json?.error
                const msg =
                    typeof raw === "string"
                        ? raw
                        : (raw as { message?: string })?.message || "Could not start Telegram connect"
                throw new Error(msg)
            }
            const url = json?.data?.url
            if (!url) throw new Error("No link returned")
            window.open(url, "_blank", "noopener,noreferrer")
        } catch (e) {
            toast.error(e instanceof Error ? e.message : "Could not start Telegram connect")
        } finally {
            setConnecting(false)
        }
    }

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter") handleSave()
        if (e.key === "Escape") handleCancel()
    }

    const isSet = currentChatId > 0

    return (
        <Card className="border-border/50 bg-card/60 backdrop-blur-md py-0 gap-0">
            <CardContent className="p-3.5 sm:p-4 md:p-5 space-y-2">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                        <BsTelegram className="w-4 h-4 text-sky-400" />
                        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
                            Telegram Chat ID
                        </h2>
                    </div>
                    {isSet && !isEditing && (
                        <div className="flex items-center gap-1">
                            <motion.button
                                type="button"
                                whileTap={{ scale: 0.95 }}
                                className="flex items-center gap-1.5 min-h-11 px-2 text-xs text-muted-foreground hover:text-foreground rounded-md hover:bg-muted/50 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60"
                                onClick={handleEdit}
                            >
                                <Pencil className="w-3 h-3" />
                                Edit
                            </motion.button>
                            <motion.button
                                type="button"
                                whileTap={{ scale: 0.95 }}
                                disabled={mutation.isPending}
                                className="flex items-center gap-1.5 min-h-11 px-2 text-xs text-destructive hover:text-destructive/80 rounded-md hover:bg-destructive/10 transition-colors disabled:opacity-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60"
                                onClick={handleClear}
                            >
                                {mutation.isPending ? (
                                    <Loader2 className="w-3 h-3 animate-spin" />
                                ) : (
                                    <Trash2 className="w-3 h-3" />
                                )}
                                Clear
                            </motion.button>
                        </div>
                    )}
                </div>

                {/* Display mode: show current value + bot link */}
                {isSet && !isEditing && (
                    <div className="space-y-1.5">
                        <div className="flex items-center gap-2 py-1.5 px-2">
                            <Check className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
                            <span className="text-sm font-mono">{currentChatId}</span>
                        </div>
                        {botUsername && (
                            <a
                                href={`https://t.me/${botUsername}`}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="flex items-center gap-1.5 min-h-11 px-2 text-xs font-medium text-sky-700 dark:text-sky-400 hover:text-sky-500 transition-colors rounded-md hover:bg-muted/50 w-fit focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60"
                            >
                                <BsTelegram className="w-3.5 h-3.5" />
                                <span>@{botUsername}</span>
                                <ExternalLink className="w-3 h-3" />
                            </a>
                        )}
                    </div>
                )}

                {/* No explicit chat ID. Either already reachable via the owner's
                    Telegram account (show that, offer override), or prompt to connect. */}
                {!isSet && !isEditing && (
                    <div className="space-y-2">
                        {connectedViaAccount ? (
                            <>
                                <div className="flex items-center gap-2 py-1.5 px-2">
                                    <Check className="w-3.5 h-3.5 text-emerald-400 shrink-0" />
                                    <span className="text-sm">Connected via your Telegram account</span>
                                </div>
                                {botUsername && (
                                    <a
                                        href={`https://t.me/${botUsername}`}
                                        target="_blank"
                                        rel="noopener noreferrer"
                                        className="flex items-center gap-1.5 min-h-11 px-2 text-xs font-medium text-sky-700 dark:text-sky-400 hover:text-sky-500 transition-colors rounded-md hover:bg-muted/50 w-fit focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60"
                                    >
                                        <BsTelegram className="w-3.5 h-3.5" />
                                        <span>@{botUsername}</span>
                                        <ExternalLink className="w-3 h-3" />
                                    </a>
                                )}
                                <p className="text-xs text-muted-foreground">
                                    Notifications go to your Telegram. Set a specific Chat ID to override.
                                </p>
                            </>
                        ) : (
                            <>
                                <p className="text-xs text-muted-foreground">
                                    Connect your Telegram to receive subscription notifications.
                                </p>
                                {botUsername ? (
                                    <motion.button
                                        type="button"
                                        whileTap={{ scale: 0.97 }}
                                        disabled={connecting}
                                        onClick={handleConnect}
                                        className="w-full flex items-center justify-center gap-2 h-9 px-3 text-sm font-medium rounded-md bg-sky-500/15 text-sky-400 hover:bg-sky-500/25 transition-colors disabled:opacity-50"
                                    >
                                        {connecting ? (
                                            <Loader2 className="w-4 h-4 animate-spin" />
                                        ) : (
                                            <BsTelegram className="w-4 h-4" />
                                        )}
                                        Connect Telegram
                                    </motion.button>
                                ) : (
                                    <p className="text-xs text-muted-foreground">Telegram bot is not configured.</p>
                                )}
                            </>
                        )}
                        {!showManual && (
                            <button
                                type="button"
                                onClick={() => {
                                    setValue("")
                                    setShowManual(true)
                                    setTimeout(() => inputRef.current?.focus(), 0)
                                }}
                                className="-ml-2 min-h-11 px-2 rounded-md text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/60"
                            >
                                › {connectedViaAccount ? "set a specific Chat ID" : "enter Chat ID manually"}
                            </button>
                        )}
                    </div>
                )}

                {/* Manual input: editing an existing value, or expanded fallback */}
                {(isEditing || (!isSet && showManual)) && (
                    <div className="flex items-center gap-2">
                        <input
                            ref={inputRef}
                            id="sub-chat-id"
                            aria-label="Telegram Chat ID"
                            type="number"
                            placeholder="Enter Chat ID"
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                            onKeyDown={handleKeyDown}
                            disabled={mutation.isPending}
                            className="flex-1 h-8 px-2.5 text-sm font-mono bg-muted/50 border border-border/50 rounded-md outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground/50 [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                        />
                        <div className="flex items-center gap-1">
                            <motion.button
                                type="button"
                                whileTap={{ scale: 0.9 }}
                                disabled={mutation.isPending}
                                onClick={handleSave}
                                className="h-8 w-8 flex items-center justify-center rounded-md bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20 transition-colors disabled:opacity-50"
                            >
                                {mutation.isPending ? (
                                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                                ) : (
                                    <Check className="w-3.5 h-3.5" />
                                )}
                            </motion.button>
                            <motion.button
                                type="button"
                                whileTap={{ scale: 0.9 }}
                                disabled={mutation.isPending}
                                onClick={handleCancel}
                                className="h-8 w-8 flex items-center justify-center rounded-md text-muted-foreground hover:bg-muted/50 transition-colors disabled:opacity-50"
                            >
                                <X className="w-3.5 h-3.5" />
                            </motion.button>
                        </div>
                    </div>
                )}
            </CardContent>
        </Card>
    )
}
