import { useState, type ReactNode } from "react"
import { ChevronRight, Info, CheckCircle2, AlertTriangle } from "lucide-react"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"

// Layout primitives shared by the inbound/outbound dialogs and the stream
// forms they embed. One look for section headers, callouts, on/off rows and
// "more fields" disclosures, so the tabs stop mixing three of each.

export function SectionTitle({ children, className }: { children: ReactNode; className?: string }) {
    return (
        <span className={cn("text-xs font-medium uppercase tracking-[0.04em] text-text-tertiary", className)}>
            {children}
        </span>
    )
}

interface FormSectionProps {
    title: ReactNode
    /** Right-aligned slot in the header row (a button, a search box, a hint). */
    right?: ReactNode
    children: ReactNode
    className?: string
}

export function FormSection({ title, right, children, className }: FormSectionProps) {
    return (
        <section className={cn("flex flex-col gap-3", className)}>
            <div className="flex min-h-5 items-center justify-between gap-3">
                <SectionTitle>{title}</SectionTitle>
                {right}
            </div>
            {children}
        </section>
    )
}

type CalloutTone = "info" | "success" | "warning"

const calloutIcon: Record<CalloutTone, typeof Info> = {
    info: Info,
    success: CheckCircle2,
    warning: AlertTriangle,
}

const calloutIconColor: Record<CalloutTone, string> = {
    info: "text-status-info",
    success: "text-status-success",
    warning: "text-status-warning",
}

export function Callout({ tone = "info", children, className }: { tone?: CalloutTone; children: ReactNode; className?: string }) {
    const Icon = calloutIcon[tone]
    return (
        <div className={cn(
            "flex items-start gap-2.5 rounded-md border border-border-subtle bg-card px-3 py-2.5 text-[13px] leading-[18px] text-muted-foreground",
            className,
        )}>
            <Icon className={cn("mt-px h-4 w-4 shrink-0", calloutIconColor[tone])} />
            <div className="min-w-0 flex-1">{children}</div>
        </div>
    )
}

interface SwitchRowProps {
    label: ReactNode
    help?: ReactNode
    checked: boolean
    onCheckedChange: (checked: boolean) => void
    id?: string
    disabled?: boolean
    className?: string
}

export function SwitchRow({ label, help, checked, onCheckedChange, id, disabled, className }: SwitchRowProps) {
    return (
        <div className={cn("flex items-center justify-between gap-4", className)}>
            <div className="flex min-w-0 flex-col gap-0.5">
                <Label htmlFor={id} className="text-sm font-medium leading-5">{label}</Label>
                {help && <p className="text-xs text-muted-foreground">{help}</p>}
            </div>
            <Switch id={id} checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
        </div>
    )
}

interface DisclosureProps {
    title: ReactNode
    /** Short read-only summary shown while collapsed ("Padding, Xmux · defaults"). */
    summary?: ReactNode
    defaultOpen?: boolean
    open?: boolean
    onOpenChange?: (open: boolean) => void
    children: ReactNode
    className?: string
}

export function Disclosure({ title, summary, defaultOpen = false, open, onOpenChange, children, className }: DisclosureProps) {
    const [internalOpen, setInternalOpen] = useState(defaultOpen)
    const isOpen = open ?? internalOpen
    const toggle = () => {
        const next = !isOpen
        setInternalOpen(next)
        onOpenChange?.(next)
    }

    return (
        <div className={cn("rounded-md border bg-white/[0.02]", className)}>
            <button
                type="button"
                onClick={toggle}
                aria-expanded={isOpen}
                className="flex w-full items-center gap-2 rounded-md px-3 py-2.5 text-left transition-colors hover:bg-accent/40"
            >
                <ChevronRight className={cn("h-4 w-4 shrink-0 text-text-tertiary transition-transform", isOpen && "rotate-90")} />
                <span className="text-[13px] font-medium leading-[18px]">{title}</span>
                {summary && !isOpen && (
                    <span className="ml-auto truncate text-xs text-text-tertiary">{summary}</span>
                )}
            </button>
            {isOpen && (
                <div className="space-y-4 border-t px-3 pb-3 pt-3">
                    {children}
                </div>
            )}
        </div>
    )
}
