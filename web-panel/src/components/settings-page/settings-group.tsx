import { useState, type ReactNode } from "react"
import { ChevronRight, CircleAlert, Info, Loader2, Pencil, type LucideIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

/** A group's saved state, shown as the colour of its icon. */
export type GroupTone = "ok" | "warn" | "danger"

const TILE: Record<GroupTone | "none", string> = {
    ok: "bg-emerald-500/10 text-emerald-500",
    warn: "bg-amber-500/10 text-amber-500",
    danger: "bg-red-500/10 text-red-500",
    none: "bg-muted text-muted-foreground",
}

export interface GroupFooterProps {
    dirty: number
    /** what saving does, next to the button that does it */
    effect?: { text: string; danger?: boolean } | null
    /** the last save failed; the edits stay */
    error?: string | null
    invalid?: boolean
    saving: boolean
    onCancel: () => void
    onSave: () => void
}

export interface SettingsGroupProps {
    id: string
    icon: LucideIcon
    title: string
    description?: string
    tone?: GroupTone
    /** state no row shows, such as a link that is down */
    badge?: ReactNode
    /** a header button in place of Edit */
    action?: ReactNode
    edit?: { editing: boolean; onEdit: () => void; disabledReason?: string }
    notice?: ReactNode
    /** greys the rows out; the reason says why and what to do */
    unavailable?: ReactNode
    advanced?: { count: number; held?: boolean; openWhenEditing?: boolean; children: ReactNode }
    footer?: GroupFooterProps | null
    children?: ReactNode
}

/** One card per group: its rows sit straight inside, and Edit turns them into a form. */
export function SettingsGroup({ id, icon: Icon, title, description, tone, badge, action, edit, notice, unavailable, advanced, footer, children }: SettingsGroupProps) {
    const [advOpen, setAdvOpen] = useState(false)
    const editing = !!edit?.editing
    const advShown = !!advanced && (advOpen || !!advanced.held || (editing && !!advanced.openWhenEditing))
    const titleId = `group-${id}-title`

    return (
        <section
            id={`group-${id}`}
            aria-labelledby={titleId}
            className={cn(
                "overflow-hidden rounded-xl border bg-card transition-shadow",
                editing ? "border-border shadow-md ring-1 ring-border" : "border-border/60",
            )}
        >
            <header className="flex items-center gap-x-3 border-b border-border/40 px-4 py-2.5">
                <span className={cn("flex size-7 shrink-0 items-center justify-center rounded-md", TILE[tone ?? "none"], unavailable && "opacity-60")}>
                    <Icon className="size-3.5" aria-hidden />
                </span>
                <div className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-3 gap-y-0.5">
                    <h3 id={titleId} className="text-sm font-semibold">{title}</h3>
                    {description && <p className="min-w-0 text-xs text-muted-foreground @max-[559px]:basis-full">{description}</p>}
                </div>
                <div className="ml-auto flex shrink-0 items-center gap-2">
                    {badge}
                    {action}
                    {edit && !editing && (
                        <span title={edit.disabledReason}>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={edit.onEdit}
                                disabled={!!edit.disabledReason}
                                aria-label={`Edit ${title}`}
                                className="h-8 gap-1.5 px-2.5 text-muted-foreground hover:text-foreground"
                            >
                                <Pencil className="size-3.5" aria-hidden />Edit
                            </Button>
                        </span>
                    )}
                </div>
            </header>
            {notice && <Note>{notice}</Note>}
            {unavailable && <Note>{unavailable}</Note>}
            <div className={cn(unavailable && "pointer-events-none opacity-45 saturate-50")} aria-disabled={unavailable ? true : undefined}>
                {children && <div className="divide-y divide-border/40">{children}</div>}
                {advanced && (
                    <div className="border-t border-border/40">
                        <button
                            type="button"
                            aria-expanded={advShown}
                            onClick={() => { if (!advanced.held) setAdvOpen(o => !o) }}
                            title={advanced.held ? "Stays open while it has unsaved changes" : undefined}
                            className="flex w-full items-center gap-1.5 px-4 py-2.5 text-left text-xs font-medium text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
                        >
                            <ChevronRight className={cn("size-3.5 transition-transform", advShown && "rotate-90")} aria-hidden />
                            Advanced · {advanced.count}
                        </button>
                        {advShown && <div className="divide-y divide-border/40 border-t border-border/40">{advanced.children}</div>}
                    </div>
                )}
            </div>
            {footer && <GroupFooter {...footer} />}
        </section>
    )
}

function Note({ children }: { children: ReactNode }) {
    return (
        <div className="mx-4 mt-3 flex items-start gap-2 rounded-md bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
            <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
            <div className="min-w-0 flex-1">{children}</div>
        </div>
    )
}

export function GroupFooter({ dirty, effect, error, invalid, saving, onCancel, onSave }: GroupFooterProps) {
    let message: ReactNode
    if (saving) message = <span className="flex items-center gap-2"><Loader2 className="size-3.5 animate-spin" aria-hidden />Saving…</span>
    else if (error) message = <span className="flex items-start gap-1.5 text-red-500"><CircleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden />{error} Your edits are kept.</span>
    else if (invalid) message = <span className="text-red-500">Fix the highlighted fields to save.</span>
    else if (dirty) message = (
        <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
            <span className="flex items-center gap-1.5 font-medium text-foreground"><span className="size-1.5 rounded-full bg-amber-500" />{dirty} unsaved {dirty === 1 ? "change" : "changes"}</span>
            {effect && <span className={cn(effect.danger ? "font-medium text-red-500" : "text-muted-foreground")}>{effect.text}</span>}
        </span>
    )
    else message = <span className="text-muted-foreground">Nothing changed yet</span>

    return (
        <footer role="region" aria-label="Unsaved changes" className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-border/60 bg-muted/30 px-4 py-2.5 text-xs">
            <div className="mr-auto min-w-0">{message}</div>
            <div className="flex gap-1.5">
                <Button type="button" variant="ghost" size="sm" onClick={onCancel} disabled={saving} className="h-8">Cancel</Button>
                <Button type="button" size="sm" onClick={onSave} disabled={saving || !dirty || invalid} className="h-8">Save</Button>
            </div>
        </footer>
    )
}

export interface SettingRowProps {
    label: ReactNode
    /** links the label to its control while editing */
    htmlFor?: string
    hint?: ReactNode
    error?: string
    /** a button at the row's end, such as Change or a copy */
    action?: ReactNode
    children?: ReactNode
}

/** Label, value (or its control while editing), then an action. */
export function SettingRow({ label, htmlFor, hint, error, action, children }: SettingRowProps) {
    const Label = htmlFor ? "label" : "div"
    return (
        <div className="grid grid-cols-[150px_minmax(0,1fr)_auto] items-start gap-x-4 px-4 py-2.5 transition-colors hover:bg-muted/20 @max-[559px]:grid-cols-[minmax(0,1fr)_auto] @max-[559px]:gap-y-1">
            <Label htmlFor={htmlFor} className="pt-1.5 text-sm text-muted-foreground @max-[559px]:col-span-1 @max-[559px]:pt-0">{label}</Label>
            <div className="min-w-0 space-y-1 text-sm @max-[559px]:col-start-1 @max-[559px]:row-start-2">
                <div className="flex min-h-8 min-w-0 items-center">{children}</div>
                {error
                    ? <p className="text-xs text-red-500">{error}</p>
                    : hint && <p className="text-xs text-muted-foreground">{hint}</p>}
            </div>
            <div className="flex min-h-8 items-center justify-end gap-1.5 @max-[559px]:col-start-2 @max-[559px]:row-span-2 @max-[559px]:row-start-1 @max-[559px]:self-center">{action}</div>
        </div>
    )
}

/** A switch's saved value, read out. */
export function OnOff({ on, onText = "On", offText = "Off" }: { on: boolean; onText?: string; offText?: string }) {
    return (
        <span className="inline-flex items-center gap-1.5">
            <span className={cn("size-1.5 rounded-full", on ? "bg-emerald-500" : "bg-muted-foreground/60")} aria-hidden />
            {on ? onText : offText}
        </span>
    )
}

/** A value that may be empty. wrap lets long ones (URLs, addresses) break instead of truncating. */
export function Value({ children, mono, wrap, empty = "Not set" }: { children?: ReactNode; mono?: boolean; wrap?: boolean; empty?: string }) {
    const blank = children === undefined || children === null || children === ""
    return blank
        ? <span className="text-muted-foreground">{empty}</span>
        : <span className={cn("min-w-0", wrap ? "[overflow-wrap:anywhere]" : "truncate", mono && "font-mono text-[13px]")}>{children}</span>
}

export function StateBadge({ tone, children }: { tone: GroupTone | "muted"; children: ReactNode }) {
    return (
        <span className={cn(
            "inline-flex h-6 items-center gap-1.5 rounded-full px-2.5 text-xs font-medium",
            tone === "ok" && "bg-emerald-500/10 text-emerald-500",
            tone === "warn" && "bg-amber-500/10 text-amber-500",
            tone === "danger" && "bg-red-500/10 text-red-500",
            tone === "muted" && "bg-muted text-muted-foreground",
        )}>
            <span className="size-1.5 rounded-full bg-current" aria-hidden />{children}
        </span>
    )
}

export type { LucideIcon }
