import { useState, useMemo } from "react"
import { QRCodeSVG } from "qrcode.react"
import { Check, Copy, Pencil, QrCode, RefreshCw, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { EditableField } from "@/components/ui/editable-field"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Separator } from "@/components/ui/separator"
import { cn, copyToClipboard, generateUUID } from "@/lib/utils"
import { toast } from "sonner"
import type { Subscription } from "@/lib/types"
import type { Account } from "@/lib/api/accounts"
import { SectionHeader } from "./section-header"

interface IdentitySectionProps {
    subscription: Subscription
    accounts: Account[]
    accountsLoading: boolean
    onRename: (label: string) => Promise<void>
    onRegenerateKey: (key: string) => Promise<void>
    onSetUUID: (uuid: string) => Promise<void>
}

function buildSharedUUID(accounts: Account[]): { uuid: string; mixed: boolean } {
    if (accounts.length === 0) return { uuid: "", mixed: false }
    const first = accounts[0].uuid
    const mixed = accounts.some((a) => a.uuid !== first)
    return { uuid: first, mixed }
}

export function IdentitySection({
    subscription,
    accounts,
    accountsLoading,
    onRename,
    onRegenerateKey,
    onSetUUID,
}: IdentitySectionProps) {
    const [isEditingLabel, setIsEditingLabel] = useState(false)
    const [labelDraft, setLabelDraft] = useState(subscription.label || "")
    const [labelSaving, setLabelSaving] = useState(false)
    const [linkCopied, setLinkCopied] = useState(false)
    const [uuidEditEnabled, setUuidEditEnabled] = useState(false)

    const { uuid: sharedUUID, mixed: uuidsDiffer } = useMemo(() => buildSharedUUID(accounts), [accounts])
    const keyValue = subscription.link_key || subscription.config_id || ""
    const subscriptionURL = subscription.subscription_url || ""

    const handleLabelSave = async () => {
        if (labelDraft === (subscription.label || "")) {
            setIsEditingLabel(false)
            return
        }
        setLabelSaving(true)
        try {
            await onRename(labelDraft)
            setIsEditingLabel(false)
        } finally {
            setLabelSaving(false)
        }
    }

    const handleLinkCopy = async () => {
        if (!subscriptionURL) return
        await copyToClipboard(subscriptionURL)
        setLinkCopied(true)
        toast.success("Subscription link copied")
        setTimeout(() => setLinkCopied(false), 1500)
    }

    return (
        <div className="space-y-4">
            {/* Label */}
            <div className="space-y-1.5">
                <SectionHeader tone="default">Label</SectionHeader>
                <div className="flex items-center gap-2 min-w-0">
                    {isEditingLabel ? (
                        <>
                            <Input
                                value={labelDraft}
                                onChange={(e) => setLabelDraft(e.target.value)}
                                placeholder="Add a label…"
                                className="flex-1 h-7 text-sm"
                                autoFocus
                                onKeyDown={(e) => {
                                    if (e.key === "Enter") handleLabelSave()
                                    if (e.key === "Escape") {
                                        setLabelDraft(subscription.label || "")
                                        setIsEditingLabel(false)
                                    }
                                }}
                                aria-label="Subscription label"
                            />
                            <Button
                                size="sm"
                                variant="outline"
                                className="h-7 text-xs shrink-0"
                                onClick={handleLabelSave}
                                disabled={labelSaving || labelDraft === (subscription.label || "")}
                            >
                                Save
                            </Button>
                            <Button
                                size="sm"
                                variant="ghost"
                                className="h-7 w-7 p-0 shrink-0"
                                onClick={() => {
                                    setLabelDraft(subscription.label || "")
                                    setIsEditingLabel(false)
                                }}
                                aria-label="Cancel label edit"
                            >
                                <X className="w-3.5 h-3.5" />
                            </Button>
                        </>
                    ) : (
                        <>
                            <span className="text-sm truncate flex-1">
                                {subscription.label || (
                                    <span className="text-muted-foreground italic">No label</span>
                                )}
                            </span>
                            <Button
                                variant="ghost"
                                size="sm"
                                className="h-6 w-6 p-0 shrink-0"
                                onClick={() => {
                                    setLabelDraft(subscription.label || "")
                                    setIsEditingLabel(true)
                                }}
                                aria-label="Edit label"
                            >
                                <Pencil className="w-3 h-3" />
                            </Button>
                        </>
                    )}
                </div>
            </div>

            {/* Subscription Link — promoted as the hero artifact with QR */}
            <div className="space-y-1.5">
                <SectionHeader tone="default">
                    Subscription Link
                </SectionHeader>
                <div className="rounded-lg border bg-gradient-to-br from-primary/5 via-background to-background p-3 space-y-2">
                    <button
                        type="button"
                        className="w-full text-left px-2.5 py-2 rounded-md bg-muted/60 hover:bg-muted active:bg-muted/80 transition-colors font-mono text-xs break-all group"
                        onClick={handleLinkCopy}
                        aria-label="Copy subscription link"
                    >
                        <div className="flex items-start gap-2">
                            <span className="flex-1 min-w-0 break-all">
                                {subscriptionURL || "Generating link…"}
                            </span>
                            <span className="shrink-0 mt-0.5">
                                {linkCopied ? (
                                    <Check className="w-3.5 h-3.5 text-emerald-500" />
                                ) : (
                                    <Copy className="w-3.5 h-3.5 text-muted-foreground/70 group-hover:text-foreground" />
                                )}
                            </span>
                        </div>
                    </button>
                    <div className="flex items-center gap-2">
                        <Popover>
                            <PopoverTrigger asChild>
                                <Button
                                    variant="outline"
                                    size="sm"
                                    className="h-7 text-xs"
                                    disabled={!subscriptionURL}
                                >
                                    <QrCode className="w-3.5 h-3.5 mr-1.5" />
                                    Show QR
                                </Button>
                            </PopoverTrigger>
                            <PopoverContent className="w-auto p-3" align="start">
                                {subscriptionURL && (
                                    <div className="flex flex-col items-center gap-2">
                                        <div className="bg-white p-2 rounded-md">
                                            <QRCodeSVG value={subscriptionURL} size={192} level="M" />
                                        </div>
                                        <p className="text-[10px] text-muted-foreground max-w-[200px] text-center">
                                            Scan with your VPN client to import this subscription.
                                        </p>
                                    </div>
                                )}
                            </PopoverContent>
                        </Popover>
                        <Button
                            variant="ghost"
                            size="sm"
                            className="h-7 text-xs ml-auto"
                            onClick={handleLinkCopy}
                            disabled={!subscriptionURL}
                        >
                            {linkCopied ? (
                                <><Check className="w-3.5 h-3.5 mr-1 text-emerald-500" /> Copied</>
                            ) : (
                                <><Copy className="w-3.5 h-3.5 mr-1" /> Copy link</>
                            )}
                        </Button>
                    </div>
                </div>
            </div>

            <Separator />

            {/* Subscription Key */}
            <div className="space-y-1.5">
                <SectionHeader>Subscription Key</SectionHeader>
                <EditableField
                    value={keyValue}
                    onApply={onRegenerateKey}
                    mono
                    copyable
                    regenerate={generateUUID}
                    placeholder="Subscription key"
                    ariaLabel="Subscription key"
                />
                <p className="text-[10px] text-muted-foreground">
                    Changes the URL only; Xray credentials stay intact.
                </p>
            </div>

            <Separator />

            {/* UUID — shown as read-only summary with optional unlock; handles mixed state */}
            <div className="space-y-1.5">
                <SectionHeader
                    right={
                        uuidsDiffer && (
                            <span className="text-[10px] font-semibold uppercase tracking-wider text-amber-500">
                                Mixed
                            </span>
                        )
                    }
                >
                    Account UUID
                </SectionHeader>
                {accountsLoading ? (
                    <div className="h-7 bg-muted/20 animate-pulse rounded-md" />
                ) : accounts.length === 0 ? (
                    <p className="text-xs text-muted-foreground">No accounts to set a UUID for.</p>
                ) : uuidsDiffer && !uuidEditEnabled ? (
                    <div className="rounded-md border border-amber-500/30 bg-amber-500/5 dark:bg-amber-500/10 p-2.5 space-y-1.5">
                        <p className="text-xs">
                            <span className="font-semibold text-amber-600 dark:text-amber-400">Accounts have different UUIDs.</span>{" "}
                            Setting a single UUID will overwrite every account under this subscription.
                        </p>
                        <Button
                            variant="outline"
                            size="sm"
                            className="h-6 text-[11px]"
                            onClick={() => setUuidEditEnabled(true)}
                        >
                            Override all
                        </Button>
                    </div>
                ) : (
                    <EditableField
                        value={sharedUUID}
                        onApply={onSetUUID}
                        mono
                        copyable
                        regenerate={generateUUID}
                        placeholder="Account UUID"
                        ariaLabel="Shared account UUID"
                    />
                )}
                <p className={cn("text-[10px] text-muted-foreground", uuidsDiffer && "text-amber-600 dark:text-amber-400")}>
                    {uuidsDiffer
                        ? `This subscription has ${accounts.length} account${accounts.length === 1 ? "" : "s"} with divergent UUIDs.`
                        : "Applied atomically to all accounts under this subscription."}
                </p>
            </div>
        </div>
    )
}
